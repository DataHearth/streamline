package book

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

const (
	// metadataMinRefreshInterval is how long an author is left alone between
	// scheduled refreshes. A manual run waives it.
	metadataMinRefreshInterval = 24 * time.Hour

	// refreshBatch caps one run to stay inside Hardcover's 1 req/s limit; the
	// oldest are refreshed first and the backlog drains over successive ticks.
	refreshBatch = 10
)

// MetadataRefresher re-pulls provider metadata for stale authors.
type MetadataRefresher interface {
	RefreshStale(ctx context.Context) error
}

var _ MetadataRefresher = (*Service)(nil)

// RefreshStale refreshes up to refreshBatch authors not refreshed within
// metadataMinRefreshInterval, oldest first. Per-row failures are logged and
// skipped; it returns an error only when the initial query fails. Without a
// Hardcover key there is nothing to refresh from, so it returns at once
// instead of failing every author on every tick.
func (s *Service) RefreshStale(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "book.refresh_stale")
	defer span.End()

	if s.metadata == nil {
		return nil
	}
	cutoff := time.Now()
	if !scheduler.Manual(ctx) {
		cutoff = cutoff.Add(-metadataMinRefreshInterval)
	}
	rows, err := s.db.ListAuthorsStaleSince(ctx, cutoff, refreshBatch)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("refresh.candidate_count", len(rows)))

	refreshed, skipped := 0, 0
	for i, a := range rows {
		scheduler.Progress(ctx, i, len(rows))
		_, err := s.RefreshOne(ctx, a.ID)
		if errors.Is(err, metadata.ErrRateLimited) {
			slog.WarnContext(ctx, "author refresh stopped: hardcover rate limited",
				"refreshed", refreshed, "remaining", len(rows)-i, "error", err)
			break
		}
		if err != nil {
			slog.WarnContext(ctx, "author refresh failed",
				"author.id", a.ID, "error", err)
			skipped++
			continue
		}
		refreshed++
	}

	span.SetAttributes(
		attribute.Int("refresh.refreshed_count", refreshed),
		attribute.Int("refresh.skipped_count", skipped),
	)
	slog.InfoContext(ctx, "book metadata refresh complete",
		"refreshed", refreshed, "skipped", skipped)
	return nil
}
