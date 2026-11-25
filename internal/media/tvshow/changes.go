package tvshow

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/metadatachange"
	"github.com/datahearth/streamline/internal/db"
)

func showOwner(id uint32) db.ChangeOwner {
	return db.ChangeOwner{Type: db.ChangeOwnerTVShow, ID: id}
}

// reviewEpisodes records the episodes holding a file that the provider no
// longer lists, or forgets the change when there are none.
func (s *Service) reviewEpisodes(
	ctx context.Context,
	showID uint32,
	unlisted []db.UnlistedEpisode,
) error {
	if len(unlisted) == 0 {
		return s.db.ClearChange(
			ctx,
			showOwner(showID),
			metadatachange.KindEpisodesUnlisted,
		)
	}
	items := make([]map[string]any, 0, len(unlisted))
	ids := make([]string, 0, len(unlisted))
	var seasons []uint16
	for _, e := range unlisted {
		items = append(items, map[string]any{
			"id":    e.ID,
			"label": fmt.Sprintf("S%02dE%02d", e.Season, e.Number),
			"title": e.Title,
		})
		ids = append(ids, strconv.FormatUint(uint64(e.ID), 10))
		if e.SeasonUnlisted && !slices.Contains(seasons, e.Season) {
			seasons = append(seasons, e.Season)
		}
	}
	return s.db.ReportChange(
		ctx,
		showOwner(showID),
		metadatachange.KindEpisodesUnlisted,
		strings.Join(ids, ","),
		map[string]any{"files": len(unlisted), "items": items, "seasons": seasons},
	)
}

func (s *Service) FollowChange(ctx context.Context, c *ent.MetadataChange) error {
	if c.Kind != metadatachange.KindEpisodesUnlisted {
		return db.ErrNotFollowable
	}
	showID := db.ChangeOwnerOf(c).ID
	var ids []uint32
	for part := range strings.SplitSeq(c.Fingerprint, ",") {
		id, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return err
		}
		ids = append(ids, uint32(id))
	}
	var seasons []uint16
	if raw, ok := c.Payload["seasons"].([]any); ok {
		for _, n := range raw {
			if f, ok := n.(float64); ok {
				seasons = append(seasons, uint16(f))
			}
		}
	}
	if err := s.db.DeleteUnlistedEpisodes(ctx, showID, ids, seasons); err != nil {
		return err
	}
	return s.db.ClearChange(
		ctx,
		showOwner(showID),
		metadatachange.KindEpisodesUnlisted,
	)
}
