package library

import (
	"math"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

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

// MatchTrack prefers the tagged (disc, track) pair and falls back to the title.
func MatchTrack(tracks []*ent.Track, info audiotags.Info) *ent.Track {
	if t := trackAt(tracks, info); t != nil {
		return t
	}
	title := strings.TrimSpace(info.Title)
	if title == "" {
		return nil
	}
	if i := slices.IndexFunc(tracks, func(t *ent.Track) bool {
		return strings.EqualFold(strings.TrimSpace(t.Title), title)
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
// album's 01 is "Regret", and by number every song landed in another song's
// slot with the interludes between them.
type TrackMatcher struct {
	tracks   []*ent.Track
	byNumber bool
}

// NewTrackMatcher trusts the numbers unless the titles show the release
// ordered otherwise: more of the files whose number reaches a track state
// another track's title than state that one's. A title naming no track of
// the album (a translation, a typo) says nothing about the order. Distrusted,
// a file matches by title alone and an interlude matches nothing.
func NewTrackMatcher(tracks []*ent.Track, infos []audiotags.Info) TrackMatcher {
	agree, elsewhere := 0, 0
	for _, info := range infos {
		t := trackAt(tracks, info)
		if t == nil {
			continue
		}
		switch {
		case TitlePrefixMatches(t.Title, info.Title):
			agree++
		case matchTitle(tracks, info.Disc, info.Title) != nil:
			elsewhere++
		}
	}
	return TrackMatcher{
		tracks:   tracks,
		byNumber: elsewhere < 2 || elsewhere <= agree,
	}
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
		return MatchTrack(m.tracks, info)
	}
	return matchTitle(m.tracks, info.Disc, info.Title)
}

// MatchByName is MatchTrackByName when the numbers are trusted, else the
// track whose title the file name states, on its disc first.
func (m TrackMatcher) MatchByName(p string) *ent.Track {
	if m.byNumber {
		return MatchTrackByName(m.tracks, p)
	}
	info := TrackInfoFromName(p)
	return matchTitle(m.tracks, info.Disc, info.Title)
}

// matchTitle is the track whose title equals title once normalized,
// preferring one on disc: an album can carry a song twice, studio and live.
func matchTitle(tracks []*ent.Track, disc uint8, title string) *ent.Track {
	if disc == 0 {
		disc = 1
	}
	var found *ent.Track
	for _, t := range tracks {
		if !TitleMatchesStrict(t.Title, title) {
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
		return TitleMatchesStrict(t.Title, title)
	}); i >= 0 {
		return tracks[i]
	}
	return nil
}
