package download

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func splitHostPort(rawURL string) (string, uint16) {
	GinkgoHelper()
	u, err := url.Parse(rawURL)
	Expect(err).NotTo(HaveOccurred())
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	Expect(err).NotTo(HaveOccurred())
	return u.Hostname(), uint16(port)
}

// qbitClientConfig sets up the config singleton with a single enabled
// password-auth qBittorrent client named name, pointed at host:port.
func qbitClientConfig(name, host string, port uint16) {
	GinkgoHelper()
	configtest.Setup(map[string]any{
		"download_clients": []map[string]any{{
			"name": name, "client_type": "qbittorrent",
			"host": host, "port": int(port), "auth_method": "password",
			"username": "admin", "password": "admin",
			"priority": 10, "enabled": true,
		}},
	})
}

var _ = Describe("Manager.Grab", Label("integration", "downloads"), func() {
	var (
		ctx      context.Context
		ts       *httptest.Server
		dbClient *ent.Client
		mgr      Downloader
		m        *ent.Movie
	)

	BeforeEach(func() {
		ctx = context.Background()

		mux := http.NewServeMux()
		mux.HandleFunc(
			"/api/v2/auth/login",
			func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-session"})
				w.WriteHeader(http.StatusOK)
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
		ts = httptest.NewServer(mux)
		DeferCleanup(ts.Close)

		var err error
		dbClient, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })

		host, port := splitHostPort(ts.URL)
		qbitClientConfig("qbit-test", host, port)

		m, err = dbClient.Movie.Create().
			SetTitle("Fight Club").
			SetOriginalTitle("Fight Club").SetYear(1999).SetTmdbID(550).
			SetStatus(movie.StatusWanted).Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		mgr = New(db.New(dbClient))
	})

	It("returns add_torrent_failed when qBittorrent rejects the magnet", func() {
		// Swap in a ts that returns 500 on add.
		badMux := http.NewServeMux()
		badMux.HandleFunc(
			"/api/v2/auth/login",
			func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "x"})
				w.WriteHeader(http.StatusOK)
			},
		)
		badMux.HandleFunc(
			"/api/v2/torrents/add",
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		)
		badMux.HandleFunc(
			"/api/v2/torrents/createCategory",
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		)
		badSrv := httptest.NewServer(badMux)
		DeferCleanup(badSrv.Close)

		badHost, badPort := splitHostPort(badSrv.URL)
		qbitClientConfig("qbit-test", badHost, badPort)

		_, err := mgr.Grab(ctx, indexer.SearchResult{
			Title:    "x",
			Download: "magnet:?xt=urn:btih:abc",
		}, m.ID)
		Expect(err).To(MatchError(ContainSubstring("add torrent")))
	})

	It("creates a DownloadRecord and flips Movie.Status to downloading", func() {
		result := indexer.SearchResult{
			Title:    "Fight.Club.1999.1080p.BluRay.x264-GROUP",
			Download: "magnet:?xt=urn:btih:abc123&dn=fightclub",
			Size:     1000,
			Seeders:  50,
			Indexer:  "my-tracker",
		}

		record, err := mgr.Grab(ctx, result, m.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(record).NotTo(BeNil())
		Expect(record.TorrentHash).To(Equal("abc123"))
		Expect(record.IndexerName).To(Equal("my-tracker"))
		Expect(record.DownloadClientName).To(Equal("qbit-test"))

		refreshed, err := dbClient.Movie.Get(ctx, m.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(refreshed.Status).To(Equal(movie.StatusDownloading))
	})
})

var _ = Describe("Manager.CheckStatus", Label("integration", "downloads"), func() {
	var (
		ctx      context.Context
		ts       *httptest.Server
		dbClient *ent.Client
		mgr      Downloader
	)

	BeforeEach(func() {
		ctx = context.Background()

		By("Setting up mock qBittorrent reporting torrent as uploading (completed)")
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
			func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(jsonBytes([]map[string]any{
					{
						"hash":      "abc123",
						"name":      "Fight.Club.1999.1080p",
						"state":     "uploading",
						"progress":  1.0,
						"size":      5000000000,
						"save_path": "/downloads/",
					},
				}))
			},
		)
		ts = httptest.NewServer(mux)
		DeferCleanup(ts.Close)

		By("Setting up in-memory DB + config download client + active record")
		var err error
		dbClient, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })

		host, port := splitHostPort(ts.URL)
		qbitClientConfig("qbit-test", host, port)

		m, err := dbClient.Movie.Create().
			SetTitle("Fight Club").
			SetOriginalTitle("Fight Club").SetYear(1999).SetTmdbID(550).
			SetStatus(movie.StatusDownloading).Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		_, err = dbClient.DownloadRecord.Create().
			SetTitle("Fight.Club.1999.1080p").
			SetTorrentHash("abc123").
			SetStatus(downloadrecord.StatusDownloading).
			SetMovieID(m.ID).
			SetDownloadClientName("qbit-test").
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		mgr = New(db.New(dbClient))
	})

	It("returns completed downloads and flips record status to importing", func() {
		completed, err := mgr.CheckStatus(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(completed).To(HaveLen(1))
		Expect(completed[0].Record.TorrentHash).To(Equal("abc123"))
		Expect(completed[0].SavePath).To(Equal(
			filepath.Join(
				config.Get().Library.DownloadPath,
				"Fight.Club.1999.1080p",
			),
		))

		By("Verifying record status flipped to importing in DB")
		rec, err := dbClient.DownloadRecord.Get(ctx, completed[0].Record.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Status).To(Equal(downloadrecord.StatusImporting))
	})
})

var _ = Describe("Manager.CheckStatus orphan reconciliation",
	Label("integration", "downloads"), func() {
		// orphanSetup wires a qB stub that reports no torrents (so GetTorrent
		// returns NotFound) and a single downloading movie record of the given
		// age. Returns the db client, manager, record id and movie id.
		orphanSetup := func(age time.Duration) (*ent.Client, Downloader, uint32, uint32) {
			ctx := context.Background()
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/info",
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]")) // torrent gone => NotFound
				})
			ts := httptest.NewServer(mux)
			DeferCleanup(ts.Close)

			dbClient, err := db.Open(ctx, ":memory:")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { dbClient.Close() })

			host, port := splitHostPort(ts.URL)
			qbitClientConfig("qbit-test", host, port)

			m, err := dbClient.Movie.Create().
				SetTitle("Fight Club").SetOriginalTitle("Fight Club").
				SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusDownloading).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			rec, err := dbClient.DownloadRecord.Create().
				SetTitle("Fight.Club.1999.1080p").SetTorrentHash("abc123").
				SetStatus(downloadrecord.StatusDownloading).
				SetMovieID(m.ID).SetDownloadClientName("qbit-test").
				SetCreateTime(time.Now().Add(-age)).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			return dbClient, New(db.New(dbClient)), rec.ID, m.ID
		}

		It(
			"purges an aged record whose torrent is gone and reverts the movie",
			func() {
				ctx := context.Background()
				dbClient, mgr, recID, movieID := orphanSetup(10 * time.Minute)

				completed, err := mgr.CheckStatus(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(BeEmpty())

				_, err = dbClient.DownloadRecord.Get(ctx, recID)
				Expect(ent.IsNotFound(err)).To(BeTrue())

				gotM, err := dbClient.Movie.Get(ctx, movieID)
				Expect(err).NotTo(HaveOccurred())
				Expect(gotM.Status).To(Equal(movie.StatusWanted))
			},
		)

		It("spares a fresh record within the monitor grace", func() {
			ctx := context.Background()
			dbClient, mgr, recID, movieID := orphanSetup(0)

			_, err := mgr.CheckStatus(ctx)
			Expect(err).NotTo(HaveOccurred())

			got, err := dbClient.DownloadRecord.Get(ctx, recID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(downloadrecord.StatusDownloading))

			gotM, err := dbClient.Movie.Get(ctx, movieID)
			Expect(err).NotTo(HaveOccurred())
			Expect(gotM.Status).To(Equal(movie.StatusDownloading))
		})
	})

var _ = Describe("Manager.RemoveTorrent", Label("integration", "downloads"), func() {
	It("delegates to the download client with deleteFiles=false", func() {
		ctx := context.Background()

		var (
			gotHashes      string
			gotDeleteFiles string
		)
		mux := http.NewServeMux()
		mux.HandleFunc(
			"/api/v2/auth/login",
			func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "sess"})
				w.WriteHeader(http.StatusOK)
			},
		)
		mux.HandleFunc(
			"/api/v2/torrents/delete",
			func(w http.ResponseWriter, r *http.Request) {
				Expect(r.ParseForm()).To(Succeed())
				gotHashes = r.Form.Get("hashes")
				gotDeleteFiles = r.Form.Get("deleteFiles")
				w.WriteHeader(http.StatusOK)
			},
		)
		ts := httptest.NewServer(mux)
		DeferCleanup(ts.Close)

		dbClient, err := db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })

		host, port := splitHostPort(ts.URL)
		qbitClientConfig("qbit-rm", host, port)

		mgr := New(db.New(dbClient))
		Expect(mgr.RemoveTorrent(ctx, "qbit-rm", "abc123")).To(Succeed())
		Expect(gotHashes).To(Equal("abc123"))
		Expect(gotDeleteFiles).To(Equal("false"))
	})
})

var _ = Describe("Manager.CheckStatus logging",
	Label("integration", "downloads"), func() {
		var (
			ctx      context.Context
			dbClient *ent.Client
			mgr      Downloader
		)

		BeforeEach(func() {
			ctx = context.Background()

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
			ts := httptest.NewServer(mux)
			DeferCleanup(ts.Close)

			var err error
			dbClient, err = db.Open(ctx, ":memory:")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { dbClient.Close() })

			host, port := splitHostPort(ts.URL)
			qbitClientConfig("qbit-test", host, port)

			m, err := dbClient.Movie.Create().
				SetTitle("Fight Club").
				SetOriginalTitle("Fight Club").SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusDownloading).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			_, err = dbClient.DownloadRecord.Create().
				SetTitle("Fight.Club.1999.1080p").
				SetTorrentHash("abc123").
				SetStatus(downloadrecord.StatusDownloading).
				SetMovieID(m.ID).SetDownloadClientName("qbit-test").Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			mgr = New(db.New(dbClient))
		})

		It("warns when the record's torrent is not found in the client", func() {
			var buf bytes.Buffer
			GinkgoWriter.TeeTo(&buf)
			DeferCleanup(GinkgoWriter.ClearTeeWriters)

			completed, err := mgr.CheckStatus(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(completed).To(BeEmpty())
			Expect(buf.String()).To(ContainSubstring("level=WARN"))
			Expect(buf.String()).To(ContainSubstring("abc123"))
		})
	})

var _ = Describe("Manager.PurgeOrphanedTorrents",
	Label("integration", "downloads"), func() {
		var (
			ctx      context.Context
			dbClient *ent.Client
			mgr      Downloader
			recID    uint32
			movieID  uint32
		)

		setup := func(aged bool) {
			ctx = context.Background()

			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/info",
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("[]")) // torrent gone => NotFound
				})
			ts := httptest.NewServer(mux)
			DeferCleanup(ts.Close)

			var err error
			dbClient, err = db.Open(ctx, ":memory:")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { dbClient.Close() })

			host, port := splitHostPort(ts.URL)
			qbitClientConfig("qbit-test", host, port)

			m, err := dbClient.Movie.Create().
				SetTitle("Fight Club").
				SetOriginalTitle("Fight Club").SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusDownloading).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			movieID = m.ID

			create := dbClient.DownloadRecord.Create().
				SetTitle("Fight.Club.1999.1080p").
				SetTorrentHash("abc123").
				SetStatus(downloadrecord.StatusDownloading).
				SetMovieID(m.ID).SetDownloadClientName("qbit-test")
			if aged {
				create = create.SetCreateTime(time.Now().Add(-2 * time.Hour))
			}
			rec, err := create.Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			recID = rec.ID

			mgr = New(db.New(dbClient))
		}

		It(
			"deletes an aged record whose torrent is gone and reverts the movie",
			func() {
				setup(true)
				Expect(mgr.(Cleaner).PurgeOrphanedTorrents(ctx)).To(Succeed())

				_, err := dbClient.DownloadRecord.Get(ctx, recID)
				Expect(ent.IsNotFound(err)).To(BeTrue())

				mv, err := dbClient.Movie.Get(ctx, movieID)
				Expect(err).NotTo(HaveOccurred())
				Expect(mv.Status).To(Equal(movie.StatusWanted))
			},
		)

		It("leaves a young record untouched (within the grace window)", func() {
			setup(false)
			Expect(mgr.(Cleaner).PurgeOrphanedTorrents(ctx)).To(Succeed())

			rec, err := dbClient.DownloadRecord.Get(ctx, recID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Status).To(Equal(downloadrecord.StatusDownloading))
		})
	})
