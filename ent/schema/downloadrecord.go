package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// HoldReason records one failed import verification check on one source file.
type HoldReason struct {
	File     string `json:"file"`
	Check    string `json:"check"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type DownloadRecord struct {
	ent.Schema
}

func (DownloadRecord) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (DownloadRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").NotEmpty(),
		field.String("quality").Optional(),
		field.Int64("size").Optional(),
		field.Enum("status").
			Values("downloading", "importing", "completed", "failed", "pending", "dismissed", "held").
			Default("downloading"),
		field.String("torrent_hash").Optional(),
		field.String("release_group").Optional(),
		field.String("save_path").Optional(),
		field.Uint8("import_attempts").Default(0),
		field.String("failure_reason").Optional(),
		field.Time("imported_at").Optional().Nillable(),
		field.String("indexer_name").Optional(),
		field.String("download_client_name").Optional(),
		// Set when a manual grab requested overwriting already-present files;
		// the importer clears the existing file(s) before re-importing.
		field.Bool("replace_existing").Default(false),
		// Why the importer stopped and asked; empty on every other status.
		field.JSON("hold_reasons", []HoldReason{}).Optional(),
		// Set by a resolve-import so the re-run skips verification.
		field.Bool("verification_bypassed").Default(false),
	}
}

func (DownloadRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).Ref("download_records").Unique(),
		edge.From("episode", Episode.Type).Ref("download_records").Unique(),
	}
}
