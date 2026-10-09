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

// Author is a person record: OPDS, naming, the shelf facet and photos. It is
// never monitored and never fetched on its own; it arrives inside book and
// series calls.
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
		field.String("image_source").Optional().
			Comment("Hardcover CDN URL for the asynchronous photo fetch."),
	}
}

func (Author) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("contributions", BookContribution.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Author) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name")}
}
