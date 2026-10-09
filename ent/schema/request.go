package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

type Request struct {
	ent.Schema
}

func (Request) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Request) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("media_type").
			Values("movie", "tvshow", "artist", "book", "book_series"),
		field.Uint32("media_id").Optional().Default(0).
			Comment("TMDB ID for movies, TVDB ID for TV shows, Hardcover ID for books and book series (separate id spaces, which is why media_type is in the uniqueness key). Zero for artists."),
		field.String("media_mbid").Optional().
			Comment("Artist MBID, for artist requests only."),
		field.String("title").NotEmpty(),
		field.Enum("status").
			Values("pending", "approved", "denied", "available").
			Default("pending"),
		field.String("reason").Optional().
			Comment("Admin-supplied reason, e.g. on denial."),
		field.String("quality_profile").Optional().
			Comment("Profile the requester asked for; empty means no preference."),
	}
}

func (Request) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("requester", User.Type).Ref("requests").Unique().Required(),
		edge.To("approved_by", User.Type).
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (Request) Indexes() []ent.Index {
	return []ent.Index{
		// Partial on purpose: the uniqueness only holds over the statuses
		// FindActiveRequest treats as active, so a denied or superseded
		// request never blocks a legitimate re-request of the same media.
		//
		// The <> 0 / <> '' guards are load-bearing: music rows carry
		// media_id = 0, so without the first every second artist would collide
		// on the placeholder; the second keeps an empty-string MBID, should a
		// writer ever store one instead of NULL, from colliding the same way.
		index.Fields("media_type", "media_id").
			Unique().
			Annotations(entsql.IndexWhere(
				"status IN ('pending', 'approved', 'available') AND media_id <> 0",
			)),
		index.Fields("media_type", "media_mbid").
			Unique().
			Annotations(entsql.IndexWhere(
				"status IN ('pending', 'approved', 'available') AND media_mbid <> ''",
			)),
		index.Edges("requester"),
		index.Edges("approved_by"),
	}
}
