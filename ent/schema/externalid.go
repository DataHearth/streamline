package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// ExternalID is another site's id for a library row; the provider's own id
// stays a column. Each metadata write replaces the owner's rows.
type ExternalID struct{ ent.Schema }

func (ExternalID) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ExternalID) Fields() []ent.Field {
	return []ent.Field{
		// metadata.Source* names the same values.
		field.Enum("source").Values(
			"imdb", "tmdb", "wikidata",
			"discogs", "allmusic", "spotify", "deezer", "bandcamp", "lastfm",
			"rateyourmusic",
			"goodreads", "openlibrary", "google_books", "audible",
		),
		field.String("value").NotEmpty(),
		// Owner columns, read by the batch lookups.
		field.Uint32("movie_id").Optional(),
		field.Uint32("tv_show_id").Optional(),
		field.Uint32("artist_id").Optional(),
		field.Uint32("album_id").Optional(),
		field.Uint32("book_edition_id").Optional(),
	}
}

// Exactly one owner is set, by the writer (the Credit pattern).
func (ExternalID) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).Ref("external_ids").Unique().Field("movie_id"),
		edge.From("tv_show", TVShow.Type).Ref("external_ids").Unique().Field("tv_show_id"),
		edge.From("artist", Artist.Type).Ref("external_ids").Unique().Field("artist_id"),
		edge.From("album", Album.Type).Ref("external_ids").Unique().Field("album_id"),
		// Hardcover maps editions, not books.
		edge.From("book_edition", BookEdition.Type).Ref("external_ids").
			Unique().Field("book_edition_id"),
	}
}

func (ExternalID) Indexes() []ent.Index {
	return []ent.Index{
		// One id per source per owner; NULLs stay distinct. The owner leads, so
		// these also serve reads and cascades.
		index.Fields("movie_id", "source").Unique(),
		index.Fields("tv_show_id", "source").Unique(),
		index.Fields("artist_id", "source").Unique(),
		index.Fields("album_id", "source").Unique(),
		index.Fields("book_edition_id", "source").Unique(),
		// Lookup by id; not unique (a TMDB id can map two series).
		index.Fields("source", "value"),
	}
}
