package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// Session represents an active authenticated session for a user.
// Each JWT carries a jti (JWT ID) that maps 1:1 to a row here so
// the server can revoke individual sessions without rotating the
// signing secret.
type Session struct {
	ent.Schema
}

func (Session) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("jti").Unique().NotEmpty(),
		field.Time("expires_at"),
		field.Time("revoked_at").Optional().Nillable(),
		field.Time("last_seen_at").Optional().Nillable(),
		field.String("ip").Optional(),
		field.String("user_agent").Optional().MaxLen(512),
	}
}

func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("sessions").
			Unique().
			Required(),
	}
}
