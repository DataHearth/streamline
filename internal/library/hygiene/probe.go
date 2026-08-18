package hygiene

import (
	"context"
	"log/slog"
	"os"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
)

// probeBatchSize bounds one tick so a large pre-probe library fills in over
// hours without a thundering herd of ffprobe processes.
const probeBatchSize = 25

// probeFetchRoundsMax bounds how many times a tick re-fetches a larger batch
// when unreadable rows (stat failures, deliberately skipped without a stamp)
// crowd out real probe attempts. Rows behind them stay in every re-fetch
// since they're never stamped, so without this bound a library backed by an
// unreachable mount would grow the fetch limit forever within one tick.
const probeFetchRoundsMax = 4

// RunMediaProbe backfills media info onto MediaFile rows that have never been
// probed (probed_at IS NULL) — including rows created by adoption, orphan
// scan and bulk import, which deliberately don't probe inline. No-ops when
// ffmpeg is disabled or missing, so the config toggle works without a
// restart.
func (s *Service) RunMediaProbe(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "hygiene.media_probe")
	defer span.End()

	if !config.Get().FFmpeg.Enabled || s.Probe == nil || !s.Probe.Available() {
		return nil
	}

	// Rows deliberately skipped for a stat failure are never stamped, so a
	// re-fetch at a larger limit sees them again — track IDs already handled
	// this tick and grow the limit until probeBatchSize probes were actually
	// attempted (or the table runs out, or probeFetchRoundsMax is hit).
	seen := make(map[uint32]struct{})
	attempted := 0
	limit := probeBatchSize
	for range probeFetchRoundsMax {
		rows, err := s.store.ListUnprobedMediaFiles(ctx, limit)
		if err != nil {
			return otelx.RecordSpanError(span, err)
		}

		for _, row := range rows {
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}

			if ctx.Err() != nil {
				return ctx.Err()
			}

			if s.probeRow(ctx, row) {
				attempted++
				if attempted >= probeBatchSize {
					break
				}
			}
		}

		if attempted >= probeBatchSize || len(rows) < limit {
			break
		}
		limit *= 2
	}
	span.SetAttributes(attribute.Int("rows", len(seen)))
	return nil
}

// probeRow stats, probes, and stamps a single row, returning true when a
// probe was actually attempted. A stat failure returns false without
// stamping — the drift-check job owns missing/unreadable files, this backfill
// just leaves them for it rather than guessing.
func (s *Service) probeRow(ctx context.Context, row *ent.MediaFile) bool {
	if _, err := os.Stat(row.Path); err != nil {
		mediaProbeMissing.Add(ctx, 1)
		return false
	}

	info, err := s.Probe.Probe(ctx, row.Path)
	if err != nil {
		slog.WarnContext(
			ctx,
			"media probe failed",
			"path",
			row.Path,
			"error",
			err,
		)
		mediaProbeFailed.Add(ctx, 1)
	} else {
		mediaProbeProbed.Add(ctx, 1)
	}

	// Log-and-continue, like checkDrift's own store-call failures: rows come
	// back oldest-first, so returning here on a deterministic per-row failure
	// would re-select the same failing row at the head of every subsequent
	// tick and starve every newer file behind it.
	if err := s.store.StampMediaFileProbe(ctx, row.ID, info); err != nil {
		// The drift-check job can delete this row between
		// ListUnprobedMediaFiles and this stamp write; that race is routine,
		// not an error.
		if !ent.IsNotFound(err) {
			slog.ErrorContext(
				ctx,
				"stamp media probe failed",
				"media_file.id",
				row.ID,
				"error",
				err,
			)
		}
	}
	return true
}
