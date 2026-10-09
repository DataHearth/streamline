package bulkimport

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("adopted file quality", Label("unit", "bulkimport"), func() {
	DescribeTable("adoptedTier reads only what the extension can say",
		func(path, want string) {
			Expect(adoptedTier(path)).To(Equal(want))
		},
		Entry("flac is lossless, never hi-res", "/m/a/01.flac", "lossless"),
		Entry("upper-case extension", "/m/a/01.FLAC", "lossless"),
		Entry("mp3 states no tier", "/m/a/01.mp3", ""),
		Entry("m4a could be alac or aac", "/m/a/01.m4a", ""),
	)

	DescribeTable("adoptedBookQuality is the upper-case profile format",
		func(ext, want string) {
			Expect(adoptedBookQuality(ext)).To(Equal(want))
		},
		Entry("epub", "epub", "EPUB"),
		Entry("cbz", "cbz", "CBZ"),
		Entry("m4b", "m4b", "M4B"),
		Entry("flac audiobook", "flac", "FLAC"),
		Entry("off the ladder", "ogg", ""),
		Entry("none", "", ""),
	)
})
