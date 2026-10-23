package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// BookContribution joins an Author to a book or a series with a role.
type BookContribution struct {
	ent.Schema
}

func (BookContribution) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (BookContribution) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("role").Values(
			"author", "writer", "artist", "colorist", "cover", "translator", "narrator",
		),
		field.String("language").Optional(),
		field.Uint8("order").Default(0),
	}
}

// Edges: author is always set; of book and series exactly one is. The two
// owners are optional because they are alternatives, so the invariant belongs
// to the writer, as with Credit and MediaEvent.
func (BookContribution) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("author", Author.Type).Ref("contributions").Unique().Required(),
		edge.From("book", Book.Type).Ref("contributions").Unique(),
		edge.From("series", BookSeries.Type).Ref("contributions").Unique(),
	}
}

func (BookContribution) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("author"),
		index.Edges("book"),
		index.Edges("series"),
	}
}
