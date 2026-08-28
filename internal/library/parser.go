package library

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type ParseResult struct {
	Title          string
	Year           uint16
	Season         uint16
	Episode        uint16
	SeasonPack     bool
	AbsoluteNumber uint16
	AirDate        *time.Time
	Resolution     string
	Source         string
	Codec          string
	Group          string
	Extension      string
}

var (
	yearRe       = regexp.MustCompile(`\b((?:19|20)\d{2})\b`)
	seasonEpRe   = regexp.MustCompile(`(?i)S(\d{2})E(\d{2})`)
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
	dailyDateRe  = regexp.MustCompile(
		`\b((?:19|20)\d{2})[.\-_ ](\d{2})[.\-_ ](\d{2})\b`,
	)
	// seasonRangeRe matches a multi-season span like "S01-S05" / "S01.S02" /
	// "S01 S02" (second season must carry an S so a resolution like "S01.1080p"
	// isn't mistaken for a range). completePackRe matches complete-series tags.
	seasonRangeRe  = regexp.MustCompile(`(?i)S\d{1,2}[-. ]+S\d{1,2}`)
	completePackRe = regexp.MustCompile(`(?i)\b(complete|int[eé]grale?)\b`)
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
		}
		if e, err := strconv.ParseUint(m[2], 10, 16); err == nil {
			r.Episode = uint16(e)
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
	// Season pack: SXX present but no SXXEXX matched.
	if r.Episode == 0 {
		if m := seasonPackRe.FindStringSubmatch(filename); m != nil {
			if s, err := strconv.Atoi(m[1]); err == nil {
				r.Season = uint16(s)
				r.SeasonPack = true
			}
		}
	}
	// Anime absolute: only when no SxxExx and no daily date.
	if r.Season == 0 && r.Episode == 0 && r.AirDate == nil {
		if m := absoluteRe.FindStringSubmatch(filename); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 2000 {
				r.AbsoluteNumber = uint16(n)
			}
		}
	}

	// Extract resolution
	if m := resolutionRe.FindString(filename); m != "" {
		r.Resolution = m
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
	"series": true, "limited": true, "internal": true,
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
func expandHyphenatedGroup(group, rest string) (string, string) {
	for {
		i := strings.LastIndex(rest, "-")
		if i < 0 {
			return group, rest
		}
		w := rest[i+1:]
		if strings.ContainsAny(w, ". ") || isNonGroupTag(w) {
			return group, rest
		}
		group = w + "-" + group
		rest = rest[:i]
	}
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
func IsWholeSeriesPack(name string) bool {
	return seasonRangeRe.MatchString(name) || completePackRe.MatchString(name)
}

// seasonRangeBoundsRe captures the two season numbers IsWholeSeriesPack only
// detects, so a span can name the seasons rather than just report that there
// are several.
var seasonRangeBoundsRe = regexp.MustCompile(`(?i)S(\d{1,2})[-. ]+S(\d{1,2})`)

// SeasonSpan is which seasons a release covers. It exists to size a pack: the
// release names its scope but never how many files it holds, and no indexer
// reports a torrent's file count either, so the episode total has to be looked
// up per season against the library.
type SeasonSpan struct {
	// Complete spans every season the show has, from a "COMPLETE"/"INTEGRALE"
	// tag. From and To are meaningless when it is set.
	Complete bool
	// From and To are inclusive and equal for a single-season pack. Both zero
	// means the release covers no whole season — a single episode, or a name
	// nothing could be read out of.
	From, To uint16
}

// ParseSeasonSpan reads a release name's season scope. A season range wins over
// a plain SXX, which wins over nothing: "S01-S03" is three seasons even though
// the first token alone would parse as season 1.
func ParseSeasonSpan(name string) SeasonSpan {
	if completePackRe.MatchString(name) {
		return SeasonSpan{Complete: true}
	}
	if m := seasonRangeBoundsRe.FindStringSubmatch(name); m != nil {
		from, err1 := strconv.ParseUint(m[1], 10, 16)
		to, err2 := strconv.ParseUint(m[2], 10, 16)
		if err1 == nil && err2 == nil {
			if from > to {
				from, to = to, from
			}
			return SeasonSpan{From: uint16(from), To: uint16(to)}
		}
	}
	if p := Parse(name); p.SeasonPack {
		return SeasonSpan{From: p.Season, To: p.Season}
	}
	return SeasonSpan{}
}

// EpisodeCount totals the episodes the span covers, given the show's per-season
// totals. Zero means "the library cannot say" — an untracked season, or a name
// that named no season at all — and callers must read it as unknown rather than
// as an empty pack.
func (s SeasonSpan) EpisodeCount(perSeason map[uint16]int) int {
	if s.Complete {
		total := 0
		for _, n := range perSeason {
			total += n
		}
		return total
	}
	if s.From == 0 && s.To == 0 {
		return 0
	}
	total := 0
	for n := s.From; n <= s.To; n++ {
		total += perSeason[n]
	}
	return total
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

	markers := []string{}
	if r.Season > 0 {
		// A pack has no SxxExx to cut at, so fall back to the bare season
		// token. Without it the cut lands on the resolution instead and the
		// season plus every language/edition tag between them is glued into
		// the title — "Good Omens S03 MULTi VF2", which TitleMatches can never
		// equal the library's "Good Omens".
		m := seasonEpRe.FindString(filename)
		if m == "" {
			m = seasonPackRe.FindString(filename)
		}
		markers = append(markers, m)
	}
	if r.Resolution != "" {
		markers = append(markers, r.Resolution)
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
