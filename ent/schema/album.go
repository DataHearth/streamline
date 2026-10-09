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
		field.String("barcode").Optional(),
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
		field.String("label").Optional(),
		field.String("catalog_number").Optional(),
		field.String("country").Optional(),
		// Comma-joined MusicMedium values, not JSON, for the reason
		// ArtistMember.instruments gives.
		field.String("media").Optional(),
		field.String("studio").Optional(),
		// metadata_fetched_at is set once the tracks are written; NULL is an
		// album still waiting for the hydration worker. credits_fetched_at is
		// set when the heavy release call succeeded, and stays NULL when only
		// the light fallback did.
		field.Time("metadata_fetched_at").Optional().Nillable(),
		field.Time("credits_fetched_at").Optional().Nillable(),
	}
}

func (Album) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("artist", Artist.Type).Ref("albums").Unique().Required(),
		edge.To("tracks", Track.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("download_records", DownloadRecord.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("credits", MusicCredit.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		// Records of discography packs expected to cover this album.
		edge.From("pack_records", DownloadRecord.Type).Ref("albums"),
	}
}

func (Album) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("artist"),
		index.Fields("status"),
		index.Fields("metadata_fetched_at"),
	}
}
