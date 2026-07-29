package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("REST API library imports", Label("e2e"), func() {
	It("lists import scans", func() {
		resp := get("/api/v1/library/imports?page=1&limit=10", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var list struct {
			Items []struct {
				Id uint32 `json:"id"`
			} `json:"items"`
			Total uint32 `json:"total"`
		}
		decode(resp, &list)
		Expect(list.Items).NotTo(BeNil())
	})

	It("422s a relative source path", func() {
		resp := post("/api/v1/library/imports", adminAuth, map[string]any{
			"source_path": "relative/path",
			"mode":        "rename",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("422s an in-place scan rooted outside the library", func() {
		resp := post("/api/v1/library/imports", adminAuth, map[string]any{
			"source_path": GinkgoT().TempDir(),
			"mode":        "in_place",
			"import_mode": "hardlink",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("404s the lifecycle routes for an unknown scan", func() {
		read := get("/api/v1/library/imports/999999", adminAuth)
		defer read.Body.Close()
		Expect(read.StatusCode).To(Equal(http.StatusNotFound))

		cancelled := post("/api/v1/library/imports/999999/cancel", adminAuth, nil)
		defer cancelled.Body.Close()
		Expect(cancelled.StatusCode).To(Equal(http.StatusNotFound))

		committed := post("/api/v1/library/imports/999999/commit", adminAuth, nil)
		defer committed.Body.Close()
		Expect(committed.StatusCode).To(Equal(http.StatusNotFound))

		deleted := del("/api/v1/library/imports/999999", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("returns empty review pages for an unknown scan", func() {
		files := get(
			"/api/v1/library/imports/999999/files?classification=ambiguous&q=x&page=1&limit=10",
			adminAuth,
		)
		defer files.Body.Close()
		Expect(files.StatusCode).To(Equal(http.StatusOK))
		var fileList struct {
			Items []struct {
				Id uint32 `json:"id"`
			} `json:"items"`
			Total uint32 `json:"total"`
		}
		decode(files, &fileList)
		Expect(fileList.Items).To(BeEmpty())
		Expect(fileList.Total).To(BeZero())

		shows := get(
			"/api/v1/library/imports/999999/shows?classification=confirmed&page=1&limit=10",
			adminAuth,
		)
		defer shows.Body.Close()
		Expect(shows.StatusCode).To(Equal(http.StatusOK))
		var showList struct {
			Items []struct {
				Id uint32 `json:"id"`
			} `json:"items"`
			Total uint32 `json:"total"`
		}
		decode(shows, &showList)
		Expect(showList.Items).To(BeEmpty())
		Expect(showList.Total).To(BeZero())
	})

	It("403s every import route for a non-admin", func() {
		list := get("/api/v1/library/imports", viewerAuth)
		defer list.Body.Close()
		Expect(list.StatusCode).To(Equal(http.StatusForbidden))

		started := post("/api/v1/library/imports", viewerAuth, map[string]any{
			"source_path": "/tmp",
			"mode":        "rename",
		})
		defer started.Body.Close()
		Expect(started.StatusCode).To(Equal(http.StatusForbidden))

		read := get("/api/v1/library/imports/1", viewerAuth)
		defer read.Body.Close()
		Expect(read.StatusCode).To(Equal(http.StatusForbidden))

		deleted := del("/api/v1/library/imports/1", viewerAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusForbidden))

		cancelled := post("/api/v1/library/imports/1/cancel", viewerAuth, nil)
		defer cancelled.Body.Close()
		Expect(cancelled.StatusCode).To(Equal(http.StatusForbidden))

		committed := post("/api/v1/library/imports/1/commit", viewerAuth, nil)
		defer committed.Body.Close()
		Expect(committed.StatusCode).To(Equal(http.StatusForbidden))

		files := get("/api/v1/library/imports/1/files", viewerAuth)
		defer files.Body.Close()
		Expect(files.StatusCode).To(Equal(http.StatusForbidden))

		// Both decision routes write before they check existence, so an
		// unknown row answers 500 rather than the documented 404; the RBAC
		// guard is the only hermetic assertion available for them.
		fileDecision := patch(
			"/api/v1/library/imports/1/files/1",
			viewerAuth,
			map[string]any{"decision": "skip"},
		)
		defer fileDecision.Body.Close()
		Expect(fileDecision.StatusCode).To(Equal(http.StatusForbidden))

		shows := get("/api/v1/library/imports/1/shows", viewerAuth)
		defer shows.Body.Close()
		Expect(shows.StatusCode).To(Equal(http.StatusForbidden))

		showDecision := patch(
			"/api/v1/library/imports/1/shows/1",
			viewerAuth,
			map[string]any{"decision": "skip"},
		)
		defer showDecision.Body.Close()
		Expect(showDecision.StatusCode).To(Equal(http.StatusForbidden))
	})
})
