package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type TranscodeJob struct{ ent.Schema }

func (TranscodeJob) Mixin() []ent.Mixin { return []ent.Mixin{mixins.UintID{}, mixin.Time{}} }

func (TranscodeJob) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("status").
			Values("queued", "running", "succeeded", "failed", "canceled", "rejected").
			Default("queued"),
		field.Uint8("attempts").Default(0),
		field.String("error").Optional(),
		field.Int64("size_before").Optional(),
		field.Int64("size_after").Optional(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("deferred_until").Optional().Nillable(),
	}
}

func (TranscodeJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("media_file", MediaFile.Type).
			Ref("transcode_jobs").
			Unique().
			Required(),
	}
}

func (TranscodeJob) Indexes() []ent.Index {
	return []ent.Index{index.Fields("status"), index.Edges("media_file")}
}
