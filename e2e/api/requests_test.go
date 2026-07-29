package api

import (
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type requestView struct {
	Id        uint32 `json:"id"`
	MediaType string `json:"media_type"`
	MediaId   uint32 `json:"media_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
}

// createRequest submits a request as the given identity. Requests have no
// delete route, so the created row is left behind and every assertion in this
// file is written to tolerate pre-existing rows.
func createRequest(id identity, mediaID uint32, title string) requestView {
	GinkgoHelper()
	resp := post("/api/v1/requests", id, map[string]any{
		"media_type": "movie",
		"media_id":   mediaID,
		"title":      title,
	})
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	var created requestView
	decode(resp, &created)
	return created
}

var _ = Describe("REST API requests", Label("e2e"), func() {
	It("creates and lists a request", func() {
		created := createRequest(adminAuth, 900001, "E2E Requested Movie")
		Expect(created.Status).To(Equal("pending"))
		Expect(created.MediaType).To(Equal("movie"))
		Expect(created.MediaId).To(BeEquivalentTo(900001))

		resp := get(
			"/api/v1/requests?status=pending&media_type=movie&page=1&limit=50",
			adminAuth,
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var list struct {
			Items []requestView `json:"items"`
			Total uint32        `json:"total"`
			Page  uint32        `json:"page"`
			Limit uint32        `json:"limit"`
		}
		decode(resp, &list)
		Expect(list.Items).To(ContainElement(HaveField("Id", created.Id)))
		Expect(list.Page).To(BeEquivalentTo(1))
		Expect(list.Limit).To(BeEquivalentTo(50))
	})

	It("409s a duplicate request for the same media", func() {
		created := createRequest(adminAuth, 900002, "E2E Duplicate Movie")
		Expect(created.Id).NotTo(BeZero())

		resp := post("/api/v1/requests", adminAuth, map[string]any{
			"media_type": "movie",
			"media_id":   900002,
			"title":      "E2E Duplicate Movie",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		var body struct {
			Code string `json:"code"`
		}
		decode(resp, &body)
		Expect(body.Code).To(Equal("duplicate"))
	})

	It("returns request counts by status", func() {
		created := createRequest(adminAuth, 900007, "E2E Counted Movie")

		resp := get("/api/v1/requests/counts", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var counts struct {
			Pending   int `json:"pending"`
			Approved  int `json:"approved"`
			Denied    int `json:"denied"`
			Available int `json:"available"`
		}
		decode(resp, &counts)
		Expect(created.Status).To(Equal("pending"))
		Expect(counts.Pending).To(BeNumerically(">=", 1))
	})

	It("denies and reopens a request", func() {
		created := createRequest(adminAuth, 900003, "E2E Denied Movie")

		denied := post(
			fmt.Sprintf("/api/v1/requests/%d/deny", created.Id),
			adminAuth,
			map[string]any{"reason": "e2e"},
		)
		defer denied.Body.Close()
		Expect(denied.StatusCode).To(Equal(http.StatusOK))
		var detail requestView
		decode(denied, &detail)
		Expect(detail.Status).To(Equal("denied"))

		reopened := post(
			fmt.Sprintf("/api/v1/requests/%d/reopen", created.Id),
			adminAuth,
			nil,
		)
		defer reopened.Body.Close()
		Expect(reopened.StatusCode).To(Equal(http.StatusOK))
		decode(reopened, &detail)
		Expect(detail.Status).To(Equal("pending"))
	})

	It("scopes the list to their own rows for a request_only caller", func() {
		own := createRequest(viewerAuth, 900004, "E2E Viewer Movie")
		foreign := createRequest(adminAuth, 900005, "E2E Admin Movie")

		resp := get("/api/v1/requests?limit=50", viewerAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var list struct {
			Items []requestView `json:"items"`
		}
		decode(resp, &list)
		Expect(list.Items).To(ContainElement(HaveField("Id", own.Id)))
		Expect(list.Items).NotTo(ContainElement(HaveField("Id", foreign.Id)))
	})

	// Approval creates the library item, which needs TMDB; only the RBAC
	// guard is hermetic.
	It("403s review actions for a request_only caller", func() {
		approved := post("/api/v1/requests/1/approve", viewerAuth, map[string]any{
			"quality_profile": "default",
		})
		defer approved.Body.Close()
		Expect(approved.StatusCode).To(Equal(http.StatusForbidden))

		denied := post("/api/v1/requests/1/deny", viewerAuth, map[string]any{
			"reason": "nope",
		})
		defer denied.Body.Close()
		Expect(denied.StatusCode).To(Equal(http.StatusForbidden))

		reopened := post("/api/v1/requests/1/reopen", viewerAuth, nil)
		defer reopened.Body.Close()
		Expect(reopened.StatusCode).To(Equal(http.StatusForbidden))
	})

	Describe("GET /requests/{id}/metadata", func() {
		It("404s an unknown request", func() {
			resp := get("/api/v1/requests/999999/metadata", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("403s a request_only caller reading someone else's request", func() {
			foreign := createRequest(adminAuth, 900006, "E2E Foreign Movie")
			resp := get(
				fmt.Sprintf("/api/v1/requests/%d/metadata", foreign.Id),
				viewerAuth,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
})
