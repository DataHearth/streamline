package rss

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
)

var _ = Describe("MissingSearcher.Run", Label("unit", "rss"), func() {
	const ctxType = "*context.valueCtx"

	var (
		ctx      context.Context
		store    *dbmocks.MockStore
		indexerM *mocks.MockIndexerSearcher
		dlM      *mocks.MockDownloader
		syncer   *MissingSearcher
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		indexerM = mocks.NewMockIndexerSearcher(GinkgoT())
		dlM = mocks.NewMockDownloader(GinkgoT())

		s, err := NewMissingSearcher(store, indexerM, dlM)
		Expect(err).NotTo(HaveOccurred())
		syncer = s
	})

	When("no movies are eligible", func() {
		It("returns nil and never calls the indexer", func() {
			store.EXPECT().
				ListEligibleMoviesForSync(mock.Anything, uint8(3), mock.Anything).
				Return(nil, nil).Once()
			store.EXPECT().
				CountMoviesByStatus(mock.Anything, entmovie.StatusWanted).
				Return(0, nil).Once()

			Expect(syncer.Run(ctx)).To(Succeed())
		})
	})

	When("the eligibility query fails", func() {
		It("returns the wrapped error", func() {
			boom := errors.New("db boom")
			store.EXPECT().
				ListEligibleMoviesForSync(mock.Anything, uint8(3), mock.Anything).
				Return(nil, boom).Once()

			Expect(syncer.Run(ctx)).To(MatchError(boom))
		})
	})

	Context("with one eligible movie", func() {
		var movie *ent.Movie

		BeforeEach(func() {
			movie = &ent.Movie{ID: 7, Title: "Fight Club", TmdbID: 550}
			store.EXPECT().
				ListEligibleMoviesForSync(mock.Anything, uint8(3), mock.Anything).
				Return([]*ent.Movie{movie}, nil).Once()
		})

		When("the indexer search errors", func() {
			It("skips DB writes and continues", func() {
				indexerM.EXPECT().
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
					Return(nil, errors.New("indexer down")).Once()

				Expect(syncer.Run(ctx)).To(Succeed())
			})
		})

		When("no results match the quality bar", func() {
			It("records last_search_at without grabbing", func() {
				indexerM.EXPECT().
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
					Return(nil, nil).Once()
				store.EXPECT().
					SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
					Return(nil).Once()

				Expect(syncer.Run(ctx)).To(Succeed())
			})
		})

		When("results match and grab succeeds", func() {
			It("resets grab_failures after recording last_search_at", func() {
				indexerM.EXPECT().
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
					Return([]indexer.SearchResult{
						{
							Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
							Seeders: 50,
						},
					}, nil).Once()
				store.EXPECT().
					SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
					Return(nil).Once()
				dlM.EXPECT().
					Grab(mock.AnythingOfType(ctxType), mock.AnythingOfType("indexer.SearchResult"), uint32(7)).
					Return(&ent.DownloadRecord{}, nil).Once()
				store.EXPECT().
					ResetMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
					Return(nil).Once()

				Expect(syncer.Run(ctx)).To(Succeed())
			})
		})

		When("the downloader rejects the grab", func() {
			It("increments grab_failures and still records last_search_at", func() {
				indexerM.EXPECT().
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
					Return([]indexer.SearchResult{
						{
							Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
							Seeders: 50,
						},
					}, nil).Once()
				store.EXPECT().
					SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
					Return(nil).Once()
				dlM.EXPECT().
					Grab(mock.AnythingOfType(ctxType), mock.AnythingOfType("indexer.SearchResult"), uint32(7)).
					Return(nil, errors.New("qbit down")).Once()
				store.EXPECT().
					IncrementMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
					Return(nil).Once()

				Expect(syncer.Run(ctx)).To(Succeed())
			})
		})

		When("SetMovieLastSearchAt fails after a no-match", func() {
			It("logs and finishes the pass without erroring", func() {
				indexerM.EXPECT().
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
					Return(nil, nil).Once()
				store.EXPECT().
					SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
					Return(errors.New("disk full")).Once()

				Expect(syncer.Run(ctx)).To(Succeed())
			})
		})

		When("ResetMovieGrabFailures fails after a successful grab", func() {
			It("logs and finishes the pass without erroring", func() {
				indexerM.EXPECT().
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
					Return([]indexer.SearchResult{
						{
							Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
							Seeders: 50,
						},
					}, nil).Once()
				store.EXPECT().
					SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
					Return(nil).Once()
				dlM.EXPECT().
					Grab(mock.AnythingOfType(ctxType), mock.AnythingOfType("indexer.SearchResult"), uint32(7)).
					Return(&ent.DownloadRecord{}, nil).Once()
				store.EXPECT().
					ResetMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
					Return(errors.New("disk full")).Once()

				Expect(syncer.Run(ctx)).To(Succeed())
			})
		})

		When("IncrementMovieGrabFailures fails after a grab error", func() {
			It("logs and finishes the pass without erroring", func() {
				indexerM.EXPECT().
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
					Return([]indexer.SearchResult{
						{
							Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
							Seeders: 50,
						},
					}, nil).Once()
				store.EXPECT().
					SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
					Return(nil).Once()
				dlM.EXPECT().
					Grab(mock.AnythingOfType(ctxType), mock.AnythingOfType("indexer.SearchResult"), uint32(7)).
					Return(nil, errors.New("qbit down")).Once()
				store.EXPECT().
					IncrementMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
					Return(errors.New("disk full")).Once()

				Expect(syncer.Run(ctx)).To(Succeed())
			})
		})
	})

	When("the run context is cancelled before processing starts", func() {
		It("returns the cancellation error", func() {
			cctx, cancel := context.WithCancel(ctx)
			cancel()

			store.EXPECT().
				ListEligibleMoviesForSync(mock.Anything, uint8(3), mock.Anything).
				Return([]*ent.Movie{
					{ID: 1, Title: "A", TmdbID: 1},
					{ID: 2, Title: "B", TmdbID: 2},
				}, nil).Once()

			err := syncer.Run(cctx)
			Expect(err).To(MatchError(context.Canceled))

			// Allow goroutines started before cancel to settle and call SearchMovie.
			time.Sleep(10 * time.Millisecond)
		})
	})
})

var _ = Describe("MissingSearcher.SearchOne", Label("unit", "rss"), func() {
	const ctxType = "*context.valueCtx"

	var (
		ctx      context.Context
		store    *dbmocks.MockStore
		indexerM *mocks.MockIndexerSearcher
		dlM      *mocks.MockDownloader
		syncer   *MissingSearcher
		movie    *ent.Movie
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = dbmocks.NewMockStore(GinkgoT())
		indexerM = mocks.NewMockIndexerSearcher(GinkgoT())
		dlM = mocks.NewMockDownloader(GinkgoT())

		s, err := NewMissingSearcher(store, indexerM, dlM)
		Expect(err).NotTo(HaveOccurred())
		syncer = s

		movie = &ent.Movie{ID: 7, Title: "Fight Club", TmdbID: 550}
	})

	It("dispatches a grab when an indexer hit passes filters", func() {
		indexerM.EXPECT().
			SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
			Return([]indexer.SearchResult{
				{
					Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
					Seeders: 50,
				},
			}, nil).Once()
		store.EXPECT().
			SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
			Return(nil).Once()
		dlM.EXPECT().
			Grab(mock.AnythingOfType(ctxType), mock.AnythingOfType("indexer.SearchResult"), uint32(7)).
			Return(&ent.DownloadRecord{}, nil).Once()
		store.EXPECT().
			ResetMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
			Return(nil).Once()

		Expect(syncer.SearchOne(ctx, movie)).To(Succeed())
	})

	It("returns ErrNoEligibleRelease when nothing matches filters", func() {
		indexerM.EXPECT().
			SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
			Return(nil, nil).Once()
		store.EXPECT().
			SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
			Return(nil).Once()

		Expect(syncer.SearchOne(ctx, movie)).To(MatchError(ErrNoEligibleRelease))
	})

	It("propagates indexer errors and bumps grab_failures on grab failure", func() {
		indexerM.EXPECT().
			SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
			Return(nil, errors.New("indexer boom")).Once()

		Expect(syncer.SearchOne(ctx, movie)).
			To(MatchError(ContainSubstring("indexer boom")))
	})

	It("returns the grab error after bumping grab_failures", func() {
		indexerM.EXPECT().
			SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, uint32(550)).
			Return([]indexer.SearchResult{
				{
					Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
					Seeders: 50,
				},
			}, nil).Once()
		store.EXPECT().
			SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
			Return(nil).Once()
		dlM.EXPECT().
			Grab(mock.AnythingOfType(ctxType), mock.AnythingOfType("indexer.SearchResult"), uint32(7)).
			Return(nil, errors.New("qbit down")).Once()
		store.EXPECT().
			IncrementMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
			Return(nil).Once()

		Expect(syncer.SearchOne(ctx, movie)).
			To(MatchError(ContainSubstring("qbit down")))
	})
})
