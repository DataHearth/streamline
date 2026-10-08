package restapi

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
)

// toSearchResults converts one search's releases for the item it was run for
// — the movie, or the show for any of the three TV scopes; set exactly one id
// — stamping each with its indexer's privacy and when it was last grabbed.
func (s *Server) toSearchResults(
	ctx context.Context,
	results []indexer.SearchResult,
	movieID, showID uint32,
) []SearchResult {
	private := indexerPrivacy()
	grabs := s.releaseGrabs(ctx, results, movieID, showID)
	items := make([]SearchResult, 0, len(results))
	for _, r := range results {
		item := toSearchResult(r)
		if p, ok := private[r.ConfiguredIndexer]; ok {
			item.IndexerPrivate = &p
		}
		if at, ok := grabs.lastGrabbed(r); ok {
			item.PreviouslyGrabbedAt = &at
		}
		items = append(items, item)
	}
	return items
}

func indexerPrivacy() map[string]bool {
	c := config.Get()
	if c == nil {
		return nil
	}
	out := make(map[string]bool, len(c.Indexers))
	for _, e := range c.Indexers {
		out[e.Name] = e.Private
	}
	return out
}

// releaseGrabs indexes the item's download records by lowercase info hash and
// by lowercase title. unhashedTitle holds only the records that carry no
// hash: a release with a hash is matched by title against those alone, since
// a record with a different hash under the same title is a different upload.
type releaseGrabs struct {
	hash, title, unhashedTitle map[string]time.Time
}

// releaseGrabs degrades to "never grabbed" on a lookup failure: a search
// missing its history badge beats a search that fails outright.
func (s *Server) releaseGrabs(
	ctx context.Context,
	results []indexer.SearchResult,
	movieID, showID uint32,
) releaseGrabs {
	if len(results) == 0 {
		return releaseGrabs{}
	}
	hashes := make([]string, 0, len(results))
	titles := make([]string, 0, len(results))
	for _, r := range results {
		if r.InfoHash != "" {
			hashes = append(hashes, r.InfoHash)
		}
		titles = append(titles, r.Title)
	}
	recs, err := s.store.ListReleaseGrabs(ctx, movieID, showID, hashes, titles)
	if err != nil {
		slog.WarnContext(ctx, "release grab history unavailable, results unmarked",
			"movie.id", movieID, "tvshow.id", showID, "error", err)
		return releaseGrabs{}
	}
	g := releaseGrabs{
		hash:          map[string]time.Time{},
		title:         map[string]time.Time{},
		unhashedTitle: map[string]time.Time{},
	}
	for _, rec := range recs {
		title := strings.ToLower(rec.Title)
		keepLatest(g.title, title, rec.CreateTime)
		if rec.TorrentHash == "" {
			keepLatest(g.unhashedTitle, title, rec.CreateTime)
		} else {
			keepLatest(g.hash, strings.ToLower(rec.TorrentHash), rec.CreateTime)
		}
	}
	return g
}

func (g releaseGrabs) lastGrabbed(r indexer.SearchResult) (time.Time, bool) {
	title := strings.ToLower(r.Title)
	if r.InfoHash == "" {
		at, ok := g.title[title]
		return at, ok
	}
	byHash, okHash := g.hash[r.InfoHash]
	byTitle, okTitle := g.unhashedTitle[title]
	switch {
	case okHash && okTitle:
		if byTitle.After(byHash) {
			return byTitle, true
		}
		return byHash, true
	case okHash:
		return byHash, true
	default:
		return byTitle, okTitle
	}
}

func keepLatest(m map[string]time.Time, key string, at time.Time) {
	if prev, ok := m[key]; !ok || at.After(prev) {
		m[key] = at
	}
}
