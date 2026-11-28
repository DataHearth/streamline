package library

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Filename Parser", Label("unit", "library"), func() {
	DescribeTable("movie filenames",
		func(input string, expected ParseResult) {
			result := Parse(input)
			Expect(result.Title).To(Equal(expected.Title))
			Expect(result.Year).To(Equal(expected.Year))
			Expect(result.Resolution).To(Equal(expected.Resolution))
			Expect(result.Source).To(Equal(expected.Source))
			Expect(result.Codec).To(Equal(expected.Codec))
			Expect(result.Group).To(Equal(expected.Group))
		},
		Entry(
			"standard movie",
			"Interstellar.2014.1080p.BluRay.x264-SPARKS",
			ParseResult{
				Title:      "Interstellar",
				Year:       2014,
				Resolution: "1080p",
				Source:     "BluRay",
				Codec:      "x264",
				Group:      "SPARKS",
			},
		),
		Entry(
			"movie with The",
			"The.Matrix.1999.2160p.UHD.BluRay.REMUX.HEVC-FraMeSToR",
			ParseResult{
				Title:      "The Matrix",
				Year:       1999,
				Resolution: "2160p",
				Source:     "BluRay",
				Codec:      "HEVC",
				Group:      "FraMeSToR",
			},
		),
		Entry(
			"Remux source",
			"Dune.2021.2160p.UHD.Remux.HEVC-GROUP",
			ParseResult{
				Title:      "Dune",
				Year:       2021,
				Resolution: "2160p",
				Source:     "Remux",
				Codec:      "HEVC",
				Group:      "GROUP",
			},
		),
		Entry(
			"WEB-DL source",
			"Dune.Part.Two.2024.720p.WEB-DL.DD5.1.H.264-GROUP",
			ParseResult{
				Title:      "Dune Part Two",
				Year:       2024,
				Resolution: "720p",
				Source:     "WEB-DL",
				Codec:      "x264",
				Group:      "GROUP",
			},
		),
		Entry(
			"no year",
			"Some.Movie.1080p.BluRay.x264-GROUP",
			ParseResult{
				Title:      "Some Movie",
				Year:       0,
				Resolution: "1080p",
				Source:     "BluRay",
				Codec:      "x264",
				Group:      "GROUP",
			},
		),
		Entry(
			"no group",
			"Interstellar.2014.1080p.BluRay.x264",
			ParseResult{
				Title:      "Interstellar",
				Year:       2014,
				Resolution: "1080p",
				Source:     "BluRay",
				Codec:      "x264",
				Group:      "",
			},
		),
		Entry(
			"parenthesized year drops dangling bracket from title",
			"A Silent Voice The Movie (2016) [imdb-tt5323662] - [WEBDL-1080p] [AAC 5.1] [x265].mkv",
			ParseResult{
				Title:      "A Silent Voice The Movie",
				Year:       2016,
				Resolution: "1080p",
				Source:     "WEB-DL",
				Codec:      "HEVC",
				Group:      "",
				Extension:  "mkv",
			},
		),
	)

	DescribeTable("release group extraction",
		func(input, wantGroup string) {
			Expect(Parse(input).Group).To(Equal(wantGroup))
		},
		Entry("dot group after codec",
			"Breaking.Bad.S01.MULTI.1080p.BluRay.x265.RamirouHD", "RamirouHD"),
		Entry("dot group after resolution",
			"Breaking.Bad.S04.Multi.1080p.PopHD", "PopHD"),
		Entry("dash group still wins",
			"Breaking.Bad.INTEGRALE.MULTI.2160p.NF.x265.DDP5.1-R3DUCT0", "R3DUCT0"),
		Entry(
			"dash group with internal spaces",
			"Breaking.Bad.COMPLETE.S01-S05.Bluray.Remux.1080p.Multi.HDMA.AC3-MAN OF STYLE",
			"MAN OF STYLE",
		),
		Entry("space after dash is a separator, not a group",
			"Breaking Bad - S01E01 - Pilot", ""),
		Entry("trailing codec is not a group",
			"Interstellar.2014.1080p.BluRay.x264", ""),
		Entry("trailing MULTI tag is not a group",
			"Breaking.Bad.S01.1080p.WEB.MULTI", ""),
		Entry("plain title last word is not a group", "The.Office", ""),
		Entry(
			"Blu-Ray tail is not a group",
			"Hellboy The Crooked Man 2024 1080p Blu-Ray HEVC x265 10Bit DDP5 1 Subs KINGDOM",
			"",
		),
		Entry("bare Blu-Ray tail is not a group",
			"Hellboy 2004 DC 1080p Blu-Ray", ""),
		Entry("WEB-DL tail is not a group", "Hellboy 2019 FRENCH 1080p WEB-DL", ""),
		Entry("trailing container extension is dropped",
			"Hellboy 2019 FRENCH 1080p WEB-DL x264-Slay3R mkv", "Slay3R"),
		Entry("bare container extension is not a group",
			"Hellboy 2019 1080p WEB x264-mp4", ""),
		Entry(
			"hyphenated group keeps both halves",
			"The.Elusive.Samurai.S02E05.VOSTFR.1080p.WEB.AAC.2.0.x264-tsundere-raws",
			"tsundere-raws",
		),
		Entry(
			"hyphenated source tag does not widen the group",
			"The.Elusive.Samurai.S02E05.1080p.WEB-DL.x264-tsundere-raws",
			"tsundere-raws",
		),
		Entry(
			"bracketed tail after the group",
			"That Time I Got Reincarnated as a Slime S04E08 VOSTFR 1080p WEB x264 AAC -Tsundere-Raws (CR)",
			"Tsundere-Raws",
		),
	)

	It("widens a dash-chained group in one pass", func(_ SpecContext) {
		chain := strings.Repeat("ab-", 100_000)
		group, rest := expandHyphenatedGroup(
			"grp",
			"Show.1080p.x264-"+chain[:len(chain)-1],
		)
		Expect(group).To(Equal(chain + "grp"))
		Expect(rest).To(Equal("Show.1080p.x264"))
	}, SpecTimeout(2*time.Second))

	It("keeps a bracketed year the group was appended after", func() {
		r := Parse("Hellboy (2019) 1080p WEB-DL x264-Slay3R (FR)")
		Expect(r.Group).To(Equal("Slay3R"))
		Expect(r.Year).To(BeEquivalentTo(2019))
	})

	DescribeTable(
		"whole-series / multi-season pack detection",
		func(input string, want bool) {
			Expect(IsWholeSeriesPack(input)).To(Equal(want))
		},
		Entry("complete tag", "Breaking.Bad.COMPLETE.MULTI.1080p.WEB-GRP", true),
		Entry(
			"integrale tag",
			"Breaking.Bad.INTEGRALE.MULTI.1080p.WEB.x265-NoTAG",
			true,
		),
		Entry("integral tag", "Breaking.Bad.INTEGRAL.1080p.BluRay.x265-GRP", true),
		Entry("season range", "Breaking.Bad.COMPLETE.S01-S05.1080p-GRP", true),
		Entry("adjacent seasons", "Show.S01.S02.1080p.WEB-GRP", true),
		Entry("single season pack is not whole-series",
			"Breaking.Bad.S01.MULTI.1080p.BluRay.x265.RamirouHD", false),
		Entry("resolution after season is not a range",
			"Breaking.Bad.S01.1080p.BluRay.x265-GRP", false),
		Entry("single episode is not whole-series",
			"Breaking.Bad.S01E05.1080p.WEB-GRP", false),
		Entry(
			"complete qualifies the season it names",
			"Breaking Bad S05 Complete Multi 1080p NF WEB-DL DD 5.1 AVC-D3L0P",
			false,
		),
		Entry("so does the French spelling",
			"Breaking Bad Saison 04 Complète Multi [EN FR] 1080p PopHD", false),
		Entry("a bare French integral is still whole-series",
			"Breaking Bad Intégrale CUSTOM MULTi 2160p WEBRip H265-Dam", true),
		Entry("an ampersand chain is a multi-season pack",
			"[KURISU_] Demon Slayer Kimetsu No Yaiba S01 & S02 & S03 1080P WEBRip",
			true),
		Entry("so is a plus-joined pair",
			"Demon Slayer S01+S02 1080p WEBRip", true),
	)

	DescribeTable(
		"resolution is canonicalised, whatever casing the group used",
		func(input, wantRes, wantTitle string) {
			r := Parse(input)
			Expect(r.Resolution).To(Equal(wantRes))
			Expect(r.Title).To(Equal(wantTitle))
		},
		Entry("uppercase P", "Demon.Slayer.S01.1080P.WEBRip.HEVC-GRP",
			"1080p", "Demon Slayer"),
		Entry("lowercase stays", "Demon.Slayer.S01.1080p.WEBRip.HEVC-GRP",
			"1080p", "Demon Slayer"),
		Entry("4K folds into its bucket", "Demon.Slayer.S01.4K.WEBRip.HEVC-GRP",
			"2160p", "Demon Slayer"),
	)

	DescribeTable("multi-season span bounds",
		func(input string, want SeasonSpan) {
			Expect(ParseSeasonSpan(input)).To(Equal(want))
		},
		Entry("dash range", "Show.S01-S05.1080p-GRP", SeasonSpan{From: 1, To: 5}),
		// The pairwise match consumes "S01 & S02" and resumes past it, so only
		// reading every token sees the third season.
		Entry("ampersand chain reaches the last season",
			"[KURISU_] Show S01 & S02 & S03 1080P WEBRip",
			SeasonSpan{From: 1, To: 3}),
	)

	DescribeTable(
		"spelled-out season packs",
		func(input string, wantSeason uint16, wantTitle string) {
			r := Parse(input)
			Expect(r.SeasonPack).To(BeTrue())
			Expect(r.Season).To(Equal(wantSeason))
			Expect(r.Title).To(Equal(wantTitle))
		},
		Entry("French, zero-padded",
			"Breaking Bad Saison 04 Complète Multi [EN FR] 1080p PopHD",
			uint16(4), "Breaking Bad"),
		Entry("English long form, unpadded",
			"Breaking Bad Season 4 Complete 1080p WEB-DL",
			uint16(4), "Breaking Bad"),
	)

	DescribeTable("extension stripping",
		func(filename, wantExt, wantTitle string) {
			r := Parse(filename)
			Expect(r.Extension).To(Equal(wantExt))
			Expect(r.Title).To(Equal(wantTitle))
		},
		Entry("mkv extension stripped", "Interstellar.2014.1080p.mkv",
			"mkv", "Interstellar"),
		Entry("mp4 extension stripped", "Interstellar.2014.1080p.mp4",
			"mp4", "Interstellar"),
		Entry("uppercase extension also stripped (normalized)",
			"Interstellar.2014.1080p.MKV", "mkv", "Interstellar"),
		Entry("unknown extension kept as part of filename",
			"Interstellar.2014.1080p.xyz", "", "Interstellar"),
		Entry("no extension at all",
			"Interstellar.2014.1080p", "", "Interstellar"),
	)

	DescribeTable("source normalization",
		func(filename, wantSource string) {
			Expect(Parse(filename).Source).To(Equal(wantSource))
		},
		Entry("BluRay", "Film.2020.1080p.BluRay.x264-GRP", "BluRay"),
		Entry("BDRip normalized to BluRay",
			"Film.2020.1080p.BDRip.x264-GRP", "BluRay"),
		Entry("BRRip normalized to BluRay",
			"Film.2020.1080p.BRRip.x264-GRP", "BluRay"),
		Entry("WEB-DL", "Film.2020.1080p.WEB-DL.x264-GRP", "WEB-DL"),
		Entry("WEBDL normalized to WEB-DL",
			"Film.2020.1080p.WEBDL.x264-GRP", "WEB-DL"),
		Entry("WEBRip", "Film.2020.1080p.WEBRip.x264-GRP", "WEBRip"),
		Entry("HDTV", "Film.2020.1080p.HDTV.x264-GRP", "HDTV"),
		Entry("DVDRip", "Film.2020.1080p.DVDRip.x264-GRP", "DVDRip"),
		Entry("bare WEB normalized to WEB-DL",
			"Film.2020.1080p.WEB.x264-GRP", "WEB-DL"),
		Entry("Remux standalone", "Film.2020.1080p.Remux.x264-GRP", "Remux"),
		Entry("REMUX normalized to Remux",
			"Film.2020.1080p.REMUX.x264-GRP", "Remux"),
		Entry("unknown source left blank",
			"Film.2020.1080p.x264-GRP", ""),
	)

	DescribeTable("codec normalization",
		func(filename, wantCodec string) {
			Expect(Parse(filename).Codec).To(Equal(wantCodec))
		},
		Entry("x264", "Film.2020.1080p.BluRay.x264-GRP", "x264"),
		Entry("H.264 → x264", "Film.2020.1080p.BluRay.H.264-GRP", "x264"),
		Entry("H264 → x264", "Film.2020.1080p.BluRay.H264-GRP", "x264"),
		Entry("AVC → x264", "Film.2020.1080p.BluRay.AVC-GRP", "x264"),
		Entry("x265 → HEVC", "Film.2020.2160p.BluRay.x265-GRP", "HEVC"),
		Entry("H.265 → HEVC", "Film.2020.2160p.BluRay.H.265-GRP", "HEVC"),
		Entry("HEVC", "Film.2020.2160p.BluRay.HEVC-GRP", "HEVC"),
		Entry("AV1", "Film.2020.2160p.WEB.AV1-GRP", "AV1"),
	)

	DescribeTable("TV episode filenames",
		func(input string, expected ParseResult) {
			result := Parse(input)
			Expect(result.Title).To(Equal(expected.Title))
			Expect(result.Season).To(Equal(expected.Season))
			Expect(result.Episode).To(Equal(expected.Episode))
			Expect(result.Resolution).To(Equal(expected.Resolution))
		},
		Entry(
			"standard episode",
			"Breaking.Bad.S01E01.720p.BluRay.x264-DEMAND",
			ParseResult{
				Title:      "Breaking Bad",
				Season:     1,
				Episode:    1,
				Resolution: "720p",
			},
		),
		// Kaamelott's first four Livres each run to 100 shorts. Pinned at two
		// digits this would parse as episode 10, so the file would match the
		// wrong episode — silently, with nothing about the name looking
		// malformed.
		Entry(
			"three-digit episode number",
			"Kaamelott.S01E100.1080p.BDRIP.x265-GRP",
			ParseResult{
				Title:      "Kaamelott",
				Season:     1,
				Episode:    100,
				Resolution: "1080p",
			},
		),
		Entry(
			"numeric title keeps its digits and takes the trailing year",
			"1917 - 2019 ",
			ParseResult{Title: "1917", Year: 2019},
		),
		Entry(
			"title opening with a year-like number",
			"2001 A Space Odyssey - 1968 ",
			ParseResult{Title: "2001 A Space Odyssey", Year: 1968},
		),
		Entry(
			"year-like suffix is not eaten as the release year",
			"Blade Runner 2049 - 2017 ",
			ParseResult{Title: "Blade Runner 2049", Year: 2017},
		),
		Entry(
			"repeated year keeps the title's own copy",
			"Fantasia 2000 (2000) [1080p]",
			ParseResult{Title: "Fantasia 2000", Year: 2000, Resolution: "1080p"},
		),
		Entry(
			"a leading year with nothing after it stays in the title",
			"1917.mkv",
			ParseResult{Title: "1917", Year: 0},
		),
		Entry(
			"episode with year in title",
			"The.Fall.Guy.2024.S01E05.1080p.WEB.H.265-SuccessfulCrab",
			ParseResult{
				Title:      "The Fall Guy",
				Year:       2024,
				Season:     1,
				Episode:    5,
				Resolution: "1080p",
			},
		),
	)

	Describe("TV release kinds", func() {
		It("detects a season pack (S03 with no episode)", func() {
			r := Parse("The.Black.Sea.S03.1080p.WEB-DL.x265-GRP")
			Expect(r.Season).To(Equal(uint16(3)))
			Expect(r.Episode).To(Equal(uint16(0)))
			Expect(r.SeasonPack).To(BeTrue())
		})

		It("cuts a season pack's title at the season token", func() {
			r := Parse("Good.Omens.S03.MULTi.VF2.1080p.WEB.H264-FW")
			Expect(r.SeasonPack).To(BeTrue())
			Expect(r.Title).To(Equal("Good Omens"))
		})

		It("detects anime absolute numbering", func() {
			r := Parse("[Grp] Hokkaido Signal - 18 [1080p].mkv")
			Expect(r.AbsoluteNumber).To(Equal(uint16(18)))
		})

		It("detects a daily date", func() {
			r := Parse("Last.Word.Tonight.2026.05.12.1080p.WEB.h264-GRP")
			Expect(r.AirDate).NotTo(BeNil())
			Expect(r.AirDate.Year()).To(Equal(2026))
		})

		// One special filed under a season: the segment between the season
		// token and the first technical tag holds "special" and a title word.
		DescribeTable(
			"reads a special named under a season token",
			func(name string) {
				r := Parse(name)
				Expect(r.TitledSpecial).To(BeTrue())
				Expect(r.SeasonPack).To(BeFalse())
				Expect(r.AbsoluteNumber).To(BeZero())
				Expect(r.Title).To(Equal("Show"))
				Expect(ParseSeasonSpan(name)).To(Equal(SeasonSpan{}))
			},
			Entry(
				"title after the word",
				"Show.S05.Special.The.Title.1080p.WEB-GRP",
			),
			Entry("title joined by a dash",
				"Show.S02.Special-Inside.Chesters.Mill.HDTV.x264-BAJSKORV"),
			Entry(
				"title before the word",
				"Show.S05.Christmas.Special.720p.HDTV-GRP",
			),
			Entry("ending the name", "Show.S00.Christmas.Special"),
			Entry("ending a file name", "Show.S00.Christmas.Special.mkv"),
			Entry(
				"a number after the title",
				"Show S00 Christmas Special 03 [1080p]",
			),
			Entry("a date after the title",
				"Show.S00.Christmas.Special.2020.12.25.720p.HDTV-GRP"),
			Entry("a complete tag past the technical tags",
				"Show.S05.Special.The.Title.1080p.COMPLETE.BluRay-GRP"),
		)

		// "Show.S01.Reunion.Special.1080p-GRP" is one special; each entry
		// adds the one signal that makes it a pack shipping its special.
		DescribeTable(
			"reads each pack signal on its own",
			func(name string) {
				Expect(Parse("Show.S01.Reunion.Special.1080p-GRP").TitledSpecial).
					To(BeTrue())
				r := Parse(name)
				Expect(r.TitledSpecial).To(BeFalse())
				Expect(r.SeasonPack).To(BeTrue())
				Expect(r.Season).To(Equal(uint16(1)))
			},
			Entry(
				"another season token",
				"Show.S01-S03.The.Reunion.Special.1080p-GRP",
			),
			Entry("a complete tag", "Show.S01.COMPLETE.Reunion.Special.1080p-GRP"),
			Entry("an ampersand", "Show.S01.&.Reunion.Special.1080p-GRP"),
			Entry("a square bracket", "Show S01 [Reunion Special] 1080p"),
			Entry("a curly bracket", "Show S01 {Reunion Special} 1080p"),
			Entry("and", "Show.S01.and.Reunion.Special.1080p-GRP"),
			Entry("incl", "Show.S01.incl.Reunion.Special.1080p-GRP"),
			Entry("inc", "Show.S01.inc.Reunion.Special.1080p-GRP"),
			Entry("including", "Show.S01.including.Reunion.Special.1080p-GRP"),
			Entry("included before", "Show.S01.included.Reunion.Special.1080p-GRP"),
			Entry("included after", "Show.S01.Reunion.Special.Included.1080p-GRP"),
			Entry("plus", "Show.S01.plus.Reunion.Special.1080p-GRP"),
			Entry("w", "Show.S01.w.Reunion.Special.1080p-GRP"),
			Entry("et", "Show.S01.et.Reunion.Special.1080p-GRP"),
			Entry("avec", "Show.S01.avec.Reunion.Special.1080p-GRP"),
			Entry("ed", "Show.S01.Reunion.Special.Ed.1080p-GRP"),
			Entry("extras", "Show.S01.Reunion.Special.Extras.1080p-GRP"),
			Entry("bonus", "Show.S01.Reunion.Special.Bonus.1080p-GRP"),
			Entry("an episode range", "Show.S01.E01-E10.Reunion.Special.1080p-GRP"),
			Entry("an episode range with one E",
				"Show.S01.E01-10.Reunion.Special.1080p-GRP"),
			Entry(
				"special features ending the name",
				"Show.S01.DVD.Special.Features",
			),
		)

		DescribeTable(
			"keeps a pack that mentions a special as a pack",
			func(name string, season uint16) {
				r := Parse(name)
				Expect(r.TitledSpecial).To(BeFalse())
				Expect(r.SeasonPack).To(BeTrue())
				Expect(r.Season).To(Equal(season))
			},
			Entry("a plus", "Show.S01.+.Special.1080p.BluRay-GRP", uint16(1)),
			Entry("a fansub pack with its special",
				"[Group] Show S02 + Special [BD 1080p]", uint16(2)),
			Entry("a plus and a named special",
				"Top.Gear.S22.+.Patagonia.Special.720p.HDTV-GRP", uint16(22)),
			Entry(
				"special features",
				"Show.S01.Special.Features.1080p-GRP",
				uint16(1),
			),
			Entry("special features past the tags",
				"Show.S01.1080p.BluRay.Special.Features-GRP", uint16(1)),
			Entry("with", "Show.S01.with.Special.1080p-GRP", uint16(1)),
			Entry(
				"a plus with no space",
				"Show.S01.OVA+Special.1080p-GRP",
				uint16(1),
			),
			Entry("a plus right after the token",
				"Show.S01+Special.Title.1080p-GRP", uint16(1)),
			Entry("an extended edition",
				"Show.S01.Special.Extended.Edition.1080p-GRP", uint16(1)),
			Entry("a collectors edition",
				"Show.S01.Special.Collectors.Edition.1080p-GRP", uint16(1)),
			Entry("an edition tag",
				"Show.S01.Special.Edition.1080p.BluRay.x264-GRP", uint16(1)),
			Entry(
				"an edition tag ending the name",
				"Show.S01.Special.Edition",
				uint16(1),
			),
			Entry("an edition tag ending a file name",
				"Show.S01.Special.Edition.mkv", uint16(1)),
			Entry("the word alone", "Show.S00.Special.1080p.WEB-GRP", uint16(0)),
			Entry("a complete tag",
				"Show.S01.COMPLETE.incl.Special.1080p-GRP", uint16(1)),
			Entry("a bracket", "Show S01 (BD Special Box) 1080p", uint16(1)),
			Entry("the specials pack",
				"The.Big.Bang.Theory.S00.Specials.1080p.WEB-GRP", uint16(0)),
			Entry("the word before the season",
				"Big.Special.Show.S05.1080p.HDTV.x264-GRP", uint16(5)),
		)

		It("keeps a season range with a special a range", func() {
			name := "Show.S01-S03.+.Special.1080p.BluRay-GRP"
			Expect(Parse(name).TitledSpecial).To(BeFalse())
			Expect(ParseSeasonSpan(name)).To(Equal(SeasonSpan{From: 1, To: 3}))
		})

		It("takes no trailing Edition for the release group", func() {
			Expect(Parse("Show.S01.Special.Edition").Group).To(BeEmpty())
		})

		// The season token is cut from the title for season 0 as for any
		// other: left in, no special's title ever equals the show's.
		DescribeTable("cuts the title at a season 0 token",
			func(name, title string) {
				Expect(Parse(name).Title).To(Equal(title))
			},
			Entry("a numbered special", "Show.S00E01.1080p.WEB-GRP", "Show"),
			Entry("special number 0", "Show.S00E00.1080p.WEB-GRP", "Show"),
			Entry("the specials pack",
				"The.Big.Bang.Theory.S00.Specials.1080p.WEB-GRP",
				"The Big Bang Theory"),
			Entry("a range opening at the specials",
				"Show.S00-S12.1080p.BluRay-GRP", "Show"),
			Entry("a spelled-out season 0", "Show Saison 0 MULTI 1080p", "Show"),
			Entry("an S00 special", "Show.S00.Christmas.Special.1080p-GRP", "Show"),
		)

		DescribeTable(
			"reads one special numbered apart from S00",
			func(name string, episode int) {
				r := Parse(name)
				Expect(r.Season).To(BeZero())
				Expect(r.Episode).To(BeEquivalentTo(episode))
				Expect(r.SeasonPack).To(BeFalse())
				Expect(r.AbsoluteNumber).To(BeZero())
				Expect(ParseSeasonSpan(name)).To(Equal(SeasonSpan{}))
			},
			Entry("fansub dash", "[Group] Show S00 - 05 [1080p]", 5),
			Entry("fansub dash and revision", "[Group] Show S00 - 05v2 (1080p)", 5),
			Entry("dash ending the name", "Show - S00 - 18", 18),
			Entry(
				"dash and a title",
				"[Group] Show S00 - 05 - The Movie [1080p]",
				5,
			),
			Entry("dash and bare tags", "[Group] Show S00 - 05 1080p WEB", 5),
			Entry("dash before the E", "Show S00 - E05 [1080p]", 5),
			Entry("spelled-out season", "Show Season 00 - 05 [1080p]", 5),
			Entry("spelled out", "Show Saison 0 Episode 5 1080p", 5),
			Entry("EP prefix", "Show.S00.EP05.1080p.WEB-GRP", 5),
			Entry("dotted", "Show.S00.E05.1080p.WEB-GRP", 5),
			Entry("spaced", "Show S00 E05 1080p WEB-GRP", 5),
		)

		DescribeTable("reads no number off a specials pack",
			func(name string) {
				r := Parse(name)
				Expect(r.SeasonPack).To(BeTrue())
				Expect(r.Season).To(BeZero())
				Expect(r.Episode).To(BeZero())
				Expect(r.AbsoluteNumber).To(BeZero())
				Expect(ParseSeasonSpan(name)).To(Equal(SeasonSpan{Specials: true}))
			},
			Entry("audio channels", "Show S00 1080p BluRay DDP5 1-GRP"),
			Entry("a year", "Show S00 1999-GRP"),
			Entry("a range", "[Group] Show S00 - 01-10 [1080p]"),
			Entry("a spaced range", "Show S00 - 01 ~ 10 [1080p]"),
			Entry("a tight range", "[Group] Show S00-12 [BD 1080p]"),
			Entry("a resolution", "Show S00 - 1080p WEB"),
			Entry("a bit depth", "Show S00 - 10bit 1080p"),
			Entry("a later range", "[Group] Show S00 OVA 01-04 [1080p]"),
		)

		It("reads a dated S00 as one special", func() {
			r := Parse("Daily.Show.S00.2011.09.25.720p.HDTV-GRP")
			Expect(r.SeasonPack).To(BeFalse())
			Expect(r.Specials).To(BeTrue())
			Expect(r.AirDate).NotTo(BeNil())
		})

		It("still parses standard SxxExx", func() {
			r := Parse("Show.S01E05.1080p.WEB-DL-GRP")
			Expect(r.Season).To(Equal(uint16(1)))
			Expect(r.Episode).To(Equal(uint16(5)))
			Expect(r.SeasonPack).To(BeFalse())
		})
	})
})

var _ = Describe("season spans", Label("unit", "library"), func() {
	DescribeTable(
		"scope read from a release name",
		func(input string, want SeasonSpan) {
			Expect(ParseSeasonSpan(input)).To(Equal(want))
		},
		Entry("single episode covers no whole season",
			"Breaking.Bad.S01E05.1080p.WEB-GRP", SeasonSpan{}),
		Entry("season pack", "Breaking.Bad.S03.1080p.BluRay-GRP",
			SeasonSpan{From: 3, To: 3}),
		Entry("season range", "Breaking.Bad.S01-S05.1080p-GRP",
			SeasonSpan{From: 1, To: 5}),
		Entry("range wins over the leading season token",
			"Breaking.Bad.S01.S02.1080p-GRP", SeasonSpan{From: 1, To: 2}),
		Entry("reversed range is normalised",
			"Show.S05-S02.1080p-GRP", SeasonSpan{From: 2, To: 5}),
		Entry("complete tag spans everything",
			"Breaking.Bad.COMPLETE.MULTI.1080p-GRP", SeasonSpan{Complete: true}),
		Entry("integrale is the same tag in French",
			"Kaamelott.INTEGRALE.1080p-GRP", SeasonSpan{Complete: true}),
		// S00 is the specials season: a span of its own, since From 0 is "no
		// season", and never a bound of the numbered range.
		Entry("specials pack", "The.Big.Bang.Theory.S00.Specials.1080p.WEB-GRP",
			SeasonSpan{Specials: true}),
		Entry("range opening at the specials", "Show.S00-S12.1080p.BluRay-GRP",
			SeasonSpan{From: 1, To: 12, Specials: true}),
		Entry("chain listing the specials", "Show.S00 & S01 & S02.1080p-GRP",
			SeasonSpan{From: 1, To: 2, Specials: true}),
		Entry("season listed with the specials", "Show.S05+S00.1080p-GRP",
			SeasonSpan{From: 5, To: 5, Specials: true}),
		Entry("specials listed with a season", "Show.S00 + S05.1080p-GRP",
			SeasonSpan{From: 5, To: 5, Specials: true}),
		Entry(
			"season and specials joined by a dot",
			"Show.S04.S00.Specials.1080p-GRP",
			SeasonSpan{From: 4, To: 4, Specials: true},
		),
		Entry("range listed with the specials", "Show.S03-S05+S00.1080p-GRP",
			SeasonSpan{From: 3, To: 5, Specials: true}),
		Entry("specials listed with a range", "Show.S00+S03-S05.1080p-GRP",
			SeasonSpan{From: 3, To: 5, Specials: true}),
		Entry("spaced range opening at the specials", "Show S00 - S05 1080p-GRP",
			SeasonSpan{From: 1, To: 5, Specials: true}),
		Entry("complete tag adding the specials", "Show.Integrale + S00.1080p-GRP",
			SeasonSpan{Complete: true, Specials: true}),
		Entry("complete tag qualifying a season listed with the specials",
			"Show.S05.COMPLETE.S00.1080p-GRP", SeasonSpan{From: 5, To: 5}),
		Entry("complete tag qualifying the specials", "Show.S00.COMPLETE.1080p-GRP",
			SeasonSpan{Specials: true}),
		Entry("complete range opening at the specials",
			"Show.COMPLETE.S00-S05.1080p-GRP",
			SeasonSpan{From: 1, To: 5, Specials: true}),
	)

	counts := map[uint16]int{0: 4, 1: 7, 2: 13, 3: 13}

	DescribeTable("episodes totalled against the library's season lengths",
		func(span SeasonSpan, want int) {
			Expect(span.EpisodeCount(counts)).To(Equal(want))
		},
		Entry("one season", SeasonSpan{From: 2, To: 2}, 13),
		Entry("a range sums its seasons", SeasonSpan{From: 1, To: 3}, 33),
		// An integral is taken to hold the numbered seasons only, as the episode
		// search reads one: the four specials stay out of its size.
		Entry("complete sums every numbered season", SeasonSpan{Complete: true}, 33),
		// Zero is "the library cannot say", which callers read as unknown and
		// degrade to an unscaled bound — never as an empty pack.
		Entry("no season named", SeasonSpan{}, 0),
		Entry("season the library does not track", SeasonSpan{From: 9, To: 9}, 0),
		Entry("range partly tracked counts what it knows",
			SeasonSpan{From: 3, To: 9}, 13),
		Entry("complete with the specials",
			SeasonSpan{Complete: true, Specials: true}, 37),
		Entry("specials pack", SeasonSpan{Specials: true}, 4),
		Entry("range with the specials",
			SeasonSpan{From: 1, To: 3, Specials: true}, 37),
	)

	It("reads a complete pack of a show tracking only specials as unknown", func() {
		Expect(SeasonSpan{Complete: true}.EpisodeCount(map[uint16]int{0: 4})).
			To(BeZero())
	})
})
