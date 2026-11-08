package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// ExternalID is one other site's id for a library title: the cross-references
// the title's own provider keeps (TMDB for a movie, TVDB for a series). The
// provider's own id stays a column on the title (movies.tmdb_id,
// tv_shows.tvdb_id) — it is the title's identity, read on every path — and
// this table holds everything else, so a new source is an enum value rather
// than a column, and a title can be found from any of its ids through the
// (source, value) index.
//
// Every metadata write replaces a title's rows wholesale with what the
// provider answered, so a re-identified title never keeps the previous one's
// ids. value is stored as the site writes it: "tt0816692", "Q13417189",
// "1396" for a TMDB id.
type ExternalID struct{ ent.Schema }

func (ExternalID) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ExternalID) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("source").Values("imdb", "tmdb", "wikidata"),
		field.String("value").NotEmpty(),
	}
}

// Edges: of movie and tv_show exactly one is set, the Credit and MediaEvent
// pattern — they are alternatives, so the invariant belongs to the writer.
func (ExternalID) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).Ref("external_ids").Unique(),
		edge.From("tv_show", TVShow.Type).Ref("external_ids").Unique(),
	}
}

func (ExternalID) Indexes() []ent.Index {
	return []ent.Index{
		// One id per source per title. The other owner's column is NULL on
		// every row, and SQLite keeps NULLs distinct in a unique index, so each
		// index only ever constrains its own owner's rows. ent puts the field
		// first, so these cannot serve a read by owner alone.
		index.Fields("source").Edges("movie").Unique(),
		index.Fields("source").Edges("tv_show").Unique(),
		// The detail page's read of one title's ids, and the cascade delete.
		index.Edges("movie"),
		index.Edges("tv_show"),
		// Lookup by id. Not unique: TVDB can map two series to one TMDB entry.
		index.Fields("source", "value"),
	}
}
