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
	}
}
