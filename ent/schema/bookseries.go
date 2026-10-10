package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// BookSeries groups volumes, which are ordinary Book rows. The series is the
// shelf item; a volume never is.
type BookSeries struct {
	ent.Schema
}

func (BookSeries) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (BookSeries) Fields() []ent.Field {
	return []ent.Field{
		field.Uint32("hardcover_id").Unique(),
		field.String("title").NotEmpty(),
		field.String("original_title").Optional(),
		field.String("sort_title").Optional(),
		field.String("overview").Optional(),
		field.String("author_name").Optional(),
		field.Enum("kind").
			Values("novel", "bd", "comic", "manga").
			Default("novel"),
		field.Uint8("rating_tenths").Optional().Nillable(),
		field.Bool("ongoing").Default(false),
		field.Uint16("since").Optional().Nillable(),
		field.Enum("monitor").
			Values("all", "future", "none").
			Default("all"),
		field.String("quality_profile").Optional(),
		field.String("edition_language").Optional(),
		field.String("edition_publisher").Optional(),
		field.Time("last_refreshed_at").Optional().Nillable().
			Comment("When the series row and its volume skeleton were last read."),
	}
}

func (BookSeries) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("volumes", Book.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("contributions", BookContribution.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
