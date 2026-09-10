package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Credit struct{ ent.Schema }

func (Credit) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Credit) Fields() []ent.Field {
	return []ent.Field{
		field.String("character").Optional(),
		field.Uint8("order").Optional().Default(0),
	}
}

// Edges: person is always set; of movie and tv_show exactly one is. The two
// owners are optional rather than required because they are alternatives — the
// same reason MediaEvent leaves its three owner edges optional — so the
// invariant belongs to the writer, not the column.
func (Credit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("person", Person.Type).Ref("credits").Unique().Required(),
		edge.From("movie", Movie.Type).Ref("credits").Unique(),
		edge.From("tv_show", TVShow.Type).Ref("credits").Unique(),
	}
}

func (Credit) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("person"),
		index.Edges("movie"),
		index.Edges("tv_show"),
	}
}
