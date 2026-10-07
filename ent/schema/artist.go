package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Artist struct {
	ent.Schema
}

func (Artist) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Artist) Fields() []ent.Field {
	return []ent.Field{
		field.String("mbid").NotEmpty().Unique(),
		field.String("name").NotEmpty(),
		field.String("sort_name").Optional(),
		field.String("overview").Optional(),
		field.Bool("monitored").Default(true),
		field.String("path").Optional(),
		field.String("quality_profile").Optional(),
		field.Time("last_refreshed_at").Optional().Nillable(),
	}
}

func (Artist) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("albums", Album.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
