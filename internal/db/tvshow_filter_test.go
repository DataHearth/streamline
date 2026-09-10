package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FilterTVShows", Label("unit", "db"), func() {
	var (
		store Store
		ctx   context.Context
		now   time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)
		now = time.Now()
	})

	// seedShow creates one show with a single season whose episodes are given
	// as (number, monitored, airDate) triples.
	seedShow := func(
		title string,
		tvdbID uint32,
		eps []EpisodeSeed,
		monitored bool,
	) *ent.TVShow {
		GinkgoHelper()
		show, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title:        title,
			Year:         2020,
			TvdbID:       tvdbID,
			SeriesStatus: "continuing",
			Type:         "standard",
			Seasons:      []SeasonSeed{{Number: 1, Episodes: eps}},
		})
		Expect(err).NotTo(HaveOccurred())
		if !monitored {
			for _, se := range show.Edges.Seasons {
				for _, e := range se.Edges.Episodes {
					Expect(store.SetEpisodeMonitored(ctx, e.ID, false)).To(Succeed())
				}
			}
		}
		return show
	}

	attachFile := func(episodeID uint32, path string) {
		GinkgoHelper()
		_, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
			Path:      path,
			Size:      1,
			EpisodeID: episodeID,
		})
		Expect(err).NotTo(HaveOccurred())
	}

	seedYear := func(title string, tvdbID uint32, year uint16) {
		GinkgoHelper()
		_, err := store.CreateTVShow(ctx, CreateTVShowParams{
			Title:        title,
			Year:         year,
			TvdbID:       tvdbID,
			SeriesStatus: "continuing",
			Type:         "standard",
		})
		Expect(err).NotTo(HaveOccurred())
	}

	titles := func(rows []*ent.TVShow) []string {
		GinkgoHelper()
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.Title)
		}
		return out
	}

	Describe("status=missing", func() {
		It("keeps a show with a monitored, file-less, aired episode", func() {
			aired := now.Add(-24 * time.Hour)
			seedShow("Wanted", 1, []EpisodeSeed{
				{Number: 1, Title: "A", AirDate: &aired},
			}, true)

			rows, _, total, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Status: "missing", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(1)))
			Expect(rows).To(HaveLen(1))
		})

		It("drops a show whose file-less episodes are all unmonitored", func() {
			aired := now.Add(-24 * time.Hour)
			seedShow("Ignored", 2, []EpisodeSeed{
				{Number: 1, Title: "A", AirDate: &aired},
			}, false)

			rows, _, total, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Status: "missing", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())
			Expect(rows).To(BeEmpty())
		})

		It("drops a show whose only gap is an unaired episode", func() {
			future := now.Add(48 * time.Hour)
			seedShow("Upcoming", 3, []EpisodeSeed{
				{Number: 1, Title: "A", AirDate: &future},
			}, true)

			_, _, total, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Status: "missing", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())
		})

		It(
			"drops an undated episode — a dateless placeholder is unaired",
			func() {
				seedShow("Undated", 4, []EpisodeSeed{
					{Number: 1, Title: "TBA"},
				}, true)

				_, _, total, err := store.FilterTVShows(ctx, FilterTVShowsParams{
					Status: "missing", Limit: 20, Now: now,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(total).To(BeZero())
			},
		)

		It("drops a show once every gap has a file", func() {
			aired := now.Add(-24 * time.Hour)
			show := seedShow("Complete", 5, []EpisodeSeed{
				{Number: 1, Title: "A", AirDate: &aired},
			}, true)
			attachFile(
				show.Edges.Seasons[0].Edges.Episodes[0].ID,
				"/tv/c/S01E01.mkv",
			)

			_, _, total, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Status: "missing", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())
		})
	})

	Describe("episode counts", func() {
		It(
			"buckets have/wanted/unaired and excludes unmonitored file-less rows",
			func() {
				aired := now.Add(-24 * time.Hour)
				future := now.Add(48 * time.Hour)
				show := seedShow("Mixed", 6, []EpisodeSeed{
					{Number: 1, Title: "have", AirDate: &aired},
					{Number: 2, Title: "wanted", AirDate: &aired},
					{Number: 3, Title: "unaired", AirDate: &future},
					{Number: 4, Title: "skipped", AirDate: &aired},
					{Number: 5, Title: "TBA"},
				}, true)
				eps := show.Edges.Seasons[0].Edges.Episodes
				attachFile(eps[0].ID, "/tv/m/S01E01.mkv")
				Expect(
					store.SetEpisodeMonitored(ctx, eps[3].ID, false),
				).To(Succeed())

				_, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
					Limit: 20, Now: now,
				})
				Expect(err).NotTo(HaveOccurred())
				c := counts[show.ID]
				// The unmonitored, file-less episode is out of scope entirely.
				Expect(c.Total).To(Equal(uint32(4)))
				Expect(c.Have).To(Equal(uint32(1)))
				Expect(c.Wanted).To(Equal(uint32(1)))
				// Both the future-dated and the dateless "TBA" row are unaired.
				Expect(c.Unaired).To(Equal(uint32(2)))
			},
		)

		It("counts the numbered seasons and leaves the specials out", func() {
			aired := now.Add(-24 * time.Hour)
			show, err := store.CreateTVShow(ctx, CreateTVShowParams{
				Title:        "With specials",
				Year:         2020,
				TvdbID:       77,
				SeriesStatus: "continuing",
				Type:         "standard",
				Seasons: []SeasonSeed{
					{Number: 0, Episodes: []EpisodeSeed{
						{Number: 1, Title: "special", AirDate: &aired},
					}},
					{Number: 1, Episodes: []EpisodeSeed{
						{Number: 1, Title: "s1e1", AirDate: &aired},
					}},
					{Number: 2, Episodes: []EpisodeSeed{
						{Number: 1, Title: "s2e1", AirDate: &aired},
					}},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			for _, se := range show.Edges.Seasons {
				if se.Number == 0 {
					continue
				}
				id := se.Edges.Episodes[0].ID
				attachFile(id, fmt.Sprintf("/tv/s/%d.mkv", id))
			}

			_, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			c := counts[show.ID]
			Expect(c.Seasons).To(Equal(uint32(2)))
			Expect(c.Total).To(Equal(uint32(2)))
			Expect(c.Have).To(Equal(uint32(2)))
			Expect(c.Wanted).To(BeZero())

			// A gap only the specials have keeps the show out of the missing
			// filter too, which must agree with those counts.
			rows, _, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Limit: 20, Now: now, Status: "missing",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(titles(rows)).NotTo(ContainElement("With specials"))
		})

		It("tallies importing across the buckets, not instead of one", func() {
			aired := now.Add(-24 * time.Hour)
			show := seedShow("Landing", 8, []EpisodeSeed{
				{Number: 1, Title: "importing", AirDate: &aired},
				{Number: 2, Title: "wanted", AirDate: &aired},
			}, true)
			eps := show.Edges.Seasons[0].Edges.Episodes
			Expect(store.SetEpisodeStatus(
				ctx, eps[0].ID, episode.StatusImporting,
			)).To(Succeed())

			_, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			c := counts[show.ID]
			Expect(c.Importing).To(Equal(uint32(1)))
			// The importing episode has no file yet, so it is still wanted.
			Expect(c.Wanted).To(Equal(uint32(2)))
			Expect(c.Have).To(BeZero())
		})

		DescribeTable(
			"names the in-flight scope from the episodes actually in flight",
			func(inFlight [][2]int, scope string, seasonNo, epNo uint16) {
				aired := now.Add(-24 * time.Hour)
				show, err := store.CreateTVShow(ctx, CreateTVShowParams{
					Title: "Scoped", Year: 2020, TvdbID: 20,
					SeriesStatus: "continuing", Type: "standard",
					Seasons: []SeasonSeed{
						{Number: 1, Episodes: []EpisodeSeed{
							{Number: 1, Title: "A", AirDate: &aired},
							{Number: 2, Title: "B", AirDate: &aired},
						}},
						{Number: 2, Episodes: []EpisodeSeed{
							{Number: 1, Title: "C", AirDate: &aired},
						}},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				for _, se := range inFlight {
					ep := show.Edges.Seasons[se[0]].Edges.Episodes[se[1]]
					Expect(store.SetEpisodeStatus(
						ctx, ep.ID, episode.StatusDownloading,
					)).To(Succeed())
				}

				_, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
					Limit: 20, Now: now,
				})
				Expect(err).NotTo(HaveOccurred())
				c := counts[show.ID]
				Expect(c.Scope).To(Equal(scope))
				Expect(c.Season).To(Equal(seasonNo))
				Expect(c.Episode).To(Equal(epNo))
				Expect(c.Downloading).To(Equal(uint32(len(inFlight))))
				Expect(c.InFlightEpisodes).To(HaveLen(len(inFlight)))
			},
			Entry("one episode", [][2]int{{0, 0}}, "episode", uint16(1), uint16(1)),
			Entry("two of one season", [][2]int{{0, 0}, {0, 1}},
				"season", uint16(1), uint16(0)),
			// Season and episode are cleared as they stop being true of the
			// whole set, so no caller can read a number the scope disowns.
			Entry("across seasons", [][2]int{{0, 0}, {1, 0}},
				"series", uint16(0), uint16(0)),
		)

		It("leaves the scope empty when nothing is in flight", func() {
			aired := now.Add(-24 * time.Hour)
			show := seedShow("Idle", 21, []EpisodeSeed{
				{Number: 1, Title: "A", AirDate: &aired},
			}, true)

			_, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(counts[show.ID].Scope).To(BeEmpty())
			Expect(counts[show.ID].InFlightEpisodes).To(BeEmpty())
		})

		It("still counts a downloaded episode its owner later unmonitored", func() {
			aired := now.Add(-24 * time.Hour)
			show := seedShow("Kept", 7, []EpisodeSeed{
				{Number: 1, Title: "A", AirDate: &aired},
			}, true)
			ep := show.Edges.Seasons[0].Edges.Episodes[0]
			attachFile(ep.ID, "/tv/k/S01E01.mkv")
			Expect(store.SetEpisodeMonitored(ctx, ep.ID, false)).To(Succeed())

			_, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(counts[show.ID].Have).To(Equal(uint32(1)))
			Expect(counts[show.ID].Total).To(Equal(uint32(1)))
		})
	})

	// Every sort key carries the direction its menu label promises — "Year
	// (newest)", "Rating (highest)", "Most episodes" — because nothing in the
	// UI can flip it. Only "Title A-Z" ascends.
	Describe("sort direction", func() {
		BeforeEach(func() {
			seedYear("Old Show", 40, 1999)
			seedYear("New Show", 41, 2024)
		})

		It("puts the newest year first by default", func() {
			rows, _, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Sort: "year", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(titles(rows)).To(Equal([]string{"New Show", "Old Show"}))
		})

		It("honours an explicit ascending override", func() {
			rows, _, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Sort: "year", Order: "asc", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(titles(rows)).To(Equal([]string{"Old Show", "New Show"}))
		})

		It("keeps title ascending, the one label that says A-Z", func() {
			rows, _, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Sort: "title", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(titles(rows)).To(Equal([]string{"New Show", "Old Show"}))
		})
	})

	Describe("sort=episodes", func() {
		It("orders by episode count without loading the tree", func() {
			seedShow("Few", 8, []EpisodeSeed{{Number: 1, Title: "A"}}, true)
			seedShow("Many", 9, []EpisodeSeed{
				{Number: 1, Title: "A"},
				{Number: 2, Title: "B"},
				{Number: 3, Title: "C"},
			}, true)

			rows, _, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Sort: "episodes", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(titles(rows)).To(Equal([]string{"Many", "Few"}))
			// The list query leaves the tree unloaded — that is the point.
			Expect(rows[0].Edges.Seasons).To(BeEmpty())
		})

		It("ranks by the count the card shows, not by raw provider rows", func() {
			// A soap with five provider episodes but only the one on disk in
			// scope. Ranking raw rows floated it above a show the card called
			// bigger — the order and the number disagreed on the same screen.
			soap := seedShow("Soap", 10, []EpisodeSeed{
				{Number: 1, Title: "A"},
				{Number: 2, Title: "B"},
				{Number: 3, Title: "C"},
				{Number: 4, Title: "D"},
				{Number: 5, Title: "E"},
			}, false)
			attachFile(
				soap.Edges.Seasons[0].Edges.Episodes[0].ID,
				"/tv/s/S01E01.mkv",
			)
			seedShow("Tracked", 11, []EpisodeSeed{
				{Number: 1, Title: "A"},
				{Number: 2, Title: "B"},
			}, true)

			rows, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Sort: "episodes", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(counts[soap.ID].Total).To(Equal(uint32(1)))
			Expect(titles(rows)).To(Equal([]string{"Tracked", "Soap"}))
		})
	})

	Describe("query", func() {
		It("matches with and without accents, either way round", func() {
			seedShow("Détective Conan", 40, nil, true)
			seedShow("Naruto", 41, nil, true)

			for _, q := range []string{"detective", "Détec", "CONAN"} {
				rows, _, total, err := store.FilterTVShows(ctx, FilterTVShowsParams{
					Query: q, Limit: 20, Now: now,
				})
				Expect(err).NotTo(HaveOccurred(), q)
				Expect(total).To(Equal(uint32(1)), q)
				Expect(titles(rows)).To(Equal([]string{"Détective Conan"}), q)
			}
		})
	})

	Describe("paging", func() {
		It("counts the whole filtered set, not the page", func() {
			for i := range 5 {
				seedShow("Show", uint32(20+i), []EpisodeSeed{
					{Number: 1, Title: "A"},
				}, true)
			}

			rows, _, total, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Offset: 2, Limit: 2, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(uint32(5)))
			Expect(rows).To(HaveLen(2))
		})
	})
})

// Unmonitoring a show writes tv_shows.monitored and nothing else — it never
// reached the episodes underneath, and a metadata refresh creates new ones from
// the season's flag. Every query that read the episode alone therefore kept
// working through shows their owner had switched off.
var _ = Describe("an unmonitored show", Label("unit", "db"), func() {
	var (
		store Store
		ctx   context.Context
		now   time.Time
		show  *ent.TVShow
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)
		now = time.Now()

		aired := now.Add(-24 * time.Hour)
		show, err = store.CreateTVShow(ctx, CreateTVShowParams{
			Title:        "Dropped",
			Year:         2020,
			TvdbID:       1,
			SeriesStatus: "continuing",
			Type:         "standard",
			Seasons: []SeasonSeed{{Number: 1, Episodes: []EpisodeSeed{
				{Number: 1, Title: "A", AirDate: &aired},
			}}},
		})
		Expect(err).NotTo(HaveOccurred())

		// The episode stays monitored throughout: that is the state the bug
		// lived in, and the show's flag alone has to be enough.
		off := false
		_, err = store.UpdateTVShow(
			ctx,
			show.ID,
			UpdateTVShowParams{Monitored: &off},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(show.Edges.Seasons[0].Edges.Episodes[0].Monitored).To(BeTrue())
	})

	It("is not offered to the missing-episode search", func() {
		shows, err := store.ListEligibleEpisodesForSync(ctx, 5, now, now)
		Expect(err).NotTo(HaveOccurred())
		Expect(shows).To(BeEmpty())
	})

	It("is out of the wanted-episode tally", func() {
		Expect(store.CountWantedEpisodes(ctx)).To(BeZero())
	})

	It("is out of the calendar", func() {
		eps, err := store.ListUpcomingEpisodes(
			ctx, now.Add(-48*time.Hour), now.Add(48*time.Hour),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(eps).To(BeEmpty())
	})

	It(
		"does not match the missing filter, and its card reads nothing wanted",
		func() {
			rows, _, total, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Status: "missing", Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(BeZero())
			Expect(rows).To(BeEmpty())

			_, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
				Limit: 20, Now: now,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(counts[show.ID].Wanted).To(BeZero())
		},
	)

	It("still counts a file it already holds", func() {
		ep := show.Edges.Seasons[0].Edges.Episodes[0]
		_, err := store.CreateMediaFile(ctx, CreateMediaFileParams{
			EpisodeID: ep.ID,
			Path:      "/tv/d/S01E01.mkv",
			Size:      1,
		})
		Expect(err).NotTo(HaveOccurred())

		_, counts, _, err := store.FilterTVShows(ctx, FilterTVShowsParams{
			Limit: 20, Now: now,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(counts[show.ID].Have).To(Equal(uint32(1)))
		Expect(counts[show.ID].Total).To(Equal(uint32(1)))
	})
})
