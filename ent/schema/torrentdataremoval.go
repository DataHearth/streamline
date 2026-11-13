package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// TorrentDataRemoval is a torrent the builtin engine no longer holds whose
// files it still owes deleting. The row is written before the torrent is
// dropped and goes once its files are gone, so a delete that fails — or a
// process that dies mid-removal — is retried instead of forgotten.
type TorrentDataRemoval struct {
	ent.Schema
}

func (TorrentDataRemoval) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (TorrentDataRemoval) Fields() []ent.Field {
	return []ent.Field{
		field.String("info_hash").NotEmpty().Unique(),
		field.String("name").NotEmpty(),
		field.String("dir").NotEmpty(),
		// The bencoded info dictionary: the file list is the only record of
		// which paths under dir/name were this torrent's.
		field.Bytes("info").NotEmpty(),
	}
}
