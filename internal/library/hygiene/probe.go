package hygiene

import (
	"context"
	"log/slog"
	"os"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
)

// probeBatchSize bounds one tick so a large pre-probe library fills in over
// hours without a thundering herd of ffprobe processes.
const probeBatchSize = 25

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

	rows, err := s.store.ListUnprobedMediaFiles(ctx, probeBatchSize)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("rows", len(rows)))

	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if _, err := os.Stat(row.Path); err != nil {
			mediaProbeMissing.Add(ctx, 1)
			continue
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

		// Log-and-continue, like checkDrift's own store-call failures: rows
		// come back oldest-first, so returning here on a deterministic
		// per-row failure would re-select the same failing row at the head
		// of every subsequent tick and starve every newer file behind it.
		if err := s.store.StampMediaFileProbe(ctx, row.ID, info); err != nil {
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
	return nil
}
