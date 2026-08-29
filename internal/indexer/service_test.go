package indexer

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Service", Label("unit", "indexers"), func() {
	var (
		ctx context.Context
		svc Manager
	)

	BeforeEach(func() {
		ctx = context.Background()
		svc = New()
	})

	Describe("SearchMovie", func() {
		When("no indexers are enabled", func() {
			It(
				"returns an empty result slice without contacting any indexer",
				func() {
					configtest.Setup()
					results, err := svc.SearchMovie(
						ctx,
						[]string{"Interstellar"},
						157336,
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(results).To(BeEmpty())
				},
			)
		})

		When("the titles slice is empty after dedup", func() {
			It("returns nil without contacting any indexer", func() {
				configtest.Setup()
				results, err := svc.SearchMovie(ctx, []string{"", ""}, 0)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(BeNil())
			})
		})
	})

	Describe("SearchSeason", func() {
		When("no indexers are enabled", func() {
			It(
				"returns an empty result slice without contacting any indexer",
				func() {
					configtest.Setup()
					results, err := svc.SearchSeason(
						ctx,
						[]string{"The Black Sea"},
						12345,
						3,
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(results).To(BeEmpty())
				},
			)
		})

		When("the titles slice is empty after dedup", func() {
			It("returns nil without contacting any indexer", func() {
				configtest.Setup()
				results, err := svc.SearchSeason(ctx, []string{"", ""}, 0, 0)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(BeNil())
			})
		})
	})

	Describe("SearchEpisode", func() {
		When("no indexers are enabled", func() {
			It(
				"returns an empty result slice without contacting any indexer",
				func() {
					configtest.Setup()
					results, hidden, err := svc.SearchEpisode(
						ctx,
						[]string{"The Black Sea"},
						12345,
						3,
						5,
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(results).To(BeEmpty())
					Expect(hidden).To(BeZero())
				},
			)
		})

		When("the titles slice is empty after dedup", func() {
			It("returns nil without contacting any indexer", func() {
				configtest.Setup()
				results, hidden, err := svc.SearchEpisode(
					ctx,
					[]string{"", ""},
					0,
					0,
					0,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(BeNil())
				Expect(hidden).To(BeZero())
			})
		})
	})

	Describe("dedupTitles", func() {
		It("strips empty entries and collapses duplicates first-seen order", func() {
			Expect(dedupTitles(nil)).To(BeNil())
			Expect(dedupTitles([]string{})).To(BeNil())
			Expect(dedupTitles([]string{""})).To(BeEmpty())
			Expect(
				dedupTitles([]string{"Fight Club", "Fight Club"}),
			).To(Equal([]string{"Fight Club"}))
			Expect(
				dedupTitles([]string{"", "Astérix", ""}),
			).To(Equal([]string{"Astérix"}))
			Expect(
				dedupTitles([]string{"Fight Club", "Astérix", "Fight Club"}),
			).To(Equal([]string{"Fight Club", "Astérix"}))
		})
	})

	Describe("dedupResults", func() {
		rel := func(idx, dl string) SearchResult {
			return SearchResult{
				Title:    "Slime.S04E07.1080p-GRP",
				Indexer:  idx,
				Download: dl,
				Size:     1_400_000_000,
			}
		}

		It("collapses one release answered under two proxy links", func() {
			out := dedupResults([]SearchResult{
				rel("C411", "http://p/1?q=local"),
				rel("C411", "http://p/1?q=original"),
			})
			Expect(out).To(HaveLen(1))
			Expect(out[0].Download).To(Equal("http://p/1?q=local"))
		})

		It("keeps the same release on a different tracker", func() {
			Expect(dedupResults([]SearchResult{
				rel("C411", "http://p/1"),
				rel("TR4KER", "http://p/2"),
			})).To(HaveLen(2))
		})

		It("keeps a same-named release of a different size", func() {
			a := rel("C411", "http://p/1")
			b := a
			b.Size = 426_000_000
			Expect(dedupResults([]SearchResult{a, b})).To(HaveLen(2))
		})
	})

	Describe("Feed", func() {
		When("the named indexer is not configured", func() {
			It("returns ErrIndexerNotFound", func() {
				configtest.Setup()
				results, err := svc.Feed(ctx, "ghost")
				Expect(err).To(MatchError(config.ErrIndexerNotFound))
				Expect(results).To(BeNil())
			})
		})
	})

	Describe("TestByName", func() {
		When("the named indexer is not configured", func() {
			It("returns ErrIndexerNotFound", func() {
				configtest.Setup()
				Expect(svc.TestByName(ctx, "ghost")).
					To(MatchError(config.ErrIndexerNotFound))
			})
		})
	})

	Describe("filterToSeason", func() {
		It("keeps only season packs of exactly the season", func() {
			in := []SearchResult{
				{Title: "Breaking.Bad.S01.MULTI.1080p.BluRay.x265.RamirouHD"},
				{Title: "Breaking.Bad.S01E05.1080p.WEB.x265-GRP"},
				{Title: "Breaking.Bad.S02.MULTI.2160p.WEBRip.x265-SQUEEZE"},
				{Title: "Breaking.Bad.INTEGRALE.MULTI.1080p.WEB.x265-NoTAG"},
				{
					Title: "Breaking.Bad.The.Complete.Series.Collection.MULTi.1080p.PopHD",
				},
				{Title: "Breaking.Bad.COMPLETE.S01-S05.Bluray.Remux.1080p-GRP"},
			}
			out := filterToSeason(in, 1)
			titles := make([]string, len(out))
			for i, r := range out {
				titles[i] = r.Title
			}
			// S02 (wrong season), the single episode, the integral, the complete
			// collection and the S01-S05 range are all dropped.
			Expect(titles).To(ConsistOf(
				"Breaking.Bad.S01.MULTI.1080p.BluRay.x265.RamirouHD",
			))
		})
	})

	Describe("filterToEpisode", func() {
		It("keeps the requested episode, absolute numbers and dailies", func() {
			in := []SearchResult{
				{Title: "Breaking.Bad.S01E05.1080p.WEB.x265-GRP"},
				{Title: "Breaking.Bad.S01E06.1080p.WEB.x265-GRP"},
				{Title: "Breaking.Bad.S02E05.1080p.WEB.x265-GRP"},
				{Title: "Breaking.Bad.S01.MULTI.1080p.BluRay.x265.RamirouHD"},
				{Title: "[SubsPlease] Breaking Bad - 05 (1080p) [A1B2C3D4]"},
				{Title: "Breaking.Bad.2011.09.25.FRENCH.720p.WEB-GRP"},
				// Names no scope at all: a tracker that publishes packs under the
				// bare show title, and a different show entirely.
				{Title: "Breaking Bad"},
				{Title: "Breaking Bad The Movie 2017 Web-DL 720p AVC AAC VOSTF"},
			}
			out, _ := filterToEpisode(in, 1, 5)
			titles := make([]string, len(out))
			for i, r := range out {
				titles[i] = r.Title
			}
			Expect(titles).To(ConsistOf(
				"Breaking.Bad.S01E05.1080p.WEB.x265-GRP",
				"[SubsPlease] Breaking Bad - 05 (1080p) [A1B2C3D4]",
				"Breaking.Bad.2011.09.25.FRENCH.720p.WEB-GRP",
			))
		})

		It("counts only the dropped packs that cover the episode", func() {
			in := []SearchResult{
				{Title: "Breaking.Bad.S01.MULTI.1080p.BluRay.x265.RamirouHD"},
				{Title: "Breaking.Bad.COMPLETE.S01-S05.Bluray.Remux.1080p-GRP"},
				{Title: "Breaking.Bad.INTEGRALE.MULTI.1080p.WEB.x265-NoTAG"},
				// Covers other seasons, not season 1.
				{Title: "Breaking.Bad.S02-S03.MULTI.1080p.WEB.x265-GRP"},
				// Plain noise: another episode, nowhere to send anyone.
				{Title: "Breaking.Bad.S01E06.1080p.WEB.x265-GRP"},
			}
			out, hidden := filterToEpisode(in, 1, 5)
			Expect(out).To(BeEmpty())
			Expect(hidden).To(Equal(3))
		})
	})

	Describe("preferTitleMatches", func() {
		It(
			"keeps this show's releases when another show shares the numbers",
			func() {
				in := []SearchResult{
					{Title: "Breaking.Bad.S01E05.1080p.WEB.x265-GRP"},
					{Title: "Ted Lasso S01E05 1080p WEB H264-CAKES"},
					{Title: "Reacher S01E05 1080p HEVC x265-MeGusta"},
					// A pack keyword and a fansub tag both survive extractTitle, so
					// the comparison has to tolerate them.
					{Title: "Breaking.Bad.S01.COMPLETE.MULTI.1080p.WEB-GRP"},
					{Title: "[SubsPlease] Breaking Bad - 05 (1080p) [A1B2C3D4]"},
				}
				out := preferTitleMatches(
					in,
					[]string{"Breaking Bad", "Breaking Bad"},
				)
				titles := make([]string, len(out))
				for i, r := range out {
					titles[i] = r.Title
				}
				Expect(titles).To(ConsistOf(
					"Breaking.Bad.S01E05.1080p.WEB.x265-GRP",
					"Breaking.Bad.S01.COMPLETE.MULTI.1080p.WEB-GRP",
					"[SubsPlease] Breaking Bad - 05 (1080p) [A1B2C3D4]",
				))
			},
		)

		It("keeps everything when no release names the show", func() {
			// A library holding a show under a translated title matches none of
			// its English releases, and dropping them leaves nothing to grab.
			in := []SearchResult{
				{
					Title: "That.Time.I.Got.Reincarnated.as.a.Slime.S04E20.VOSTFR.1080p.WEB",
				},
				{Title: "Slime.Datta.Ken.S04E20.VOSTFR.1080p.WEB"},
			}
			out := preferTitleMatches(in, []string{
				"Moi, quand je me réincarne en Slime",
				"転生したらスライムだった件",
			})
			Expect(out).To(HaveLen(2))
		})
	})
})
