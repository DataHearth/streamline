package restapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book"
	requestsvc "github.com/datahearth/streamline/internal/request"
)

var _ = Describe("Handler: Requests", Label("unit", "server", "request"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
		app.addMember("")
	})

	send := func(method, path, key, body string) *http.Response {
		GinkgoHelper()
		var req *http.Request
		if body == "" {
			req = app.req(method, path, key, nil)
		} else {
			req = app.req(method, path, key, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		return app.do(req)
	}

	Describe("GET /requests", func() {
		It("passes the media_type list through as a set", func() {
			app.requests.EXPECT().
				List(mock.Anything, mock.MatchedBy(func(p db.ListRequestsParams) bool {
					return len(p.MediaTypes) == 2 &&
						p.MediaTypes[0] == "book" && p.MediaTypes[1] == "book_series" &&
						p.RequesterID == 0
				})).
				Return([]*ent.Request{}, 0, nil).Once()

			resp := send(http.MethodGet,
				"/api/v1/requests?media_type=book,book_series", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("answers 400 for an unknown member of the list", func() {
			resp := send(http.MethodGet,
				"/api/v1/requests?media_type=book,album", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("scopes a request-only caller to their own rows", func() {
			app.requests.EXPECT().
				List(mock.Anything, mock.MatchedBy(func(p db.ListRequestsParams) bool {
					return p.RequesterID == app.requestOnlyID
				})).
				Return([]*ent.Request{}, 0, nil).Once()

			resp := send(http.MethodGet, "/api/v1/requests", app.requestOnlyKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("POST /requests", func() {
		It("creates an artist request keyed by MBID", func() {
			app.requests.EXPECT().
				Create(mock.Anything, mock.MatchedBy(func(p requestsvc.CreateParams) bool {
					return p.MediaType == "artist" && p.MediaMBID == "mbid-1" &&
						p.MediaID == 0 && p.QualityProfile == "Lossless"
				})).
				Return(&ent.Request{ID: 1, MediaType: "artist", MediaMbid: "mbid-1"}, nil).
				Once()

			resp := send(
				http.MethodPost,
				"/api/v1/requests",
				app.requestOnlyKey,
				`{"media_type":"artist","media_mbid":"mbid-1","title":"A","quality_profile":"Lossless"}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var got Request
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.MediaType).To(Equal(RequestMediaType("artist")))
			Expect(*got.MediaMbid).To(Equal("mbid-1"))
		})

		It("creates a series request by Hardcover id", func() {
			app.requests.EXPECT().
				Create(mock.Anything, mock.MatchedBy(func(p requestsvc.CreateParams) bool {
					return p.MediaType == "book_series" && p.MediaID == 77
				})).
				Return(&ent.Request{ID: 2, MediaType: "book_series", MediaID: 77}, nil).
				Once()

			resp := send(http.MethodPost, "/api/v1/requests", app.requestOnlyKey,
				`{"media_type":"book_series","media_id":77,"title":"S"}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})

		It("answers 409 duplicate", func() {
			app.requests.EXPECT().Create(mock.Anything, mock.Anything).
				Return(nil, requestsvc.ErrDuplicate).Once()

			resp := send(http.MethodPost, "/api/v1/requests", app.requestOnlyKey,
				`{"media_type":"book","media_id":5,"title":"B"}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("answers 400 for the removed album type", func() {
			resp := send(http.MethodPost, "/api/v1/requests", app.requestOnlyKey,
				`{"media_type":"album","media_mbid":"x","title":"A"}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("POST /requests/{id}/approve", func() {
		approve := func(body string) *http.Response {
			return send(
				http.MethodPost,
				"/api/v1/requests/4/approve",
				app.memberKey,
				body,
			)
		}

		It("approves with the reviewer's profile", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(4), app.memberID, "Retail").
				Return(&ent.Request{ID: 4, MediaType: "book", MediaID: 9}, nil).
				Once()

			resp := approve(`{"quality_profile":"Retail"}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It(
			"answers 422 for a profile outside the family and leaves the request pending",
			func() {
				app.requests.EXPECT().
					Approve(mock.Anything, uint32(4), app.memberID, "Lossless").
					Return(nil, fmt.Errorf("%w: %q", requestsvc.ErrUnknownProfile, "Lossless")).
					Once()

				resp := approve(`{"quality_profile":"Lossless"}`)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			},
		)

		It("answers 503 for a book when Hardcover is not configured", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(4), app.memberID, "").
				Return(nil, fmt.Errorf("approve: add book: %w", book.ErrNotConfigured)).
				Once()

			resp := approve("")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/requests/4/approve",
				app.requestOnlyKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("GET /requests/{id}/metadata", func() {
		It(
			"answers 403 to a request-only caller reading someone else's request",
			func() {
				app.requests.EXPECT().Get(mock.Anything, uint32(4)).
					Return(&ent.Request{
						ID: 4, MediaType: "artist",
						Edges: ent.RequestEdges{Requester: &ent.User{ID: 99}},
					}, nil).Once()

				resp := send(
					http.MethodGet,
					"/api/v1/requests/4/metadata",
					app.requestOnlyKey,
					"",
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			},
		)

		It("answers 404 for an unknown request", func() {
			app.requests.EXPECT().Get(mock.Anything, uint32(4)).
				Return(nil, &ent.NotFoundError{}).Once()

			resp := send(
				http.MethodGet,
				"/api/v1/requests/4/metadata",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})
