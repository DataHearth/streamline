package download

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/config"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Manager", Label("unit", "downloads"), func() {
	var (
		ctx   context.Context
		store *dbmocks.MockStore
		mgr   Downloader
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		mgr = New(store, nil)
	})

	Describe("Grab", func() {
		When("no enabled download client exists", func() {
			It("returns the no-client error", func() {
				configtest.Setup()
				_, err := mgr.Grab(ctx, indexer.SearchResult{Title: "x"}, 1)
				Expect(
					err,
				).To(MatchError(ContainSubstring("no enabled download client")))
			})
		})
	})

	Describe("GrabEpisode", func() {
		When("no enabled download client exists", func() {
			It("returns the no-client error", func() {
				configtest.Setup()
				_, err := mgr.GrabEpisode(ctx, indexer.SearchResult{Title: "x"}, 1)
				Expect(
					err,
				).To(MatchError(ContainSubstring("no enabled download client")))
			})
		})
	})

	Describe("CheckStatus", func() {
		When("the store fails to list downloading records", func() {
			It("returns the wrapped error", func() {
				boom := errors.New("db boom")
				store.EXPECT().
					ListDownloadingRecordsWithMovie(mock.Anything).
					Return(nil, boom).Once()

				_, err := mgr.CheckStatus(ctx)
				Expect(err).To(MatchError(boom))
			})
		})

		When("there are no downloading records", func() {
			It("returns an empty slice without polling any client", func() {
				store.EXPECT().
					ListDownloadingRecordsWithMovie(mock.Anything).
					Return(nil, nil).Once()

				completed, err := mgr.CheckStatus(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(BeEmpty())
			})
		})

		When("a record references no known download client", func() {
			It("skips it and returns no completions", func() {
				configtest.Setup()
				store.EXPECT().
					ListDownloadingRecordsWithMovie(mock.Anything).
					Return([]*ent.DownloadRecord{
						{ID: 1, TorrentHash: "abc"},
					}, nil).Once()

				completed, err := mgr.CheckStatus(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(completed).To(BeEmpty())
			})
		})
	})

	Describe("RemoveTorrent", func() {
		When("the download client is unknown", func() {
			It("returns a not-found error", func() {
				configtest.Setup()
				Expect(
					mgr.RemoveTorrent(ctx, "ghost", "abc"),
				).To(MatchError(ContainSubstring("not found")))
			})
		})
	})

	Describe("Test", func() {
		When("the supplied client type is unsupported", func() {
			It("returns ErrUnsupportedClient", func() {
				// Free-form TestParams bypass config's oneof validation, so an
				// unknown type still reaches buildClient's default guard.
				err := mgr.Test(ctx, TestParams{
					ClientType: "rtorrent",
					Host:       "rt.local",
				})
				Expect(err).To(MatchError(ErrUnsupportedClient))
			})
		})
	})

	Describe("TestByName", func() {
		When("the entry is missing", func() {
			It("returns ErrDownloadClientNotFound", func() {
				configtest.Setup()
				Expect(mgr.TestByName(ctx, "ghost")).
					To(MatchError(ContainSubstring("not found")))
			})
		})
	})

	Describe("PurgeOldRecords", func() {
		It("returns nil when both deletes succeed with zero rows", func() {
			cleaner := mgr.(Cleaner)
			store.EXPECT().DeleteCompletedDownloadRecordsBefore(
				mock.Anything, mock.AnythingOfType("time.Time"),
			).Return(0, nil).Once()
			store.EXPECT().DeleteFailedDownloadRecordsBefore(
				mock.Anything, mock.AnythingOfType("time.Time"),
			).Return(0, nil).Once()
			Expect(cleaner.PurgeOldRecords(ctx)).To(Succeed())
		})

		It("passes the right cutoffs to each delete", func() {
			cleaner := mgr.(Cleaner)
			var compCutoff, failCutoff time.Time
			store.EXPECT().DeleteCompletedDownloadRecordsBefore(
				mock.Anything, mock.AnythingOfType("time.Time"),
			).Run(func(_ context.Context, c time.Time) { compCutoff = c }).
				Return(2, nil).Once()
			store.EXPECT().DeleteFailedDownloadRecordsBefore(
				mock.Anything, mock.AnythingOfType("time.Time"),
			).Run(func(_ context.Context, c time.Time) { failCutoff = c }).
				Return(1, nil).Once()

			Expect(cleaner.PurgeOldRecords(ctx)).To(Succeed())
			Expect(
				time.Since(compCutoff),
			).To(BeNumerically("~", completedRecordRetention, time.Second))
			Expect(
				time.Since(failCutoff),
			).To(BeNumerically("~", failedRecordRetention, time.Second))
		})

		It("joins errors when both deletes fail", func() {
			cleaner := mgr.(Cleaner)
			store.EXPECT().DeleteCompletedDownloadRecordsBefore(
				mock.Anything, mock.Anything,
			).Return(0, errors.New("comp")).Once()
			store.EXPECT().DeleteFailedDownloadRecordsBefore(
				mock.Anything, mock.Anything,
			).Return(0, errors.New("fail")).Once()
			err := cleaner.PurgeOldRecords(ctx)
			Expect(err).To(MatchError(ContainSubstring("comp")))
			Expect(err).To(MatchError(ContainSubstring("fail")))
		})
	})

	Describe("Queue", func() {
		It("wraps the store error", func() {
			boom := errors.New("db boom")
			store.EXPECT().ListActiveDownloadRecords(mock.Anything).
				Return(nil, boom).Once()
			_, err := mgr.Queue(ctx)
			Expect(err).To(MatchError(boom))
		})

		It("maps importing records to progress 1.0 without polling", func() {
			rec := &ent.DownloadRecord{
				ID: 7, Title: "Dune", Status: downloadrecord.StatusImporting,
			}
			rec.Edges.Movie = &ent.Movie{ID: 1, Title: "Dune"}
			store.EXPECT().ListActiveDownloadRecords(mock.Anything).
				Return([]*ent.DownloadRecord{rec}, nil).Once()
			snap, err := mgr.Queue(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(snap.Items).To(HaveLen(1))
			Expect(snap.Items[0].Status).To(Equal("importing"))
			Expect(snap.Items[0].Progress).To(Equal(1.0))
		})

		It("serves the cached snapshot within the TTL (one store call)", func() {
			// Two calls in immediate succession land inside the 2s TTL
			// window, so the store is queried exactly once.
			store.EXPECT().ListActiveDownloadRecords(mock.Anything).
				Return(nil, nil).Once()
			_, err := mgr.Queue(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = mgr.Queue(ctx)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("CancelQueueItem", func() {
		It("propagates NotFound when the record is absent", func() {
			store.EXPECT().
				FindActiveDownloadRecordByID(mock.Anything, uint32(9)).
				Return(nil, &ent.NotFoundError{}).Once()
			err := mgr.CancelQueueItem(ctx, 9)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})

		It("deletes the record and reverts the movie (no client edge)", func() {
			rec := &ent.DownloadRecord{
				ID: 3, Status: downloadrecord.StatusDownloading,
			}
			rec.Edges.Movie = &ent.Movie{ID: 5}
			store.EXPECT().
				FindActiveDownloadRecordByID(mock.Anything, uint32(3)).
				Return(rec, nil).Once()
			store.EXPECT().
				DeleteDownloadRecord(mock.Anything, uint32(3)).
				Return(nil).Once()
			store.EXPECT().
				RevertMovieToWantedIfNoFile(mock.Anything, uint32(5)).
				Return(nil).Once()
			Expect(mgr.CancelQueueItem(ctx, 3)).To(Succeed())
		})
	})

	Describe("PauseQueueItem", func() {
		It("propagates NotFound when the record is absent", func() {
			store.EXPECT().
				FindActiveDownloadRecordByID(mock.Anything, uint32(1)).
				Return(nil, &ent.NotFoundError{}).Once()
			Expect(ent.IsNotFound(mgr.PauseQueueItem(ctx, 1))).To(BeTrue())
		})
	})
})

// stubClient is a no-op Client used to assert buildClient returns the injected
// builtin engine by pointer identity. A local stub (rather than
// download/mocks) keeps this internal test package free of the import cycle
// download/mocks → download.
type stubClient struct{}

func (stubClient) AddTorrent(context.Context, TorrentSource) (string, error) {
	return "", nil
}

func (stubClient) GetTorrent(context.Context, string) (*Torrent, error) {
	return nil, nil
}

func (stubClient) ListTorrents(context.Context) ([]Torrent, error) {
	return nil, nil
}
func (stubClient) RemoveTorrent(context.Context, string, bool) error { return nil }
func (stubClient) PauseTorrent(context.Context, string) error        { return nil }
func (stubClient) ResumeTorrent(context.Context, string) error       { return nil }
func (stubClient) TestConnection(context.Context) error              { return nil }

var _ = Describe("buildClient builtin", Label("unit", "downloads"), func() {
	It("returns the injected engine", func() {
		engine := &stubClient{}
		d := New(nil, engine).(*download)
		c, err := d.buildClient(config.DownloadClientEntry{
			ClientType: "builtin", Name: "embedded",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c).To(BeIdenticalTo(engine))
	})

	It("errors when no engine is running", func() {
		d := New(nil, nil).(*download)
		_, err := d.buildClient(config.DownloadClientEntry{
			ClientType: "builtin", Name: "embedded",
		})
		Expect(err).To(MatchError(ErrUnsupportedClient))
		Expect(err.Error()).To(ContainSubstring("restart"))
	})
})
