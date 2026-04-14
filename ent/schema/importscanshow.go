package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// ScannedShowCandidate is a TVDB match option surfaced for a show folder. Stored
// as JSON in ImportScanShow.candidates; defined here so ent code-gen can
// reference it without an import cycle (mirrors ScannedCandidate).
type ScannedShowCandidate struct {
	TVDBID uint32 `json:"tvdb_id"`
	Title  string `json:"title"`
	Year   uint16 `json:"year,omitempty"`
}

// ImportScanShow is one detected show folder in a series import scan.
type ImportScanShow struct{ ent.Schema }

func (ImportScanShow) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ImportScanShow) Fields() []ent.Field {
	return []ent.Field{
		field.String("folder_path").NotEmpty(),
		field.String("parsed_title").Optional(),
		field.Uint16("parsed_year").Optional().Nillable(),
		field.Enum("classification").
			Values("confirmed", "ambiguous", "unmatched", "existing").
			Default("unmatched"),
		field.Uint32("tvdb_id").Optional().Nillable(),
		field.JSON("candidates", []ScannedShowCandidate{}).Optional(),
		field.Uint32("existing_tvshow_id").Optional().Nillable(),
		field.Uint16("file_count").Default(0),

		field.Enum("decision").
			Values("pending", "accept", "skip").
			Default("pending"),
		field.Uint32("decision_tvdb_id").Optional().Nillable(),

		field.Enum("outcome").
			Values("pending", "created", "failed").
			Default("pending"),
		field.String("outcome_message").Optional(),
		field.Uint32("created_tvshow_id").Optional().Nillable(),
	}
}

func (ImportScanShow) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("scan", ImportScan.Type).Ref("shows").Unique().Required(),
	}
}

func (ImportScanShow) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("classification"),
		index.Fields("decision"),
	}
}
