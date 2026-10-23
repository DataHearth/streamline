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

var _ = Describe("PathUnderRoot", Label("unit", "library"), func() {
	It("accepts the root itself and its children", func() {
		Expect(PathUnderRoot("/downloads", "/downloads")).To(BeTrue())
		Expect(PathUnderRoot("/downloads/a/b", "/downloads")).To(BeTrue())
	})

	It("rejects a sibling sharing the root's prefix", func() {
		Expect(PathUnderRoot("/downloads-evil/a", "/downloads")).To(BeFalse())
	})

	It("rejects a path that climbs out through dot-dot", func() {
		Expect(PathUnderRoot("/downloads/../etc/passwd", "/downloads")).To(BeFalse())
	})
})
