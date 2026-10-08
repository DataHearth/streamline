package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// ScannedAlbumCandidate is a MusicBrainz release-group match option surfaced
// for an album folder. Stored as JSON in ImportScanAlbum.candidates; defined
// here so ent code-gen can reference it without an import cycle (mirrors
// ScannedShowCandidate).
type ScannedAlbumCandidate struct {
	ReleaseGroupMBID string `json:"release_group_mbid"`
	ArtistMBID       string `json:"artist_mbid"`
	Title            string `json:"title"`
	Artist           string `json:"artist"`
	Year             uint16 `json:"year,omitempty"`
}

// ImportScanAlbum is one detected album folder in a music import scan.
type ImportScanAlbum struct{ ent.Schema }

func (ImportScanAlbum) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (ImportScanAlbum) Fields() []ent.Field {
	return []ent.Field{
		field.String("folder_path").NotEmpty(),
		// Majority (albumartist, falling back to artist; album) tag values of
		// the folder's audio files; empty when the files are untagged.
		field.String("tagged_artist").Optional(),
		field.String("tagged_album").Optional(),
		field.Enum("classification").
			Values("confirmed", "ambiguous", "unmatched", "existing").
			Default("unmatched"),
		field.String("release_group_mbid").Optional(),
		field.String("artist_mbid").Optional(),
		field.JSON("candidates", []ScannedAlbumCandidate{}).Optional(),
		field.Uint32("existing_album_id").Optional().Nillable(),
		field.Uint16("file_count").Default(0),

		field.Enum("decision").
			Values("pending", "accept", "skip").
			Default("pending"),
		field.String("decision_release_group_mbid").Optional(),

		field.Enum("outcome").
			Values("pending", "created", "failed").
			Default("pending"),
		field.String("outcome_message").Optional(),
		field.Uint32("created_album_id").Optional().Nillable(),
	}
}

func (ImportScanAlbum) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("scan", ImportScan.Type).Ref("albums").Unique().Required(),
	}
}

func (ImportScanAlbum) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("classification"),
		index.Fields("decision"),
		index.Edges("scan"),
		index.Fields("folder_path"),
	}
}
