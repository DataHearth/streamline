package api

import (
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("REST API activity", Label("e2e"), func() {
	Describe("GET /activity", func() {
		It("returns the event feed", func() {
			since := url.QueryEscape(
				time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			)
			before := url.QueryEscape(time.Now().UTC().Format(time.RFC3339))
			resp := get(
				"/api/v1/activity?type=grabbed&type=imported&movie_id=1"+
					"&since="+since+"&before="+before+"&limit=50",
				adminAuth,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var feed struct {
				Events []struct {
					Id uint32 `json:"id"`
				} `json:"events"`
			}
			decode(resp, &feed)
			Expect(feed.Events).NotTo(BeNil())
		})

		It("400s an undecodable cursor", func() {
			resp := get("/api/v1/activity?cursor=not-a-cursor", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("/activity/queue", func() {
		It("returns a queue snapshot", func() {
			resp := get("/api/v1/activity/queue", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var queue struct {
				Items []struct {
					Id uint32 `json:"id"`
				} `json:"items"`
				RefreshedAt time.Time `json:"refreshed_at"`
			}
			decode(resp, &queue)
			Expect(queue.Items).NotTo(BeNil())
			Expect(queue.RefreshedAt).NotTo(BeZero())
		})

		It("404s cancel, pause and resume for an unknown queue item", func() {
			cancelled := del("/api/v1/activity/queue/999999", adminAuth, nil)
			defer cancelled.Body.Close()
			Expect(cancelled.StatusCode).To(Equal(http.StatusNotFound))

			paused := post("/api/v1/activity/queue/999999/pause", adminAuth, nil)
			defer paused.Body.Close()
			Expect(paused.StatusCode).To(Equal(http.StatusNotFound))

			resumed := post("/api/v1/activity/queue/999999/resume", adminAuth, nil)
			defer resumed.Body.Close()
			Expect(resumed.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("/activity/history", func() {
		It("returns terminal download history", func() {
			resp := get("/api/v1/activity/history?limit=25", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var history struct {
				Items []struct {
					Id uint32 `json:"id"`
				} `json:"items"`
			}
			decode(resp, &history)
			Expect(history.Items).NotTo(BeNil())
		})

		It("400s an undecodable cursor", func() {
			resp := get("/api/v1/activity/history?cursor=not-a-cursor", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("404s deleting an unknown history record", func() {
			resp := del("/api/v1/activity/history/999999", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("clears completed records", func() {
			resp := post("/api/v1/activity/history/clear-completed", adminAuth, nil)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			// Nothing in this suite reaches a completed download, so the
			// clear is always a no-op.
			var result struct {
				Deleted int `json:"deleted"`
			}
			decode(resp, &result)
			Expect(result.Deleted).To(BeZero())
		})
	})

	Describe("/activity/pending", func() {
		It("lists adopted-torrent proposals", func() {
			resp := get("/api/v1/activity/pending", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var pending struct {
				Items []struct {
					Id uint32 `json:"id"`
				} `json:"items"`
			}
			decode(resp, &pending)
			Expect(pending.Items).NotTo(BeNil())
		})

		It("404s import, replace and ignore for an unknown proposal", func() {
			imported := post(
				"/api/v1/activity/pending/999999/import",
				adminAuth,
				nil,
			)
			defer imported.Body.Close()
			Expect(imported.StatusCode).To(Equal(http.StatusNotFound))

			replaced := post(
				"/api/v1/activity/pending/999999/replace",
				adminAuth,
				map[string]any{"remove_old_torrent": true},
			)
			defer replaced.Body.Close()
			Expect(replaced.StatusCode).To(Equal(http.StatusNotFound))

			ignored := post(
				"/api/v1/activity/pending/999999/ignore",
				adminAuth,
				map[string]any{"remove_torrent": true},
			)
			defer ignored.Body.Close()
			Expect(ignored.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /calendar/upcoming", func() {
		It("returns upcoming movies and episodes in the window", func() {
			from := url.QueryEscape(time.Now().UTC().Format(time.RFC3339))
			to := url.QueryEscape(
				time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
			)
			resp := get("/api/v1/calendar/upcoming?from="+from+"&to="+to, adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var upcoming struct {
				Movies []struct {
					Id uint32 `json:"id"`
				} `json:"movies"`
				Episodes []struct {
					Id uint32 `json:"id"`
				} `json:"episodes"`
			}
			decode(resp, &upcoming)
			Expect(upcoming.Movies).NotTo(BeNil())
			Expect(upcoming.Episodes).NotTo(BeNil())
		})

		It("400s a window whose end precedes its start", func() {
			from := url.QueryEscape(time.Now().UTC().Format(time.RFC3339))
			to := url.QueryEscape(
				time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			)
			resp := get("/api/v1/calendar/upcoming?from="+from+"&to="+to, adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	It("403s every mutating activity route for a non-admin", func() {
		cancelled := del("/api/v1/activity/queue/1", viewerAuth, nil)
		defer cancelled.Body.Close()
		Expect(cancelled.StatusCode).To(Equal(http.StatusForbidden))

		paused := post("/api/v1/activity/queue/1/pause", viewerAuth, nil)
		defer paused.Body.Close()
		Expect(paused.StatusCode).To(Equal(http.StatusForbidden))

		resumed := post("/api/v1/activity/queue/1/resume", viewerAuth, nil)
		defer resumed.Body.Close()
		Expect(resumed.StatusCode).To(Equal(http.StatusForbidden))

		deleted := del("/api/v1/activity/history/1", viewerAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusForbidden))

		cleared := post("/api/v1/activity/history/clear-completed", viewerAuth, nil)
		defer cleared.Body.Close()
		Expect(cleared.StatusCode).To(Equal(http.StatusForbidden))

		imported := post("/api/v1/activity/pending/1/import", viewerAuth, nil)
		defer imported.Body.Close()
		Expect(imported.StatusCode).To(Equal(http.StatusForbidden))

		replaced := post("/api/v1/activity/pending/1/replace", viewerAuth, nil)
		defer replaced.Body.Close()
		Expect(replaced.StatusCode).To(Equal(http.StatusForbidden))

		ignored := post("/api/v1/activity/pending/1/ignore", viewerAuth, nil)
		defer ignored.Body.Close()
		Expect(ignored.StatusCode).To(Equal(http.StatusForbidden))
	})
})
