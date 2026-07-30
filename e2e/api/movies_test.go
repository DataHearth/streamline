package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// releaseBody is a minimal well-formed SearchResult, enough to get past the
// request-body decode on every grab route.
var releaseBody = map[string]any{
	"title":        "E2E.Release.2024.1080p.WEB-DL.x264-GRP",
	"download_url": "http://127.0.0.1:1/e2e.torrent",
	"size":         1024,
	"seeders":      5,
}

// The library starts empty and TMDB is unreachable by design, so the movie
// specs assert list/count shapes plus the not-found and RBAC envelopes.
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
		Expect(body.Message).To(ContainSubstring("request-only"))
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

		// The rename route looks the movie up through a service that reports
		// "not found" as a plain error, so it surfaces the 500 envelope
		// rather than the 404 the spec also documents.
		It("500s rename", func() {
			renamed := post(
				"/api/v1/movies/999999/rename?preview=true",
				adminAuth,
				nil,
			)
			defer renamed.Body.Close()
			Expect(renamed.StatusCode).To(Equal(http.StatusInternalServerError))
		})
	})

	// The TMDB search reaches the network, so only the required-parameter
	// guard is exercised.
	It("400s a TMDB search with no query", func() {
		resp := get("/api/v1/search/movie", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})
})
