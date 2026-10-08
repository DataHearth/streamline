package db

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/schema"
)

type CreateImportScanBookParams struct {
	FilePaths         []string
	Slot              entimportscanbook.Slot
	ParsedTitle       string
	ParsedAuthor      string
	ParsedISBN        string
	Classification    entimportscanbook.Classification
	BookHardcoverID   uint32
	AuthorHardcoverID uint32
	Candidates        []schema.ScannedBookCandidate
	ExistingBookID    *uint32
}

type UpdateScanBookOutcomeOpts struct {
	Message       string
	CreatedBookID uint32
}

func (db *DB) BulkCreateImportScanBooks(
	ctx context.Context, scanID uint32, books []CreateImportScanBookParams,
) error {
	if len(books) == 0 {
		return nil
	}
	creates := make([]*ent.ImportScanBookCreate, 0, len(books))
	for _, p := range books {
		c := db.client.ImportScanBook.Create().
			SetScanID(scanID).
			SetFilePaths(p.FilePaths).
			SetSlot(p.Slot).
			SetParsedTitle(p.ParsedTitle).
			SetParsedAuthor(p.ParsedAuthor).
			SetParsedIsbn(p.ParsedISBN).
			SetClassification(p.Classification).
			SetBookHardcoverID(p.BookHardcoverID).
			SetAuthorHardcoverID(p.AuthorHardcoverID).
			SetNillableExistingBookID(p.ExistingBookID)
		if len(p.Candidates) > 0 {
			c.SetCandidates(p.Candidates)
		}
		creates = append(creates, c)
	}
	if _, err := db.client.ImportScanBook.CreateBulk(creates...).
		Save(ctx); err != nil {
		return fmt.Errorf("bulk create import scan books: %w", err)
	}
	return nil
}

// BookHardcoverIndex maps hardcover_id to book id for books already holding a
// file of the given slot, so a scanned candidate for that slot is flagged
// existing instead of adopted twice.
func (db *DB) BookHardcoverIndex(
	ctx context.Context, kind string,
) (map[uint32]uint32, error) {
	var rows []struct {
		ID          uint32 `json:"id"`
		HardcoverID uint32 `json:"hardcover_id"`
	}
	err := db.client.Book.Query().
		Where(book.HasMediaFilesWith(
			entmediafile.BookKindEQ(entmediafile.BookKind(kind)),
		)).
		Select(book.FieldID, book.FieldHardcoverID).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("book hardcover index: %w", err)
	}
	out := make(map[uint32]uint32, len(rows))
	for _, r := range rows {
		out[r.HardcoverID] = r.ID
	}
	return out, nil
}

// ListImportScanBooksForCommit returns the books to adopt when a book scan is
// committed: everything not explicitly skipped that was either accepted by the
// reviewer or auto-matched (confirmed / existing).
func (db *DB) ListImportScanBooksForCommit(
	ctx context.Context, scanID uint32,
) ([]*ent.ImportScanBook, error) {
	return db.client.ImportScanBook.Query().
		Where(
			entimportscanbook.HasScanWith(entimportscan.ID(scanID)),
			entimportscanbook.DecisionNEQ(entimportscanbook.DecisionSkip),
			entimportscanbook.Or(
				entimportscanbook.DecisionEQ(entimportscanbook.DecisionAccept),
				entimportscanbook.ClassificationIn(
					entimportscanbook.ClassificationConfirmed,
					entimportscanbook.ClassificationExisting,
				),
			),
		).
		All(ctx)
}

func (db *DB) BulkUpdateImportScanBookDecisions(
	ctx context.Context,
	scanID uint32,
	decision entimportscanbook.Decision,
	classification entimportscanbook.Classification,
	ids []uint32,
) (int, error) {
	u := db.client.ImportScanBook.Update().
		Where(entimportscanbook.HasScanWith(entimportscan.ID(scanID))).
		SetDecision(decision)
	if classification != "" {
		u = u.Where(entimportscanbook.ClassificationEQ(classification))
	}
	if len(ids) > 0 {
		u = u.Where(entimportscanbook.IDIn(ids...))
	}
	n, err := u.Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("bulk update import scan book decisions: %w", err)
	}
	return n, nil
}

func (db *DB) UpdateImportScanBookOutcome(
	ctx context.Context, id uint32,
	outcome entimportscanbook.Outcome, opts UpdateScanBookOutcomeOpts,
) error {
	u := db.client.ImportScanBook.UpdateOneID(id).SetOutcome(outcome)
	if opts.Message != "" {
		u = u.SetOutcomeMessage(opts.Message)
	}
	if opts.CreatedBookID != 0 {
		u = u.SetCreatedBookID(opts.CreatedBookID)
	}
	return u.Exec(ctx)
}

// MarkBookSlotAvailable monitors the slot and marks it available. SetBookSlot
// cannot: it only moves a slot between skipped and wanted, and an adopted file
// is neither.
func (db *DB) MarkBookSlotAvailable(
	ctx context.Context, bookID uint32, kind string,
) error {
	u := db.client.Book.UpdateOneID(bookID)
	switch entmediafile.BookKind(kind) {
	case entmediafile.BookKindEbook:
		u = u.SetEbookMonitored(true).SetEbookStatus(book.EbookStatusAvailable)
	case entmediafile.BookKindAudiobook:
		u = u.SetAudiobookMonitored(true).
			SetAudiobookStatus(book.AudiobookStatusAvailable)
	default:
		return fmt.Errorf("mark book slot available: unknown kind %q", kind)
	}
	return u.Exec(ctx)
}
