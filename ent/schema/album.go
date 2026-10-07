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

type Album struct {
	ent.Schema
}

func (Album) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Album) Fields() []ent.Field {
	return []ent.Field{
		// mbid is the MusicBrainz release-group ID: albums are flattened
		// release-groups, and the track list comes from one canonical release.
		field.String("mbid").NotEmpty().Unique(),
		field.String("release_mbid").Optional(),
		field.String("title").NotEmpty(),
		field.Enum("type").
			Values("album", "ep", "single", "compilation", "live", "other").
			Default("album"),
		field.Time("release_date").Optional().Nillable(),
		field.Bool("monitored").Default(true),
		field.Uint8("grab_failures").Default(0),
		field.Time("last_search_at").Optional().Nillable(),
		field.Enum("status").
			Values("wanted", "downloading", "paused", "available", "skipped").
			Default("wanted"),
	}
}

func (Album) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("artist", Artist.Type).Ref("albums").Unique().Required(),
		edge.To("tracks", Track.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("download_records", DownloadRecord.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Album) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("artist"),
		index.Fields("status"),
	}
}
