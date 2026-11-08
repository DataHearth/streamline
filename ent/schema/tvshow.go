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

type TVShow struct {
	ent.Schema
}

func (TVShow) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (TVShow) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").NotEmpty(),
		field.String("original_title").Optional(),
		field.Uint16("year"),
		// First air date, as TVDB reports it — the series counterpart to
		// Movie.release_date. Absent for an announced show with no date set,
		// which is why `year` stays and stays required.
		field.Time("first_aired").Optional().Nillable(),
		field.String("overview").Optional(),
		field.Enum("series_status").
			Values("continuing", "ended").
			Default("continuing"),
		field.Enum("type").
			Values("standard", "anime", "daily").
			Default("standard"),
		field.Bool("monitored").Default(true),
		field.Uint32("tvdb_id").Unique(),
		// Cross-references from TVDB's remoteIds, refreshed with the rest of
		// the metadata. tmdb_id is not unique: TVDB maps the ids, and two
		// series pointing at one TMDB entry is TVDB's data, not a conflict
		// to refuse an add over. 0 / empty when TVDB lists none.
		field.Uint32("tmdb_id").Optional(),
		field.String("imdb_id").Optional(),
		field.String("wikidata_id").Optional(),
		field.String("poster_path").Optional(),
		field.String("network").Optional(),
		field.String("creator").Optional(),
		field.Uint16("runtime").Optional().Default(0),
		field.Float("rating").Optional().Default(0),
		field.Strings("genres").Optional(),
		// TVDB's alternate names, every translation included: a release is as
		// likely to carry the English or romaji title as the library's.
		field.Strings("aliases").Optional(),
		field.Time("last_refreshed_at").Optional().Nillable(),
		field.String("quality_profile").Optional(),
	}
}

func (TVShow) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("seasons", Season.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("events", MediaEvent.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("credits", Credit.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (TVShow) Indexes() []ent.Index {
	// Mirrors Movie: create_time orders the list page, series_status filters
	// it. FilterTVShows pushes both into SQL, so both are hit per request.
	return []ent.Index{
		index.Fields("create_time"),
		index.Fields("series_status"),
	}
}
