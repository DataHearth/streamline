package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type MediaFile struct {
	ent.Schema
}

func (MediaFile) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (MediaFile) Fields() []ent.Field {
	return []ent.Field{
		field.String("path").NotEmpty(),
		field.Int64("size"),
		field.String("quality").Optional(),
		field.String("format").Optional(),
		field.String("release_group").Optional(),
		field.Enum("source").
			Values("wizard", "orphan", "auto").
			Default("auto"),
		field.Time("last_seen_at").
			Optional().
			Nillable().
			Default(time.Now),
		field.String("container").Optional(),
		field.Uint32("duration_seconds").Optional(),
		field.String("video_codec").Optional(),
		field.Uint16("width").Optional(),
		field.Uint16("height").Optional(),
		field.String("audio_codec").Optional(),
		field.Uint8("audio_channels").Optional(),
		field.Uint32("bitrate").Optional(),
		field.Time("probed_at").Optional().Nillable(),
	}
}

func (MediaFile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).Ref("media_files").Unique(),
		edge.From("episode", Episode.Type).Ref("media_files").Unique(),
	}
}
