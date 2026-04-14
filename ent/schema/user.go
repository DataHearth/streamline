package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type User struct {
	ent.Schema
}

func (User) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("email").Unique().NotEmpty(),
		field.String("password_hash").Optional(),
		field.Enum("role").
			Values("admin", "member", "request_only").
			Default("member"),
		field.Enum("auth_method").Values("local", "oidc", "both").Default("local"),
		field.String("display_name").Optional(),
		field.Uint8("failed_login_count").Default(0),
		field.Time("last_failed_login_at").Optional().Nillable(),
		field.Time("locked_until").Optional().Nillable(),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("api_keys", ApiKey.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("oidc_identities", OIDCIdentity.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("requests", Request.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("sessions", Session.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
