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

var _ = Describe("ReleaseNamesWork", Label("unit", "library"), func() {
	DescribeTable(
		"reads the creator and title off the release name",
		func(name string, other bool) {
			Expect(
				ReleaseNamesWork(name, "Gojira", "From Mars to Sirius"),
			).To(Equal(!other))
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
			true,
		),
		Entry("a bare artist name", "Gojira FLAC", true),
		Entry(
			"a covers album naming the artist inside",
			"Williams.Joshua.Gojira.Relaxing.Acoustic.Covers.2024.FLAC",
			true,
		),
		Entry(
			"a scene name holding the album after the artist's name elsewhere",
			"Live.Gojira.From.Mars.to.Sirius.FLAC",
			true,
		),
	)

	DescribeTable(
		"reads the other names a tracker writes beside the artist and the title",
		func(name, creator, title string, other bool) {
			Expect(ReleaseNamesWork(name, creator, title)).To(Equal(!other))
		},
		Entry(
			"a transliteration in parentheses",
			"(Darkwave) [WEB] Kælan mikla (Kaelan mikla) - Nótt eftir nótt (Nott eftir nott) - 2018, FLAC",
			"Kælan Mikla",
			"Nótt eftir nótt",
			false,
		),
		Entry(
			"the searched name in the parentheses",
			"(Synthpop) [CD] Molchat Doma (Молчат Дома) - Monument (Монумент) - 2020, FLAC",
			"Молчат Дома",
			"Monument",
			false,
		),
		Entry(
			"the searched name in square brackets",
			"[UKR] [CD] ДахаБраха [DakhaBrakha] - Alambari - 2020, FLAC (tracks+.cue)",
			"DakhaBrakha",
			"Alambari",
			false,
		),
		Entry(
			"groups inside the name",
			"[TR24][OF] Sheena (Shiina) Ringo (椎名林檎) - Carnival (放生会) - 2024",
			"Sheena Ringo", "Carnival", false,
		),
		Entry(
			"both sides of a slash",
			"[WEB] Kælan Mikla / Kaelan Mikla - Mánadans / Manadans (2013-2014) - 2017, FLAC",
			"Kaelan Mikla",
			"Manadans",
			false,
		),
		Entry(
			"letters typed without their ligature or stroke",
			"Kaelan Mikla - Nott eftir nott (2018) [FLAC]",
			"Kælan Mikla", "Nótt eftir nótt", false,
		),
		Entry(
			"another artist's album beside a transliteration",
			"[WEB] Kælan Mikla (Kaelan Mikla) - Undir Köldum Norðurljósum - 2021, FLAC",
			"Kælan Mikla",
			"Nótt eftir nótt",
			true,
		),
	)

	DescribeTable(
		"reads past the tags a tracker opens the name on",
		func(name, creator, title string, other bool) {
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
			"the year between the artist and the album",
			"The Glitch Mob - 2021 - Revisions [FLAC]",
			"The Glitch Mob", "Revisions", false,
		),
		Entry(
			"another album behind a leading year",
			"The Glitch Mob - 2014 - Love Death Immortality [FLAC]",
			"The Glitch Mob", "Revisions", true,
		),
		Entry(
			"an album named after a year",
			"Some Band - 1984 [FLAC]",
			"Some Band", "1984", false,
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
				ReleaseNamesWork(name, "Avatar", "Feathers & Flesh"),
			).To(Equal(!other))
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

	It("names nothing when the artist normalizes to nothing", func() {
		Expect(ReleaseNamesWork("!!! - Magma [FLAC]", "!!!", "Magma")).
			To(BeFalse())
	})

	DescribeTable(
		"compares an album titled in Cyrillic",
		func(name string, other bool) {
			Expect(ReleaseNamesWork(name, "IC3PEAK", "Сказка")).To(Equal(!other))
		},
		Entry("in its own script", "IC3PEAK - СКАЗКА (2018) [MP3 320]", false),
		Entry("romanized", "IC3PEAK.Skazka[FLAC_16bits_44.1Khz](2018)", false),
		Entry(
			"another album",
			"IC3PEAK - Kiss of Death - 2022 (FLAC 44.1 kHz)",
			true,
		),
		Entry(
			"another album, romanized",
			"IC3PEAK.Sladkaja_žizn[FLAC_16bits_44.1khz](2017)",
			true,
		),
	)

	DescribeTable(
		"compares a title mixing two scripts on all of its letters",
		func(artist, title, name string, other bool) {
			Expect(ReleaseNamesWork(name, artist, title)).To(Equal(!other))
		},
		Entry("itself", "Ado", "患者A", "Ado - 患者A (2026) [FLAC]", false),
		Entry("its Latin letters alone", "Ado", "患者A", "Ado - 残夢 [FLAC]", true),
		Entry("another title ending alike",
			"Ado", "勇気のReason", "Ado - 和解のReason [FLAC]", true),
		Entry("a two-script title by its Latin half",
			"IC3PEAK", "Червь / Worm", "IC3PEAK - Worm (2020) [FLAC]", false),
		Entry("an artist in another script, no separator",
			"星野源", "エピソード", "星野源 エピソード FLAC", false),
		Entry("that artist's other album",
			"星野源", "エピソード", "星野源 - ばかのうた [FLAC]", true),
	)

	It("compares a title in a script it cannot romanize", func() {
		Expect(ReleaseNamesWork("ゴジラ - The Link [FLAC]", "ゴジラ", "Magma")).
			To(BeFalse())
		Expect(ReleaseNamesWork("坂本龍一 - 千のナイフ [FLAC]", "坂本龍一", "千のナイフ")).
			To(BeTrue())
	})

	It(
		"keeps a release named for the shorter title the library holds in full",
		func() {
			Expect(ReleaseNamesWork(
				"Gojira - Live at Hellfest [FLAC]",
				"Gojira",
				"Live at Hellfest 2016",
			)).To(BeTrue())
		},
	)
})
