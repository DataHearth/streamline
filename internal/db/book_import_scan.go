package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// ErrImportScanBookNotFound is returned when the scan-scoped UPDATE matched no
// row because the book id is unknown or belongs to a different scan.
var ErrImportScanBookNotFound = errors.New("import scan book not found")

type ListImportScanBooksParams struct {
	ScanID         uint32
	Classification entimportscanbook.Classification // empty = all
	Query          string
	Offset, Limit  uint32
}

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

func (db *DB) ListImportScanBooks(
	ctx context.Context, p ListImportScanBooksParams,
) ([]*ent.ImportScanBook, uint32, error) {
	q := db.client.ImportScanBook.Query().
		Where(entimportscanbook.HasScanWith(entimportscan.ID(p.ScanID)))
	if p.Classification != "" {
		q = q.Where(entimportscanbook.ClassificationEQ(p.Classification))
	}
	if p.Query != "" {
		q = q.Where(entimportscanbook.Or(
			entimportscanbook.ParsedTitleContainsFold(p.Query),
			entimportscanbook.ParsedAuthorContainsFold(p.Query),
		))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count import scan books: %w", err)
	}
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	rows, err := q.Order(
		ent.Asc(entimportscanbook.FieldParsedTitle),
		ent.Asc(entimportscanbook.FieldID),
	).Offset(int(p.Offset)).Limit(int(limit)).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list import scan books: %w", err)
	}
	return rows, numeric.SaturateU32(total), nil
}

func (db *DB) FindImportScanBook(
	ctx context.Context, scanID, bookID uint32,
) (*ent.ImportScanBook, error) {
	row, err := db.client.ImportScanBook.Query().
		Where(
			entimportscanbook.ID(bookID),
			entimportscanbook.HasScanWith(entimportscan.ID(scanID)),
		).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("find import scan book: %w", err)
	}
	return row, nil
}

// UpdateImportScanBookDecision scopes by scan in the UPDATE predicate so a
// book id from another scan matches nothing instead of being mutated.
func (db *DB) UpdateImportScanBookDecision(
	ctx context.Context, scanID, bookID uint32,
	decision entimportscanbook.Decision, hardcoverID *uint32,
) error {
	u := db.client.ImportScanBook.Update().
		Where(
			entimportscanbook.ID(bookID),
			entimportscanbook.HasScanWith(entimportscan.ID(scanID)),
		).
		SetDecision(decision)
	if hardcoverID != nil {
		u = u.SetDecisionBookHardcoverID(*hardcoverID)
	} else {
		u = u.ClearDecisionBookHardcoverID()
	}
	n, err := u.Save(ctx)
	if err != nil {
		return fmt.Errorf("update import scan book decision: %w", err)
	}
	if n == 0 {
		return ErrImportScanBookNotFound
	}
	return nil
}
