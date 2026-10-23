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
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/metadata"
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
				"/api/v1/requests?media_type=book,author", app.adminKey, "")
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

		It("creates an album request with the artist hints", func() {
			app.requests.EXPECT().
				Create(mock.Anything, mock.MatchedBy(func(p requestsvc.CreateParams) bool {
					return p.MediaType == "album" && p.MediaMBID == "rg-1" &&
						p.ArtistMBID == "a-1" && p.ArtistName == "Nirvana" &&
						p.MediaID == 0
				})).
				Return(&ent.Request{
					ID: 2, MediaType: "album", MediaMbid: "rg-1", Title: "Nevermind",
					ArtistMbid: "a-1", ArtistName: "Nirvana", RequestedAs: "Nirvana",
				}, nil).Once()

			resp := send(
				http.MethodPost,
				"/api/v1/requests",
				app.requestOnlyKey,
				`{"media_type":"album","media_mbid":"rg-1","artist_mbid":"a-1","artist_name":"Nirvana","title":"Nevermind"}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var got Request
			Expect(json.NewDecoder(resp.Body).Decode(&got)).To(Succeed())
			Expect(got.MediaType).To(Equal(RequestMediaType("album")))
			Expect(got.ArtistMbid).To(HaveValue(Equal("a-1")))
			Expect(got.ArtistName).To(HaveValue(Equal("Nirvana")))
			Expect(got.RequestedAs).To(HaveValue(Equal("Nirvana")))
		})

		It("answers 400 for an invalid album identity", func() {
			app.requests.EXPECT().Create(mock.Anything, mock.Anything).
				Return(nil, fmt.Errorf("%w: album needs an artist_name", requestsvc.ErrInvalidRequest)).
				Once()

			resp := send(http.MethodPost, "/api/v1/requests", app.requestOnlyKey,
				`{"media_type":"album","media_mbid":"rg-1","title":"A"}`)
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

		It("answers 422 album_not_found and leaves the request pending", func() {
			app.requests.EXPECT().
				Approve(mock.Anything, uint32(4), app.memberID, "").
				Return(nil, fmt.Errorf("%w: rg-1", requestsvc.ErrAlbumNotFound)).
				Once()

			resp := approve("")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			var body Error
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Code).To(HaveValue(Equal("album_not_found")))
		})

		DescribeTable(
			"answers 503 with a code when Hardcover cannot serve the add",
			func(cause error, code string) {
				app.requests.EXPECT().
					Approve(mock.Anything, uint32(4), app.memberID, "").
					Return(nil, fmt.Errorf("approve: add book: %w", cause)).
					Once()

				resp := approve("")
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
				var body Error
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Code).To(HaveValue(Equal(code)))
			},
			Entry("no key", book.ErrNotConfigured, "hardcover_not_configured"),
			Entry(
				"key refused",
				metadata.ErrHardcoverUnauthorized,
				"hardcover_key_rejected",
			),
		)

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

		It("describes the artist MusicBrainz names, never the stored hint", func() {
			app.requests.EXPECT().Get(mock.Anything, uint32(4)).
				Return(&ent.Request{
					ID: 4, MediaType: "album", MediaMbid: "rg-1",
					ArtistMbid: "liar-mbid", ArtistName: "Liar",
				}, nil).Once()
			app.metadataMusic.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
				Return(&metadata.ReleaseGroupDetails{
					ArtistMBID: "real-mbid", ArtistName: "Nirvana",
				}, nil).Once()
			app.music.EXPECT().LookupArtist(mock.Anything, "real-mbid", "en").
				Return(&music.LookupDetail{
					MBID: "real-mbid",
					Name: "Nirvana",
				}, nil).Once()

			resp := send(
				http.MethodGet,
				"/api/v1/requests/4/metadata",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body MusicArtistLookupDetail
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Mbid).To(Equal("real-mbid"))
		})

		It("answers 404 for an album MusicBrainz does not know", func() {
			app.requests.EXPECT().Get(mock.Anything, uint32(4)).
				Return(&ent.Request{ID: 4, MediaType: "album", MediaMbid: "rg-1"}, nil).
				Once()
			app.metadataMusic.EXPECT().GetReleaseGroup(mock.Anything, "rg-1").
				Return(nil, metadata.ErrNotFound).Once()

			resp := send(
				http.MethodGet,
				"/api/v1/requests/4/metadata",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

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
