package library

import (
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
	)

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
		Entry("WEB", "Film.2020.1080p.WEB.x264-GRP", "WEB"),
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

		It("detects anime absolute numbering", func() {
			r := Parse("[Grp] Hokkaido Signal - 18 [1080p].mkv")
			Expect(r.AbsoluteNumber).To(Equal(uint16(18)))
		})

		It("detects a daily date", func() {
			r := Parse("Last.Word.Tonight.2026.05.12.1080p.WEB.h264-GRP")
			Expect(r.AirDate).NotTo(BeNil())
			Expect(r.AirDate.Year()).To(Equal(2026))
		})

		It("still parses standard SxxExx", func() {
			r := Parse("Show.S01E05.1080p.WEB-DL-GRP")
			Expect(r.Season).To(Equal(uint16(1)))
			Expect(r.Episode).To(Equal(uint16(5)))
			Expect(r.SeasonPack).To(BeFalse())
		})
	})
})
