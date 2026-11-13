package library

import (
	"regexp"
	"slices"
	"strings"
)

var (
	releaseYearRe     = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)
	releaseLeadYearRe = regexp.MustCompile(`^\s*(?:19|20)\d{2}\s+-\s+`)
	releaseDelimRe    = regexp.MustCompile(`[._]+`)
	// releaseTagRe finds the first format or quality word after an audio or book
	// title that carries no brackets ("Elantris Unabridged M4B 128kbps", "Hi
	// Scores FLAC 24bit"). It is anchored on word boundaries so a title word
	// that merely contains one ("Cdrom", "Webs") is left alone.
	// releaseLeadTagsRe matches the bracketed groups a tracker writes before
	// the name: RuTracker's "(Groove Metal) [CD] " or "[TR24][OF][LDR] ".
	releaseLeadTagsRe = regexp.MustCompile(`^\s*(?:[(\[][^)\]]*[)\]]\s*)+`)
	releaseTagRe      = regexp.MustCompile(
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
	// "Artist - 2021 - Album": the year leads the title. An album named
	// after a year ("Artist - 1984") has no " - " after it and stays whole.
	rest = releaseLeadYearRe.ReplaceAllString(rest, "")
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

// ReleaseNamesWork reports whether a release name reads as "creator - title"
// naming this creator and this work. A scene name with no " - " is read by
// finding the creator's words at its head, and a name opening on tracker
// tags ("(Groove Metal) [CD] Artist - Album - 2026, FLAC") is read without
// them too: cut at its first " - ", the creator was "(Groove Metal) [CD]
// Artist", and every RuTracker release was refused as another artist's.
func ReleaseNamesWork(name, creator, title string) bool {
	for _, r := range releaseReadings(name, creator) {
		if sameCreator(r[0], creator) && sameWork(r[1], title) {
			return true
		}
	}
	return false
}

// ReleaseNamedByCreator reports whether a release name reads as one of
// creator's, whatever the title. Only a " - " separator says where the
// creator ends: with no title to confirm, a scene name led by the creator's
// words could be another's ("Nirvana Tribute Discography").
func ReleaseNamedByCreator(name, creator string) bool {
	for _, r := range releaseReadings(name, "") {
		if sameCreator(r[0], creator) {
			return true
		}
	}
	return false
}

// ReleaseOpensOnCreator reports whether a release name reads as one of
// creator's under any reading, a scene name led by the creator's words
// included. It says an indexer searched for the creator, which is less than
// ReleaseNamedByCreator needs to call a release the creator's.
func ReleaseOpensOnCreator(name, creator string) bool {
	for _, r := range releaseReadings(name, creator) {
		if sameCreator(r[0], creator) {
			return true
		}
	}
	return false
}

// releaseReadings is every (creator, title) pair a release name can be read
// as: the name itself, then the name without the tags it opens on. The first
// reading stays for a creator whose own name opens on brackets ("(hed) p.e.").
// An empty creator reads " - " separated names only.
func releaseReadings(name, creator string) [][2]string {
	names := []string{name}
	if bare := releaseLeadTagsRe.ReplaceAllString(
		name,
		"",
	); bare != name &&
		bare != "" {
		names = append(names, bare)
	}
	var out [][2]string
	for _, n := range names {
		c, t, ok := SplitCreatorTitle(n)
		if !ok {
			c, t, ok = splitAtCreator(n, creator)
		}
		if !ok {
			continue
		}
		for _, c := range alternateNames(c) {
			for _, t := range alternateNames(t) {
				if TitleComparable(c) && TitleComparable(t) {
					out = append(out, [2]string{c, t})
				}
			}
		}
	}
	return out
}

var (
	bracketGroupRe = regexp.MustCompile(`[(\[]([^)\]]*)[)\]]`)
	slashRe        = regexp.MustCompile(`\s+/\s+`)
)

// alternateNames is a creator or title part and every other name a tracker
// wrote into it: RuTracker credits "Kælan Mikla (Kaelan Mikla)", "ДахаБраха
// [DakhaBrakha]" and "Sheena (Shiina) Ringo (椎名林檎)", and titles "Mánadans /
// Manadans". The part as written comes first, then it without its bracketed
// groups, each group's own text, and each side of a spaced slash.
func alternateNames(part string) []string {
	out := []string{part}
	add := func(s string) {
		if s = strings.Join(strings.Fields(s), " "); s != "" &&
			!slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	add(bracketGroupRe.ReplaceAllString(part, " "))
	for _, m := range bracketGroupRe.FindAllStringSubmatch(part, -1) {
		add(m[1])
	}
	for _, side := range slashRe.Split(part, -1) {
		add(side)
		add(bracketGroupRe.ReplaceAllString(side, " "))
	}
	return out
}

// sameCreator and sameWork also read "&" as "and" on both sides, since
// normalizing drops the one and keeps the other: "Avatar.Feathers.And.Flesh"
// never equalled "Feathers & Flesh". The plain comparison stays for a name
// that drops the "&" altogether.
func sameCreator(release, creator string) bool {
	return TitleMatchesStrict(release, creator) ||
		TitleMatchesStrict(ampersandAsAnd(release), ampersandAsAnd(creator))
}

func sameWork(release, title string) bool {
	return TitleNamesSameWork(release, title) ||
		TitleNamesSameWork(ampersandAsAnd(release), ampersandAsAnd(title))
}

func ampersandAsAnd(s string) string { return strings.ReplaceAll(s, "&", " and ") }

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
