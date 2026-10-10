package library

import (
	"regexp"
	"strings"
)

var (
	releaseYearRe  = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)
	releaseDelimRe = regexp.MustCompile(`[._]+`)
	// releaseTagRe finds the first format or quality word after an audio or book
	// title that carries no brackets ("Elantris Unabridged M4B 128kbps", "Hi
	// Scores FLAC 24bit"). It is anchored on word boundaries so a title word
	// that merely contains one ("Cdrom", "Webs") is left alone.
	releaseTagRe = regexp.MustCompile(
		`(?i)\b(?:epub|azw3?|mobi|pdf|cbz|cbr|m4b|m4a|mp3|flac|alac|aac|ogg|opus|` +
			`unabridged|abridged|audiobook|ebook|retail|lossless|web|cd|vinyl|` +
			`v0|v2|\d{2}\s?bit|\d{2,3}(?:\.\d)?\s?khz|\d{2,4}\s?k(?:bps)?)\b`,
	)
)

// SplitCreatorTitle splits a "Creator - Title (2020) [FLAC]" release name into
// its creator and title parts. Scene-style names without a " - " separator
// yield ok=false: there is no reliable place to cut them.
func SplitCreatorTitle(name string) (string, string, bool) {
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
	if loc := releaseTagRe.FindStringIndex(rest); loc != nil && loc[0] > 0 {
		rest = rest[:loc[0]]
	}
	creator, title := strings.TrimSpace(creator), strings.Trim(rest, " -")
	return creator, title, creator != "" && title != ""
}
