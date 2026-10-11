package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// IndexerSession is what a built-in indexer's tracker login left behind,
// keyed by the indexer's config name: the cookies replayed on every request,
// and the login failures that hold the next attempt back so a wrong password
// is not retried into an account ban.
type IndexerSession struct {
	ent.Schema
}

func (IndexerSession) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (IndexerSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().Unique(),
		field.JSON("cookies", map[string]string{}).Optional(),
		// The user agent a browser challenge was solved under: its clearance
		// cookie is only honoured alongside it.
		field.String("user_agent").Optional(),
		field.Time("expires_at").Optional().Nillable(),
		field.Uint8("login_failures").Default(0),
		field.Time("retry_at").Optional().Nillable(),
		field.String("last_error").Optional(),
	}
}
