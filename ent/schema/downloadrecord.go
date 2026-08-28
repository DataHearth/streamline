package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"

	"github.com/datahearth/streamline/ent/schema/mixins"
)

// HoldReason records one failed import verification check on one source file.
type HoldReason struct {
	File     string `json:"file"`
	Check    string `json:"check"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type DownloadRecord struct {
	ent.Schema
}

func (DownloadRecord) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.UintID{}, mixin.Time{}}
}

func (DownloadRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").NotEmpty(),
		field.String("quality").Optional(),
		field.Int64("size").Optional(),
		field.Enum("status").
			Values("downloading", "importing", "completed", "failed", "pending", "dismissed", "held").
			Default("downloading"),
		field.String("torrent_hash").Optional(),
		field.String("release_group").Optional(),
		field.String("save_path").Optional(),
		field.Uint8("import_attempts").Default(0),
		field.String("failure_reason").Optional(),
		field.Time("imported_at").Optional().Nillable(),
		field.String("indexer_name").Optional(),
		field.String("download_client_name").Optional(),
		// How the importer treats episodes that already have a file. `all` is
		// an operator's explicit overwrite (manual grab); `upgrades` is the
		// scanner saying "replace what this release beats", decided per
		// episode at import time.
		field.Enum("replace_mode").
			Values("none", "upgrades", "all").
			Default("none"),
		// Why the importer stopped and asked; empty on every other status.
		field.JSON("hold_reasons", []HoldReason{}).Optional(),
		// Set by a resolve-import so the re-run skips verification.
		field.Bool("verification_bypassed").Default(false),
		// Grab-time intent: which episodes this record's file selection was
		// meant to cover, for a season pack where only some episodes are
		// wanted. Movie records and packs pulled in whole leave this empty.
		field.JSON("wanted_episodes", []uint32{}).Optional(),
		// Resolution of that intent against the torrent's actual file list —
		// the indices SetWantedFiles was called with.
		field.JSON("selected_files", []int{}).Optional(),
		// Sum of the selected files' sizes, computed once at selection time —
		// the SPA's "2.1 GB of 115 GB" numerator.
		field.Int64("selected_bytes").Optional(),
		field.Enum("selection_state").
			Values("pending", "applied", "unsupported", "skipped").
			Default("skipped"),
	}
}

// Indexes covers the file-selection pass's per-tick
// ListPendingSelectionRecords, whose only predicate is selection_state and
// which runs on a schedule against a table that keeps a month of completed
// records.
func (DownloadRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("selection_state"),
		// status: the queue, the pending list and the 30s monitor tick all
		// filter on it. torrent_hash: the monitor's orphan sweep and the
		// torrents page look records up by hash. (update_time, id): history
		// pages order by it, and without the index each page of the infinite
		// scroll full-scans into a temp sort.
		index.Fields("status"),
		index.Fields("torrent_hash"),
		index.Fields("update_time", "id"),
		// SQLite indexes no foreign key on its own, so deleting a movie or an
		// episode scans this table for children to cascade.
		index.Edges("movie"),
		index.Edges("episode"),
	}
}

func (DownloadRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("movie", Movie.Type).Ref("download_records").Unique(),
		edge.From("episode", Episode.Type).Ref("download_records").Unique(),
	}
}
