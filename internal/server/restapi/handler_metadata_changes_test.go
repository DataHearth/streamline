package restapi

import (
	"encoding/json"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/metadatachange"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe("Handler: MetadataChanges", Label("unit", "server"), func() {
	var app *apiKeyApp

	BeforeEach(func() { app = newAPIKeyApp() })

	errorOf := func(resp *http.Response) Error {
		GinkgoHelper()
		defer resp.Body.Close()
		var e Error
		Expect(json.NewDecoder(resp.Body).Decode(&e)).To(Succeed())
		return e
	}

	It("is for admins only", func() {
		app.addMember("member")
		for _, path := range []string{"/api/v1/metadata-changes", "/api/v1/metadata-changes/counts"} {
			resp := app.do(app.req(http.MethodGet, path, app.memberKey, nil))
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden), path)
		}
	})

	It("lists an album's change under its artist, filtered to music", func() {
		at := time.Date(2026, 11, 25, 7, 34, 0, 0, time.UTC)
		app.store.EXPECT().
			ListChanges(mock.Anything, db.ChangeFilter{
				Types: []db.ChangeOwnerType{
					db.ChangeOwnerArtist,
					db.ChangeOwnerAlbum,
				},
			}, uint32(50), uint32(0)).
			Return([]*ent.MetadataChange{{
				ID: 4, Kind: metadatachange.KindEditionPreferred, UpdateTime: at,
				Payload: map[string]any{"files": float64(7)},
				Edges: ent.MetadataChangeEdges{Album: &ent.Album{
					ID:    9,
					Title: "Train of Thought",
					Edges: ent.AlbumEdges{
						Artist: &ent.Artist{ID: 3, Name: "Dream Theater"},
					},
				}},
			}}, uint32(1), nil).Once()

		resp := app.do(
			app.req(
				http.MethodGet,
				"/api/v1/metadata-changes?media=music",
				app.adminKey,
				nil,
			),
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var body MetadataChangeList
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		Expect(body.Total).To(BeNumerically("==", 1))
		Expect(body.Items[0].Kind).To(Equal(MetadataChangeKind("edition_preferred")))
		Expect(body.Items[0].Owner.Type).To(Equal(MetadataChangeOwnerTypeAlbum))
		Expect(*body.Items[0].Owner.ArtistId).To(BeNumerically("==", 3))
		Expect(*body.Items[0].Owner.ArtistName).To(Equal("Dream Theater"))
		Expect(
			body.Items[0].Details,
		).To(HaveKeyWithValue("files", BeNumerically("==", 7)))
		Expect(body.Items[0].DetectedAt).To(BeTemporally("==", at))
	})

	It("refuses an unknown kind", func() {
		resp := app.do(
			app.req(
				http.MethodGet,
				"/api/v1/metadata-changes?kind=nope",
				app.adminKey,
				nil,
			),
		)
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("sums artists and albums into music", func() {
		app.store.EXPECT().CountChanges(mock.Anything).Return(db.ChangeCounts{
			Total: 3,
			ByType: map[db.ChangeOwnerType]uint32{
				db.ChangeOwnerArtist: 1,
				db.ChangeOwnerAlbum:  1,
				db.ChangeOwnerMovie:  1,
			},
			ByKind: map[metadatachange.Kind]uint32{
				metadatachange.KindMovieRemoved: 1,
			},
		}, nil).Once()

		resp := app.do(
			app.req(
				http.MethodGet,
				"/api/v1/metadata-changes/counts",
				app.adminKey,
				nil,
			),
		)
		defer resp.Body.Close()
		var body MetadataChangeCounts
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		Expect(body.Music).To(BeNumerically("==", 2))
		Expect(body.Movies).To(BeNumerically("==", 1))
		Expect(
			body.Kinds,
		).To(HaveKeyWithValue("movie_removed", BeNumerically("==", 1)))
	})

	It("answers 404 when there is no pending change to keep", func() {
		app.store.EXPECT().
			KeepChange(mock.Anything, uint32(7)).
			Return(&ent.NotFoundError{}).
			Once()
		resp := app.do(
			app.req(
				http.MethodPost,
				"/api/v1/metadata-changes/7/keep",
				app.adminKey,
				nil,
			),
		)
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})
	Describe("follow", func() {
		albumChange := func(state metadatachange.State) *ent.MetadataChange {
			return &ent.MetadataChange{
				ID: 5, Kind: metadatachange.KindEditionPreferred, State: state,
				Edges: ent.MetadataChangeEdges{Album: &ent.Album{ID: 9}},
			}
		}

		It("hands an album's change to the music service", func() {
			c := albumChange(metadatachange.StatePending)
			app.store.EXPECT().
				FindChange(mock.Anything, uint32(5)).
				Return(c, nil).
				Once()
			app.music.EXPECT().FollowChange(mock.Anything, c).Return(nil).Once()
			resp := app.do(
				app.req(
					http.MethodPost,
					"/api/v1/metadata-changes/5/follow",
					app.adminKey,
					nil,
				),
			)
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It(
			"answers 409 not_followable for a change that only removes the item",
			func() {
				c := albumChange(metadatachange.StatePending)
				app.store.EXPECT().
					FindChange(mock.Anything, uint32(5)).
					Return(c, nil).
					Once()
				app.music.EXPECT().
					FollowChange(mock.Anything, c).
					Return(db.ErrNotFollowable).
					Once()
				resp := app.do(
					app.req(
						http.MethodPost,
						"/api/v1/metadata-changes/5/follow",
						app.adminKey,
						nil,
					),
				)
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
				Expect(*errorOf(resp).Code).To(Equal("not_followable"))
			},
		)

		It("answers 404 for a change already kept", func() {
			app.store.EXPECT().FindChange(mock.Anything, uint32(5)).
				Return(albumChange(metadatachange.StateKept), nil).Once()
			resp := app.do(
				app.req(
					http.MethodPost,
					"/api/v1/metadata-changes/5/follow",
					app.adminKey,
					nil,
				),
			)
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})
