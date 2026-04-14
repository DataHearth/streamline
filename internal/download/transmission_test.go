package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// trServer spins up a Transmission RPC test server. handler is dispatched on
// the decoded method name; the first request (no session id) gets the 409
// CSRF challenge so the client's handshake is always exercised.
func trServer(
	handler func(method string, args json.RawMessage) (string, any),
) *httptest.Server {
	GinkgoHelper()
	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Transmission-Session-Id") == "" {
				w.Header().Set("X-Transmission-Session-Id", "sess-123")
				w.WriteHeader(http.StatusConflict)
				return
			}
			var req struct {
				Method    string          `json:"method"`
				Arguments json.RawMessage `json:"arguments"`
			}
			Expect(json.NewDecoder(r.Body).Decode(&req)).To(Succeed())
			result, args := handler(req.Method, req.Arguments)
			if result == "" {
				result = "success"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(jsonBytes(map[string]any{
				"result": result, "arguments": args,
			}))
		},
	))
}

var _ = Describe("Transmission Client", Label("unit", "downloads"), func() {
	Describe("TestConnection", func() {
		It("completes the 409 session handshake then calls session-get", func() {
			srv := trServer(func(method string, _ json.RawMessage) (string, any) {
				Expect(method).To(Equal("session-get"))
				return "success", map[string]any{}
			})
			DeferCleanup(srv.Close)

			c := NewTransmission(srv.URL, "", "")
			Expect(c.TestConnection(context.Background())).To(Succeed())
		})

		It("maps 401 to ErrUnauthorized", func() {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}))
			DeferCleanup(srv.Close)

			err := NewTransmission(srv.URL, "u", "p").
				TestConnection(context.Background())
			Expect(err).To(MatchError(ErrUnauthorized))
		})
	})

	Describe("AddTorrent", func() {
		It("uploads metainfo and returns the lowercased hash", func() {
			srv := trServer(func(method string, args json.RawMessage) (string, any) {
				Expect(method).To(Equal("torrent-add"))
				var a struct {
					Metainfo string   `json:"metainfo"`
					Labels   []string `json:"labels"`
				}
				Expect(json.Unmarshal(args, &a)).To(Succeed())
				Expect(a.Metainfo).NotTo(BeEmpty())
				Expect(a.Labels).To(ContainElement(managedCategory))
				return "success", map[string]any{
					"torrent-added": map[string]any{"hashString": "ABCDEF"},
				}
			})
			DeferCleanup(srv.Close)

			hash, err := NewTransmission(srv.URL, "", "").
				AddTorrent(context.Background(), TorrentSource{Bytes: []byte("d8:announce")})
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal("abcdef"))
		})

		It("passes a magnet through as filename", func() {
			srv := trServer(func(_ string, args json.RawMessage) (string, any) {
				var a struct {
					Filename string `json:"filename"`
				}
				Expect(json.Unmarshal(args, &a)).To(Succeed())
				Expect(a.Filename).To(HavePrefix("magnet:"))
				return "success", map[string]any{
					"torrent-added": map[string]any{"hashString": "deadbeef"},
				}
			})
			DeferCleanup(srv.Close)

			hash, err := NewTransmission(srv.URL, "", "").AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:deadbeef"})
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal("deadbeef"))
		})

		It("returns ErrTorrentAlreadyExists on torrent-duplicate", func() {
			srv := trServer(func(_ string, _ json.RawMessage) (string, any) {
				return "success", map[string]any{
					"torrent-duplicate": map[string]any{"hashString": "dup"},
				}
			})
			DeferCleanup(srv.Close)

			_, err := NewTransmission(srv.URL, "", "").AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:dup"})
			Expect(err).To(MatchError(ErrTorrentAlreadyExists))
		})
	})

	Describe("GetTorrent", func() {
		It("maps fields, clamps a negative eta, and reports seeding", func() {
			srv := trServer(func(method string, _ json.RawMessage) (string, any) {
				Expect(method).To(Equal("torrent-get"))
				return "success", map[string]any{
					"torrents": []map[string]any{{
						"hashString":   "ABC",
						"name":         "Dune.2021",
						"percentDone":  1.0,
						"totalSize":    int64(8000000000),
						"downloadDir":  "/downloads",
						"rateDownload": int64(0),
						"eta":          -1,
						"status":       6,
					}},
				}
			})
			DeferCleanup(srv.Close)

			t, err := NewTransmission(srv.URL, "", "").
				GetTorrent(context.Background(), "abc")
			Expect(err).NotTo(HaveOccurred())
			Expect(t.Hash).To(Equal("abc"))
			Expect(t.Name).To(Equal("Dune.2021"))
			Expect(t.Status).To(Equal(StatusSeeding))
			Expect(t.ETA).To(Equal(int64(0)))
			Expect(t.SavePath).To(Equal("/downloads"))
		})

		It("returns ErrTorrentNotFound for an empty torrents list", func() {
			srv := trServer(func(_ string, _ json.RawMessage) (string, any) {
				return "success", map[string]any{"torrents": []any{}}
			})
			DeferCleanup(srv.Close)

			_, err := NewTransmission(srv.URL, "", "").
				GetTorrent(context.Background(), "missing")
			Expect(err).To(MatchError(ErrTorrentNotFound))
		})
	})

	Describe("RemoveTorrent", func() {
		It("sends ids and delete-local-data", func() {
			srv := trServer(func(method string, args json.RawMessage) (string, any) {
				Expect(method).To(Equal("torrent-remove"))
				var a struct {
					IDs    []string `json:"ids"`
					Delete bool     `json:"delete-local-data"`
				}
				Expect(json.Unmarshal(args, &a)).To(Succeed())
				Expect(a.IDs).To(Equal([]string{"abc"}))
				Expect(a.Delete).To(BeTrue())
				return "success", map[string]any{}
			})
			DeferCleanup(srv.Close)

			Expect(NewTransmission(srv.URL, "", "").
				RemoveTorrent(context.Background(), "abc", true)).To(Succeed())
		})
	})

	It("surfaces a non-success result as ErrUnexpectedStatus", func() {
		srv := trServer(func(_ string, _ json.RawMessage) (string, any) {
			return "duplicate torrent", nil
		})
		DeferCleanup(srv.Close)

		_, err := NewTransmission(srv.URL, "", "").
			GetTorrent(context.Background(), "x")
		Expect(errors.Is(err, ErrUnexpectedStatus)).To(BeTrue())
	})
})

var _ = Describe("mapTransmissionState", Label("unit", "downloads"), func() {
	DescribeTable("maps Transmission status to TorrentStatus",
		func(status int, pct float64, errStr string, want TorrentStatus) {
			Expect(mapTransmissionState(status, pct, errStr)).To(Equal(want))
		},
		Entry("error string wins", 4, 0.5, "tracker down", StatusError),
		Entry("downloading", 4, 0.5, "", StatusDownloading),
		Entry("verify wait", 1, 0.0, "", StatusDownloading),
		Entry("seeding", 6, 1.0, "", StatusSeeding),
		Entry("queued to seed", 5, 1.0, "", StatusSeeding),
		Entry("stopped + complete", 0, 1.0, "", StatusCompleted),
		Entry("stopped + partial", 0, 0.3, "", StatusPaused),
	)
})
