package book

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
)

// removeFiles deletes a book's files from disk, each under the root of its
// slot. A failure is logged and does not stop the delete.
func removeFiles(ctx context.Context, b *ent.Book) {
	lib := config.Get().Library
	for _, f := range b.Edges.MediaFiles {
		root := library.BookRoot(lib, string(f.BookKind))
		if err := library.RemoveMediaFile(ctx, f.Path, root); err != nil {
			slog.ErrorContext(ctx, "book file was not deleted from disk",
				"book.id", b.ID, "path", f.Path, "error", err)
		}
	}
}

// removePosters drops the cached covers and the photos of the people the
// delete left with nothing: SQLite reuses rowids, so a stale cache entry would
// show on the next row with that id.
func (s *Service) removePosters(ctx context.Context, bookIDs, authorIDs []uint32) {
	for _, id := range bookIDs {
		if err := s.posters.Remove(coverKind, id); err != nil {
			slog.WarnContext(ctx, "book poster was not removed",
				"book.id", id, "error", err)
		}
	}
	for _, id := range authorIDs {
		if err := s.posters.Remove(photoKind, id); err != nil {
			slog.WarnContext(ctx, "author poster was not removed",
				"author.id", id, "error", err)
		}
	}
}

// DeleteBook removes a book through the schema's cascades, and its files too
// when asked. A volume is refused: it would come back at the next refresh, so
// it goes with its series.
func (s *Service) DeleteBook(
	ctx context.Context,
	id uint32,
	deleteFiles bool,
) error {
	ctx, span := tracer.Start(ctx, "book.delete",
		trace.WithAttributes(
			attribute.Int("book.id", int(id)),
			attribute.Bool("delete_files", deleteFiles),
		))
	defer span.End()

	row, err := s.GetBook(ctx, id)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	if row.Edges.Series != nil {
		return otelx.RecordSpanError(span, ErrSeriesVolume)
	}
	if deleteFiles {
		removeFiles(ctx, row)
	}
	orphans, err := s.db.DeleteBook(ctx, id)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	s.removePosters(ctx, []uint32{id}, orphans)
	slog.InfoContext(ctx, "book deleted", "book.id", id)
	return nil
}

// DeleteSeries removes a series with its volumes, their files and records.
func (s *Service) DeleteSeries(
	ctx context.Context,
	id uint32,
	deleteFiles bool,
) error {
	ctx, span := tracer.Start(ctx, "book.delete_series",
		trace.WithAttributes(
			attribute.Int("series.id", int(id)),
			attribute.Bool("delete_files", deleteFiles),
		))
	defer span.End()

	row, err := s.GetSeries(ctx, id)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	volumeIDs := make([]uint32, 0, len(row.Edges.Volumes))
	for _, v := range row.Edges.Volumes {
		volumeIDs = append(volumeIDs, v.ID)
		if deleteFiles {
			removeFiles(ctx, v)
		}
	}
	orphans, err := s.db.DeleteSeries(ctx, id)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	s.removePosters(ctx, volumeIDs, orphans)
	slog.InfoContext(
		ctx,
		"series deleted",
		"series.id",
		id,
		"volumes",
		len(volumeIDs),
	)
	return nil
}
