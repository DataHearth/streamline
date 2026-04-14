package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Episode struct {
	ent.Schema
}

func (Episode) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Episode) Fields() []ent.Field {
	return []ent.Field{
		field.Uint16("number"),
		field.String("title").Optional(),
		field.Time("air_date").Optional(),
		field.Bool("monitored").Default(true),
		field.Uint16("absolute_number").Optional().Default(0),
		field.Uint8("grab_failures").Default(0),
		field.Time("last_search_at").Optional().Nillable(),
		field.Enum("status").
			Values("wanted", "downloading", "paused", "available", "skipped").
			Default("wanted"),
	}
}

func (Episode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("season", Season.Type).Ref("episodes").Unique().Required(),
		edge.To("download_records", DownloadRecord.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("media_files", MediaFile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
