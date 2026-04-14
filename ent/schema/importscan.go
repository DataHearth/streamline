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

type ImportScan struct{ ent.Schema }

func (ImportScan) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ImportScan) Fields() []ent.Field {
	return []ent.Field{
		field.String("source_path").NotEmpty(),
		field.Enum("kind").
			Values("movie", "series").
			Default("movie"),
		field.Enum("mode").Values("in_place", "rename"),
		field.Enum("import_mode").
			Values("hardlink", "copy", "move").
			Optional(),
		field.Enum("status").
			Values("running", "awaiting_review", "committing", "completed", "cancelled", "failed").
			Default("running"),
		field.Uint32("total_count").Default(0),
		field.Uint32("processed_count").Default(0),
		field.Uint32("commit_success_count").Default(0),
		field.Uint32("commit_failed_count").Default(0),
		field.String("failure_reason").Optional(),
		field.Time("scanned_at").Optional().Nillable(),
		field.Time("committed_at").Optional().Nillable(),
	}
}

func (ImportScan) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("files", ImportScanFile.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("shows", ImportScanShow.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (ImportScan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("kind"),
	}
}
