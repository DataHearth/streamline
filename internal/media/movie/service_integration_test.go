package movie

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
	mockposters "github.com/datahearth/streamline/internal/posters/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("MovieService end-to-end", Label("integration", "movies"), func() {
	var (
		ctx     context.Context
		client  *ent.Client
		store   *db.DB
		meta    *mockmeta.MockProvider
		posters *mockposters.MockManager
		svc     *Service
	)

	const profileName = "HD"

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = db.New(client)

		meta = mockmeta.NewMockProvider(GinkgoT())
		posters = mockposters.NewMockManager(GinkgoT())
		svc = NewService(store, meta, posters, nil)
		configtest.Setup(map[string]any{
			"metadata": map[string]any{"tmdb_region": ""},
			"quality_profiles": []map[string]any{{
				"name": profileName, "preferred_resolution": "1080p",
				"min_resolution": "720p",
			}},
			"quality_default_profile": profileName,
		})
	})

	Describe("Add", func() {
		It(
			"persists the movie row, resolves the default profile, and dispatches poster fetch",
			func() {
				meta.EXPECT().GetMovie(mock.Anything, uint32(157336)).
					Return(&metadata.MovieDetails{
						MovieResult: metadata.MovieResult{
							TMDBID:        157336,
							Title:         "Interstellar",
							OriginalTitle: "Interstellar",
							Year:          2014,
							Overview:      "A team travels through a wormhole.",
							PosterPath:    "/abc.jpg",
						},
					}, nil).Once()

				done := make(chan struct{})
				posters.EXPECT().
					Fetch(mock.Anything, "movies", mock.AnythingOfType("uint32"),
						"https://image.tmdb.org/t/p/original/abc.jpg").
					RunAndReturn(func(_ context.Context, _ string, _ uint32, _ string) error {
						close(done)
						return nil
					}).
					Once()

				m, posterPath, err := svc.Add(ctx, 157336, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(m.Title).To(Equal("Interstellar"))
				Expect(m.Year).To(Equal(uint16(2014)))
				Expect(m.Status).To(Equal(entmovie.StatusWanted))
				Expect(posterPath).To(Equal("/abc.jpg"))

				persisted, err := store.FindMovieByID(ctx, m.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(persisted.TmdbID).To(Equal(uint32(157336)))

				Eventually(done).Should(BeClosed())
			},
		)

		It("returns ErrNoQualityProfile when no profile exists", func() {
			configtest.Setup(map[string]any{
				"quality_profiles":        []any{},
				"quality_default_profile": "",
			})
			_, _, err := svc.Add(ctx, 1, "")
			Expect(err).To(MatchError(ErrNoQualityProfile))
		})

		It("rejects a duplicate add for the same TMDB id", func() {
			meta.EXPECT().GetMovie(mock.Anything, uint32(157336)).
				Return(&metadata.MovieDetails{
					MovieResult: metadata.MovieResult{
						TMDBID:        157336,
						Title:         "Interstellar",
						OriginalTitle: "Interstellar",
						Year:          2014,
					},
				}, nil).Twice()

			_, _, err := svc.Add(ctx, 157336, profileName)
			Expect(err).NotTo(HaveOccurred())

			_, _, err = svc.Add(ctx, 157336, profileName)
			Expect(err).To(MatchError(ContainSubstring("already exists")))
		})

		It(
			"under concurrent adds for the same TMDB id, persists only one row",
			func() {
				const concurrency = 4
				meta.EXPECT().GetMovie(mock.Anything, uint32(99)).
					Return(&metadata.MovieDetails{
						MovieResult: metadata.MovieResult{
							TMDBID:        99,
							Title:         "Solo",
							OriginalTitle: "Solo",
							Year:          2018,
						},
					}, nil).Times(concurrency)

				start := make(chan struct{})
				var wg sync.WaitGroup
				errs := make([]error, concurrency)
				for i := range concurrency {
					wg.Go(func() {
						<-start
						_, _, err := svc.Add(ctx, 99, profileName)
						errs[i] = err
					})
				}
				close(start)
				wg.Wait()

				var success, failure int
				for _, err := range errs {
					if err == nil {
						success++
						continue
					}
					if errors.Is(err, ErrNoQualityProfile) {
						Fail("unexpected ErrNoQualityProfile under concurrent add")
					}
					failure++
				}
				Expect(success).To(Equal(1))
				Expect(failure).To(Equal(concurrency - 1))

				rows, err := store.FindMoviesByTMDBIDs(ctx, []uint32{99})
				Expect(err).NotTo(HaveOccurred())
				Expect(rows).To(HaveLen(1))
			},
		)
	})

	Describe("FilterList against real schema", func() {
		seed := func(title string, tmdbID uint32, status entmovie.Status) {
			GinkgoHelper()
			_, err := store.CreateMovie(ctx, db.CreateMovieParams{
				Title: title, OriginalTitle: title, TmdbID: tmdbID, Status: status,
				QualityProfile: profileName,
			})
			Expect(err).NotTo(HaveOccurred())
		}

		BeforeEach(func() {
			seed("Alpha", 1, entmovie.StatusWanted)
			seed("Beta", 2, entmovie.StatusWanted)
			seed("Charlie", 3, entmovie.StatusWanted)
			seed("Delta", 4, entmovie.StatusAvailable)
		})

		It("filters by status, sorts by title asc, paginates", func() {
			items, total, err := svc.FilterList(ctx, FilterParams{
				Status: string(entmovie.StatusWanted),
				Sort:   "title", Order: "asc",
				Page: 1, Limit: 2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(3)))
			Expect(items).To(HaveLen(2))
			Expect(items[0].Title).To(Equal("Alpha"))
			Expect(items[1].Title).To(Equal("Beta"))
		})

		It("defaults page=1 limit=20 when zero", func() {
			items, total, err := svc.FilterList(ctx, FilterParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(4)))
			Expect(items).To(HaveLen(4))
		})

		It("returns the second page when paged", func() {
			items, total, err := svc.FilterList(ctx, FilterParams{
				Status: string(entmovie.StatusWanted),
				Sort:   "title", Order: "asc",
				Page: 2, Limit: 2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(3)))
			Expect(items).To(HaveLen(1))
			Expect(items[0].Title).To(Equal("Charlie"))
		})
	})

	Describe("Counts against real schema", func() {
		It("aggregates counts across statuses", func() {
			_, err := store.CreateMovie(ctx, db.CreateMovieParams{
				Title:          "A",
				OriginalTitle:  "A",
				TmdbID:         1,
				Status:         entmovie.StatusWanted,
				QualityProfile: profileName,
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = store.CreateMovie(ctx, db.CreateMovieParams{
				Title:          "B",
				OriginalTitle:  "B",
				TmdbID:         2,
				Status:         entmovie.StatusDownloading,
				QualityProfile: profileName,
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = store.CreateMovie(ctx, db.CreateMovieParams{
				Title:          "C",
				OriginalTitle:  "C",
				TmdbID:         3,
				Status:         entmovie.StatusAvailable,
				QualityProfile: profileName,
			})
			Expect(err).NotTo(HaveOccurred())

			c, err := svc.Counts(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Total).To(Equal(3))
			Expect(c.Wanted).To(Equal(1))
			Expect(c.Downloading).To(Equal(1))
			Expect(c.Available).To(Equal(1))
			// All three were just created → the trend rises to the total today.
			Expect(c.Trend).To(HaveLen(trendDays))
			Expect(c.Trend[trendDays-1]).To(Equal(3))
			Expect(sort.IntsAreSorted(c.Trend)).To(BeTrue())
		})
	})

	Describe("RefreshStale", func() {
		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"metadata": map[string]any{
					"tmdb_api_key": "test-key",
					"language":     "en",
					"tmdb_region":  "US",
				},
			})
		})

		It("refreshes metadata and persists the digital release date", func() {
			m, err := store.CreateMovie(ctx, db.CreateMovieParams{
				Title: "Old Title", OriginalTitle: "Old Title",
				Year: 2020, TmdbID: 555,
				Status: entmovie.StatusWanted, QualityProfile: profileName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Force the row past the refresh cutoff window so RefreshStale
			// considers it.
			past := time.Now().Add(-48 * time.Hour)
			_, err = client.Movie.UpdateOneID(m.ID).
				SetUpdateTime(past).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			drd := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
			meta.EXPECT().GetMovie(mock.Anything, uint32(555)).
				Return(&metadata.MovieDetails{
					MovieResult: metadata.MovieResult{
						TMDBID:        555,
						Title:         "New Title",
						OriginalTitle: "New Title",
						Year:          2020,
					},
				}, nil).Once()
			meta.EXPECT().
				FetchDigitalRelease(mock.Anything, uint32(555), "US").
				Return(&drd, nil).Once()

			Expect(svc.RefreshStale(ctx)).To(Succeed())

			refreshed, err := store.FindMovieByID(ctx, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.Title).To(Equal("New Title"))
			Expect(refreshed.DigitalReleaseDate).NotTo(BeNil())
			Expect(refreshed.DigitalReleaseDate.UTC()).To(Equal(drd))
		})

		It(
			"leaves digital_release_date untouched when the TMDB lookup errors",
			func() {
				m, err := store.CreateMovie(ctx, db.CreateMovieParams{
					Title: "Stable", OriginalTitle: "Stable",
					Year: 2021, TmdbID: 777,
					Status: entmovie.StatusWanted, QualityProfile: profileName,
				})
				Expect(err).NotTo(HaveOccurred())
				past := time.Now().Add(-48 * time.Hour)
				_, err = client.Movie.UpdateOneID(m.ID).
					SetUpdateTime(past).Save(ctx)
				Expect(err).NotTo(HaveOccurred())

				meta.EXPECT().GetMovie(mock.Anything, uint32(777)).
					Return(&metadata.MovieDetails{
						MovieResult: metadata.MovieResult{
							TMDBID:        777,
							Title:         "Stable",
							OriginalTitle: "Stable",
							Year:          2021,
						},
					}, nil).Once()
				meta.EXPECT().
					FetchDigitalRelease(mock.Anything, uint32(777), "US").
					Return(nil, errors.New("boom")).Once()

				Expect(svc.RefreshStale(ctx)).To(Succeed())

				refreshed, err := store.FindMovieByID(ctx, m.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(refreshed.DigitalReleaseDate).To(BeNil())
			},
		)
	})

	Describe("AnnotateTMDBResults against real schema", func() {
		It("flags rows whose tmdb_id matches an existing movie", func() {
			_, err := store.CreateMovie(ctx, db.CreateMovieParams{
				Title: "Existing", OriginalTitle: "Existing",
				TmdbID: 200, Status: entmovie.StatusWanted,
				QualityProfile: profileName,
			})
			Expect(err).NotTo(HaveOccurred())

			out, err := svc.AnnotateTMDBResults(ctx, []metadata.MovieResult{
				{TMDBID: 100, Title: "New"},
				{TMDBID: 200, Title: "Existing"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(2))
			Expect(out[0].AlreadyAdded).To(BeFalse())
			Expect(out[1].AlreadyAdded).To(BeTrue())
		})
	})
})
