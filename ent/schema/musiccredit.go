package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// MusicCredit is everything credited on an album or one of its tracks:
// production roles and performers on the album, featured artists and
// writers on a track. It is not Credit, which is TMDB/TVDB cast keyed on
// provider ids.
type MusicCredit struct {
	ent.Schema
}

func (MusicCredit) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (MusicCredit) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("kind").Values("credit", "performer", "featuring", "writer"),
		field.Enum("role").
			Values("producer", "recording", "mix", "mastering", "artwork").
			Optional(),
		field.String("name").NotEmpty(),
		field.String("mbid").Optional(),
		field.String("instruments").Optional(),
		field.Bool("guest").Default(false),
		field.Uint16("ordinal"),
	}
}

// Edges: of album and track exactly one is set. credit and performer rows
// hang off the album, featuring and writer rows off the track; the invariant
// belongs to the writer, as it does for MediaEvent.
func (MusicCredit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("album", Album.Type).Ref("credits").Unique(),
		edge.From("track", Track.Type).Ref("credits").Unique(),
	}
}

func (MusicCredit) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("album"),
		index.Edges("track"),
		index.Fields("mbid"),
	}
}
