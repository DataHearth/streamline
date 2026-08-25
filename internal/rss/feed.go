package rss

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/quality/qualityctx"

	"go.opentelemetry.io/otel/attribute"
)

// FeedRunner is the consumer-facing surface for triggering one feed scan.
// jobs.RSSFeed accepts it.
type FeedRunner interface {
	Run(ctx context.Context) error
}

// FeedScanner pulls each indexer's RSS forward-feed once per tick, matches
// items against wanted movies by title+year, and grabs anything that passes
// the quality filter. Movies that already have a file are matched too, and
// grabbed as an upgrade when the release outscores what is on disk.
// Opportunistic — bypasses the missing-search cooldown.
type FeedScanner struct {
	store    db.Store
	indexers IndexerFeeder
	grabber  Downloader
}

func NewFeedScanner(
	store db.Store,
	indexers IndexerFeeder,
	grabber Downloader,
) *FeedScanner {
	return &FeedScanner{
		store:    store,
		indexers: indexers,
		grabber:  grabber,
	}
}

// feedPass carries one tick's state: the two title+year indexes items are
// matched against, the profiles resolved for the movies in them, and the
// movie IDs already grabbed. grabbed is shared by both branches so a wanted
// grab and an upgrade grab never both fire for one movie.
type feedPass struct {
	wanted   map[string]*ent.Movie
	upgrades map[string]*ent.Movie
	profiles map[string]quality.Profile
	grabbed  map[uint32]struct{}
}

func (s *FeedScanner) Run(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "rss.feed_scan")
	defer span.End()

	indexers := config.EnabledIndexers()
	if len(indexers) == 0 {
		return nil
	}

	wanted, err := s.store.ListWantedMovies(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	// Same reasoning as MissingSearcher.Run: no client means every grab below
	// fails identically, after pulling each indexer's whole feed and bumping
	// grab_failures per matched movie. Kept after the wanted query so the span
	// still carries the backlog size.
	if _, ok := config.PickDownloadClient(); !ok {
		span.SetAttributes(attribute.Int("rss.feed_scan.wanted", len(wanted)))
		slog.InfoContext(ctx, "feed-scan: no enabled download client",
			"wanted", len(wanted),
		)
		return nil
	}
	upgradable, err := s.store.ListUpgradeCandidateMovies(ctx)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}

	pass := &feedPass{
		wanted:   buildWantedIndex(wanted),
		upgrades: buildWantedIndex(upgradable),
		profiles: resolveProfiles(ctx, wanted, upgradable),
		grabbed:  make(map[uint32]struct{}, len(wanted)+len(upgradable)),
	}
	var matched int
	for _, idx := range indexers {
		items, err := s.indexers.Feed(ctx, idx.Name)
		if err != nil {
			slog.WarnContext(ctx, "feed-scan: indexer failed",
				"indexer", idx.Name, "error", err)
			continue
		}
		matched += s.processItems(ctx, items, pass)
	}

	span.SetAttributes(
		attribute.Int("rss.feed_scan.indexers", len(indexers)),
		attribute.Int("rss.feed_scan.upgrade_candidates", len(upgradable)),
		attribute.Int("rss.feed_scan.matched", matched),
	)
	slog.InfoContext(ctx, "feed-scan complete",
		"indexers", len(indexers), "matched", matched)
	return nil
}

func (s *FeedScanner) processItems(
	ctx context.Context,
	items []indexer.SearchResult,
	pass *feedPass,
) int {
	matched := 0
	for _, item := range items {
		if m := matchItem(item, pass.wanted); m != nil {
			if _, already := pass.grabbed[m.ID]; already {
				continue
			}
			res := evaluateRelease(pass.profiles[m.QualityProfile], item)
			if res.Rejected {
				slog.DebugContext(ctx, "feed-scan: quality rejected",
					"movie", m.Title, "release", item.Title,
					"reason", res.RejectReason)
				continue
			}
			matched++
			pass.grabbed[m.ID] = struct{}{}
			if _, err := s.grabber.Grab(ctx, item, m.ID); err != nil {
				slog.WarnContext(ctx, "feed-scan: grab failed",
					"movie", m.Title, "error", err)
				if bumpErr := s.store.IncrementMovieGrabFailures(
					ctx,
					m.ID,
				); bumpErr != nil {
					slog.WarnContext(ctx, "feed-scan: bump grab_failures failed",
						"movie", m.Title, "error", bumpErr)
				}
				continue
			}
			s.markSearched(ctx, m)
			continue
		}
		if m := matchItem(item, pass.upgrades); m != nil {
			if s.tryUpgrade(ctx, item, m, pass) {
				matched++
			}
		}
	}
	return matched
}

// tryUpgrade grabs item as a replacement for the movie's current file when the
// release outscores it under the movie's profile, and flags the new record so
// the importer overwrites instead of skipping. Reports whether it grabbed.
func (s *FeedScanner) tryUpgrade(
	ctx context.Context,
	item indexer.SearchResult,
	m *ent.Movie,
	pass *feedPass,
) bool {
	if _, already := pass.grabbed[m.ID]; already {
		return false
	}
	p := pass.profiles[m.QualityProfile]
	if !p.UpgradeAllowed || len(m.Edges.MediaFiles) == 0 {
		return false
	}
	rel := evaluateRelease(p, item)
	if rel.Rejected {
		slog.DebugContext(ctx, "feed-scan: quality rejected",
			"movie", m.Title, "release", item.Title,
			"reason", rel.RejectReason)
		return false
	}
	mf := m.Edges.MediaFiles[0]
	file := qualityctx.ContextFromFile(
		filepath.Base(mf.Path), mf.Size, int(mf.Width), mf.VideoCodec,
	)
	if !p.UpgradableFrom(file.Resolution) {
		slog.DebugContext(ctx, "feed-scan: upgrade skipped, file outside band",
			"movie", m.Title, "file.resolution", file.Resolution,
			"release", item.Title)
		return false
	}
	cur := quality.Evaluate(p, file)
	if !p.ShouldUpgrade(cur.Score, rel.Score) {
		return false
	}

	pass.grabbed[m.ID] = struct{}{}
	rec, err := s.grabber.Grab(ctx, item, m.ID)
	if err != nil {
		slog.WarnContext(ctx, "feed-scan: upgrade grab failed",
			"movie", m.Title, "release", item.Title, "error", err)
		if bumpErr := s.store.IncrementMovieGrabFailures(ctx, m.ID); bumpErr != nil {
			slog.WarnContext(ctx, "feed-scan: bump grab_failures failed",
				"movie", m.Title, "error", bumpErr)
		}
		return false
	}
	if err := s.store.MarkDownloadRecordReplaceExisting(ctx, rec.ID); err != nil {
		slog.WarnContext(ctx, "feed-scan: mark replace_existing failed",
			"movie", m.Title, "record.id", rec.ID, "error", err)
	}
	s.markSearched(ctx, m)
	slog.InfoContext(ctx, "feed-scan: grabbed upgrade",
		"movie", m.Title, "release", item.Title,
		"file_score", cur.Score, "release_score", rel.Score)
	return true
}

func (s *FeedScanner) markSearched(ctx context.Context, m *ent.Movie) {
	if err := s.store.ResetMovieGrabFailures(ctx, m.ID); err != nil {
		slog.WarnContext(ctx, "feed-scan: reset grab_failures failed",
			"movie", m.Title, "error", err)
	}
	if err := s.store.SetMovieLastSearchAt(ctx, m.ID, time.Now()); err != nil {
		slog.WarnContext(ctx, "feed-scan: set last_search_at failed",
			"movie", m.Title, "error", err)
	}
}

// resolveProfiles resolves each distinct profile name once per tick. Resolving
// per item recompiles every custom format's regex for every RSS item an
// indexer returns.
func resolveProfiles(
	ctx context.Context,
	lists ...[]*ent.Movie,
) map[string]quality.Profile {
	profiles := make(map[string]quality.Profile)
	for _, list := range lists {
		for _, m := range list {
			if _, ok := profiles[m.QualityProfile]; ok {
				continue
			}
			profiles[m.QualityProfile] = qualityFor(ctx, m.QualityProfile)
		}
	}
	return profiles
}

func buildWantedIndex(movies []*ent.Movie) map[string]*ent.Movie {
	byTitleYear := make(map[string]*ent.Movie, len(movies))
	for _, m := range movies {
		byTitleYear[titleYearKey(m.Title, m.Year)] = m
	}
	return byTitleYear
}

func titleYearKey(title string, year uint16) string {
	return fmt.Sprintf("%s|%d", strings.ToLower(strings.TrimSpace(title)), year)
}

func matchItem(
	item indexer.SearchResult,
	byTitleYear map[string]*ent.Movie,
) *ent.Movie {
	parsed := library.Parse(item.Title)
	if parsed.Year == 0 {
		return nil
	}
	if m, ok := byTitleYear[titleYearKey(parsed.Title, parsed.Year)]; ok {
		return m
	}
	return nil
}
