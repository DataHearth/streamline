package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// Bookmark is a listener's saved position in one track, the Subsonic
// createBookmark: one per user and track.
type Bookmark struct {
	ent.Schema
}

func (Bookmark) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Bookmark) Fields() []ent.Field {
	return []ent.Field{
		// Milliseconds into the track.
		field.Int64("position").Min(0),
		field.String("comment").Optional(),
		field.Uint32("user_id"),
		field.Uint32("track_id"),
	}
}

func (Bookmark) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("bookmarks").
			Unique().
			Required().
			Field("user_id"),
		edge.From("track", Track.Type).
			Ref("bookmarks").
			Unique().
			Required().
			Field("track_id"),
	}
}

func (Bookmark) Indexes() []ent.Index {
	return []ent.Index{index.Edges("user", "track").Unique()}
}
