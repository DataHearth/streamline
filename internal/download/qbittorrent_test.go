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

func jsonBytes(v any) []byte {
	GinkgoHelper()
	b, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return b
}

var _ = Describe("qBittorrent Client", Label("unit", "downloads"), func() {
	var (
		ts     *httptest.Server
		client Client
	)

	BeforeEach(func() {
		mux := http.NewServeMux()
		mux.HandleFunc(
			"/api/v2/auth/login",
			func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-session"})
				w.WriteHeader(http.StatusOK)
			},
		)
		mux.HandleFunc(
			"/api/v2/torrents/info",
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(jsonBytes([]map[string]any{
					{
						"hash":      "abc123",
						"name":      "Interstellar.2014.1080p.BluRay",
						"state":     "uploading",
						"progress":  1.0,
						"size":      5368709120,
						"save_path": "/downloads/",
					},
				}))
			},
		)
		mux.HandleFunc(
			"/api/v2/torrents/add",
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		)
		mux.HandleFunc(
			"/api/v2/torrents/createCategory",
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		)
		mux.HandleFunc(
			"/api/v2/torrents/delete",
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		)
		ts = httptest.NewServer(mux)
		DeferCleanup(func() { ts.Close() })
		client = NewQBittorrentPassword(ts.URL, "admin", "password")
	})

	Describe("ListTorrents", func() {
		It("should authenticate and return torrents", func() {
			torrents, err := client.ListTorrents(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(torrents).To(HaveLen(1))
			Expect(torrents[0].Hash).To(Equal("abc123"))
			Expect(torrents[0].Status).To(Equal(StatusSeeding))
			Expect(torrents[0].Progress).To(Equal(1.0))
		})

		It("maps dlspeed/eta and normalizes the ∞ ETA sentinel to 0", func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/info",
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes([]map[string]any{
						{
							"hash": "active", "name": "Dune", "state": "downloading",
							"progress": 0.42, "size": 8000000000,
							"dlspeed": 1048576, "eta": 120,
						},
						{
							"hash": "idle", "name": "Tenet", "state": "stalledDL",
							"progress": 0.0, "size": 4000000000,
							"dlspeed": 0, "eta": 8640000,
						},
					}))
				})
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			torrents, err := c.ListTorrents(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(torrents).To(HaveLen(2))
			Expect(torrents[0].DownloadSpeed).To(Equal(int64(1048576)))
			Expect(torrents[0].ETA).To(Equal(int64(120)))
			Expect(torrents[1].DownloadSpeed).To(Equal(int64(0)))
			Expect(torrents[1].ETA).To(Equal(int64(0)))
		})
	})

	Describe("AddTorrent", func() {
		It("ensures the category then sends category=streamline on add", func() {
			var (
				createCalled bool
				addCategory  string
			)
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/createCategory",
				func(w http.ResponseWriter, r *http.Request) {
					createCalled = true
					Expect(r.ParseForm()).To(Succeed())
					Expect(r.PostFormValue("category")).To(Equal("streamline"))
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/add",
				func(w http.ResponseWriter, r *http.Request) {
					addCategory = r.FormValue("category")
					w.WriteHeader(http.StatusOK)
				})
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, err := c.AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:abc123"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(createCalled).To(BeTrue())
			Expect(addCategory).To(Equal("streamline"))
		})

		It("treats a 409 from createCategory as success", func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/createCategory",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusConflict)
				})
			mux.HandleFunc("/api/v2/torrents/add",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, err := c.AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:abc123"},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("fails the add when createCategory hard-errors", func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/createCategory",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				})
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, err := c.AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:abc123"},
			)
			Expect(err).To(HaveOccurred())
		})

		It("extracts btih from a magnet when qBittorrent gives no envelope", func() {
			hash, err := client.AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:abc123"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal("abc123"))
		})

		It("returns the hash from the JSON envelope on multipart upload", func() {
			var receivedCT string
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/createCategory",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/add",
				func(w http.ResponseWriter, r *http.Request) {
					receivedCT = r.Header.Get("Content-Type")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(
						`{"added_torrent_ids":["BB4C35E3081CCA7B36D4CDCB425C578980B3F4DB"],` +
							`"success_count":1,"failure_count":0,"pending_count":0}`,
					))
				})
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			hash, err := c.AddTorrent(
				context.Background(),
				TorrentSource{Bytes: []byte("d8:announce0:e")},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal("bb4c35e3081cca7b36d4cdcb425c578980b3f4db"))
			Expect(receivedCT).To(HavePrefix("multipart/form-data"))
		})

		It("errors when bytes are sent and qBittorrent returns no hash", func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/createCategory",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/add",
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(
						`{"added_torrent_ids":[],"success_count":0,` +
							`"failure_count":0,"pending_count":1}`,
					))
				})
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, err := c.AddTorrent(
				context.Background(),
				TorrentSource{Bytes: []byte("d8:announce0:e")},
			)
			Expect(err).To(MatchError(ContainSubstring("no hash returned")))
		})

		It("rejects an empty TorrentSource", func() {
			_, err := client.AddTorrent(
				context.Background(), TorrentSource{},
			)
			Expect(err).To(MatchError(ContainSubstring("empty torrent source")))
		})
	})

	Describe("RemoveTorrent", func() {
		It("should delete the torrent", func() {
			err := client.RemoveTorrent(context.Background(), "abc123", false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("TestConnection", func() {
		It("should succeed when login works", func() {
			err := client.TestConnection(context.Background())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("GetTorrent", func() {
		It("returns the torrent matching the requested hash", func() {
			torrent, err := client.GetTorrent(context.Background(), "abc123")
			Expect(err).NotTo(HaveOccurred())
			Expect(torrent).NotTo(BeNil())
			Expect(torrent.Hash).To(Equal("abc123"))
			Expect(torrent.Name).To(Equal("Interstellar.2014.1080p.BluRay"))
			Expect(torrent.Status).To(Equal(StatusSeeding))
		})

		It("reauthenticates when qBittorrent returns 403", func() {
			var loginCalls, listCalls int
			mux := http.NewServeMux()
			mux.HandleFunc(
				"/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					loginCalls++
					http.SetCookie(
						w, &http.Cookie{Name: "SID", Value: "session"},
					)
					w.WriteHeader(http.StatusOK)
				},
			)
			mux.HandleFunc(
				"/api/v2/torrents/info",
				func(w http.ResponseWriter, r *http.Request) {
					listCalls++
					// First call after initial login fails with 403 — forces
					// the reauth branch in doRequest.
					if listCalls == 1 {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]"))
				},
			)
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			torrents, err := c.ListTorrents(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(torrents).To(BeEmpty())
			Expect(loginCalls).To(Equal(2))
			Expect(listCalls).To(Equal(2))
		})

		It("AddTorrent surfaces 415 as an invalid-torrent error", func() {
			mux := http.NewServeMux()
			mux.HandleFunc(
				"/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w,
						&http.Cookie{Name: "SID", Value: "session"})
					w.WriteHeader(http.StatusOK)
				},
			)
			mux.HandleFunc(
				"/api/v2/torrents/createCategory",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				},
			)
			mux.HandleFunc(
				"/api/v2/torrents/add",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnsupportedMediaType)
				},
			)
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, err := c.AddTorrent(
				context.Background(),
				TorrentSource{Bytes: []byte("not a torrent")},
			)
			Expect(err).To(MatchError(ContainSubstring("invalid torrent")))
		})

		It("AddTorrent wraps unexpected non-200 status codes", func() {
			mux := http.NewServeMux()
			mux.HandleFunc(
				"/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w,
						&http.Cookie{Name: "SID", Value: "session"})
					w.WriteHeader(http.StatusOK)
				},
			)
			mux.HandleFunc(
				"/api/v2/torrents/createCategory",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				},
			)
			mux.HandleFunc(
				"/api/v2/torrents/add",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				},
			)
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, err := c.AddTorrent(
				context.Background(),
				TorrentSource{Magnet: "magnet:?xt=urn:btih:abc"},
			)
			Expect(err).To(MatchError(ContainSubstring("unexpected status 500")))
		})

		It("accepts qBittorrent 5.x login: 204 + QBT_SID_<port> cookie", func() {
			var sentSession string
			mux := http.NewServeMux()
			mux.HandleFunc(
				"/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{
						Name:  "QBT_SID_8090",
						Value: "v5session",
					})
					w.WriteHeader(http.StatusNoContent)
				},
			)
			mux.HandleFunc(
				"/api/v2/torrents/info",
				func(w http.ResponseWriter, r *http.Request) {
					if c, err := r.Cookie("QBT_SID_8090"); err == nil {
						sentSession = c.Value
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]"))
				},
			)
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, err := c.ListTorrents(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(sentSession).To(Equal("v5session"))
		})

		It("returns error for unknown hash", func() {
			By("Starting mock that returns an empty list for any hashes filter")
			emptyMux := http.NewServeMux()
			emptyMux.HandleFunc(
				"/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test"})
					w.WriteHeader(http.StatusOK)
				},
			)
			emptyMux.HandleFunc(
				"/api/v2/torrents/info",
				func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]"))
				},
			)
			emptySrv := httptest.NewServer(emptyMux)
			DeferCleanup(emptySrv.Close)

			c := NewQBittorrentPassword(emptySrv.URL, "admin", "password")

			torrent, err := c.GetTorrent(context.Background(), "nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(torrent).To(BeNil())
		})

		It("filters info queries by category=streamline", func() {
			var gotCategory string
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/info",
				func(w http.ResponseWriter, r *http.Request) {
					gotCategory = r.URL.Query().Get("category")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]"))
				})
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, _ = c.ListTorrents(context.Background())
			Expect(gotCategory).To(Equal("streamline"))
		})

		It("returns ErrTorrentNotFound when the filtered list is empty", func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/info",
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]"))
				})
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentPassword(srv.URL, "admin", "password")
			_, err := c.GetTorrent(context.Background(), "missing")
			Expect(errors.Is(err, ErrTorrentNotFound)).To(BeTrue())
		})
	})

	Describe("API key mode", func() {
		It("TestConnection probes /api/v2/app/version with Bearer", func() {
			var gotAuth string
			mux := http.NewServeMux()
			mux.HandleFunc(
				"/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					Fail("login must not be called in API key mode")
				},
			)
			mux.HandleFunc(
				"/api/v2/app/version",
				func(w http.ResponseWriter, r *http.Request) {
					gotAuth = r.Header.Get("Authorization")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("v5.2.0"))
				},
			)
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentAPIKey(srv.URL, "qbt_testkey")
			Expect(c.TestConnection(context.Background())).To(Succeed())
			Expect(gotAuth).To(Equal("Bearer qbt_testkey"))
		})

		It("ListTorrents sends Bearer and skips login", func() {
			var gotAuth string
			mux := http.NewServeMux()
			mux.HandleFunc(
				"/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					Fail("login must not be called in API key mode")
				},
			)
			mux.HandleFunc(
				"/api/v2/torrents/info",
				func(w http.ResponseWriter, r *http.Request) {
					gotAuth = r.Header.Get("Authorization")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]"))
				},
			)
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentAPIKey(srv.URL, "qbt_testkey")
			torrents, err := c.ListTorrents(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(torrents).To(BeEmpty())
			Expect(gotAuth).To(Equal("Bearer qbt_testkey"))
		})

		It("TestConnection surfaces 401 as ErrUnauthorized", func() {
			mux := http.NewServeMux()
			mux.HandleFunc(
				"/api/v2/app/version",
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				},
			)
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)

			c := NewQBittorrentAPIKey(srv.URL, "qbt_rotated")
			err := c.TestConnection(context.Background())
			Expect(err).To(MatchError(ErrUnauthorized))
		})
	})
})

var _ = Describe("qBittorrent pause/resume", Label("unit", "downloads"), func() {
	It("maps qB 5.x stopped states to paused", func() {
		Expect(mapQBState("stoppedDL")).To(Equal(StatusPaused))
		Expect(mapQBState("stoppedUP")).To(Equal(StatusPaused))
	})

	It("PauseTorrent uses qB 5.x /stop and falls back to 4.x /pause on 404", func() {
		var stopCalls, pauseCalls int
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v2/auth/login",
			func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
				w.WriteHeader(http.StatusOK)
			})
		mux.HandleFunc("/api/v2/torrents/stop",
			func(w http.ResponseWriter, _ *http.Request) {
				stopCalls++
				w.WriteHeader(http.StatusNotFound)
			})
		mux.HandleFunc("/api/v2/torrents/pause",
			func(w http.ResponseWriter, _ *http.Request) {
				pauseCalls++
				w.WriteHeader(http.StatusOK)
			})
		srv := httptest.NewServer(mux)
		DeferCleanup(srv.Close)

		c := NewQBittorrentPassword(srv.URL, "admin", "password")
		Expect(c.PauseTorrent(context.Background(), "abc")).To(Succeed())
		Expect(stopCalls).To(Equal(1))
		Expect(pauseCalls).To(Equal(1))
	})

	It("ResumeTorrent uses qB 5.x /start when available", func() {
		var startCalls int
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v2/auth/login",
			func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
				w.WriteHeader(http.StatusOK)
			})
		mux.HandleFunc("/api/v2/torrents/start",
			func(w http.ResponseWriter, _ *http.Request) {
				startCalls++
				w.WriteHeader(http.StatusOK)
			})
		srv := httptest.NewServer(mux)
		DeferCleanup(srv.Close)

		c := NewQBittorrentPassword(srv.URL, "admin", "password")
		Expect(c.ResumeTorrent(context.Background(), "abc")).To(Succeed())
		Expect(startCalls).To(Equal(1))
	})
})
