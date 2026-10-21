package library

import (
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

var discFolderRe = regexp.MustCompile(`(?i)^(?:cd|dis[ck])\s*(\d+)$`)

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
	disc := info.Disc
	if disc == 0 {
		disc = 1
	}
	if info.Track > 0 {
		if i := slices.IndexFunc(tracks, func(t *ent.Track) bool {
			return t.Disc == disc && t.Position == info.Track
		}); i >= 0 {
			return tracks[i]
		}
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
