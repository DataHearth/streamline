package rss

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// matchIDs matches a grab's wanted-episode argument against an exact ID set,
// order-insensitively: which episodes a pack was grabbed for is the contract,
// the order the union happens to build them in is not.
func matchIDs(ids ...uint32) any {
	want := slices.Sorted(slices.Values(ids))
	return mock.MatchedBy(func(got []uint32) bool {
		return slices.Equal(slices.Sorted(slices.Values(got)), want)
	})
}

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
		show := &ent.TVShow{
			ID: 1, Title: "The Black Sea",
			OriginalTitle: "Karadeniz", TvdbID: 9001,
		}
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

	// expectNoUpgradeCandidates stubs the on-disk lookup a pack attempt makes
	// under an upgrade-permitting profile with the empty answer: the specs
	// below hold nothing on disk, so their packs fill gaps only.
	expectNoUpgradeCandidates := func() {
		store.EXPECT().UpgradeCandidateShow(mock.Anything, uint32(1)).
			Return(nil, nil).Once()
	}

	// expectReplaceMode stubs the flag every pack grab stamps: a pack may beat
	// files on disk, and the importer needs the permission to act on it.
	expectReplaceMode := func(recordID uint32) {
		store.EXPECT().
			SetDownloadRecordReplaceMode(
				mock.Anything, recordID, downloadrecord.ReplaceModeUpgrades,
			).Return(nil).Once()
	}

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
					SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3)).
					Return([]indexer.SearchResult{{Title: acceptablePack, Seeders: 10}}, nil).
					Once()
				expectNoUpgradeCandidates()

				// Grabbed once, against the first wanted episode, carrying both
				// wanted episode IDs.
				dlM.EXPECT().
					GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
						uint32(11), []uint32{11, 12}).
					Return(&ent.DownloadRecord{ID: 55}, nil).Once()
				expectReplaceMode(55)

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
				SearchEpisode(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3), uint16(1)).
				Return([]indexer.SearchResult{{Title: acceptableEp, Seeders: 10}}, 0, nil).
				Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
					uint32(11), []uint32{11}).
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
				SearchEpisode(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3), uint16(1)).
				Return([]indexer.SearchResult{{Title: "The.Black.Sea.S03E01.DVDRip-GRP"}}, 0, nil).
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
				SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3)).
				Return(nil, nil).Once()

			for _, n := range []uint16{1, 2} {
				indexerM.EXPECT().
					SearchEpisode(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3), n).
					Return([]indexer.SearchResult{{Title: acceptableEp, Seeders: 10}}, 0, nil).
					Once()
			}
			for _, id := range []uint32{11, 12} {
				dlM.EXPECT().
					GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
						id, []uint32{id}).
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

	When("a season pack holds nothing for the wanted episodes", func() {
		// ErrNoWantedFiles means the release cannot fill the gap at all, which
		// is exactly the standing the single-episode path counts. Uncounted, a
		// season whose only packs all mismatch is re-searched every tick and
		// never reaches max_grab_failures.
		It("bumps grab_failures on the anchor episode", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			ep2 := &ent.Episode{ID: 12, Number: 2}
			expectEligible([]*ent.TVShow{showWith(ep1, ep2)}, nil)

			indexerM.EXPECT().
				SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{{Title: acceptablePack, Seeders: 10}}, nil).
				Once()
			expectNoUpgradeCandidates()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
					uint32(11), []uint32{11, 12}).
				Return(nil, fmt.Errorf("%w: pack", download.ErrNoWantedFiles)).Once()
			store.EXPECT().
				IncrementEpisodeGrabFailures(mock.Anything, uint32(11)).
				Return(nil).Once()

			// The pack was the only result, so the run falls through to the
			// per-episode search for both episodes.
			for _, n := range []uint16{1, 2} {
				indexerM.EXPECT().
					SearchEpisode(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3), n).
					Return(nil, 0, nil).Once()
			}
			for _, id := range []uint32{11, 12} {
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, id, mock.AnythingOfType("time.Time")).
					Return(nil).Once()
			}

			Expect(searcher.Run(ctx)).To(Succeed())
		})
	})

	When("a season-pack grab fails for any other reason", func() {
		// Negative control: only ErrNoWantedFiles is evidence about the
		// episode. An unreachable client says nothing about the release.
		It("leaves grab_failures alone", func() {
			ep1 := &ent.Episode{ID: 11, Number: 1}
			ep2 := &ent.Episode{ID: 12, Number: 2}
			expectEligible([]*ent.TVShow{showWith(ep1, ep2)}, nil)

			indexerM.EXPECT().
				SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{{Title: acceptablePack, Seeders: 10}}, nil).
				Once()
			expectNoUpgradeCandidates()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
					uint32(11), []uint32{11, 12}).
				Return(nil, errors.New("client unreachable")).Once()

			for _, n := range []uint16{1, 2} {
				indexerM.EXPECT().
					SearchEpisode(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3), n).
					Return(nil, 0, nil).Once()
			}
			for _, id := range []uint32{11, 12} {
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, id, mock.AnythingOfType("time.Time")).
					Return(nil).Once()
			}

			Expect(searcher.Run(ctx)).To(Succeed())
			// IncrementEpisodeGrabFailures carries no expectation above:
			// calling it would panic the mock.
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
				SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{
					{Title: acceptablePack, Seeders: 500},
					remuxPack,
				}, nil).Once()
			expectNoUpgradeCandidates()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, remuxPack, uint32(11), []uint32{11, 12}).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			expectReplaceMode(55)
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
				SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{runnerUp, remuxPack}, nil).Once()
			// One lookup for the whole attempt: the fall-through to the
			// runner-up re-scores the beat-set, it does not re-query.
			expectNoUpgradeCandidates()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, remuxPack, uint32(11), []uint32{11, 12}).
				Return(nil, context.DeadlineExceeded).Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, runnerUp, uint32(11), []uint32{11, 12}).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			expectReplaceMode(55)
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
				SearchEpisode(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3), uint16(1)).
				Return([]indexer.SearchResult{
					{Title: acceptableEp, Seeders: 500},
					remuxEp,
				}, 0, nil).Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, remuxEp, uint32(11), []uint32{11}).
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
				SearchEpisode(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3), uint16(1)).
				Return([]indexer.SearchResult{{Title: acceptableEp, Seeders: 10}}, 0, nil).
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
				SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{
					{Title: acceptablePack, Seeders: 30},
					uhdPack,
				}, nil).
				Once()
			expectNoUpgradeCandidates()
			// The higher-seeded 1080p pack is skipped for the pinned floor.
			dlM.EXPECT().
				GrabEpisode(mock.Anything, uhdPack, uint32(11), []uint32{11, 12}).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			expectReplaceMode(55)
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

	Context("a season pack that also beats files on disk", func() {
		// remux only: it outscores a plain WEB-DL file and ties a remux one, so
		// one release splits the season's on-disk episodes into beaten and not.
		const remuxPack = "The.Black.Sea.S03.1080p.BluRay.REMUX.x264-GRP"

		// onDisk builds an upgrade candidate: one episode holding a file, 1080p
		// by both its name and its probed width.
		onDisk := func(id uint32, number uint16, source string) *ent.Episode {
			e := &ent.Episode{ID: id, Number: number}
			e.Edges.MediaFiles = []*ent.MediaFile{{
				Path: fmt.Sprintf(
					"/tv/The Black Sea/Season 03/The.Black.Sea.S03E%02d.%s.mkv",
					number, source,
				),
				Size:       4_000_000_000,
				Width:      1920,
				VideoCodec: "h264",
			}}
			return e
		}
		beaten := func(id uint32, number uint16) *ent.Episode {
			return onDisk(id, number, "1080p.WEB-DL.x264-GRP")
		}
		kept := func(id uint32, number uint16) *ent.Episode {
			return onDisk(id, number, "1080p.BluRay.REMUX.x264-GRP")
		}

		expectPackSearch := func() {
			indexerM.EXPECT().
				SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(3)).
				Return([]indexer.SearchResult{{Title: remuxPack, Seeders: 10}}, nil).
				Once()
		}
		expectDownloading := func(ids ...uint32) {
			for _, id := range ids {
				store.EXPECT().
					SetEpisodeStatus(mock.Anything, id, episode.StatusDownloading).
					Return(nil).Once()
				store.EXPECT().
					SetEpisodeLastSearchAt(
						mock.Anything, id, mock.AnythingOfType("time.Time"),
					).
					Return(nil).Once()
			}
		}

		BeforeEach(func() { configtest.Setup(upgradeConfig()) })

		It("unions beatable episodes into a season-pack grab", func() {
			expectEligible([]*ent.TVShow{showWith(
				&ent.Episode{ID: 11, Number: 1},
				&ent.Episode{ID: 12, Number: 2},
				&ent.Episode{ID: 13, Number: 3},
				&ent.Episode{ID: 14, Number: 4},
				&ent.Episode{ID: 15, Number: 5},
			)}, nil)
			store.EXPECT().UpgradeCandidateShow(mock.Anything, uint32(1)).
				Return(showWith(
					beaten(16, 6), beaten(17, 7), beaten(18, 8), beaten(19, 9),
					kept(20, 10), kept(21, 11), kept(22, 12),
				), nil).Once()
			expectPackSearch()

			// The missing five plus the four the release beats, and nothing the
			// release only ties.
			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
					uint32(11), matchIDs(11, 12, 13, 14, 15, 16, 17, 18, 19)).
				Return(&ent.DownloadRecord{ID: 40}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(40), downloadrecord.ReplaceModeUpgrades,
				).Return(nil).Once()
			// Only the missing episodes are marked: a beaten one keeps its file's
			// "available" until the importer decides per file. No expectation is
			// registered for 16-22, so a status write on one fails the spec.
			expectDownloading(11, 12, 13, 14, 15)

			Expect(searcher.Run(ctx)).To(Succeed())
		})

		It("skips the upgrade query when the profile forbids upgrades", func() {
			show := showWith(
				&ent.Episode{ID: 11, Number: 1},
				&ent.Episode{ID: 12, Number: 2},
			)
			show.QualityProfile = lockedProfile
			expectEligible([]*ent.TVShow{show}, nil)
			expectPackSearch()

			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
					uint32(11), matchIDs(11, 12)).
				Return(&ent.DownloadRecord{ID: 41}, nil).Once()
			// The mode is stamped regardless: an empty beat-set imports exactly
			// as replace_mode none did.
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(41), downloadrecord.ReplaceModeUpgrades,
				).Return(nil).Once()
			expectDownloading(11, 12)

			// UpgradeCandidateShow carries no expectation: calling it fails the
			// spec, which is the assertion that the query never runs.
			Expect(searcher.Run(ctx)).To(Succeed())
		})

		It("grabs whole when every episode is missing or beaten", func() {
			// wanted here is all 12 IDs; the collapse to a plain whole-torrent
			// grab (selection_state: skipped) is the download manager's job,
			// covered by "every candidate wanted: whole grab, record left at
			// the skipped default" in internal/download/manager_test.go.
			// selective_files: false collapses the same way, covered there by
			// "selective_files off: identical to a nil wanted set even with
			// one present".
			expectEligible([]*ent.TVShow{showWith(
				&ent.Episode{ID: 11, Number: 1},
				&ent.Episode{ID: 12, Number: 2},
			)}, nil)
			store.EXPECT().UpgradeCandidateShow(mock.Anything, uint32(1)).
				Return(showWith(
					beaten(13, 3), beaten(14, 4), beaten(15, 5), beaten(16, 6),
					beaten(17, 7), beaten(18, 8), beaten(19, 9), beaten(20, 10),
					beaten(21, 11), beaten(22, 12),
				), nil).Once()
			expectPackSearch()

			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
					uint32(11), matchIDs(
						11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22,
					)).
				Return(&ent.DownloadRecord{ID: 42}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(42), downloadrecord.ReplaceModeUpgrades,
				).Return(nil).Once()
			expectDownloading(11, 12)

			Expect(searcher.Run(ctx)).To(Succeed())
		})
	})

	Context("a per-tick grabbed set", func() {
		It("does not re-search episodes a pack grab already serves", func() {
			ep21 := &ent.Episode{ID: 21, Number: 1}
			ep22 := &ent.Episode{ID: 22, Number: 2}
			season2 := &ent.Season{ID: 6, Number: 2}
			season2.Edges.Episodes = []*ent.Episode{ep21, ep22}

			// Season 3's only wanted episode reuses season 2's ID 21 — a shape
			// impossible for a real season pack, chosen to prove the skip is
			// driven by the shared set, not by season topology.
			season3 := &ent.Season{ID: 7, Number: 3}
			season3.Edges.Episodes = []*ent.Episode{{ID: 21, Number: 1}}

			show := &ent.TVShow{
				ID: 1, Title: "The Black Sea",
				OriginalTitle: "Karadeniz", TvdbID: 9001,
			}
			show.Edges.Seasons = []*ent.Season{season2, season3}
			expectEligible([]*ent.TVShow{show}, nil)

			indexerM.EXPECT().
				SearchSeason(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(2)).
				Return([]indexer.SearchResult{{Title: "The.Black.Sea.S02.1080p.WEB-DL.x265-GRP", Seeders: 10}}, nil).
				Once()
			expectNoUpgradeCandidates()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
					uint32(21), []uint32{21, 22}).
				Return(&ent.DownloadRecord{ID: 60}, nil).Once()
			expectReplaceMode(60)
			for _, id := range []uint32{21, 22} {
				store.EXPECT().
					SetEpisodeStatus(mock.Anything, id, episode.StatusDownloading).
					Return(nil).Once()
				store.EXPECT().
					SetEpisodeLastSearchAt(mock.Anything, id, mock.AnythingOfType("time.Time")).
					Return(nil).Once()
			}

			// Season 3 issues no SearchEpisode call and no GrabEpisode call for
			// ID 21: the mocks fail the spec on any unexpected call.
			Expect(searcher.Run(ctx)).To(Succeed())
		})

		It("single-episode grabs enter the set too", func() {
			epA := &ent.Episode{ID: 30, Number: 1}
			season1 := &ent.Season{ID: 8, Number: 1}
			season1.Edges.Episodes = []*ent.Episode{epA}

			// Season 2's list reuses ID 30 — again an impossible-in-practice
			// shape, chosen to prove a single grab enters the same set a pack
			// grab would.
			season2 := &ent.Season{ID: 9, Number: 2}
			season2.Edges.Episodes = []*ent.Episode{{ID: 30, Number: 1}}

			show := &ent.TVShow{
				ID: 1, Title: "The Black Sea",
				OriginalTitle: "Karadeniz", TvdbID: 9001,
			}
			show.Edges.Seasons = []*ent.Season{season1, season2}
			expectEligible([]*ent.TVShow{show}, nil)

			indexerM.EXPECT().
				SearchEpisode(mock.Anything, []string{"The Black Sea", "Karadeniz"}, uint32(9001), uint16(1), uint16(1)).
				Return([]indexer.SearchResult{{Title: acceptableEp, Seeders: 10}}, 0, nil).
				Once()
			dlM.EXPECT().
				GrabEpisode(mock.Anything, mock.AnythingOfType("indexer.SearchResult"),
					uint32(30), []uint32{30}).
				Return(&ent.DownloadRecord{}, nil).Once()
			store.EXPECT().
				SetEpisodeStatus(mock.Anything, uint32(30), episode.StatusDownloading).
				Return(nil).Once()
			store.EXPECT().
				ResetEpisodeGrabFailures(mock.Anything, uint32(30)).
				Return(nil).Once()
			store.EXPECT().
				SetEpisodeLastSearchAt(mock.Anything, uint32(30), mock.AnythingOfType("time.Time")).
				Return(nil).Once()

			// Season 2 issues no SearchEpisode call: the mock's query-count
			// expectation (Once, above) fails the spec on a second call.
			Expect(searcher.Run(ctx)).To(Succeed())
		})
	})
})
