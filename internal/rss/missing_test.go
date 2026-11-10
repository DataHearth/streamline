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
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/rss/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/roottest"
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
		// Nothing rejected; blocklist_integration_test.go covers the filter.
		store.EXPECT().RejectedReleases(mock.Anything).Return(nil, nil).Maybe()
		indexerM = mocks.NewMockIndexerSearcher(GinkgoT())
		dlM = mocks.NewMockDownloader(GinkgoT())

		syncer = NewMissingSearcher(store, indexerM, dlM)
	})

	When("a library root is offline", func() {
		It("grabs nothing for offline movies", func() {
			roottest.Offline(db.RootMovies)

			Expect(syncer.Run(ctx)).To(Succeed())
		})

		It("still runs while another root is offline", func() {
			roottest.Offline(db.RootMusic)
			store.EXPECT().
				ListEligibleMoviesForSync(mock.Anything, uint8(3), mock.Anything).
				Return(nil, nil).Once()
			store.EXPECT().
				CountMoviesByStatus(mock.Anything, entmovie.StatusWanted).
				Return(0, nil).Once()

			Expect(syncer.Run(ctx)).To(Succeed())
		})
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

	When("no download client is enabled", func() {
		It("skips the pass after the eligibility query", func() {
			overlay := defaultRSSConfig()
			overlay["download_clients"] = []map[string]any{}
			configtest.Setup(overlay)

			// The count still runs — an operator staring at the logs needs to
			// know how much work is queued behind the missing client.
			store.EXPECT().
				ListEligibleMoviesForSync(mock.Anything, uint8(3), mock.Anything).
				Return([]*ent.Movie{{ID: 7, Title: "Fight Club", TmdbID: 550}}, nil).
				Once()

			Expect(syncer.Run(ctx)).To(Succeed())
			// No SearchMovie, no Grab, no grab_failures write: the mocks would
			// fail the spec on any unexpected call.
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

	When("the library knobs change after construction", func() {
		It("queries with the edited cap and cooldown", func() {
			overlay := defaultRSSConfig()
			library := overlay["library"].(map[string]any)
			library["max_grab_failures"] = 7
			library["no_match_cooldown"] = "1h"
			configtest.Setup(overlay)

			var cutoff time.Time
			store.EXPECT().
				ListEligibleMoviesForSync(
					mock.Anything,
					uint8(7),
					mock.AnythingOfType("time.Time"),
				).
				Run(func(_ context.Context, _ uint8, notSearchedSince time.Time) {
					cutoff = notSearchedSince
				}).
				Return(nil, nil).Once()
			store.EXPECT().
				CountMoviesByStatus(mock.Anything, entmovie.StatusWanted).
				Return(0, nil).Once()

			Expect(syncer.Run(ctx)).To(Succeed())
			Expect(cutoff).To(
				BeTemporally("~", time.Now().Add(-time.Hour), time.Minute),
			)
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
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
					Return(nil, errors.New("indexer down")).Once()

				Expect(syncer.Run(ctx)).To(Succeed())
			})
		})

		When("no results match the quality bar", func() {
			It("records last_search_at without grabbing", func() {
				indexerM.EXPECT().
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
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
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
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
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
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
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
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
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
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
					SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
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
		// Nothing rejected; blocklist_integration_test.go covers the filter.
		store.EXPECT().RejectedReleases(mock.Anything).Return(nil, nil).Maybe()
		indexerM = mocks.NewMockIndexerSearcher(GinkgoT())
		dlM = mocks.NewMockDownloader(GinkgoT())

		syncer = NewMissingSearcher(store, indexerM, dlM)

		movie = &ent.Movie{ID: 7, Title: "Fight Club", TmdbID: 550}
	})

	It("dispatches a grab when an indexer hit passes filters", func() {
		indexerM.EXPECT().
			SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
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

	It(
		"keeps a search-now running after the request that asked for it is gone",
		func() {
			reqCtx, cancel := context.WithCancel(ctx)
			release := make(chan struct{})
			DeferCleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			searchErr := make(chan error, 1)
			indexerM.EXPECT().
				SearchMovie(mock.Anything, []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
				Run(func(c context.Context, _, _ []string, _ uint32, _ string) {
					<-release
					searchErr <- c.Err()
				}).
				Return(nil, nil).Once()
			stamped := make(chan struct{})
			store.EXPECT().
				SetMovieLastSearchAt(mock.Anything, uint32(7), mock.AnythingOfType("time.Time")).
				Run(func(context.Context, uint32, time.Time) { close(stamped) }).
				Return(nil).Once()

			returned := make(chan struct{})
			go func() {
				syncer.SearchNow(reqCtx, movie)
				close(returned)
			}()
			Eventually(returned).Should(BeClosed())
			cancel()
			close(release)

			Eventually(searchErr).Should(Receive(BeNil()))
			Eventually(stamped).Should(BeClosed())
		},
	)

	It("returns ErrNoEligibleRelease when nothing matches filters", func() {
		indexerM.EXPECT().
			SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
			Return(nil, nil).Once()
		store.EXPECT().
			SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
			Return(nil).Once()

		Expect(syncer.SearchOne(ctx, movie)).To(MatchError(ErrNoEligibleRelease))
	})

	It("propagates indexer errors and bumps grab_failures on grab failure", func() {
		indexerM.EXPECT().
			SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
			Return(nil, errors.New("indexer boom")).Once()

		Expect(syncer.SearchOne(ctx, movie)).
			To(MatchError(ContainSubstring("indexer boom")))
	})

	It("returns the grab error after bumping grab_failures", func() {
		indexerM.EXPECT().
			SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
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

	Context("when several results clear the quality bar", func() {
		It("grabs the highest-scoring release, not the first listed", func() {
			movie.QualityProfile = scoredProfile
			remux := indexer.SearchResult{
				Title:   "Fight.Club.1999.2160p.BluRay.REMUX.x265-GROUP",
				Seeders: 3,
			}
			indexerM.EXPECT().
				SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
				Return([]indexer.SearchResult{
					{
						Title:   "Fight.Club.1999.1080p.WEB-DL.x264-GROUP",
						Seeders: 900,
					},
					remux,
				}, nil).Once()
			store.EXPECT().
				SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
				Return(nil).Once()
			dlM.EXPECT().
				Grab(mock.AnythingOfType(ctxType), remux, uint32(7)).
				Return(&ent.DownloadRecord{}, nil).Once()
			store.EXPECT().
				ResetMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
				Return(nil).Once()

			Expect(syncer.SearchOne(ctx, movie)).To(Succeed())
		})

		It("breaks a score tie on seeders, whatever the indexer order", func() {
			seeded := indexer.SearchResult{
				Title:   "Fight.Club.1999.1080p.BluRay.x264-OTHER",
				Seeders: 400,
			}
			indexerM.EXPECT().
				SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
				Return([]indexer.SearchResult{
					{
						Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
						Seeders: 5,
					},
					seeded,
				}, nil).Once()
			store.EXPECT().
				SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
				Return(nil).Once()
			dlM.EXPECT().
				Grab(mock.AnythingOfType(ctxType), seeded, uint32(7)).
				Return(&ent.DownloadRecord{}, nil).Once()
			store.EXPECT().
				ResetMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
				Return(nil).Once()

			Expect(syncer.SearchOne(ctx, movie)).To(Succeed())
		})
	})

	Context("when another film shares the title", func() {
		BeforeEach(func() {
			movie = &ent.Movie{ID: 7, Title: "Road House", Year: 1989, TmdbID: 10135}
		})

		expectSearch := func(results ...indexer.SearchResult) {
			indexerM.EXPECT().
				SearchMovie(mock.AnythingOfType(ctxType), []string{"Road House", ""}, mock.Anything, uint32(10135), "").
				Return(results, nil).Once()
			store.EXPECT().
				SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
				Return(nil).Once()
		}

		It(
			"grabs the release of the wanted year over a better-seeded remake",
			func() {
				wanted := indexer.SearchResult{
					Title:   "Road.House.1989.1080p.BluRay.x264-GROUP",
					Seeders: 5,
				}
				expectSearch(
					indexer.SearchResult{
						Title:   "Road.House.2024.1080p.BluRay.x264-OTHER",
						Seeders: 400,
					},
					wanted,
				)
				dlM.EXPECT().
					Grab(mock.AnythingOfType(ctxType), wanted, uint32(7)).
					Return(&ent.DownloadRecord{}, nil).Once()
				store.EXPECT().
					ResetMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
					Return(nil).Once()

				Expect(syncer.SearchOne(ctx, movie)).To(Succeed())
			},
		)

		It("grabs nothing when only the other film is on offer", func() {
			expectSearch(indexer.SearchResult{
				Title:   "Road.House.2024.1080p.BluRay.x264-OTHER",
				Seeders: 400,
			})

			Expect(syncer.SearchOne(ctx, movie)).
				To(MatchError(ErrNoEligibleRelease))
		})

		It("still grabs a release that states no year", func() {
			bare := indexer.SearchResult{
				Title:   "Road.House.1080p.BluRay.x264-GROUP",
				Seeders: 5,
			}
			expectSearch(bare)
			dlM.EXPECT().
				Grab(mock.AnythingOfType(ctxType), bare, uint32(7)).
				Return(&ent.DownloadRecord{}, nil).Once()
			store.EXPECT().
				ResetMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
				Return(nil).Once()

			Expect(syncer.SearchOne(ctx, movie)).To(Succeed())
		})
	})

	DescribeTable("namesOtherYear",
		func(release string, m *ent.Movie, want bool) {
			Expect(namesOtherYear(release, m)).To(Equal(want))
		},
		Entry("the wanted year",
			"Road.House.1989.1080p.BluRay.x264",
			&ent.Movie{Title: "Road House", Year: 1989}, false),
		Entry("a remake decades later",
			"Road.House.2024.1080p.WEB.x264",
			&ent.Movie{Title: "Road House", Year: 1989}, true),
		Entry("one year late, a festival premiere against its release",
			"Road.House.1990.1080p.BluRay.x264",
			&ent.Movie{Title: "Road House", Year: 1989}, false),
		Entry("one year early",
			"Road.House.1988.1080p.BluRay.x264",
			&ent.Movie{Title: "Road House", Year: 1989}, false),
		Entry("two years off",
			"Road.House.1991.1080p.BluRay.x264",
			&ent.Movie{Title: "Road House", Year: 1989}, true),
		Entry("no year in the release",
			"Road.House.1080p.BluRay.x264",
			&ent.Movie{Title: "Road House", Year: 1989}, false),
		Entry("no year on the movie",
			"Road.House.2024.1080p.WEB.x264",
			&ent.Movie{Title: "Road House"}, false),
		Entry("a title opening on a number, year stated",
			"2001.A.Space.Odyssey.1968.1080p.BluRay.x264",
			&ent.Movie{Title: "2001: A Space Odyssey", Year: 1968}, false),
		Entry("a title opening on a number, no year",
			"2001.A.Space.Odyssey.1080p.BluRay.x264",
			&ent.Movie{Title: "2001: A Space Odyssey", Year: 1968}, false),
		Entry("a title that is a number, year stated",
			"1917.2019.1080p.BluRay.x264",
			&ent.Movie{Title: "1917", Year: 2019}, false),
		Entry("a title that is a number, no year",
			"1917.1080p.BluRay.x264",
			&ent.Movie{Title: "1917", Year: 2019}, false),
		Entry("a title ending on a number, year stated",
			"Blade.Runner.2049.2017.1080p.BluRay.x264",
			&ent.Movie{Title: "Blade Runner 2049", Year: 2017}, false),
		Entry("a title ending on a number the parser reads as the year",
			"Blade.Runner.2049.1080p.BluRay.x264",
			&ent.Movie{Title: "Blade Runner 2049", Year: 2017}, false),
		Entry("that number in the original title only",
			"Wonder.Woman.1984.1080p.WEB.x264",
			&ent.Movie{
				Title:         "Wonder Woman 84",
				OriginalTitle: "Wonder Woman 1984",
				Year:          2020,
			}, false),
		Entry("that number in an alias only",
			"Death.Race.2000.1080p.BluRay.x264",
			&ent.Movie{
				Title:   "La Course à la mort de l'an 2000",
				Aliases: []string{"Death Race 2000"},
				Year:    1975,
			}, false),
		Entry("the film's year, then the year of the cut",
			"Blade.Runner.1982.The.Final.Cut.2007.1080p.BluRay.x264-GRP",
			&ent.Movie{Title: "Blade Runner", Year: 1982}, false),
		Entry("the film's year, then the year of the restoration",
			"Metropolis.1927.Restored.2010.1080p.BluRay.x264-GRP",
			&ent.Movie{Title: "Metropolis", Year: 1927}, false),
		Entry("the film's year, then the year of the remaster",
			"Les.Visiteurs.1993.MULTI.1080p.BluRay.REMASTER.2023.x264",
			&ent.Movie{Title: "Les Visiteurs", Year: 1993}, false),
		Entry("the film's year, then a bracketed disc year",
			"Road.House.1989.1080p.BluRay.x264-GRP[2024]",
			&ent.Movie{Title: "Road House", Year: 1989}, false),
		Entry("two years, neither the film's",
			"Road.House.2024.1080p.BluRay.REMASTER.2025.x264",
			&ent.Movie{Title: "Road House", Year: 1989}, true),
		Entry("the sequel's number against the first film",
			"Blade.Runner.2049.1080p.BluRay.x264",
			&ent.Movie{Title: "Blade Runner", Year: 1982}, true),
		Entry("a numbered title with another film's year",
			"Death.Race.2000.2008.1080p.BluRay.x264",
			&ent.Movie{Title: "Death Race 2000", Year: 1975}, true),
	)

	Context("when the movie pins a quality profile", func() {
		BeforeEach(func() { movie.QualityProfile = uhdProfile })

		It("rejects a release that only clears the default profile", func() {
			indexerM.EXPECT().
				SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
				Return([]indexer.SearchResult{
					{
						Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
						Seeders: 50,
					},
				}, nil).Once()
			store.EXPECT().
				SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
				Return(nil).Once()

			Expect(syncer.SearchOne(ctx, movie)).To(MatchError(ErrNoEligibleRelease))
		})

		It("grabs the release clearing the pinned floor", func() {
			uhd := indexer.SearchResult{
				Title:   "Fight.Club.1999.2160p.BluRay.x265-GROUP",
				Seeders: 5,
			}
			indexerM.EXPECT().
				SearchMovie(mock.AnythingOfType(ctxType), []string{"Fight Club", ""}, mock.Anything, uint32(550), "").
				Return([]indexer.SearchResult{
					{
						Title:   "Fight.Club.1999.1080p.BluRay.x264-GROUP",
						Seeders: 50,
					},
					uhd,
				}, nil).Once()
			store.EXPECT().
				SetMovieLastSearchAt(mock.AnythingOfType(ctxType), uint32(7), mock.AnythingOfType("time.Time")).
				Return(nil).Once()
			dlM.EXPECT().
				Grab(mock.AnythingOfType(ctxType), uhd, uint32(7)).
				Return(&ent.DownloadRecord{}, nil).Once()
			store.EXPECT().
				ResetMovieGrabFailures(mock.AnythingOfType(ctxType), uint32(7)).
				Return(nil).Once()

			Expect(syncer.SearchOne(ctx, movie)).To(Succeed())
		})
	})
})
