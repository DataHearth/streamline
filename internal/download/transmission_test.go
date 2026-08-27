package download

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
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

		It(
			"sends files-unwanted as the complement of WantedFiles and"+
				" omits files-wanted",
			func() {
				var gotArgs json.RawMessage
				srv := trServer(
					func(method string, args json.RawMessage) (string, any) {
						if method == "torrent-add" {
							gotArgs = args
						}
						return "success", map[string]any{
							"torrent-added": map[string]any{"hashString": "abc"},
						}
					},
				)
				DeferCleanup(srv.Close)

				info, err := bencode.Marshal(metainfo.Info{
					Files: []metainfo.FileInfo{
						{Path: []string{"a.mkv"}, Length: 4},
						{Path: []string{"b.mkv"}, Length: 4},
						{Path: []string{"c.mkv"}, Length: 4},
					},
					PieceLength: 32768,
					Pieces:      make([]byte, 20),
				})
				Expect(err).NotTo(HaveOccurred())
				mi := metainfo.MetaInfo{InfoBytes: info}
				var torrent bytes.Buffer
				Expect(mi.Write(&torrent)).To(Succeed())

				_, err = NewTransmission(srv.URL, "", "").AddTorrent(
					context.Background(),
					TorrentSource{
						Bytes:       torrent.Bytes(),
						WantedFiles: []int{0},
						Selective:   true,
					},
				)
				Expect(err).NotTo(HaveOccurred())

				var a map[string]any
				Expect(json.Unmarshal(gotArgs, &a)).To(Succeed())
				Expect(a).NotTo(HaveKey("files-wanted"))
				Expect(a["files-unwanted"]).To(ConsistOf(float64(1), float64(2)))
			},
		)
	})

	Describe("ListFiles", func() {
		It("maps files and fileStats, with the wanted flag from fileStats",
			func() {
				srv := trServer(
					func(method string, _ json.RawMessage) (string, any) {
						Expect(method).To(Equal("torrent-get"))
						return "success", map[string]any{
							"torrents": []map[string]any{{
								"files": []map[string]any{
									{"name": "a.mkv", "length": int64(100)},
									{"name": "b.mkv", "length": int64(200)},
								},
								"fileStats": []map[string]any{
									{
										"bytesCompleted": int64(100),
										"wanted":         true,
										"priority":       0,
									},
									{
										"bytesCompleted": int64(0),
										"wanted":         false,
										"priority":       0,
									},
								},
							}},
						}
					},
				)
				DeferCleanup(srv.Close)

				files, err := NewTransmission(srv.URL, "", "").
					ListFiles(context.Background(), "abc")
				Expect(err).NotTo(HaveOccurred())
				Expect(files).To(Equal([]TorrentFile{
					{Index: 0, Path: "a.mkv", Size: 100, Wanted: true},
					{Index: 1, Path: "b.mkv", Size: 200, Wanted: false},
				}))
			},
		)

		It("returns an empty slice for a magnet with unresolved metadata",
			func() {
				srv := trServer(
					func(method string, _ json.RawMessage) (string, any) {
						Expect(method).To(Equal("torrent-get"))
						return "success", map[string]any{
							"torrents": []map[string]any{{
								"files":     []any{},
								"fileStats": []any{},
							}},
						}
					},
				)
				DeferCleanup(srv.Close)

				files, err := NewTransmission(srv.URL, "", "").
					ListFiles(context.Background(), "abc")
				Expect(err).NotTo(HaveOccurred())
				Expect(files).To(Equal([]TorrentFile{}))
			},
		)
	})

	Describe("SetWantedFiles", func() {
		It("sends files-wanted without files-unwanted when every file is wanted",
			func() {
				var gotArgs json.RawMessage
				srv := trServer(
					func(method string, args json.RawMessage) (string, any) {
						switch method {
						case "torrent-get":
							return "success", map[string]any{
								"torrents": []map[string]any{{
									"files": []map[string]any{
										{"name": "a.mkv", "length": int64(4)},
										{"name": "b.mkv", "length": int64(4)},
										{"name": "c.mkv", "length": int64(4)},
									},
									"fileStats": []map[string]any{
										{
											"bytesCompleted": int64(0),
											"wanted":         true,
											"priority":       0,
										},
										{
											"bytesCompleted": int64(0),
											"wanted":         true,
											"priority":       0,
										},
										{
											"bytesCompleted": int64(0),
											"wanted":         true,
											"priority":       0,
										},
									},
								}},
							}
						case "torrent-set":
							gotArgs = args
							return "success", map[string]any{}
						default:
							Fail("unexpected method " + method)
							return "success", map[string]any{}
						}
					},
				)
				DeferCleanup(srv.Close)

				err := NewTransmission(srv.URL, "", "").SetWantedFiles(
					context.Background(), "abc", []int{0, 1, 2},
				)
				Expect(err).NotTo(HaveOccurred())

				var a map[string]any
				Expect(json.Unmarshal(gotArgs, &a)).To(Succeed())
				Expect(a).NotTo(HaveKey("files-unwanted"))
				Expect(a["files-wanted"]).To(
					ConsistOf(float64(0), float64(1), float64(2)),
				)
			},
		)

		It("rejects a skip-everything call before issuing any RPC", func() {
			called := false
			srv := trServer(func(_ string, _ json.RawMessage) (string, any) {
				called = true
				return "success", map[string]any{}
			})
			DeferCleanup(srv.Close)

			err := NewTransmission(srv.URL, "", "").
				SetWantedFiles(context.Background(), "abc", nil)
			Expect(err).To(HaveOccurred())
			Expect(called).To(BeFalse())
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
