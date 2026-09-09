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
				"download_cancelled",
				"grab_widened",
				"imported",
				"import_failed",
				"import_held_for_review",
				"drift_detected",
				"drift_confirmed",
				"searched",
				"added",
				"file_renamed",
				"file_removed",
				"reidentified",
				"metadata_refreshed",
				"monitoring_changed",
				"request_approved",
				"transcode_completed",
				"transcode_failed",
				"transcode_rejected",
			),
		field.JSON("payload", map[string]any{}).Optional(),
	}
}

// Edges: exactly one owner is set. Movie and episode carry the per-file
// lifecycle (grab, import, drift, transcode, rename); tv_show carries what
// belongs to the series as a whole rather than to any one episode — a search
// issued at series or season scope, and the show-level actions (added,
// reidentified, metadata_refreshed, monitoring_changed). The owner is
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
