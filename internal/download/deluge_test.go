package download

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// delugeServer handles the session bootstrap (login → cookie, the not-connected
// path → get_hosts → connect) itself and delegates core.* calls to fn. fn
// returns (result, errMsg); a non-empty errMsg becomes a JSON-RPC error object.
func delugeServer(
	fn func(method string, params []json.RawMessage) (any, string),
) *httptest.Server {
	GinkgoHelper()
	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			Expect(json.NewDecoder(r.Body).Decode(&req)).To(Succeed())

			var (
				result any
				errMsg string
			)
			switch req.Method {
			case "auth.login":
				http.SetCookie(w, &http.Cookie{Name: "_session_id", Value: "s"})
				result = true
			case "web.connected":
				result = false
			case "web.get_hosts":
				result = []any{[]any{"host-1", "127.0.0.1", 58846, "Online"}}
			case "web.connect":
				result = nil
			default:
				result, errMsg = fn(req.Method, req.Params)
			}

			body := map[string]any{"id": 1, "result": result, "error": nil}
			if errMsg != "" {
				body["result"] = nil
				body["error"] = map[string]any{"message": errMsg, "code": 1}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(jsonBytes(body))
		},
	))
}

var _ = Describe("Deluge Client", Label("unit", "downloads"), func() {
	Describe("TestConnection", func() {
		It("logs in and attaches to the daemon via get_hosts/connect", func() {
			srv := delugeServer(func(string, []json.RawMessage) (any, string) {
				return nil, ""
			})
			DeferCleanup(srv.Close)

			Expect(NewDeluge(srv.URL, "pw").
				TestConnection(context.Background())).To(Succeed())
		})

		It("maps a rejected password to ErrUnauthorized", func() {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes(map[string]any{
						"id": 1, "result": false, "error": nil,
					}))
				}))
			DeferCleanup(srv.Close)

			err := NewDeluge(srv.URL, "bad").TestConnection(context.Background())
			Expect(err).To(MatchError(ErrUnauthorized))
		})
	})

	Describe("AddTorrent", func() {
		It("sends a base64 dump and returns the lowercased hash", func() {
			srv := delugeServer(
				func(method string, params []json.RawMessage) (any, string) {
					Expect(method).To(Equal("core.add_torrent_file"))
					Expect(params).To(HaveLen(3))
					var dump string
					Expect(json.Unmarshal(params[1], &dump)).To(Succeed())
					Expect(dump).NotTo(BeEmpty())
					return "ABCDEF", ""
				},
			)
			DeferCleanup(srv.Close)

			hash, err := NewDeluge(srv.URL, "pw").AddTorrent(
				context.Background(), TorrentSource{Bytes: []byte("d8:announce")})
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal("abcdef"))
		})

		It("uses add_torrent_magnet for a magnet source", func() {
			srv := delugeServer(
				func(method string, _ []json.RawMessage) (any, string) {
					Expect(method).To(Equal("core.add_torrent_magnet"))
					return "deadbeef", ""
				},
			)
			DeferCleanup(srv.Close)

			hash, err := NewDeluge(srv.URL, "pw").AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:deadbeef"})
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal("deadbeef"))
		})

		It("maps an 'already in session' error to ErrTorrentAlreadyExists", func() {
			srv := delugeServer(func(string, []json.RawMessage) (any, string) {
				return nil, "Torrent already in session (deadbeef)."
			})
			DeferCleanup(srv.Close)

			_, err := NewDeluge(srv.URL, "pw").AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:deadbeef"})
			Expect(err).To(MatchError(ErrTorrentAlreadyExists))
		})
	})

	Describe("GetTorrent", func() {
		It("scales progress to 0-1 and maps the state", func() {
			srv := delugeServer(
				func(method string, _ []json.RawMessage) (any, string) {
					Expect(method).To(Equal("core.get_torrent_status"))
					return map[string]any{
						"name":                  "Dune.2021",
						"progress":              42.0,
						"total_size":            int64(8000000000),
						"save_path":             "/downloads",
						"download_payload_rate": 1048576.0,
						"eta":                   120.0,
						"state":                 "Downloading",
						"is_finished":           false,
					}, ""
				},
			)
			DeferCleanup(srv.Close)

			t, err := NewDeluge(srv.URL, "pw").
				GetTorrent(context.Background(), "abc")
			Expect(err).NotTo(HaveOccurred())
			Expect(t.Progress).To(BeNumerically("~", 0.42, 0.001))
			Expect(t.Status).To(Equal(StatusDownloading))
			Expect(t.DownloadSpeed).To(Equal(int64(1048576)))
			Expect(t.ETA).To(Equal(int64(120)))
		})

		It("returns ErrTorrentNotFound for an empty status object", func() {
			srv := delugeServer(func(string, []json.RawMessage) (any, string) {
				return map[string]any{}, ""
			})
			DeferCleanup(srv.Close)

			_, err := NewDeluge(srv.URL, "pw").
				GetTorrent(context.Background(), "missing")
			Expect(err).To(MatchError(ErrTorrentNotFound))
		})
	})

	Describe("RemoveTorrent", func() {
		It("passes the hash and remove-data flag", func() {
			srv := delugeServer(
				func(method string, params []json.RawMessage) (any, string) {
					Expect(method).To(Equal("core.remove_torrent"))
					var hash string
					var del bool
					Expect(json.Unmarshal(params[0], &hash)).To(Succeed())
					Expect(json.Unmarshal(params[1], &del)).To(Succeed())
					Expect(hash).To(Equal("abc"))
					Expect(del).To(BeTrue())
					return true, ""
				},
			)
			DeferCleanup(srv.Close)

			Expect(NewDeluge(srv.URL, "pw").
				RemoveTorrent(context.Background(), "abc", true)).To(Succeed())
		})
	})
})

var _ = Describe("mapDelugeState", Label("unit", "downloads"), func() {
	DescribeTable("maps Deluge state to TorrentStatus",
		func(state string, finished bool, want TorrentStatus) {
			Expect(mapDelugeState(state, finished)).To(Equal(want))
		},
		Entry("downloading", "Downloading", false, StatusDownloading),
		Entry("checking", "Checking", false, StatusDownloading),
		Entry("queued", "Queued", false, StatusDownloading),
		Entry("seeding", "Seeding", true, StatusSeeding),
		Entry("paused mid-download", "Paused", false, StatusPaused),
		Entry("paused + finished (seeding off)", "Paused", true, StatusCompleted),
		Entry("error", "Error", false, StatusError),
	)
})
