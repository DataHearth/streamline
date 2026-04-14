package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// buildBaseURL composes scheme://host:port[path] for indexer requests.
func buildBaseURL(host string, port uint16, path string, useSSL bool) string {
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path)
}

// newClient returns the indexer client for a protocol. Jackett is configured
// as plain torznab (its /indexers/all aggregate feed is a standard Torznab
// endpoint — only the prefilled path differs), so it needs no branch here.
// Prowlarr has no aggregate Torznab feed and needs its native JSON search
// client.
func newClient(protocol, baseURL, apiKey string) Client {
	switch protocol {
	case "prowlarr":
		return NewProwlarr(baseURL, apiKey)
	default: // torznab
		return NewTorznab(baseURL, apiKey)
	}
}

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/indexer")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/indexer")

	searchCounter  metric.Int64Counter
	searchDuration metric.Float64Histogram
	indexerQueries metric.Int64Counter
	indexerTests   metric.Int64Counter
)

func init() {
	searchCounter = otelx.Must(meter.Int64Counter(
		"streamline.indexer.searches",
		metric.WithDescription("Aggregate indexer search operations"),
	))
	searchDuration = otelx.Must(meter.Float64Histogram(
		"streamline.indexer.search.duration",
		metric.WithDescription("Aggregate search duration across all indexers"),
		metric.WithUnit("s"),
	))
	indexerQueries = otelx.Must(meter.Int64Counter(
		"streamline.indexer.queries",
		metric.WithDescription("Per-indexer query count by outcome"),
	))
	indexerTests = otelx.Must(meter.Int64Counter(
		"streamline.indexer.tests",
		metric.WithDescription("Indexer connection-test invocations by outcome"),
	))

	ctx := context.Background()
	searchCounter.Add(ctx, 0)
	indexerQueries.Add(ctx, 0)
	indexerTests.Add(ctx, 0)
	searchDuration.Record(ctx, 0)
}

// Manager is the consumer-facing surface used by HTTP handlers and rss.
// CRUD over indexers lives in the YAML config (config.AddIndexer etc.); this
// surface keeps the behavioral operations that act on the configured entries.
type Manager interface {
	Test(ctx context.Context, p TestParams) error
	TestByName(ctx context.Context, name string) error
	SearchMovie(
		ctx context.Context,
		titles []string,
		tmdbID uint32,
	) ([]SearchResult, error)
	SearchSeason(
		ctx context.Context,
		titles []string,
		tvdbID uint32,
		season uint16,
	) ([]SearchResult, error)
	SearchSeries(
		ctx context.Context,
		titles []string,
		tvdbID uint32,
	) ([]SearchResult, error)
	SearchEpisode(
		ctx context.Context,
		titles []string,
		tvdbID uint32,
		season, episode uint16,
	) ([]SearchResult, error)
	Feed(ctx context.Context, indexerName string) ([]SearchResult, error)
}

// TestParams describes ad-hoc credentials for a connection test that has not
// yet been persisted as an Indexer row.
type TestParams struct {
	Protocol string
	Host     string
	Port     uint16
	Path     string
	UseSSL   bool
	APIKey   string
}

// indexer searches across all enabled indexers in parallel. The configured
// indexer set is read live from config.Get() per operation.
type indexer struct{}

func New() Manager {
	return &indexer{}
}

// dedupTitles strips empty entries and collapses duplicates while
// preserving first-seen order. Empty input → empty output.
func dedupTitles(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// SearchMovie queries all enabled indexers for a movie against each
// (deduped) title and returns results merged, deduped by Download URL,
// and sorted by seeders descending. Per-indexer queries run sequentially
// across titles to respect indexer rate limits; indexers themselves are
// fanned out in parallel.
func (i *indexer) SearchMovie(
	ctx context.Context,
	titles []string,
	tmdbID uint32,
) ([]SearchResult, error) {
	titles = dedupTitles(titles)
	ctx, span := tracer.Start(ctx, "indexer.search_movie",
		trace.WithAttributes(
			attribute.Int("movie.titles.count", len(titles)),
			attribute.Int64("movie.tmdb_id", int64(tmdbID)),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		searchDuration.Record(ctx, time.Since(start).Seconds())
		searchCounter.Add(ctx, 1)
	}()

	if len(titles) == 0 {
		slog.WarnContext(ctx, "indexer search skipped: no titles after dedup",
			"movie.tmdb_id", tmdbID)
		return nil, nil
	}

	return i.searchAll(ctx, span, titles, SearchParams{TMDBID: tmdbID}), nil
}

// SearchSeason queries all enabled indexers for a season pack of the given
// series (a tvsearch keyed by tvdbid + season, no episode). Results are
// aggregated, deduped, and sorted exactly like SearchMovie.
func (i *indexer) SearchSeason(
	ctx context.Context,
	titles []string,
	tvdbID uint32,
	season uint16,
) ([]SearchResult, error) {
	titles = dedupTitles(titles)
	ctx, span := tracer.Start(ctx, "indexer.search_season",
		trace.WithAttributes(
			attribute.Int("series.titles.count", len(titles)),
			attribute.Int64("series.tvdb_id", int64(tvdbID)),
			attribute.Int("series.season", int(season)),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		searchDuration.Record(ctx, time.Since(start).Seconds())
		searchCounter.Add(ctx, 1)
	}()

	if len(titles) == 0 {
		slog.WarnContext(ctx, "indexer search skipped: no titles after dedup",
			"series.tvdb_id", tvdbID)
		return nil, nil
	}

	// Indexers behind Prowlarr frequently ignore the season param and return
	// the whole series, so drop releases that belong to a different season.
	results := i.searchAll(
		ctx,
		span,
		titles,
		SearchParams{TVDBID: tvdbID, Season: season},
	)
	filtered := filterToSeason(results, season)
	span.SetAttributes(
		attribute.Int("results.pre_season_filter", len(results)),
		attribute.Int("results.total", len(filtered)),
	)
	return filtered, nil
}

// filterToSeason keeps only releases scoped to exactly the requested season.
// Whole-series / multi-season packs (COMPLETE, INTEGRALE, S01-S05) are dropped
// even though they cover the season, because grabbing one imports every season
// it contains — those belong to the whole-series scope. Releases tagged for a
// different specific season are dropped too.
func filterToSeason(results []SearchResult, season uint16) []SearchResult {
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if library.IsWholeSeriesPack(r.Title) {
			continue
		}
		if p := library.Parse(r.Title); p.Season == season {
			out = append(out, r)
		}
	}
	return out
}

// SearchSeries queries all enabled indexers for whole-series releases (a
// tvsearch keyed by tvdbid with no season, catching integral / multi-season
// packs). Results are aggregated, deduped, and sorted exactly like SearchMovie.
func (i *indexer) SearchSeries(
	ctx context.Context,
	titles []string,
	tvdbID uint32,
) ([]SearchResult, error) {
	titles = dedupTitles(titles)
	ctx, span := tracer.Start(ctx, "indexer.search_series",
		trace.WithAttributes(
			attribute.Int("series.titles.count", len(titles)),
			attribute.Int64("series.tvdb_id", int64(tvdbID)),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		searchDuration.Record(ctx, time.Since(start).Seconds())
		searchCounter.Add(ctx, 1)
	}()

	if len(titles) == 0 {
		slog.WarnContext(ctx, "indexer search skipped: no titles after dedup",
			"series.tvdb_id", tvdbID)
		return nil, nil
	}

	return i.searchAll(ctx, span, titles, SearchParams{TVDBID: tvdbID}), nil
}

// SearchEpisode queries all enabled indexers for a single episode (a tvsearch
// keyed by tvdbid + season + episode). Results are aggregated, deduped, and
// sorted exactly like SearchMovie.
func (i *indexer) SearchEpisode(
	ctx context.Context,
	titles []string,
	tvdbID uint32,
	season, episode uint16,
) ([]SearchResult, error) {
	titles = dedupTitles(titles)
	ctx, span := tracer.Start(ctx, "indexer.search_episode",
		trace.WithAttributes(
			attribute.Int("series.titles.count", len(titles)),
			attribute.Int64("series.tvdb_id", int64(tvdbID)),
			attribute.Int("series.season", int(season)),
			attribute.Int("series.episode", int(episode)),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		searchDuration.Record(ctx, time.Since(start).Seconds())
		searchCounter.Add(ctx, 1)
	}()

	if len(titles) == 0 {
		slog.WarnContext(ctx, "indexer search skipped: no titles after dedup",
			"series.tvdb_id", tvdbID)
		return nil, nil
	}

	return i.searchAll(
		ctx, span, titles,
		SearchParams{TVDBID: tvdbID, Season: season, Episode: episode},
	), nil
}

// searchAll fans out one query per (indexer, title) across every enabled
// indexer, merging results deduped by Download URL and sorted by seeders
// descending. base carries the id/season/episode params shared by every
// query; Query is filled per title. Per-indexer errors are logged, never
// returned. When a query keyed by a database id (tmdbid/tvdbid) comes back
// empty, it is retried once on the bare title (keeping season/episode) since
// many private trackers don't index by id.
func (i *indexer) searchAll(
	ctx context.Context,
	span trace.Span,
	titles []string,
	base SearchParams,
) []SearchResult {
	indexers := config.EnabledIndexers()
	span.SetAttributes(attribute.Int("indexers.count", len(indexers)))

	var (
		mu      sync.Mutex
		results []SearchResult
		wg      sync.WaitGroup
	)

	for _, idx := range indexers {
		wg.Go(func() {
			baseURL := buildBaseURL(idx.Host, idx.Port, idx.Path, idx.UseSSL)
			client := newClient(
				idx.Protocol,
				baseURL,
				config.SecretValue(idx.APIKey, idx.APIKeyFile),
			)
			for _, title := range titles {
				queryCtx, childSpan := tracer.Start(ctx, "indexer.query",
					trace.WithAttributes(
						attribute.String("indexer.name", idx.Name),
						attribute.String("indexer.url", baseURL),
						attribute.String("query.title", title),
					),
				)
				params := base
				params.Query = title
				res, err := client.Search(queryCtx, params)
				if err != nil {
					indexerQueries.Add(queryCtx, 1, metric.WithAttributes(
						attribute.String("indexer.name", idx.Name),
						attribute.String("outcome", "error"),
					))
					otelx.RecordSpanError(childSpan, err)
					slog.WarnContext(queryCtx,
						"indexer search failed",
						"indexer", idx.Name,
						"query.title", title,
						"error", err,
					)
					childSpan.End()
					continue
				}
				// Most private trackers behind Prowlarr don't index by
				// TMDB/TVDB ID and silently return 0 when the id is set.
				// Retry once without it so keyword search runs against the
				// title only (season/episode are preserved).
				if len(res) == 0 && (base.TMDBID > 0 || base.TVDBID > 0) {
					slog.DebugContext(queryCtx,
						"indexer search empty with id, retrying without",
						"indexer", idx.Name,
						"title", title,
					)
					retry, retryErr := client.Search(queryCtx, SearchParams{
						Query:   title,
						Season:  base.Season,
						Episode: base.Episode,
					})
					if retryErr == nil {
						res = retry
					}
				}
				indexerQueries.Add(queryCtx, 1, metric.WithAttributes(
					attribute.String("indexer.name", idx.Name),
					attribute.String("outcome", "success"),
				))
				childSpan.SetAttributes(attribute.Int("results.count", len(res)))
				slog.InfoContext(queryCtx,
					"indexer query complete",
					"indexer.name", idx.Name,
					"query.term", title,
					"result.count", len(res),
				)
				childSpan.End()

				for k := range res {
					// Prowlarr stamps the real sub-tracker; only fall back to
					// the config name when the client left it blank (Torznab).
					if res[k].Indexer == "" {
						res[k].Indexer = idx.Name
					}
				}
				mu.Lock()
				results = append(results, res...)
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	if len(results) > 1 {
		seen := make(map[string]struct{}, len(results))
		out := results[:0]
		for _, r := range results {
			if _, ok := seen[r.Download]; ok {
				continue
			}
			seen[r.Download] = struct{}{}
			out = append(out, r)
		}
		results = out
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Seeders > results[j].Seeders
	})

	span.SetAttributes(attribute.Int("results.total", len(results)))
	slog.DebugContext(ctx,
		"indexer search complete",
		"titles.count", len(titles),
		"total_results", len(results),
	)
	return results
}

// Feed loads the indexer row by ID, dials Torznab, and returns the indexer's
// forward-feed items. Used by the rss-sync FeedScanner.
func (i *indexer) Feed(
	ctx context.Context,
	indexerName string,
) ([]SearchResult, error) {
	ctx, span := tracer.Start(ctx, "indexer.feed",
		trace.WithAttributes(attribute.String("indexer.name", indexerName)),
	)
	defer span.End()

	row, ok := config.FindIndexer(indexerName)
	if !ok {
		return nil, otelx.RecordSpanError(span, config.ErrIndexerNotFound)
	}

	baseURL := buildBaseURL(row.Host, row.Port, row.Path, row.UseSSL)
	results, err := newClient(
		row.Protocol,
		baseURL,
		config.SecretValue(row.APIKey, row.APIKeyFile),
	).Feed(ctx)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	for k := range results {
		// Prowlarr stamps the real sub-tracker; only fall back to the config
		// name when the client left it blank (Torznab). Mirrors Search.
		if results[k].Indexer == "" {
			results[k].Indexer = row.Name
		}
	}
	span.SetAttributes(attribute.Int("results.count", len(results)))
	slog.InfoContext(ctx,
		"indexer feed fetched",
		"indexer.name", row.Name,
		"result.count", len(results),
	)
	return results, nil
}

// Test exercises a Torznab endpoint with the supplied connection params.
// Returns one of the typed torznab errors (ErrUnreachable, ErrUnauthorized,
// ErrUnexpectedStatus, ErrBadResponse) on failure so callers can map them
// to user-facing messages.
func (i *indexer) Test(ctx context.Context, p TestParams) error {
	baseURL := buildBaseURL(p.Host, p.Port, p.Path, p.UseSSL)
	ctx, span := tracer.Start(ctx, "indexer.test",
		trace.WithAttributes(attribute.String("indexer.url", baseURL)),
	)
	defer span.End()

	if err := newClient(
		p.Protocol,
		baseURL,
		p.APIKey,
	).TestConnection(ctx); err != nil {
		indexerTests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", "error"),
		))
		return otelx.RecordSpanError(span, err)
	}
	indexerTests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", "success"),
	))
	return nil
}

// TestByName loads the named indexer from config and runs Test against its
// credentials. Returns config.ErrIndexerNotFound when no entry carries the
// name.
func (i *indexer) TestByName(ctx context.Context, name string) error {
	ctx, span := tracer.Start(ctx, "indexer.test_by_name",
		trace.WithAttributes(attribute.String("indexer.name", name)),
	)
	defer span.End()

	idx, ok := config.FindIndexer(name)
	if !ok {
		return otelx.RecordSpanError(span, config.ErrIndexerNotFound)
	}
	return i.Test(ctx, TestParams{
		Protocol: idx.Protocol,
		Host:     idx.Host,
		Port:     idx.Port,
		Path:     idx.Path,
		UseSSL:   idx.UseSSL,
		APIKey:   config.SecretValue(idx.APIKey, idx.APIKeyFile),
	})
}
