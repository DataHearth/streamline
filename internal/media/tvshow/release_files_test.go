package tvshow

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/download"
	mockdownload "github.com/datahearth/streamline/internal/download/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
)

var _ = Describe("PackReleaseFiles", Label("unit", "series"), func() {
	var (
		ctx     context.Context
		storeMk *dbmocks.MockStore_Expecter
		dlMk    *mockdownload.MockDownloader_Expecter
		svc     *Service
	)

	const big = library.MinEpisodeSize + 1
	release := indexer.SearchResult{Title: "Show S01", Download: "https://ix/dl"}

	// Season 1: E1 on disk, E2 wanted and aired, E3 wanted but not aired yet.
	// Season 2: E1 wanted.
	show := func() *ent.TVShow {
		past := time.Now().Add(-24 * time.Hour)
		future := time.Now().Add(720 * time.Hour)
		return &ent.TVShow{ID: 3, Edges: ent.TVShowEdges{Seasons: []*ent.Season{
			{Number: 1, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{
					ID: 10, Number: 1, Title: "One", Status: episode.StatusAvailable,
					AirDate: past,
					Edges:   ent.EpisodeEdges{MediaFiles: []*ent.MediaFile{{ID: 1}}},
				},
				{
					ID:      11,
					Number:  2,
					Title:   "Two",
					Status:  episode.StatusWanted,
					AirDate: past,
				},
				{
					ID:      12,
					Number:  3,
					Title:   "Three",
					Status:  episode.StatusWanted,
					AirDate: future,
				},
			}}},
			{Number: 2, Edges: ent.SeasonEdges{Episodes: []*ent.Episode{
				{
					ID:      20,
					Number:  1,
					Title:   "Four",
					Status:  episode.StatusWanted,
					AirDate: past,
				},
			}}},
		}}}
	}
	files := []download.ReleaseFile{
		{Path: "Show.S01E01.1080p.mkv", Size: big},
		{Path: "Show.S01E02.1080p.mkv", Size: big},
		// A sample is never a candidate, as the selective keep-set reads it.
		{Path: "Sample/Show.S01E03.sample.mkv", Size: 1024},
		{Path: "Show.S02E01.1080p.mkv", Size: big},
		{Path: "Show.S01.nfo", Size: 10},
	}

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMk = store.EXPECT()
		dl := mockdownload.NewMockDownloader(GinkgoT())
		dlMk = dl.EXPECT()
		svc = NewService(store, nil, nil, dl, nil)
	})

	type row struct{ has, wanted, in bool }
	flags := func(v *PackFilesView) map[uint32]row {
		out := map[uint32]row{}
		for _, e := range v.Episodes {
			out[e.ID] = row{e.HasFile, e.Wanted, e.InRelease}
		}
		return out
	}

	It("maps a season pack onto the season's episodes", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(3)).Return(show(), nil).Once()
		dlMk.ReleaseFiles(mock.Anything, release).Return(files, nil).Once()

		season := uint16(1)
		v, err := svc.PackReleaseFiles(ctx, 3, &season, release)
		Expect(err).NotTo(HaveOccurred())
		Expect(flags(v)).To(Equal(map[uint32]row{
			10: {has: true, in: true},
			11: {wanted: true, in: true},
			12: {},
		}))
		ids := make([]uint32, len(v.Files))
		for i, f := range v.Files {
			ids[i] = f.EpisodeID
		}
		// The S02 file still names its episode, outside the listed scope.
		Expect(ids).To(Equal([]uint32{10, 11, 0, 20, 0}))
	})

	It("lists every episode of the show for a whole-series release", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(3)).Return(show(), nil).Once()
		dlMk.ReleaseFiles(mock.Anything, release).Return(files, nil).Once()

		v, err := svc.PackReleaseFiles(ctx, 3, nil, release)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Episodes).To(HaveLen(4))
		Expect(flags(v)[20]).To(Equal(row{wanted: true, in: true}))
		Expect(v.Episodes[3].Season).To(Equal(uint16(2)))
	})

	It("refuses a season the show does not have before asking the indexer", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(3)).Return(show(), nil).Once()
		season := uint16(9)
		_, err := svc.PackReleaseFiles(ctx, 3, &season, release)
		Expect(err).To(MatchError(ErrSeasonNotFound))
	})

	It("maps a missing show to ErrSeriesNotFound", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(3)).
			Return(nil, &ent.NotFoundError{}).Once()
		_, err := svc.PackReleaseFiles(ctx, 3, nil, release)
		Expect(err).To(MatchError(ErrSeriesNotFound))
	})

	It("passes the download manager's refusal through", func() {
		storeMk.FindTVShowByID(mock.Anything, uint32(3)).Return(show(), nil).Once()
		dlMk.ReleaseFiles(mock.Anything, release).
			Return(nil, download.ErrFileListUnavailable).Once()
		_, err := svc.PackReleaseFiles(ctx, 3, nil, release)
		Expect(err).To(MatchError(download.ErrFileListUnavailable))
	})
})
