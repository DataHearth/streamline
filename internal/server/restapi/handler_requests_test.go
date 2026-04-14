package restapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	requestsvc "github.com/datahearth/streamline/internal/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("Request handlers", Label("unit", "restapi"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
		app.addMember("")
	})

	Describe("GET /requests", func() {
		It("401s without auth", func() {
			resp, err := http.DefaultClient.Do(
				app.req(http.MethodGet, "/api/v1/requests", "invalid-token", nil),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("admins see all (no requester scoping)", func() {
			app.requests.EXPECT().
				List(mock.Anything, mock.MatchedBy(func(p db.ListRequestsParams) bool {
					return p.RequesterID == 0 // admins are not scoped
				})).
				Return([]*ent.Request{
					{
						ID:        1,
						MediaType: "movie",
						MediaID:   5,
						Title:     "A",
						Status:    "pending",
					},
				}, 1, nil).Once()

			resp, err := http.DefaultClient.Do(
				app.req(http.MethodGet, "/api/v1/requests", app.adminKey, nil),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body PaginatedRequests
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
		})
	})

	Describe("POST /requests", func() {
		It("creates a request (201)", func() {
			app.requests.EXPECT().
				Create(mock.Anything, "movie", uint32(5), "Flick", app.memberID).
				Return(&ent.Request{ID: 1, MediaType: "movie", MediaID: 5, Title: "Flick", Status: "pending"}, nil).
				Once()

			payload, _ := json.Marshal(map[string]any{
				"media_type": "movie", "media_id": 5, "title": "Flick",
			})
			r := app.req(
				http.MethodPost,
				"/api/v1/requests",
				app.memberKey,
				bytes.NewReader(payload),
			)
			r.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(r)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})

		It("409s on duplicate", func() {
			app.requests.EXPECT().
				Create(mock.Anything, "movie", uint32(5), "Flick", app.memberID).
				Return(nil, requestsvc.ErrDuplicate).Once()

			payload, _ := json.Marshal(map[string]any{
				"media_type": "movie", "media_id": 5, "title": "Flick",
			})
			r := app.req(
				http.MethodPost,
				"/api/v1/requests",
				app.memberKey,
				bytes.NewReader(payload),
			)
			r.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(r)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})
	})

	Describe("POST /requests/{id}/approve", func() {
		It("403s for request_only callers", func() {
			resp, err := http.DefaultClient.Do(
				app.req(
					http.MethodPost,
					"/api/v1/requests/1/approve",
					app.requestOnlyKey,
					nil,
				),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("approves as admin with the chosen profile (200)", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(1), app.adminID, "uhd").
				Return(&ent.Request{ID: 1, MediaType: "movie", MediaID: 5, Title: "A", Status: "approved"}, nil).
				Once()

			payload, _ := json.Marshal(map[string]any{"quality_profile": "uhd"})
			r := app.req(
				http.MethodPost,
				"/api/v1/requests/1/approve",
				app.adminKey,
				bytes.NewReader(payload),
			)
			r.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(r)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("approves as member, defaulting the profile (200)", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(1), app.memberID, "").
				Return(&ent.Request{ID: 1, MediaType: "movie", MediaID: 5, Title: "A", Status: "approved"}, nil).
				Once()

			resp, err := http.DefaultClient.Do(
				app.req(
					http.MethodPost,
					"/api/v1/requests/1/approve",
					app.memberKey,
					nil,
				),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})
})
