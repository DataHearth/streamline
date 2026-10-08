package importer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// importBookRecord routes a completed book download into the slot it was
// grabbed for. One record fills one slot: an ebook record imports a single
// file, an audiobook record every audio file into one folder. Files are
// transferred as they are — nothing is probed, tagged or held.
func (w *Worker) importBookRecord(
	ctx context.Context,
	span trace.Span,
	rec *ent.DownloadRecord,
	libCfg config.LibraryConfig,
) error {
	b := rec.Edges.Book
	author := b.Edges.Author
	if author == nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("book %d missing author context", b.ID),
		)
	}
	span.SetAttributes(
		attribute.Int64("book.id", int64(b.ID)),
		attribute.String("book.kind", string(rec.BookKind)),
	)
	defer w.lockEntity(fmt.Sprintf("book:%d:%s", b.ID, rec.BookKind))()

	kind := mediafile.BookKind(rec.BookKind)
	switch rec.BookKind {
	case downloadrecord.BookKindEbook, downloadrecord.BookKindAudiobook:
	default:
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("record %d: unknown book kind %q", rec.ID, rec.BookKind),
		)
	}

	existing, err := w.db.ListMediaFilesByBook(ctx, b.ID, kind)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("list book files: %w", err))
	}
	replace := rec.ReplaceMode != downloadrecord.ReplaceModeNone
	if len(existing) > 0 && !replace {
		return otelx.RecordSpanError(span, library.ErrDestExists)
	}

	var (
		placed []library.ImportedFile
		aside  []string
	)
	switch rec.BookKind {
	case downloadrecord.BookKindEbook:
		profile, ok := config.ResolveEbookQualityProfile(author.EbookQualityProfile)
		if !ok {
			return otelx.RecordSpanError(span, ErrNoBookProfile)
		}
		aside, err = setAside(ctx, existing)
		if err != nil {
			return otelx.RecordSpanError(span, err)
		}
		f, err := w.lib.ImportEbook(ctx, rec.SavePath, author, b, profile, replace)
		if err != nil {
			putBack(ctx, aside)
			return otelx.RecordSpanError(span, err)
		}
		placed = []library.ImportedFile{f}
	case downloadrecord.BookKindAudiobook:
		aside, err = setAside(ctx, existing)
		if err != nil {
			return otelx.RecordSpanError(span, err)
		}
		placed, err = w.lib.ImportAudiobook(ctx, rec.SavePath, author, b, replace)
		if err != nil {
			putBack(ctx, aside)
			return otelx.RecordSpanError(span, err)
		}
	}

	rows := make([]db.MediaFileRow, 0, len(placed))
	for _, f := range placed {
		format := f.Parsed.Extension
		rows = append(rows, db.MediaFileRow{
			Path:    f.Path,
			Size:    f.Size,
			Quality: bookQuality(kind, format),
			Format:  format,
			Parsed:  &f.Parsed,
		})
	}
	replacedIDs := make([]uint32, 0, len(existing))
	for _, mf := range existing {
		replacedIDs = append(replacedIDs, mf.ID)
	}
	if err := w.db.RecordBookImportSuccess(ctx, db.RecordBookImportSuccessParams{
		RecordID:        rec.ID,
		BookID:          b.ID,
		Kind:            kind,
		Files:           rows,
		ReplacedFileIDs: replacedIDs,
	}); err != nil {
		putBack(ctx, aside)
		return otelx.RecordSpanError(
			span, fmt.Errorf("record book import success: %w", err),
		)
	}
	dropAside(ctx, aside)
	slog.InfoContext(ctx, "imported book",
		"book.id", b.ID, "book.kind", string(rec.BookKind), "files", len(placed))

	w.cleanupTorrent(ctx, rec, libCfg)
	return nil
}

// bookQuality is the profile-ladder tier of a file: its format when the
// ladder names it, "other" otherwise.
func bookQuality(kind mediafile.BookKind, ext string) string {
	ext = strings.ToLower(ext)
	switch kind {
	case mediafile.BookKindEbook:
		switch ext {
		case "epub", "azw3", "mobi", "pdf":
			return ext
		}
	case mediafile.BookKindAudiobook:
		switch ext {
		case "m4b", "mp3":
			return ext
		}
	}
	return "other"
}
