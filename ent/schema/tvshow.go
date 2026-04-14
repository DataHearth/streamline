package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type TVShow struct {
	ent.Schema
}

func (TVShow) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (TVShow) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").NotEmpty(),
		field.String("original_title").Optional(),
		field.Uint16("year"),
		field.String("overview").Optional(),
		field.Enum("series_status").
			Values("continuing", "ended", "upcoming").
			Default("continuing"),
		field.Enum("type").
			Values("standard", "anime", "daily").
			Default("standard"),
		field.Bool("monitored").Default(true),
		field.Uint32("tvdb_id").Unique(),
		field.String("poster_path").Optional(),
		field.String("network").Optional(),
		field.String("creator").Optional(),
		field.Uint16("runtime").Optional().Default(0),
		field.Float("rating").Optional().Default(0),
		field.Strings("genres").Optional(),
		field.Time("last_refreshed_at").Optional().Nillable(),
		field.String("quality_profile").Optional(),
	}
}

func (TVShow) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("seasons", Season.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
