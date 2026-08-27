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
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/config"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// fakePassClient is a spy Client for RunSelectionPass specs, covering the
// calls Flow A's fakes don't need: GetTorrent, RemoveTorrent and
// ResumeTorrent — a hand-rolled fake instead of download/mocks for the same
// import-cycle reason as stubClient.
type fakePassClient struct {
	stubClient

	torrent       *Torrent
	getTorrentErr error

	listFilesResult []TorrentFile
	listFilesErr    error

	setWantedCalls int
	setWantedHash  string
	setWantedFiles []int
	setWantedErr   error

	resumeCalls int
	resumeHash  string

	removeCalls  int
	removeHash   string
	removeDelete bool
	removeErr    error
}

func (f *fakePassClient) GetTorrent(
	_ context.Context, _ string,
) (*Torrent, error) {
	return f.torrent, f.getTorrentErr
}

func (f *fakePassClient) ListFiles(
	_ context.Context, _ string,
) ([]TorrentFile, error) {
	return f.listFilesResult, f.listFilesErr
}

func (f *fakePassClient) SetWantedFiles(
	_ context.Context, hash string, wanted []int,
) error {
	f.setWantedCalls++
	f.setWantedHash = hash
	f.setWantedFiles = wanted
	return f.setWantedErr
}

func (f *fakePassClient) ResumeTorrent(_ context.Context, hash string) error {
	f.resumeCalls++
	f.resumeHash = hash
	return nil
}

func (f *fakePassClient) RemoveTorrent(
	_ context.Context, hash string, deleteFiles bool,
) error {
	f.removeCalls++
	f.removeHash = hash
	f.removeDelete = deleteFiles
	return f.removeErr
}

// passShow builds a two-episode show (S01E01 id=21, S01E02 id=22) with edges
// threaded all the way from the anchor episode back up to the show — the
// shape ListPendingSelectionRecords' WithEpisode(withEpisodeContext) eager
// load carries, so showForRecord never needs to fall back to
// TVShowForEpisode.
func passShow() *ent.Episode {
	e1 := &ent.Episode{ID: 21, Number: 1}
	e2 := &ent.Episode{ID: 22, Number: 2}
	season := &ent.Season{
		Number: 1,
		Edges:  ent.SeasonEdges{Episodes: []*ent.Episode{e1, e2}},
	}
	show := &ent.TVShow{
		ID:    1,
		Type:  tvshow.TypeStandard,
		Edges: ent.TVShowEdges{Seasons: []*ent.Season{season}},
	}
	season.Edges.TvShow = show
	return &ent.Episode{
		ID:     21,
		Number: 1,
		Edges:  ent.EpisodeEdges{Season: season},
	}
}

var _ = Describe("RunSelectionPass", Label("unit", "downloads"), func() {
	var (
		ctx    context.Context
		store  *dbmocks.MockStore
		client *fakePassClient
		mgr    *download
	)

	const hash = "abc123"

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		client = &fakePassClient{}
		mgr = New(store, client).(*download)
		configtest.Setup(map[string]any{
			"download_clients": []map[string]any{{
				"name": "embedded", "client_type": "builtin",
				"download_dir": "/downloads", "enabled": true,
			}},
		})
	})

	pendingRecord := func(anchor *ent.Episode, createdAt time.Time) *ent.DownloadRecord {
		return &ent.DownloadRecord{
			ID:                 42,
			TorrentHash:        hash,
			DownloadClientName: "embedded",
			Status:             downloadrecord.StatusDownloading,
			SelectionState:     downloadrecord.SelectionStatePending,
			WantedEpisodes:     []uint32{21},
			CreateTime:         createdAt,
			Edges:              ent.DownloadRecordEdges{Episode: anchor},
		}
	}

	It("metadata not yet available: record untouched, torrent untouched", func() {
		anchor := passShow()
		store.EXPECT().
			ListPendingSelectionRecords(mock.Anything).
			Return([]*ent.DownloadRecord{pendingRecord(anchor, time.Now())}, nil).
			Once()
		client.listFilesResult = nil

		err := mgr.RunSelectionPass(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(client.resumeCalls).To(Equal(0))
		Expect(client.removeCalls).To(Equal(0))
		Expect(client.setWantedCalls).To(Equal(0))
		// SetDownloadRecordSelection carries no expectation above: calling it
		// would panic the mock.
	})

	It("grace expired: skipped and resumed, torrent kept", func() {
		anchor := passShow()
		rec := pendingRecord(anchor, time.Now().Add(-11*time.Minute))
		store.EXPECT().
			ListPendingSelectionRecords(mock.Anything).
			Return([]*ent.DownloadRecord{rec}, nil).Once()
		client.listFilesResult = nil
		store.EXPECT().
			SetDownloadRecordSelection(
				mock.Anything, uint32(42), downloadrecord.SelectionStateSkipped,
				[]int(nil), int64(0),
			).
			Return(nil).Once()

		err := mgr.RunSelectionPass(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(client.resumeCalls).To(Equal(1))
		Expect(client.resumeHash).To(Equal(hash))
		Expect(client.removeCalls).To(Equal(0))
	})

	It(
		"ListFiles ErrNotSupported: unsupported and resumed immediately, "+
			"no grace wait",
		func() {
			anchor := passShow()
			rec := pendingRecord(anchor, time.Now())
			store.EXPECT().
				ListPendingSelectionRecords(mock.Anything).
				Return([]*ent.DownloadRecord{rec}, nil).Once()
			client.listFilesErr = ErrNotSupported
			store.EXPECT().
				SetDownloadRecordSelection(
					mock.Anything, uint32(42),
					downloadrecord.SelectionStateUnsupported, []int(nil), int64(0),
				).
				Return(nil).Once()

			err := mgr.RunSelectionPass(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(client.resumeCalls).To(Equal(1))
			Expect(client.resumeHash).To(Equal(hash))
			Expect(client.setWantedCalls).To(Equal(0))
			Expect(client.removeCalls).To(Equal(0))
		},
	)

	It(
		"ListFiles erroring within grace: record untouched, retried next tick",
		func() {
			anchor := passShow()
			rec := pendingRecord(anchor, time.Now())
			store.EXPECT().
				ListPendingSelectionRecords(mock.Anything).
				Return([]*ent.DownloadRecord{rec}, nil).Once()
			client.listFilesErr = errors.New("connection refused")

			err := mgr.RunSelectionPass(ctx)

			// RunSelectionPass swallows the per-record error and continues;
			// this only proves no terminal state was written — the mock
			// would panic on an unexpected SetDownloadRecordSelection call.
			Expect(err).NotTo(HaveOccurred())
			Expect(client.resumeCalls).To(Equal(0))
			Expect(client.removeCalls).To(Equal(0))
		},
	)

	It(
		"ListFiles erroring past grace: gives up exactly like an empty "+
			"listing — skipped and resumed, no torrent removal",
		func() {
			anchor := passShow()
			rec := pendingRecord(anchor, time.Now().Add(-11*time.Minute))
			store.EXPECT().
				ListPendingSelectionRecords(mock.Anything).
				Return([]*ent.DownloadRecord{rec}, nil).Once()
			client.listFilesErr = errors.New("torrent not found: removed hash")
			store.EXPECT().
				SetDownloadRecordSelection(
					mock.Anything, uint32(42), downloadrecord.SelectionStateSkipped,
					[]int(nil), int64(0),
				).
				Return(nil).Once()

			err := mgr.RunSelectionPass(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(client.resumeCalls).To(Equal(1))
			Expect(client.resumeHash).To(Equal(hash))
			Expect(client.removeCalls).To(Equal(0))
		},
	)

	It(
		"zero matches: selection flipped to skipped, record failed, "+
			"torrent removed with files, grab_failures bumped",
		func() {
			anchor := passShow()
			rec := pendingRecord(anchor, time.Now())
			rec.WantedEpisodes = []uint32{999} // no episode on the show
			store.EXPECT().
				ListPendingSelectionRecords(mock.Anything).
				Return([]*ent.DownloadRecord{rec}, nil).Once()
			client.listFilesResult = []TorrentFile{
				{Index: 0, Path: "Show.S01E01.mkv", Size: aboveFloor},
				{Index: 1, Path: "Show.S01E02.mkv", Size: aboveFloor},
			}
			// selection_state must leave "pending" so the record drops out of
			// the next ListPendingSelectionRecords pass — otherwise a failed
			// record with a now-removed hash re-enters every tick.
			store.EXPECT().
				SetDownloadRecordSelection(
					mock.Anything, uint32(42), downloadrecord.SelectionStateSkipped,
					[]int(nil), int64(0),
				).
				Return(nil).Once()
			store.EXPECT().
				FailDownloadRecord(mock.Anything, uint32(42), mock.MatchedBy(
					func(reason string) bool { return reason != "" },
				)).
				Return(nil).Once()
			store.EXPECT().
				IncrementEpisodeGrabFailures(mock.Anything, uint32(21)).
				Return(nil).Once()

			err := mgr.RunSelectionPass(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(client.removeCalls).To(Equal(1))
			Expect(client.removeHash).To(Equal(hash))
			Expect(client.removeDelete).To(BeTrue())
			Expect(client.resumeCalls).To(Equal(0))
			Expect(client.setWantedCalls).To(Equal(0))
		},
	)

	It(
		"zero matches: RemoveTorrent failing still leaves the record "+
			"terminal — the DB writes already landed",
		func() {
			anchor := passShow()
			rec := pendingRecord(anchor, time.Now())
			rec.WantedEpisodes = []uint32{999}
			store.EXPECT().
				ListPendingSelectionRecords(mock.Anything).
				Return([]*ent.DownloadRecord{rec}, nil).Once()
			client.listFilesResult = []TorrentFile{
				{Index: 0, Path: "Show.S01E01.mkv", Size: aboveFloor},
				{Index: 1, Path: "Show.S01E02.mkv", Size: aboveFloor},
			}
			client.removeErr = errors.New("client unreachable")
			store.EXPECT().
				SetDownloadRecordSelection(
					mock.Anything, uint32(42), downloadrecord.SelectionStateSkipped,
					[]int(nil), int64(0),
				).
				Return(nil).Once()
			store.EXPECT().
				FailDownloadRecord(mock.Anything, uint32(42), mock.Anything).
				Return(nil).Once()
			store.EXPECT().
				IncrementEpisodeGrabFailures(mock.Anything, uint32(21)).
				Return(nil).Once()

			err := mgr.RunSelectionPass(ctx)

			// RunSelectionPass swallows per-record errors (logs and
			// continues), so this only proves the DB calls above — the
			// mock would otherwise panic on an unexpected call — ran before
			// RemoveTorrent's failure was even reached.
			Expect(err).NotTo(HaveOccurred())
			Expect(client.removeCalls).To(Equal(1))
		},
	)

	It(
		"resolved: SetWantedFiles gets the keep-set, record applied, resumed",
		func() {
			anchor := passShow()
			rec := pendingRecord(anchor, time.Now())
			store.EXPECT().
				ListPendingSelectionRecords(mock.Anything).
				Return([]*ent.DownloadRecord{rec}, nil).Once()
			client.listFilesResult = []TorrentFile{
				{Index: 0, Path: "Show.S01E01.mkv", Size: aboveFloor},
				{Index: 1, Path: "Show.S01E02.mkv", Size: aboveFloor},
			}
			store.EXPECT().
				SetDownloadRecordSelection(
					mock.Anything, uint32(42), downloadrecord.SelectionStateApplied,
					[]int{0}, aboveFloor,
				).
				Return(nil).Once()

			err := mgr.RunSelectionPass(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(client.setWantedCalls).To(Equal(1))
			Expect(client.setWantedHash).To(Equal(hash))
			Expect(client.setWantedFiles).To(Equal([]int{0}))
			Expect(client.resumeCalls).To(Equal(1))
			Expect(client.resumeHash).To(Equal(hash))
			Expect(client.removeCalls).To(Equal(0))
		},
	)

	It("ErrNotSupported: record flips unsupported, still resumed", func() {
		anchor := passShow()
		rec := pendingRecord(anchor, time.Now())
		store.EXPECT().
			ListPendingSelectionRecords(mock.Anything).
			Return([]*ent.DownloadRecord{rec}, nil).Once()
		client.listFilesResult = []TorrentFile{
			{Index: 0, Path: "Show.S01E01.mkv", Size: aboveFloor},
			{Index: 1, Path: "Show.S01E02.mkv", Size: aboveFloor},
		}
		client.setWantedErr = ErrNotSupported
		store.EXPECT().
			SetDownloadRecordSelection(
				mock.Anything, uint32(42),
				downloadrecord.SelectionStateUnsupported, []int(nil), int64(0),
			).
			Return(nil).Once()

		err := mgr.RunSelectionPass(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(client.setWantedCalls).To(Equal(1))
		Expect(client.resumeCalls).To(Equal(1))
	})

	It(
		"a per-record failure is logged and does not stop the rest of the pass",
		func() {
			anchor1 := passShow()
			anchor2 := passShow()
			badRec := pendingRecord(anchor1, time.Now())
			badRec.ID = 1
			badRec.DownloadClientName = "missing-client"
			goodRec := pendingRecord(anchor2, time.Now())
			goodRec.ID = 2
			store.EXPECT().
				ListPendingSelectionRecords(mock.Anything).
				Return([]*ent.DownloadRecord{badRec, goodRec}, nil).Once()
			client.listFilesResult = []TorrentFile{
				{Index: 0, Path: "Show.S01E01.mkv", Size: aboveFloor},
				{Index: 1, Path: "Show.S01E02.mkv", Size: aboveFloor},
			}
			store.EXPECT().
				SetDownloadRecordSelection(
					mock.Anything, uint32(2), downloadrecord.SelectionStateApplied,
					[]int{0}, aboveFloor,
				).
				Return(nil).Once()

			err := mgr.RunSelectionPass(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(client.setWantedCalls).To(Equal(1))
			Expect(client.setWantedHash).To(Equal(hash))
		},
	)
})

var _ = Describe("SelectionGraceDuration", Label("unit", "downloads"), func() {
	It("parses a valid duration", func() {
		configtest.Setup(map[string]any{
			"download": map[string]any{"selection_grace": "20m"},
		})
		Expect(config.Get().Download.SelectionGraceDuration()).
			To(Equal(20 * time.Minute))
	})

	It("falls back to 10 minutes on an unparseable value", func() {
		configtest.Setup(map[string]any{
			"download": map[string]any{"selection_grace": "not-a-duration"},
		})
		Expect(config.Get().Download.SelectionGraceDuration()).
			To(Equal(10 * time.Minute))
	})
})
