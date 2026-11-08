package book

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
)

// editionLinksBatch is how many editions one Hardcover request reads, the
// provider's own batch size, so a listing is exactly one request.
const editionLinksBatch = 100

// backfillEditionLinks reads Hardcover's mappings (Goodreads, Open Library,
// Wikidata, Google Books, Audible) for every edition a slot follows that has
// none yet. Only those: a popular book has hundreds of editions and the
// library keeps up to forty, but a link only ever opens the edition a slot
// follows, so a book costs at most two editions here and a whole library a
// request per hundred editions, once: a stamped edition is never asked for
// again, links or none, and nothing here runs on an add, a refresh or a
// lookup. An edition a slot moves to later (an add, a
// hand-picked edition, a slot a refresh fills) has no stamp and is read on
// the next pass.
//
// It runs at the end of book-metadata-refresh, after the refreshes, and stops
// where they stop: at the scan reserve of the day's budget, or on a rate
// limit. A failed request leaves its editions unstamped for the next run; the
// cursor keeps them out of the rest of this one.
func (s *Service) backfillEditionLinks(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "book.backfill_edition_links")
	defer span.End()

	done, failed := 0, 0
	defer func() {
		span.SetAttributes(
			attribute.Int("links.done_count", done),
			attribute.Int("links.failed_count", failed),
		)
		if done+failed > 0 {
			slog.InfoContext(ctx, "edition links backfill complete",
				"done", done, "failed", failed)
		}
	}()

	var cursor uint32
	for {
		if s.budgetLow() {
			return
		}
		rows, err := s.db.ListEditionsAwaitingLinks(ctx, cursor, editionLinksBatch)
		if err != nil {
			_ = otelx.RecordSpanError(span, err)
			slog.WarnContext(ctx, "edition links backfill failed", "error", err)
			return
		}
		if len(rows) == 0 {
			return
		}
		cursor = rows[len(rows)-1].ID
		n, err := s.storeEditionLinks(ctx, rows)
		done += n
		switch {
		case err == nil:
		case errors.Is(err, metadata.ErrRateLimited) || ctx.Err() != nil:
			slog.InfoContext(ctx, "edition links backfill paused",
				"done", done, "error", err)
			return
		default:
			failed += len(rows) - n
			slog.WarnContext(ctx, "edition links backfill: batch failed",
				"editions", len(rows), "error", err)
		}
		if len(rows) < editionLinksBatch {
			return
		}
	}
}

// storeEditionLinks reads one batch's mappings in one request and stores
// them. An edition Hardcover no longer knows is stamped with no links, so it
// is not asked for again.
func (s *Service) storeEditionLinks(
	ctx context.Context,
	rows []*ent.BookEdition,
) (int, error) {
	hcIDs := make([]uint32, 0, len(rows))
	for _, e := range rows {
		hcIDs = append(hcIDs, e.HardcoverEditionID)
	}
	got, err := s.metadata.EditionLinks(ctx, hcIDs)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	stored := 0
	for _, e := range rows {
		ids := db.ExternalIDs(got[e.HardcoverEditionID])
		if ids == nil {
			ids = db.ExternalIDs{}
		}
		if err := s.db.SetEditionLinks(ctx, e.ID, ids, now); err != nil {
			return stored, err
		}
		stored++
	}
	return stored, nil
}
