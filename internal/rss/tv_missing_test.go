package rss

import (
	"context"
	"math"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
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
		// Season lengths are looked up once per pass to size packs; the specs
		// below assert grab behaviour, not the arithmetic, so a permissive stub
		// keeps them focused. condition_test.go covers the scaling itself.
		store.EXPECT().
			SeasonEpisodeCounts(mock.Anything, mock.Anything).
			Return(map[uint32]map[uint16]int{}, nil).Maybe()

		searcher = NewEpisodeMissingSearcher(store, indexerM, dlM)
	})

	// showWith builds a wanted show with one season carrying the given episodes.
	showWith := func(eps ...*ent.Episode) *ent.TVShow {
		show := &ent.TVShow{ID: 1, Title: "The Black Sea", TvdbID: 9001}
		se := &ent.Season{ID: 5, Number: 3}
		se.Edges.Episodes = eps
		show.Edges.Seasons = []*ent.Season{se}
		return show
	}

	// expectEligible stubs the eligibility query, asserting the searcher passes
	// the configured failure cap and a cooldown-derived cutoff in the past.
	expectEligible := func(shows []*ent.TVShow, err error) {
		store.EXPECT().
			ListEligibleEpisodesForSync(
				mock.Anything,
				uint8(3),
				mock.AnythingOfType("time.Time"),
				mock.AnythingOfType("time.Time"),
			).
			Return(shows, err).Once()
	}

	const acceptablePack = "The.Black.Sea.S03.1080p.WEB-DL.x265-GRP"
	const acceptableEp = "The.Black.Sea.S03E01.1080p.WEB-DL.x265-GRP"

	When("no download client is enabled", func() {
		It("skips the pass after the eligibility query", func() {
			overlay := defaultRSSConfig()
			overlay["download_clients"] = []map[string]any{}
			configtest.Setup(overlay)

			expectEligible(
				[]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})},
				nil,
			)

			Expect(searcher.Run(ctx)).To(Succeed())
			// Neither the indexer nor the grabber is touched: the mocks fail the
			// spec on any unexpected call.
		})
	})

	When("a whole season is wanted", func() {
		It(
			"prefers a season pack, grabs it once, marks all episodes downloading",
			func() {
				ep1 := &ent.Episode{ID: 11, Number: 1}
				ep2 := &ent.Episode{ID: 12, Number: 2}
				expectEligible([]*ent.TVShow{showWith(ep1, ep2)}, nil)

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
			expectEligible([]*ent.TVShow{showWith(ep1)}, nil)

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
			expectEligible([]*ent.TVShow{showWith(ep1)}, nil)

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
			expectEligible([]*ent.TVShow{showWith(ep1, ep2)}, nil)

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

	When("the eligibility query fails", func() {
		It("returns the error", func() {
			boom := context.DeadlineExceeded
			expectEligible(nil, boom)
			Expect(searcher.Run(ctx)).To(MatchError(boom))
		})
	})

	When("the library knobs change after construction", func() {
		It("queries with the edited cap, cooldown and an aired cutoff", func() {
			overlay := defaultRSSConfig()
			library := overlay["library"].(map[string]any)
			library["max_grab_failures"] = 9
			library["no_match_cooldown"] = "2h"
			configtest.Setup(overlay)

			var cooldownCutoff, airedBefore time.Time
			store.EXPECT().
				ListEligibleEpisodesForSync(
					mock.Anything,
					uint8(9),
					mock.AnythingOfType("time.Time"),
					mock.AnythingOfType("time.Time"),
				).
				Run(func(_ context.Context, _ uint8, notSearchedSince, aired time.Time) {
					cooldownCutoff, airedBefore = notSearchedSince, aired
				}).
				Return(nil, nil).Once()

			Expect(searcher.Run(ctx)).To(Succeed())
			Expect(cooldownCutoff).To(
				BeTemporally("~", time.Now().Add(-2*time.Hour), time.Minute),
			)
			Expect(airedBefore).To(BeTemporally("~", time.Now(), time.Minute))
		})
	})

	When("a user triggers a scoped search", func() {
		It("waives the failure cap and the cooldown", func() {
			var cutoff time.Time
			store.EXPECT().
				ListEligibleEpisodesForSync(
					mock.Anything,
					uint8(math.MaxUint8),
					mock.AnythingOfType("time.Time"),
					mock.AnythingOfType("time.Time"),
				).
				Run(func(_ context.Context, _ uint8, notSearchedSince, _ time.Time) {
					cutoff = notSearchedSince
				}).
				Return(nil, nil).Once()

			Expect(searcher.SearchShow(ctx, 1)).To(Succeed())
			Expect(cutoff).To(BeTemporally("~", time.Now(), time.Minute))
		})
	})

	When("several season packs clear the quality bar", func() {
		It("grabs the highest-scoring pack, not the first listed", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			ep2 := &ent.Episode{ID: 12, Number: 2}
			show := showWith(ep1, ep2)
			show.QualityProfile = scoredProfile
			expectEligible([]*ent.TVShow{show}, nil)

			remuxPack := indexer.SearchResult{
				Title:   "The.Black.Sea.S03.2160p.BluRay.REMUX.x265-GRP",
				Seeders: 2,
			}
			indexerM.EXPECT().
				SearchSeason(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{
					{Title: acceptablePack, Seeders: 500},
					remuxPack,
				}, nil).Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, remuxPack, uint32(11)).
				Return(&ent.DownloadRecord{}, nil).Once()
			for _, id := range []uint32{11, 12} {
				store.EXPECT().
					SetEpisodeStatus(mock.Anything, id, episode.StatusDownloading).
					Return(nil).Once()
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, id, mock.AnythingOfType("time.Time")).
					Return(nil).Once()
			}

			Expect(searcher.Run(ctx)).To(Succeed())
		})

		It("falls through to the next-best pack when the grab fails", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			ep2 := &ent.Episode{ID: 12, Number: 2}
			show := showWith(ep1, ep2)
			show.QualityProfile = scoredProfile
			expectEligible([]*ent.TVShow{show}, nil)

			remuxPack := indexer.SearchResult{
				Title:   "The.Black.Sea.S03.2160p.BluRay.REMUX.x265-GRP",
				Seeders: 2,
			}
			runnerUp := indexer.SearchResult{Title: acceptablePack, Seeders: 500}
			indexerM.EXPECT().
				SearchSeason(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{runnerUp, remuxPack}, nil).Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, remuxPack, uint32(11)).
				Return(nil, context.DeadlineExceeded).Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, runnerUp, uint32(11)).
				Return(&ent.DownloadRecord{}, nil).Once()
			for _, id := range []uint32{11, 12} {
				store.EXPECT().
					SetEpisodeStatus(mock.Anything, id, episode.StatusDownloading).
					Return(nil).Once()
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, id, mock.AnythingOfType("time.Time")).
					Return(nil).Once()
			}

			Expect(searcher.Run(ctx)).To(Succeed())
		})
	})

	When("several episode releases clear the quality bar", func() {
		It("grabs the highest-scoring release, not the first listed", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			show := showWith(ep1)
			show.QualityProfile = scoredProfile
			expectEligible([]*ent.TVShow{show}, nil)

			remuxEp := indexer.SearchResult{
				Title:   "The.Black.Sea.S03E01.2160p.BluRay.REMUX.x265-GRP",
				Seeders: 2,
			}
			indexerM.EXPECT().
				SearchEpisode(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3), uint16(1)).
				Return([]indexer.SearchResult{
					{Title: acceptableEp, Seeders: 500},
					remuxEp,
				}, nil).Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, remuxEp, uint32(11)).
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

	When("the show pins a quality profile the release misses", func() {
		It("rejects the release and only stamps last_search_at", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			show := showWith(ep1)
			show.QualityProfile = uhdProfile
			expectEligible([]*ent.TVShow{show}, nil)

			// 1080p clears the default profile but not the show's 2160p floor.
			indexerM.EXPECT().
				SearchEpisode(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3), uint16(1)).
				Return([]indexer.SearchResult{{Title: acceptableEp, Seeders: 10}}, nil).
				Once()
			store.EXPECT().
				SetEpisodeLastSearchAt(mock.Anything, uint32(11), mock.AnythingOfType("time.Time")).
				Return(nil).Once()

			Expect(searcher.Run(ctx)).To(Succeed())
		})

		It("grabs a season pack that clears the pinned floor", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			ep2 := &ent.Episode{ID: 12, Number: 2}
			show := showWith(ep1, ep2)
			show.QualityProfile = uhdProfile
			expectEligible([]*ent.TVShow{show}, nil)

			uhdPack := indexer.SearchResult{
				Title:   "The.Black.Sea.S03.2160p.WEB-DL.x265-GRP",
				Seeders: 10,
			}
			indexerM.EXPECT().
				SearchSeason(mock.Anything, []string{"The Black Sea"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{
					{Title: acceptablePack, Seeders: 30},
					uhdPack,
				}, nil).
				Once()
			// The higher-seeded 1080p pack is skipped for the pinned floor.
			dlM.EXPECT().
				GrabEpisode(mock.Anything, uhdPack, uint32(11)).
				Return(&ent.DownloadRecord{}, nil).Once()
			for _, id := range []uint32{11, 12} {
				store.EXPECT().
					SetEpisodeStatus(mock.Anything, id, episode.StatusDownloading).
					Return(nil).Once()
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, id, mock.AnythingOfType("time.Time")).
					Return(nil).Once()
			}

			Expect(searcher.Run(ctx)).To(Succeed())
		})
	})
})
