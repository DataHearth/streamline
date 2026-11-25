package library

import (
	"math"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

// ConvertsWAV reports whether a WAV file can be imported, as the FLAC the
// importer converts it to. Read per call: both switches change at runtime.
func ConvertsWAV(p ffmpeg.Prober) bool {
	cfg := config.Get()
	return p != nil && cfg.FFmpeg.Enabled && cfg.Library.ConvertWAV &&
		p.Available() && p.FFmpegPath() != ""
}

// IsReleaseAudio is audiotags.IsAudioFile for the files of a download or an
// upload, which may also be WAV when wav says it converts. The library itself
// never holds one, so audiotags does not know the extension.
func IsReleaseAudio(p string, wav bool) bool {
	if audiotags.IsAudioFile(p) {
		return true
	}
	base := filepath.Base(p)
	return wav && !strings.HasPrefix(base, ".") &&
		strings.EqualFold(filepath.Ext(base), ".wav")
}

var discFolderRe = regexp.MustCompile(`(?i)^(?:cd|dis[ck])\s*(\d+)$`)

// CueImageReason is the one wording of a disc image refusal, from the release
// name, the torrent's file list or the importer.
const CueImageReason = "disc image with a CUE sheet; splitting it into tracks is not supported"

// cueImageAudio is wider than audiotags.AudioExtensions: an image is as often
// APE, WavPack or WAV, formats the library never imports.
var cueImageAudio = map[string]struct{}{
	".flac": {}, ".ape": {}, ".wv": {}, ".wav": {}, ".tta": {}, ".tak": {},
	".aiff": {}, ".m4a": {}, ".mp3": {}, ".ogg": {}, ".opus": {},
}

// CueImage reports an album of tracks ripped to images its CUE sheets split:
// the folders holding a sheet hold at most half as many audio files as the
// album has tracks. paths are every file of the release, slash or OS
// separated. Counting against the album catches one image per disc and one
// per vinyl side alike ("Side A.flac" … "Side D.flac" beside one sheet), which
// a one-file-per-folder rule let through to fail as unmatched; a tracks+cue
// release missing more than half its tracks reads as an image too. tracks 0
// means the album's tracks are not fetched yet, and a manual grab can come
// before them: only a folder whose one audio file sits beside a sheet is then
// known to be an image.
func CueImage(paths []string, tracks int) bool {
	audio := map[string]int{}
	sheets := map[string]bool{}
	for _, p := range paths {
		p = filepath.ToSlash(p)
		ext := strings.ToLower(path.Ext(p))
		if ext == ".cue" {
			sheets[path.Dir(p)] = true
		} else if _, ok := cueImageAudio[ext]; ok {
			audio[path.Dir(p)]++
		}
	}
	images, single := 0, false
	for dir, n := range audio {
		if sheets[dir] {
			images += n
			single = single || n == 1
		}
	}
	if tracks == 0 {
		return single
	}
	return images > 0 && 2*images <= tracks
}

// DiscFolderNumber reads the disc number off a folder name such as "CD1",
// "Disc 2" or "Disk3".
func DiscFolderNumber(name string) (uint8, bool) {
	m := discFolderRe.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseUint(m[1], 10, 8)
	if err != nil || n == 0 || n > math.MaxUint8 {
		return 0, false
	}
	return uint8(n), true
}

// MatchTrack prefers the tagged (disc, track) pair and falls back to the
// title. The title may come from the file's name when it carries no tag, so
// punctuation does not count: a vinyl rip named "A1 - Goblin..flac" reads as
// "Goblin." and is the track "Goblin". The exact comparison stays for a title
// that normalizes to nothing, which TitleMatchesStrict never matches.
func MatchTrack(tracks []*ent.Track, info audiotags.Info) *ent.Track {
	if t := trackAt(tracks, info); t != nil {
		return t
	}
	title := strings.TrimSpace(info.Title)
	if title == "" {
		return nil
	}
	if i := slices.IndexFunc(tracks, func(t *ent.Track) bool {
		return strings.EqualFold(strings.TrimSpace(t.Title), title) ||
			trackTitleIs(t, title)
	}); i >= 0 {
		return tracks[i]
	}
	return nil
}

// trackAt is the track at the file's (disc, track) pair, disc 1 when it
// states none.
func trackAt(tracks []*ent.Track, info audiotags.Info) *ent.Track {
	if info.Track == 0 {
		return nil
	}
	disc := info.Disc
	if disc == 0 {
		disc = 1
	}
	if i := slices.IndexFunc(tracks, func(t *ent.Track) bool {
		return t.Disc == disc && t.Position == info.Track
	}); i >= 0 {
		return tracks[i]
	}
	return nil
}

// TrackMatcher matches the files of one release to an album's tracks. The
// files decide together whether their numbers name the album's tracks: by
// number is right for an ordinary rip and wrong for an edition numbered
// otherwise. Avatar's "Feathers & Flesh (In His Own Words)" puts a spoken
// interlude before every song, so its 01 is "An Ancient Old Tale" where the
// album's 01 is "Regret", and by number every song would land in another
// song's slot with the interludes between them.
type TrackMatcher struct {
	tracks   []*ent.Track
	byNumber bool
	// label is the bracketed label every file's title ends with
	// (ReleaseTitleLabel), "" when they share none.
	label string
	// near is the track each title no track carries is another spelling
	// of, keyed by the title normalized, its label set aside (nearTitles).
	near map[string]*ent.Track
}

// NewTrackMatcher trusts the numbers unless the titles show the release
// ordered otherwise: more of the files whose number reaches a track state
// another track's title than state that one's. A title naming no track of
// the album (a translation, a typo) says nothing about the order. Distrusted,
// a file matches by title alone and an interlude matches nothing.
func NewTrackMatcher(tracks []*ent.Track, infos []audiotags.Info) TrackMatcher {
	titles := make([]string, 0, len(infos))
	for _, info := range infos {
		titles = append(titles, info.Title)
	}
	m := TrackMatcher{tracks: tracks, label: ReleaseTitleLabel(titles)}
	agree, elsewhere := 0, 0
	for _, info := range infos {
		t := trackAt(tracks, info)
		if t == nil {
			continue
		}
		switch {
		case TrackTitleAgrees(t, info.Title) ||
			TrackTitleAgrees(t, m.bare(info.Title)):
			agree++
		case matchTitle(tracks, info.Disc, info.Title) != nil ||
			matchTitle(tracks, info.Disc, m.bare(info.Title)) != nil:
			elsewhere++
		}
	}
	m.byNumber = elsewhere < 2 || elsewhere <= agree
	m.near = nearTitles(tracks, infos, m.Match, m.bare)
	return m
}

// NewTrackMatcherByName is NewTrackMatcher for files known by their paths
// only, as TrackInfoFromName reads them.
func NewTrackMatcherByName(tracks []*ent.Track, paths []string) TrackMatcher {
	infos := make([]audiotags.Info, 0, len(paths))
	for _, p := range paths {
		infos = append(infos, TrackInfoFromName(p))
	}
	return NewTrackMatcher(tracks, infos)
}

// ByNumber reports whether the files' numbers were trusted.
func (m TrackMatcher) ByNumber() bool { return m.byNumber }

// Match is MatchTrack when the numbers are trusted, else the track whose
// title the file states, on the file's disc first.
func (m TrackMatcher) Match(info audiotags.Info) *ent.Track {
	if m.byNumber {
		return m.fallBack(MatchTrack(m.tracks, info), info)
	}
	return m.fallBack(matchTitle(m.tracks, info.Disc, info.Title), info)
}

// Suggest is Match unless the file states the title of another track: the guess
// offered to someone choosing a held file's track.
func (m TrackMatcher) Suggest(info audiotags.Info) *ent.Track {
	t := m.Match(info)
	if t != nil && (TrackTitleAgrees(t, info.Title) ||
		TrackTitleAgrees(t, m.bare(info.Title))) {
		return t
	}
	if named := matchTitle(m.tracks, info.Disc, info.Title); named != nil {
		return named
	}
	if named := matchTitle(m.tracks, info.Disc, m.bare(info.Title)); named != nil {
		return named
	}
	return t
}

// MatchByName is MatchTrackByName when the numbers are trusted, else the
// track whose title the file name states, on its disc first.
func (m TrackMatcher) MatchByName(p string) *ent.Track {
	info := TrackInfoFromName(p)
	if m.byNumber {
		return m.fallBack(MatchTrackByName(m.tracks, p), info)
	}
	return m.fallBack(matchTitle(m.tracks, info.Disc, info.Title), info)
}

// fallBack is t, else the track the file's title names once the release's
// label is set aside, else the one nearTitles paired it with.
func (m TrackMatcher) fallBack(t *ent.Track, info audiotags.Info) *ent.Track {
	if t != nil {
		return t
	}
	bare := m.bare(info.Title)
	if bare != info.Title {
		if t := matchTitle(m.tracks, info.Disc, bare); t != nil {
			return t
		}
	}
	return m.near[normalizeTitle(bare)]
}

// bare is title without the release's label.
func (m TrackMatcher) bare(title string) string {
	return StripTitleLabel(title, m.label)
}

// titleLabel reads the bracketed label a title ends with: "(Live)",
// "[Remastered 2011]".
var titleLabel = regexp.MustCompile(`\s*[\[(]\s*([^\[\]()]+?)\s*[\])]\s*$`)

// ReleaseTitleLabel is the bracketed label every titled file of a release
// ends with, folded to lower case, or "" when they share none. Carried by
// every file, a "(Live)" describes the release, not one recording: a live
// album's files say it where its track titles do not. One file in a release
// carrying it is a bonus version, and it then stays part of that title.
func ReleaseTitleLabel(titles []string) string {
	label, titled := "", 0
	for _, title := range titles {
		if strings.TrimSpace(title) == "" {
			continue
		}
		m := titleLabel.FindStringSubmatch(title)
		if m == nil {
			return ""
		}
		l := strings.Join(strings.Fields(strings.ToLower(m[1])), " ")
		if titled > 0 && l != label {
			return ""
		}
		label = l
		titled++
	}
	if titled < 2 {
		return ""
	}
	return label
}

// StripTitleLabel is title without label, the ReleaseTitleLabel of its
// release, when it ends with it; else title as it is.
func StripTitleLabel(title, label string) string {
	if label == "" {
		return title
	}
	m := titleLabel.FindStringSubmatchIndex(title)
	if m == nil ||
		strings.Join(
			strings.Fields(strings.ToLower(title[m[2]:m[3]])),
			" ",
		) != label {
		return title
	}
	if bare := strings.TrimSpace(title[:m[0]]); bare != "" {
		return bare
	}
	return title
}

// nearTitles pairs the files whose title names no track with the track that
// title is another spelling of (TitleNear): "Un Élan De Coeur" is the
// album's "Un élan du cœur". A file is paired only with a track no file
// matches outright, only when one such track is nearest, and only when no
// other file is nearest to it too, so a bonus song never takes the slot of a
// track the release lacks unless its title is all but that track's.
func nearTitles(
	tracks []*ent.Track,
	infos []audiotags.Info,
	match func(audiotags.Info) *ent.Track,
	bare func(string) string,
) map[string]*ent.Track {
	claimed := make(map[uint32]bool, len(tracks))
	loose := map[string]string{}
	for _, info := range infos {
		if t := match(info); t != nil {
			claimed[t.ID] = true
			continue
		}
		title := bare(info.Title)
		if n := normalizeTitle(title); n != "" {
			if _, seen := loose[n]; !seen {
				loose[n] = title
			}
		}
	}
	pick := map[string]*ent.Track{}
	takers := map[uint32]int{}
	for n, title := range loose {
		var best *ent.Track
		bestDist, tied := math.MaxInt, false
		for _, t := range tracks {
			if claimed[t.ID] {
				continue
			}
			for _, own := range TrackTitles(t) {
				d, ok := titleNear(own, title)
				if !ok || d > bestDist || (d == bestDist && best == t) {
					continue
				}
				tied = d == bestDist
				best, bestDist = t, d
			}
		}
		if best != nil && !tied {
			pick[n] = best
			takers[best.ID]++
		}
	}
	for n, t := range pick {
		if takers[t.ID] > 1 {
			delete(pick, n)
		}
	}
	return pick
}

// TitleNear reports whether two track titles are spellings of one title: a
// store's content label set aside ("Dancefloor Bastards [Explicit]"), the
// same words in another order ("Heretik System Remix" for "Remix Heretik
// System"), or a misspelling ("Cardiotronic" for "Cardiotonic").
func TitleNear(a, b string) bool {
	_, ok := titleNear(a, b)
	return ok
}

// titleNear is TitleNear with how far apart the two are: 0 for a store label
// or another word order, else the edit distance between the normalized
// titles, ok only when it is small enough to be a misspelling: as many
// words on both sides (a "(Live)" or "(Remix)" more is another recording,
// however long the title), at most one edit in six characters of the longer,
// the shorter at least five long, and the digits equal, since "Part 1" and
// "Part 2" are two songs.
func titleNear(a, b string) (int, bool) {
	a, b = storeLabel.ReplaceAllString(a, ""), storeLabel.ReplaceAllString(b, "")
	na, nb := normalizePair(a, b)
	if na == "" || nb == "" {
		return 0, false
	}
	if na == nb {
		return 0, true
	}
	wa, wb := titleWords(a), titleWords(b)
	if len(wa) > 1 && slices.Equal(sortedWords(wa), sortedWords(wb)) {
		return 0, true
	}
	ra, rb := []rune(na), []rune(nb)
	if min(len(ra), len(rb)) < 5 || digitsOf(na) != digitsOf(nb) ||
		len(wa) != len(wb) {
		return 0, false
	}
	d := levenshtein(ra, rb)
	return d, 6*d <= max(len(ra), len(rb))
}

// storeLabel is the content advisory a download store appends to a title:
// "[Explicit]", "(Clean Version)". It names no other recording.
var storeLabel = regexp.MustCompile(
	`(?i)\s*[\[(]\s*(?:explicit|clean)(?:\s+(?:version|edit|lyrics))?\s*[\])]\s*$`,
)

// titleWords is a Latin title's words, folded as normalizeTitle folds them.
// Nil for a title with no Latin letter, whose words a space may not part.
func titleWords(s string) []string {
	s = unaccentable.Replace(strings.ToLower(norm.NFD.String(s)))
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, s)
	if !titleLatinLetter.MatchString(s) {
		return nil
	}
	return strings.FieldsFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

func sortedWords(words []string) []string {
	return slices.Sorted(slices.Values(words))
}

func digitsOf(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func levenshtein(a, b []rune) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diag := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			diag, row[j] = row[j], min(row[j]+1, row[j-1]+1, diag+cost)
		}
	}
	return row[len(b)]
}

// matchTitle is the track whose title equals title once normalized,
// preferring one on disc: an album can carry a song twice, studio and live.
func matchTitle(tracks []*ent.Track, disc uint8, title string) *ent.Track {
	if disc == 0 {
		disc = 1
	}
	var found *ent.Track
	for _, t := range tracks {
		if !trackTitleIs(t, title) {
			continue
		}
		if t.Disc == disc {
			return t
		}
		if found == nil {
			found = t
		}
	}
	return found
}

var (
	// discTrackRe reads "1-03", "1.03" or "2_11" off the front of a name.
	discTrackRe = regexp.MustCompile(`^(\d)[-._](\d{1,2})(?:\D|$)`)
	// leadNumberRe reads the leading track number, "03" or "103".
	leadNumberRe = regexp.MustCompile(`^(\d{1,3})(?:\D|$)`)
	// segmentNumberRe reads a number standing alone between " - "
	// separators: "Artist - Album - 03 - Title".
	segmentNumberRe = regexp.MustCompile(`(?:^|\s-\s)(\d{1,3})\s-\s`)
	// nameSeparators are what sits between a track number and its title.
	nameSeparators = " -._)]"
)

// TrackInfoFromName reads the disc, track number and title a release's file
// path states, for a file whose tags are not at hand: a torrent's metainfo
// lists names only. The parent folder supplies the disc when it is a "CD2"
// one; a three-digit number ("203") is disc and track. Format is set from the
// extension, as audiotags.Read does.
func TrackInfoFromName(p string) audiotags.Info {
	p = strings.ReplaceAll(p, `\`, "/")
	base := path.Base(p)
	ext := path.Ext(base)
	info := audiotags.Info{Format: strings.TrimPrefix(strings.ToLower(ext), ".")}
	name := strings.TrimSpace(strings.TrimSuffix(base, ext))
	if d, ok := DiscFolderNumber(path.Base(path.Dir(p))); ok {
		info.Disc = d
	}

	rest := name
	if m := discTrackRe.FindStringSubmatch(name); m != nil {
		d, _ := strconv.ParseUint(m[1], 10, 8)
		t, _ := strconv.ParseUint(m[2], 10, 16)
		if info.Disc == 0 {
			info.Disc = uint8(d)
		}
		info.Track = uint16(t)
		rest = name[len(m[1])+1+len(m[2]):]
	} else if m := leadNumberRe.FindStringSubmatch(name); m != nil {
		n, _ := strconv.ParseUint(m[1], 10, 16)
		info.Track = uint16(n)
		if len(m[1]) == 3 && m[1][0] != '0' && n%100 != 0 && info.Disc == 0 {
			info.Disc, info.Track = m[1][0]-'0', uint16(n%100)
		}
		rest = name[len(m[1]):]
	} else if m := segmentNumberRe.FindStringSubmatchIndex(name); m != nil {
		n, _ := strconv.ParseUint(name[m[2]:m[3]], 10, 16)
		info.Track = uint16(n)
		rest = name[m[1]:]
	}
	rest = strings.TrimLeft(rest, nameSeparators)
	// "03 - Artist - Title": the title is the last segment.
	if i := strings.LastIndex(rest, " - "); i >= 0 {
		rest = rest[i+3:]
	}
	info.Title = strings.TrimSpace(rest)
	return info
}

// ReadTrackInfo is audiotags.Read with the disc, track number and title the
// tags lack taken from the file's path, field by field: a vinyl rip often
// carries no tags at all, or a TRACKNUMBER like "A1" that reads as no number.
func ReadTrackInfo(p string) (audiotags.Info, error) {
	info, err := audiotags.Read(p)
	if err != nil {
		return info, err
	}
	if info.Track != 0 && info.Disc != 0 && info.Title != "" {
		return info, nil
	}
	named := TrackInfoFromName(p)
	if info.Track == 0 {
		info.Track = named.Track
	}
	if info.Disc == 0 {
		info.Disc = named.Disc
	}
	if info.Title == "" {
		info.Title = named.Title
	}
	return info, nil
}

// MatchTrackByName is MatchTrack for a file known by its name only. The
// number wins as MatchTrack's tagged pair does; the title, compared loosely
// since a file name drops the characters a file system refuses, decides when
// there is no number or it names no track.
func MatchTrackByName(tracks []*ent.Track, p string) *ent.Track {
	info := TrackInfoFromName(p)
	title := info.Title
	info.Title = ""
	if t := MatchTrack(tracks, info); t != nil {
		return t
	}
	if i := slices.IndexFunc(tracks, func(t *ent.Track) bool {
		return trackTitleIs(t, title)
	}); i >= 0 {
		return tracks[i]
	}
	return nil
}
