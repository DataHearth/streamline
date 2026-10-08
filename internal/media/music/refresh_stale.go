package music

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

const (
	// metadataMinRefreshInterval is how long an artist is left alone between
	// scheduled refreshes. A manual run waives it.
	metadataMinRefreshInterval = 24 * time.Hour

	// refreshBatch caps one run: each artist costs a MusicBrainz request per
	// new release-group at 1 req/s, so the oldest are refreshed first and the
	// backlog drains over successive ticks.
	refreshBatch = 10
)

// MetadataRefresher re-pulls provider metadata for stale artists.
type MetadataRefresher interface {
	RefreshStale(ctx context.Context) error
}

var _ MetadataRefresher = (*Service)(nil)

// RefreshStale refreshes up to refreshBatch artists not refreshed within
// metadataMinRefreshInterval, oldest first. Per-row failures are logged and
// skipped; it returns an error only when the initial query fails.
func (s *Service) RefreshStale(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "music.refresh_stale")
	defer span.End()

	cutoff := time.Now()
	if !scheduler.Manual(ctx) {
		cutoff = cutoff.Add(-metadataMinRefreshInterval)
	}
	rows, err := s.db.ListArtistsStaleSince(ctx, cutoff, refreshBatch)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("refresh.candidate_count", len(rows)))

	refreshed, skipped := 0, 0
	for i, a := range rows {
		scheduler.Progress(ctx, i, len(rows))
		if _, err := s.RefreshOne(ctx, a.ID); err != nil {
			slog.WarnContext(ctx, "artist refresh failed",
				"artist.id", a.ID, "error", err)
			skipped++
			continue
		}
		refreshed++
	}

	span.SetAttributes(
		attribute.Int("refresh.refreshed_count", refreshed),
		attribute.Int("refresh.skipped_count", skipped),
	)
	slog.InfoContext(ctx, "music metadata refresh complete",
		"refreshed", refreshed, "skipped", skipped)
	return nil
}
