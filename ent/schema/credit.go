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
		// job is empty for an acting credit and names the crew role otherwise
		// ("Director", "Screenplay"); department is the provider's grouping of
		// it ("Directing", "Writing"), empty where none is known.
		field.String("job").Optional(),
		field.String("department").Optional(),
	}
}

// Edges: person is always set; of movie, tv_show and episode exactly one is.
// The owners are optional rather than required because they are alternatives
// — the same reason MediaEvent leaves its owner edges optional — so the
// invariant belongs to the writer, not the column. An episode credit is never
// also a tv_show one: replacing a series' credits must leave its episodes'.
func (Credit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("person", Person.Type).Ref("credits").Unique().Required(),
		edge.From("movie", Movie.Type).Ref("credits").Unique(),
		edge.From("tv_show", TVShow.Type).Ref("credits").Unique(),
		edge.From("episode", Episode.Type).Ref("credits").Unique(),
	}
}

func (Credit) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("person"),
		index.Edges("movie"),
		index.Edges("tv_show"),
		index.Edges("episode"),
	}
}
