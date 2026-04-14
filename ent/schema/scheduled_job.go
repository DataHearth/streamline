package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type ScheduledJob struct {
	ent.Schema
}

func (ScheduledJob) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}}
}

func (ScheduledJob) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique().Immutable().NotEmpty(),
		field.Bool("paused").Default(false),
		field.Time("last_started_at").Optional().Nillable(),
		field.Time("last_finished_at").Optional().Nillable(),
		field.Enum("last_status").
			Values("never", "success", "error", "skipped").
			Default("never"),
		field.String("last_error").Optional(),
		field.Uint32("last_duration_ms").Default(0),
	}
}

func (ScheduledJob) Edges() []ent.Edge {
	return nil
}
