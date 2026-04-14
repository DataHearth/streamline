package movie

import (
	"context"
	"errors"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	mockdownload "github.com/datahearth/streamline/internal/download/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("MovieService unit", Label("unit", "movies"), func() {
	var (
		ctx          context.Context
		storeMock    *dbmocks.MockStore_Expecter
		metaMock     *mockmeta.MockProvider_Expecter
		fetchMock    *mockposters.MockManager_Expecter
		downloadMock *mockdownload.MockDownloader_Expecter
		posters      *mockposters.MockManager
		svc          *Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMock = store.EXPECT()
		meta := mockmeta.NewMockProvider(GinkgoT())
		metaMock = meta.EXPECT()
		posters = mockposters.NewMockManager(GinkgoT())
		fetchMock = posters.EXPECT()
		dl := mockdownload.NewMockDownloader(GinkgoT())
		downloadMock = dl.EXPECT()
		svc = NewService(store, meta, posters, dl)
		configtest.Setup(map[string]any{
			"metadata": map[string]any{"tmdb_region": ""},
		})
	})

	Describe("Add", func() {
		Context("when no quality profile is configured", func() {
			It("returns ErrNoQualityProfile", func() {
				configtest.Setup(map[string]any{
					"quality_profiles":        []any{},
					"quality_default_profile": "",
				})

				_, _, err := svc.Add(ctx, 1, "")
				Expect(err).To(MatchError(ErrNoQualityProfile))
			})
		})

		Context("with an explicit profile name", func() {
			It("surfaces metadata provider errors", func() {
				metaErr := errors.New("tmdb unreachable")
				metaMock.GetMovie(mock.Anything, uint32(42)).
					Return(nil, metaErr).Once()

				_, _, err := svc.Add(ctx, 42, "default")
				Expect(err).To(MatchError(ContainSubstring("fetch tmdb metadata")))
				Expect(err).To(MatchError(metaErr))
			})

			It("returns an already-exists error on constraint violation", func() {
				metaMock.GetMovie(mock.Anything, uint32(157336)).
					Return(&metadata.MovieDetails{
						MovieResult: metadata.MovieResult{
							TMDBID: 157336, Title: "Interstellar", Year: 2014,
						},
					}, nil).Once()
				storeMock.CreateMovie(mock.Anything, mock.AnythingOfType("db.CreateMovieParams")).
					Return(nil, &ent.ConstraintError{}).
					Once()

				_, _, err := svc.Add(ctx, 157336, "default")
				Expect(err).To(MatchError(ContainSubstring("already exists")))
			})

			It("wraps generic create errors", func() {
				metaMock.GetMovie(mock.Anything, uint32(1)).
					Return(&metadata.MovieDetails{
						MovieResult: metadata.MovieResult{TMDBID: 1, Title: "X"},
					}, nil).Once()
				createErr := errors.New("insert blew up")
				storeMock.CreateMovie(mock.Anything, mock.AnythingOfType("db.CreateMovieParams")).
					Return(nil, createErr).
					Once()

				_, _, err := svc.Add(ctx, 1, "default")
				Expect(err).To(MatchError(ContainSubstring("create movie")))
				Expect(err).To(MatchError(createErr))
			})

			It("dispatches poster fetch when TMDB returns a poster path", func() {
				metaMock.GetMovie(mock.Anything, uint32(157336)).
					Return(&metadata.MovieDetails{
						MovieResult: metadata.MovieResult{
							TMDBID: 157336, Title: "Interstellar", Year: 2014,
							PosterPath: "/abc.jpg",
						},
					}, nil).Once()
				storeMock.CreateMovie(mock.Anything, mock.MatchedBy(func(p db.CreateMovieParams) bool {
					return p.TmdbID == 157336 &&
						p.Title == "Interstellar" &&
						p.Year == 2014 &&
						p.QualityProfile == "default" &&
						p.Status == entmovie.StatusWanted
				})).
					Return(&ent.Movie{ID: 11, Title: "Interstellar", TmdbID: 157336}, nil).
					Once()

				done := make(chan struct{})
				fetchMock.Fetch(mock.Anything, "movies", uint32(11),
					"https://image.tmdb.org/t/p/original/abc.jpg").
					RunAndReturn(func(_ context.Context, _ string, _ uint32, _ string) error {
						close(done)
						return nil
					}).
					Once()

				m, posterPath, err := svc.Add(ctx, 157336, "default")
				Expect(err).NotTo(HaveOccurred())
				Expect(m.ID).To(Equal(uint32(11)))
				Expect(posterPath).To(Equal("/abc.jpg"))
				Eventually(done).Should(BeClosed())
			})

			It(
				"logs but does not fail Add when poster fetch returns an error",
				func() {
					metaMock.GetMovie(mock.Anything, uint32(157336)).
						Return(&metadata.MovieDetails{
							MovieResult: metadata.MovieResult{
								TMDBID: 157336, Title: "Interstellar", Year: 2014,
								PosterPath: "/abc.jpg",
							},
						}, nil).Once()
					storeMock.CreateMovie(mock.Anything, mock.AnythingOfType("db.CreateMovieParams")).
						Return(&ent.Movie{ID: 11, Title: "Interstellar", TmdbID: 157336}, nil).
						Once()

					done := make(chan struct{})
					fetchMock.Fetch(mock.Anything, "movies", uint32(11),
						"https://image.tmdb.org/t/p/original/abc.jpg").
						RunAndReturn(func(_ context.Context, _ string, _ uint32, _ string) error {
							close(done)
							return errors.New("network blew up")
						}).
						Once()

					_, _, err := svc.Add(ctx, 157336, "default")
					Expect(err).NotTo(HaveOccurred())
					Eventually(done).Should(BeClosed())
				},
			)

			It("skips poster fetch when TMDB has no poster path", func() {
				metaMock.GetMovie(mock.Anything, uint32(2)).
					Return(&metadata.MovieDetails{
						MovieResult: metadata.MovieResult{TMDBID: 2, Title: "NoArt"},
					}, nil).Once()
				storeMock.CreateMovie(mock.Anything, mock.AnythingOfType("db.CreateMovieParams")).
					Return(&ent.Movie{ID: 3, Title: "NoArt"}, nil).
					Once()

				_, posterPath, err := svc.Add(ctx, 2, "default")
				Expect(err).NotTo(HaveOccurred())
				Expect(posterPath).To(BeEmpty())
			})
		})
	})

	Describe("List", func() {
		It("rejects page=0", func() {
			_, _, err := svc.List(ctx, 0, 10)
			Expect(err).To(MatchError("page must be > 0"))
		})

		It("rejects limit=0", func() {
			_, _, err := svc.List(ctx, 1, 0)
			Expect(err).To(MatchError("limit must be > 0"))
		})

		It("wraps count errors", func() {
			countErr := errors.New("count blew up")
			storeMock.CountMovies(mock.Anything).Return(0, countErr).Once()

			_, _, err := svc.List(ctx, 1, 10)
			Expect(err).To(MatchError(ContainSubstring("count movies")))
			Expect(err).To(MatchError(countErr))
		})

		It("wraps list errors", func() {
			storeMock.CountMovies(mock.Anything).Return(1, nil).Once()
			listErr := errors.New("select blew up")
			storeMock.ListMovies(mock.Anything, uint32(0), uint32(10)).
				Return(nil, listErr).Once()

			_, _, err := svc.List(ctx, 1, 10)
			Expect(err).To(MatchError(ContainSubstring("list movies")))
			Expect(err).To(MatchError(listErr))
		})

		It("returns total + paginated rows", func() {
			storeMock.CountMovies(mock.Anything).Return(42, nil).Once()
			rows := []*ent.Movie{{ID: 1}, {ID: 2}}
			storeMock.ListMovies(mock.Anything, uint32(20), uint32(10)).
				Return(rows, nil).Once()

			items, total, err := svc.List(ctx, 3, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(42)))
			Expect(items).To(Equal(rows))
		})
	})

	Describe("FilterList", func() {
		It("defaults page=1 limit=20 when zero", func() {
			storeMock.FilterMovies(mock.Anything, mock.MatchedBy(func(p db.FilterMoviesParams) bool {
				return p.Offset == 0 && p.Limit == 20
			})).
				Return([]*ent.Movie{}, 0, nil).
				Once()

			_, _, err := svc.FilterList(ctx, FilterParams{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("computes offset from page+limit and forwards filters", func() {
			storeMock.FilterMovies(mock.Anything, mock.MatchedBy(func(p db.FilterMoviesParams) bool {
				return p.Offset == 4 && p.Limit == 2 &&
					p.Status == entmovie.StatusWanted &&
					p.Query == "inter" && p.Sort == "title" && p.Order == "asc"
			})).
				Return([]*ent.Movie{{ID: 1}}, 5, nil).
				Once()

			items, total, err := svc.FilterList(ctx, FilterParams{
				Status: string(entmovie.StatusWanted),
				Query:  "inter", Sort: "title", Order: "asc",
				Page: 3, Limit: 2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(total).To(Equal(uint32(5)))
		})

		It("wraps filter errors", func() {
			filterErr := errors.New("filter blew up")
			storeMock.FilterMovies(mock.Anything, mock.AnythingOfType("db.FilterMoviesParams")).
				Return(nil, 0, filterErr).
				Once()

			_, _, err := svc.FilterList(ctx, FilterParams{Page: 1, Limit: 10})
			Expect(err).To(MatchError(ContainSubstring("filter movies")))
			Expect(err).To(MatchError(filterErr))
		})
	})

	Describe("Get", func() {
		It("returns the movie when found", func() {
			storeMock.FindMovieByID(mock.Anything, uint32(7)).
				Return(&ent.Movie{ID: 7, Title: "Solo"}, nil).Once()

			m, err := svc.Get(ctx, 7)
			Expect(err).NotTo(HaveOccurred())
			Expect(m.ID).To(Equal(uint32(7)))
		})

		It("maps NotFound to a domain not-found error", func() {
			storeMock.FindMovieByID(mock.Anything, uint32(99)).
				Return(nil, &ent.NotFoundError{}).Once()

			_, err := svc.Get(ctx, 99)
			Expect(err).To(MatchError(ContainSubstring("movie 99 not found")))
		})

		It("wraps generic store errors", func() {
			storeErr := errors.New("query fail")
			storeMock.FindMovieByID(mock.Anything, uint32(1)).
				Return(nil, storeErr).Once()

			_, err := svc.Get(ctx, 1)
			Expect(err).To(MatchError(ContainSubstring("get movie")))
			Expect(err).To(MatchError(storeErr))
		})
	})

	Describe("GetByTMDBID", func() {
		It("returns the movie when one matches the tmdb id", func() {
			storeMock.FindMovieByTMDBID(mock.Anything, uint32(157336)).
				Return(&ent.Movie{ID: 1, TmdbID: 157336}, nil).Once()

			m, err := svc.GetByTMDBID(ctx, 157336)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(m.TmdbID).To(Equal(uint32(157336)))
		})

		It("returns (nil, nil) when no row matches", func() {
			storeMock.FindMovieByTMDBID(mock.Anything, uint32(99999)).
				Return(nil, &ent.NotFoundError{}).Once()

			m, err := svc.GetByTMDBID(ctx, 99999)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(BeNil())
		})

		It("wraps generic store errors", func() {
			storeMock.FindMovieByTMDBID(mock.Anything, uint32(1)).
				Return(nil, errors.New("query fail")).Once()

			_, err := svc.GetByTMDBID(ctx, 1)
			Expect(err).To(MatchError(ContainSubstring("get movie by tmdb_id")))
		})
	})

	Describe("Counts", func() {
		It("aggregates total + per-status counts", func() {
			storeMock.CountMovies(mock.Anything).Return(10, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusWanted).
				Return(4, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusDownloading).
				Return(2, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusAvailable).
				Return(3, nil).Once()
			storeMock.MovieCreateTimesSince(mock.Anything, mock.Anything).
				Return([]time.Time{}, nil).Once()

			c, err := svc.Counts(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Total).To(Equal(10))
			Expect(c.Wanted).To(Equal(4))
			Expect(c.Downloading).To(Equal(2))
			Expect(c.Available).To(Equal(3))
			// No recent additions → the whole window is the flat baseline (= total).
			Expect(c.Trend).To(HaveLen(trendDays))
			Expect(c.Trend).To(HaveEach(10))
			Expect(c.Trend[trendDays-1]).To(Equal(c.Total))
		})

		It("buckets recent additions into a rising trend ending at total", func() {
			storeMock.CountMovies(mock.Anything).Return(3, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, mock.Anything).
				Return(1, nil).Times(3)
			// Two added today, one yesterday; no prior baseline.
			now := time.Now().UTC()
			storeMock.MovieCreateTimesSince(mock.Anything, mock.Anything).
				Return([]time.Time{
					now.Add(-24 * time.Hour),
					now,
					now,
				}, nil).Once()

			c, err := svc.Counts(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Trend).To(HaveLen(trendDays))
			Expect(c.Trend[0]).To(Equal(0))           // baseline empty
			Expect(c.Trend[trendDays-1]).To(Equal(3)) // ends at total
			Expect(
				c.Trend[trendDays-2],
			).To(Equal(1))
			// yesterday's single add
			Expect(
				sort.IntsAreSorted(c.Trend),
			).To(BeTrue())
			// monotonic non-decreasing
		})

		It("wraps total count errors", func() {
			storeMock.CountMovies(mock.Anything).Return(0, errors.New("boom")).Once()
			_, err := svc.Counts(ctx)
			Expect(err).To(MatchError(ContainSubstring("count movies")))
		})

		It("wraps wanted count errors", func() {
			storeMock.CountMovies(mock.Anything).Return(1, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusWanted).
				Return(0, errors.New("boom")).Once()
			_, err := svc.Counts(ctx)
			Expect(err).To(MatchError(ContainSubstring("count wanted")))
		})

		It("wraps downloading count errors", func() {
			storeMock.CountMovies(mock.Anything).Return(1, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusWanted).
				Return(0, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusDownloading).
				Return(0, errors.New("boom")).Once()
			_, err := svc.Counts(ctx)
			Expect(err).To(MatchError(ContainSubstring("count downloading")))
		})

		It("wraps available count errors", func() {
			storeMock.CountMovies(mock.Anything).Return(1, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusWanted).
				Return(0, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusDownloading).
				Return(0, nil).Once()
			storeMock.CountMoviesByStatus(mock.Anything, entmovie.StatusAvailable).
				Return(0, errors.New("boom")).Once()
			_, err := svc.Counts(ctx)
			Expect(err).To(MatchError(ContainSubstring("count available")))
		})
	})

	Describe("Update", func() {
		It("returns the updated movie on success", func() {
			status := entmovie.StatusAvailable
			storeMock.UpdateMovie(mock.Anything, uint32(7),
				mock.MatchedBy(func(p db.UpdateMovieParams) bool {
					return p.Status != nil && *p.Status == entmovie.StatusAvailable
				})).Return(&ent.Movie{ID: 7, Status: entmovie.StatusAvailable}, nil).Once()

			m, err := svc.Update(ctx, 7, UpdateParams{Status: &status})
			Expect(err).NotTo(HaveOccurred())
			Expect(m.Status).To(Equal(entmovie.StatusAvailable))
		})

		It("maps NotFound to a domain not-found error", func() {
			storeMock.UpdateMovie(mock.Anything, uint32(99),
				mock.AnythingOfType("db.UpdateMovieParams")).
				Return(nil, &ent.NotFoundError{}).Once()

			_, err := svc.Update(ctx, 99, UpdateParams{})
			Expect(err).To(MatchError(ContainSubstring("movie 99 not found")))
		})

		It("wraps generic update errors", func() {
			updateErr := errors.New("update blew up")
			storeMock.UpdateMovie(mock.Anything, uint32(1),
				mock.AnythingOfType("db.UpdateMovieParams")).
				Return(nil, updateErr).Once()

			_, err := svc.Update(ctx, 1, UpdateParams{})
			Expect(err).To(MatchError(ContainSubstring("update movie")))
			Expect(err).To(MatchError(updateErr))
		})
	})

	Describe("AnnotateTMDBResults", func() {
		It("returns nil for empty input without querying", func() {
			out, err := svc.AnnotateTMDBResults(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(BeNil())
		})

		It("flags rows whose tmdb_id matches an existing movie", func() {
			input := []metadata.MovieResult{
				{TMDBID: 1, Title: "A"},
				{TMDBID: 2, Title: "B"},
				{TMDBID: 3, Title: "C"},
			}
			storeMock.FindMoviesByTMDBIDs(mock.Anything,
				mock.MatchedBy(func(ids []uint32) bool {
					return len(ids) == 3 &&
						ids[0] == 1 && ids[1] == 2 && ids[2] == 3
				})).Return([]*ent.Movie{
				{ID: 11, TmdbID: 2},
			}, nil).Once()

			out, err := svc.AnnotateTMDBResults(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(3))
			Expect(out[0].AlreadyAdded).To(BeFalse())
			Expect(out[1].AlreadyAdded).To(BeTrue())
			Expect(out[2].AlreadyAdded).To(BeFalse())
		})

		It("wraps store errors", func() {
			storeErr := errors.New("lookup blew up")
			storeMock.FindMoviesByTMDBIDs(mock.Anything, mock.Anything).
				Return(nil, storeErr).Once()

			_, err := svc.AnnotateTMDBResults(ctx,
				[]metadata.MovieResult{{TMDBID: 1}})
			Expect(err).To(MatchError(ContainSubstring("find movies by tmdb ids")))
			Expect(err).To(MatchError(storeErr))
		})
	})

	Describe("Delete", func() {
		It("calls store delete on success", func() {
			storeMock.DeleteMovie(mock.Anything, uint32(7)).Return(nil).Once()

			Expect(svc.Delete(ctx, 7, DeleteOptions{})).To(Succeed())
		})

		It("maps NotFound to a domain not-found error", func() {
			storeMock.DeleteMovie(mock.Anything, uint32(99)).
				Return(&ent.NotFoundError{}).Once()

			err := svc.Delete(ctx, 99, DeleteOptions{})
			Expect(err).To(MatchError(ContainSubstring("movie 99 not found")))
		})

		It("wraps generic delete errors", func() {
			deleteErr := errors.New("delete blew up")
			storeMock.DeleteMovie(mock.Anything, uint32(1)).Return(deleteErr).Once()

			err := svc.Delete(ctx, 1, DeleteOptions{})
			Expect(err).To(MatchError(ContainSubstring("delete movie")))
			Expect(err).To(MatchError(deleteErr))
		})
	})

	Describe("RefreshStale", func() {
		BeforeEach(func() {
			// Region empty → refreshOne skips the digital-release lookup,
			// so existing GetMovie+UpdateMovieMetadata-only assertions stay
			// valid without needing additional FetchDigitalRelease mocks.
			configtest.Setup(map[string]any{
				"metadata": map[string]any{"tmdb_region": ""},
			})
		})
		It("noops when no stale movies exist", func() {
			storeMock.ListMoviesStaleSince(mock.Anything, mock.AnythingOfType("time.Time")).
				Return(nil, nil).
				Once()
			Expect(svc.RefreshStale(ctx)).To(Succeed())
		})

		It("updates title/year/overview via UpdateMovieMetadata", func() {
			old := &ent.Movie{ID: 1, TmdbID: 42, Title: "Old", Year: 2023}
			storeMock.ListMoviesStaleSince(mock.Anything, mock.AnythingOfType("time.Time")).
				Return([]*ent.Movie{old}, nil).
				Once()
			metaMock.GetMovie(mock.Anything, uint32(42)).
				Return(&metadata.MovieDetails{
					MovieResult: metadata.MovieResult{
						TMDBID:        42,
						Title:         "New",
						OriginalTitle: "Nouveau",
						Year:          2024,
						Overview:      "fresh",
					},
				}, nil).
				Once()
			storeMock.UpdateMovieMetadata(
				mock.Anything, uint32(1), db.UpdateMovieMetadataParams{
					Title:         "New",
					OriginalTitle: "Nouveau",
					Overview:      "fresh",
					Year:          2024,
				},
			).Return(nil).Once()
			Expect(svc.RefreshStale(ctx)).To(Succeed())
		})

		It("skips a movie on provider error and continues with the rest", func() {
			m1 := &ent.Movie{ID: 1, TmdbID: 1, Title: "A"}
			m2 := &ent.Movie{ID: 2, TmdbID: 2, Title: "B"}
			storeMock.ListMoviesStaleSince(mock.Anything, mock.AnythingOfType("time.Time")).
				Return([]*ent.Movie{m1, m2}, nil).
				Once()
			metaMock.GetMovie(mock.Anything, uint32(1)).
				Return(nil, errors.New("tmdb 404")).Once()
			metaMock.GetMovie(mock.Anything, uint32(2)).
				Return(&metadata.MovieDetails{
					MovieResult: metadata.MovieResult{
						TMDBID:        2,
						Title:         "B",
						OriginalTitle: "B",
						Year:          2024,
					},
				}, nil).
				Once()
			storeMock.UpdateMovieMetadata(
				mock.Anything, uint32(2), db.UpdateMovieMetadataParams{
					Title:         "B",
					OriginalTitle: "B",
					Year:          2024,
				},
			).Return(nil).Once()
			Expect(svc.RefreshStale(ctx)).To(Succeed())
		})

		It("returns the DB error when ListMoviesStaleSince fails", func() {
			storeMock.ListMoviesStaleSince(mock.Anything, mock.AnythingOfType("time.Time")).
				Return(nil, errors.New("db down")).
				Once()
			err := svc.RefreshStale(ctx)
			Expect(err).To(MatchError(ContainSubstring("db down")))
		})
	})

	Describe("DeleteFile", func() {
		It(
			"deletes the file, reverts the movie, and removes the torrent when asked",
			func() {
				storeMock.FindMediaFileByID(mock.Anything, uint32(7)).
					Return(&ent.MediaFile{ID: 7, Path: "/lib/does-not-exist.mkv"}, nil).
					Once()
				storeMock.DeleteMediaFileAndRevertMovie(mock.Anything, uint32(7), uint32(3)).
					Return(nil).
					Once()
				storeMock.LatestImportedRecordForMovie(mock.Anything, uint32(3)).
					Return(&ent.DownloadRecord{TorrentHash: "H", DownloadClientName: "qb"}, nil).
					Once()
				downloadMock.RemoveTorrent(mock.Anything, "qb", "H").
					Return(nil).
					Once()

				err := svc.DeleteFile(
					ctx,
					3,
					7,
					DeleteFileOptions{RemoveTorrent: true},
				)
				Expect(err).ToNot(HaveOccurred())
			},
		)

		It("skips torrent removal when not requested", func() {
			storeMock.FindMediaFileByID(mock.Anything, uint32(7)).
				Return(&ent.MediaFile{ID: 7, Path: "/lib/does-not-exist.mkv"}, nil).
				Once()
			storeMock.DeleteMediaFileAndRevertMovie(mock.Anything, uint32(7), uint32(3)).
				Return(nil).
				Once()

			err := svc.DeleteFile(ctx, 3, 7, DeleteFileOptions{RemoveTorrent: false})
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns a not-found error when the media file is absent", func() {
			storeMock.FindMediaFileByID(mock.Anything, uint32(9)).
				Return(nil, &ent.NotFoundError{}).Once()

			err := svc.DeleteFile(ctx, 3, 9, DeleteFileOptions{})
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
	})
})
