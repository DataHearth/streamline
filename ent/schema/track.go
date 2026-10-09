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
		field.String("mbid").Optional(),
		field.String("title").NotEmpty(),
		field.Uint8("disc").Default(1),
		field.Uint16("position"),
		field.Uint32("duration").Optional().Default(0),
		field.Bool("bonus").Default(false),
	}
}

func (Track) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("album", Album.Type).Ref("tracks").Unique().Required(),
		edge.To("media_files", MediaFile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("credits", MusicCredit.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Track) Indexes() []ent.Index {
	return []ent.Index{index.Edges("album")}
}
