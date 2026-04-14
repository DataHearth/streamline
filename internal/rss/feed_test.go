package rss

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func indexerConfig(names ...string) map[string]any {
	entries := make([]map[string]any, 0, len(names))
	for _, n := range names {
		entries = append(entries, map[string]any{
			"name": n, "host": "idx", "port": 9117, "api_key": "k",
			"protocol": "torznab", "enabled": true,
		})
	}
	return map[string]any{"indexers": entries}
}

var _ = Describe("FeedScanner.Run", Label("unit", "rss"), func() {
	var (
		ctx     context.Context
		store   *dbmocks.MockStore
		feeder  *mocks.MockIndexerFeeder
		grabber *mocks.MockDownloader
		scanner *FeedScanner
	)

	newScanner := func() {
		var err error
		scanner, err = NewFeedScanner(store, feeder, grabber)
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		feeder = mocks.NewMockIndexerFeeder(GinkgoT())
		grabber = mocks.NewMockDownloader(GinkgoT())
	})

	It("noops when no indexers are configured", func() {
		configtest.Setup()
		newScanner()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("continues to the next indexer on per-indexer error", func() {
		configtest.Setup(indexerConfig("a", "b"))
		newScanner()
		store.EXPECT().ListWantedMovies(mock.Anything).Return(nil, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return(nil, errors.New("boom")).Once()
		feeder.EXPECT().Feed(mock.Anything, "b").Return(nil, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("grabs on title+year match passing the quality filter", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.BluRay.x264-GROUP"},
			}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(7)).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().ResetMovieGrabFailures(mock.Anything, uint32(7)).
			Return(nil).Once()
		store.EXPECT().SetMovieLastSearchAt(
			mock.Anything, uint32(7), mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("rejects items that fail the quality filter without grabbing", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.480p.CAM.XViD-LOL"},
			}, nil).Once()
		// No grabber/store EXPECTs — quality reject means no further calls.
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("bumps grab_failures when grab errors", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.BluRay.x264-GROUP"},
			}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(7)).
			Return(nil, errors.New("client offline")).Once()
		store.EXPECT().IncrementMovieGrabFailures(mock.Anything, uint32(7)).
			Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("skips items whose titles have no parseable year", func() {
		configtest.Setup(indexerConfig("a"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Random Pack Without Year"},
			}, nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})

	It("only grabs the first match when two indexers return the same movie", func() {
		configtest.Setup(indexerConfig("a", "b"))
		newScanner()
		wanted := &ent.Movie{ID: 7, TmdbID: 42, Title: "Dune", Year: 2021}
		store.EXPECT().ListWantedMovies(mock.Anything).
			Return([]*ent.Movie{wanted}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "a").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.BluRay.x264-GROUP"},
			}, nil).Once()
		feeder.EXPECT().Feed(mock.Anything, "b").
			Return([]indexer.SearchResult{
				{Title: "Dune.2021.1080p.WEB-DL.x265-OTHER"},
			}, nil).Once()
		grabber.EXPECT().Grab(mock.Anything, mock.Anything, uint32(7)).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().ResetMovieGrabFailures(mock.Anything, uint32(7)).
			Return(nil).Once()
		store.EXPECT().SetMovieLastSearchAt(
			mock.Anything, uint32(7), mock.AnythingOfType("time.Time"),
		).Return(nil).Once()
		Expect(scanner.Run(ctx)).To(Succeed())
	})
})
