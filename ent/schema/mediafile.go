package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
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
		// Set on the first drift tick that cannot stat the file, cleared the
		// moment it is seen again. last_seen_at alone cannot tell the first
		// missing tick from the fifth — it just stops advancing — and that
		// edge is what the drift_detected event fires on.
		field.Time("missing_since").Optional().Nillable(),
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

// SQLite does not index a foreign key on its own, so every episode->file
// lookup was a full media_files scan. The series list runs one per episode row
// of the page: 3.9s for 20 shows on a real library.
func (MediaFile) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("episode"),
		index.Edges("movie"),
		// Partial: the media-probe backfill asks for the oldest rows that were
		// never probed, so once the backfill drains, the index is empty and
		// the 15-minute job stops scanning the whole table forever.
		index.Fields("probed_at").
			Annotations(entsql.IndexWhere("probed_at IS NULL")),
	}
}
