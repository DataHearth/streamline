package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	entmovie "github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/library"
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

	Describe("the parsed_* columns", func() {
		It("fills them from the filename at create", func() {
			m := seed("Parsed", 2020, 995, entmovie.StatusAvailable)
			f, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
				MovieID: m.ID,
				Path:    "/lib/Parsed/Parsed.2020.1080p.BluRay.x264-GRP.mkv",
				Size:    10,
			})
			Expect(err).NotTo(HaveOccurred())
			// Stored once, so the ~12 regex passes never run again for this row.
			Expect(f.ParsedResolution).To(Equal("1080p"))
			Expect(f.ParsedCodec).To(Equal("x264"))
			Expect(f.ParsedSource).ToNot(BeEmpty())
		})

		It("prefers the caller's parse over the renamed basename", func() {
			m := seed("Renamed", 2020, 994, entmovie.StatusAvailable)
			parsed := library.Parse("Renamed.2020.1080p.BluRay.x264-GRP.mkv")
			f, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
				MovieID: m.ID,
				// What the default naming template renders: no source, no
				// codec, so parsing this back finds neither.
				Path:   "/lib/Renamed (2020)/Renamed (2020) [1080p].mkv",
				Size:   10,
				Parsed: &parsed,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(f.ParsedSource).To(Equal("BluRay"))
			Expect(f.ParsedCodec).To(Equal("x264"))
			Expect(f.ParsedResolution).To(Equal("1080p"))
		})
	})

	Describe("MovieTMDBIndex", func() {
		It("maps every tracked tmdb id to its row id", func() {
			a := seed("a", 2020, 980, entmovie.StatusWanted)
			b := seed("b", 2020, 981, entmovie.StatusAvailable)

			got, err := store.MovieTMDBIndex(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(map[uint32]uint32{980: a.ID, 981: b.ID}))
		})
	})

	Describe("MovieFacetCounts", func() {
		It("groups every status in one pass, omitting empty ones", func() {
			seed("a", 2020, 962, entmovie.StatusWanted)
			seed("b", 2020, 963, entmovie.StatusWanted)
			seed("c", 2020, 964, entmovie.StatusAvailable)

			got, err := store.MovieFacetCounts(ctx, FilterMoviesParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Total).To(Equal(3))
			Expect(got.StatusTotal).To(Equal(3))
			Expect(got.ByStatus).To(Equal(map[entmovie.Status]int{
				entmovie.StatusWanted:    2,
				entmovie.StatusAvailable: 1,
			}))
		})

		It("counts each facet against the other's filter, not its own", func() {
			// Two wanted (one unmonitored), one available and monitored.
			a := seed("a", 2020, 972, entmovie.StatusWanted)
			seed("b", 2020, 973, entmovie.StatusWanted)
			seed("c", 2020, 974, entmovie.StatusAvailable)
			_, err := client.Movie.UpdateOneID(a.ID).SetMonitored(false).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			on := true
			got, err := store.MovieFacetCounts(ctx, FilterMoviesParams{
				Status:    entmovie.StatusWanted,
				Monitored: &on,
			})
			Expect(err).NotTo(HaveOccurred())

			// Status tallies drop their own filter but keep monitored=true, so
			// the unmonitored wanted movie is out and the available one is in —
			// which is what lets "available" still be selectable.
			Expect(got.ByStatus).To(Equal(map[entmovie.Status]int{
				entmovie.StatusWanted:    1,
				entmovie.StatusAvailable: 1,
			}))
			Expect(got.StatusTotal).To(Equal(2))

			// Monitoring tallies drop theirs but keep status=wanted: both wanted
			// movies, one of each.
			Expect(got.MonitoredTotal).To(Equal(2))
			Expect(got.Monitored).To(Equal(1))
			Expect(got.Unmonitored).To(Equal(1))

			// Total ignores every filter.
			Expect(got.Total).To(Equal(3))
		})

		It("returns an empty map for an empty library", func() {
			got, err := store.MovieFacetCounts(ctx, FilterMoviesParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ByStatus).To(BeEmpty())
			Expect(got.Total).To(BeZero())
		})
	})

	Describe("MovieCreateTimesSince", func() {
		It("scans create_time straight into times, oldest first", func() {
			seed("a", 2020, 965, entmovie.StatusWanted)
			seed("b", 2020, 966, entmovie.StatusWanted)

			got, err := store.MovieCreateTimesSince(
				ctx, time.Now().Add(-time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(2))
			Expect(got[0]).To(BeTemporally("<=", got[1]))
		})

		It("excludes rows created before the window", func() {
			seed("a", 2020, 967, entmovie.StatusWanted)

			got, err := store.MovieCreateTimesSince(
				ctx, time.Now().Add(time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})
	})

	Describe("MovieFileSummaries", func() {
		It("rolls up count, total size and the largest file's path", func() {
			m := seed("Summed", 2020, 990, entmovie.StatusAvailable)
			for _, f := range []struct {
				path string
				size int64
			}{
				{"/lib/Summed/small.1080p.x264.mkv", 100},
				{"/lib/Summed/big.2160p.hevc.mkv", 900},
			} {
				_, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
					MovieID: m.ID, Path: f.path, Size: f.size,
				})
				Expect(err).NotTo(HaveOccurred())
			}

			got, err := store.MovieFileSummaries(ctx, []uint32{m.ID})
			Expect(err).NotTo(HaveOccurred())
			Expect(got[m.ID].FileCount).To(Equal(uint32(2)))
			Expect(got[m.ID].SizeBytes).To(Equal(int64(1000)))
			// Largest wins the primary slot, so the quality column reports the
			// file a viewer would actually play.
			Expect(got[m.ID].PrimaryPath).To(HaveSuffix("big.2160p.hevc.mkv"))
		})

		It("omits a movie with no files rather than reporting a zero row", func() {
			m := seed("Empty", 2020, 991, entmovie.StatusWanted)
			got, err := store.MovieFileSummaries(ctx, []uint32{m.ID})
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("returns an empty map for no ids without querying", func() {
			got, err := store.MovieFileSummaries(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
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

	Describe("ListUpgradeCandidateMovies", func() {
		It("returns monitored movies that have a file, with files loaded", func() {
			withFile := seed("has-file", 2020, 1201, entmovie.StatusAvailable)
			_, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
				Path:    "/media/has-file/has-file.mkv",
				Size:    1234,
				MovieID: withFile.ID,
			})
			Expect(err).NotTo(HaveOccurred())
			seed("no-file", 2021, 1202, entmovie.StatusWanted)

			items, err := store.ListUpgradeCandidateMovies(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Title).To(Equal("has-file"))
			Expect(items[0].Edges.MediaFiles).To(HaveLen(1))
			Expect(items[0].Edges.MediaFiles[0].Path).
				To(Equal("/media/has-file/has-file.mkv"))
		})

		It("excludes a movie whose upgrade is already downloading", func() {
			m := seed(
				"downloading-with-file", 2020, 1204, entmovie.StatusDownloading,
			)
			_, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
				Path:    "/media/downloading/file.mkv",
				Size:    42,
				MovieID: m.ID,
			})
			Expect(err).NotTo(HaveOccurred())

			items, err := store.ListUpgradeCandidateMovies(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})

		It("excludes unmonitored movies even when they have a file", func() {
			m := seed("unmonitored-with-file", 2020, 1203, entmovie.StatusAvailable)
			_, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
				Path:    "/media/unmonitored/file.mkv",
				Size:    99,
				MovieID: m.ID,
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = client.Movie.UpdateOneID(m.ID).SetMonitored(false).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			items, err := store.ListUpgradeCandidateMovies(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeEmpty())
		})
	})

	Describe("ListMoviesStaleSince", func() {
		It("returns movies last refreshed before cutoff", func() {
			stale := seed("stale", 2020, 1101, entmovie.StatusAvailable)
			_, err := client.Movie.UpdateOneID(stale.ID).
				SetLastRefreshedAt(time.Now().Add(-48 * time.Hour)).Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			fresh := seed("fresh", 2024, 1102, entmovie.StatusAvailable)
			_, err = client.Movie.UpdateOneID(fresh.ID).
				SetLastRefreshedAt(time.Now()).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			cutoff := time.Now().Add(-24 * time.Hour)
			items, err := store.ListMoviesStaleSince(ctx, cutoff)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Title).To(Equal("stale"))
		})

		It("returns a never-refreshed movie written to moments ago", func() {
			m := seed("scanned", 2020, 1103, entmovie.StatusAvailable)
			_, err := client.Movie.UpdateOneID(m.ID).
				SetStatus(entmovie.StatusAvailable).Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			items, err := store.ListMoviesStaleSince(
				ctx, time.Now().Add(-24*time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Title).To(Equal("scanned"))
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
			Expect(got.LastRefreshedAt).NotTo(BeNil())
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
