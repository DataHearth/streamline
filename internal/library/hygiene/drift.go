package hygiene

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RunDriftCheck verifies every tracked MediaFile is still present on disk.
// Missing files start (or advance) a grace clock keyed off last_seen_at; once
// the file has been gone for at least cfg.DriftGraceTicks intervals the row is
// deleted and its owning movie or episode reverts to "wanted".
func (s *Service) RunDriftCheck(ctx context.Context, interval time.Duration) error {
	graceWindow := interval * time.Duration(s.cfg.DriftGraceTicks)

	ctx, span := tracer.Start(ctx, "hygiene.drift_check")
	defer span.End()

	rows, err := s.store.ListAllMediaFilesWithOwners(ctx)
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

	first, err := s.store.MarkMediaFileMissing(ctx, row.ID)
	if err != nil {
		slog.WarnContext(ctx, "mark missing failed",
			"media_file.id", row.ID, "error", err)
	}
	if first {
		s.recordDrift(ctx, row, events.TypeDriftDetected, map[string]any{
			"path": row.Path,
		})
	}

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

	s.recordDrift(ctx, row, events.TypeDriftConfirmed, map[string]any{
		"path":          row.Path,
		"missing_since": row.LastSeenAt.UTC(),
	})

	switch {
	case row.Edges.Movie != nil:
		s.revertMovie(ctx, row.ID, row.Edges.Movie.ID)
	case row.Edges.Episode != nil:
		s.revertEpisode(ctx, row.ID, row.Edges.Episode.ID)
	default:
		s.deleteOrphan(ctx, row)
	}
}

// recordDrift attributes a drift event to the row's owner. An ownerless row
// (a legacy orphan, reaped below) has nobody to tell, so it is skipped.
func (s *Service) recordDrift(
	ctx context.Context,
	row *ent.MediaFile,
	t events.Type,
	payload map[string]any,
) {
	var scope events.Scope
	var id uint32
	switch {
	case row.Edges.Movie != nil:
		scope, id = events.ScopeMovie, row.Edges.Movie.ID
	case row.Edges.Episode != nil:
		scope, id = events.ScopeEpisode, row.Edges.Episode.ID
	default:
		return
	}
	if err := events.Record(ctx, nil, t, scope, id, payload); err != nil {
		slog.WarnContext(ctx, "record drift event failed",
			"media_file.id", row.ID, "event.type", string(t), "error", err)
	}
}

func (s *Service) revertMovie(ctx context.Context, mediaFileID, movieID uint32) {
	if err := s.store.DeleteMediaFileAndRevertMovie(
		ctx,
		mediaFileID,
		movieID,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file.id", mediaFileID, "movie.id", movieID, "error", err)
		return
	}
	driftReverted.Add(ctx, 1, metric.WithAttributes(
		attribute.Int64("movie.id", int64(movieID)),
	))
}

func (s *Service) revertEpisode(ctx context.Context, mediaFileID, episodeID uint32) {
	if err := s.store.DeleteMediaFileAndRevertEpisode(
		ctx,
		mediaFileID,
		episodeID,
	); err != nil {
		slog.ErrorContext(ctx, "drift revert failed",
			"media_file.id", mediaFileID, "episode.id", episodeID, "error", err)
		return
	}
	driftReverted.Add(ctx, 1, metric.WithAttributes(
		attribute.Int64("episode.id", int64(episodeID)),
	))
}

// deleteOrphan reaps a row whose owner is gone. Movie deletes predating the
// cascading FKs left media_files rows behind with a NULL owner; without this
// they would be re-examined, and warned about, on every tick forever.
func (s *Service) deleteOrphan(ctx context.Context, row *ent.MediaFile) {
	if err := s.store.DeleteMediaFile(ctx, row.ID); err != nil {
		slog.ErrorContext(ctx, "drift orphan delete failed",
			"media_file.id", row.ID, "error", err)
		return
	}
	driftOrphansDeleted.Add(ctx, 1)
	slog.InfoContext(ctx, "drift deleted an ownerless media file",
		"media_file.id", row.ID, "media_file.path", row.Path)
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
