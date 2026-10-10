package library

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SplitCreatorTitle", Label("unit", "library"), func() {
	DescribeTable(
		"cuts the title before what describes the files",
		func(name, creator, title string) {
			c, t, ok := SplitCreatorTitle(name)
			Expect(ok).To(BeTrue())
			Expect(c).To(Equal(creator))
			Expect(t).To(Equal(title))
		},
		Entry(
			"bracketed tags",
			"Boards of Canada - Hi Scores [MP3 320]",
			"Boards of Canada",
			"Hi Scores",
		),
		Entry("year", "Nirvana - Nevermind 1991 FLAC", "Nirvana", "Nevermind"),
		Entry(
			"bare audiobook tags",
			"Brandon Sanderson - Elantris Unabridged M4B 128kbps",
			"Brandon Sanderson",
			"Elantris",
		),
		Entry(
			"bare lossless tags",
			"Boards of Canada - Hi Scores FLAC 24bit 96kHz",
			"Boards of Canada",
			"Hi Scores",
		),
		Entry(
			"ebook format",
			"Ted Chiang - Exhalation EPUB",
			"Ted Chiang",
			"Exhalation",
		),
		Entry(
			"a title word that only contains a tag",
			"Massive Attack - Webs of Sound",
			"Massive Attack",
			"Webs of Sound",
		),
	)

	It("keeps a title that starts with a tag word", func() {
		_, t, ok := SplitCreatorTitle("Front 242 - Web Code")
		Expect(ok).To(BeTrue())
		Expect(t).To(Equal("Web Code"))
	})
})
