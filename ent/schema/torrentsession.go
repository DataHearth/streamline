package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// TorrentSession is one torrent held by the builtin BitTorrent engine.
// Rows are re-added to the engine on boot; piece completion lives in the
// engine's bolt file, so a re-add never re-hashes or re-downloads.
type TorrentSession struct {
	ent.Schema
}

func (TorrentSession) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (TorrentSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("info_hash").NotEmpty().Unique(),
		field.String("name").Optional(),
		field.String("save_path").NotEmpty(),
		// Exactly one source is set; boot re-add rebuilds the spec from it.
		field.String("source_magnet").Optional(),
		field.Bytes("source_torrent").Optional(),
		field.Bool("paused").Default(false),
		field.Time("completed_at").Optional().Nillable(),
		field.Bool("seed_stopped").Default(false),
	}
}
