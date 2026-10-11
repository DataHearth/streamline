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

// ReleaseNamesOtherWork reports whether a release name can be read as
// "creator - title" and names a different creator or a different work than the
// one searched for. A scene name with no " - " is read by finding the creator's
// words at its head. Anything it cannot read answers false — a name that does
// not start with the creator, a creator or title that normalizes to nothing —
// since an interactive search shows every result and unsure is not "other".
func ReleaseNamesOtherWork(name, creator, title string) bool {
	if normalizeTitle(creator) == "" || normalizeTitle(title) == "" {
		return false
	}
	c, t, ok := SplitCreatorTitle(name)
	if !ok {
		c, t, ok = splitAtCreator(name, creator)
		if !ok {
			return false
		}
	}
	if normalizeTitle(c) == "" || normalizeTitle(t) == "" {
		return false
	}
	return !TitleMatchesStrict(c, creator) || !TitleNamesSameWork(t, title)
}

// splitAtCreator splits a release name with no " - " separator after the
// leading words that normalize to creator, then cuts the title the way
// SplitCreatorTitle does.
func splitAtCreator(name, creator string) (string, string, bool) {
	want := normalizeTitle(creator)
	words := strings.Fields(releaseDelimRe.ReplaceAllString(name, " "))
	for i := range words {
		head := normalizeTitle(strings.Join(words[:i+1], " "))
		if head == want {
			// Nothing but tags after the creator ("Artist FLAC") names no title.
			rest := strings.Join(words[i+1:], " ")
			if strings.IndexAny(rest, "([") == 0 ||
				startsWith(releaseTagRe, rest) || startsWith(releaseYearRe, rest) {
				return "", "", false
			}
			return SplitCreatorTitle(strings.Join(words[:i+1], " ") + " - " + rest)
		}
		if !strings.HasPrefix(want, head) {
			break
		}
	}
	return "", "", false
}

func startsWith(re *regexp.Regexp, s string) bool {
	loc := re.FindStringIndex(s)
	return loc != nil && loc[0] == 0
}
