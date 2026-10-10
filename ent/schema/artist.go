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

type Artist struct {
	ent.Schema
}

func (Artist) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Artist) Fields() []ent.Field {
	return []ent.Field{
		field.String("mbid").NotEmpty().Unique(),
		field.String("name").NotEmpty(),
		field.String("sort_name").Optional(),
		// overview and overview_source are the English Wikipedia extract and
		// its page; the _fr pair is the French one. A locale with no article
		// stays empty and the API falls back to English.
		field.String("overview").Optional(),
		field.String("overview_source").Optional(),
		field.String("overview_fr").Optional(),
		field.String("overview_source_fr").Optional(),
		field.Enum("monitor").
			Values("all", "future", "manual", "none").
			Default("all"),
		field.Enum("type").Values("group", "person").Optional(),
		field.String("origin").Optional(),
		field.Uint16("since").Optional(),
		field.String("genre").Optional(),
		field.Uint32("deezer_id").Optional(),
		field.String("wikidata_id").Optional(),
		field.String("path").Optional(),
		field.String("quality_profile").Optional(),
		field.Time("last_refreshed_at").Optional().Nillable(),
		// Stamped once the overview and photo steps have run, so neither is
		// repeated for an artist that simply has no article or no picture.
		field.Time("details_fetched_at").Optional().Nillable(),
	}
}

func (Artist) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("albums", Album.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("members", ArtistMember.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("download_records", DownloadRecord.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Artist) Indexes() []ent.Index {
	return []ent.Index{index.Fields("create_time")}
}
