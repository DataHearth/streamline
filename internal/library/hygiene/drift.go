package hygiene

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RunDriftCheck verifies every tracked MediaFile is still present on disk.
// Missing files start (or advance) a grace clock keyed off last_seen_at; once
// the file has been gone for at least cfg.DriftGraceTicks intervals the row
// is deleted and the owning movie reverts to "wanted".
func (s *Service) RunDriftCheck(ctx context.Context, interval time.Duration) error {
	graceWindow := interval * time.Duration(s.cfg.DriftGraceTicks)

	ctx, span := tracer.Start(ctx, "hygiene.drift_check")
	defer span.End()

	rows, err := s.store.ListAllMediaFilesWithMovie(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("rows", len(rows)))

	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.checkDrift(ctx, row, graceWindow)
	}
	return nil
}

func (s *Service) checkDrift(
	ctx context.Context,
	row *ent.MediaFile,
	graceWindow time.Duration,
) {
	_, statErr := os.Stat(row.Path)
	switch {
	case statErr == nil:
		if err := s.store.BumpMediaFileLastSeen(ctx, row.ID); err != nil {
			slog.WarnContext(ctx, "bump last_seen_at failed",
				"media_file_id", row.ID, "error", err)
			return
		}
		driftVerified.Add(ctx, 1)
	case errors.Is(statErr, fs.ErrNotExist):
		s.handleMissing(ctx, row, graceWindow)
	default:
		slog.WarnContext(ctx, "stat failed (transient)",
			"path", row.Path, "error", statErr)
		driftStatErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_kind", classifyStatErr(statErr)),
		))
	}
}

func (s *Service) handleMissing(
	ctx context.Context,
	row *ent.MediaFile,
	graceWindow time.Duration,
) {
	driftDrifted.Add(ctx, 1)

	// First-tick free pass: NULL last_seen_at → start grace clock.
	if row.LastSeenAt == nil {
		if err := s.store.BumpMediaFileLastSeen(ctx, row.ID); err != nil {
			slog.WarnContext(ctx, "bump last_seen_at (grace start) failed",
				"media_file_id", row.ID, "error", err)
		}
		return
	}
	if time.Since(*row.LastSeenAt) < graceWindow {
		return
	}

	movieID := uint32(0)
	if row.Edges.Movie != nil {
		movieID = row.Edges.Movie.ID
	}
	if movieID == 0 {
		slog.WarnContext(ctx, "drift revert: media_file has no movie edge",
			"media_file_id", row.ID)
		return
	}

	if err := s.store.DeleteMediaFileAndRevertMovie(
		ctx,
		row.ID,
		movieID,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file_id", row.ID, "movie_id", movieID, "error", err)
		return
	}
	driftReverted.Add(ctx, 1, metric.WithAttributes(
		attribute.Int64("movie.id", int64(movieID)),
	))
}

func classifyStatErr(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "permission"
	case errors.Is(err, fs.ErrInvalid):
		return "invalid"
	default:
		return "io"
	}
}
