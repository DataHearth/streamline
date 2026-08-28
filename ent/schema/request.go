package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
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
		field.String("quality_profile").Optional().
			Comment("Profile the requester asked for; empty means no preference."),
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

func (Request) Indexes() []ent.Index {
	return []ent.Index{
		// Partial on purpose: the uniqueness only holds over the statuses
		// FindActiveRequest treats as active, so a denied or superseded
		// request never blocks a legitimate re-request of the same media.
		index.Fields("media_type", "media_id").
			Unique().
			Annotations(entsql.IndexWhere(
				"status IN ('pending', 'approved', 'available')",
			)),
		index.Edges("requester"),
		index.Edges("approved_by"),
	}
}
