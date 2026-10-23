package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// MusicAnnotation is one listener's own state on an artist, album or track:
// the star, the 1-5 rating and the play count the Subsonic API reads and
// writes. A scrobble counts on the track, its album and its artist at once,
// so album and artist lists sort on their own row rather than an aggregate.
type MusicAnnotation struct {
	ent.Schema
}

func (MusicAnnotation) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (MusicAnnotation) Fields() []ent.Field {
	return []ent.Field{
		field.Time("starred_at").Optional().Nillable(),
		// 0 is unrated.
		field.Uint8("rating").Default(0).Max(5),
		field.Uint32("play_count").Default(0),
		field.Time("played_at").Optional().Nillable(),
		// Edge fields, so a batch read can key rows on their target without
		// loading it.
		field.Uint32("user_id"),
		field.Uint32("artist_id").Optional().Nillable(),
		field.Uint32("album_id").Optional().Nillable(),
		field.Uint32("track_id").Optional().Nillable(),
	}
}

// Edges: of artist, album and track exactly one is set; the invariant belongs
// to the writer, as it does for MusicCredit.
func (MusicAnnotation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("music_annotations").
			Unique().
			Required().
			Field("user_id"),
		edge.From("artist", Artist.Type).
			Ref("annotations").
			Unique().
			Field("artist_id"),
		edge.From("album", Album.Type).Ref("annotations").Unique().Field("album_id"),
		edge.From("track", Track.Type).Ref("annotations").Unique().Field("track_id"),
	}
}

func (MusicAnnotation) Indexes() []ent.Index {
	return []ent.Index{
		// SQLite treats NULLs as distinct, so each pair is unique only where
		// its target is set.
		index.Edges("user", "artist").Unique(),
		index.Edges("user", "album").Unique(),
		index.Edges("user", "track").Unique(),
	}
}
