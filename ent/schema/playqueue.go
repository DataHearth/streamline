package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// PlayQueue is the queue a Subsonic client saves so another device can pick
// up where it left off: one per user, overwritten whole by every save.
type PlayQueue struct {
	ent.Schema
}

func (PlayQueue) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (PlayQueue) Fields() []ent.Field {
	return []ent.Field{
		field.JSON("track_ids", []uint32{}).Optional(),
		// Index into track_ids of the playing entry; an index rather than a
		// track id because the queue may hold the same track twice.
		field.Int("current").Default(0).Min(0),
		// Milliseconds into the current entry.
		field.Int64("position").Default(0).Min(0),
		// The client name (Subsonic c parameter) that saved it.
		field.String("changed_by").Optional().MaxLen(64),
	}
}

func (PlayQueue) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("play_queue").Unique().Required(),
	}
}
