package rss

import (
	"regexp"
	"strings"
)

var (
	releaseYearRe  = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)
	releaseDelimRe = regexp.MustCompile(`[._]+`)
)

// splitCreatorTitle splits a "Creator - Title (2020) [FLAC]" release name into
// its creator and title parts. Scene-style names without a " - " separator
// yield ok=false: there is no reliable place to cut them.
func splitCreatorTitle(name string) (string, string, bool) {
	name = releaseDelimRe.ReplaceAllString(name, " ")
	creator, rest, found := strings.Cut(name, " - ")
	if !found {
		return "", "", false
	}
	if i := strings.IndexAny(rest, "(["); i >= 0 {
		rest = rest[:i]
	}
	if loc := releaseYearRe.FindStringIndex(rest); loc != nil && loc[0] > 0 {
		rest = rest[:loc[0]]
	}
	creator, title := strings.TrimSpace(creator), strings.Trim(rest, " -")
	return creator, title, creator != "" && title != ""
}
