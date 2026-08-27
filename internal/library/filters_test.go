package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IsVideoPath", Label("unit", "library"), func() {
	DescribeTable("extension matching",
		func(p string, want bool) {
			Expect(IsVideoPath(p)).To(Equal(want))
		},
		Entry("mkv", "Show.S01E01.mkv", true),
		Entry("uppercase extension", "Show.S01E01.MKV", true),
		Entry("mp4", "movie.mp4", true),
		Entry("nested path", "Season 01/Show.S01E01.mkv", true),
		Entry("subtitle", "Show.S01E01.srt", false),
		Entry("nfo", "movie.nfo", false),
		Entry("no extension", "README", false),
	)
})
