package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Manager.CheckStatus payload location",
	Label("integration", "downloads"), func() {
		const name = "Fight.Club.1999.1080p"

		var (
			ctx      context.Context
			dbClient *ent.Client
			mgr      Downloader
			recID    uint32
			movieID  uint32
			// downloads is library.download_path, client the directory the
			// client's reported save path maps to.
			downloads, client string

			mu       sync.Mutex
			savePath string
		)

		reportSavePath := func(p string) {
			mu.Lock()
			defer mu.Unlock()
			savePath = p
		}

		BeforeEach(func() {
			ctx = context.Background()
			root := GinkgoT().TempDir()
			downloads = filepath.Join(root, "srv", "downloads")
			client = filepath.Join(root, "srv", "qbit")
			Expect(os.MkdirAll(downloads, 0o755)).To(Succeed())
			Expect(os.MkdirAll(client, 0o755)).To(Succeed())
			// qBittorrent in Manual mode: the torrent sits in its default save
			// path, which it reports as /data/qbit, mounted here at client.
			reportSavePath("/data/qbit")

			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/auth/login",
				func(w http.ResponseWriter, _ *http.Request) {
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s"})
					w.WriteHeader(http.StatusOK)
				})
			mux.HandleFunc("/api/v2/torrents/info",
				func(w http.ResponseWriter, _ *http.Request) {
					mu.Lock()
					p := savePath
					mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(jsonBytes([]map[string]any{{
						"hash": "abc123", "name": name, "state": "uploading",
						"progress": 1.0, "size": 5000, "save_path": p,
					}}))
				})
			ts := httptest.NewServer(mux)
			DeferCleanup(ts.Close)

			var err error
			dbClient, err = db.Open(ctx, ":memory:")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { dbClient.Close() })

			host, port := splitHostPort(ts.URL)
			configtest.Setup(map[string]any{
				"library": map[string]any{"download_path": downloads},
				"download": map[string]any{
					"path_mappings": []map[string]any{
						{"from": "/data/qbit", "to": client},
					},
				},
				"download_clients": []map[string]any{{
					"name": "qbit-test", "client_type": "qbittorrent",
					"host": host, "port": int(port), "auth_method": "password",
					"username": "admin", "password": "admin",
					"priority": 10, "enabled": true,
				}},
			})

			m, err := dbClient.Movie.Create().
				SetTitle("Fight Club").SetOriginalTitle("Fight Club").
				SetYear(1999).SetTmdbID(550).
				SetStatus(movie.StatusDownloading).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			movieID = m.ID

			rec, err := dbClient.DownloadRecord.Create().
				SetTitle(name).SetTorrentHash("abc123").
				SetStatus(downloadrecord.StatusDownloading).
				SetMovieID(m.ID).SetDownloadClientName("qbit-test").
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			recID = rec.ID

			mgr = New(db.New(dbClient), nil)
		})

		movieStatus := func() movie.Status {
			GinkgoHelper()
			m, err := dbClient.Movie.Get(ctx, movieID)
			Expect(err).NotTo(HaveOccurred())
			return m.Status
		}

		It(
			"imports from the client's mapped save path when the convention path is empty",
			func() {
				Expect(os.Mkdir(filepath.Join(client, name), 0o755)).To(Succeed())

				completed, err := mgr.CheckStatus(ctx)

				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(HaveLen(1))
				Expect(completed[0].SavePath).To(Equal(filepath.Join(client, name)))
				rec, err := dbClient.DownloadRecord.Get(ctx, recID)
				Expect(err).NotTo(HaveOccurred())
				Expect(rec.Status).To(Equal(downloadrecord.StatusImporting))
				Expect(rec.SavePath).To(Equal(filepath.Join(client, name)))
				Expect(movieStatus()).To(Equal(movie.StatusImporting))
			},
		)

		It("prefers the convention path when the payload is at both", func() {
			Expect(os.Mkdir(filepath.Join(client, name), 0o755)).To(Succeed())
			Expect(os.Mkdir(filepath.Join(downloads, name), 0o755)).To(Succeed())

			completed, err := mgr.CheckStatus(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(completed).To(HaveLen(1))
			Expect(completed[0].SavePath).To(Equal(filepath.Join(downloads, name)))
		})

		It(
			"holds a payload at neither path with both paths and no attempt spent",
			func() {
				completed, err := mgr.CheckStatus(ctx)

				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(BeEmpty())
				rec, err := dbClient.DownloadRecord.Get(ctx, recID)
				Expect(err).NotTo(HaveOccurred())
				Expect(rec.Status).To(Equal(downloadrecord.StatusHeld))
				Expect(rec.ImportAttempts).To(BeZero())
				Expect(rec.HoldReasons).To(Equal([]schema.HoldReason{{
					File:     name,
					Check:    schema.HoldCheckLocation,
					Expected: filepath.Join(downloads, name),
					Actual:   filepath.Join(client, name),
				}}))
				Expect(movieStatus()).To(Equal(movie.StatusDownloading))

				By(
					"staying held, and spending nothing, on a tick that still finds nothing",
				)
				completed, err = mgr.CheckStatus(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(BeEmpty())
				rec, err = dbClient.DownloadRecord.Get(ctx, recID)
				Expect(err).NotTo(HaveOccurred())
				Expect(rec.Status).To(Equal(downloadrecord.StatusHeld))
				Expect(rec.ImportAttempts).To(BeZero())
			},
		)

		It("releases the hold on the first tick that finds the payload", func() {
			_, err := mgr.CheckStatus(ctx)
			Expect(err).NotTo(HaveOccurred())

			By("the operator moving the torrent where the mapping reaches")
			Expect(os.Mkdir(filepath.Join(client, name), 0o755)).To(Succeed())
			completed, err := mgr.CheckStatus(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(completed).To(HaveLen(1))
			Expect(completed[0].Record.ID).To(Equal(recID))
			Expect(completed[0].SavePath).To(Equal(filepath.Join(client, name)))
			rec, err := dbClient.DownloadRecord.Get(ctx, recID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Status).To(Equal(downloadrecord.StatusImporting))
			Expect(rec.SavePath).To(Equal(filepath.Join(client, name)))
			Expect(rec.HoldReasons).To(BeEmpty())
			Expect(movieStatus()).To(Equal(movie.StatusImporting))
		})

		It("follows the client's report while the payload is still missing", func() {
			_, err := mgr.CheckStatus(ctx)
			Expect(err).NotTo(HaveOccurred())

			reportSavePath("/elsewhere")
			_, err = mgr.CheckStatus(ctx)
			Expect(err).NotTo(HaveOccurred())

			rec, err := dbClient.DownloadRecord.Get(ctx, recID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Status).To(Equal(downloadrecord.StatusHeld))
			Expect(rec.HoldReasons).To(HaveLen(1))
			Expect(
				rec.HoldReasons[0].Actual,
			).To(Equal(filepath.Join("/elsewhere", name)))
		})

		It("never releases a record held by import verification", func() {
			Expect(os.Mkdir(filepath.Join(downloads, name), 0o755)).To(Succeed())
			Expect(dbClient.DownloadRecord.UpdateOneID(recID).
				SetStatus(downloadrecord.StatusHeld).
				SetHoldReasons([]schema.HoldReason{{
					File: name, Check: "corrupt", Actual: "probe failed",
				}}).Exec(ctx)).To(Succeed())

			completed, err := mgr.CheckStatus(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(completed).To(BeEmpty())
			rec, err := dbClient.DownloadRecord.Get(ctx, recID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Status).To(Equal(downloadrecord.StatusHeld))
		})
	})
