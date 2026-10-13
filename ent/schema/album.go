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

type Album struct {
	ent.Schema
}

func (Album) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (Album) Fields() []ent.Field {
	return []ent.Field{
		// mbid is the MusicBrainz release-group ID: albums are flattened
		// release-groups, and the track list comes from one canonical release.
		field.String("mbid").NotEmpty().Unique(),
		field.String("release_mbid").Optional(),
		field.String("barcode").Optional(),
		field.String("title").NotEmpty(),
		field.Enum("type").
			Values("album", "ep", "single", "compilation", "live", "other").
			Default("album"),
		field.Time("release_date").Optional().Nillable(),
		// original_date is the release group's first-release-date and
		// edition_date the canonical release's own date, both as MusicBrainz
		// writes them (YYYY, YYYY-MM or YYYY-MM-DD): they are what the tag
		// writer puts in ORIGINALDATE and DATE, and a parsed time would turn
		// a year-only date into the first of January.
		field.String("original_date").Optional(),
		field.String("edition_date").Optional(),
		// genres are the release group's top genres by vote. NULL is an album
		// hydrated before genres were fetched, which the hydration worker
		// backfills; hydration always writes a list, empty when MusicBrainz
		// has none.
		field.JSON("genres", []string{}).Optional(),
		field.Bool("monitored").Default(true),
		// monitor_pinned marks a monitored flag set by hand, which the
		// monitored-types catch-up leaves alone; changing the artist's policy
		// clears it.
		field.Bool("monitor_pinned").Default(false),
		// tags_managed is whether the music tag writer re-queues this
		// album's files on its own (a managed refresh, a settings change,
		// Retag library). An album adopted from disk with tagging off at the
		// import review is not; one Streamline downloaded is. A file queued
		// explicitly, a new download's, is written either way.
		field.Bool("tags_managed").Default(true),
		// art_sidecar_hash is the SHA-256 of the cover.jpg (or .png) the tag
		// writer left beside the album's files: a cover file with any other
		// content is someone else's and is never replaced, and the cover
		// resolver never reads Streamline's own back as a source.
		field.String("art_sidecar_hash").Optional(),
		// tag_overrides are tags set by hand for every file of the album,
		// key to values, written over what the library would write (an
		// empty list removes the key). A track's own overrides win over
		// these; library.music_tags.preserve wins over both. NULL is none.
		field.JSON("tag_overrides", map[string][]string{}).Optional(),
		field.Uint8("grab_failures").Default(0),
		field.Time("last_search_at").Optional().Nillable(),
		field.Enum("status").
			Values("wanted", "downloading", "paused", "available", "skipped", "failed").
			Default("wanted"),
		// failure_reason is the terminal import error that parked the album
		// at failed. The missing searches skip a failed album, so this is what
		// tells the user why nothing more is happening.
		field.String("failure_reason").Optional(),
		field.String("label").Optional(),
		field.String("catalog_number").Optional(),
		field.String("country").Optional(),
		// Comma-joined MusicMedium values, not JSON, for the reason
		// ArtistMember.instruments gives.
		field.String("media").Optional(),
		field.String("studio").Optional(),
		// metadata_fetched_at is set once the tracks are written; NULL is an
		// album still waiting for the hydration worker. credits_fetched_at is
		// set when the heavy release call succeeded, and stays NULL when only
		// the light fallback did.
		field.Time("metadata_fetched_at").Optional().Nillable(),
		field.Time("credits_fetched_at").Optional().Nillable(),
		// credits_version is the layout the credits were stored under
		// (library.MusicCreditsVersion when current). An album holding files
		// whose credits are older is fetched again in the background, and
		// until then the tag writer leaves the credit tags alone.
		field.Uint8("credits_version").Default(0),
	}
}

func (Album) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("artist", Artist.Type).Ref("albums").Unique().Required(),
		edge.To("tracks", Track.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("download_records", DownloadRecord.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("credits", MusicCredit.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		// Records of discography packs expected to cover this album.
		edge.From("pack_records", DownloadRecord.Type).Ref("albums"),
	}
}

func (Album) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("artist"),
		index.Fields("status"),
		index.Fields("metadata_fetched_at"),
	}
}
