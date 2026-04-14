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
					results, err := svc.SearchEpisode(
						ctx,
						[]string{"The Black Sea"},
						12345,
						3,
						5,
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(results).To(BeEmpty())
				},
			)
		})

		When("the titles slice is empty after dedup", func() {
			It("returns nil without contacting any indexer", func() {
				configtest.Setup()
				results, err := svc.SearchEpisode(ctx, []string{"", ""}, 0, 0, 0)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(BeNil())
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
		It("keeps only releases scoped to exactly the season", func() {
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
			// S02 (wrong season), the integral, the complete collection and the
			// S01-S05 range are dropped — only the season-1 pack + episode stay.
			Expect(titles).To(ConsistOf(
				"Breaking.Bad.S01.MULTI.1080p.BluRay.x265.RamirouHD",
				"Breaking.Bad.S01E05.1080p.WEB.x265-GRP",
			))
		})
	})
})
