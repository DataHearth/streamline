package rss

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
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
		expectEligible(nil, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return(nil, errors.New("boom")).Once()
		feeder.EXPECT().Feed(mock.Anything, "b").Return(nil, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("grabs a single episode matched by season+episode number", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		expectEligible([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
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
			expectEligible([]*ent.TVShow{showWith(
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
		expectEligible([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
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
		expectEligible([]*ent.TVShow{show}, nil)
		// 1080p clears the default profile but not the show's 2160p floor.
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{{Title: acceptableEp}}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("ignores an episode the show doesn't want", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		expectEligible([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "The.Black.Sea.S03E09.1080p.WEB-DL.x265-GRP"},
			}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("bumps grab_failures when the grab errors", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		expectEligible([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
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
		expectEligible([]*ent.TVShow{showWith(&ent.Episode{ID: 11, Number: 1})}, nil)
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
		expectEligible([]*ent.TVShow{show}, nil)
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
		expectEligible([]*ent.TVShow{show}, nil)
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
		expectEligible([]*ent.TVShow{showWith(
			&ent.Episode{ID: 11, Number: 1, AbsoluteNumber: 18},
		)}, nil)
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "The Black Sea - 18 [1080p][HEVC]"},
			}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})
})
