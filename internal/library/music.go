package library

import (
	"slices"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

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
