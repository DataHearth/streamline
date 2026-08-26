package qualityctx_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/quality/qualityctx"
)

var _ = Describe("ContextFromRelease", Label("unit", "quality"), func() {
	It("fills parsed fields and marks seeders present", func() {
		title := "Movie.2024.2160p.BluRay.REMUX.HDR.x265-GRP"
		r := qualityctx.ContextFromRelease(title, 8<<30, 50, 1)
		Expect(r.Title).To(Equal(title))
		Expect(r.Size).To(Equal(int64(8 << 30)))
		Expect(r.Seeders).To(Equal(50))
		Expect(r.HasSeeders).To(BeTrue())
		Expect(r.Resolution).To(Equal("2160p"))
		Expect(r.Source).To(Equal("BluRay"))
		Expect(r.Codec).To(Equal("HEVC"))
		Expect(r.Group).To(Equal("GRP"))
	})

	It("reports a seeder count of 0 as unknown, not as zero seeders", func() {
		r := qualityctx.ContextFromRelease("Movie.2024.1080p.WEB", 1<<30, 0, 1)
		Expect(r.Seeders).To(Equal(0))
		Expect(r.HasSeeders).To(BeFalse())
	})

	It("reports a single seeder as present", func() {
		r := qualityctx.ContextFromRelease("Movie.2024.1080p.WEB", 1<<30, 1, 1)
		Expect(r.Seeders).To(Equal(1))
		Expect(r.HasSeeders).To(BeTrue())
	})
})

var _ = Describe("ContextFromFile", Label("unit", "quality"), func() {
	It(
		"prefers probed width/codec over the filename parse and never carries seeders",
		func() {
			r := qualityctx.ContextFromFile("Movie.2024.mkv", 4<<30, 1920, "hevc")
			Expect(r.Title).To(Equal("Movie.2024.mkv"))
			Expect(r.Size).To(Equal(int64(4 << 30)))
			Expect(r.Resolution).To(Equal("1080p"))
			Expect(r.Codec).To(Equal("hevc"))
			Expect(r.HasSeeders).To(BeFalse())
		},
	)

	It("falls back to the filename parse when width is 0", func() {
		r := qualityctx.ContextFromFile(
			"Movie.2024.2160p.BluRay.x265-GRP.mkv",
			0,
			0,
			"",
		)
		Expect(r.Resolution).To(Equal("2160p"))
		Expect(r.Codec).To(Equal("HEVC"))
	})
})

var _ = Describe("ResolutionFromWidth", Label("unit", "quality"), func() {
	It("buckets by width", func() {
		Expect(quality.ResolutionFromWidth(3200)).To(Equal("2160p"))
		Expect(quality.ResolutionFromWidth(1920)).To(Equal("1080p"))
		Expect(quality.ResolutionFromWidth(1280)).To(Equal("720p"))
		Expect(quality.ResolutionFromWidth(640)).To(Equal("480p"))
		Expect(quality.ResolutionFromWidth(0)).To(Equal(""))
	})
})
