package music

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

// metadataMinRefreshInterval is how long an artist is left alone between
// scheduled refreshes. A manual run waives it.
const metadataMinRefreshInterval = 24 * time.Hour

// MetadataRefresher re-pulls provider metadata for stale artists.
type MetadataRefresher interface {
	RefreshStale(ctx context.Context) error
}

var _ MetadataRefresher = (*Service)(nil)

// RefreshStale refreshes every artist not refreshed within
// metadataMinRefreshInterval, oldest first. Per-row failures are logged and
// skipped, and a rate limit ends the run, the rest waiting for the next one;
// it returns an error only when the initial query fails.
func (s *Service) RefreshStale(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "music.refresh_stale")
	defer span.End()

	cutoff := time.Now()
	if !scheduler.Manual(ctx) {
		cutoff = cutoff.Add(-metadataMinRefreshInterval)
	}
	rows, err := s.db.ListArtistsStaleSince(ctx, cutoff)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("refresh.candidate_count", len(rows)))
	// The hydration sweep rides this job: it queues unfinished albums for the
	// background worker and returns, so it costs the run nothing.
	s.sweepHydration(ctx)

	refreshed, skipped := 0, 0
	for i, a := range rows {
		scheduler.Progress(ctx, i, len(rows))
		if _, err := s.RefreshOne(metadata.Background(ctx), a.ID); err != nil {
			slog.WarnContext(ctx, "artist refresh failed",
				"artist.id", a.ID, "error", err)
			skipped++
			// The limiter refuses every background call for the length of
			// the wait, so each artist left would fail the same way.
			if errors.Is(err, metadata.ErrRateLimited) || ctx.Err() != nil {
				break
			}
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
