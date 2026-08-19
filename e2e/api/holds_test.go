package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent/downloadrecord"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe("REST API held downloads", Label("e2e"), func() {
	// always_ask is the one hold reason that needs no probe, so the flow runs
	// without depending on an ffprobe binary being present on the runner.
	alwaysAsk := func(on bool) {
		GinkgoHelper()
		resp := patch("/api/v1/config/library", adminAuth, map[string]any{
			"probe": map[string]any{"always_ask": on},
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}

	queueStatus := func(id uint32) (string, []string) {
		GinkgoHelper()
		resp := get("/api/v1/activity/queue", adminAuth)
		defer resp.Body.Close()
		var queue struct {
			Items []struct {
				Id          uint32 `json:"id"`
				Status      string `json:"status"`
				HoldReasons []struct {
					Check string `json:"check"`
				} `json:"hold_reasons"`
			} `json:"items"`
		}
		decode(resp, &queue)
		for _, it := range queue.Items {
			if it.Id != id {
				continue
			}
			checks := make([]string, 0, len(it.HoldReasons))
			for _, r := range it.HoldReasons {
				checks = append(checks, r.Check)
			}
			return it.Status, checks
		}
		return "", nil
	}

	It("holds an import for review and imports it on resolve", func() {
		alwaysAsk(true)
		DeferCleanup(func() { alwaysAsk(false) })

		ctx := context.Background()
		movie, err := app.Store.CreateMovie(ctx, db.CreateMovieParams{
			Title: "Held Flick", OriginalTitle: "Held Flick", Year: 2024,
			TmdbID: 770001, Status: entmovie.StatusWanted,
		})
		Expect(err).NotTo(HaveOccurred())
		// Deleting the movie cascades its download record away. Left behind,
		// a *completed* record would break the history spec's assertion that
		// no hermetic spec ever completes a download.
		DeferCleanup(func() {
			Expect(app.Store.DeleteMovie(ctx, movie.ID)).To(Succeed())
		})

		// Sparse, but over library.MinMediaSize — the importer skips anything
		// smaller as a sample rather than a release.
		src := filepath.Join(downloadDir, "Held.Flick.2024.1080p.mkv")
		f, err := os.Create(src)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Truncate(60 << 20)).To(Succeed())
		Expect(f.Close()).To(Succeed())
		DeferCleanup(os.Remove, src)

		rec, err := app.Store.CreateDownloadRecord(
			ctx,
			db.CreateDownloadRecordParams{
				Title: "Held.Flick.2024.1080p-GRP", Size: 1024,
				Status: downloadrecord.StatusImporting, MovieID: movie.ID,
				SavePath: src,
			},
		)
		Expect(err).NotTo(HaveOccurred())

		app.Importer.Enqueue(rec.ID)
		Eventually(func() []string {
			_, checks := queueStatus(rec.ID)
			return checks
		}, 10*time.Second, 500*time.Millisecond).To(ContainElement("always_ask"))
		status, _ := queueStatus(rec.ID)
		Expect(status).To(Equal("held"))

		resolved := post(
			fmt.Sprintf("/api/v1/downloads/%d/resolve", rec.ID),
			adminAuth, map[string]any{"action": "import"},
		)
		defer resolved.Body.Close()
		Expect(resolved.StatusCode).To(Equal(http.StatusNoContent))

		Eventually(func() string {
			resp := get(fmt.Sprintf("/api/v1/movies/%d", movie.ID), adminAuth)
			defer resp.Body.Close()
			var detail struct {
				Status     string `json:"status"`
				MediaFiles []struct {
					Path string `json:"path"`
				} `json:"media_files"`
			}
			decode(resp, &detail)
			if len(detail.MediaFiles) == 0 {
				return ""
			}
			return detail.Status
		}, 10*time.Second, 500*time.Millisecond).To(Equal("available"))
	})

	It("409s a resolve for a record that is not held", func() {
		ctx := context.Background()
		movie, err := app.Store.CreateMovie(ctx, db.CreateMovieParams{
			Title: "Plain Flick", OriginalTitle: "Plain Flick", Year: 2024,
			TmdbID: 770002, Status: entmovie.StatusWanted,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(app.Store.DeleteMovie(ctx, movie.ID)).To(Succeed())
		})
		rec, err := app.Store.CreateDownloadRecord(
			ctx,
			db.CreateDownloadRecordParams{
				Title: "Plain.Flick", Size: 1,
				Status: downloadrecord.StatusCompleted, MovieID: movie.ID,
			},
		)
		Expect(err).NotTo(HaveOccurred())

		resp := post(
			fmt.Sprintf("/api/v1/downloads/%d/resolve", rec.ID),
			adminAuth, map[string]any{"action": "import"},
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
	})
})
