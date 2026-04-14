package importer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	entdr "github.com/datahearth/streamline/ent/downloadrecord"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/importer"
	"github.com/datahearth/streamline/internal/jobs"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

func qbitStub(savePath string) *httptest.Server {
	GinkgoHelper()
	mux := http.NewServeMux()
	mux.HandleFunc(
		"/api/v2/auth/login",
		func(w http.ResponseWriter, _ *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "sess"})
			w.WriteHeader(http.StatusOK)
		},
	)
	mux.HandleFunc(
		"/api/v2/torrents/info",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"hash":      "abc123",
				"name":      "Test Movie",
				"state":     "uploading",
				"progress":  1.0,
				"size":      60 << 20,
				"save_path": savePath,
			}})
		},
	)
	mux.HandleFunc(
		"/api/v2/torrents/delete",
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	)
	return httptest.NewServer(mux)
}

func plexStub(libPath string, refreshes *atomic.Int32) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc(
		"/library/sections",
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"MediaContainer": map[string]any{
					"Directory": []map[string]any{{
						"key":      "1",
						"title":    "Movies",
						"Location": []map[string]any{{"path": libPath}},
					}},
				},
			})
		},
	)
	mux.HandleFunc(
		"/library/sections/1/refresh",
		func(w http.ResponseWriter, _ *http.Request) {
			refreshes.Add(1)
			w.WriteHeader(http.StatusOK)
		},
	)
	return httptest.NewServer(mux)
}

var _ = Describe("Import pipeline", Label("integration", "importer"), func() {
	It(
		"completed download lands in library, movie goes available, media server refreshes",
		func() {
			ctx := context.Background()
			tmp := GinkgoT().TempDir()
			downloadDir := filepath.Join(tmp, "downloads")
			// Content path must match <library.download_path>/<qBit torrent.Name>.
			savePath := filepath.Join(downloadDir, "Test Movie")
			libPath := filepath.Join(tmp, "library")
			Expect(os.MkdirAll(savePath, 0o755)).To(Succeed())
			Expect(os.MkdirAll(libPath, 0o755)).To(Succeed())

			mediaFile := filepath.Join(savePath, "Test.Movie.2024.1080p.WEB-DL.mkv")
			f, err := os.Create(mediaFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(f.Truncate(60 << 20)).To(Succeed())
			Expect(f.Close()).To(Succeed())

			entClient := dbtest.SetupTestDB(ctx)
			DeferCleanup(entClient.Close)
			store := db.New(entClient)

			var refreshes atomic.Int32
			msSrv := plexStub(libPath, &refreshes)
			DeferCleanup(msSrv.Close)
			qbit := qbitStub(savePath)
			DeferCleanup(qbit.Close)

			qbitParsed, err := url.Parse(qbit.URL)
			Expect(err).NotTo(HaveOccurred())
			qbitPort, err := strconv.ParseUint(qbitParsed.Port(), 10, 16)
			Expect(err).NotTo(HaveOccurred())

			configtest.Setup(map[string]any{
				"library": map[string]any{
					"movie_path":           libPath,
					"download_path":        downloadDir,
					"movie_naming":         "{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}",
					"import_mode":          "hardlink",
					"import_max_attempts":  3,
					"keep_torrent_seeding": false,
				},
				"download_clients": []map[string]any{{
					"name": "stub", "client_type": "qbittorrent",
					"host": qbitParsed.Hostname(), "port": int(qbitPort),
					"auth_method": "password", "username": "u", "password": "p",
					"enabled": true,
				}},
				"media_server": map[string]any{
					"servers": []map[string]any{{
						"name": "plex", "server_type": "plex",
						"host": msSrv.URL, "api_key": "x", "enabled": true,
					}},
				},
			})

			m, err := store.CreateMovie(ctx, db.CreateMovieParams{
				Title: "Test Movie", OriginalTitle: "Test Movie",
				Year: 2024, TmdbID: 999,
				Status: entmovie.StatusDownloading, QualityProfile: "HD",
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = entClient.DownloadRecord.Create().
				SetTitle("Test Movie").
				SetTorrentHash("abc123").
				SetStatus(entdr.StatusDownloading).
				SetMovieID(m.ID).
				SetDownloadClientName("stub").
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			dlManager := download.New(store)
			libSvc := library.NewImportService(&config.Get().Library)
			dispatcher := mediaserver.NewDispatcher()
			w := importer.NewWorker(importer.Deps{
				DB:          store,
				Library:     libSvc,
				Download:    dlManager,
				MediaServer: dispatcher,
			})

			workerCtx, cancel := context.WithCancel(ctx)
			DeferCleanup(cancel)
			go w.Start(workerCtx)

			Expect(
				jobs.DownloadMonitor(
					dlManager,
					dlManager.(download.Adopter),
					w,
				)(
					ctx,
				),
			).
				To(Succeed())

			Eventually(func() string {
				got, _ := store.FindMovieByID(ctx, m.ID)
				return string(got.Status)
			}).WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
				Should(Equal(string(entmovie.StatusAvailable)))

			recs, _ := entClient.DownloadRecord.Query().All(ctx)
			Expect(recs).To(HaveLen(1))
			Expect(recs[0].Status).To(Equal(entdr.StatusCompleted))

			// The media-server refresh fires after the movie is marked
			// available, so poll rather than assert synchronously.
			Eventually(refreshes.Load).
				WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
				Should(BeNumerically(">=", 1))

			expectedPath := filepath.Join(
				libPath,
				"Test Movie (2024) {tmdb-999}",
				"Test Movie (2024) [1080p].mkv",
			)
			_, err = os.Stat(expectedPath)
			Expect(
				err,
			).NotTo(HaveOccurred(), fmt.Sprintf("expected %s", expectedPath))
		},
	)

	It("restart recovery: importing row gets picked up by Scan", func() {
		ctx := context.Background()
		tmp := GinkgoT().TempDir()
		savePath := filepath.Join(tmp, "downloads", "movie")
		libPath := filepath.Join(tmp, "library")
		Expect(os.MkdirAll(savePath, 0o755)).To(Succeed())
		Expect(os.MkdirAll(libPath, 0o755)).To(Succeed())

		mediaFile := filepath.Join(savePath, "Restart.Movie.2024.1080p.mkv")
		f, err := os.Create(mediaFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Truncate(60 << 20)).To(Succeed())
		Expect(f.Close()).To(Succeed())

		configtest.Setup(map[string]any{
			"library": map[string]any{
				"movie_path":           libPath,
				"movie_naming":         "{title} ({year})/{title}.{ext}",
				"import_mode":          "hardlink",
				"import_max_attempts":  3,
				"keep_torrent_seeding": true,
			},
			"download_clients": []map[string]any{{
				"name": "stub", "client_type": "qbittorrent",
				"host": "unused", "port": 8080,
				"auth_method": "password", "username": "u", "password": "p",
				"enabled": true,
			}},
		})

		entClient := dbtest.SetupTestDB(ctx)
		DeferCleanup(entClient.Close)
		store := db.New(entClient)

		m, err := store.CreateMovie(ctx, db.CreateMovieParams{
			Title: "Restart Movie", OriginalTitle: "Restart Movie",
			Year: 2024, TmdbID: 888,
			Status: entmovie.StatusDownloading, QualityProfile: "HD",
		})
		Expect(err).NotTo(HaveOccurred())
		rec, err := entClient.DownloadRecord.Create().
			SetTitle("Restart Movie").
			SetStatus(entdr.StatusImporting).
			SetTorrentHash("restart-hash").
			SetSavePath(savePath).
			SetMovieID(m.ID).
			SetDownloadClientName("stub").
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		dlManager := download.New(store)
		libSvc := library.NewImportService(&config.Get().Library)
		dispatcher := mediaserver.NewDispatcher()
		w := importer.NewWorker(importer.Deps{
			DB: store, Library: libSvc, Download: dlManager, MediaServer: dispatcher,
		})

		workerCtx, cancel := context.WithCancel(ctx)
		DeferCleanup(cancel)
		go w.Start(workerCtx)

		Expect(w.Scan(ctx)).To(Succeed())

		Eventually(func() string {
			got, _ := store.FindMovieByID(ctx, m.ID)
			return string(got.Status)
		}).WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
			Should(Equal(string(entmovie.StatusAvailable)))

		got, _ := entClient.DownloadRecord.Get(ctx, rec.ID)
		Expect(got.Status).To(Equal(entdr.StatusCompleted))
	})

	It(
		"replace_existing overwrites an already-present file instead of failing",
		func() {
			ctx := context.Background()
			tmp := GinkgoT().TempDir()
			savePath := filepath.Join(tmp, "downloads", "movie")
			libPath := filepath.Join(tmp, "library")
			Expect(os.MkdirAll(savePath, 0o755)).To(Succeed())

			// The new (better) release waiting in the download dir.
			newFile := filepath.Join(savePath, "Replace.Movie.2024.1080p.mkv")
			f, err := os.Create(newFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(f.Truncate(60 << 20)).To(Succeed())
			Expect(f.Close()).To(Succeed())

			// The file already sitting in the library at the template dest path.
			destDir := filepath.Join(libPath, "Replace Movie (2024)")
			Expect(os.MkdirAll(destDir, 0o755)).To(Succeed())
			destPath := filepath.Join(destDir, "Replace Movie.mkv")
			Expect(os.WriteFile(destPath, []byte("old"), 0o644)).To(Succeed())

			configtest.Setup(map[string]any{
				"library": map[string]any{
					"movie_path":           libPath,
					"movie_naming":         "{title} ({year})/{title}.{ext}",
					"import_mode":          "copy",
					"import_max_attempts":  3,
					"keep_torrent_seeding": true,
				},
				"download_clients": []map[string]any{{
					"name": "stub", "client_type": "qbittorrent",
					"host": "unused", "port": 8080,
					"auth_method": "password", "username": "u", "password": "p",
					"enabled": true,
				}},
			})

			entClient := dbtest.SetupTestDB(ctx)
			DeferCleanup(entClient.Close)
			store := db.New(entClient)

			m, err := store.CreateMovie(ctx, db.CreateMovieParams{
				Title: "Replace Movie", OriginalTitle: "Replace Movie",
				Year: 2024, TmdbID: 777,
				Status: entmovie.StatusAvailable, QualityProfile: "HD",
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = entClient.MediaFile.Create().
				SetPath(destPath).SetSize(3).SetQuality("720p").SetFormat("mkv").
				SetMovieID(m.ID).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			rec, err := entClient.DownloadRecord.Create().
				SetTitle("Replace Movie 1080p").
				SetStatus(entdr.StatusImporting).
				SetTorrentHash("replace-hash").
				SetSavePath(savePath).
				SetMovieID(m.ID).
				SetDownloadClientName("stub").
				SetReplaceExisting(true).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			dlManager := download.New(store)
			libSvc := library.NewImportService(&config.Get().Library)
			dispatcher := mediaserver.NewDispatcher()
			w := importer.NewWorker(importer.Deps{
				DB:          store,
				Library:     libSvc,
				Download:    dlManager,
				MediaServer: dispatcher,
			})

			workerCtx, cancel := context.WithCancel(ctx)
			DeferCleanup(cancel)
			go w.Start(workerCtx)

			Expect(w.Scan(ctx)).To(Succeed())

			Eventually(func() string {
				got, _ := entClient.DownloadRecord.Get(ctx, rec.ID)
				return string(got.Status)
			}).WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
				Should(Equal(string(entdr.StatusCompleted)))

			// Old file overwritten by the new release, and exactly one media file row.
			info, err := os.Stat(destPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(Equal(int64(60 << 20)))

			files, err := store.ListMediaFilesByMovieID(ctx, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(HaveLen(1))
		},
	)
})
