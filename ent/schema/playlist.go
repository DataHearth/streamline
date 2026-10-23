package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// Playlist is a Subsonic playlist. Its entries are an ordered list of track
// ids, duplicates allowed, read and rewritten whole; an id whose track is gone
// is skipped on read rather than chased on delete.
type Playlist struct {
	ent.Schema
}

func (Playlist) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Playlist) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("comment").Optional(),
		field.Bool("public").Default(false),
		field.JSON("track_ids", []uint32{}).Optional(),
	}
}

func (Playlist) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("playlists").Unique().Required(),
	}
}

func (Playlist) Indexes() []ent.Index {
	return []ent.Index{index.Edges("owner")}
}
