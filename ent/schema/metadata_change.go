package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// MetadataChange is a provider change a refresh would not apply on its own,
// because it touches what an item's files are matched against or takes the
// item out of the library. It waits for an admin: `pending` until decided,
// `kept` once they chose to keep the item as it is. A kept row is the memory
// of that choice: the same change (same fingerprint) is not asked again, a
// different one is.
type MetadataChange struct{ ent.Schema }

func (MetadataChange) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (MetadataChange) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("kind").
			Values(
				"edition_removed",
				"edition_merged",
				"edition_preferred",
				"tracks_unlisted",
				"album_removed",
				"album_merged",
				"artist_removed",
				"artist_merged",
				"series_removed",
				"episodes_unlisted",
				"movie_removed",
				"book_removed",
			),
		field.Enum("state").Values("pending", "kept").Default("pending"),
		field.String("fingerprint").Default(""),
		field.JSON("payload", map[string]any{}).Optional(),
	}
}

// Edges: exactly one owner is set, enforced by db.ReportChange.
func (MetadataChange) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("artist", Artist.Type).Ref("metadata_changes").Unique(),
		edge.From("album", Album.Type).Ref("metadata_changes").Unique(),
		edge.From("tv_show", TVShow.Type).Ref("metadata_changes").Unique(),
		edge.From("movie", Movie.Type).Ref("metadata_changes").Unique(),
		edge.From("book", Book.Type).Ref("metadata_changes").Unique(),
	}
}

// One row per owner and kind. The owner columns are NULL on every row but
// one, and SQLite never counts two NULLs as equal, so each index only binds
// the rows of its own owner type.
func (MetadataChange) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kind").Edges("artist").Unique(),
		index.Fields("kind").Edges("album").Unique(),
		index.Fields("kind").Edges("tv_show").Unique(),
		index.Fields("kind").Edges("movie").Unique(),
		index.Fields("kind").Edges("book").Unique(),
		index.Fields("state"),
	}
}
