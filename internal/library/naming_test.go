package library

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
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

	// The default movie template ends "[{quality}].{ext}", so a file with no
	// known quality rendered "13 Hours (2016) [].mkv" — and the renamer then
	// re-parsed that name on the next pass, which is what made the empty
	// brackets permanent.
	Describe("ApplyTemplate with an empty bracketed token", func() {
		const movieTpl = "{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}"

		It("drops the brackets and the space before them", func() {
			out := ApplyTemplate(movieTpl, map[string]string{
				"title": "13 Hours", "year": "2016",
				"tmdb_id": "300671", "quality": "", "ext": "mkv",
			})
			Expect(out).To(Equal(
				"13 Hours (2016) {tmdb-300671}/13 Hours (2016).mkv",
			))
		})

		It("keeps the brackets when the token has a value", func() {
			out := ApplyTemplate(movieTpl, map[string]string{
				"title": "13 Hours", "year": "2016",
				"tmdb_id": "300671", "quality": "2160p", "ext": "mkv",
			})
			Expect(out).To(Equal(
				"13 Hours (2016) {tmdb-300671}/13 Hours (2016) [2160p].mkv",
			))
		})

		It("drops an empty parenthesised year the same way", func() {
			out := ApplyTemplate("{title} ({year}).{ext}", map[string]string{
				"title": "Untitled", "ext": "mkv",
			})
			Expect(out).To(Equal("Untitled.mkv"))
		})

		// One half of a pair is literal text the author wrote for some other
		// reason, so an empty token must not eat it.
		It("leaves an unmatched delimiter alone", func() {
			out := ApplyTemplate("{title} [{quality}.{ext}", map[string]string{
				"title": "x", "quality": "", "ext": "mkv",
			})
			Expect(out).To(Equal("x [.mkv"))
		})
	})

	Describe("BuildMovieVars", func() {
		It("populates tmdb_id/group/source/codec when present", func() {
			vars := BuildMovieVars(
				"Test Movie",
				2024,
				12345,
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
			Expect(vars["quality"]).To(Equal("1080p"))
			Expect(vars["source"]).To(Equal("WEB-DL"))
			Expect(vars["codec"]).To(Equal("x264"))
			Expect(vars["group"]).To(Equal("GRP"))
			Expect(vars["ext"]).To(Equal("mkv"))
		})

		It("omits tmdb_id/year/ext when unset", func() {
			vars := BuildMovieVars(
				"Movie",
				0,
				0,
				ParseResult{Resolution: "720p"},
			)
			_, hasYear := vars["year"]
			_, hasTmdb := vars["tmdb_id"]
			_, hasExt := vars["ext"]
			Expect(hasYear).To(BeFalse())
			Expect(hasTmdb).To(BeFalse())
			Expect(hasExt).To(BeFalse())
		})

		It("renders Plex-style {tmdb-{tmdb_id}} literal braces around id", func() {
			vars := BuildMovieVars("X", 2024, 999, ParseResult{Extension: "mkv"})
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
					81189,
					3,
					7,
					"One Minute",
					ParseResult{
						Resolution: "1080p",
						Source:     "BluRay",
						Codec:      "x264",
						Group:      "GROUP",
						Extension:  "mkv",
					},
				)

				Expect(vars["title"]).To(Equal("Breaking Bad"))
				Expect(vars["year"]).To(Equal("2008"))
				Expect(vars["tvdb_id"]).To(Equal("81189"))
				Expect(vars["season"]).To(Equal("3"))
				Expect(vars["episode"]).To(Equal("7"))
				Expect(vars["episode_title"]).To(Equal("One Minute"))
				Expect(vars["quality"]).To(Equal("1080p"))
				Expect(vars["source"]).To(Equal("BluRay"))
				Expect(vars["codec"]).To(Equal("x264"))
				Expect(vars["group"]).To(Equal("GROUP"))
				Expect(vars["ext"]).To(Equal("mkv"))
			},
		)

		It("omits year key when year is 0", func() {
			vars := BuildEpisodeVars(
				"Show",
				0,
				0,
				1,
				1,
				"",
				ParseResult{Resolution: "720p"},
			)
			_, hasYear := vars["year"]
			_, hasTvdb := vars["tvdb_id"]
			Expect(hasYear).To(BeFalse())
			Expect(hasTvdb).To(BeFalse())
		})

		It("omits ext key when extension is empty", func() {
			vars := BuildEpisodeVars(
				"Show",
				2020,
				0,
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
				0,
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

var _ = Describe("ApplyTemplate path safety", Label("unit", "library"), func() {
	It("keeps a slash in a title from becoming a directory separator", func() {
		out := ApplyTemplate(
			"{title} ({year})/Season {season:02}",
			map[string]string{
				"title": "In/Spectre", "year": "2020", "season": "1",
			},
		)
		Expect(out).To(Equal("In-Spectre (2020)/Season 01"))
		Expect(strings.Split(out, "/")).To(HaveLen(2))
	})

	It("still splits on the template's own separators", func() {
		out := ApplyTemplate("{title}/{title}.{ext}", map[string]string{
			"title": "Rambo: Last Blood", "ext": "mkv",
		})
		Expect(strings.Split(out, "/")).To(Equal([]string{
			"Rambo - Last Blood", "Rambo - Last Blood.mkv",
		}))
	})
})

var _ = Describe("ParsedFromMediaFile", Label("unit", "library"), func() {
	// The path the renamer wrote back is not evidence: the default template
	// keeps only {quality}, so everything else was gone by the second pass.
	const renamed = "/srv/movies/13 Hours (2016) {tmdb-300671}/13 Hours (2016) [].mkv"

	It("recovers the release facts the naming template dropped", func() {
		p := ParsedFromMediaFile(&ent.MediaFile{
			Path:             renamed,
			ReleaseGroup:     "TSuNDeRe-RaWS",
			ParsedSource:     "BluRay",
			ParsedResolution: "2160p",
			ParsedCodec:      "x265",
		})

		Expect(p.Resolution).To(Equal("2160p"))
		Expect(p.Source).To(Equal("BluRay"))
		Expect(p.Codec).To(Equal("x265"))
		Expect(p.Group).To(Equal("TSuNDeRe-RaWS"))
		Expect(p.Extension).To(Equal("mkv"))
	})

	// The reported case: nothing was stored from the release name, but the
	// probe measured the file. Without this the name re-renders "[]" forever.
	It("falls back to the probe when the stored parse is empty", func() {
		p := ParsedFromMediaFile(&ent.MediaFile{
			Path: renamed, Width: 3840, VideoCodec: "hevc",
		})

		Expect(p.Resolution).To(Equal("2160p"))
		Expect(p.Codec).To(Equal("hevc"))
	})

	It("lets the probe's measurement outrank the release's claim", func() {
		p := ParsedFromMediaFile(&ent.MediaFile{
			Path: renamed, ParsedResolution: "1080p", Width: 3840,
		})

		Expect(p.Resolution).To(Equal("2160p"))
	})

	// ffprobe says "hevc" and a release says "x265"; {codec} in a template
	// means the second, so a measurement only fills what nobody claimed.
	It("keeps the release's codec spelling over the probe's", func() {
		p := ParsedFromMediaFile(&ent.MediaFile{
			Path: renamed, ParsedCodec: "x265", VideoCodec: "hevc",
		})

		Expect(p.Codec).To(Equal("x265"))
	})

	// An orphan scan or an adoption stores none of these columns, so the name
	// on disk is still the release's own and is the only evidence there is.
	It("falls back to the basename for an uninstrumented row", func() {
		path := "/srv/movies/13 Hours 2016 1080p BluRay x264-GRP.mkv"
		p := ParsedFromMediaFile(&ent.MediaFile{Path: path})

		Expect(p.Resolution).To(Equal("1080p"))
		Expect(p.Source).To(Equal("BluRay"))
		Expect(p.Codec).To(Equal("x264"))
		Expect(p.Group).To(Equal("GRP"))
	})

	It("takes the extension off the path when the name parses none", func() {
		p := ParsedFromMediaFile(&ent.MediaFile{Path: renamed})
		Expect(p.Extension).To(Equal("mkv"))
	})
})

var _ = Describe("SanitizePath", Label("unit", "library"), func() {
	DescribeTable("invalid characters",
		func(in, want string) { Expect(SanitizePath(in)).To(Equal(want)) },
		Entry("colon already spaced does not double the space",
			"2001 : L'Odyssée de l'espace", "2001 - L'Odyssée de l'espace"),
		Entry("colon without a space still gains one",
			"Rambo: Last Blood", "Rambo - Last Blood"),
		Entry("deleted characters do not leave a gap",
			"Who | What", "Who What"),
		Entry("slash becomes a dash", "In/Spectre", "In-Spectre"),
		Entry("trailing space is trimmed", "Movie: ", "Movie -"),
		Entry("plain title is untouched", "Breaking Bad", "Breaking Bad"),
	)
})
