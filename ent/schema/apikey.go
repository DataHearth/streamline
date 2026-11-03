package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type ApiKey struct {
	ent.Schema
}

func (ApiKey) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ApiKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("key_hash").NotEmpty().Sensitive(),
		field.Time("last_used_at").Optional().Nillable(),
		// scope bounds what the key may do regardless of its owner's role: a
		// read key is refused every unsafe method in the auth middleware,
		// before the RBAC table sees the request. Existing keys migrate to
		// full, which is what they could already do.
		field.Enum("scope").Values("read", "full").Default("full"),
		// expires_at is when the key stops authenticating; nil never expires.
		// An expired key is refused exactly like an unknown one and its row is
		// dropped by the purge-api-keys job.
		field.Time("expires_at").Optional().Nillable(),
	}
}

func (ApiKey) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("api_keys").Unique().Required(),
	}
}

func (ApiKey) Indexes() []ent.Index {
	return []ent.Index{
		// Every X-API-Key request looks a row up by hash; without this it is a
		// full scan of the table per request. Unique because two keys hashing
		// the same is a collision the schema should refuse, not tolerate.
		index.Fields("key_hash").Unique(),
		index.Edges("owner"),
		// The purge job deletes by expiry.
		index.Fields("expires_at"),
	}
}
