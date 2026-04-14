package library

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Naming Templates", Label("unit", "library"), func() {
	Describe("ApplyTemplate", func() {
		It("should replace tokens in movie template", func() {
			tpl := "{title} ({year})/{title} ({year}) - {quality}.{ext}"
			result := ApplyTemplate(tpl, map[string]string{
				"title":   "Interstellar",
				"year":    "2014",
				"quality": "1080p",
				"ext":     "mkv",
			})
			Expect(
				result,
			).To(Equal("Interstellar (2014)/Interstellar (2014) - 1080p.mkv"))
		})

		It("should zero-pad season and episode numbers", func() {
			tpl := "Season {season:02}/S{season:02}E{episode:02} - {episode_title}.{ext}"
			result := ApplyTemplate(tpl, map[string]string{
				"season":        "1",
				"episode":       "5",
				"episode_title": "Pilot",
				"ext":           "mkv",
			})
			Expect(result).To(Equal("Season 01/S01E05 - Pilot.mkv"))
		})

		It("should handle episode without title", func() {
			tpl := "S{season:02}E{episode:02} - {episode_title}.{ext}"
			result := ApplyTemplate(tpl, map[string]string{
				"season": "3", "episode": "12", "episode_title": "", "ext": "mkv",
			})
			Expect(result).To(Equal("S03E12 - .mkv"))
		})

		It("renders unknown tokens as empty string", func() {
			result := ApplyTemplate("{title}-{nope}.{ext}", map[string]string{
				"title": "x", "ext": "mkv",
			})
			Expect(result).To(Equal("x-.mkv"))
		})
	})

	Describe("BuildMovieVars", func() {
		It("populates tmdb_id/imdb_id/group/source/codec when present", func() {
			vars := BuildMovieVars(
				"Test Movie",
				2024,
				12345,
				"tt9999",
				ParseResult{
					Resolution: "1080p",
					Source:     "WEB-DL",
					Codec:      "x264",
					Group:      "GRP",
					Extension:  "mkv",
				},
			)
			Expect(vars["title"]).To(Equal("Test Movie"))
			Expect(vars["year"]).To(Equal("2024"))
			Expect(vars["tmdb_id"]).To(Equal("12345"))
			Expect(vars["imdb_id"]).To(Equal("tt9999"))
			Expect(vars["quality"]).To(Equal("1080p"))
			Expect(vars["source"]).To(Equal("WEB-DL"))
			Expect(vars["codec"]).To(Equal("x264"))
			Expect(vars["group"]).To(Equal("GRP"))
			Expect(vars["ext"]).To(Equal("mkv"))
		})

		It("omits tmdb_id/imdb_id/year/ext when unset", func() {
			vars := BuildMovieVars(
				"Movie",
				0,
				0,
				"",
				ParseResult{Resolution: "720p"},
			)
			_, hasYear := vars["year"]
			_, hasTmdb := vars["tmdb_id"]
			_, hasImdb := vars["imdb_id"]
			_, hasExt := vars["ext"]
			Expect(hasYear).To(BeFalse())
			Expect(hasTmdb).To(BeFalse())
			Expect(hasImdb).To(BeFalse())
			Expect(hasExt).To(BeFalse())
		})

		It("renders Plex-style {tmdb-{tmdb_id}} literal braces around id", func() {
			vars := BuildMovieVars("X", 2024, 999, "", ParseResult{Extension: "mkv"})
			got := ApplyTemplate(
				"{title} ({year}) {tmdb-{tmdb_id}}/file.{ext}",
				vars,
			)
			Expect(got).To(Equal("X (2024) {tmdb-999}/file.mkv"))
		})
	})

	Describe("BuildEpisodeVars", func() {
		It(
			"creates template variables for episode with all optional fields",
			func() {
				vars := BuildEpisodeVars(
					"Breaking Bad",
					2008,
					3,
					7,
					"One Minute",
					ParseResult{
						Resolution: "1080p",
						Source:     "BluRay",
						Codec:      "x264",
						Extension:  "mkv",
					},
				)

				Expect(vars["title"]).To(Equal("Breaking Bad"))
				Expect(vars["year"]).To(Equal("2008"))
				Expect(vars["season"]).To(Equal("3"))
				Expect(vars["episode"]).To(Equal("7"))
				Expect(vars["episode_title"]).To(Equal("One Minute"))
				Expect(vars["quality"]).To(Equal("1080p"))
				Expect(vars["ext"]).To(Equal("mkv"))
			},
		)

		It("omits year key when year is 0", func() {
			vars := BuildEpisodeVars(
				"Show",
				0,
				1,
				1,
				"",
				ParseResult{Resolution: "720p"},
			)
			_, hasYear := vars["year"]
			Expect(hasYear).To(BeFalse())
		})

		It("omits ext key when extension is empty", func() {
			vars := BuildEpisodeVars(
				"Show",
				2020,
				1,
				1,
				"",
				ParseResult{Resolution: "720p"},
			)
			_, hasExt := vars["ext"]
			Expect(hasExt).To(BeFalse())
		})

		It("renders absolute + air_date tokens", func() {
			ad := time.Date(2024, 5, 12, 0, 0, 0, 0, time.UTC)
			vars := BuildEpisodeVars(
				"Hokkaido Signal",
				2024,
				1,
				18,
				"Static",
				ParseResult{
					Resolution:     "1080p",
					Extension:      "mkv",
					AbsoluteNumber: 18,
					AirDate:        &ad,
				},
			)
			Expect(vars["absolute"]).To(Equal("18"))
			Expect(vars["air_date"]).To(Equal("2024-05-12"))
		})
	})
})
