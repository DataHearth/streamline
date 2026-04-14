package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// ScannedCandidate is the JSON inner type stored in ImportScanFile.candidates.
// Defined here (rather than in the bulkimport package) so ent code-gen can
// reference it without an import cycle.
type ScannedCandidate struct {
	TMDBID     uint32  `json:"tmdb_id"`
	Title      string  `json:"title"`
	Year       uint16  `json:"year"`
	Popularity float64 `json:"popularity"`
}

type ImportScanFile struct{ ent.Schema }

func (ImportScanFile) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ImportScanFile) Fields() []ent.Field {
	return []ent.Field{
		field.String("source_path").NotEmpty(),
		field.Int64("size"),

		field.String("parsed_title").Optional(),
		field.Uint16("parsed_year").Optional().Nillable(),
		field.String("parsed_quality").Optional(),
		field.String("parsed_release_group").Optional(),

		field.Enum("classification").
			Values("confirmed", "ambiguous", "unmatched", "existing").
			Default("unmatched"),
		field.JSON("candidates", []ScannedCandidate{}).Optional(),
		field.Uint32("tmdb_id").Optional(),
		field.Uint32("existing_movie_id").Optional(),

		field.Enum("decision").
			Values("pending", "accept", "skip").
			Default("pending"),
		field.Uint32("decision_tmdb_id").Optional(),

		field.Enum("outcome").
			Values("pending", "created", "attached", "skipped", "failed").
			Default("pending"),
		field.String("outcome_message").Optional(),
		field.Uint32("created_movie_id").Optional(),
	}
}

func (ImportScanFile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("scan", ImportScan.Type).Ref("files").Unique().Required(),
	}
}

func (ImportScanFile) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("classification"),
		index.Fields("decision"),
	}
}
