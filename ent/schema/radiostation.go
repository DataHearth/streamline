package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// RadioStation is an internet radio stream listed to every Subsonic client.
// Streamline never fetches stream_url: the client plays it directly.
type RadioStation struct {
	ent.Schema
}

func (RadioStation) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (RadioStation) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("stream_url").NotEmpty(),
		field.String("homepage_url").Optional(),
	}
}
