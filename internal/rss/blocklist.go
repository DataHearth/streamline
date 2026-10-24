package rss

import (
	"context"
	"log/slog"
	"slices"

	"github.com/datahearth/streamline/internal/indexer"
)

// blockedFor is one of db.ReleaseBlocklist's per-kind lookups (Movie, Show,
// Album, Book), taken as a method value; a nil blocklist blocks nothing.
type blockedFor func(id uint32, infoHash, title string) bool

// rejected reports whether the user rejected r for item id from the
// held-download dialog, and logs the skip.
func rejected(
	ctx context.Context,
	r indexer.SearchResult,
	blocked blockedFor,
	id uint32,
) bool {
	if !blocked(id, r.InfoHash, r.Title) {
		return false
	}
	slog.InfoContext(ctx, "skipping a release the user rejected",
		"release", r.Title, "indexer", r.Indexer)
	return true
}

// dropRejected removes the releases the user rejected for item id. Every
// automatic pick from a search runs over what is left, so the next-best
// release is taken instead of the rejected one coming straight back.
func dropRejected(
	ctx context.Context,
	results []indexer.SearchResult,
	blocked blockedFor,
	id uint32,
) []indexer.SearchResult {
	return slices.DeleteFunc(
		slices.Clone(results),
		func(r indexer.SearchResult) bool {
			return rejected(ctx, r, blocked, id)
		},
	)
}
