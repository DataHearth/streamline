package api

import (
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/fakes"
)

// releaseBody is a minimal well-formed SearchResult, enough to get past the
// request-body decode on every grab route.
var releaseBody = map[string]any{
	"title":        "E2E.Release.2024.1080p.WEB-DL.x264-GRP",
	"download_url": "http://127.0.0.1:1/e2e.torrent",
	"size":         1024,
	"seeders":      5,
}

// The library starts empty and TMDB is answered by the in-process fake
// (e2e/fakes), so the specs cover the add/detail/search happy paths on top of
// the list, count, not-found and RBAC envelopes.
var _ = Describe("REST API movies", Label("e2e"), func() {
	It("lists movies with pagination metadata", func() {
		resp := get("/api/v1/movies?page=1&limit=5", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var page struct {
			Items []struct {
				Id uint32 `json:"id"`
			} `json:"items"`
			Total uint32 `json:"total"`
			Page  uint32 `json:"page"`
			Limit uint16 `json:"limit"`
		}
		decode(resp, &page)
		Expect(page.Items).NotTo(BeNil())
		Expect(page.Page).To(BeEquivalentTo(1))
		Expect(page.Limit).To(BeEquivalentTo(5))
	})

	It("returns movie status counts", func() {
		resp := get("/api/v1/movies/counts", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var counts struct {
			Total       uint32   `json:"total"`
			Wanted      uint32   `json:"wanted"`
			Downloading uint32   `json:"downloading"`
			Available   uint32   `json:"available"`
			Failed      uint32   `json:"failed"`
			Trend       []uint32 `json:"trend"`
		}
		decode(resp, &counts)
		Expect(counts.Total).To(Equal(
			counts.Wanted + counts.Downloading + counts.Available + counts.Failed,
		))
		Expect(counts.Trend).NotTo(BeNil())
	})

	It("adds a movie from TMDB and serves its detail", func() {
		created := post("/api/v1/movies", adminAuth, map[string]any{
			"tmdb_id": fakes.MovieTMDBID,
		})
		defer created.Body.Close()
		Expect(created.StatusCode).To(Equal(http.StatusCreated))
		var movie struct {
			Id       uint32 `json:"id"`
			Title    string `json:"title"`
			Year     uint16 `json:"year"`
			Overview string `json:"overview"`
			Runtime  uint16 `json:"runtime"`
			Status   string `json:"status"`
		}
		decode(created, &movie)
		DeferCleanup(func() {
			removed := del(
				fmt.Sprintf("/api/v1/movies/%d", movie.Id),
				adminAuth,
				nil,
			)
			defer removed.Body.Close()
			Expect(removed.StatusCode).To(BeElementOf(
				http.StatusNoContent, http.StatusNotFound,
			))
		})
		Expect(movie.Title).To(Equal(fakes.MovieTitle))
		Expect(movie.Year).To(BeEquivalentTo(fakes.MovieYear))
		Expect(movie.Overview).To(Equal(fakes.MovieOverview))
		Expect(movie.Runtime).To(BeEquivalentTo(fakes.MovieRuntime))
		Expect(movie.Status).To(Equal("wanted"))

		detail := get(fmt.Sprintf("/api/v1/movies/%d", movie.Id), adminAuth)
		defer detail.Body.Close()
		Expect(detail.StatusCode).To(Equal(http.StatusOK))
		var view struct {
			Id     uint32   `json:"id"`
			TmdbId uint32   `json:"tmdb_id"`
			Genres []string `json:"genres"`
			Rating float32  `json:"rating"`
			Cast   []struct {
				Name string `json:"name"`
			} `json:"cast"`
		}
		decode(detail, &view)
		Expect(view.Id).To(Equal(movie.Id))
		Expect(view.TmdbId).To(BeEquivalentTo(fakes.MovieTMDBID))
		Expect(view.Genres).To(ContainElement(fakes.MovieGenre))
		Expect(view.Cast).To(ContainElement(HaveField("Name", fakes.MovieCastName)))
		Expect(view.Rating).To(BeNumerically("~", fakes.MovieRating, 0.01))
	})

	It("403s a direct library add for a request_only caller", func() {
		resp := post("/api/v1/movies", viewerAuth, map[string]any{
			"tmdb_id": 550,
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		var body struct {
			Message string `json:"message"`
		}
		decode(resp, &body)
		Expect(body.Message).To(ContainSubstring("member role required"))
	})

	It("403s a grab for a request_only caller", func() {
		resp := post("/api/v1/movies/1/grab", viewerAuth, map[string]any{
			"title":        "x",
			"download_url": "magnet:?xt=urn:btih:0",
			"size":         1,
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("403s a delete-with-files for a request_only caller", func() {
		resp := del("/api/v1/movies/1?delete_files=true", viewerAuth, nil)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	Describe("unknown movie id", func() {
		It("404s read, patch and delete", func() {
			read := get("/api/v1/movies/999999", adminAuth)
			defer read.Body.Close()
			Expect(read.StatusCode).To(Equal(http.StatusNotFound))

			patched := patch("/api/v1/movies/999999", adminAuth, map[string]any{
				"monitored": false,
			})
			defer patched.Body.Close()
			Expect(patched.StatusCode).To(Equal(http.StatusNotFound))

			deleted := del("/api/v1/movies/999999?delete_files=true", adminAuth, nil)
			defer deleted.Body.Close()
			Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s the search, recommendation and play-on routes", func() {
			search := post("/api/v1/movies/999999/search", adminAuth, nil)
			defer search.Body.Close()
			Expect(search.StatusCode).To(Equal(http.StatusNotFound))

			searchNow := post("/api/v1/movies/999999/search-now", adminAuth, nil)
			defer searchNow.Body.Close()
			Expect(searchNow.StatusCode).To(Equal(http.StatusNotFound))

			recs := get("/api/v1/movies/999999/recommendations", adminAuth)
			defer recs.Body.Close()
			Expect(recs.StatusCode).To(Equal(http.StatusNotFound))

			playOn := get("/api/v1/movies/999999/play-on", adminAuth)
			defer playOn.Body.Close()
			Expect(playOn.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s grabbing a release", func() {
			resp := post("/api/v1/movies/999999/grab", adminAuth, releaseBody)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s deleting a media file", func() {
			resp := del(
				"/api/v1/movies/999999/files/1",
				adminAuth,
				map[string]any{"remove_torrent": true},
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s refresh-metadata", func() {
			resp := post(
				"/api/v1/movies/999999/refresh-metadata",
				adminAuth,
				nil,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s rename", func() {
			renamed := post(
				"/api/v1/movies/999999/rename?preview=true",
				adminAuth,
				nil,
			)
			defer renamed.Body.Close()
			Expect(renamed.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	It("searches TMDB by title", func() {
		resp := get("/api/v1/search/movie?q=fight", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var results []struct {
			TmdbId    uint32 `json:"tmdb_id"`
			Title     string `json:"title"`
			Year      uint16 `json:"year"`
			Overview  string `json:"overview"`
			PosterUrl string `json:"poster_url"`
		}
		decode(resp, &results)
		Expect(results).To(HaveLen(1))
		Expect(results[0].TmdbId).To(BeEquivalentTo(fakes.MovieTMDBID))
		Expect(results[0].Title).To(Equal(fakes.MovieTitle))
		Expect(results[0].Year).To(BeEquivalentTo(fakes.MovieYear))
		Expect(results[0].Overview).To(Equal(fakes.MovieOverview))
		Expect(results[0].PosterUrl).To(HaveSuffix(fakes.MoviePosterPath))
	})

	It("400s a TMDB search with no query", func() {
		resp := get("/api/v1/search/movie", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})
})
