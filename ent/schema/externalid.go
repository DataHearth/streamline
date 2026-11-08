package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// ExternalID is one other site's id for a library row: the cross-references
// the row's own provider keeps (TMDB for a movie, TVDB for a series,
// MusicBrainz's url relations for an artist or an album). The provider's own
// id stays a column on the row (movies.tmdb_id, tv_shows.tvdb_id,
// artists.mbid, albums.mbid) — it is the row's identity, read on every path —
// and this table holds everything else, so a new source is an enum value rather
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
		// metadata.Source* names the same values.
		field.Enum("source").Values(
			"imdb", "tmdb", "wikidata",
			"discogs", "allmusic", "spotify", "deezer", "bandcamp", "lastfm",
			"rateyourmusic",
			"goodreads", "openlibrary", "google_books", "audible",
		),
		field.String("value").NotEmpty(),
		// The owner edges' columns, declared so a batch read (every album of
		// an artist) can group rows by owner without loading the owners.
		field.Uint32("movie_id").Optional(),
		field.Uint32("tv_show_id").Optional(),
		field.Uint32("artist_id").Optional(),
		field.Uint32("album_id").Optional(),
		field.Uint32("book_edition_id").Optional(),
	}
}

// Edges: of the five owners exactly one is set, the Credit and MediaEvent
// pattern — they are alternatives, so the invariant belongs to the writer.
func (ExternalID) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).Ref("external_ids").Unique().Field("movie_id"),
		edge.From("tv_show", TVShow.Type).Ref("external_ids").Unique().Field("tv_show_id"),
		edge.From("artist", Artist.Type).Ref("external_ids").Unique().Field("artist_id"),
		edge.From("album", Album.Type).Ref("external_ids").Unique().Field("album_id"),
		// A book's ids are its editions': Hardcover maps each edition to
		// Goodreads, Open Library and the others, never the book as a whole.
		edge.From("book_edition", BookEdition.Type).Ref("external_ids").
			Unique().Field("book_edition_id"),
	}
}

func (ExternalID) Indexes() []ent.Index {
	return []ent.Index{
		// One id per source per row. The other owners' columns are NULL on
		// every row, and SQLite keeps NULLs distinct in a unique index, so each
		// index only ever constrains its own owner's rows. The owner column
		// leads, so the same index serves the read of one row's ids, the batch
		// read and the cascade delete.
		index.Fields("movie_id", "source").Unique(),
		index.Fields("tv_show_id", "source").Unique(),
		index.Fields("artist_id", "source").Unique(),
		index.Fields("album_id", "source").Unique(),
		index.Fields("book_edition_id", "source").Unique(),
		// Lookup by id. Not unique: TVDB can map two series to one TMDB entry,
		// and MusicBrainz an artist's Spotify page to two of its entries.
		index.Fields("source", "value"),
	}
}
