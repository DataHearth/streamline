package rss

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/quality"
)

var _ = Describe("qualityFor", Label("unit", "rss"), func() {
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	accepts := func(profile, title string) bool {
		return !evaluateRelease(
			qualityFor(ctx, profile),
			indexer.SearchResult{Title: title},
			singleRelease,
		).Rejected
	}

	DescribeTable(
		"the default profile's band is min_resolution..preferred_resolution",
		func(title string, want bool) {
			Expect(accepts("", title)).To(Equal(want))
		},
		Entry(
			"720p sits on the floor",
			"Fight.Club.1999.720p.BluRay.x264-GROUP",
			true,
		),
		Entry(
			"1080p sits on the ceiling",
			"Fight.Club.1999.1080p.BluRay.x264-GROUP",
			true,
		),
		Entry(
			"2160p is above the ceiling",
			"Fight.Club.1999.2160p.BluRay.x265-GROUP",
			false,
		),
		Entry("unparseable title rejected", "some.random.garbage.title", false),
	)

	DescribeTable(
		"a pinned profile narrows the band",
		func(title string, want bool) {
			Expect(accepts(uhdProfile, title)).To(Equal(want))
		},
		Entry(
			"720p below the floor",
			"Fight.Club.1999.720p.BluRay.x264-GROUP",
			false,
		),
		Entry(
			"1080p below the floor",
			"Fight.Club.1999.1080p.BluRay.x264-GROUP",
			false,
		),
		Entry(
			"2160p inside the band",
			"Fight.Club.1999.2160p.BluRay.x265-GROUP",
			true,
		),
	)

	It("resolves an unknown name to the configured default", func() {
		Expect(
			accepts("no-such-profile", "Fight.Club.1999.1080p.BluRay.x264-GROUP"),
		).
			To(BeTrue())
	})

	// The zero profile is what qualityFor returns once nothing resolves. Its
	// band is empty, so it must reject rather than wave everything through.
	DescribeTable("the zero profile rejects every release",
		func(title string) {
			Expect(evaluateRelease(
				quality.Profile{},
				indexer.SearchResult{Title: title},
				singleRelease,
			).Rejected).To(BeTrue())
		},
		Entry("720p", "Fight.Club.1999.720p.BluRay.x264-GROUP"),
		Entry("1080p", "Fight.Club.1999.1080p.BluRay.x264-GROUP"),
		Entry("2160p", "Fight.Club.1999.2160p.BluRay.x265-GROUP"),
		Entry("unparseable", "some.random.garbage.title"),
	)
})

var _ = Describe("rankAccepted", Label("unit", "rss"), func() {
	It("orders accepted releases by score then seeders, dropping rejects", func() {
		p := scoringTestProfile(0)
		ranked := rankAccepted(p, []indexer.SearchResult{
			{Title: "Movie.2024.1080p.WEB-DL.x264", Seeders: 5},
			{Title: "Movie.2024.720p.WEB-DL.x264", Seeders: 900},
			{Title: "Movie.2024.1080p.WEB-DL.x264-OTHER", Seeders: 50},
			{Title: "Movie.2024.2160p.BluRay.REMUX.HDR", Seeders: 1},
		}, singleRelease)

		titles := make([]string, len(ranked))
		for i, r := range ranked {
			titles[i] = r.Title
		}
		Expect(titles).To(Equal([]string{
			"Movie.2024.2160p.BluRay.REMUX.HDR",
			"Movie.2024.1080p.WEB-DL.x264-OTHER",
			"Movie.2024.1080p.WEB-DL.x264",
		}))
	})
})

var _ = Describe("scoreBest", Label("unit", "rss"), func() {
	It("picks the highest total, not the first acceptable", func() {
		results := []indexer.SearchResult{
			{Title: "Movie.2024.1080p.WEB-DL.x264", Seeders: 900},
			{Title: "Movie.2024.2160p.BluRay.REMUX.HDR", Seeders: 10},
		}

		got, ok := scoreBest(scoringTestProfile(0), results)
		Expect(ok).To(BeTrue())
		Expect(got.Title).To(ContainSubstring("REMUX"))
	})

	It("breaks score ties by seeders", func() {
		results := []indexer.SearchResult{
			{Title: "Movie.2024.1080p.WEB-DL.x264", Seeders: 5},
			{Title: "Movie.2024.1080p.WEB-DL.x264-OTHER", Seeders: 50},
		}

		got, ok := scoreBest(scoringTestProfile(0), results)
		Expect(ok).To(BeTrue())
		Expect(got.Seeders).To(Equal(uint32(50)))
	})

	It("returns false when every release is outside the band", func() {
		results := []indexer.SearchResult{
			{Title: "Movie.2024.720p.WEB-DL.x264", Seeders: 900},
			{Title: "Movie.2024.DVDRip.x264", Seeders: 900},
		}

		_, ok := scoreBest(scoringTestProfile(0), results)
		Expect(ok).To(BeFalse())
	})

	It("returns false when every release is below min_score", func() {
		results := []indexer.SearchResult{
			{Title: "Movie.2024.1080p.WEB-DL.x264", Seeders: 900},
			{Title: "Movie.2024.2160p.WEB-DL.HDR", Seeders: 900},
		}

		_, ok := scoreBest(scoringTestProfile(250), results)
		Expect(ok).To(BeFalse())
	})

	It("returns false for an empty result set", func() {
		_, ok := scoreBest(scoringTestProfile(0), nil)
		Expect(ok).To(BeFalse())
	})
})

// scoringTestProfile is a 1080p..2160p band scoring remux 200 and HDR 100, so
// a spec can tell a scored pick apart from a first-acceptable one.
func scoringTestProfile(minScore int) quality.Profile {
	GinkgoHelper()
	remux, err := quality.NewFormat("remux", []quality.Condition{{
		Type:     quality.ConditionReleaseTitle,
		Pattern:  `(?i)\bremux\b`,
		Required: true,
	}})
	Expect(err).NotTo(HaveOccurred())
	hdr, err := quality.NewFormat("hdr", []quality.Condition{{
		Type:     quality.ConditionReleaseTitle,
		Pattern:  `(?i)\bhdr\b`,
		Required: true,
	}})
	Expect(err).NotTo(HaveOccurred())

	return quality.Profile{
		MinResolution: "1080p",
		MaxResolution: "2160p",
		MinScore:      minScore,
		Formats: []quality.ScoredFormat{
			{Format: remux, Score: 200},
			{Format: hdr, Score: 100},
		},
	}
}
