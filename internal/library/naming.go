package library

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/quality"
)

// templateTokenRe matches a {key} or {key:02} token together with the
// whitespace and the bracket pair that wrap it, so an empty token can take its
// own punctuation with it. Submatches: leading whitespace, opening delimiter,
// key, format spec, closing delimiter.
var templateTokenRe = regexp.MustCompile(`(\s*)([\[(])?\{(\w+)(?::(\d+))?\}([\])])?`)

// templateGroupRe matches a bracket pair, with the whitespace before it, that
// holds no bracket and no path separator: "[{BitDepth}-{SampleRate}]". A pair
// holding several tokens that all render empty goes whole, separator included,
// where the per-token rule would leave "[-]".
var templateGroupRe = regexp.MustCompile(`\s*(?:\[[^\[\]()/]*\]|\([^\[\]()/]*\))`)

// ApplyTemplate replaces {key} and {key:02} tokens in a template string
// with values from the provided map. Format spec {key:02} zero-pads
// numeric values to the given width. Unknown tokens render as empty —
// keeps optional segments clean when not populated.
//
// A token that renders empty *and* is wrapped in a bracket pair takes the pair
// and the whitespace before it with it. The default movie template ends
// "[{quality}].{ext}", so a file whose quality is unknown would otherwise land
// as "13 Hours (2016) [].mkv" — and since the renamer re-parses that name on
// the next pass, the empty brackets would be self-perpetuating. Punctuation in a
// template delimits a value; with no value there is nothing to delimit. A bare
// token is left alone: "{title}-{group}" has no pair to remove, and inventing
// a rule for the separator would change names nobody complained about.
//
// Substituted values are run through SanitizePath. The rendered string is a
// path whose "/" are structural, and callers split on them: a title carrying
// one of its own ("In/Spectre", "Face/Off") would otherwise become two
// directories, which sanitising the split segments afterwards can no longer
// see.
func ApplyTemplate(tpl string, vars map[string]string) string {
	tpl = templateGroupRe.ReplaceAllStringFunc(tpl, func(match string) string {
		tokens := templateTokenRe.FindAllStringSubmatch(match, -1)
		if len(tokens) < 2 {
			return match
		}
		for _, t := range tokens {
			if renderToken(vars, t[3], t[4]) != "" {
				return match
			}
		}
		return ""
	})
	return templateTokenRe.ReplaceAllStringFunc(tpl, func(match string) string {
		parts := templateTokenRe.FindStringSubmatch(match)
		space, open, key, fmtSpec, closing := parts[1], parts[2], parts[3], parts[4], parts[5]

		val := renderToken(vars, key, fmtSpec)
		if val == "" && bracketPair(open, closing) {
			return ""
		}
		return space + open + val + closing
	})
}

// renderToken resolves one token's value. An unknown key renders empty, which
// is what keeps an optional segment clean when nothing populated it.
func renderToken(vars map[string]string, key, fmtSpec string) string {
	val, ok := vars[key]
	if !ok {
		return ""
	}
	if fmtSpec != "" {
		width, _ := strconv.Atoi(fmtSpec)
		if n, err := strconv.Atoi(val); err == nil {
			return fmt.Sprintf("%0*d", width, n)
		}
	}
	// A value is provider data, so one rendering to "." or ".." must not
	// survive as a path segment of its own: a title of ".." under a "{title}/"
	// template walks the rename out of the library root. Literal dots in the
	// template are the admin's and are left for the importer's containment
	// check to refuse.
	if v := SanitizePath(val); v != "." && v != ".." {
		return v
	}
	return "_"
}

// bracketPair reports whether the two delimiters around a token open and close
// each other. One half alone is literal text the template author wrote for
// some other reason, so it survives an empty token untouched.
func bracketPair(open, closing string) bool {
	return (open == "[" && closing == "]") || (open == "(" && closing == ")")
}

// BuildMovieVars creates template variables from a movie's metadata
// and parsed release info. Empty fields are omitted so ApplyTemplate's
// unknown-token policy can drop unpopulated optional segments cleanly.
func BuildMovieVars(
	title string,
	year uint16,
	tmdbID uint32,
	parsed ParseResult,
) map[string]string {
	vars := map[string]string{
		"title":   title,
		"quality": parsed.Resolution,
		"source":  parsed.Source,
		"codec":   parsed.Codec,
		"group":   parsed.Group,
	}
	if year > 0 {
		vars["year"] = strconv.FormatUint(uint64(year), 10)
	}
	if tmdbID != 0 {
		vars["tmdb_id"] = strconv.FormatUint(uint64(tmdbID), 10)
	}
	if parsed.Extension != "" {
		vars["ext"] = parsed.Extension
	}
	return vars
}

// BuildEpisodeVars creates template variables for TV episode naming.
func BuildEpisodeVars(
	showTitle string,
	year uint16,
	tvdbID uint32,
	season, episode uint16,
	episodeTitle string,
	parsed ParseResult,
) map[string]string {
	vars := map[string]string{
		"title":         showTitle,
		"season":        strconv.FormatUint(uint64(season), 10),
		"episode":       strconv.FormatUint(uint64(episode), 10),
		"episode_title": episodeTitle,
		"quality":       parsed.Resolution,
		"source":        parsed.Source,
		"codec":         parsed.Codec,
		"group":         parsed.Group,
	}
	if year > 0 {
		vars["year"] = strconv.FormatUint(uint64(year), 10)
	}
	if tvdbID != 0 {
		vars["tvdb_id"] = strconv.FormatUint(uint64(tvdbID), 10)
	}
	if parsed.Extension != "" {
		vars["ext"] = parsed.Extension
	}
	if parsed.AbsoluteNumber > 0 {
		vars["absolute"] = strconv.FormatUint(uint64(parsed.AbsoluteNumber), 10)
	}
	if parsed.AirDate != nil {
		vars["air_date"] = parsed.AirDate.Format("2006-01-02")
	}
	return vars
}

// BuildAlbumTrackVars creates template variables for music track naming. The
// keys are capitalised because the music template is: ApplyTemplate looks a
// key up verbatim. Disc renders as "<n>-" for a multi-disc album and is absent
// otherwise, so a single-disc file name carries no disc prefix.
func BuildAlbumTrackVars(
	artist, album string,
	year uint16,
	disc uint8,
	multiDisc bool,
	track uint16,
	title, ext string,
	audio AudioNaming,
) map[string]string {
	vars := map[string]string{
		"Artist": artist,
		"Album":  album,
		"Title":  title,
		"Track":  strconv.FormatUint(uint64(track), 10),
		"ext":    ext,
	}
	if year > 0 {
		vars["Year"] = strconv.FormatUint(uint64(year), 10)
	}
	if multiDisc {
		vars["Disc"] = strconv.FormatUint(uint64(disc), 10) + "-"
	}
	if audio.Quality != "" {
		vars["Quality"] = audio.Quality
	}
	if audio.BitDepth > 0 {
		vars["BitDepth"] = strconv.FormatUint(uint64(audio.BitDepth), 10)
	}
	if audio.SampleRateHz > 0 {
		vars["SampleRate"] = strconv.FormatFloat(
			float64(audio.SampleRateHz)/1000, 'f', -1, 64,
		)
	}
	return vars
}

// AudioNaming is what a music template can say about how one file sounds:
// {Quality} is the tier, {BitDepth} and {SampleRate} (kHz: 44.1, 96) what a
// lossless file measures. A zero field renders empty.
type AudioNaming struct {
	Quality      string
	BitDepth     uint8
	SampleRateHz uint32
}

// AudioNamingOf reads the naming facts a track file's row stores.
func AudioNamingOf(f *ent.MediaFile) AudioNaming {
	n := AudioNaming{Quality: f.Quality, BitDepth: f.BitDepth}
	if f.SampleRate != nil {
		n.SampleRateHz = *f.SampleRate
	}
	return n
}

// ParsedFromMediaFile builds the naming inputs for a file already in the
// library, off the columns the importer stored rather than off its path.
//
// The path is the wrong input. The renamer *writes* that path from the naming
// template, and the default template keeps only {quality} — so the first
// rename drops the source, the codec and the group, and every pass after it
// would re-parse a name that no longer carries them. Worse, a file whose
// quality was never in the name could not recover one: it would re-render "[]"
// forever while release_group, parsed_resolution and a full probe sit on the
// row untouched. qualityctx.ContextFromRow reads the row on the scoring side
// for the same reason.
//
// Precedence runs probe → stored parse → current basename. The basename is
// last but not dropped: it is the only evidence for a file the importer never
// instrumented — an orphan scan, an adoption, a bulk import the media-probe
// backfill has not reached — where the row's columns are all empty and the
// name on disk is still the release's own.
//
// The probe wins for resolution and only backfills the codec, which is not an
// inconsistency: 2160p is 2160p however it was measured, while a codec has two
// vocabularies. ffprobe says "hevc" and a release says "x265", and {codec} in
// a naming template means the second — so the measurement fills the field only
// when nothing claimed one, rather than rewriting every name that has one.
func ParsedFromMediaFile(f *ent.MediaFile) ParseResult {
	p := Parse(filepath.Base(f.Path))
	if p.Extension == "" {
		p.Extension = strings.TrimPrefix(filepath.Ext(f.Path), ".")
	}

	if f.ParsedResolution != "" {
		p.Resolution = f.ParsedResolution
	}
	if f.ParsedSource != "" {
		p.Source = f.ParsedSource
	}
	if f.ParsedCodec != "" {
		p.Codec = f.ParsedCodec
	}
	if f.ReleaseGroup != "" {
		p.Group = f.ReleaseGroup
	}

	if r := quality.ResolutionFromDims(int(f.Width), int(f.Height)); r != "" {
		p.Resolution = r
	}
	if p.Codec == "" {
		p.Codec = f.VideoCodec
	}
	return p
}

// pathReplacer maps characters that are invalid in filenames. Package-level
// because strings.Replacer is safe for concurrent use and builds a lookup
// table once.
var pathReplacer = strings.NewReplacer(
	":", " -",
	"/", "-",
	"\\", "-",
	"<", "",
	">", "",
	"\"", "",
	"|", "",
	"?", "",
	"*", "",
)

// pathSpaceRun matches the whitespace runs the replacements leave behind.
var pathSpaceRun = regexp.MustCompile(`\s{2,}`)

// SanitizePath removes characters that are invalid in filenames.
//
// Runs of whitespace are collapsed afterwards: ":" expands to " -" and the
// deleted characters vanish outright, so either one doubles a space it already
// had beside it — "2001 : L'Odyssée" would otherwise land as "2001  - L'Odyssée"
// and "A | B" as "A  B".
func SanitizePath(s string) string {
	s = pathReplacer.Replace(s)
	s = pathSpaceRun.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// BookNaming is what a book's naming template reads.
type BookNaming struct {
	Author   string
	Title    string
	Language string
	Series   string
	Year     uint16
	// Volume is the position in the series, nil for a standalone book.
	Volume *float64
}

// BuildBookVars creates template variables for ebook and audiobook naming.
// Keys are capitalised to match the default `{Author}/{Title} ({Year})`
// templates; renderToken is case-sensitive. {Volume:02} pads a whole number
// and leaves a fractional one (1.5) as it is.
func BuildBookVars(n BookNaming) map[string]string {
	vars := map[string]string{
		"Author": n.Author,
		"Title":  n.Title,
	}
	if n.Year > 0 {
		vars["Year"] = strconv.FormatUint(uint64(n.Year), 10)
	}
	if n.Language != "" {
		vars["Language"] = n.Language
	}
	if n.Series != "" {
		vars["Series"] = n.Series
	}
	if n.Volume != nil {
		vars["Volume"] = strconv.FormatFloat(*n.Volume, 'f', -1, 64)
	}
	return vars
}
