package rss

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"

	"go.opentelemetry.io/otel/attribute"
)

// FeedRunner is the consumer-facing surface for triggering one feed scan.
// jobs.RSSFeed accepts it.
type FeedRunner interface {
	Run(ctx context.Context) error
}

// FeedScanner pulls each indexer's RSS forward-feed once per tick, matches
// items against wanted movies by title+year, and grabs anything that passes
// the quality filter. Opportunistic — bypasses the missing-search cooldown.
type FeedScanner struct {
	store    db.Store
	indexers IndexerFeeder
	grabber  Downloader
	quality  QualityConfig
}

// NewFeedScanner builds a FeedScanner using library.default_quality from the
// config singleton. Returns an error if the cooldown duration fails to parse.
func NewFeedScanner(
	store db.Store,
	indexers IndexerFeeder,
	grabber Downloader,
) (*FeedScanner, error) {
	q, err := loadQualityConfig()
	if err != nil {
		return nil, err
	}
	return &FeedScanner{
		store:    store,
		indexers: indexers,
		grabber:  grabber,
		quality:  q,
	}, nil
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
	byTitleYear := buildWantedIndex(wanted)

	// grabbed tracks movie IDs already attempted this tick so a second
	// indexer returning the same title doesn't double-grab.
	grabbed := make(map[uint32]struct{}, len(wanted))
	var matched int
	for _, idx := range indexers {
		items, err := s.indexers.Feed(ctx, idx.Name)
		if err != nil {
			slog.WarnContext(ctx, "feed-scan: indexer failed",
				"indexer", idx.Name, "error", err)
			continue
		}
		matched += s.processItems(ctx, items, byTitleYear, grabbed)
	}

	span.SetAttributes(
		attribute.Int("rss.feed_scan.indexers", len(indexers)),
		attribute.Int("rss.feed_scan.matched", matched),
	)
	slog.InfoContext(ctx, "feed-scan complete",
		"indexers", len(indexers), "matched", matched)
	return nil
}

func (s *FeedScanner) processItems(
	ctx context.Context,
	items []indexer.SearchResult,
	byTitleYear map[string]*ent.Movie,
	grabbed map[uint32]struct{},
) int {
	matched := 0
	// TODO: when the profiling/scoring system lands, replace first-match-wins
	// with a weighted pick across all indexer items for the same movie this
	// tick (rank by quality + seeders + release group score, etc.).
	for _, item := range items {
		m := matchItem(item, byTitleYear)
		if m == nil {
			continue
		}
		if _, already := grabbed[m.ID]; already {
			continue
		}
		if !s.quality.Accepts(item.Title) {
			slog.DebugContext(ctx, "feed-scan: quality rejected",
				"movie", m.Title, "release", item.Title)
			continue
		}
		matched++
		grabbed[m.ID] = struct{}{}
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
		if err := s.store.ResetMovieGrabFailures(ctx, m.ID); err != nil {
			slog.WarnContext(ctx, "feed-scan: reset grab_failures failed",
				"movie", m.Title, "error", err)
		}
		if err := s.store.SetMovieLastSearchAt(ctx, m.ID, time.Now()); err != nil {
			slog.WarnContext(ctx, "feed-scan: set last_search_at failed",
				"movie", m.Title, "error", err)
		}
	}
	return matched
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
