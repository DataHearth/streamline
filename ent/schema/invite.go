package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Invite struct {
	ent.Schema
}

func (Invite) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Invite) Fields() []ent.Field {
	return []ent.Field{
		field.String("token_hash").Unique().NotEmpty(),
		field.String("email").Optional(),
		field.Enum("role").
			Values("admin", "member", "request_only").
			Default("member"),
		field.Time("expires_at"),
		field.Time("used_at").Optional().Nillable(),
	}
}

func (Invite) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("created_by", User.Type).Unique().Required(),
		edge.To("used_by", User.Type).Unique(),
	}
}
