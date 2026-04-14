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
// keeps optional segments like {imdb_id} clean when not populated.
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

		return val
	})
}

// BuildMovieVars creates template variables from a movie's metadata
// and parsed release info. Empty fields are omitted so ApplyTemplate's
// unknown-token policy can drop unpopulated optional segments cleanly.
func BuildMovieVars(
	title string,
	year uint16,
	tmdbID uint32,
	imdbID string,
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
	if imdbID != "" {
		vars["imdb_id"] = imdbID
	}
	if parsed.Extension != "" {
		vars["ext"] = parsed.Extension
	}
	return vars
}

// BuildEpisodeVars creates template variables for TV episode naming.
func BuildEpisodeVars(
	showTitle string,
	year, season, episode uint16,
	episodeTitle string,
	parsed ParseResult,
) map[string]string {
	vars := map[string]string{
		"title":         showTitle,
		"season":        strconv.FormatUint(uint64(season), 10),
		"episode":       strconv.FormatUint(uint64(episode), 10),
		"episode_title": episodeTitle,
		"quality":       parsed.Resolution,
	}
	if year > 0 {
		vars["year"] = strconv.FormatUint(uint64(year), 10)
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

// SanitizePath removes characters that are invalid in filenames.
func SanitizePath(s string) string {
	replacer := strings.NewReplacer(
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
	return replacer.Replace(s)
}
