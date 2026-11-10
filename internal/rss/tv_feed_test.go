package rss

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/roottest"
)

var _ = Describe("TVFeedScanner.Run", Label("unit", "rss"), func() {
	var (
		ctx     context.Context
		store   *dbmocks.MockStore
		feeder  *mocks.MockIndexerFeeder
		grabber *mocks.MockEpisodeGrabber
		scanner *TVFeedScanner
		// library is what ListShowNames answers: the shows outside both
		// listings. The listed shows are resolved alongside it either way.
		library []*ent.TVShow
	)

	newScanner := func() {
		scanner = NewTVFeedScanner(store, feeder, grabber)
	}

	BeforeEach(func() {
		ctx = context.Background()
		library = nil
		store = dbmocks.NewMockStore(GinkgoT())
		// Nothing rejected; blocklist_integration_test.go covers the filter.
		store.EXPECT().RejectedReleases(mock.Anything).Return(nil, nil).Maybe()
		feeder = mocks.NewMockIndexerFeeder(GinkgoT())
		grabber = mocks.NewMockEpisodeGrabber(GinkgoT())
		// Season lengths are looked up once per pass to size packs; the specs
		// below assert grab behaviour, not the arithmetic, so a permissive stub
		// keeps them focused. condition_test.go covers the scaling itself.
		store.EXPECT().
			SeasonEpisodeCounts(mock.Anything, mock.Anything).
			Return(map[uint32]map[uint16]int{}, nil).Maybe()
	})

	// showWith builds a wanted show whose season 3 carries the given episodes.
	showWith := func(eps ...*ent.Episode) *ent.TVShow {
		show := &ent.TVShow{
			ID: 1, Title: "The Black Sea", TvdbID: 9001,
			Type: tvshow.TypeStandard,
		}
		se := &ent.Season{ID: 5, Number: 3}
		se.Edges.Episodes = eps
		show.Edges.Seasons = []*ent.Season{se}
		return show
	}

	// expectEligible stubs the eligibility query. The cooldown is waived by the
	// feed scanner, so the only knob asserted is the configured failure cap.
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

	// expectQueries stubs both per-tick listings: the wanted backlog, and the
	// episodes already on disk an upgrade may replace. Every pass that gets
	// past the download-client check runs both.
	expectQueries := func(wanted, upgradable []*ent.TVShow) {
		expectEligible(wanted, nil)
		store.EXPECT().ListUpgradeCandidateShows(mock.Anything).
			Return(upgradable, nil).Once()
		store.EXPECT().ListShowNames(mock.Anything).Return(library, nil).Once()
	}

	// expectDownloading stubs the two writes every successful grab performs.
	expectDownloading := func(id uint32) {
		store.EXPECT().
			SetEpisodeStatus(mock.Anything, id, episode.StatusDownloading).
			Return(nil).Once()
		store.EXPECT().
			SetEpisodeLastSearchAt(mock.Anything, id, mock.AnythingOfType("time.Time")).
			Return(nil).Once()
	}

	const acceptableEp = "The.Black.Sea.S03E01.1080p.WEB-DL.x265-GRP"
	const acceptablePack = "The.Black.Sea.S03.1080p.WEB-DL.x265-GRP"

	It("grabs nothing for offline series", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		roottest.Offline(db.RootSeries)

		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("noops when no indexers are configured", func() {
		configtest.Setup()
		newScanner()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("skips the pass when no download client is enabled", func() {
		cfg := indexerConfig("a")
		cfg["download_clients"] = []map[string]any{}
		configtest.Setup(cfg)
		newScanner()
		expectEligible([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
		// No Feed call: the whole point is not to pull feeds we can't act on.
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("continues to the next indexer on per-indexer error", func() {
		configtest.Setup(indexerConfig("a", "b"))
		newScanner()
		expectQueries(nil, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return(nil, errors.New("boom")).Once()
		feeder.EXPECT().Feed(mock.Anything, "b").Return(nil, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("grabs a single episode matched by season+episode number", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		expectQueries([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: acceptableEp}}, nil).Once()
		grabber.EXPECT().
			GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().ResetEpisodeGrabFailures(mock.Anything, uint32(11)).
			Return(nil).Once()
		expectDownloading(11)
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It(
		"grabs a season pack once and marks every wanted episode downloading",
		func() {
			configtest.Setup(indexerConfig("a"))
			newScanner()
			expectQueries([]*ent.TVShow{showWith(
				&ent.Episode{ID: 11, Number: 1},
				&ent.Episode{ID: 12, Number: 2},
			)}, nil)
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: acceptablePack}}, nil).Once()
			// One record, against the first wanted episode.
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(55), downloadrecord.ReplaceModeUpgrades,
				).Return(nil).Once()
			expectDownloading(11)
			expectDownloading(12)
			Expect(scanner.Run(ctx)).To(Succeed())
		},
	)

	It(
		"bumps grab_failures on the anchor when a pack holds no wanted file",
		func() {
			// ErrNoWantedFiles means the release cannot fill the gap at all —
			// the same standing the single-episode arm just above counts.
			// Uncounted, a season whose only packs all mismatch is re-tried on
			// every feed tick and never reaches max_grab_failures.
			configtest.Setup(indexerConfig("a"))
			newScanner()
			expectQueries([]*ent.TVShow{showWith(
				&ent.Episode{ID: 11, Number: 1},
				&ent.Episode{ID: 12, Number: 2},
			)}, nil)
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: acceptablePack}}, nil).Once()
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
				Return(nil, fmt.Errorf("%w: pack", download.ErrNoWantedFiles)).Once()
			store.EXPECT().IncrementEpisodeGrabFailures(mock.Anything, uint32(11)).
				Return(nil).Once()
			Expect(scanner.Run(ctx)).To(Succeed())
			// SetDownloadRecordReplaceMode and the downloading markers carry no
			// expectation above: calling any would panic the mock.
		},
	)

	It(
		"leaves grab_failures alone when a pack grab fails for another reason",
		func() {
			// Negative control: only ErrNoWantedFiles is evidence about the
			// episode; an offline client says nothing about the release.
			configtest.Setup(indexerConfig("a"))
			newScanner()
			expectQueries([]*ent.TVShow{showWith(
				&ent.Episode{ID: 11, Number: 1},
				&ent.Episode{ID: 12, Number: 2},
			)}, nil)
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: acceptablePack}}, nil).Once()
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
				Return(nil, errors.New("client offline")).Once()
			Expect(scanner.Run(ctx)).To(Succeed())
			// IncrementEpisodeGrabFailures carries no expectation above:
			// calling it would panic the mock.
		},
	)

	It("skips whole-series packs", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		expectQueries([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "The.Black.Sea.S01-S05.COMPLETE.1080p.WEB-DL.x265-GRP"},
			}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("rejects items that fail the show's quality profile", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		show := showWith(&ent.Episode{ID: 11, Number: 1})
		show.QualityProfile = uhdProfile
		expectQueries([]*ent.TVShow{show}, nil)
		// 1080p clears the default profile but not the show's 2160p floor.
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: acceptableEp}}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("ignores an episode the show doesn't want", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		expectQueries([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "The.Black.Sea.S03E09.1080p.WEB-DL.x265-GRP"},
			}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("bumps grab_failures when the grab errors", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		expectQueries([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: acceptableEp}}, nil).Once()
		grabber.EXPECT().
			GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
			Return(nil, errors.New("client offline")).Once()
		store.EXPECT().IncrementEpisodeGrabFailures(mock.Anything, uint32(11)).
			Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("only grabs once when two indexers carry the same episode", func() {
		configtest.Setup(indexerConfig("a", "b"))
		newScanner()
		expectQueries([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
		for _, name := range []string{"a", "b"} {
			feeder.EXPECT().Feed(mock.Anything, name).
				Return([]indexer.SearchResult{{Title: acceptableEp}}, nil).Once()
		}
		grabber.EXPECT().
			GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().ResetEpisodeGrabFailures(mock.Anything, uint32(11)).
			Return(nil).Once()
		expectDownloading(11)
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("matches an anime release by absolute number", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		show := showWith(&ent.Episode{ID: 11, Number: 1, AbsoluteNumber: 18})
		show.Title = "Blue Lock"
		show.Type = tvshow.TypeAnime
		expectQueries([]*ent.TVShow{show}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "[SubGrp] Blue Lock - 18 [1080p][HEVC]"},
			}, nil).Once()
		grabber.EXPECT().
			GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().ResetEpisodeGrabFailures(mock.Anything, uint32(11)).
			Return(nil).Once()
		expectDownloading(11)
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("matches a daily release by air date", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		aired := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
		show := showWith(&ent.Episode{ID: 11, Number: 1, AirDate: aired})
		show.Title = "The Daily Show"
		show.Type = tvshow.TypeDaily
		expectQueries([]*ent.TVShow{show}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "The.Daily.Show.2026.07.14.1080p.WEB-DL.x265-GRP"},
			}, nil).Once()
		grabber.EXPECT().
			GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().ResetEpisodeGrabFailures(mock.Anything, uint32(11)).
			Return(nil).Once()
		expectDownloading(11)
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("does not absolute-match a standard show", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		expectQueries([]*ent.TVShow{showWith(
			&ent.Episode{ID: 11, Number: 1, AbsoluteNumber: 18},
		)}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "The Black Sea - 18 [1080p][HEVC]"},
			}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	Context("upgrades", func() {
		const (
			plainEpFile = "The.Black.Sea.S03E01.1080p.WEB-DL.x264-GRP.mkv"
			multiEpFile = "The.Black.Sea.S03E02.1080p.MULTi.BluRay.x264-GRP.mkv"

			betterEp   = "The.Black.Sea.S03E01.2160p.MULTi.BluRay.x265-GRP"
			betterPack = "The.Black.Sea.S03.2160p.MULTi.BluRay.x265-GRP"
			// multi-audio only: ties the 200 a multi-audio file already scores.
			tiedPack = "The.Black.Sea.S03.1080p.MULTi.BluRay.x264-GRP"
			// betterEp's E02 twin. Fed as a second item in the same tick to turn
			// "which episodes ended up in `selected`/`targets`" into an
			// observable: GrabEpisode always fires against the pack's first
			// episode regardless of how many episodes it actually covers, so
			// asserting on that call alone can't tell a correct selection from a
			// wrong one. This release, offered on its own, only gets grabbed
			// if E02 is still up for grabs — i.e. wasn't already claimed by the
			// pack this tick (`pass.grabbed`).
			betterEp2 = "The.Black.Sea.S03E02.2160p.MULTi.BluRay.x265-GRP"
		)

		// epWithFile builds an upgrade candidate episode: one on disk, 1080p by
		// both its name and its probed width.
		epWithFile := func(id uint32, number uint16, basename string) *ent.Episode {
			e := &ent.Episode{ID: id, Number: number}
			e.Edges.MediaFiles = []*ent.MediaFile{fileFixture(
				"/tv/The Black Sea/Season 03/"+basename, basename, 1920,
			)}
			return e
		}

		onProfile := func(name string, show *ent.TVShow) *ent.TVShow {
			show.QualityProfile = name
			return show
		}

		It("grabs an episode upgrade and flags the record replace_existing", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			expectQueries(nil, []*ent.TVShow{
				showWith(epWithFile(11, 1, plainEpFile)),
			})
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: betterEp}}, nil).Once()
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(55), downloadrecord.ReplaceModeUpgrades,
				).
				Return(nil).Once()
			// The target already has a file, so the grab stays a background
			// replacement: no SetEpisodeStatus/SetEpisodeLastSearchAt stub is
			// registered, and the strict mock fails the test if either fires.
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		DescribeTable(
			"grabs no upgrade for an episode whose file is missing from disk",
			func(release string) {
				configtest.Setup(upgradeConfig("a"))
				newScanner()
				e := epWithFile(11, 1, plainEpFile)
				e.Edges.MediaFiles[0].MissingSince = new(time.Now())
				expectQueries(nil, []*ent.TVShow{showWith(e)})
				feeder.EXPECT().Feed(mock.Anything, "a").
					Return([]indexer.SearchResult{{Title: release}}, nil).Once()
				// No grab expectation: the strict mock fails the spec if one fires.
				Expect(scanner.Run(ctx)).To(Succeed())
			},
			Entry("a single episode", betterEp),
			Entry("a season pack", betterPack),
		)

		It("upgrades a whole season against its best file", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			expectQueries(nil, []*ent.TVShow{showWith(
				epWithFile(11, 1, plainEpFile),
				epWithFile(12, 2, multiEpFile),
			)})
			// betterEp2 rides along in the same feed: GrabEpisode always fires
			// against the pack's first episode regardless of how many episodes
			// it covers, so that call alone can't prove E02 was included too.
			// No grabber/store expectation is registered for it — if the pack's
			// selection silently dropped E02, betterEp2 would find it still up
			// for grabs and the strict mock would fail on the unexpected call.
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{
					{Title: betterPack}, {Title: betterEp2},
				}, nil).Once()
			// One record, against the first episode of the season.
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(55), downloadrecord.ReplaceModeUpgrades,
				).
				Return(nil).Once()
			// Both targets already have files; see the single-episode case above.
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("replaces the episodes a tied pack beats and leaves the rest", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			expectQueries(nil, []*ent.TVShow{showWith(
				epWithFile(11, 1, plainEpFile),
				epWithFile(12, 2, multiEpFile),
			)})
			// betterEp2 rides along in the same feed as the real assertion that
			// E02 was "left": GrabEpisode(...,11) fires identically whether the
			// pack's selection was {E01} or {E01, E02} (it always grabs against
			// selected[0]), so that call can't tell "leaves the rest" from
			// "replaces the whole season". betterEp2 only gets grabbed if E02 is
			// still up for grabs, i.e. the tied pack didn't select it.
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{
					{Title: tiedPack}, {Title: betterEp2},
				}, nil).Once()
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(55), downloadrecord.ReplaceModeUpgrades,
				).Return(nil).Once()
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(12), mock.Anything).
				Return(&ent.DownloadRecord{ID: 66}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(66), downloadrecord.ReplaceModeUpgrades,
				).Return(nil).Once()
			// Neither episode gets a status write; both already have files.
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("never upgrades under a profile with upgrade_allowed false", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			expectQueries(nil, []*ent.TVShow{
				onProfile(lockedProfile, showWith(epWithFile(11, 1, plainEpFile))),
			})
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: betterEp}}, nil).Once()
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("fills a wanted episode and replaces what the pack beats", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			expectQueries(
				[]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})},
				[]*ent.TVShow{showWith(epWithFile(12, 2, multiEpFile))},
			)
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: tiedPack}}, nil).Once()
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11), mock.Anything).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(55), downloadrecord.ReplaceModeUpgrades,
				).Return(nil).Once()
			expectDownloading(11)
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("does not grab a pack that beats nothing and fills nothing", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			expectQueries(nil, []*ent.TVShow{showWith(
				epWithFile(11, 1, multiEpFile),
				epWithFile(12, 2, multiEpFile),
			)})
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: tiedPack}}, nil).Once()
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It(
			"carries the gap-filled episode and the one it beats, but not the one it doesn't",
			func() {
				configtest.Setup(upgradeConfig("a"))
				newScanner()
				expectQueries(
					[]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})},
					[]*ent.TVShow{showWith(
						epWithFile(12, 2, plainEpFile),
						epWithFile(13, 3, multiEpFile),
					)},
				)
				// tiedPack beats the plain E02 file (200 > 0) but only ties the
				// remux E03 file (200 == 200, not strictly greater) — the exact
				// mix that tells "beats" apart from "wanted".
				feeder.EXPECT().Feed(mock.Anything, "a").
					Return([]indexer.SearchResult{{Title: tiedPack}}, nil).Once()
				grabber.EXPECT().
					GrabEpisode(
						mock.Anything, mock.Anything, uint32(11), []uint32{11, 12},
					).
					Return(&ent.DownloadRecord{ID: 55}, nil).Once()
				store.EXPECT().
					SetDownloadRecordReplaceMode(
						mock.Anything, uint32(55), downloadrecord.ReplaceModeUpgrades,
					).
					Return(nil).Once()
				// Only the missing episode (11) is marked; the beaten one (12)
				// keeps status "available" — the importer decides it per file.
				expectDownloading(11)
				Expect(scanner.Run(ctx)).To(Succeed())
			},
		)

		It(
			"leaves the beat set out of the gap-fill when the profile forbids upgrades",
			func() {
				configtest.Setup(upgradeConfig("a"))
				newScanner()
				expectQueries(
					[]*ent.TVShow{onProfile(
						lockedProfile, showWith(&ent.Episode{ID: 11, Number: 1}),
					)},
					[]*ent.TVShow{onProfile(
						lockedProfile,
						showWith(epWithFile(12, 2, plainEpFile)),
					)},
				)
				feeder.EXPECT().Feed(mock.Anything, "a").
					Return([]indexer.SearchResult{{Title: tiedPack}}, nil).Once()
				grabber.EXPECT().
					GrabEpisode(
						mock.Anything, mock.Anything, uint32(11), []uint32{11},
					).
					Return(&ent.DownloadRecord{ID: 55}, nil).Once()
				store.EXPECT().
					SetDownloadRecordReplaceMode(
						mock.Anything, uint32(55), downloadrecord.ReplaceModeUpgrades,
					).
					Return(nil).Once()
				expectDownloading(11)
				Expect(scanner.Run(ctx)).To(Succeed())
			},
		)

		It(
			"carries exactly the episode a pure upgrade beats, non-empty",
			func() {
				configtest.Setup(upgradeConfig("a"))
				newScanner()
				expectQueries(nil, []*ent.TVShow{showWith(
					epWithFile(11, 1, plainEpFile),
					epWithFile(12, 2, multiEpFile),
				)})
				feeder.EXPECT().Feed(mock.Anything, "a").
					Return([]indexer.SearchResult{{Title: tiedPack}}, nil).Once()
				grabber.EXPECT().
					GrabEpisode(
						mock.Anything, mock.Anything, uint32(11), []uint32{11},
					).
					Return(&ent.DownloadRecord{ID: 55}, nil).Once()
				store.EXPECT().
					SetDownloadRecordReplaceMode(
						mock.Anything, uint32(55), downloadrecord.ReplaceModeUpgrades,
					).
					Return(nil).Once()
				Expect(scanner.Run(ctx)).To(Succeed())
			},
		)
	})
	Context("show names", func() {
		// named builds a wanted show whose season 1 holds episode 1 (id epID).
		named := func(id, epID uint32, title string, aliases ...string) *ent.TVShow {
			show := &ent.TVShow{
				ID: id, Title: title, Aliases: aliases, Type: tvshow.TypeStandard,
			}
			se := &ent.Season{ID: id * 10, Number: 1}
			se.Edges.Episodes = []*ent.Episode{{ID: epID, Number: 1}}
			show.Edges.Seasons = []*ent.Season{se}
			return show
		}

		feed := func(titles ...string) {
			items := make([]indexer.SearchResult, len(titles))
			for i, t := range titles {
				items[i] = indexer.SearchResult{Title: t}
			}
			feeder.EXPECT().Feed(mock.Anything, "a").Return(items, nil).Once()
		}

		expectGrab := func(epID uint32) {
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, epID, []uint32{epID}).
				Return(&ent.DownloadRecord{}, nil).Once()
			store.EXPECT().ResetEpisodeGrabFailures(mock.Anything, epID).
				Return(nil).Once()
			expectDownloading(epID)
		}

		BeforeEach(func() {
			configtest.Setup(indexerConfig("a"))
			newScanner()
		})

		It("grabs a release in a non-Latin script for its own show only", func() {
			expectQueries([]*ent.TVShow{
				named(1, 11, "Мастер и Маргарита"),
				named(2, 21, "Слово пацана"),
			}, nil)
			feed(
				"Мастер.и.Маргарита.S01E01.1080p.WEB-DL.x265-GRP",
				// Other shows in a non-Latin script: were every such title one
				// key, these would be grabbed for whichever show held it.
				"Метод.S01E01.1080p.WEB-DL.x265-GRP",
				"Ψ.S01E01.1080p.WEB-DL.x265-GRP",
			)
			expectGrab(11)
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It(
			"matches an accented title against the release that drops the accent",
			func() {
				expectQueries([]*ent.TVShow{named(1, 11, "Shōgun")}, nil)
				feed("Shogun.2024.S01E01.1080p.WEB-DL.x265-GRP")
				expectGrab(11)
				Expect(scanner.Run(ctx)).To(Succeed())
			},
		)

		It("grabs nothing for a release whose name holds no title", func() {
			expectQueries([]*ent.TVShow{named(1, 11, "Мастер и Маргарита")}, nil)
			feed("S01E01.1080p.WEB-DL.x265-GRP", "!!!.S01E01.1080p.WEB-DL.x265-GRP")
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("matches a release named by an alias", func() {
			expectQueries([]*ent.TVShow{
				named(1, 11, "La casa de papel", "Money Heist"),
			}, nil)
			feed("Money.Heist.S01E01.1080p.WEB-DL.x265-GRP")
			expectGrab(11)
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("never lets an alias take another show's title", func() {
			// Show 2's alias is show 1's title; show 1 is outside both listings
			// this tick, and still owns its name.
			library = []*ent.TVShow{{ID: 1, Title: "Dark"}}
			expectQueries([]*ent.TVShow{named(2, 21, "Dark Matter", "Dark")}, nil)
			feed("Dark.S01E01.1080p.WEB-DL.x265-GRP")
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("matches neither of two shows sharing a title", func() {
			expectQueries([]*ent.TVShow{
				named(1, 11, "The Office"),
				named(2, 21, "The Office"),
			}, nil)
			feed("The.Office.S01E01.1080p.WEB-DL.x265-GRP")
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It(
			"matches neither of two shows sharing an alias, and each by its title",
			func() {
				expectQueries([]*ent.TVShow{
					named(1, 11, "Kaamelott", "Camelot"),
					named(2, 21, "Merlin", "Camelot"),
				}, nil)
				feed(
					"Camelot.S01E01.1080p.WEB-DL.x265-GRP",
					"Merlin.S01E01.1080p.WEB-DL.x265-GRP",
				)
				expectGrab(21)
				Expect(scanner.Run(ctx)).To(Succeed())
			},
		)

		It(
			"matches neither show when the wanted and upgrade listings split a title",
			func() {
				configtest.Setup(upgradeConfig("a"))
				newScanner()
				onDisk := named(2, 21, "The Office")
				onDisk.Edges.Seasons[0].Edges.Episodes[0].Edges.MediaFiles = []*ent.MediaFile{
					fileFixture(
						"/tv/The Office/S01E01.mkv",
						"The.Office.S01E01.720p.mkv",
						1280,
					),
				}
				expectQueries(
					[]*ent.TVShow{named(1, 11, "The Office")},
					[]*ent.TVShow{onDisk},
				)
				feed("The.Office.S01E01.2160p.MULTi.BluRay.x265-GRP")
				Expect(scanner.Run(ctx)).To(Succeed())
			},
		)

		It(
			"matches a title carrying a year against its release",
			func() {
				expectQueries([]*ent.TVShow{named(1, 11, "Foundation (2021)")}, nil)
				feed("Foundation.2021.S01E01.1080p.WEB-DL.x265-GRP")
				expectGrab(11)
				Expect(scanner.Run(ctx)).To(Succeed())
			},
		)

		It("grabs for the same-named show the release's year names", func() {
			classic := named(1, 11, "Doctor Who")
			classic.Year = 1963
			revival := named(2, 21, "Doctor Who (2005)")
			revival.Year = 2005
			expectQueries([]*ent.TVShow{classic, revival}, nil)
			feed(
				"Doctor.Who.2005.S01E01.1080p.WEB-DL.x265-GRP",
				// No year: either show could be meant.
				"Doctor.Who.S01E01.1080p.WEB-DL.x265-GRP",
			)
			expectGrab(21)
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("is not ambiguous with itself when a show is in both listings", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			wanted := named(1, 11, "The Black Sea")
			onDisk := named(1, 12, "The Black Sea")
			onDisk.Edges.Seasons[0].Edges.Episodes[0].Number = 2
			onDisk.Edges.Seasons[0].Edges.Episodes[0].Edges.MediaFiles = []*ent.MediaFile{
				fileFixture(
					"/tv/The Black Sea/S01E02.mkv",
					"The.Black.Sea.S01E02.720p.mkv",
					1280,
				),
			}
			library = []*ent.TVShow{{ID: 1, Title: "The Black Sea"}}
			expectQueries([]*ent.TVShow{wanted}, []*ent.TVShow{onDisk})
			feed("The.Black.Sea.S01E01.1080p.WEB-DL.x265-GRP")
			expectGrab(11)
			Expect(scanner.Run(ctx)).To(Succeed())
		})
	})
})

var _ = Describe("showKey", Label("unit", "rss"), func() {
	DescribeTable(
		"normalizes a title",
		func(title, want string) {
			Expect(showKey(title)).To(Equal(want))
		},
		Entry("dots and case", "The.Black.SEA", "the black sea"),
		Entry(
			"Cyrillic keeps its letters",
			"Мастер и Маргарита",
			"мастер и маргарита",
		),
		Entry("Cyrillic ё folds to е", "Ёлки", "елки"),
		Entry("Greek accents are dropped", "Ψυχή", "ψυχη"),
		Entry("CJK keeps its letters", "葬送のフリーレン", "葬送のフリーレン"),
		Entry("accented Latin", "Shōgun", "shogun"),
		Entry("decomposed input keys like composed input", "Shōgun", "shogun"),
		Entry(
			"Latin letters NFKD keeps whole",
			"Bjørk Straße Łódź",
			"bjork strasse lodz",
		),
		Entry("full-width digits", "Ｆｏｏ　１", "foo 1"),
		Entry("a fansub tag goes", "[SubsPlease] Frieren", "frieren"),
		Entry(
			"a fansub tag whose bracket was trimmed",
			"SubsPlease] 葬送のフリーレン - 18",
			"葬送のフリーレン 18",
		),
		Entry("punctuation only keys to nothing", "!!! ...", ""),
		Entry("empty", "", ""),
	)

	It("keeps the marks of a script whose marks are part of the letter", func() {
		// Dakuten: ブ and フ are different letters, so neither may fold into
		// the other — but both spellings of ブ still share a key.
		Expect(showKey("ブ")).NotTo(Equal(showKey("フ")))
		Expect(showKey("ブ")).To(Equal(showKey("ブ")))
	})
})

var _ = Describe("resolveShowNames", Label("unit", "rss"), func() {
	show := func(id uint32, title, original string, aliases ...string) *ent.TVShow {
		return &ent.TVShow{
			ID:            id,
			Title:         title,
			OriginalTitle: original,
			Aliases:       aliases,
		}
	}
	// resolve is the resolved show ID, 0 for none (fixture IDs start at 1).
	resolve := func(names showNames, key string, year uint16) uint32 {
		id, ok := names.resolve(key, year)
		if !ok {
			return 0
		}
		return id
	}

	It("maps every title, original title and alias to its show", func() {
		names := resolveShowNames(context.Background(),
			[]*ent.TVShow{show(1, "Shōgun", "将軍", "Shogun 2024")})
		Expect(names.claims).To(Equal(map[string][]uint32{
			"shogun": {1}, "将軍": {1}, "shogun 2024": {1},
		}))
	})

	It("lets a title outrank another show's alias", func() {
		names := resolveShowNames(context.Background(),
			[]*ent.TVShow{show(1, "Dark", "")},
			[]*ent.TVShow{show(2, "Dark Matter", "", "Dark")})
		Expect(resolve(names, "dark", 0)).To(Equal(uint32(1)))
		Expect(resolve(names, "dark matter", 0)).To(Equal(uint32(2)))
	})

	It("resolves a key two shows claim in the same tier to neither", func() {
		names := resolveShowNames(context.Background(),
			[]*ent.TVShow{show(1, "The Office", ""), show(3, "A", "", "Camelot")},
			[]*ent.TVShow{show(2, "The Office", ""), show(4, "B", "", "Camelot")})
		Expect(resolve(names, "the office", 0)).To(BeZero())
		Expect(resolve(names, "camelot", 2020)).To(BeZero())
		Expect(resolve(names, "a", 0)).To(Equal(uint32(3)))
	})

	It("is not ambiguous with itself across lists", func() {
		names := resolveShowNames(context.Background(),
			[]*ent.TVShow{show(1, "The Black Sea", "", "Mer Noire")},
			[]*ent.TVShow{show(1, "The Black Sea", "", "Mer Noire")})
		Expect(names.claims).To(Equal(map[string][]uint32{
			"the black sea": {1}, "mer noire": {1},
		}))
	})

	It("never claims the empty key", func() {
		names := resolveShowNames(context.Background(),
			[]*ent.TVShow{show(1, "!!!", "", "...")})
		Expect(names.claims).To(BeEmpty())
		Expect(resolve(names, "", 0)).To(BeZero())
	})

	It("keys a title without the year that tells it apart", func() {
		names := resolveShowNames(context.Background(),
			[]*ent.TVShow{show(1, "Foundation (2021)", "")})
		Expect(names.claims).To(Equal(map[string][]uint32{"foundation": {1}}))
		Expect(names.years[1]).To(Equal([]uint16{2021}))
		// The only show under its key: any year, or none, is still it.
		Expect(resolve(names, "foundation", 0)).To(Equal(uint32(1)))
		Expect(resolve(names, "foundation", 1999)).To(Equal(uint32(1)))
	})

	It("tells same-named shows apart by the release's year", func() {
		classic := show(1, "Doctor Who", "")
		classic.Year = 1963
		revival := show(2, "Doctor Who (2005)", "")
		revival.Year = 2005
		names := resolveShowNames(
			context.Background(),
			[]*ent.TVShow{classic, revival},
		)
		Expect(resolve(names, "doctor who", 2005)).To(Equal(uint32(2)))
		Expect(resolve(names, "doctor who", 1963)).To(Equal(uint32(1)))
		Expect(resolve(names, "doctor who", 0)).To(BeZero())
		Expect(resolve(names, "doctor who", 2023)).To(BeZero())
	})

	It("resolves to neither when both same-named shows answer to the year", func() {
		a, b := show(1, "The Office", ""), show(2, "The Office (2005)", "")
		a.Year, b.Year = 2005, 2005
		names := resolveShowNames(context.Background(), []*ent.TVShow{a, b})
		Expect(resolve(names, "the office", 2005)).To(BeZero())
	})
})
