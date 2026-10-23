package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// BookEdition is the best Hardcover edition for one (language, publisher,
// format) triple of a book. A slot points at one; a row a slot points at is
// never pruned, so its id is stable across refreshes.
type BookEdition struct {
	ent.Schema
}

func (BookEdition) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (BookEdition) Fields() []ent.Field {
	return []ent.Field{
		field.Uint32("hardcover_edition_id"),
		field.String("language").NotEmpty(),
		field.String("title").NotEmpty(),
		field.String("publisher").Default("").
			Comment("Empty when Hardcover has none; part of the winner key."),
		field.Uint16("year").Default(0),
		field.Enum("format").Values("ebook", "audiobook"),
		field.Bool("original").Default(false),
		field.Uint16("pages").Optional().Nillable(),
		field.Uint32("duration_seconds").Optional().Nillable(),
		field.String("narrator").Optional(),
		field.String("translator").Optional(),
		field.String("isbn_13").Optional(),
		field.String("asin").Optional(),
		field.Uint32("popularity").Default(0).
			Comment("Hardcover users_count; ranks winners."),
	}
}

func (BookEdition) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("book", Book.Type).Ref("editions").Unique().Required(),
	}
}

func (BookEdition) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("hardcover_edition_id").Edges("book").Unique(),
		index.Fields("language", "format", "publisher").Edges("book"),
		index.Fields("isbn_13"),
	}
}
