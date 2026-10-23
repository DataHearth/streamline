package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The e2e config declares no builtin download client, so the engine is absent
// and every torrent route reports "no builtin download client configured".
var _ = Describe("REST API torrents", Label("e2e"), func() {
	It("404s the collection routes without a builtin client", func() {
		list := get("/api/v1/torrents", adminAuth)
		defer list.Body.Close()
		Expect(list.StatusCode).To(Equal(http.StatusNotFound))

		added := post("/api/v1/torrents", adminAuth, map[string]any{
			"magnet": "magnet:?xt=urn:btih:" + hash40,
		})
		defer added.Body.Close()
		Expect(added.StatusCode).To(Equal(http.StatusNotFound))
		var body struct {
			Message string `json:"message"`
		}
		decode(added, &body)
		Expect(body.Message).To(ContainSubstring("builtin download client"))
	})

	It("404s the per-torrent routes without a builtin client", func() {
		read := get("/api/v1/torrents/"+hash40, adminAuth)
		defer read.Body.Close()
		Expect(read.StatusCode).To(Equal(http.StatusNotFound))

		paused := post("/api/v1/torrents/"+hash40+"/pause", adminAuth, nil)
		defer paused.Body.Close()
		Expect(paused.StatusCode).To(Equal(http.StatusNotFound))

		resumed := post("/api/v1/torrents/"+hash40+"/resume", adminAuth, nil)
		defer resumed.Body.Close()
		Expect(resumed.StatusCode).To(Equal(http.StatusNotFound))

		prioritised := patch(
			"/api/v1/torrents/"+hash40+"/files/0",
			adminAuth,
			map[string]any{"priority": "high"},
		)
		defer prioritised.Body.Close()
		Expect(prioritised.StatusCode).To(Equal(http.StatusNotFound))

		deleted := del(
			"/api/v1/torrents/"+hash40+"?delete_files=true",
			adminAuth,
			nil,
		)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("403s the mutating routes for a non-admin", func() {
		added := post("/api/v1/torrents", viewerAuth, map[string]any{
			"magnet": "magnet:?xt=urn:btih:" + hash40,
		})
		defer added.Body.Close()
		Expect(added.StatusCode).To(Equal(http.StatusForbidden))

		paused := post("/api/v1/torrents/"+hash40+"/pause", viewerAuth, nil)
		defer paused.Body.Close()
		Expect(paused.StatusCode).To(Equal(http.StatusForbidden))

		resumed := post("/api/v1/torrents/"+hash40+"/resume", viewerAuth, nil)
		defer resumed.Body.Close()
		Expect(resumed.StatusCode).To(Equal(http.StatusForbidden))

		prioritised := patch(
			"/api/v1/torrents/"+hash40+"/files/0",
			viewerAuth,
			map[string]any{"priority": "skip"},
		)
		defer prioritised.Body.Close()
		Expect(prioritised.StatusCode).To(Equal(http.StatusForbidden))

		deleted := del("/api/v1/torrents/"+hash40, viewerAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusForbidden))
	})
})
