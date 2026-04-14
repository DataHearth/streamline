package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

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
			Values("downloading", "importing", "completed", "failed", "pending", "dismissed").
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
	}
}

func (DownloadRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).Ref("download_records").Unique(),
		edge.From("episode", Episode.Type).Ref("download_records").Unique(),
	}
}
