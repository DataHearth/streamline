package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// Share is a public link to some music, served without authentication at
// /share/{token}. items keeps the Subsonic ids the share was created from
// (tracks, albums, a playlist) and is expanded when the link is opened, so a
// shared album holds what is on disk at that moment.
type Share struct {
	ent.Schema
}

func (Share) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Share) Fields() []ent.Field {
	return []ent.Field{
		// The token is the only credential a visitor holds.
		field.String("token").NotEmpty().Sensitive(),
		field.String("description").Optional(),
		field.JSON("items", []string{}),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("last_visited_at").Optional().Nillable(),
		field.Uint32("visit_count").Default(0),
	}
}

func (Share) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("shares").Unique().Required(),
	}
}

func (Share) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token").Unique(),
		index.Edges("owner"),
	}
}
