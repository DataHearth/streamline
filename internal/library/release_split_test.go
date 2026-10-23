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
		Entry(
			"the album",
			"Gojira - From Mars to Sirius (2005 Metal) [Flac 16-44]",
			false,
		),
		Entry(
			"the album, scene-named",
			"Gojira.From.Mars.to.Sirius.2005.FLAC.[16bit.44.1kHz]-RmKv",
			false,
		),
		Entry(
			"the album, remastered",
			"Gojira - From Mars to Sirius Remastered FLAC",
			false,
		),
		Entry(
			"another album of the artist",
			"Gojira - The Link 2003 CD FLAC 16 Bit 44 1 Hz - Kefren",
			true,
		),
		Entry("another album, scene-named", "Gojira.The.Link.2003.FLAC-GRP", true),
		Entry("another artist", "Mastodon - From Mars to Sirius [FLAC]", true),
		Entry(
			"a scene name not led by the artist",
			"From.Mars.to.Sirius.FLAC-GRP",
			false,
		),
		Entry("a bare artist name", "Gojira FLAC", false),
	)

	DescribeTable(
		"reads past the tags a tracker opens the name on",
		func(name, creator, title string, other bool) {
			Expect(ReleaseNamesOtherWork(name, creator, title)).To(Equal(other))
			Expect(ReleaseNamesWork(name, creator, title)).To(Equal(!other))
		},
		Entry(
			"a genre and a medium",
			"(Groove Metal) [CD] Lamb Of God - Into Oblivion - 2026, FLAC (image+.cue), lossless",
			"Lamb of God",
			"Into Oblivion",
			false,
		),
		Entry(
			"tracker flags",
			"[TR24][OF][LDR] Gojira - Our Time Is Now - 2022 (Progressive-Groove-Death Metal)",
			"Gojira",
			"Our Time Is Now",
			false,
		),
		Entry(
			"a remaster with two years",
			"(Thrash, Experimental) [LP] [24/96] Meshuggah - Nothing 2019 Remaster - 2002/2019, FLAC (tracks)",
			"Meshuggah",
			"Nothing",
			false,
		),
		Entry(
			"another album behind the tags",
			"(Melodic Death Metal) [WEB] Avatar - Schlacht - 2007, FLAC (tracks), lossless",
			"Avatar",
			"Black Waltz",
			true,
		),
		Entry(
			"a compilation behind the tags",
			"VA - The Top 500 Heavy Metal Songs of All Time - 2017 (35 CD) - MP3 320 kb/s",
			"Meshuggah",
			"Nothing",
			true,
		),
		Entry(
			"an artist whose name opens on brackets",
			"(hed) p.e. - Blackout (2003) [FLAC]",
			"(hed) p.e.", "Blackout", false,
		),
	)

	DescribeTable(
		"reads \"&\" as \"and\"",
		func(name string, other bool) {
			Expect(
				ReleaseNamesOtherWork(name, "Avatar", "Feathers & Flesh"),
			).To(Equal(other))
		},
		Entry(
			"spelled out, scene-named",
			"Avatar.Feathers.And.Flesh.2016.FLAC-GRP",
			false,
		),
		Entry("dropped", "Avatar - Feathers Flesh (2016) [FLAC]", false),
		Entry(
			"as the album writes it",
			"Avatar - Feathers & Flesh [Deluxe Edition] (2016)",
			false,
		),
		Entry(
			"spelled out, with more title after it",
			"Avatar.Feathers.And.Flesh.In.His.Own.Words.2017.FLAC.[16bit.44kHz]-NOTAG",
			true,
		),
	)

	It("answers false when the artist normalizes to nothing", func() {
		Expect(ReleaseNamesOtherWork("ゴジラ - The Link [FLAC]", "ゴジラ", "Magma")).
			To(BeFalse())
	})

	It(
		"keeps a release named for the shorter title the library holds in full",
		func() {
			Expect(ReleaseNamesOtherWork(
				"Gojira - Live at Hellfest [FLAC]",
				"Gojira",
				"Live at Hellfest 2016",
			)).To(BeFalse())
		},
	)
})
