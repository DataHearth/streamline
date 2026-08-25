package quality_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/quality/qualityctx"
)

var _ = Describe("Builtins", Label("unit", "quality"), func() {
	It("compiles and is name-unique", func() {
		seen := map[string]bool{}
		for _, f := range quality.Builtins() {
			Expect(seen[f.Name]).To(BeFalse(), f.Name)
			seen[f.Name] = true
		}
	})
	It("gives every builtin a non-empty description", func() {
		for _, f := range quality.Builtins() {
			Expect(f.Description).NotTo(BeEmpty(), f.Name)
		}
	})
	DescribeTable("matching",
		func(name, title string, want bool) {
			f, ok := quality.BuiltinByName(name)
			Expect(ok).To(BeTrue())
			Expect(f.Matches(qualityctx.ContextFromRelease(title, 0, 0))).
				To(Equal(want))
		},
		Entry(nil, "remux", "Movie.2024.2160p.BluRay.REMUX-GRP", true),
		Entry(nil, "remux", "Movie.2024.2160p.WEB-DL-GRP", false),
		Entry(nil, "x265", "Movie.2024.1080p.x265-GRP", true),
		Entry(nil, "x265", "Movie.2024.1080p.HEVC-GRP", true),
		Entry(nil, "x265", "Movie.2024.1080p.x264-GRP", false),
		Entry(nil, "hdr", "Movie.2024.2160p.HDR10.WEB-GRP", true),
		Entry(nil, "hdr", "Movie.2024.2160p.Dolby.Vision.WEB-GRP", true),
		Entry(nil, "hdr", "Movie.2024.1080p.DVDRip-GRP", false),
		Entry(nil, "scene-junk", "Movie.2024.1080p.HDCAM-GRP", true),
		Entry(nil, "scene-junk", "Movie.2024.1080p.BluRay-GRP", false),
		Entry(nil, "bad-group", "Movie.2024.1080p.WEB.x264-YIFY", true),
		Entry(nil, "bad-group", "Movie.2024.1080p.WEB.x264-GOODGRP", false),
		Entry(nil, "resolution-2160p", "Movie.2024.2160p.WEB-GRP", true),
		Entry(nil, "resolution-2160p", "Movie.2024.1080p.WEB-GRP", false),
		Entry(nil, "x264", "Movie.2024.1080p.WEB.x264-GRP", true),
		Entry(nil, "x264", "Movie.2024.1080p.WEB.x265-GRP", false),
		Entry(nil, "av1", "Movie.2024.2160p.WEB.AV1-GRP", true),
		Entry(nil, "av1", "Movie.2024.2160p.WEB.x264-GRP", false),
		Entry(nil, "resolution-1080p", "Movie.2024.1080p.WEB-GRP", true),
		Entry(nil, "resolution-1080p", "Movie.2024.720p.WEB-GRP", false),
		Entry(nil, "resolution-720p", "Movie.2024.720p.WEB-GRP", true),
		Entry(nil, "resolution-720p", "Movie.2024.1080p.WEB-GRP", false),
		Entry(nil, "re-encode", "Movie.2024.1080p.BluRay.RE-ENCODE-GRP", true),
		Entry(nil, "re-encode", "Movie.2024.1080p.BluRay-GRP", false),
		Entry(nil, "multi-audio", "Movie.2024.1080p.BluRay.MULTi-GRP", true),
		Entry(nil, "multi-audio", "Movie.2024.1080p.BluRay-GRP", false),
		Entry(nil, "dubbed", "Movie.2024.1080p.WEB.DUBBED-GRP", true),
		Entry(nil, "dubbed", "Movie.2024.1080p.WEB-GRP", false),
	)
	It(
		"matches x264 via the probed codec arm even when the title doesn't mention it",
		func() {
			f, ok := quality.BuiltinByName("x264")
			Expect(ok).To(BeTrue())
			ctx := qualityctx.ContextFromFile(
				"Movie.2024.1080p.WEBRip-GRP.mkv", 0, 0, "h264")
			Expect(f.Matches(ctx)).To(BeTrue())
		},
	)
})
