package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type OIDCIdentity struct {
	ent.Schema
}

func (OIDCIdentity) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (OIDCIdentity) Fields() []ent.Field {
	return []ent.Field{
		field.String("provider").NotEmpty(),
		field.String("subject").NotEmpty(),
		field.String("email").Optional(),
	}
}

func (OIDCIdentity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("oidc_identities").Unique().Required(),
	}
}

func (OIDCIdentity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider", "subject").Unique(),
	}
}
