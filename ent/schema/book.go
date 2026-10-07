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
		field.String("sort_title").Optional(),
		field.Time("release_date").Optional().Nillable(),
		field.String("overview").Optional(),
		// Flat series strings from Hardcover — display/sort only, no entity.
		field.String("series_name").Optional(),
		field.String("series_position").Optional(),

		// Two independent format slots. A slot is wanted when the author is
		// monitored, the slot flag is set, and no file of that kind exists.
		field.Bool("ebook_monitored").Default(false),
		field.Enum("ebook_status").
			Values("wanted", "downloading", "paused", "available", "skipped").
			Default("skipped"),
		field.Uint8("ebook_grab_failures").Default(0),
		field.Time("ebook_last_search_at").Optional().Nillable(),

		field.Bool("audiobook_monitored").Default(false),
		field.Enum("audiobook_status").
			Values("wanted", "downloading", "paused", "available", "skipped").
			Default("skipped"),
		field.Uint8("audiobook_grab_failures").Default(0),
		field.Time("audiobook_last_search_at").Optional().Nillable(),
	}
}

func (Book) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("author", Author.Type).Ref("books").Unique().Required(),
		edge.To("media_files", MediaFile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("download_records", DownloadRecord.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// SQLite indexes no foreign key on its own; deleting an author scans books for
// children to cascade.
func (Book) Indexes() []ent.Index {
	return []ent.Index{index.Edges("author")}
}
