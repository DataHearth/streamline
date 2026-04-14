package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type MovieEvent struct{ ent.Schema }

func (MovieEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (MovieEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("type").
			Values(
				"grabbed",
				"download_completed",
				"download_failed",
				"imported",
				"import_failed",
				"drift_detected",
				"drift_confirmed",
				"searched",
			),
		field.JSON("payload", map[string]any{}).Optional(),
	}
}

func (MovieEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).
			Ref("events").
			Unique().
			Required(),
	}
}

func (MovieEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("create_time"),
		index.Fields("type", "create_time"),
		index.Edges("movie").Fields("create_time"),
	}
}
