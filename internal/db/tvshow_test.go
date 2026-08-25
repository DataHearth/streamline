package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TVShow store", Label("unit", "db"), func() {
	var (
		store  Store
		client *ent.Client
		ctx    context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
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

	Describe("TBA episodes", func() {
		aired := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

		epByNum := func(showID uint32, seasonNo, epNo uint16) *ent.Episode {
			GinkgoHelper()
			got, err := store.FindTVShowByID(ctx, showID)
			Expect(err).NotTo(HaveOccurred())
			for _, se := range got.Edges.Seasons {
				if se.Number != seasonNo {
					continue
				}
				for _, e := range se.Edges.Episodes {
					if e.Number == epNo {
						return e
					}
				}
			}
			Fail("episode not found")
			return nil
		}

		It("start unmonitored when neither title nor air date is known", func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "X", Year: 2024, TvdbID: 90,
				Seasons: []SeasonSeed{{Number: 1, Episodes: []EpisodeSeed{
					{Number: 1, Title: "Pilot"},
					{Number: 2, AirDate: &aired},
					{Number: 3},
				}}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(epByNum(show.ID, 1, 1).Monitored).To(BeTrue())
			Expect(epByNum(show.ID, 1, 2).Monitored).To(BeTrue())
			Expect(epByNum(show.ID, 1, 3).Monitored).To(BeFalse())
		})

		It("become monitored once a refresh publishes either field", func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "X", Year: 2024, TvdbID: 91,
				Seasons: []SeasonSeed{{Number: 1, Episodes: []EpisodeSeed{
					{Number: 1}, {Number: 2}, {Number: 3},
				}}},
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = store.ReconcileEpisodes(ctx, show.ID, []SeasonSeed{
				{Number: 1, Episodes: []EpisodeSeed{
					{Number: 1, Title: "Pilot"},
					{Number: 2, AirDate: &aired},
					{Number: 3},
				}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(epByNum(show.ID, 1, 1).Monitored).To(BeTrue())
			Expect(epByNum(show.ID, 1, 2).Monitored).To(BeTrue())
			Expect(epByNum(show.ID, 1, 3).Monitored).To(BeFalse())
		})

		It("stay unmonitored when their season is unmonitored", func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "X", Year: 2024, TvdbID: 92,
				Seasons: []SeasonSeed{
					{Number: 1, Episodes: []EpisodeSeed{{Number: 1}}},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(
				store.SetSeasonMonitored(ctx, show.Edges.Seasons[0].ID, false),
			).To(Succeed())

			_, err = store.ReconcileEpisodes(ctx, show.ID, []SeasonSeed{
				{Number: 1, Episodes: []EpisodeSeed{{Number: 1, Title: "Pilot"}}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(epByNum(show.ID, 1, 1).Monitored).To(BeFalse())
		})

		It("do not re-monitor an episode the user unmonitored", func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "X", Year: 2024, TvdbID: 93,
				Seasons: []SeasonSeed{
					{
						Number:   1,
						Episodes: []EpisodeSeed{{Number: 1, Title: "Pilot"}},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			ep := show.Edges.Seasons[0].Edges.Episodes[0]
			Expect(store.SetEpisodeMonitored(ctx, ep.ID, false)).To(Succeed())

			_, err = store.ReconcileEpisodes(ctx, show.ID, []SeasonSeed{
				{Number: 1, Episodes: []EpisodeSeed{
					{Number: 1, Title: "Pilot", AirDate: &aired},
				}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(epByNum(show.ID, 1, 1).Monitored).To(BeFalse())
		})
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

	Describe("UpdateTVShowMetadata", func() {
		It("keeps the stored cast when the refresh carries none", func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "X", Year: 2020, TvdbID: 11,
				Cast: []schema.CastMember{{Name: "Ana Vidal"}},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(
				store.UpdateTVShowMetadata(ctx, show.ID, UpdateTVShowMetadataParams{
					Title: "X", Year: 2020,
				}),
			).To(Succeed())

			got, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Cast).To(HaveLen(1))
			Expect(got.Cast[0].Name).To(Equal("Ana Vidal"))
		})
	})

	Describe("ListTVShowsStaleSince", func() {
		It("returns never-refreshed shows and skips freshly refreshed ones", func() {
			stale, err := store.CreateTVShow(
				ctx, CreateTVShowParams{Title: "stale", Year: 2020, TvdbID: 21},
			)
			Expect(err).NotTo(HaveOccurred())
			fresh, err := store.CreateTVShow(
				ctx, CreateTVShowParams{Title: "fresh", Year: 2020, TvdbID: 22},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				store.SetTVShowRefreshedAt(ctx, fresh.ID, time.Now()),
			).To(Succeed())

			items, err := store.ListTVShowsStaleSince(
				ctx, time.Now().Add(-24*time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal(stale.ID))
		})
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
					{Number: 1, Episodes: []EpisodeSeed{
						{Number: 1, Title: "A"}, {Number: 2, Title: "B"},
					}},
					{Number: 2, Episodes: []EpisodeSeed{{Number: 1, Title: "C"}}},
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

	It(
		"CascadeSpecialsMonitored(false) switches off season 0 everywhere",
		func() {
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "Everything on", Year: 2020, TvdbID: 93,
				Seasons: []SeasonSeed{
					{
						Number:   0,
						Episodes: []EpisodeSeed{{Number: 1, Title: "Recap"}},
					},
					{
						Number:   1,
						Episodes: []EpisodeSeed{{Number: 1, Title: "Pilot"}},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			n, err := store.CascadeSpecialsMonitored(ctx, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))

			got, err := store.FindTVShowByID(ctx, show.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Seasons[0].Monitored).To(BeFalse())
			Expect(got.Edges.Seasons[0].Edges.Episodes[0].Monitored).To(BeFalse())
			// The regular season is untouched.
			Expect(got.Edges.Seasons[1].Monitored).To(BeTrue())
			Expect(got.Edges.Seasons[1].Edges.Episodes[0].Monitored).To(BeTrue())
		},
	)

	It(
		"CascadeSpecialsMonitored(true) switches on season 0 of monitored shows only",
		func() {
			specials := SeasonSeed{
				Number:      0,
				Unmonitored: true,
				Episodes:    []EpisodeSeed{{Number: 1, Title: "Recap"}},
			}
			regular := SeasonSeed{
				Number:   1,
				Episodes: []EpisodeSeed{{Number: 1, Title: "Pilot"}},
			}
			kept, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "Watched", Year: 2020, TvdbID: 91,
				Seasons: []SeasonSeed{specials, regular},
			})
			Expect(err).NotTo(HaveOccurred())
			ignored, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "Dropped", Year: 2020, TvdbID: 92,
				Seasons: []SeasonSeed{specials, regular},
			})
			Expect(err).NotTo(HaveOccurred())
			off := false
			_, err = store.UpdateTVShow(
				ctx,
				ignored.ID,
				UpdateTVShowParams{Monitored: &off},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.CascadeShowMonitored(ctx, ignored.ID, false)).To(Succeed())

			n, err := store.CascadeSpecialsMonitored(ctx, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))

			got, err := store.FindTVShowByID(ctx, kept.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Seasons[0].Number).To(Equal(uint16(0)))
			Expect(got.Edges.Seasons[0].Monitored).To(BeTrue())
			Expect(got.Edges.Seasons[0].Edges.Episodes[0].Monitored).To(BeTrue())

			// The unmonitored show keeps its specials off — otherwise the
			// fetcher would pick up work its owner explicitly switched off.
			skipped, err := store.FindTVShowByID(ctx, ignored.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(skipped.Edges.Seasons[0].Monitored).To(BeFalse())
			Expect(skipped.Edges.Seasons[0].Edges.Episodes[0].Monitored).
				To(BeFalse())
		},
	)

	It("counts wanted monitored episodes", func() {
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title:  "X",
			Year:   2020,
			TvdbID: 1,
			Seasons: []SeasonSeed{
				{Number: 1, Episodes: []EpisodeSeed{
					{Number: 1, Title: "A"}, {Number: 2, Title: "B"},
				}},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(store.CountWantedEpisodes(ctx)).To(Equal(2))

		// Marking an episode available drops it from the wanted set.
		reloaded, err := store.FindTVShowByID(ctx, show.ID)
		Expect(err).NotTo(HaveOccurred())
		epID := reloaded.Edges.Seasons[0].Edges.Episodes[0].ID
		Expect(
			store.SetEpisodeStatus(ctx, epID, episode.StatusAvailable),
		).To(Succeed())
		Expect(store.CountWantedEpisodes(ctx)).To(Equal(1))
	})

	Describe("ListEligibleEpisodesForSync", func() {
		var (
			show *ent.TVShow
			eps  map[uint16]*ent.Episode
		)

		// One show, season 1, episodes 1 and 2 — both wanted, monitored,
		// never searched, no air date. Every spec below makes exactly one of
		// them ineligible and asserts the other survives.
		BeforeEach(func() {
			var err error
			show, err = store.CreateTVShow(ctx, CreateTVShowParams{
				Title: "X", Year: 2020, TvdbID: 42,
				Seasons: []SeasonSeed{
					{Number: 1, Episodes: []EpisodeSeed{
						{Number: 1, Title: "A"}, {Number: 2, Title: "B"},
					}},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			eps = map[uint16]*ent.Episode{}
			for _, e := range show.Edges.Seasons[0].Edges.Episodes {
				eps[e.Number] = e
			}
		})

		// eligible runs the query with generous defaults and returns the
		// episode numbers that survived.
		eligible := func() []uint16 {
			GinkgoHelper()
			shows, err := store.ListEligibleEpisodesForSync(
				ctx, 5, time.Now(), time.Now(),
			)
			Expect(err).NotTo(HaveOccurred())
			var nums []uint16
			for _, sh := range shows {
				for _, se := range sh.Edges.Seasons {
					for _, e := range se.Edges.Episodes {
						nums = append(nums, e.Number)
					}
				}
			}
			return nums
		}

		It("returns wanted, monitored, never-searched episodes", func() {
			Expect(eligible()).To(ConsistOf(uint16(1), uint16(2)))
		})

		It("excludes episodes at or above the grab-failure cap", func() {
			Expect(client.Episode.UpdateOneID(eps[1].ID).
				SetGrabFailures(5).Exec(ctx)).To(Succeed())
			Expect(eligible()).To(ConsistOf(uint16(2)))
		})

		It("excludes episodes searched inside the cooldown window", func() {
			Expect(store.SetEpisodeLastSearchAt(
				ctx, eps[1].ID, time.Now().Add(-time.Minute),
			)).To(Succeed())
			shows, err := store.ListEligibleEpisodesForSync(
				ctx, 5, time.Now().Add(-time.Hour), time.Now(),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(shows[0].Edges.Seasons[0].Edges.Episodes).To(HaveLen(1))
			Expect(
				shows[0].Edges.Seasons[0].Edges.Episodes[0].Number,
			).To(Equal(uint16(2)))
		})

		It("excludes episodes that have not aired yet", func() {
			Expect(client.Episode.UpdateOneID(eps[1].ID).
				SetAirDate(time.Now().Add(48 * time.Hour)).Exec(ctx)).To(Succeed())
			Expect(eligible()).To(ConsistOf(uint16(2)))
		})

		It("keeps episodes that aired before the cutoff", func() {
			Expect(client.Episode.UpdateOneID(eps[1].ID).
				SetAirDate(time.Now().Add(-48 * time.Hour)).Exec(ctx)).To(Succeed())
			Expect(eligible()).To(ConsistOf(uint16(1), uint16(2)))
		})

		It("excludes episodes with an in-flight download record", func() {
			for _, status := range []downloadrecord.Status{
				downloadrecord.StatusDownloading,
				downloadrecord.StatusImporting,
			} {
				rec, err := store.CreateDownloadRecord(
					ctx,
					CreateDownloadRecordParams{
						Title: "rel", Status: status, EpisodeID: eps[1].ID,
					},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(eligible()).To(ConsistOf(uint16(2)))
				Expect(
					client.DownloadRecord.DeleteOneID(rec.ID).Exec(ctx),
				).To(Succeed())
			}
		})

		It("includes an episode whose only record failed", func() {
			_, err := store.CreateDownloadRecord(ctx, CreateDownloadRecordParams{
				Title: "rel", Status: "failed", EpisodeID: eps[1].ID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(eligible()).To(ConsistOf(uint16(1), uint16(2)))
		})

		It("excludes unmonitored and non-wanted episodes", func() {
			Expect(store.SetEpisodeMonitored(ctx, eps[1].ID, false)).To(Succeed())
			Expect(store.SetEpisodeStatus(
				ctx, eps[2].ID, episode.StatusAvailable,
			)).To(Succeed())
			Expect(eligible()).To(BeEmpty())
		})
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
