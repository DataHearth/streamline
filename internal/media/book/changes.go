package book

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/metadatachange"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
)

func bookOwner(id uint32) db.ChangeOwner {
	return db.ChangeOwner{Type: db.ChangeOwnerBook, ID: id}
}

// reviewEditions checks, in one Hardcover request, the editions followed by
// the formats of these books that hold a file, and records the ones
// Hardcover no longer has. The books come loaded with their files and slot
// editions.
func (s *Service) reviewEditions(
	ctx context.Context,
	prov metadata.BookProvider,
	books []*ent.Book,
) error {
	var ids []uint32
	for _, b := range books {
		for _, kind := range slotKinds {
			if e := slotEdition(b, kind); e != nil && hasSlotFile(b, kind) {
				ids = append(ids, e.HardcoverEditionID)
			}
		}
	}
	exist := map[uint32]bool{}
	if len(ids) > 0 {
		var err error
		if exist, err = prov.ExistingEditions(ctx, ids); err != nil {
			return err
		}
	}
	for _, b := range books {
		var (
			slots   []string
			current []map[string]any
			keys    []string
		)
		for _, kind := range slotKinds {
			e := slotEdition(b, kind)
			if e == nil || !hasSlotFile(b, kind) || exist[e.HardcoverEditionID] {
				continue
			}
			slots = append(slots, kind)
			current = append(current, map[string]any{
				"format": kind, "title": e.Title, "publisher": e.Publisher,
				"year": e.Year, "language": e.Language,
			})
			keys = append(keys, fmt.Sprintf("%s:%d", kind, e.HardcoverEditionID))
		}
		owner := bookOwner(b.ID)
		if len(slots) == 0 {
			if err := s.db.ClearChange(
				ctx,
				owner,
				metadatachange.KindEditionRemoved,
			); err != nil {
				return err
			}
			continue
		}
		if err := s.db.ReportChange(ctx, owner, metadatachange.KindEditionRemoved,
			strings.Join(keys, ","),
			map[string]any{"slots": slots, "current": current}); err != nil {
			return err
		}
	}
	return nil
}

// FollowChange applies a book's pending change: a format whose edition
// Hardcover removed gets the edition the pick rules choose now, its file
// kept. db.ErrNotFollowable for a book Hardcover removed. The change row
// is left to RefreshBook's review, so a failed refresh keeps it to retry.
func (s *Service) FollowChange(ctx context.Context, c *ent.MetadataChange) error {
	if c.Kind != metadatachange.KindEditionRemoved {
		return db.ErrNotFollowable
	}
	id := db.ChangeOwnerOf(c).ID
	raw, _ := c.Payload["slots"].([]any)
	for _, v := range raw {
		kind, _ := v.(string)
		if !slices.Contains(slotKinds, kind) {
			continue
		}
		if err := s.db.SetBookSlotEdition(ctx, id, kind, 0); err != nil {
			return err
		}
	}
	_, err := s.RefreshBook(ctx, id)
	return err
}
