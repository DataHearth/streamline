package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	entmovie "github.com/datahearth/streamline/ent/movie"
)

var _ = Describe("Movie filter + lookup", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
	)

	const qualityProfile = "HD"

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
	})

	seed := func(title string, year uint16, tmdbID uint32, status entmovie.Status) *ent.Movie {
		GinkgoHelper()
		m, err := store.CreateMovie(ctx, CreateMovieParams{
			Title:          title,
			OriginalTitle:  title,
			Year:           year,
			TmdbID:         tmdbID,
			Status:         status,
			QualityProfile: qualityProfile,
		})
		Expect(err).NotTo(HaveOccurred())
		return m
	}

	Describe("FilterMovies", func() {
		It("filters by status and query, sorts by title asc, paginates", func() {
			seed("Alpha", 2020, 1, entmovie.StatusWanted)
			seed("Beta", 2021, 2, entmovie.StatusWanted)
			seed("Charlie", 2022, 3, entmovie.StatusWanted)
			seed("Delta", 2019, 4, entmovie.StatusAvailable)

			items, total, err := store.FilterMovies(ctx, FilterMoviesParams{
				Status: entmovie.StatusWanted,
				Sort:   "title", Order: "asc",
				Offset: 0, Limit: 2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(3))
			Expect(items).To(HaveLen(2))
			Expect(items[0].Title).To(Equal("Alpha"))
			Expect(items[1].Title).To(Equal("Beta"))
		})

		It("substring-matches title case-insensitively", func() {
			seed("The Matrix", 1999, 10, entmovie.StatusWanted)
			seed("Gone Girl", 2014, 11, entmovie.StatusWanted)
			items, total, err := store.FilterMovies(ctx, FilterMoviesParams{
				Query: "MATRIX", Limit: 10,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(1))
			Expect(items[0].Title).To(Equal("The Matrix"))
		})

		It("orders by create_time desc by default", func() {
			a := seed("A", 2020, 20, entmovie.StatusWanted)
			b := seed("B", 2020, 21, entmovie.StatusWanted)
			items, _, err := store.FilterMovies(ctx, FilterMoviesParams{Limit: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(items[0].ID).To(Equal(b.ID))
			Expect(items[1].ID).To(Equal(a.ID))
		})

		It("orders by create_time asc when order=asc and sort empty", func() {
			a := seed("A", 2020, 30, entmovie.StatusWanted)
			b := seed("B", 2020, 31, entmovie.StatusWanted)
			items, _, err := store.FilterMovies(ctx, FilterMoviesParams{
				Order: "asc", Limit: 10,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(items[0].ID).To(Equal(a.ID))
			Expect(items[1].ID).To(Equal(b.ID))
		})

		It("sorts by title desc", func() {
			seed("Alpha", 2020, 40, entmovie.StatusWanted)
			seed("Beta", 2020, 41, entmovie.StatusWanted)
			items, _, err := store.FilterMovies(ctx, FilterMoviesParams{
				Sort: "title", Order: "desc", Limit: 10,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(items[0].Title).To(Equal("Beta"))
			Expect(items[1].Title).To(Equal("Alpha"))
		})

		It("sorts by year asc", func() {
			seed("Old", 1999, 50, entmovie.StatusWanted)
			seed("New", 2024, 51, entmovie.StatusWanted)
			items, _, err := store.FilterMovies(ctx, FilterMoviesParams{
				Sort: "year", Order: "asc", Limit: 10,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items[0].Year).To(Equal(uint16(1999)))
			Expect(items[1].Year).To(Equal(uint16(2024)))
		})

		It("sorts by year desc", func() {
			seed("Old", 1999, 60, entmovie.StatusWanted)
			seed("New", 2024, 61, entmovie.StatusWanted)
			items, _, err := store.FilterMovies(ctx, FilterMoviesParams{
				Sort: "year", Order: "desc", Limit: 10,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items[0].Year).To(Equal(uint16(2024)))
			Expect(items[1].Year).To(Equal(uint16(1999)))
		})
	})

	Describe("FindMovieByTMDBID", func() {
		It("returns NotFound when absent", func() {
			_, err := store.FindMovieByTMDBID(ctx, 9999)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
		It("returns the row when present", func() {
			m := seed("Dune", 2021, 438631, entmovie.StatusWanted)
			got, err := store.FindMovieByTMDBID(ctx, 438631)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(m.ID))
		})
	})

	Describe("FindMoviesByTMDBIDs", func() {
		It("returns the matching rows", func() {
			a := seed("Dune", 2021, 700, entmovie.StatusWanted)
			seed("Other", 2020, 701, entmovie.StatusWanted)
			items, err := store.FindMoviesByTMDBIDs(ctx, []uint32{700})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal(a.ID))
		})
	})

	Describe("UpdateMovie", func() {
		It("updates status and quality profile when both provided", func() {
			m := seed("Dune", 2021, 800, entmovie.StatusWanted)

			newStatus := entmovie.StatusAvailable
			newProfile := "4K"
			updated, err := store.UpdateMovie(ctx, m.ID, UpdateMovieParams{
				Status:         &newStatus,
				QualityProfile: &newProfile,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status).To(Equal(entmovie.StatusAvailable))
			Expect(updated.QualityProfile).To(Equal("4K"))
		})
	})

	Describe("CreateMovie", func() {
		It("persists optional overview when provided", func() {
			m, err := store.CreateMovie(ctx, CreateMovieParams{
				Title: "Dune", OriginalTitle: "Dune",
				Year: 2021, TmdbID: 900,
				Status:   entmovie.StatusWanted,
				Overview: "Spice planet", QualityProfile: qualityProfile,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(m.Overview).To(Equal("Spice planet"))
		})
	})

	Describe("FindMovieByID", func() {
		It("returns the row with its quality profile name", func() {
			m := seed("Dune", 2021, 901, entmovie.StatusWanted)
			got, err := store.FindMovieByID(ctx, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.QualityProfile).To(Equal(qualityProfile))
		})

		It("returns NotFound when absent", func() {
			_, err := store.FindMovieByID(ctx, 99999)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("CountMovies + CountMoviesByStatus", func() {
		It("counts total and per-status", func() {
			seed("a", 2020, 902, entmovie.StatusWanted)
			seed("b", 2020, 903, entmovie.StatusAvailable)

			n, err := store.CountMovies(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(2))

			nWanted, err := store.CountMoviesByStatus(ctx, entmovie.StatusWanted)
			Expect(err).NotTo(HaveOccurred())
			Expect(nWanted).To(Equal(1))
		})
	})

	Describe("ListMovies", func() {
		It("returns a page newest-first", func() {
			a := seed("a", 2020, 904, entmovie.StatusWanted)
			b := seed("b", 2020, 905, entmovie.StatusWanted)
			items, err := store.ListMovies(ctx, 0, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(items[0].ID).To(Equal(b.ID))
			Expect(items[1].ID).To(Equal(a.ID))
		})
	})

	Describe("ListEligibleMoviesForSync", func() {
		It("returns wanted, sub-cap, never-searched movies", func() {
			seed("eligible", 2020, 906, entmovie.StatusWanted)
			seed("not-wanted", 2020, 907, entmovie.StatusAvailable)
			items, err := store.ListEligibleMoviesForSync(ctx, 5, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Title).To(Equal("eligible"))
		})

		It("excludes movies with grab_failures at or above the cap", func() {
			m := seed("over-cap", 2020, 908, entmovie.StatusWanted)
			Expect(store.IncrementMovieGrabFailures(ctx, m.ID)).To(Succeed())
			items, err := store.ListEligibleMoviesForSync(ctx, 1, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("excludes movies that are not monitored", func() {
			m := seed("unmonitored", 2020, 909, entmovie.StatusWanted)
			_, err := client.Movie.UpdateOneID(m.ID).SetMonitored(false).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			items, err := store.ListEligibleMoviesForSync(ctx, 5, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("excludes movies that already have a downloading record", func() {
			m := seed("in-flight-dl", 2020, 910, entmovie.StatusWanted)
			_, err := client.DownloadRecord.Create().
				SetTitle("rel").
				SetTorrentHash("h").
				SetStatus(downloadrecord.StatusDownloading).
				SetMovieID(m.ID).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			items, err := store.ListEligibleMoviesForSync(ctx, 5, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("excludes movies that already have an importing record", func() {
			m := seed("in-flight-import", 2020, 911, entmovie.StatusWanted)
			_, err := client.DownloadRecord.Create().
				SetTitle("rel").
				SetTorrentHash("h").
				SetStatus(downloadrecord.StatusImporting).
				SetMovieID(m.ID).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			items, err := store.ListEligibleMoviesForSync(ctx, 5, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It(
			"includes a movie whose only download_record is in failed status",
			func() {
				m := seed("retry-after-fail", 2020, 912, entmovie.StatusWanted)
				_, err := client.DownloadRecord.Create().
					SetTitle("rel").
					SetTorrentHash("h").
					SetStatus(downloadrecord.StatusFailed).
					SetMovieID(m.ID).
					Save(ctx)
				Expect(err).NotTo(HaveOccurred())
				items, err := store.ListEligibleMoviesForSync(ctx, 5, time.Now())
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(HaveLen(1))
				Expect(items[0].Title).To(Equal("retry-after-fail"))
			},
		)
	})

	Describe("ListWantedMovies", func() {
		It("returns only movies with status=wanted", func() {
			seed("wanted", 2024, 1001, entmovie.StatusWanted)
			seed("available", 2023, 1002, entmovie.StatusAvailable)
			items, err := store.ListWantedMovies(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Title).To(Equal("wanted"))
		})

		It("excludes movies that are not monitored", func() {
			m := seed("unmonitored", 2024, 1003, entmovie.StatusWanted)
			_, err := client.Movie.UpdateOneID(m.ID).SetMonitored(false).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			items, err := store.ListWantedMovies(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})
	})

	Describe("ListMoviesStaleSince", func() {
		It("returns movies whose update_time is older than cutoff", func() {
			stale := seed("stale", 2020, 1101, entmovie.StatusAvailable)
			_, err := client.Movie.UpdateOneID(stale.ID).
				SetUpdateTime(time.Now().Add(-48 * time.Hour)).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			seed("fresh", 2024, 1102, entmovie.StatusAvailable)

			cutoff := time.Now().Add(-24 * time.Hour)
			items, err := store.ListMoviesStaleSince(ctx, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Title).To(Equal("stale"))
		})
	})

	Describe("UpdateMovieMetadata", func() {
		It("updates only metadata fields, leaves status untouched", func() {
			m := seed("Old", 2020, 1201, entmovie.StatusWanted)
			Expect(store.UpdateMovieMetadata(ctx, m.ID, UpdateMovieMetadataParams{
				Title:         "New",
				OriginalTitle: "Original Nouveau",
				Overview:      "fresh",
				Year:          2024,
				Runtime:       144,
			})).To(Succeed())
			got, err := store.FindMovieByID(ctx, m.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Title).To(Equal("New"))
			Expect(got.OriginalTitle).To(Equal("Original Nouveau"))
			Expect(got.Year).To(Equal(uint16(2024)))
			Expect(got.Overview).To(Equal("fresh"))
			Expect(got.Status).To(Equal(entmovie.StatusWanted))
		})
	})

	Describe("DeleteMovie", func() {
		It("removes the row", func() {
			m := seed("a", 2020, 909, entmovie.StatusWanted)
			Expect(store.DeleteMovie(ctx, m.ID)).To(Succeed())
			_, err := store.FindMovieByID(ctx, m.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("UpdateMovieStatus", func() {
		It("updates the status", func() {
			m := seed("a", 2020, 910, entmovie.StatusWanted)
			Expect(
				store.UpdateMovieStatus(ctx, m.ID, entmovie.StatusAvailable),
			).To(Succeed())
			got, _ := store.FindMovieByID(ctx, m.ID)
			Expect(got.Status).To(Equal(entmovie.StatusAvailable))
		})
	})

	Describe("SetMovieLastSearchAt", func() {
		It("sets last_search_at", func() {
			m := seed("a", 2020, 911, entmovie.StatusWanted)
			Expect(store.SetMovieLastSearchAt(ctx, m.ID, time.Now())).To(Succeed())
			got, _ := store.FindMovieByID(ctx, m.ID)
			Expect(got.LastSearchAt).NotTo(BeNil())
		})
	})

	Describe("IncrementMovieGrabFailures + ResetMovieGrabFailures", func() {
		It("bumps and resets grab_failures", func() {
			m := seed("a", 2020, 912, entmovie.StatusWanted)
			Expect(store.IncrementMovieGrabFailures(ctx, m.ID)).To(Succeed())
			Expect(store.IncrementMovieGrabFailures(ctx, m.ID)).To(Succeed())
			got, _ := store.FindMovieByID(ctx, m.ID)
			Expect(got.GrabFailures).To(Equal(uint8(2)))

			Expect(store.ResetMovieGrabFailures(ctx, m.ID)).To(Succeed())
			got, _ = store.FindMovieByID(ctx, m.ID)
			Expect(got.GrabFailures).To(Equal(uint8(0)))
		})
	})
})
