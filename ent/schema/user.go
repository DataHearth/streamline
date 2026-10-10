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
		field.String("password_hash").Optional().Sensitive(),
		field.Enum("role").
			Values("admin", "member", "request_only").
			Default("member"),
		field.Enum("auth_method").Values("local", "oidc", "both").Default("local"),
		field.String("display_name").Optional(),
		// Recoverable on purpose: the Subsonic token scheme (t=md5(pass+salt))
		// needs the plaintext. Never valid for web or REST auth.
		field.String("subsonic_password").Optional().Sensitive(),
		field.Time("subsonic_created_at").Optional().Nillable(),
		field.Time("subsonic_last_used_at").Optional().Nillable(),
		field.String("subsonic_last_client").Optional().MaxLen(64),
		// SHA-256 hex of the token, never the token: POST shows it once. Used
		// as the HTTP Basic password for the OPDS catalog; never valid for web
		// or REST auth.
		field.String("opds_token").Optional().Sensitive(),
		field.Time("opds_created_at").Optional().Nillable(),
		field.Time("opds_last_used_at").Optional().Nillable(),
		field.String("opds_last_client").Optional().MaxLen(64),
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
