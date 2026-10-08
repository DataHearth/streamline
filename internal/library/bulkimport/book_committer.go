package bulkimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book"
)

// runCommitBooks adopts every reviewed book in a book scan in place: it
// resolves (or creates) the author and links the files already on disk to the
// matched book. Sequential, with one author lookup per author for the run — a
// 300-book library must not re-fetch the same author for each of its books.
func (s *Service) runCommitBooks(ctx context.Context, scan *ent.ImportScan) {
	ctx, span := tracer.Start(ctx, "bulkimport.run_commit_books",
		trace.WithAttributes(attribute.Int64("scan.id", int64(scan.ID))))
	defer span.End()

	defer func() {
		if r := recover(); r != nil {
			s.markScanFailed(ctx, scan.ID, fmt.Sprintf("panic: %v", r))
		}
	}()

	books, err := s.store.ListImportScanBooksForCommit(ctx, scan.ID)
	if err != nil {
		s.markScanFailed(ctx, scan.ID, err.Error())
		return
	}

	authors := map[uint32]*ent.Author{}
	var success, failed uint32
	for _, sb := range books {
		outcome, msg, createdID := s.commitBook(ctx, sb, authors)
		if uerr := s.store.UpdateImportScanBookOutcome(
			ctx,
			sb.ID,
			outcome,
			db.UpdateScanBookOutcomeOpts{Message: msg, CreatedBookID: createdID},
		); uerr != nil {
			slog.ErrorContext(ctx, "book commit: failed to record book outcome",
				"scan.id", scan.ID, "scan_book.id", sb.ID, "error", uerr)
		}
		switch outcome {
		case entimportscanbook.OutcomeCreated:
			success++
		case entimportscanbook.OutcomeFailed:
			failed++
		}
	}

	committedAt := time.Now()
	if err := s.store.UpdateImportScanStatus(
		ctx,
		scan.ID,
		entimportscan.StatusCompleted,
		db.UpdateScanStatusOpts{
			CommittedAt:        &committedAt,
			CommitSuccessCount: &success,
			CommitFailedCount:  &failed,
		},
	); err != nil {
		slog.ErrorContext(ctx, "book commit: failed to flip scan to completed",
			"scan.id", scan.ID, "error", err)
	}
	slog.InfoContext(ctx, "book commit finished",
		"scan.id", scan.ID,
		"commit.success_count", success,
		"commit.failed_count", failed)
	countCommit(ctx, "book", "success", int64(success))
	countCommit(ctx, "book", "failed", int64(failed))
}

func (s *Service) commitBook(
	ctx context.Context,
	sc *ent.ImportScanBook,
	authors map[uint32]*ent.Author,
) (entimportscanbook.Outcome, string, uint32) {
	ctx, span := tracer.Start(ctx, "bulkimport.commit_book",
		trace.WithAttributes(attribute.Int64("scan_book.id", int64(sc.ID))))
	defer span.End()

	target, err := s.resolveBook(ctx, sc, authors)
	if err != nil {
		return commitBookFail("resolve book", err, 0)
	}

	kind := entmediafile.BookKind(sc.Slot)
	attached := 0
	for _, p := range sc.FilePaths {
		info, serr := os.Stat(p)
		if serr != nil {
			slog.WarnContext(ctx, "book commit: file vanished, skipping",
				"path", p, "error", serr)
			continue
		}
		if slices.ContainsFunc(target.Edges.MediaFiles, func(f *ent.MediaFile) bool {
			return f.Path == p
		}) {
			attached++
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
		if _, cerr := s.store.CreateMediaFile(ctx, db.CreateMediaFileParams{
			BookID:   target.ID,
			BookKind: kind,
			Path:     p,
			Size:     info.Size(),
			Quality:  ext,
			Format:   ext,
			Source:   entmediafile.SourceOrphan,
		}); cerr != nil {
			return commitBookFail("record file "+p, cerr, target.ID)
		}
		attached++
	}
	if attached == 0 {
		return commitBookFail(
			"attach files",
			errors.New("no file could be attached"),
			target.ID,
		)
	}

	if err := s.store.MarkBookSlotAvailable(
		ctx,
		target.ID,
		string(sc.Slot),
	); err != nil {
		return commitBookFail("mark slot available", err, target.ID)
	}
	return entimportscanbook.OutcomeCreated, "", target.ID
}

// resolveBook returns the eager-loaded book to adopt into. The reviewer's pick
// wins over the classifier's match, and a scan-time existing book is only used
// when the reviewer made no pick.
func (s *Service) resolveBook(
	ctx context.Context,
	sc *ent.ImportScanBook,
	authors map[uint32]*ent.Author,
) (*ent.Book, error) {
	bookHC := sc.BookHardcoverID
	picked := sc.DecisionBookHardcoverID != 0
	if picked {
		bookHC = sc.DecisionBookHardcoverID
	}
	if !picked && sc.ExistingBookID != nil {
		return s.store.FindBookByID(ctx, *sc.ExistingBookID)
	}
	if bookHC == 0 {
		return nil, errors.New("no hardcover match to adopt")
	}

	author, err := s.resolveBookAuthor(ctx, sc, bookHC, authors)
	if err != nil {
		return nil, err
	}
	for _, b := range author.Edges.Books {
		if b.HardcoverID == bookHC {
			return s.store.FindBookByID(ctx, b.ID)
		}
	}
	return nil, fmt.Errorf(
		"hardcover book %d is missing from author %q's bibliography",
		bookHC, author.Name,
	)
}

func (s *Service) resolveBookAuthor(
	ctx context.Context,
	sc *ent.ImportScanBook,
	bookHC uint32,
	authors map[uint32]*ent.Author,
) (*ent.Author, error) {
	authorHC := knownAuthorID(sc, bookHC)
	if authorHC == 0 {
		// Search hits never carry the author id.
		details, err := s.bookmeta.GetBook(ctx, bookHC)
		if err != nil {
			return nil, fmt.Errorf("get book: %w", err)
		}
		authorHC = details.AuthorHardcover
	}
	if cached, ok := authors[authorHC]; ok {
		return cached, nil
	}

	row, err := s.store.FindAuthorByHardcoverID(ctx, authorHC)
	if err != nil {
		return nil, fmt.Errorf("look up author: %w", err)
	}
	if row == nil {
		row, err = s.bookAdder.Add(ctx, book.AddParams{
			HardcoverID:   authorHC,
			Monitored:     true,
			MonitorPolicy: "none",
		})
		if errors.Is(err, book.ErrAuthorExists) {
			row, err = s.store.FindAuthorByHardcoverID(ctx, authorHC)
			if err == nil && row == nil {
				err = fmt.Errorf(
					"author %d vanished after a concurrent add",
					authorHC,
				)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("add author: %w", err)
		}
	}
	full, err := s.store.FindAuthorByID(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("load author: %w", err)
	}
	authors[authorHC] = full
	return full, nil
}

// knownAuthorID is the author id recorded at scan time for this exact book, or
// 0. A reviewer's pick of a different book invalidates the scan's author id.
func knownAuthorID(sc *ent.ImportScanBook, bookHC uint32) uint32 {
	if bookHC == sc.BookHardcoverID && sc.AuthorHardcoverID != 0 {
		return sc.AuthorHardcoverID
	}
	for _, c := range sc.Candidates {
		if c.BookHardcoverID == bookHC && c.AuthorHardcoverID != 0 {
			return c.AuthorHardcoverID
		}
	}
	return 0
}

func commitBookFail(
	label string, err error, bookID uint32,
) (entimportscanbook.Outcome, string, uint32) {
	return entimportscanbook.OutcomeFailed, fmt.Sprintf("%s: %v", label, err), bookID
}
