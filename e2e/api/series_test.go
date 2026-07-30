package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The library starts empty and TVDB is unreachable by design, so the series
// specs assert list/count shapes plus the not-found and RBAC envelopes.
var _ = Describe("REST API series", Label("e2e"), func() {
	It("lists series with pagination metadata", func() {
		resp := get(
			"/api/v1/series?page=1&limit=5&sort=title&status=continuing&type=standard&query=e2e",
			adminAuth,
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var page struct {
			Items []struct {
				Id uint32 `json:"id"`
			} `json:"items"`
			Page  uint32 `json:"page"`
			Limit uint16 `json:"limit"`
		}
		decode(resp, &page)
		Expect(page.Items).NotTo(BeNil())
		Expect(page.Page).To(BeEquivalentTo(1))
		Expect(page.Limit).To(BeEquivalentTo(5))
	})

	It("returns series status counts", func() {
		resp := get("/api/v1/series/counts", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var counts struct {
			Total          uint32 `json:"total"`
			Continuing     uint32 `json:"continuing"`
			Ended          uint32 `json:"ended"`
			WantedEpisodes uint32 `json:"wanted_episodes"`
		}
		decode(resp, &counts)
		Expect(counts.Total).To(BeNumerically(">=", counts.Continuing+counts.Ended))
	})

	It("403s a direct library add for a request_only caller", func() {
		resp := post("/api/v1/series", viewerAuth, map[string]any{
			"tvdb_id": 121361,
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	// The TVDB lookup reaches the network, so only the required-parameter
	// guard is exercised.
	It("400s a TVDB lookup with no query", func() {
		resp := get("/api/v1/series/lookup", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	Describe("unknown series id", func() {
		It("404s read, patch and delete", func() {
			read := get("/api/v1/series/999999", adminAuth)
			defer read.Body.Close()
			Expect(read.StatusCode).To(Equal(http.StatusNotFound))

			patched := patch("/api/v1/series/999999", adminAuth, map[string]any{
				"monitored": false,
				"preset":    "all",
			})
			defer patched.Body.Close()
			Expect(patched.StatusCode).To(Equal(http.StatusNotFound))

			deleted := del("/api/v1/series/999999?delete_files=true", adminAuth, nil)
			defer deleted.Body.Close()
			Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s the season routes", func() {
			patched := patch(
				"/api/v1/series/999999/seasons/1",
				adminAuth,
				map[string]any{"monitored": true},
			)
			defer patched.Body.Close()
			Expect(patched.StatusCode).To(Equal(http.StatusNotFound))

			browsed := post("/api/v1/series/999999/seasons/1/search", adminAuth, nil)
			defer browsed.Body.Close()
			Expect(browsed.StatusCode).To(Equal(http.StatusNotFound))

			grabbed := post(
				"/api/v1/series/999999/seasons/1/grab",
				adminAuth,
				releaseBody,
			)
			defer grabbed.Body.Close()
			Expect(grabbed.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s the episode routes", func() {
			patched := patch(
				"/api/v1/series/999999/episodes/999999",
				adminAuth,
				map[string]any{"monitored": true},
			)
			defer patched.Body.Close()
			Expect(patched.StatusCode).To(Equal(http.StatusNotFound))

			browsed := post(
				"/api/v1/series/999999/episodes/999999/search",
				adminAuth,
				nil,
			)
			defer browsed.Body.Close()
			Expect(browsed.StatusCode).To(Equal(http.StatusNotFound))

			grabbed := post(
				"/api/v1/series/999999/episodes/999999/grab",
				adminAuth,
				releaseBody,
			)
			defer grabbed.Body.Close()
			Expect(grabbed.StatusCode).To(Equal(http.StatusNotFound))

			deleted := del(
				"/api/v1/series/999999/episodes/999999/file",
				adminAuth,
				map[string]any{"remove_torrent": true},
			)
			defer deleted.Body.Close()
			Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s the whole-series search, browse and grab routes", func() {
			searched := post("/api/v1/series/999999/search", adminAuth, nil)
			defer searched.Body.Close()
			Expect(searched.StatusCode).To(Equal(http.StatusNotFound))

			browsed := post("/api/v1/series/999999/browse", adminAuth, nil)
			defer browsed.Body.Close()
			Expect(browsed.StatusCode).To(Equal(http.StatusNotFound))

			grabbed := post("/api/v1/series/999999/grab", adminAuth, releaseBody)
			defer grabbed.Body.Close()
			Expect(grabbed.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s refresh-metadata and play-on", func() {
			refreshed := post(
				"/api/v1/series/999999/refresh-metadata",
				adminAuth,
				nil,
			)
			defer refreshed.Body.Close()
			Expect(refreshed.StatusCode).To(Equal(http.StatusNotFound))

			playOn := get("/api/v1/series/999999/play-on", adminAuth)
			defer playOn.Body.Close()
			Expect(playOn.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("404s rename", func() {
			resp := post("/api/v1/series/999999/rename?preview=true", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			var body struct {
				Message string `json:"message"`
			}
			decode(resp, &body)
			Expect(body.Message).To(Equal("series not found"))
		})
	})
})
