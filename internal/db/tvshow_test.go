package db

import (
	"context"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TVShow store", Label("unit", "db"), func() {
	var (
		store Store
		ctx   context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)
	})

	It("creates a show with seasons and episodes atomically", func() {
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title:        "The Black Sea",
			Year:         2023,
			TvdbID:       123,
			SeriesStatus: "continuing",
			Type:         "standard",
			Network:      "Halcyon",
			Genres:       []string{"Drama"},
			Seasons: []SeasonSeed{{
				Number: 1,
				Episodes: []EpisodeSeed{
					{Number: 1, Title: "Pilot"},
					{Number: 2, Title: "Tide"},
				},
			}},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(show.ID).NotTo(BeZero())

		got, err := store.FindTVShowByID(ctx, show.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Edges.Seasons).To(HaveLen(1))
		Expect(got.Edges.Seasons[0].Edges.Episodes).To(HaveLen(2))
	})

	It("reconciles refreshed titles and inserts new seasons/episodes", func() {
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title: "X", Year: 2020, TvdbID: 5,
			Seasons: []SeasonSeed{
				{Number: 1, Episodes: []EpisodeSeed{{Number: 1, Title: "Pilot"}}},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		// User unmonitors season 1 before the refresh runs.
		s1 := show.Edges.Seasons[0]
		Expect(store.SetSeasonMonitored(ctx, s1.ID, false)).To(Succeed())

		removed, err := store.ReconcileEpisodes(ctx, show.ID, []SeasonSeed{
			// Season 1: existing ep retitled + a newly-aired ep appended.
			{Number: 1, Episodes: []EpisodeSeed{
				{Number: 1, Title: "Le Pilote"},
				{Number: 2, Title: "Nouveau"},
			}},
			// Season 2: brand new.
			{Number: 2, Episodes: []EpisodeSeed{{Number: 1, Title: "S2E1"}}},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(removed).To(BeEmpty())

		got, err := store.FindTVShowByID(ctx, show.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Edges.Seasons).To(HaveLen(2))

		var season1, season2 *ent.Season
		for _, se := range got.Edges.Seasons {
			switch se.Number {
			case 1:
				season1 = se
			case 2:
				season2 = se
			}
		}
		// Existing episode retitled; new one inherits the unmonitored season.
		Expect(season1.Edges.Episodes).To(HaveLen(2))
		byNum := map[uint16]*ent.Episode{}
		for _, e := range season1.Edges.Episodes {
			byNum[e.Number] = e
		}
		Expect(byNum[1].Title).To(Equal("Le Pilote"))
		Expect(byNum[2].Title).To(Equal("Nouveau"))
		Expect(byNum[2].Monitored).To(BeFalse())
		// New season + its episode inserted (season defaults to monitored).
		Expect(season2.Monitored).To(BeTrue())
		Expect(season2.Edges.Episodes).To(HaveLen(1))
	})

	It(
		"prunes provider-removed seasons/episodes and returns their file paths",
		func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "X", Year: 2020, TvdbID: 6,
				Seasons: []SeasonSeed{
					{Number: 1, Episodes: []EpisodeSeed{{Number: 1}, {Number: 2}}},
					{Number: 2, Episodes: []EpisodeSeed{{Number: 1}}},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			got, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			// Attach a file to S01E02 (which the refresh will drop).
			var s1e2 uint32
			for _, se := range got.Edges.Seasons {
				if se.Number == 1 {
					for _, e := range se.Edges.Episodes {
						if e.Number == 2 {
							s1e2 = e.ID
						}
					}
				}
			}
			rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
				Title: "rel", Status: "importing", EpisodeID: s1e2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(
				store.RecordEpisodeImportSuccess(
					ctx,
					RecordEpisodeImportSuccessParams{
						RecordID: rec.ID, EpisodeID: s1e2,
						File: MediaFileRow{Path: "/lib/orphan.mkv", Size: 1},
					},
				),
			).To(Succeed())

			// Provider now reports only S01E01 — S01E02 and all of season 2 are gone.
			removed, err := store.ReconcileEpisodes(ctx, show.ID, []SeasonSeed{
				{Number: 1, Episodes: []EpisodeSeed{{Number: 1}}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(removed).To(ConsistOf("/lib/orphan.mkv"))

			after, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Edges.Seasons).To(HaveLen(1))
			Expect(after.Edges.Seasons[0].Number).To(Equal(uint16(1)))
			Expect(after.Edges.Seasons[0].Edges.Episodes).To(HaveLen(1))
		},
	)

	It(
		"does not prune when the provider returns nothing (failed fetch guard)",
		func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title:  "X",
				Year:   2020,
				TvdbID: 7,
				Seasons: []SeasonSeed{
					{Number: 1, Episodes: []EpisodeSeed{{Number: 1}}},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			removed, err := store.ReconcileEpisodes(ctx, show.ID, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(removed).To(BeEmpty())

			after, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Edges.Seasons).To(HaveLen(1))
		},
	)

	It("finds a show by tvdb id or returns nil", func() {
		_, err := store.CreateTVShow(
			ctx,
			CreateTVShowParams{Title: "X", Year: 2020, TvdbID: 9},
		)
		Expect(err).NotTo(HaveOccurred())
		got, err := store.FindTVShowByTVDBID(ctx, 9)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		none, err := store.FindTVShowByTVDBID(ctx, 404)
		Expect(err).NotTo(HaveOccurred())
		Expect(none).To(BeNil())
	})

	It("toggles season and episode monitored flags", func() {
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title: "X", Year: 2020, TvdbID: 1,
			Seasons: []SeasonSeed{{Number: 1, Episodes: []EpisodeSeed{{Number: 1}}}},
		})
		Expect(err).NotTo(HaveOccurred())
		got, err := store.FindTVShowByID(ctx, show.ID)
		Expect(err).NotTo(HaveOccurred())
		seasonID := got.Edges.Seasons[0].ID
		epID := got.Edges.Seasons[0].Edges.Episodes[0].ID

		Expect(store.SetSeasonMonitored(ctx, seasonID, false)).To(Succeed())
		Expect(store.SetEpisodeMonitored(ctx, epID, false)).To(Succeed())

		got2, err := store.FindTVShowByID(ctx, show.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got2.Edges.Seasons[0].Monitored).To(BeFalse())
		Expect(got2.Edges.Seasons[0].Edges.Episodes[0].Monitored).To(BeFalse())
	})

	It("CascadeShowMonitored flows the flag to every season and episode", func() {
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title: "X", Year: 2020, TvdbID: 2,
			Seasons: []SeasonSeed{
				{Number: 1, Episodes: []EpisodeSeed{{Number: 1}, {Number: 2}}},
				{Number: 2, Episodes: []EpisodeSeed{{Number: 1}}},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(store.CascadeShowMonitored(ctx, show.ID, false)).To(Succeed())

		got, err := store.FindTVShowByID(ctx, show.ID)
		Expect(err).NotTo(HaveOccurred())
		for _, se := range got.Edges.Seasons {
			Expect(se.Monitored).To(BeFalse())
			for _, e := range se.Edges.Episodes {
				Expect(e.Monitored).To(BeFalse())
			}
		}
	})

	It(
		"CascadeSeasonMonitored flows the flag to only that season's episodes",
		func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "X", Year: 2020, TvdbID: 3,
				Seasons: []SeasonSeed{
					{Number: 1, Episodes: []EpisodeSeed{{Number: 1}, {Number: 2}}},
					{Number: 2, Episodes: []EpisodeSeed{{Number: 1}}},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			got, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			s1 := got.Edges.Seasons[0]

			Expect(store.CascadeSeasonMonitored(ctx, s1.ID, false)).To(Succeed())

			after, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			for _, se := range after.Edges.Seasons {
				if se.Number == 1 {
					Expect(se.Monitored).To(BeFalse())
					for _, e := range se.Edges.Episodes {
						Expect(e.Monitored).To(BeFalse())
					}
				} else {
					// Season 2 is untouched.
					Expect(se.Monitored).To(BeTrue())
					Expect(se.Edges.Episodes[0].Monitored).To(BeTrue())
				}
			}
		},
	)

	It("lists shows with wanted monitored episodes", func() {
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title:  "X",
			Year:   2020,
			TvdbID: 1,
			Seasons: []SeasonSeed{
				{Number: 1, Episodes: []EpisodeSeed{{Number: 1}, {Number: 2}}},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		shows, err := store.ListWantedEpisodes(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(shows).To(HaveLen(1))
		Expect(shows[0].ID).To(Equal(show.ID))
		Expect(shows[0].Edges.Seasons[0].Edges.Episodes).To(HaveLen(2))

		// Marking an episode available drops it from the wanted set.
		epID := shows[0].Edges.Seasons[0].Edges.Episodes[0].ID
		Expect(
			store.SetEpisodeStatus(ctx, epID, episode.StatusAvailable),
		).To(Succeed())
		shows2, err := store.ListWantedEpisodes(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(shows2).To(HaveLen(1))
		Expect(shows2[0].Edges.Seasons[0].Edges.Episodes).To(HaveLen(1))
	})

	It("cascade-deletes seasons, episodes, and episode-linked records", func() {
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title: "X", Year: 2020, TvdbID: 1,
			Seasons: []SeasonSeed{{Number: 1, Episodes: []EpisodeSeed{{Number: 1}}}},
		})
		Expect(err).NotTo(HaveOccurred())
		got, err := store.FindTVShowByID(ctx, show.ID)
		Expect(err).NotTo(HaveOccurred())
		epID := got.Edges.Seasons[0].Edges.Episodes[0].ID

		rec, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
			Title: "rel", Status: "importing", EpisodeID: epID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(
			store.RecordEpisodeImportSuccess(ctx, RecordEpisodeImportSuccessParams{
				RecordID: rec.ID, EpisodeID: epID,
				File: MediaFileRow{Path: "/x.mkv", Size: 1},
			}),
		).To(Succeed())

		// With ON DELETE CASCADE the show deletes cleanly despite the
		// episode-linked download_record + media_file FKs.
		Expect(store.DeleteTVShow(ctx, show.ID)).To(Succeed())

		none, err := store.FindTVShowByTVDBID(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(none).To(BeNil())
	})
})
