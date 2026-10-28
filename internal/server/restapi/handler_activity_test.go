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

		It("carries the torrent hash lower-cased, omitting an empty one", func() {
			app.downloads.EXPECT().Queue(mock.Anything).Return(
				download.QueueSnapshot{
					RefreshedAt: time.Now(),
					Items: []download.QueueEntry{
						{
							RecordID:       1,
							Status:         "downloading",
							Title:          "rel",
							Movie:          &ent.Movie{ID: 2, Title: "Dune"},
							DownloadClient: "builtin",
							TorrentHash:    "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
						},
						{
							RecordID: 3, Status: "downloading", Title: "magnet",
							Movie: &ent.Movie{ID: 4, Title: "Arrival"},
						},
					},
				}, nil).Once()

			resp, err := http.Get(app.srv.URL + "/api/v1/activity/queue")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			var body DownloadQueue
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(2))
			Expect(body.Items[0].TorrentHash).To(HaveValue(
				Equal("abcdef0123456789abcdef0123456789abcdef01")))
			Expect(body.Items[1].TorrentHash).To(BeNil())
		})

		It("names the album, the pack's artist and the book a row is for", func() {
			album := &ent.Album{ID: 24, Title: "L'Enfant sauvage"}
			album.Edges.Artist = &ent.Artist{ID: 1, Name: "Gojira"}
			pos := 3.0
			volume := &ent.Book{
				ID: 8, Title: "Tome 3", AuthorName: "Goscinny",
				SeriesPosition: &pos,
			}
			volume.Edges.Series = &ent.BookSeries{ID: 2, Title: "Astérix"}
			app.downloads.EXPECT().Queue(mock.Anything).Return(
				download.QueueSnapshot{
					RefreshedAt: time.Now(),
					Items: []download.QueueEntry{
						{RecordID: 1, Status: "held", Title: "a", Album: album},
						{
							RecordID:     2,
							Status:       "downloading",
							Title:        "b",
							Artist:       &ent.Artist{ID: 1, Name: "Gojira"},
							PackAlbumIDs: []uint32{14, 35, 8},
						},
						{
							RecordID: 3,
							Status:   "downloading",
							Title:    "c",
							Book: &ent.Book{
								ID:         7,
								Title:      "Exhalation",
								AuthorName: "Ted Chiang",
							},
						},
						{
							RecordID: 4,
							Status:   "downloading",
							Title:    "d",
							Book:     volume,
							BookKind: "audiobook",
						},
					},
				}, nil).Once()

			resp, err := http.Get(app.srv.URL + "/api/v1/activity/queue")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body DownloadQueue
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(4))
			Expect(
				body.Items[0].Album,
			).To(Equal(&ActivityAlbumRef{Id: 24, Title: "L'Enfant sauvage", ArtistId: 1, ArtistName: "Gojira"}))
			Expect(body.Items[0].Artist).To(BeNil())
			Expect(
				body.Items[1].Artist,
			).To(Equal(&ActivityArtistRef{Id: 1, Name: "Gojira", AlbumCount: 3}))
			Expect(body.Items[1].Album).To(BeNil())
			Expect(
				body.Items[2].Book,
			).To(Equal(&ActivityBookRef{Id: 7, Title: "Exhalation", Author: "Ted Chiang"}))
			Expect(body.Items[2].Movie.Id).To(BeZero())
			slot := ActivityBookRefSlotAudiobook
			Expect(body.Items[3].Book).To(Equal(&ActivityBookRef{
				Id: 8, Title: "Tome 3", Author: "Goscinny", Slot: &slot,
				Series: &BookSeriesRef{Id: 2, Title: "Astérix", Number: 3},
			}))
		})

		It("carries selection fields for an applied selection", func() {
			app.downloads.EXPECT().Queue(mock.Anything).Return(
				download.QueueSnapshot{
					RefreshedAt: time.Now(),
					Items: []download.QueueEntry{{
						RecordID: 1, Status: "downloading", Title: "rel",
						Movie:          &ent.Movie{ID: 2, Title: "Dune"},
						Progress:       0.5,
						Size:           1000,
						SelectionState: string(downloadrecord.SelectionStateApplied),
						SelectedFiles:  []int{0, 2},
						SelectedBytes:  600,
					}},
				}, nil).Once()

			resp, err := http.Get(app.srv.URL + "/api/v1/activity/queue")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body DownloadQueue
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
			entry := body.Items[0]
			Expect(*entry.SelectionState).To(Equal(QueueEntrySelectionStateApplied))
			Expect(*entry.SelectedFilesCount).To(Equal(2))
			Expect(*entry.SelectedBytes).To(Equal(int64(600)))
			Expect(entry.Size).To(Equal(int64(1000)))
		})

		It("omits selection fields when the selection was skipped", func() {
			app.downloads.EXPECT().Queue(mock.Anything).Return(
				download.QueueSnapshot{
					RefreshedAt: time.Now(),
					Items: []download.QueueEntry{{
						RecordID: 1, Status: "downloading", Title: "rel",
						Movie:          &ent.Movie{ID: 2, Title: "Dune"},
						Progress:       0.5,
						Size:           1000,
						SelectionState: string(downloadrecord.SelectionStateSkipped),
					}},
				}, nil).Once()

			resp, err := http.Get(app.srv.URL + "/api/v1/activity/queue")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body DownloadQueue
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
			entry := body.Items[0]
			Expect(entry.SelectionState).To(BeNil())
			Expect(entry.SelectedFilesCount).To(BeNil())
			Expect(entry.SelectedBytes).To(BeNil())
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

		It("DELETE /activity/queue/{id} 409s a held record", func() {
			app.downloads.EXPECT().CancelQueueItem(mock.Anything, uint32(7)).
				Return(download.ErrRecordHeld).Once()
			req, _ := http.NewRequest(http.MethodDelete,
				app.srv.URL+"/api/v1/activity/queue/7", nil)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("POST /activity/queue/{id}/pause 409s a held record", func() {
			app.downloads.EXPECT().PauseQueueItem(mock.Anything, uint32(7)).
				Return(download.ErrRecordHeld).Once()
			resp, err := http.Post(
				app.srv.URL+"/api/v1/activity/queue/7/pause",
				"application/json", nil)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("POST /activity/queue/{id}/resume 409s a held record", func() {
			app.downloads.EXPECT().ResumeQueueItem(mock.Anything, uint32(7)).
				Return(download.ErrRecordHeld).Once()
			resp, err := http.Post(
				app.srv.URL+"/api/v1/activity/queue/7/resume",
				"application/json", nil)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("POST /activity/history/clear-completed returns the count", func() {
			app.downloads.EXPECT().SeedingTorrents(mock.Anything).
				Return(download.NewSeedingSet(true, nil, []string{"B", "a"})).Once()
			app.store.EXPECT().DeleteAllCompletedDownloadRecords(
				mock.Anything, []string{"a", "b"},
			).Return(4, nil).Once()
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

		It(
			"POST /activity/history/clear-completed clears nothing when a client cannot be listed",
			func() {
				app.downloads.EXPECT().SeedingTorrents(mock.Anything).
					Return(download.NewSeedingSet(false, nil, nil)).Once()
				resp, err := http.Post(
					app.srv.URL+"/api/v1/activity/history/clear-completed",
					"application/json", nil)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
			},
		)

		It("GET /activity/history maps records and status", func() {
			rec := &ent.DownloadRecord{
				ID: 1, Title: "rel", Status: "completed",
				Size: 10, CreateTime: time.Now(), UpdateTime: time.Now(),
				TorrentHash: "abcdef0123456789abcdef0123456789abcdef01",
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
			Expect(body.Items[0].TorrentHash).To(HaveValue(
				Equal("abcdef0123456789abcdef0123456789abcdef01")))
		})

		It("GET /activity/history counts a pack's albums", func() {
			rec := &ent.DownloadRecord{
				ID: 1, Title: "pack", Status: "completed",
				Size: 10, CreateTime: time.Now(), UpdateTime: time.Now(),
			}
			rec.Edges.Artist = &ent.Artist{ID: 1, Name: "Gojira"}
			rec.Edges.Albums = []*ent.Album{{ID: 14}, {ID: 35}}
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
			var body DownloadHistory
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].Artist).To(Equal(
				&ActivityArtistRef{Id: 1, Name: "Gojira", AlbumCount: 2}))
		})

		It("rejects a limit above the documented maximum", func() {
			resp, err := http.Get(
				app.srv.URL + "/api/v1/activity/history?limit=2147483647")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
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

		It("400s an unrecognised action instead of deleting", func() {
			Expect(post(5, "cancel").StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("400s a body with no action at all", func() {
			resp, err := http.Post(
				app.srv.URL+"/api/v1/downloads/6/resolve",
				"application/json", strings.NewReader(`{}`))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(resp.Body.Close)
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})
