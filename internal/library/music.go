package library

import (
	"math"
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
