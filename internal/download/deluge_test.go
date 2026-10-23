package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
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

		It(
			"sends file_priorities as a full array, 4 (normal) for wanted"+
				" and 0 (skip) for the rest — not 1",
			func() {
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

				srv := delugeServer(
					func(method string, params []json.RawMessage) (any, string) {
						Expect(method).To(Equal("core.add_torrent_file"))
						Expect(params).To(HaveLen(3))
						var options map[string]any
						Expect(json.Unmarshal(params[2], &options)).To(Succeed())
						Expect(options["file_priorities"]).To(
							Equal([]any{4.0, 0.0, 0.0}),
						)
						return "abcdef", ""
					},
				)
				DeferCleanup(srv.Close)

				_, err = NewDeluge(srv.URL, "pw").AddTorrent(
					context.Background(),
					TorrentSource{
						Bytes:       torrent.Bytes(),
						WantedFiles: []int{0},
						Selective:   true,
					},
				)
				Expect(err).NotTo(HaveOccurred())
			},
		)
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

	Describe("ListFiles", func() {
		It(
			"maps Deluge's file listing, Wanted from file_priorities[i] != 0",
			func() {
				srv := delugeServer(
					func(method string, _ []json.RawMessage) (any, string) {
						Expect(method).To(Equal("core.get_torrent_status"))
						return map[string]any{
							"files": []any{
								map[string]any{
									"index": 0,
									"path":  "a.mkv",
									"size":  4,
								},
								map[string]any{
									"index": 1,
									"path":  "b.mkv",
									"size":  4,
								},
							},
							"file_priorities": []any{4, 0},
						}, ""
					},
				)
				DeferCleanup(srv.Close)

				files, err := NewDeluge(srv.URL, "pw").
					ListFiles(context.Background(), "abc")
				Expect(err).NotTo(HaveOccurred())
				Expect(files).To(HaveLen(2))
				Expect(files[0]).To(Equal(TorrentFile{
					Index: 0, Path: "a.mkv", Size: 4, Wanted: true,
				}))
				Expect(files[1]).To(Equal(TorrentFile{
					Index: 1, Path: "b.mkv", Size: 4, Wanted: false,
				}))
			},
		)

		It(
			"maps a pre-metadata empty files array to an empty, non-nil slice",
			func() {
				srv := delugeServer(func(string, []json.RawMessage) (any, string) {
					return map[string]any{
						"files": []any{}, "file_priorities": []any{},
					}, ""
				})
				DeferCleanup(srv.Close)

				files, err := NewDeluge(srv.URL, "pw").
					ListFiles(context.Background(), "abc")
				Expect(err).NotTo(HaveOccurred())
				Expect(files).NotTo(BeNil())
				Expect(files).To(BeEmpty())
			},
		)
	})

	Describe("SetWantedFiles", func() {
		It(
			"lists files first, then sets the full priorities array on the hash",
			func() {
				var setCalls int
				var gotHash []string
				var gotPriorities []int
				srv := delugeServer(
					func(method string, params []json.RawMessage) (any, string) {
						switch method {
						case "core.get_torrent_status":
							return map[string]any{
								"files": []any{
									map[string]any{
										"index": 0,
										"path":  "a.mkv",
										"size":  4,
									},
									map[string]any{
										"index": 1,
										"path":  "b.mkv",
										"size":  4,
									},
									map[string]any{
										"index": 2,
										"path":  "c.mkv",
										"size":  4,
									},
								},
								"file_priorities": []any{4, 4, 4},
							}, ""
						case "core.set_torrent_options":
							setCalls++
							Expect(json.Unmarshal(params[0], &gotHash)).To(Succeed())
							var options map[string]any
							Expect(json.Unmarshal(params[1], &options)).To(Succeed())
							raw, merr := json.Marshal(options["file_priorities"])
							Expect(merr).NotTo(HaveOccurred())
							Expect(json.Unmarshal(raw, &gotPriorities)).To(Succeed())
							return true, ""
						default:
							Fail("unexpected method " + method)
							return nil, ""
						}
					},
				)
				DeferCleanup(srv.Close)

				err := NewDeluge(srv.URL, "pw").
					SetWantedFiles(context.Background(), "abc", []int{0})
				Expect(err).NotTo(HaveOccurred())
				Expect(setCalls).To(Equal(1))
				Expect(gotHash).To(Equal([]string{"abc"}))
				Expect(gotPriorities).To(Equal([]int{4, 0, 0}))
			},
		)

		It(
			"makes no set_torrent_options call when ListFiles reports no"+
				" files yet — the torrent.py pre-metadata trap",
			func() {
				var setCalls int
				srv := delugeServer(
					func(method string, _ []json.RawMessage) (any, string) {
						switch method {
						case "core.get_torrent_status":
							return map[string]any{
								"files": []any{}, "file_priorities": []any{},
							}, ""
						case "core.set_torrent_options":
							setCalls++
							return true, ""
						default:
							Fail("unexpected method " + method)
							return nil, ""
						}
					},
				)
				DeferCleanup(srv.Close)

				err := NewDeluge(srv.URL, "pw").
					SetWantedFiles(context.Background(), "abc", []int{0})
				Expect(err).To(HaveOccurred())
				Expect(setCalls).To(Equal(0))
			},
		)

		It("rejects a skip-everything call before issuing any RPC", func() {
			called := false
			srv := delugeServer(func(string, []json.RawMessage) (any, string) {
				called = true
				return nil, ""
			})
			DeferCleanup(srv.Close)

			err := NewDeluge(srv.URL, "pw").
				SetWantedFiles(context.Background(), "abc", nil)
			Expect(err).To(HaveOccurred())
			Expect(called).To(BeFalse())
		})
	})

	Describe("FetchMagnetMetadata", func() {
		It(
			"decodes the base64 info dict and returns loadable metainfo bytes",
			func() {
				info, err := bencode.Marshal(metainfo.Info{
					Name: "Show",
					Files: []metainfo.FileInfo{
						{Path: []string{"a.mkv"}, Length: 4},
						{Path: []string{"b.mkv"}, Length: 4},
					},
					PieceLength: 32768,
					Pieces:      make([]byte, 20),
				})
				Expect(err).NotTo(HaveOccurred())
				b64 := base64.StdEncoding.EncodeToString(info)

				srv := delugeServer(
					func(method string, params []json.RawMessage) (any, string) {
						Expect(method).To(Equal("core.prefetch_magnet_metadata"))
						Expect(params).To(HaveLen(2))
						var magnet string
						Expect(json.Unmarshal(params[0], &magnet)).To(Succeed())
						Expect(magnet).To(Equal("magnet:?xt=urn:btih:deadbeef"))
						var timeout int
						Expect(json.Unmarshal(params[1], &timeout)).To(Succeed())
						Expect(timeout).To(Equal(30))
						return []any{"session-id", b64}, ""
					},
				)
				DeferCleanup(srv.Close)

				raw, err := NewDeluge(srv.URL, "pw").FetchMagnetMetadata(
					context.Background(), "magnet:?xt=urn:btih:deadbeef",
				)
				Expect(err).NotTo(HaveOccurred())

				mi, lerr := metainfo.Load(bytes.NewReader(raw))
				Expect(lerr).NotTo(HaveOccurred())
				got, uerr := mi.UnmarshalInfo()
				Expect(uerr).NotTo(HaveOccurred())
				Expect(got.Name).To(Equal("Show"))
				Expect(got.Files).To(HaveLen(2))
			},
		)

		It("propagates an RPC error", func() {
			srv := delugeServer(func(string, []json.RawMessage) (any, string) {
				return nil, "timed out"
			})
			DeferCleanup(srv.Close)

			_, err := NewDeluge(srv.URL, "pw").FetchMagnetMetadata(
				context.Background(), "magnet:?xt=urn:btih:deadbeef",
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("timed out"))
		})

		DescribeTable(
			"errors instead of succeeding on a malformed result — json.Unmarshal"+
				" leaves a missing array element zeroed rather than failing",
			func(result any) {
				srv := delugeServer(func(string, []json.RawMessage) (any, string) {
					return result, ""
				})
				DeferCleanup(srv.Close)

				_, err := NewDeluge(srv.URL, "pw").FetchMagnetMetadata(
					context.Background(), "magnet:?xt=urn:btih:deadbeef",
				)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(ErrBadResponse))
			},
			Entry("null result", nil),
			Entry("one-element array (no b64)", []any{"session-id"}),
			Entry("empty-string b64 element", []any{"session-id", ""}),
		)
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
