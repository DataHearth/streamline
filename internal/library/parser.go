package library

import (
	"regexp"
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
	groupRe = regexp.MustCompile(`-([A-Za-z0-9][A-Za-z0-9\s]*)$`)
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

	// Strip known media file extensions
	knownExts := map[string]bool{
		"mkv": true, "mp4": true, "avi": true, "wmv": true,
		"flv": true, "mov": true, "m4v": true, "ts": true,
		"webm": true, "mpg": true, "mpeg": true,
	}
	if idx := strings.LastIndex(filename, "."); idx > 0 {
		ext := strings.ToLower(filename[idx+1:])
		if knownExts[ext] {
			r.Extension = ext
			filename = filename[:idx]
		}
	}

	// Extract group. Scene names use `-GROUP`; some P2P groups append `.GROUP`
	// after the codec/quality. Accept a trailing dot-token only when it isn't a
	// known technical tag and the rest still looks like a release, so a plain
	// title's last word ("The.Office") is never taken as a group.
	if m := groupRe.FindStringSubmatch(filename); m != nil {
		r.Group = strings.TrimSpace(m[1])
		filename = filename[:len(filename)-len(m[0])]
	} else if m := dotGroupRe.FindStringSubmatch(filename); m != nil {
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

	// Extract year — find all matches, use the one that appears
	// before S##E## or before technical tokens
	if matches := yearRe.FindAllStringSubmatchIndex(filename, -1); len(matches) > 0 {
		// Use the first year that isn't part of a season/episode pattern
		for _, match := range matches {
			if y, err := strconv.ParseUint(
				filename[match[2]:match[3]],
				10,
				16,
			); err == nil {
				r.Year = uint16(y)
				break
			}
		}
	}

	// Title = everything before the first matched token
	r.Title = extractTitle(filename, r)

	return r
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

func extractTitle(filename string, r ParseResult) string {
	// Find the position of the first technical token
	cutPos := len(filename)

	markers := []string{}
	if r.Year > 0 {
		markers = append(markers, strconv.FormatUint(uint64(r.Year), 10))
	}
	if r.Season > 0 {
		markers = append(markers, seasonEpRe.FindString(filename))
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
	case "WEB-DL", "WEBDL":
		return "WEB-DL"
	case "WEBRIP":
		return "WEBRip"
	case "HDTV":
		return "HDTV"
	case "DVDRIP":
		return "DVDRip"
	case "WEB":
		return "WEB"
	default:
		return s
	}
}
