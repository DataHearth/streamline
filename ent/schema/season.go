package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Season struct {
	ent.Schema
}

func (Season) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Season) Fields() []ent.Field {
	return []ent.Field{
		field.Uint16("number"),
		field.String("name").Optional(),
		field.Bool("monitored").Default(true),
	}
}

func (Season) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tv_show", TVShow.Type).Ref("seasons").Unique().Required(),
		edge.To("episodes", Episode.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
