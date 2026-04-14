package rss

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
)

var _ = Describe("EpisodeMissingSearcher.Run", Label("unit", "rss"), func() {
	var (
		ctx      context.Context
		store    *dbmocks.MockStore
		indexerM *mocks.MockTVIndexerSearcher
		dlM      *mocks.MockEpisodeGrabber
		searcher *EpisodeMissingSearcher
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		indexerM = mocks.NewMockTVIndexerSearcher(GinkgoT())
		dlM = mocks.NewMockEpisodeGrabber(GinkgoT())

		s, err := NewEpisodeMissingSearcher(store, indexerM, dlM)
		Expect(err).NotTo(HaveOccurred())
		searcher = s
	})

	// showWith builds a wanted show with one season carrying the given episodes.
	showWith := func(eps ...*ent.Episode) *ent.TVShow {
		show := &ent.TVShow{ID: 1, Title: "The Black Sea", TvdbID: 9001}
		se := &ent.Season{ID: 5, Number: 3}
		se.Edges.Episodes = eps
		show.Edges.Seasons = []*ent.Season{se}
		return show
	}

	const acceptablePack = "The.Black.Sea.S03.1080p.WEB-DL.x265-GRP"
	const acceptableEp = "The.Black.Sea.S03E01.1080p.WEB-DL.x265-GRP"

	When("a whole season is wanted", func() {
		It(
			"prefers a season pack, grabs it once, marks all episodes downloading",
			func() {
				ep1 := &ent.Episode{ID: 11, Number: 1}
				ep2 := &ent.Episode{ID: 12, Number: 2}
				store.EXPECT().ListWantedEpisodes(mock.Anything).
					Return([]*ent.TVShow{showWith(ep1, ep2)}, nil).Once()

				indexerM.EXPECT().
					SearchSeason(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3)).
					Return([]indexer.SearchResult{{Title: acceptablePack, Seeders: 10}}, nil).
					Once()

				// Grabbed once, against the first wanted episode.
				dlM.EXPECT().
					GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"), uint32(11)).
					Return(&ent.DownloadRecord{}, nil).Once()

				// Every wanted episode flipped to downloading + stamped.
				store.EXPECT().
					SetEpisodeStatus(mock.Anything, uint32(11), episode.StatusDownloading).
					Return(nil).Once()
				store.EXPECT().
					SetEpisodeStatus(mock.Anything, uint32(12), episode.StatusDownloading).
					Return(nil).Once()
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, uint32(11), mock.AnythingOfType("time.Time")).
					Return(nil).Once()
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, uint32(12), mock.AnythingOfType("time.Time")).
					Return(nil).Once()

				Expect(searcher.Run(ctx)).To(Succeed())
			},
		)
	})

	When("only one episode of a season is wanted", func() {
		It("skips the pack and grabs the single episode", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			store.EXPECT().ListWantedEpisodes(mock.Anything).
				Return([]*ent.TVShow{showWith(ep1)}, nil).Once()

			indexerM.EXPECT().
				SearchEpisode(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3), uint16(1)).
				Return([]indexer.SearchResult{{Title: acceptableEp, Seeders: 10}}, nil).
				Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"), uint32(11)).
				Return(&ent.DownloadRecord{}, nil).Once()
			store.EXPECT().
				SetEpisodeStatus(mock.Anything, uint32(11), episode.StatusDownloading).
				Return(nil).Once()
			store.EXPECT().
				ResetEpisodeGrabFailures(mock.Anything, uint32(11)).
				Return(nil).Once()
			store.EXPECT().
				SetEpisodeLastSearchAt(mock.Anything, uint32(11), mock.AnythingOfType("time.Time")).
				Return(nil).Once()

			Expect(searcher.Run(ctx)).To(Succeed())
		})
	})

	When("the only release fails the quality bar", func() {
		It("grabs nothing and only stamps last_search_at", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			store.EXPECT().ListWantedEpisodes(mock.Anything).
				Return([]*ent.TVShow{showWith(ep1)}, nil).Once()

			// No resolution token → rejected by the quality filter.
			indexerM.EXPECT().
				SearchEpisode(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3), uint16(1)).
				Return([]indexer.SearchResult{{Title: "The.Black.Sea.S03E01.DVDRip-GRP"}}, nil).
				Once()
			store.EXPECT().
				SetEpisodeLastSearchAt(mock.Anything, uint32(11), mock.AnythingOfType("time.Time")).
				Return(nil).Once()

			Expect(searcher.Run(ctx)).To(Succeed())
		})
	})

	When("the season pack search yields nothing acceptable", func() {
		It("falls back to per-episode grabs", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			ep2 := &ent.Episode{ID: 12, Number: 2}
			store.EXPECT().ListWantedEpisodes(mock.Anything).
				Return([]*ent.TVShow{showWith(ep1, ep2)}, nil).Once()

			indexerM.EXPECT().
				SearchSeason(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3)).
				Return(nil, nil).Once()

			for _, n := range []uint16{1, 2} {
				indexerM.EXPECT().
					SearchEpisode(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3), n).
					Return([]indexer.SearchResult{{Title: acceptableEp, Seeders: 10}}, nil).
					Once()
			}
			for _, id := range []uint32{11, 12} {
				dlM.EXPECT().
					GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"), id).
					Return(&ent.DownloadRecord{}, nil).Once()
				store.EXPECT().
					SetEpisodeStatus(mock.Anything, id, episode.StatusDownloading).
					Return(nil).Once()
				store.EXPECT().
					ResetEpisodeGrabFailures(mock.Anything, id).Return(nil).Once()
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, id, mock.AnythingOfType("time.Time")).
					Return(nil).Once()
			}

			Expect(searcher.Run(ctx)).To(Succeed())
		})
	})

	When("the wanted-episode query fails", func() {
		It("returns the error", func() {
			boom := context.DeadlineExceeded
			store.EXPECT().ListWantedEpisodes(mock.Anything).Return(nil, boom).Once()
			Expect(searcher.Run(ctx)).To(MatchError(boom))
		})
	})
})
