package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Request struct {
	ent.Schema
}

func (Request) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Request) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("media_type").Values("movie", "tvshow"),
		field.Uint32("media_id").Comment("TMDB ID for movies, TVDB ID for TV shows"),
		field.String("title").NotEmpty(),
		field.Enum("status").
			Values("pending", "approved", "denied", "available").
			Default("pending"),
		field.String("reason").Optional().
			Comment("Admin-supplied reason, e.g. on denial."),
	}
}

func (Request) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("requester", User.Type).Ref("requests").Unique().Required(),
		edge.To("approved_by", User.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}
