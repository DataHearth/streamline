package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Person struct {
	ent.Schema
}

func (Person) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Person) Fields() []ent.Field {
	return []ent.Field{
		// tmdb_id and tvdb_id are provider-scoped and mutually exclusive in
		// practice: movie cast comes from TMDB, series cast from TVDB, so
		// normally exactly one is set and the other stays 0. 0 means "that
		// provider gave us no id for this person", not "person zero" — which is
		// why neither is a unique index: every provider-less person would
		// collide on it. Upserts de-duplicate on the non-zero id, falling back
		// to name.
		field.Uint32("tmdb_id").Optional().Default(0),
		field.Uint32("tvdb_id").Optional().Default(0),
		field.String("name").NotEmpty(),
		field.String("profile_url").Optional(),
	}
}

func (Person) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("credits", Credit.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Person) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tmdb_id"),
		index.Fields("tvdb_id"),
		index.Fields("name"),
	}
}
