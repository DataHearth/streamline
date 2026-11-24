package library

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type ParseResult struct {
	// Specials is set when the season read is season 0, which tells the name
	// from one naming no season: Season is 0 for both.
	Specials   bool
	Title      string
	Year       uint16
	Season     uint16
	Episode    uint16
	SeasonPack bool
	// TitledSpecial is one special named by its title under a season token
	// ("Show.S05.Special.The.Title", "Show.S00.Christmas.Special"), which
	// leaves SeasonPack and AbsoluteNumber unset. namesSpecial holds the rule.
	TitledSpecial  bool
	AbsoluteNumber uint16
	AirDate        *time.Time
	Resolution     string
	Source         string
	Codec          string
	Group          string
	Extension      string
}

var (
	yearRe = regexp.MustCompile(`\b((?:19|20)\d{2})\b`)
	// The episode number takes a third digit: a season past 99 episodes is
	// rare but real (Kaamelott's first four Livres run to 100 shorts each),
	// and pinned at two digits "S01E100" would parse as episode 10 — silently,
	// so the file would match the wrong episode or none at all, and nothing
	// about the release name would look malformed. Greedy, so the third digit
	// wins when it is there. The season stays at two: every spelling of a
	// season past 99 is a different problem, and widening it makes "S1E1"
	// ambiguous with tokens this regex is not anchored against.
	seasonEpRe   = regexp.MustCompile(`(?i)S(\d{2})E(\d{2,3})`)
	resolutionRe = regexp.MustCompile(`(?i)\b(720p|1080p|2160p|4K)\b`)
	sourceRe     = regexp.MustCompile(
		`(?i)\b(BluRay|WEB-DL|WEBDL|WEBRip|HDTV|DVDRip|BDRip|BRRip|Remux|WEB)\b`,
	)
	codecRe = regexp.MustCompile(
		`(?i)\b(x264|x265|H\.264|H\.265|H264|H265|HEVC|AV1|MPEG2|VC-1|AVC)\b`,
	)
	// Group after the last dash. Allows internal whitespace (some P2P groups
	// like "MAN OF STYLE") but must start with an alphanumeric so a " - Title"
	// separator (whitespace right after the dash) is not taken as a group.
	// The whitespace allowance is what makes cleanGroup necessary: in a
	// space-separated name the last dash is usually inside "Blu-Ray", so the
	// match swallows every tag after it. A group may be followed by bracketed
	// afterthoughts the group itself appends ("-Tsundere-Raws (CR)"), captured
	// separately so they can be put back: they often hold the year.
	groupRe = regexp.MustCompile(
		`-([A-Za-z0-9][A-Za-z0-9\s]*?)((?:\s*[\(\[][^\)\]]*[\)\]])*)$`,
	)
	// Some P2P groups append their tag after the codec/quality with a dot
	// instead of a dash (e.g. "x265.RamirouHD", "1080p.PopHD"). dotGroupRe
	// grabs the trailing dot-token; isNonGroupTag rejects known technical tags
	// so ".MULTI"/".COMPLETE"/".1080p" aren't mistaken for a group. groupRe
	// (dash) wins when present.
	dotGroupRe = regexp.MustCompile(`\.([A-Za-z0-9]+)$`)

	seasonPackRe = regexp.MustCompile(`(?i)\bS(\d{2})\b(?:[^E]|$)`)
	// specialEpRe finds the number of one special written apart from its
	// season ("S00 - 05 [1080p]", "S00.E05"). What follows the dashed number
	// is a bracket, a word, a resolution or the end, so a range ("S00 - 01-10")
	// and a tag's digits ("S00 - 10bit") are not one.
	specialEpRe = regexp.MustCompile(
		`(?i)\b(?:S00|(?:season|saison)[. ]00?)` +
			`(?:[. ](?:-[. ])?E(?:p(?:isode[. ]?)?)?(\d{1,3})\b|\s+-\s+(\d{1,3})(?:v\d)?` +
			`(?:\s*$|\s*[\[(]|\s+(?:-\s+)?(?:\pL|\d{3,4}p)))`,
	)
	// specialWordRe captures the word "special" standing on its own.
	specialWordRe = regexp.MustCompile(
		`(?i)(?:^|[^\pL\pN])(special)(?:[^\pL\pN]|$)`,
	)
	// Non-scene releases spell the season out — French trackers in particular
	// ("Breaking Bad Saison 04 Complète Multi …"). Without this the name carries
	// no season token at all, which reads as "single episode of nothing" and
	// lets a season pack through every scope filter.
	longSeasonRe = regexp.MustCompile(`(?i)\b(?:season|saison)\s*(\d{1,2})\b`)
	dailyDateRe  = regexp.MustCompile(
		`\b((?:19|20)\d{2})[.\-_ ](\d{2})[.\-_ ](\d{2})\b`,
	)
	// seasonRangeRe matches a multi-season span like "S01-S05" / "S01.S02" /
	// "S01 S02" / "S01 & S02 & S03" (second season must carry an S so a
	// resolution like "S01.1080p" isn't mistaken for a range). The separator
	// class covers the join characters groups actually use — an ampersand chain
	// is a real shape ("Demon Slayer S01 & S02 & S03") and read as a single
	// season it lands in a one-season search offering every season.
	// completePackRe matches complete-series tags.
	seasonRangeRe = regexp.MustCompile(`(?i)S\d{1,2}[-.,+& ]+S\d{1,2}`)
	// specialsTokenRe finds the bare specials season.
	specialsTokenRe = regexp.MustCompile(`(?i)\bS00\b`)
	// specialsRangeRe matches a range opening at the specials ("S00-S12").
	specialsRangeRe = regexp.MustCompile(`(?i)\bS00?[. ]*-[. ]*S\d`)
	// seasonTokenRe finds every season token in a name, for bounding a chain
	// whose pairwise matches cannot overlap.
	seasonTokenRe = regexp.MustCompile(`(?i)\bS(\d{1,2})\b`)
	// "complet"/"complète" are the French spellings, and they qualify whatever
	// noun the name gives them — see IsWholeSeriesPack for why that matters.
	completePackRe = regexp.MustCompile(`(?i)\b(compl[eè]te?|int[eé]grale?)\b`)
	// absoluteRe matches an anime absolute number like " - 18 ". It is
	// deliberately conservative; false positives are acceptable since absolute
	// matching is a fallback only used for type=anime shows downstream.
	absoluteRe = regexp.MustCompile(`(?:^|\s|\]|-)\s?(\d{1,4})\s?(?:\[|\(|v\d|$)`)
)

func Parse(filename string) ParseResult {
	var r ParseResult

	if idx := strings.LastIndex(filename, "."); idx > 0 {
		ext := strings.ToLower(filename[idx+1:])
		if mediaExtensions[ext] {
			r.Extension = ext
			filename = filename[:idx]
		}
	}

	// Extract group. Scene names use `-GROUP`; some P2P groups append `.GROUP`
	// after the codec/quality. Accept a trailing dot-token only when it isn't a
	// known technical tag and the rest still looks like a release, so a plain
	// title's last word ("The.Office") is never taken as a group.
	if m := groupRe.FindStringSubmatch(
		filename,
	); m != nil &&
		cleanGroup(m[1]) != "" {
		r.Group = cleanGroup(m[1])
		r.Group, filename = expandHyphenatedGroup(
			r.Group, filename[:len(filename)-len(m[0])],
		)
		filename += m[2]
	} else if m := dotGroupRe.FindStringSubmatch(
		filename,
	); m != nil {
		rest := filename[:len(filename)-len(m[0])]
		if !isNonGroupTag(m[1]) && looksLikeRelease(rest) {
			r.Group = m[1]
			filename = rest
		}
	}

	// Extract season/episode
	if m := seasonEpRe.FindStringSubmatch(filename); m != nil {
		if s, err := strconv.ParseUint(m[1], 10, 16); err == nil {
			r.Season = uint16(s)
			r.Specials = s == 0
		}
		if e, err := strconv.ParseUint(m[2], 10, 16); err == nil {
			r.Episode = uint16(e)
		}
	}

	if r.Season == 0 && r.Episode == 0 {
		if m := specialEpRe.FindStringSubmatch(filename); m != nil {
			if e, err := strconv.ParseUint(m[1]+m[2], 10, 16); err == nil {
				r.Episode, r.Specials = uint16(e), true
			}
		}
	}

	// Daily date (takes precedence; dailies have no SxxExx).
	if m := dailyDateRe.FindStringSubmatch(filename); m != nil && r.Season == 0 {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
			r.AirDate = &t
		}
	}
	// Season pack: SXX (or a spelled-out season) present but no SXXEXX matched.
	if r.Episode == 0 {
		m := seasonPackRe.FindStringSubmatchIndex(filename)
		if m == nil {
			m = longSeasonRe.FindStringSubmatchIndex(filename)
		}
		if m != nil {
			if s, err := strconv.ParseUint(
				filename[m[2]:m[3]], 10, 16,
			); err == nil {
				r.Season = uint16(s)
				r.Specials = s == 0
				if namesSpecial(filename[m[3]:]) {
					r.TitledSpecial = true
				} else {
					// A dated S00 is the special aired that day.
					r.SeasonPack = s != 0 || r.AirDate == nil
				}
			}
		}
	}
	// Anime absolute: only when no season token and no daily date. The digits
	// of an S00 pack are its tags or its range, and a number beside a
	// special's title counts nothing the library numbers.
	if !r.SeasonPack && !r.TitledSpecial &&
		r.Season == 0 && r.Episode == 0 && r.AirDate == nil {
		if m := absoluteRe.FindStringSubmatch(filename); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 2000 {
				r.AbsoluteNumber = uint16(n)
			}
		}
	}

	// Extract resolution
	if m := resolutionRe.FindString(filename); m != "" {
		r.Resolution = normalizeResolution(m)
	}

	// Extract source
	if m := sourceRe.FindString(filename); m != "" {
		r.Source = normalizeSource(m)
	}

	// Extract codec
	if m := codecRe.FindString(filename); m != "" {
		r.Codec = normalizeCodec(m)
	}

	// Extract year — take the *last* year token, and never one that opens the
	// name. A numeric title ("1917 - 2019", "2001 A Space Odyssey - 1968") or a
	// year-like suffix ("Blade Runner 2049 - 2017", "Fantasia 2000 (2000)")
	// otherwise loses to the title's own digits, and since the title is cut at
	// the year the release is left with an empty title and no candidates at all.
	// A name whose only year leads it ("1917.mkv") keeps the digits as its title
	// and reports no year.
	yearIdx := -1
	for _, match := range yearRe.FindAllStringSubmatchIndex(filename, -1) {
		if match[2] == 0 {
			continue
		}
		if y, err := strconv.ParseUint(
			filename[match[2]:match[3]],
			10,
			16,
		); err == nil {
			r.Year = uint16(y)
			yearIdx = match[2]
		}
	}

	// Title = everything before the first matched token
	r.Title = extractTitle(filename, r, yearIdx)

	return r
}

var mediaExtensions = map[string]bool{
	"mkv": true, "mp4": true, "avi": true, "wmv": true,
	"flv": true, "mov": true, "m4v": true, "ts": true,
	"webm": true, "mpg": true, "mpeg": true,
}

// nonGroupTags are trailing dot-tokens that are quality/language/edition
// descriptors, not release groups. Kept lowercase for case-insensitive lookup.
var nonGroupTags = map[string]bool{
	// language / audio
	"multi": true, "vostfr": true, "vff": true, "vfq": true, "vfi": true,
	"vf": true, "vo": true, "french": true, "truefrench": true,
	"subfrench": true, "ac3": true, "eac3": true, "dts": true, "ddp": true,
	"ddp5": true, "dd5": true, "dd": true, "aac": true, "flac": true,
	"truehd": true, "atmos": true, "mp3": true,
	// video descriptors / editions
	"10bit": true, "8bit": true, "hdr": true, "hdr10": true, "dv": true,
	"sdr": true, "hlg": true, "imax": true, "remux": true, "proper": true,
	"repack": true, "extended": true, "remastered": true, "uncut": true,
	"complete": true, "integral": true, "integrale": true, "collection": true,
	"series": true, "limited": true, "internal": true, "edition": true,
	"special": true, "specials": true, "features": true,
	// tails of hyphenated source tags ("Blu-Ray", "WEB-DL"), which groupRe
	// captures when the name is space-separated.
	"ray": true, "dl": true,
}

// isNonGroupTag reports whether a trailing dot-token is a known technical/
// descriptor tag (or bare number) rather than a release group.
func isNonGroupTag(tok string) bool {
	if _, err := strconv.Atoi(tok); err == nil {
		return true
	}
	if nonGroupTags[strings.ToLower(tok)] {
		return true
	}
	return resolutionRe.MatchString(tok) ||
		sourceRe.MatchString(tok) ||
		codecRe.MatchString(tok)
}

// cleanGroup validates a dash-captured group candidate, returning "" when it is
// really trailing metadata. A space-separated release name puts the last dash
// inside "Blu-Ray", so groupRe's whitespace allowance captures the whole tail
// ("Ray HEVC x265 10Bit DDP5 1 Subs KINGDOM"). A container extension is dropped
// as noise ("Slay3R mkv"); any other technical word means the candidate is not
// a group at all.
func cleanGroup(s string) string {
	words := strings.Fields(s)
	if n := len(words); n > 0 && mediaExtensions[strings.ToLower(words[n-1])] {
		words = words[:n-1]
	}
	if slices.ContainsFunc(words, isNonGroupTag) {
		return ""
	}
	return strings.Join(words, " ")
}

// expandHyphenatedGroup walks left from a dash-captured group over further
// dash-joined words, returning the widened group and the name it was cut from.
// groupRe stops at the *last* dash, which splits a group whose own name carries
// one: "…x264-tsundere-raws" is the group "tsundere-raws", not "raws". A word
// holding a dot or a space is a separate token rather than part of the name
// (".WEB-DL.x264-GRP", "Blu-Ray x264-GRP"), and a technical tag ends the walk.
//
// The walk only moves a cut index and builds the group once at the end: an
// indexer title is attacker-authored, and prepending word by word copied the
// growing group on every dash — quadratic in a long dash chain.
func expandHyphenatedGroup(group, rest string) (string, string) {
	cut := len(rest)
	for {
		i := strings.LastIndex(rest[:cut], "-")
		if i < 0 {
			break
		}
		w := rest[i+1 : cut]
		if strings.ContainsAny(w, ". ") || isNonGroupTag(w) {
			break
		}
		cut = i
	}
	if cut == len(rest) {
		return group, rest
	}
	return rest[cut+1:] + "-" + group, rest[:cut]
}

// looksLikeRelease reports whether s carries at least one release token,
// guarding the dot-group heuristic from grabbing a plain title's last word.
func looksLikeRelease(s string) bool {
	return resolutionRe.MatchString(s) || sourceRe.MatchString(s) ||
		codecRe.MatchString(s) || seasonEpRe.MatchString(s) ||
		seasonPackRe.MatchString(s) || yearRe.MatchString(s)
}

// IsWholeSeriesPack reports whether a release name denotes a complete-series or
// multi-season pack (e.g. "COMPLETE", "INTEGRALE", "S01-S05") that spans more
// than one season. A season-scoped search filters these out since grabbing one
// imports every season it contains.
//
// A "complete" tag alone is not enough: it qualifies whatever the name names,
// and half the season packs in the wild say it about the season they carry
// ("Breaking Bad S05 Complete Multi …", "Breaking Bad Saison 04 Complète …").
// Read as whole-series, those would be dropped from a search for exactly the
// season they hold. So the tag only spans the series when no single season is
// named — and a season *range* wins over both, since "COMPLETE S01-S05" spans
// five. The specials season after the tag is added to the series rather than
// qualified by it ("INTEGRALE + S00").
func IsWholeSeriesPack(name string) bool {
	if seasonRangeRe.MatchString(name) || namesSeriesAndSpecials(name) {
		return true
	}
	return completePackRe.MatchString(name) && !namesOneSeason(name)
}

// namesSeriesAndSpecials reports a complete tag followed by the specials
// season ("INTEGRALE + S00"): the tag covers the series and S00 is added to
// it, where a tag after the season qualifies that season ("S00 COMPLETE").
// A numbered season before the tag is the one it qualifies ("S05 COMPLETE S00").
func namesSeriesAndSpecials(name string) bool {
	tag := completePackRe.FindStringIndex(name)
	s00 := specialsTokenRe.FindStringIndex(name)
	return tag != nil && s00 != nil && tag[0] < s00[0] &&
		!namesOneSeason(name[:tag[0]])
}

// namesOneSeason reports whether the name scopes itself to a single season, in
// any of the three spellings the parser knows.
func namesOneSeason(name string) bool {
	return seasonEpRe.MatchString(name) ||
		seasonPackRe.MatchString(name) ||
		longSeasonRe.MatchString(name)
}

// seasonRangeBounds returns the lowest and highest season a multi-season name
// covers, for the spans IsWholeSeriesPack only detects the existence of. It
// reads *every* season token rather than the pair seasonRangeRe matched:
// pairwise matches cannot overlap, so a chain ("S01 & S02 & S03") matches
// "S01 & S02" and then resumes past it, leaving the tail unread and the pack
// sized two seasons short.
//
// An S00 token is reported apart, as specials, since lo is 0 until a numbered
// season is seen. A dash from it opens a range that runs on from season 1
// ("S00-S12" is the specials plus seasons 1 to 12); listed any other way it
// names the specials beside the rest ("S05+S00" is season 5 and the specials).
func seasonRangeBounds(name string) (lo, hi uint16, specials bool) {
	for _, m := range seasonTokenRe.FindAllStringSubmatch(name, -1) {
		n, err := strconv.ParseUint(m[1], 10, 16)
		if err != nil {
			continue
		}
		s := uint16(n)
		if s == 0 {
			specials = true
			continue
		}
		if lo == 0 || s < lo {
			lo = s
		}
		if s > hi {
			hi = s
		}
	}
	if lo > 0 && specialsRangeRe.MatchString(name) {
		lo = 1
	}
	return lo, hi, specials
}

// SeasonSpan is which seasons a release covers. It exists to size a pack: the
// release names its scope but never how many files it holds, and no indexer
// reports a torrent's file count either, so the episode total has to be looked
// up per season against the library.
type SeasonSpan struct {
	// Complete spans every numbered season the show has, from a
	// "COMPLETE"/"INTEGRALE" tag. From and To are meaningless when it is set.
	Complete bool
	// From and To are inclusive and equal for a single-season pack, and name
	// numbered seasons only. Both zero with Specials unset means the release
	// covers no whole season — a single episode, or a name nothing could be
	// read out of.
	From, To uint16
	// Specials is set when the name claims season 0 ("S00", "S00-S05"),
	// alone or beside the numbered range. It is a field of its own because
	// From 0 already means "no season".
	Specials bool
}

// ParseSeasonSpan reads a release name's season scope. A season range wins over
// a complete tag followed by S00, which wins over a plain SXX, which wins over a
// bare complete/integral tag, which wins over nothing: "S01-S03" is three
// seasons even though the first token alone would parse as season 1, and "S05
// Complete" is season 5 rather than the series (same reasoning as
// IsWholeSeriesPack).
func ParseSeasonSpan(name string) SeasonSpan {
	if seasonRangeRe.MatchString(name) {
		if from, to, specials := seasonRangeBounds(name); from > 0 {
			return SeasonSpan{From: from, To: to, Specials: specials}
		}
	}
	if namesSeriesAndSpecials(name) {
		return SeasonSpan{Complete: true, Specials: true}
	}
	switch p := Parse(name); {
	case p.TitledSpecial:
		// One special, whatever tag follows its title.
		return SeasonSpan{}
	case p.SeasonPack && p.Season == 0:
		return SeasonSpan{Specials: true}
	case p.SeasonPack:
		return SeasonSpan{From: p.Season, To: p.Season}
	}
	if completePackRe.MatchString(name) {
		return SeasonSpan{Complete: true}
	}
	return SeasonSpan{}
}

// EpisodeCount totals the episodes the span covers, given the show's per-season
// totals. Zero means "the library cannot say" — an untracked season, or a name
// that named no season at all — and callers must read it as unknown rather than
// as an empty pack.
//
// A complete pack totals the numbered seasons and leaves the specials out
// unless it names them: a bare tag rarely says whether they ship. The episode
// search reads one the same way (packCoversSeason in internal/indexer), so the
// size bound and the hidden-packs hint agree on what an integral holds.
func (s SeasonSpan) EpisodeCount(perSeason map[uint16]int) int {
	total := 0
	for season, n := range perSeason {
		switch {
		case season == 0 && s.Specials,
			season != 0 && s.Complete,
			season != 0 && s.From <= season && season <= s.To:
			total += n
		}
	}
	return total
}

// specialJoiners are the words that add a special to a pack ("S01 with
// Special"), and specialExtras the ones that make "special" a tag of the pack
// ("S01 Special Edition", "S01 Special Features", "S01 Special Included").
var (
	specialJoiners = map[string]bool{
		"and": true, "with": true, "w": true, "inc": true, "incl": true,
		"including": true, "included": true, "plus": true, "et": true,
		"avec": true,
	}
	specialExtras = map[string]bool{
		"edition": true, "ed": true, "features": true, "extras": true,
		"bonus": true, "included": true,
	}
	// episodeRangeRe is a run of episodes written apart from its season
	// ("S01.E01-E10").
	episodeRangeRe = regexp.MustCompile(`(?i)\bE\d{2,3}\s*-\s*E?\d{2,3}\b`)
)

// namesSpecial reports whether what follows a season token names one special
// by its title rather than a pack. Only the segment up to the first technical
// tag is read: it must hold the word "special" and another title word. A pack
// that ships its specials says so with a joiner or a bracket before the word
// ("S01 + Special", "S02 [BD + Special]"), another season token, an episode
// range or a complete tag, and a word after it can make it a tag ("Special
// Edition").
func namesSpecial(rest string) bool {
	seg := rest[:techIndex(rest)]
	m := specialWordRe.FindStringSubmatchIndex(seg)
	if m == nil || seasonTokenRe.MatchString(seg) ||
		completePackRe.MatchString(seg) || episodeRangeRe.MatchString(seg) ||
		strings.ContainsAny(seg[:m[2]], "+&([{") {
		return false
	}
	before := splitWords(strings.ToLower(seg[:m[2]]))
	after := splitWords(strings.ToLower(seg[m[3]:]))
	joiner := func(w string) bool { return specialJoiners[w] }
	extra := func(w string) bool { return specialExtras[w] }
	if slices.ContainsFunc(before, joiner) || slices.ContainsFunc(after, extra) {
		return false
	}
	return slices.ContainsFunc(append(before, after...), func(w string) bool {
		return !isNonGroupTag(w)
	})
}

// splitWords splits s at everything but letters, digits and the marks that
// belong to a letter.
func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	})
}

// techIndex is where the first resolution, source or codec tag starts in s,
// len(s) when it holds none.
func techIndex(s string) int {
	cut := len(s)
	for _, re := range []*regexp.Regexp{resolutionRe, sourceRe, codecRe} {
		if m := re.FindStringIndex(s); m != nil && m[0] < cut {
			cut = m[0]
		}
	}
	return cut
}

// extractTitle returns the name up to its first technical token. yearIdx is the
// byte offset of the year Parse settled on, or -1 for none — searching for the
// year's *text* instead would find the title's own digits first ("Fantasia 2000
// (2000)" would cut at the title).
func extractTitle(filename string, r ParseResult, yearIdx int) string {
	// Find the position of the first technical token
	cutPos := len(filename)
	if yearIdx >= 0 && yearIdx < cutPos {
		cutPos = yearIdx
	}

	// A pack has no SxxExx to cut at, so fall back to the bare season token.
	// Without it the cut lands on the resolution instead and the season plus
	// every language/edition tag between them is glued into the title — "Good
	// Omens S03 MULTi VF2", which TitleMatches can never equal the library's
	// "Good Omens". The token decides, not the season's value: S00 names the
	// specials, and left in the title ("Show S00E01") it equals no show.
	marker := seasonEpRe.FindString(filename)
	if marker == "" {
		marker = seasonPackRe.FindString(filename)
	}
	if marker == "" {
		marker = longSeasonRe.FindString(filename)
	}
	markers := []string{marker}
	if r.Resolution != "" {
		// The stored value is normalised, so it need not appear in the name as
		// written ("1080P", "4K"). Re-find the original text, like Source below.
		if m := resolutionRe.FindString(filename); m != "" {
			markers = append(markers, m)
		}
	}
	if r.Source != "" {
		// Find original source string in filename (before normalization)
		if m := sourceRe.FindStringIndex(filename); m != nil {
			markers = append(markers, filename[m[0]:m[1]])
		}
	}

	for _, marker := range markers {
		if marker == "" {
			continue
		}
		idx := strings.Index(filename, marker)
		if idx >= 0 && idx < cutPos {
			cutPos = idx
		}
	}

	title := filename[:cutPos]
	title = strings.NewReplacer(".", " ", "_", " ").Replace(title)
	// Cut lands just before the year/resolution token, which is usually
	// preceded by an opening bracket (e.g. "Title (2016)") — trim the dangling
	// delimiter so it doesn't render as "Title ( (2016)".
	return strings.Trim(title, " ([{-")
}

// normalizeCodec canonicalises codec captures so x264/H.264/AVC all collapse
// to one tag, and HEVC variants likewise — keeps filters/pills coherent.
func normalizeCodec(s string) string {
	upper := strings.ToUpper(s)
	upper = strings.ReplaceAll(upper, ".", "")
	switch upper {
	case "X264", "H264", "AVC":
		return "x264"
	case "X265", "H265", "HEVC":
		return "HEVC"
	case "AV1":
		return "AV1"
	case "MPEG2":
		return "MPEG2"
	case "VC-1":
		return "VC-1"
	default:
		return s
	}
}

// normalizeResolution canonicalises a matched resolution token to the bucket
// name every consumer keys on. Groups spell it in any casing they like, and
// `quality.resolutionRank` is a plain switch: an uppercase "1080P" would rank
// 0, which `Evaluate` reports as "resolution outside profile band" — a whole
// release rejected for the case of one letter.
func normalizeResolution(s string) string {
	lower := strings.ToLower(s)
	if lower == "4k" {
		return "2160p"
	}
	return lower
}

func normalizeSource(s string) string {
	upper := strings.ToUpper(s)
	switch upper {
	case "BLURAY", "BDRIP", "BRRIP":
		return "BluRay"
	case "REMUX":
		return "Remux"
	// Bare WEB is shorthand for WEB-DL, not a third source: a stream ripped
	// untranscoded is what "…1080p.WEB.x264-GRP" names, and the *arr parsers
	// fold it the same way. Kept apart, a profile written against one silently
	// scored nothing from groups that spell it the other. WEBRip stays its own
	// thing — that one really is a re-encode.
	case "WEB-DL", "WEBDL", "WEB":
		return "WEB-DL"
	case "WEBRIP":
		return "WEBRip"
	case "HDTV":
		return "HDTV"
	case "DVDRIP":
		return "DVDRip"
	default:
		return s
	}
}

// embeddedIDRe matches a provider id Streamline's own naming template writes
// into a path — "Fantasia 2000 (2000) {tmdb-49948}". Both brace and bracket
// forms are accepted since other tools in the *arr ecosystem use brackets.
var embeddedIDRe = regexp.MustCompile(`(?i)[{\[](tmdb|tvdb)-(\d{1,9})[}\]]`)

// EmbeddedIDs are provider ids lifted from a path. A zero field means the path
// carried no id for that provider.
type EmbeddedIDs struct {
	TMDB uint32
	TVDB uint32
}

// ParseEmbeddedIDs lifts {tmdb-N} / {tvdb-N} out of a path. Such an id is
// authoritative: the path was rendered *from* it, so re-deriving a title from
// the filename and searching a provider for it can only lose information.
func ParseEmbeddedIDs(path string) EmbeddedIDs {
	var ids EmbeddedIDs
	for _, m := range embeddedIDRe.FindAllStringSubmatch(path, -1) {
		n, err := strconv.ParseUint(m[2], 10, 32)
		if err != nil || n == 0 {
			continue
		}
		switch strings.ToLower(m[1]) {
		case "tmdb":
			ids.TMDB = uint32(n)
		case "tvdb":
			ids.TVDB = uint32(n)
		}
	}
	return ids
}
