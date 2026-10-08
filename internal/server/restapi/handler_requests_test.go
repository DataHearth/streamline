package restapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
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
		It("rejects an out-of-range page with a JSON 400", func() {
			resp := app.do(app.req(
				http.MethodGet,
				"/api/v1/requests?page=70000",
				app.adminKey,
				nil,
			))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Header.Get("Content-Type")).
				To(HavePrefix("application/json"))
		})

		It("rejects an explicit page=0 with a JSON 400", func() {
			resp := app.do(app.req(
				http.MethodGet,
				"/api/v1/requests?page=0",
				app.adminKey,
				nil,
			))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("401s without auth", func() {
			resp, err := http.DefaultClient.Do(
				app.req(http.MethodGet, "/api/v1/requests", "invalid-token", nil),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("rejects a limit above the documented maximum", func() {
			resp, err := http.DefaultClient.Do(
				app.req(
					http.MethodGet,
					"/api/v1/requests?limit=4294967295",
					app.adminKey,
					nil,
				),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
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
				Create(mock.Anything, requestsvc.CreateParams{
					MediaType: "movie", MediaID: 5, Title: "Flick",
					RequesterID: app.memberID, QualityProfile: "Remux",
				}).
				Return(&ent.Request{ID: 1, MediaType: "movie", MediaID: 5, Title: "Flick", Status: "pending"}, nil).
				Once()

			payload, _ := json.Marshal(map[string]any{
				"media_type": "movie", "media_id": 5, "title": "Flick",
				"quality_profile": "Remux",
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

		postRequest := func(key string, body map[string]any) *http.Response {
			payload, _ := json.Marshal(body)
			r := app.req(
				http.MethodPost, "/api/v1/requests", key, bytes.NewReader(payload),
			)
			r.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(r)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(resp.Body.Close)
			return resp
		}

		It("lets request_only create an artist request by mbid (201)", func() {
			app.requests.EXPECT().
				Create(mock.Anything, requestsvc.CreateParams{
					MediaType: "artist", MediaMBID: "a-uuid", Title: "X",
					RequesterID: app.requestOnlyID,
				}).
				Return(&ent.Request{ID: 1, MediaType: "artist", MediaMbid: "a-uuid", Title: "X", Status: "pending"}, nil).
				Once()

			resp := postRequest(app.requestOnlyKey, map[string]any{
				"media_type": "artist", "media_mbid": "a-uuid", "title": "X",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var got map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got).To(HaveKeyWithValue("media_mbid", "a-uuid"))
		})

		It("creates an album request by release-group mbid (201)", func() {
			app.requests.EXPECT().
				Create(mock.Anything, requestsvc.CreateParams{
					MediaType: "album", MediaMBID: "rg-uuid", Title: "Y",
					RequesterID: app.memberID,
				}).
				Return(&ent.Request{ID: 2, MediaType: "album", MediaMbid: "rg-uuid", Title: "Y", Status: "pending"}, nil).
				Once()

			resp := postRequest(app.memberKey, map[string]any{
				"media_type": "album", "media_mbid": "rg-uuid", "title": "Y",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})

		It("400s a body the service rejects as invalid", func() {
			app.requests.EXPECT().
				Create(mock.Anything, requestsvc.CreateParams{
					MediaType: "album", Title: "Y", RequesterID: app.memberID,
				}).
				Return(nil, fmt.Errorf("%w: album needs media_mbid", requestsvc.ErrInvalidRequest)).
				Once()

			resp := postRequest(app.memberKey, map[string]any{
				"media_type": "album", "title": "Y",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			var got map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got).To(HaveKey("message"))
		})

		It("400s an unknown media_type without calling the service", func() {
			resp := postRequest(app.memberKey, map[string]any{
				"media_type": "podcast", "media_id": 1, "title": "Z",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("creates an author request as request_only (201)", func() {
			app.requests.EXPECT().
				Create(mock.Anything, requestsvc.CreateParams{
					MediaType: "author", MediaID: 123, Title: "A",
					RequesterID: app.requestOnlyID,
				}).
				Return(&ent.Request{ID: 3, MediaType: "author", MediaID: 123, Title: "A", Status: "pending"}, nil).
				Once()

			resp := postRequest(app.requestOnlyKey, map[string]any{
				"media_type": "author", "media_id": 123, "title": "A",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})

		It("creates a book request with its kind (201)", func() {
			app.requests.EXPECT().
				Create(mock.Anything, requestsvc.CreateParams{
					MediaType: "book", MediaID: 9, BookKind: "ebook", Title: "B",
					RequesterID: app.memberID,
				}).
				Return(&ent.Request{ID: 4, MediaType: "book", MediaID: 9, BookKind: "ebook", Title: "B", Status: "pending"}, nil).
				Once()

			resp := postRequest(app.memberKey, map[string]any{
				"media_type": "book", "media_id": 9,
				"book_kind": "ebook", "title": "B",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var got map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got).To(HaveKeyWithValue("book_kind", "ebook"))
		})

		It("400s a book request the service rejects", func() {
			app.requests.EXPECT().
				Create(mock.Anything, requestsvc.CreateParams{
					MediaType: "book", MediaID: 9, Title: "B",
					RequesterID: app.memberID,
				}).
				Return(nil, fmt.Errorf("%w: book needs book_kind", requestsvc.ErrInvalidRequest)).
				Once()

			resp := postRequest(app.memberKey, map[string]any{
				"media_type": "book", "media_id": 9, "title": "B",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("409s on duplicate", func() {
			app.requests.EXPECT().
				Create(mock.Anything, requestsvc.CreateParams{
					MediaType: "movie", MediaID: 5, Title: "Flick",
					RequesterID: app.memberID,
				}).
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

		It("approves a music request and returns its mbid (200)", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(1), app.adminID, "").
				Return(&ent.Request{ID: 1, MediaType: "artist", MediaMbid: "a-uuid", Title: "A", Status: "approved"}, nil).
				Once()

			resp, err := http.DefaultClient.Do(
				app.req(
					http.MethodPost,
					"/api/v1/requests/1/approve",
					app.adminKey,
					nil,
				),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var got map[string]any
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got).To(HaveKeyWithValue("media_mbid", "a-uuid"))
		})

		It("answers 429 with Retry-After when Hardcover is rate limiting", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(1), app.adminID, "").
				Return(nil, fmt.Errorf("approve: add book: %w",
					&metadata.RateLimitedError{RetryAfter: 30 * time.Second})).
				Once()

			resp, err := http.DefaultClient.Do(
				app.req(
					http.MethodPost,
					"/api/v1/requests/1/approve",
					app.adminKey,
					nil,
				),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusTooManyRequests))
			Expect(resp.Header.Get("Retry-After")).To(Equal("30"))
		})

		It("approves a book request as admin (200)", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(1), app.adminID, "").
				Return(&ent.Request{ID: 1, MediaType: "book", MediaID: 9, BookKind: "both", Title: "B", Status: "approved"}, nil).
				Once()

			resp, err := http.DefaultClient.Do(
				app.req(
					http.MethodPost,
					"/api/v1/requests/1/approve",
					app.adminKey,
					nil,
				),
			)
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

		It("404s an unknown request without calling TMDB", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(999), app.adminID, "").
				Return(nil, fmt.Errorf(
					"request 999: %w", requestsvc.ErrRequestNotFound,
				)).
				Once()

			resp, err := http.DefaultClient.Do(
				app.req(
					http.MethodPost,
					"/api/v1/requests/999/approve",
					app.adminKey,
					nil,
				),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("POST /requests/{id}/deny", func() {
		It("404s an unknown request", func() {
			app.requests.EXPECT().
				Deny(mock.Anything, uint32(999), app.adminID, "nope").
				Return(nil, fmt.Errorf(
					"request 999: %w", requestsvc.ErrRequestNotFound,
				)).
				Once()

			payload, _ := json.Marshal(map[string]any{"reason": "nope"})
			r := app.req(
				http.MethodPost,
				"/api/v1/requests/999/deny",
				app.adminKey,
				bytes.NewReader(payload),
			)
			r.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(r)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("POST /requests/{id}/reopen", func() {
		It("404s an unknown request", func() {
			app.requests.EXPECT().
				Reopen(mock.Anything, uint32(999)).
				Return(nil, fmt.Errorf(
					"request 999: %w", requestsvc.ErrRequestNotFound,
				)).
				Once()

			resp, err := http.DefaultClient.Do(
				app.req(
					http.MethodPost,
					"/api/v1/requests/999/reopen",
					app.adminKey,
					nil,
				),
			)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})
