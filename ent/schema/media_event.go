package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type MediaEvent struct{ ent.Schema }

func (MediaEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (MediaEvent) Fields() []ent.Field {
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

// Edges: exactly one owner is set. Movie and episode carry the per-item
// lifecycle (grab, import, drift); tv_show carries only the searches issued at
// series or season scope, which belong to no single episode. The owner is
// optional rather than required because the three are alternatives — the
// invariant is enforced by events.Record, which refuses a row without one.
func (MediaEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).Ref("events").Unique(),
		edge.From("episode", Episode.Type).Ref("events").Unique(),
		edge.From("tv_show", TVShow.Type).Ref("events").Unique(),
	}
}

func (MediaEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("create_time"),
		index.Fields("type", "create_time"),
		index.Edges("movie").Fields("create_time"),
		index.Edges("episode").Fields("create_time"),
		index.Edges("tv_show").Fields("create_time"),
	}
}
