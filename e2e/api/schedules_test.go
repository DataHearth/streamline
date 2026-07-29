package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type scheduleView struct {
	Name     string `json:"name"`
	Interval string `json:"interval"`
	System   bool   `json:"system"`
	Paused   bool   `json:"paused"`
	Running  bool   `json:"running"`
	Status   string `json:"status"`
}

func readSchedule(name string) scheduleView {
	GinkgoHelper()
	resp := get("/api/v1/schedules/"+name, adminAuth)
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	var sched scheduleView
	decode(resp, &sched)
	return sched
}

var _ = Describe("REST API schedules", Label("e2e"), func() {
	It("lists every registered job", func() {
		resp := get("/api/v1/schedules", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var list struct {
			Items []scheduleView `json:"items"`
		}
		decode(resp, &list)
		Expect(list.Items).To(ContainElement(HaveField("Name", "rss-sync")))
		Expect(list.Items).To(ContainElement(HaveField("System", true)))
	})

	It("returns a single job by name", func() {
		sched := readSchedule("rss-sync")
		Expect(sched.Name).To(Equal("rss-sync"))
		Expect(sched.Interval).NotTo(BeEmpty())
		Expect(sched.System).To(BeFalse())
	})

	It("404s an unknown job name", func() {
		read := get("/api/v1/schedules/e2e-missing", adminAuth)
		defer read.Body.Close()
		Expect(read.StatusCode).To(Equal(http.StatusNotFound))

		paused := post("/api/v1/schedules/e2e-missing/pause", adminAuth, nil)
		defer paused.Body.Close()
		Expect(paused.StatusCode).To(Equal(http.StatusNotFound))

		resumed := post("/api/v1/schedules/e2e-missing/resume", adminAuth, nil)
		defer resumed.Body.Close()
		Expect(resumed.StatusCode).To(Equal(http.StatusNotFound))

		ran := post("/api/v1/schedules/e2e-missing/run", adminAuth, nil)
		defer ran.Body.Close()
		Expect(ran.StatusCode).To(Equal(http.StatusNotFound))

		patched := patch("/api/v1/schedules/e2e-missing", adminAuth, map[string]any{
			"interval": "30m",
		})
		defer patched.Body.Close()
		Expect(patched.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("edits a user-configurable interval", func() {
		before := readSchedule("cleanup")
		DeferCleanup(func() {
			restore := patch("/api/v1/schedules/cleanup", adminAuth, map[string]any{
				"interval": before.Interval,
			})
			defer restore.Body.Close()
			Expect(restore.StatusCode).To(Equal(http.StatusOK))
		})

		resp := patch("/api/v1/schedules/cleanup", adminAuth, map[string]any{
			"interval": "45m",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var sched scheduleView
		decode(resp, &sched)
		Expect(sched.Interval).To(Equal("45m0s"))
	})

	It("422s an interval below the floor", func() {
		resp := patch("/api/v1/schedules/cleanup", adminAuth, map[string]any{
			"interval": "1s",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("422s an unparseable interval", func() {
		resp := patch("/api/v1/schedules/cleanup", adminAuth, map[string]any{
			"interval": "not-a-duration",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("403s editing a system job", func() {
		resp := patch("/api/v1/schedules/purge-sessions", adminAuth, map[string]any{
			"interval": "30m",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("pauses and resumes a job", func() {
		paused := post("/api/v1/schedules/metadata-refresh/pause", adminAuth, nil)
		defer paused.Body.Close()
		// Registered before the first assertion so a mid-spec failure still
		// un-pauses the job. Conflict means it was never paused.
		DeferCleanup(func() {
			restore := post(
				"/api/v1/schedules/metadata-refresh/resume",
				adminAuth,
				nil,
			)
			defer restore.Body.Close()
			Expect(restore.StatusCode).To(BeElementOf(
				http.StatusOK, http.StatusConflict,
			))
		})
		Expect(paused.StatusCode).To(Equal(http.StatusOK))
		var sched scheduleView
		decode(paused, &sched)
		Expect(sched.Paused).To(BeTrue())

		again := post("/api/v1/schedules/metadata-refresh/pause", adminAuth, nil)
		defer again.Body.Close()
		Expect(again.StatusCode).To(Equal(http.StatusConflict))

		resumed := post("/api/v1/schedules/metadata-refresh/resume", adminAuth, nil)
		defer resumed.Body.Close()
		Expect(resumed.StatusCode).To(Equal(http.StatusOK))
		decode(resumed, &sched)
		Expect(sched.Paused).To(BeFalse())

		notPaused := post(
			"/api/v1/schedules/metadata-refresh/resume",
			adminAuth,
			nil,
		)
		defer notPaused.Body.Close()
		Expect(notPaused.StatusCode).To(Equal(http.StatusConflict))
	})

	// The suite boots the app without starting the scheduler loop, so a
	// run-now on a real job would report ErrNotStarted; only the unknown-job
	// and RBAC guards are meaningful here.
	It("403s triggering a run for a non-admin", func() {
		resp := post("/api/v1/schedules/cleanup/run", viewerAuth, nil)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("403s every schedule route for a non-admin", func() {
		list := get("/api/v1/schedules", viewerAuth)
		defer list.Body.Close()
		Expect(list.StatusCode).To(Equal(http.StatusForbidden))

		read := get("/api/v1/schedules/cleanup", viewerAuth)
		defer read.Body.Close()
		Expect(read.StatusCode).To(Equal(http.StatusForbidden))

		patched := patch("/api/v1/schedules/cleanup", viewerAuth, map[string]any{
			"interval": "30m",
		})
		defer patched.Body.Close()
		Expect(patched.StatusCode).To(Equal(http.StatusForbidden))

		paused := post("/api/v1/schedules/cleanup/pause", viewerAuth, nil)
		defer paused.Body.Close()
		Expect(paused.StatusCode).To(Equal(http.StatusForbidden))

		resumed := post("/api/v1/schedules/cleanup/resume", viewerAuth, nil)
		defer resumed.Body.Close()
		Expect(resumed.StatusCode).To(Equal(http.StatusForbidden))
	})
})
