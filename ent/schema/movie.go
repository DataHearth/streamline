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

type Movie struct {
	ent.Schema
}

func (Movie) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Movie) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").NotEmpty(),
		field.String("original_title").NotEmpty(),
		field.Uint16("year"),
		field.String("overview").Optional(),
		field.Uint16("runtime").Optional().Default(0),
		field.Enum("status").
			Values("wanted", "downloading", "available", "failed").
			Default("wanted"),
		field.Bool("monitored").Default(true),
		field.Uint32("tmdb_id").Unique(),
		field.Time("last_search_at").Optional().Nillable(),
		field.Time("digital_release_date").Optional().Nillable(),
		field.Uint8("grab_failures").Default(0),
		field.String("failure_reason").Optional(),
		field.String("quality_profile").Optional(),
	}
}

func (Movie) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("download_records", DownloadRecord.Type),
		edge.To("media_files", MediaFile.Type),
		edge.To("events", MovieEvent.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Movie) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("digital_release_date"),
	}
}
