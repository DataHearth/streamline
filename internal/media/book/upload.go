package book

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"path"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/upload"
)

const defaultUploadTitle = "Upload"

// ReceiveFunc is the handler's half of an upload: it reads the request body once
// the service has checked the target.
type ReceiveFunc func(ctx context.Context, kind upload.Kind) (*upload.Staged, error)

type UploadOptions struct {
	Replace bool
	Title   string
}

const (
	outcomeKept    = "kept"
	outcomeIgnored = "ignored"
	outcomeRefused = "refused"
)

// UploadFileView is what an upload's import would do with one file.
type UploadFileView struct {
	Path    string
	Size    int64
	Outcome string
}

// BookUploadPreview lists what an upload of files to one slot would keep. An
// ebook import keeps the single best file under the book's profile, the way
// ImportEbook picks it; an audiobook import keeps every audio file.
func (s *Service) BookUploadPreview(
	ctx context.Context,
	bookID uint32,
	kind string,
	files []download.ReleaseFile,
) ([]UploadFileView, error) {
	if !validSlotKind(kind) {
		return nil, ErrInvalidSlotKind
	}
	b, err := s.findBook(ctx, bookID)
	if err != nil {
		return nil, err
	}
	busy, err := s.db.BookSlotHasLiveRecord(
		ctx,
		bookID,
		downloadrecord.BookKind(kind),
	)
	if err != nil {
		return nil, fmt.Errorf("check live record: %w", err)
	}
	if busy {
		return nil, upload.ErrConflict
	}
	out := make([]UploadFileView, 0, len(files))
	var total int64
	for _, f := range files {
		total += f.Size
		out = append(out, UploadFileView{
			Path:    f.Path,
			Size:    f.Size,
			Outcome: uploadOutcome(slotUploadKind(kind), f.Path),
		})
	}
	if err := upload.CheckSpace(total); err != nil {
		return nil, err
	}
	if kind != slotEbook {
		return out, nil
	}
	profile, ok := ProfileFor(b)
	if !ok {
		return nil, ErrNoQualityProfile
	}
	best, bestScore := -1, -1
	for i, v := range out {
		if v.Outcome != outcomeKept {
			continue
		}
		sc := library.ScoreEbookRelease(
			library.ParseBookRelease(path.Base(v.Path)), profile,
		)
		if sc < 0 {
			out[i].Outcome = outcomeIgnored
			continue
		}
		if sc > bestScore || (sc == bestScore && v.Size > out[best].Size) {
			best, bestScore = i, sc
		}
	}
	for i := range out {
		if out[i].Outcome == outcomeKept && i != best {
			out[i].Outcome = outcomeIgnored
		}
	}
	return out, nil
}

func uploadOutcome(kind upload.Kind, name string) string {
	switch upload.Classify(kind, name) {
	case upload.Keep:
		return outcomeKept
	case upload.Refuse:
		return outcomeRefused
	}
	return outcomeIgnored
}

func slotUploadKind(kind string) upload.Kind {
	if kind == slotAudiobook {
		return upload.KindAudiobook
	}
	return upload.KindEbook
}

// UploadBook files the received files as an importing record for one slot.
func (s *Service) UploadBook(
	ctx context.Context,
	bookID uint32,
	kind string,
	o UploadOptions,
	recv ReceiveFunc,
) (*ent.DownloadRecord, error) {
	ctx, span := tracer.Start(ctx, "book.upload",
		trace.WithAttributes(
			attribute.Int("book.id", int(bookID)),
			attribute.String("book.kind", kind),
		))
	defer span.End()

	if !validSlotKind(kind) {
		return nil, otelx.RecordSpanError(span, ErrInvalidSlotKind)
	}
	b, err := s.findBook(ctx, bookID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if b.LastRefreshedAt == nil {
		return nil, otelx.RecordSpanError(span, ErrNotHydrated)
	}
	if kind == slotEbook {
		if _, ok := ProfileFor(b); !ok {
			return nil, otelx.RecordSpanError(span, ErrNoQualityProfile)
		}
	}
	recKind := downloadrecord.BookKind(kind)
	busy, err := s.db.BookSlotHasLiveRecord(ctx, bookID, recKind)
	if err != nil {
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("check live record: %w", err))
	}
	if busy {
		return nil, otelx.RecordSpanError(span, upload.ErrConflict)
	}
	held := slices.ContainsFunc(
		db.PresentFiles(b.Edges.MediaFiles),
		func(f *ent.MediaFile) bool { return string(f.BookKind) == kind },
	)
	if held && !o.Replace {
		return nil, otelx.RecordSpanError(span, fmt.Errorf(
			"%w: the %s slot already has a file; replace it to upload",
			upload.ErrRejected, kind,
		))
	}

	staged, err := recv(ctx, slotUploadKind(kind))
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	defer staged.Done()

	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	busy, err = s.db.BookSlotHasLiveRecord(ctx, bookID, recKind)
	if err != nil {
		staged.Discard()
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("check live record: %w", err))
	}
	if busy {
		staged.Discard()
		return nil, otelx.RecordSpanError(span, upload.ErrConflict)
	}

	var size int64
	for _, f := range staged.Files {
		size += f.Size
	}
	// Marked first and created with its replace mode: the record is born
	// importing, so a scan can pick it up the moment it exists.
	if from := slotStatus(b, kind); grabbableSlotStatus(kind, from) {
		if err := s.db.SetBookSlotStatus(
			ctx, bookID, kind, from, slotDownloading,
		); err != nil {
			slog.WarnContext(ctx, "upload book: set slot status failed",
				"book.id", bookID, "book.kind", kind, "error", err)
		}
	}
	p := db.CreateDownloadRecordParams{
		Title:    cmp.Or(o.Title, defaultUploadTitle),
		Size:     size,
		Status:   downloadrecord.StatusImporting,
		SavePath: staged.Dir,
		Uploaded: true,
		BookID:   bookID,
		BookKind: recKind,
	}
	if o.Replace {
		p.ReplaceMode = downloadrecord.ReplaceModeAll
	}
	rec, err := s.db.CreateDownloadRecord(ctx, p)
	if err != nil {
		staged.Discard()
		return nil, otelx.RecordSpanError(
			span, fmt.Errorf("create upload record: %w", err))
	}

	slog.InfoContext(ctx, "uploaded book files",
		"book.kind", kind, "files", len(staged.Files))
	return rec, nil
}
