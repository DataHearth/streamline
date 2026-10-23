package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// ArtistMember is one member of a group, as MusicBrainz lists it. The set is
// small and rewritten whole on every refresh.
type ArtistMember struct {
	ent.Schema
}

func (ArtistMember) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ArtistMember) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("mbid").Optional(),
		// Comma-joined rather than JSON: ent decodes a JSON column on every
		// scanned row, and these sit on the artist detail path.
		field.String("instruments").Optional(),
		field.Uint16("from_year").Optional(),
		field.Uint16("to_year").Optional(),
		field.Uint16("ordinal"),
	}
}

func (ArtistMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("artist", Artist.Type).Ref("members").Unique().Required(),
	}
}

func (ArtistMember) Indexes() []ent.Index {
	return []ent.Index{index.Edges("artist")}
}
