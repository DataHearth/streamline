package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// ScannedBookCandidate is a Hardcover match option surfaced for a scanned
// item. Stored as JSON in ImportScanBook.candidates; defined here so ent
// code-gen can reference it without an import cycle.
type ScannedBookCandidate struct {
	BookHardcoverID   uint32 `json:"book_hardcover_id"`
	AuthorHardcoverID uint32 `json:"author_hardcover_id"`
	Title             string `json:"title"`
	Author            string `json:"author"`
	Year              uint16 `json:"year,omitempty"`
}

// ImportScanBook is one detected book (ebook file-group or audiobook folder)
// in a book import scan.
type ImportScanBook struct{ ent.Schema }

func (ImportScanBook) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ImportScanBook) Fields() []ent.Field {
	return []ent.Field{
		// ebook: files sharing (dir, stem), e.g. the epub and mobi of one title.
		// audiobook: every audio file in one folder.
		field.JSON("file_paths", []string{}),
		field.Enum("slot").Values("ebook", "audiobook"),
		field.String("parsed_title").Optional(),
		field.String("parsed_author").Optional(),
		field.String("parsed_isbn").Optional(),
		field.Enum("classification").
			Values("confirmed", "ambiguous", "unmatched", "existing").
			Default("unmatched"),
		field.Uint32("book_hardcover_id").Optional(),
		field.Uint32("author_hardcover_id").Optional(),
		field.JSON("candidates", []ScannedBookCandidate{}).Optional(),
		field.Uint32("existing_book_id").Optional().Nillable(),

		field.Enum("decision").
			Values("pending", "accept", "skip").
			Default("pending"),
		field.Uint32("decision_book_hardcover_id").Optional(),

		field.Enum("outcome").
			Values("pending", "created", "failed").
			Default("pending"),
		field.String("outcome_message").Optional(),
		field.Uint32("created_book_id").Optional().Nillable(),
	}
}

func (ImportScanBook) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("scan", ImportScan.Type).Ref("books").Unique().Required(),
	}
}

func (ImportScanBook) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("classification"),
		index.Fields("decision"),
		index.Edges("scan"),
	}
}
