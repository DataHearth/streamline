package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Author struct {
	ent.Schema
}

func (Author) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Author) Fields() []ent.Field {
	return []ent.Field{
		field.Uint32("hardcover_id").Unique(),
		field.String("name").NotEmpty(),
		field.String("sort_name").Optional(),
		field.String("overview").Optional(),
		field.Bool("monitored").Default(true),
		// folder is a name, not a path: resolved under library.ebook_path
		// and/or library.audiobook_path depending on the slot.
		field.String("folder").Optional(),
		// monitor_policy is applied to newly discovered books on refresh:
		// all → monitored, future → monitored when released after the author
		// was added, none → unmonitored.
		field.Enum("monitor_policy").
			Values("all", "future", "none").
			Default("all"),
		// want_kinds decides which slots newly monitored books get.
		field.Enum("want_kinds").
			Values("ebook", "audiobook", "both").
			Default("ebook"),
		field.String("ebook_quality_profile").Optional(),
		field.String("audiobook_quality_profile").Optional(),
		field.Time("last_refreshed_at").Optional().Nillable(),
	}
}

func (Author) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("books", Book.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
