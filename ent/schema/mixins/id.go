package mixins

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
)

type UintID struct{ mixin.Schema }

func (UintID) Fields() []ent.Field {
	return []ent.Field{
		field.Uint32("id"),
	}
}
