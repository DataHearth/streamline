package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	moviesvc "github.com/datahearth/streamline/internal/media/movie"
)

var _ = Describe("Handler: Pending", Label("unit", "server", "activity"), func() {
	var app *apiKeyApp

	BeforeEach(func() { app = newAPIKeyApp() })

	Describe("ListPending", func() {
		It("returns pending items with mapped media", func() {
			app.store.EXPECT().ListPendingDownloadRecords(mock.Anything).
				Return([]*ent.DownloadRecord{
					{
						ID: 1, Title: "The Batman 2022 720p", Quality: "720p",
						FailureReason: `resolution "720p" below minimum "1080p"`,
						Edges: ent.DownloadRecordEdges{
							Movie: &ent.Movie{
								ID: 3, Title: "The Batman", Year: 2022,
							}, // no media files -> has_file false
						},
					},
					{
						ID: 2, Title: "The Batman 2022 2160p", Quality: "2160p",
						FailureReason: "already have a file",
						Edges: ent.DownloadRecordEdges{
							Movie: &ent.Movie{
								ID: 3, Title: "The Batman", Year: 2022,
								Edges: ent.MovieEdges{
									MediaFiles: []*ent.MediaFile{{ID: 9}},
								},
							}, // has a media file -> has_file true
						},
					},
				}, nil).Once()

			resp := app.do(app.req(
				http.MethodGet, "/api/v1/activity/pending", app.adminKey, nil,
			))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body PendingList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(2))
			Expect(body.Items[0].Id).To(Equal(uint32(1)))
			Expect(body.Items[0].Reason).To(ContainSubstring("below minimum"))
			Expect(body.Items[0].HasFile).To(BeFalse())
			Expect(body.Items[0].Media).NotTo(BeNil())
			Expect(body.Items[0].Media.Type).To(Equal(PendingMediaTypeMovie))
			Expect(body.Items[0].Media.Title).To(Equal("The Batman"))
			Expect(body.Items[1].HasFile).To(BeTrue())
		})
	})

	Describe("IdentifyPending", func() {
		// A pack for a show the library has never heard of: the shape the
		// adoption pass now files instead of dropping.
		unidentified := func() *ent.DownloadRecord {
			return &ent.DownloadRecord{
				ID:    1,
				Title: "Good.Omens.S03.MULTi.VF2.1080p.WEB.H264-FW",
			}
		}
		goodOmens := func() *ent.TVShow {
			ep := &ent.Episode{ID: 501, Number: 1}
			return &ent.TVShow{
				ID: 40, Title: "Good Omens", TvdbID: 359569,
				Edges: ent.TVShowEdges{Seasons: []*ent.Season{{
					Number: 3,
					Edges:  ent.SeasonEdges{Episodes: []*ent.Episode{ep}},
				}}},
			}
		}
		identify := func(body string) *http.Response {
			GinkgoHelper()
			req := app.req(
				http.MethodPost, "/api/v1/activity/pending/1/identify",
				app.adminKey, strings.NewReader(body),
			)
			req.Header.Set("Content-Type", "application/json")
			return app.do(req)
		}

		It("adds the series then links the proposal to it", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(unidentified(), nil).Once()
			app.store.EXPECT().
				FindTVShowByTVDBID(mock.Anything, uint32(359569)).
				Return(nil, nil).Once()
			app.tvshows.EXPECT().
				Add(mock.Anything, uint32(359569), "").
				Return(&ent.TVShow{ID: 40}, nil).Once()
			app.tvshows.EXPECT().
				Get(mock.Anything, uint32(40)).Return(goodOmens(), nil).Once()
			app.store.EXPECT().
				IdentifyDownloadRecord(
					mock.Anything, uint32(1), uint32(0), uint32(501),
					mock.AnythingOfType("string"),
				).Return(nil).Once()

			resp := identify(`{"kind":"series","provider_id":359569}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		// The show is already tracked — adding it again would collide on the
		// unique tvdb_id, so the existing row is reused.
		It("reuses a series already in the library", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(unidentified(), nil).Once()
			app.store.EXPECT().
				FindTVShowByTVDBID(mock.Anything, uint32(359569)).
				Return(&ent.TVShow{ID: 40}, nil).Once()
			app.tvshows.EXPECT().
				Get(mock.Anything, uint32(40)).Return(goodOmens(), nil).Once()
			app.store.EXPECT().
				IdentifyDownloadRecord(
					mock.Anything, uint32(1), uint32(0), uint32(501),
					mock.AnythingOfType("string"),
				).Return(nil).Once()

			resp := identify(`{"kind":"series","provider_id":359569}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("adds the movie then links the proposal to it", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(&ent.DownloadRecord{
					ID: 1, Title: "The.Batman.2022.1080p.WEB-GRP",
				}, nil).Once()
			// GetByTMDBID reports "not in the library" as a nil row with a nil
			// error, exactly like FindTVShowByTVDBID. Mocking it as an ent
			// NotFound is what let a nil deref through a green suite once.
			app.movies.EXPECT().
				GetByTMDBID(mock.Anything, uint32(414906)).
				Return(nil, nil).Once()
			app.movies.EXPECT().
				Add(mock.Anything, uint32(414906), "").
				Return(&ent.Movie{ID: 3}, "", nil).Once()
			app.store.EXPECT().
				IdentifyDownloadRecord(
					mock.Anything, uint32(1), uint32(3), uint32(0),
					mock.AnythingOfType("string"),
				).Return(nil).Once()

			resp := identify(`{"kind":"movie","provider_id":414906}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("409s when the proposal already carries media", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(&ent.DownloadRecord{
					ID: 1,
					Edges: ent.DownloadRecordEdges{
						Movie: &ent.Movie{ID: 3},
					},
				}, nil).Once()

			resp := identify(`{"kind":"movie","provider_id":414906}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		// The operator picked a real show, but it has no season 3 — naming the
		// wrong title is the likely cause, so say which season was looked for
		// rather than linking the pack somewhere arbitrary.
		It("422s when the chosen series has no matching episode", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(unidentified(), nil).Once()
			app.store.EXPECT().
				FindTVShowByTVDBID(mock.Anything, uint32(359569)).
				Return(&ent.TVShow{ID: 40}, nil).Once()
			app.tvshows.EXPECT().
				Get(mock.Anything, uint32(40)).
				Return(&ent.TVShow{ID: 40, Title: "Good Omens"}, nil).Once()

			resp := identify(`{"kind":"series","provider_id":359569}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))

			var body Error
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Message).To(ContainSubstring("season 3"))
		})

		It("403s for a non-admin", func() {
			app.addMember("m@test.com")
			req := app.req(
				http.MethodPost, "/api/v1/activity/pending/1/identify",
				app.memberKey, strings.NewReader(
					`{"kind":"movie","provider_id":1}`,
				),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("ImportPending", func() {
		It("flips the record to importing and returns 204", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(&ent.DownloadRecord{
					ID:    1,
					Edges: ent.DownloadRecordEdges{Movie: &ent.Movie{ID: 3}},
				}, nil).Once()
			app.store.EXPECT().
				UpdateDownloadRecordStatus(mock.Anything, uint32(1),
					downloadrecord.StatusImporting).
				Return(nil).Once()

			resp := app.do(app.req(
				http.MethodPost, "/api/v1/activity/pending/1/import",
				app.adminKey, nil,
			))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("404s when the record is absent", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(9)).
				Return(nil, &ent.NotFoundError{}).Once()

			resp := app.do(app.req(
				http.MethodPost, "/api/v1/activity/pending/9/import",
				app.adminKey, nil,
			))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("403s for a non-admin", func() {
			app.addMember("m@test.com")
			resp := app.do(app.req(
				http.MethodPost, "/api/v1/activity/pending/1/import",
				app.memberKey, nil,
			))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("IgnorePending", func() {
		It("dismisses and removes the torrent when asked", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(&ent.DownloadRecord{
					ID: 1, TorrentHash: "H", DownloadClientName: "qb",
				}, nil).Once()
			app.store.EXPECT().
				UpdateDownloadRecordStatus(mock.Anything, uint32(1),
					downloadrecord.StatusDismissed).
				Return(nil).Once()
			app.downloads.EXPECT().
				RemoveTorrent(mock.Anything, "qb", "H", false).Return(nil).Once()

			req := app.req(
				http.MethodPost, "/api/v1/activity/pending/1/ignore",
				app.adminKey, strings.NewReader(`{"remove_torrent":true}`),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})
	})

	Describe("ReplacePending", func() {
		It("deletes the existing movie file then flips to importing", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(&ent.DownloadRecord{
					ID: 1, TorrentHash: "NEW",
					Edges: ent.DownloadRecordEdges{Movie: &ent.Movie{ID: 3}},
				}, nil).Once()
			app.store.EXPECT().
				ListMediaFilesByMovieID(mock.Anything, uint32(3)).
				Return([]*ent.MediaFile{{ID: 7}}, nil).Once()
			app.movies.EXPECT().
				DeleteFile(mock.Anything, uint32(3), uint32(7),
					moviesvc.DeleteFileOptions{}).
				Return(nil).Once()
			app.store.EXPECT().
				UpdateDownloadRecordStatus(mock.Anything, uint32(1),
					downloadrecord.StatusImporting).
				Return(nil).Once()

			req := app.req(
				http.MethodPost, "/api/v1/activity/pending/1/replace",
				app.adminKey, strings.NewReader(`{"remove_old_torrent":false}`),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})
	})
})
