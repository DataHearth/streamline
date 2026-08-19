package restapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
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

		It("clamps a limit above the documented maximum", func() {
			app.store.EXPECT().ListDownloadHistory(
				mock.Anything, activityMaxLimit,
				mock.AnythingOfType("string")).
				Return(&db.DownloadHistoryResult{}, nil).Once()
			resp, err := http.Get(
				app.srv.URL + "/api/v1/activity/history?limit=2147483647")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

var _ = Describe("Handler: resolve held downloads",
	Label("unit", "server", "activity"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		held := func(id uint32) *ent.DownloadRecord {
			return &ent.DownloadRecord{
				ID:                 id,
				Status:             downloadrecord.StatusHeld,
				TorrentHash:        "H",
				DownloadClientName: "qb",
			}
		}

		post := func(id uint32, action string) *http.Response {
			GinkgoHelper()
			body := strings.NewReader(`{"action":"` + action + `"}`)
			resp, err := http.Post(
				fmt.Sprintf("%s/api/v1/downloads/%d/resolve", app.srv.URL, id),
				"application/json", body)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(resp.Body.Close)
			return resp
		}

		It("import releases the record and re-enqueues it", func() {
			app.store.EXPECT().FindDownloadRecordByID(mock.Anything, uint32(1)).
				Return(held(1), nil).Once()
			app.store.EXPECT().
				ReleaseHeldDownloadRecord(mock.Anything, uint32(1)).
				Return(nil).Once()
			app.importer.EXPECT().Enqueue(uint32(1)).Once()

			Expect(post(1, "import").StatusCode).To(Equal(http.StatusNoContent))
		})

		It("regrab deletes the torrent with its data and requeues", func() {
			app.store.EXPECT().FindDownloadRecordByID(mock.Anything, uint32(2)).
				Return(held(2), nil).Once()
			app.downloads.EXPECT().
				RemoveTorrent(mock.Anything, "qb", "H", true).Return(nil).Once()
			app.store.EXPECT().
				FailHeldDownloadRecord(mock.Anything, uint32(2), mock.Anything, true).
				Return(nil).Once()

			Expect(post(2, "regrab").StatusCode).To(Equal(http.StatusNoContent))
		})

		It("delete removes the torrent without requeueing", func() {
			app.store.EXPECT().FindDownloadRecordByID(mock.Anything, uint32(3)).
				Return(held(3), nil).Once()
			app.downloads.EXPECT().
				RemoveTorrent(mock.Anything, "qb", "H", true).Return(nil).Once()
			app.store.EXPECT().
				FailHeldDownloadRecord(mock.Anything, uint32(3), mock.Anything, false).
				Return(nil).Once()

			Expect(post(3, "delete").StatusCode).To(Equal(http.StatusNoContent))
		})

		It("409s a record that is not held", func() {
			rec := held(4)
			rec.Status = downloadrecord.StatusCompleted
			app.store.EXPECT().FindDownloadRecordByID(mock.Anything, uint32(4)).
				Return(rec, nil).Once()

			Expect(post(4, "import").StatusCode).To(Equal(http.StatusConflict))
		})

		It("404s an unknown record", func() {
			app.store.EXPECT().FindDownloadRecordByID(mock.Anything, uint32(9)).
				Return(nil, &ent.NotFoundError{}).Once()

			Expect(post(9, "import").StatusCode).To(Equal(http.StatusNotFound))
		})
	})
