package rss

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/tvshow"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("TVFeedScanner.Run", Label("unit", "rss"), func() {
	var (
		ctx     context.Context
		store   *dbmocks.MockStore
		feeder  *mocks.MockIndexerFeeder
		grabber *mocks.MockEpisodeGrabber
		scanner *TVFeedScanner
	)

	newScanner := func() {
		scanner = NewTVFeedScanner(store, feeder, grabber)
	}

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		feeder = mocks.NewMockIndexerFeeder(GinkgoT())
		grabber = mocks.NewMockEpisodeGrabber(GinkgoT())
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
			GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
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
				GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
				Return(&ent.DownloadRecord{}, nil).Once()
			expectDownloading(11)
			expectDownloading(12)
			Expect(scanner.Run(ctx)).To(Succeed())
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
			GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
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
			GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
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
			GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
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
			GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
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
			remuxEpFile = "The.Black.Sea.S03E02.1080p.BluRay.REMUX.x264-GRP.mkv"

			betterEp   = "The.Black.Sea.S03E01.2160p.BluRay.REMUX.HDR.x265-GRP"
			betterPack = "The.Black.Sea.S03.2160p.BluRay.REMUX.HDR.x265-GRP"
			// remux only: ties the 200 a remux file already scores.
			tiedPack = "The.Black.Sea.S03.1080p.BluRay.REMUX.x264-GRP"
		)

		// epWithFile builds an upgrade candidate episode: one on disk, 1080p by
		// both its name and its probed width.
		epWithFile := func(id uint32, number uint16, basename string) *ent.Episode {
			e := &ent.Episode{ID: id, Number: number}
			e.Edges.MediaFiles = []*ent.MediaFile{{
				Path:       "/tv/The Black Sea/Season 03/" + basename,
				Size:       4_000_000_000,
				Width:      1920,
				VideoCodec: "h264",
			}}
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
				GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(55), downloadrecord.ReplaceModeAll,
				).
				Return(nil).Once()
			expectDownloading(11)
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("upgrades a whole season against its best file", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			expectQueries(nil, []*ent.TVShow{showWith(
				epWithFile(11, 1, plainEpFile),
				epWithFile(12, 2, remuxEpFile),
			)})
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: betterPack}}, nil).Once()
			// One record, against the first episode of the season.
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			store.EXPECT().
				SetDownloadRecordReplaceMode(
					mock.Anything, uint32(55), downloadrecord.ReplaceModeAll,
				).
				Return(nil).Once()
			expectDownloading(11)
			expectDownloading(12)
			Expect(scanner.Run(ctx)).To(Succeed())
		})

		It("leaves the season alone when the pack only ties its best file", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			// E01 alone would be upgraded by this pack; E02's remux is what
			// stops it, and a pack has no per-episode veto.
			expectQueries(nil, []*ent.TVShow{showWith(
				epWithFile(11, 1, plainEpFile),
				epWithFile(12, 2, remuxEpFile),
			)})
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: tiedPack}}, nil).Once()
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

		It("fills a wanted episode rather than upgrading the season", func() {
			configtest.Setup(upgradeConfig("a"))
			newScanner()
			// Same show in both listings: E01 missing, E02 on disk. The pack is
			// grabbed for the hole, and the file it doesn't beat stays put.
			expectQueries(
				[]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})},
				[]*ent.TVShow{showWith(epWithFile(12, 2, remuxEpFile))},
			)
			feeder.EXPECT().Feed(mock.Anything, "a").
				Return([]indexer.SearchResult{{Title: tiedPack}}, nil).Once()
			grabber.EXPECT().
				GrabEpisode(mock.Anything, mock.Anything, uint32(11)).
				Return(&ent.DownloadRecord{ID: 55}, nil).Once()
			expectDownloading(11)
			Expect(scanner.Run(ctx)).To(Succeed())
		})
	})
})
