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

	Describe("ImportPending", func() {
		It("flips the record to importing and returns 204", func() {
			app.store.EXPECT().
				FindPendingDownloadRecordByID(mock.Anything, uint32(1)).
				Return(&ent.DownloadRecord{ID: 1}, nil).Once()
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
				RemoveTorrent(mock.Anything, "qb", "H").Return(nil).Once()

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
