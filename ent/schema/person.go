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
		field.String("biography").Optional(),
		// known_for is TMDB's known_for_department ("Acting", "Writing"). TVDB
		// has no equivalent field, so a TVDB-sourced person leaves this empty
		// — that is expected, not a fetch failure.
		field.String("known_for").Optional(),
		// birthday/deathday stay strings, not time.Time: providers return
		// partial or malformed dates ("1984", ""), and a date column would
		// force a lossy parse at ingest. Kept as provider-format YYYY-MM-DD.
		field.String("birthday").Optional(),
		field.String("deathday").Optional(),
		field.String("place_of_birth").Optional(),
		field.String("imdb_id").Optional(),
		field.String("instagram_id").Optional(),
		field.String("twitter_id").Optional(),
		// details_fetched_at marks whether the provider person-detail call has
		// ever succeeded for this row. A person can exist from cast ingest
		// alone with none of the fields above filled; nil here is how a later
		// refresh knows to skip people already enriched rather than re-fetch
		// everyone on every pass.
		field.Time("details_fetched_at").Optional().Nillable(),
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
