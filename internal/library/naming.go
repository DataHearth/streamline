package library

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var templateTokenRe = regexp.MustCompile(`\{(\w+)(?::(\d+))?\}`)

// ApplyTemplate replaces {key} and {key:02} tokens in a template string
// with values from the provided map. Format spec {key:02} zero-pads
// numeric values to the given width. Unknown tokens render as empty —
// keeps optional segments clean when not populated.
//
// Substituted values are run through SanitizePath. The rendered string is a
// path whose "/" are structural, and callers split on them: a title carrying
// one of its own ("In/Spectre", "Face/Off") would otherwise become two
// directories, which sanitising the split segments afterwards can no longer
// see.
func ApplyTemplate(tpl string, vars map[string]string) string {
	return templateTokenRe.ReplaceAllStringFunc(tpl, func(match string) string {
		parts := templateTokenRe.FindStringSubmatch(match)
		key := parts[1]
		fmtSpec := parts[2]

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

		return SanitizePath(val)
	})
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
