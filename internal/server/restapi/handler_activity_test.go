package restapi

import (
	"encoding/json"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
)

var _ = Describe("Handler: Activity queue/history",
	Label("unit", "server", "activity"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		It("GET /activity/queue returns the snapshot", func() {
			app.downloads.EXPECT().Queue(mock.Anything).Return(
				download.QueueSnapshot{
					RefreshedAt: time.Now(),
					Items: []download.QueueEntry{{
						RecordID: 1, Status: "downloading", Title: "rel",
						Movie:    &ent.Movie{ID: 2, Title: "Dune"},
						Progress: 0.5, Size: 100,
					}},
				}, nil).Once()

			resp, err := http.Get(app.srv.URL + "/api/v1/activity/queue")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body DownloadQueue
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].Movie.Title).To(Equal("Dune"))
			Expect(body.Items[0].Status).To(Equal(QueueEntryStatus("downloading")))
		})

		It("DELETE /activity/queue/{id} 404s when not found", func() {
			app.downloads.EXPECT().CancelQueueItem(mock.Anything, uint32(9)).
				Return(&ent.NotFoundError{}).Once()
			req, _ := http.NewRequest(http.MethodDelete,
				app.srv.URL+"/api/v1/activity/queue/9", nil)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("POST /activity/queue/{id}/pause returns 204", func() {
			app.downloads.EXPECT().PauseQueueItem(mock.Anything, uint32(3)).
				Return(nil).Once()
			resp, err := http.Post(
				app.srv.URL+"/api/v1/activity/queue/3/pause",
				"application/json", nil)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("POST /activity/history/clear-completed returns the count", func() {
			app.store.EXPECT().DeleteAllCompletedDownloadRecords(mock.Anything).
				Return(4, nil).Once()
			resp, err := http.Post(
				app.srv.URL+"/api/v1/activity/history/clear-completed",
				"application/json", nil)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body ClearCompletedResult
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Deleted).To(Equal(4))
		})

		It("GET /activity/history maps records and status", func() {
			rec := &ent.DownloadRecord{
				ID: 1, Title: "rel", Status: "completed",
				Size: 10, CreateTime: time.Now(), UpdateTime: time.Now(),
			}
			rec.Edges.Movie = &ent.Movie{ID: 2, Title: "Dune"}
			app.store.EXPECT().ListDownloadHistory(
				mock.Anything, mock.AnythingOfType("int"),
				mock.AnythingOfType("string")).
				Return(&db.DownloadHistoryResult{
					Records: []*ent.DownloadRecord{rec},
				}, nil).Once()
			resp, err := http.Get(
				app.srv.URL + "/api/v1/activity/history?limit=20")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body DownloadHistory
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].Status).To(
				Equal(HistoryEntryStatus("completed")))
		})
	})
