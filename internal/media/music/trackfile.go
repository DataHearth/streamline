package music

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
)

// DeleteTrackFile removes every media file of the track from disk and the
// database, then moves its album from available back to wanted. Every path is
// checked against the music root before anything is deleted: one outside it
// answers ErrOutsideRoot and leaves the track as it was.
func (s *Service) DeleteTrackFile(ctx context.Context, trackID uint32) error {
	ctx, span := tracer.Start(ctx, "music.delete_track_file",
		trace.WithAttributes(attribute.Int64("track.id", int64(trackID))))
	defer span.End()

	t, err := s.db.FindTrackByID(ctx, trackID)
	if err != nil {
		if ent.IsNotFound(err) {
			return otelx.RecordSpanError(span, ErrTrackNotFound)
		}
		return otelx.RecordSpanError(span, err)
	}
	root := config.Get().Library.MusicPath
	for _, f := range t.Edges.MediaFiles {
		if !library.PathUnderRoot(f.Path, root) {
			return otelx.RecordSpanError(span,
				fmt.Errorf("delete %s: %w", f.Path, ErrOutsideRoot))
		}
	}
	for _, f := range t.Edges.MediaFiles {
		// A file already gone (or taken with a same-named sibling's sidecars)
		// is what the delete wanted anyway; its row still has to go.
		if err := library.RemoveMediaFile(ctx, f.Path, root); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return otelx.RecordSpanError(span, err)
		}
		if err := s.db.DeleteMediaFile(ctx, f.ID); err != nil {
			return otelx.RecordSpanError(span,
				fmt.Errorf("delete media file %d: %w", f.ID, err))
		}
	}
	if alb := t.Edges.Album; alb != nil {
		if _, err := s.db.SetAlbumStatus(
			ctx, alb.ID,
			[]album.Status{album.StatusAvailable},
			album.StatusWanted,
		); err != nil {
			slog.WarnContext(ctx, "mark album wanted after track file delete failed",
				"album.id", alb.ID, "error", err)
		}
	}
	return nil
}
