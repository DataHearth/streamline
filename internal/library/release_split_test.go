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

var _ = Describe("ReleaseNamesOtherWork", Label("unit", "library"), func() {
	DescribeTable(
		"reads the creator and title off the release name",
		func(name string, other bool) {
			Expect(
				ReleaseNamesOtherWork(name, "Gojira", "From Mars to Sirius"),
			).To(Equal(other))
		},
		Entry("the album", "Gojira - From Mars to Sirius (2005 Metal) [Flac 16-44]", false),
		Entry(
			"the album, scene-named",
			"Gojira.From.Mars.to.Sirius.2005.FLAC.[16bit.44.1kHz]-RmKv",
			false,
		),
		Entry("the album, remastered", "Gojira - From Mars to Sirius Remastered FLAC", false),
		Entry(
			"another album of the artist",
			"Gojira - The Link 2003 CD FLAC 16 Bit 44 1 Hz - Kefren",
			true,
		),
		Entry("another album, scene-named", "Gojira.The.Link.2003.FLAC-GRP", true),
		Entry("another artist", "Mastodon - From Mars to Sirius [FLAC]", true),
		Entry("a scene name not led by the artist", "From.Mars.to.Sirius.FLAC-GRP", false),
		Entry("a bare artist name", "Gojira FLAC", false),
	)

	It("answers false when the artist normalizes to nothing", func() {
		Expect(ReleaseNamesOtherWork("ゴジラ - The Link [FLAC]", "ゴジラ", "Magma")).
			To(BeFalse())
	})

	It("keeps a release named for the shorter title the library holds in full", func() {
		Expect(ReleaseNamesOtherWork(
			"Gojira - Live at Hellfest [FLAC]", "Gojira", "Live at Hellfest 2016",
		)).To(BeFalse())
	})
})
