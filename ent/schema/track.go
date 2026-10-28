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

type Track struct {
	ent.Schema
}

func (Track) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Track) Fields() []ent.Field {
	return []ent.Field{
		// mbid is the recording; release_track_mbid the track of the
		// canonical release, which is what MUSICBRAINZ_RELEASETRACKID names.
		field.String("mbid").Optional(),
		field.String("release_track_mbid").Optional(),
		field.String("title").NotEmpty(),
		field.Uint8("disc").Default(1),
		field.Uint16("position"),
		field.Uint32("duration").Optional().Default(0),
		field.Bool("bonus").Default(false),
		// artist_credit is the track's credited artists as MusicBrainz joins
		// them ("A x B"), what ARTIST names when they are not the album's.
		field.String("artist_credit").Optional(),
		// tag_overrides are tags set by hand for this track's files, written
		// over the album's overrides and what the library would write.
		field.JSON("tag_overrides", map[string][]string{}).Optional(),
	}
}

func (Track) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("album", Album.Type).Ref("tracks").Unique().Required(),
		edge.To("media_files", MediaFile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("credits", MusicCredit.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("annotations", MusicAnnotation.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("bookmarks", Bookmark.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Track) Indexes() []ent.Index {
	return []ent.Index{index.Edges("album")}
}
