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

// editionLinksBatch matches the provider's batch: one listing, one request.
const editionLinksBatch = 100

// backfillEditionLinks reads Hardcover's mappings for the editions a slot
// follows that were never read, once each, at the end of
// book-metadata-refresh. It stops at the scan reserve, on a rate limit and
// on a rejected key; a failed batch waits for the next run.
func (s *Service) backfillEditionLinks(ctx context.Context) {
	ctx, span := tracer.Start(ctx, "book.backfill_edition_links")
	defer span.End()

	done, failed := 0, 0
	var failures []error
	defer func() {
		span.SetAttributes(
			attribute.Int("links.done_count", done),
			attribute.Int("links.failed_count", failed),
		)
		_ = otelx.RecordSpanError(span, errors.Join(failures...))
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
			failures = append(failures, err)
			slog.WarnContext(ctx, "edition links backfill failed", "error", err)
			return
		}
		if len(rows) == 0 {
			return
		}
		cursor = rows[len(rows)-1].ID
		err = s.storeEditionLinks(ctx, rows)
		switch {
		case err == nil:
			done += len(rows)
		case errors.Is(err, metadata.ErrRateLimited) || ctx.Err() != nil:
			slog.InfoContext(ctx, "edition links backfill paused",
				"done", done, "error", err)
			return
		case errors.Is(err, metadata.ErrHardcoverUnauthorized):
			failed += len(rows)
			failures = append(failures, err)
			slog.WarnContext(ctx, "edition links backfill stopped: key rejected",
				"done", done, "error", err)
			return
		default:
			failed += len(rows)
			failures = append(failures, err)
			slog.WarnContext(ctx, "edition links backfill: batch failed",
				"editions", len(rows), "error", err)
		}
		if len(rows) < editionLinksBatch {
			return
		}
	}
}

// storeEditionLinks reads a batch in one request and stores it in one
// transaction. An edition Hardcover no longer knows is stamped with none.
func (s *Service) storeEditionLinks(
	ctx context.Context,
	rows []*ent.BookEdition,
) error {
	hcIDs := make([]uint32, 0, len(rows))
	for _, e := range rows {
		hcIDs = append(hcIDs, e.HardcoverEditionID)
	}
	got, err := s.metadata.EditionLinks(ctx, hcIDs)
	if err != nil {
		return err
	}
	links := make(map[uint32]db.ExternalIDs, len(rows))
	for _, e := range rows {
		ids := db.ExternalIDs(got[e.HardcoverEditionID])
		if ids == nil {
			ids = db.ExternalIDs{}
		}
		links[e.ID] = ids
	}
	return s.db.SetEditionLinks(ctx, links, time.Now())
}
