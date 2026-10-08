package db

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
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
