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

// Book is the root of the books vertical: it is a shelf item by itself, or a
// volume of a BookSeries. People hang off it through BookContribution.
type Book struct {
	ent.Schema
}

func (Book) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Book) Fields() []ent.Field {
	return []ent.Field{
		field.Uint32("hardcover_id").Unique(),
		field.String("title").NotEmpty(),
		field.String("original_title").Optional(),
		field.String("sort_title").Optional(),
		field.String("author_name").Optional(),
		field.Enum("kind").
			Values("novel", "bd", "comic", "manga").
			Default("novel"),
		field.String("genre").Optional(),
		field.Uint8("rating_tenths").Optional().Nillable(),
		field.Uint16("release_year").Optional().Nillable(),
		field.Time("release_date").Optional().Nillable(),
		field.String("overview").Optional(),
		field.String("preferred_language").Default("en"),
		field.String("quality_profile").Optional(),
		field.Float("series_position").Optional().Nillable(),

		field.Bool("ebook_monitored").Default(false),
		field.Enum("ebook_status").
			Values("wanted", "downloading", "paused", "available", "skipped").
			Default("skipped"),
		field.Uint8("ebook_grab_failures").Default(0),
		field.Time("ebook_last_search_at").Optional().Nillable(),
		field.String("ebook_replacing_language").Optional(),

		field.Bool("audiobook_monitored").Default(false),
		field.Enum("audiobook_status").
			Values("wanted", "downloading", "paused", "available", "skipped").
			Default("skipped"),
		field.Uint8("audiobook_grab_failures").Default(0),
		field.Time("audiobook_last_search_at").Optional().Nillable(),
		field.String("audiobook_replacing_language").Optional(),

		field.Time("last_refreshed_at").Optional().Nillable().
			Comment("Null on a series stub the hydration worker has not reached; such a row is never searched, grabbed or imported."),
	}
}

func (Book) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("series", BookSeries.Type).Ref("volumes").Unique(),
		edge.To("editions", BookEdition.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("contributions", BookContribution.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("ebook_edition", BookEdition.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("audiobook_edition", BookEdition.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("media_files", MediaFile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("download_records", DownloadRecord.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// SQLite indexes no foreign key on its own; the shelf rollup joins on series.
func (Book) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("series"),
		index.Edges("series").Fields("series_position"),
		index.Fields("kind"),
		index.Fields("author_name"),
	}
}
