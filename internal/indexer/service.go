package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
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
	// SearchEpisode also reports how many season/whole-series packs covering
	// the episode were filtered out, so a caller can say why an empty list is
	// empty and point at the season scope.
	SearchEpisode(
		ctx context.Context,
		titles []string,
		tvdbID uint32,
		season, episode uint16,
	) ([]SearchResult, int, error)
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

// dedupResults collapses the same release appearing more than once in a merged
// result set, preserving first-seen order.
//
// The key is not the download URL: a series is queried once per title (local +
// original) and Prowlarr answered the two queries with the same releases under
// different proxy links, so every release showed up twice in the manual-search
// modal. Title+indexer+size is the release identity; the indexer stays in the
// key so the same release on two trackers keeps both rows, which carry their
// own seeder counts.
func dedupResults(in []SearchResult) []SearchResult {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, r := range in {
		key := fmt.Sprintf("%s|%s|%d", r.Title, r.Indexer, r.Size)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out
}

// SearchMovie queries all enabled indexers for a movie against each
// (deduped) title and returns results merged, deduped by release identity,
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
	filtered := preferTitleMatches(filterToSeason(results, season), titles)
	span.SetAttributes(
		attribute.Int("results.pre_season_filter", len(results)),
		attribute.Int("results.total", len(filtered)),
	)
	return filtered, nil
}

// A fansub tag survives extractTitle and would make every tagged release read
// as a different show. The opening bracket is usually already gone —
// library.Parse trims leading delimiters — so it is optional here, and
// everything through the first closing bracket goes. Same shape as
// rss.fansubTagRe, which normalizes the same names for the feed scanner.
var fansubTagRe = regexp.MustCompile(`^\[?[^\]]*\]\s*`)

// preferTitleMatches returns only the results whose parsed title names this
// show, and every result when none does.
//
// The scope filters match on numbers alone, so an indexer answering a keyword
// search offers every show sharing them: a search for one anime's S04E03 came
// back with Reacher, Ted Lasso and Strange New Worlds alongside it, any of
// which can outrank the right release under a profile that cannot tell them
// apart. The importer files whatever was grabbed under the record's anchor
// episode, so a wrong result becomes a wrong file with nothing downstream to
// catch it.
//
// Dropping the non-matches outright is what this deliberately does not do.
// titles is the show's own two names — TVShow stores no aliases and neither
// caller loads TVDB's — so a library holding a show under a translated title
// (`Moi, quand je me réincarne en Slime`, original `転生したらスライムだった件`)
// matches none of its English releases, which are most of what its indexers
// carry. Preferring keeps the wrong show from winning on score whenever the
// right one is present, and costs nothing when it isn't.
//
// A release whose title the parser could not read counts as a match: an empty
// title is no evidence of the wrong show. Matching is prefix-tolerant in both
// directions because a parsed title keeps what extractTitle could not cut —
// `Breaking Bad COMPLETE`, a fansub tag, a translated suffix.
func preferTitleMatches(results []SearchResult, titles []string) []SearchResult {
	if len(titles) == 0 {
		return results
	}
	matched := make([]SearchResult, 0, len(results))
	for _, r := range results {
		name := fansubTagRe.ReplaceAllString(library.Parse(r.Title).Title, "")
		if name == "" {
			matched = append(matched, r)
			continue
		}
		for _, t := range titles {
			if library.TitlePrefixMatches(name, t) {
				matched = append(matched, r)
				break
			}
		}
	}
	if len(matched) == 0 {
		return results
	}
	return matched
}

// filterToSeason keeps only season packs of exactly the requested season.
// Whole-series / multi-season packs (COMPLETE, INTEGRALE, S01-S05) are dropped
// even though they cover the season, because grabbing one imports every season
// it contains — those belong to the whole-series scope. Single episodes are
// dropped too: both callers treat what comes back as a pack, sizing it against
// the season's episode count and marking the whole season downloading.
func filterToSeason(results []SearchResult, season uint16) []SearchResult {
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if library.IsWholeSeriesPack(r.Title) {
			continue
		}
		if p := library.Parse(r.Title); p.SeasonPack && p.Season == season {
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

	results := i.searchAll(ctx, span, titles, SearchParams{TVDBID: tvdbID})
	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		// A tvsearch with no season is a plain series query, so single episodes
		// and single-season packs come back alongside the integrals. This scope
		// grabs a release as covering every season, so only those qualify.
		if library.IsWholeSeriesPack(r.Title) {
			filtered = append(filtered, r)
		}
	}
	span.SetAttributes(
		attribute.Int("results.pre_series_filter", len(results)),
		attribute.Int("results.total", len(filtered)),
	)
	return filtered, nil
}

// SearchEpisode queries all enabled indexers for a single episode (a tvsearch
// keyed by tvdbid + season + episode). Results are aggregated, deduped, and
// sorted exactly like SearchMovie.
func (i *indexer) SearchEpisode(
	ctx context.Context,
	titles []string,
	tvdbID uint32,
	season, episode uint16,
) ([]SearchResult, int, error) {
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
		return nil, 0, nil
	}

	// Same reason as the season scope: the season/episode params are routinely
	// ignored, and the empty-result retry drops the id entirely, so what comes
	// back is a keyword search over the whole series.
	results := i.searchAll(
		ctx, span, titles,
		SearchParams{TVDBID: tvdbID, Season: season, Episode: episode},
	)
	filtered, hiddenPacks := filterToEpisode(results, season, episode)
	filtered = preferTitleMatches(filtered, titles)
	span.SetAttributes(
		attribute.Int("results.pre_episode_filter", len(results)),
		attribute.Int("results.hidden_packs", hiddenPacks),
		attribute.Int("results.total", len(filtered)),
	)
	return filtered, hiddenPacks, nil
}

// filterToEpisode keeps only releases naming exactly the requested episode.
// Season packs are dropped: they are the season scope's business, and the
// callers here size a result as one episode.
//
// A release naming no season and no episode is kept only when it carries an
// anime absolute number or a daily air date — those shows never spell SxxExx,
// and dropping them would hide every release they have. Keeping *anything*
// unnumbered was the first cut of this and it let through the bulk of the noise
// the filter exists to remove: a bare "Breaking Bad", a whole different show,
// and any pack whose only scope word the parser cannot read.
//
// The second return counts the dropped releases that are packs *covering* this
// episode, which is the only part of the noise an operator can act on: it is
// what the season and whole-series scopes would offer instead. Releases for
// some other episode are not counted — there is nowhere to send anyone for
// those.
func filterToEpisode(
	results []SearchResult,
	season, episode uint16,
) ([]SearchResult, int) {
	out := make([]SearchResult, 0, len(results))
	hiddenPacks := 0
	for _, r := range results {
		p := library.Parse(r.Title)
		if p.Season == season && p.Episode == episode {
			out = append(out, r)
			continue
		}
		// The span is read before the "names nothing" fallback below, not after:
		// a COMPLETE/INTEGRALE pack carries no season token either, so the
		// fallback would otherwise keep every integral in an episode search.
		sp := library.ParseSeasonSpan(r.Title)
		switch {
		case sp.Complete ||
			(sp.From != 0 && sp.From <= season && season <= sp.To):
			hiddenPacks++
		case sp.From == 0 && p.Season == 0 && p.Episode == 0 &&
			(p.AbsoluteNumber > 0 || p.AirDate != nil):
			out = append(out, r)
		}
	}
	return out, hiddenPacks
}

// searchAll fans out one query per (indexer, title) across every enabled
// indexer, merging results deduped by release identity and sorted by seeders
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
						attribute.String("indexer.url", redactURL(baseURL)),
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

	results = dedupResults(results)

	sort.Slice(results, func(i, j int) bool {
		return results[i].Seeders > results[j].Seeders
	})

	// Truncated after the sort, so what survives is the best of the merged
	// set rather than whichever indexer answered first. The fan-out is
	// indexers × titles, each already capped at torznabLimit, so a handful of
	// indexers can still merge into thousands of rows — every one of which is
	// then scored, converted and serialized for a UI showing a page of them.
	dropped := 0
	if len(results) > maxMergedResults {
		dropped = len(results) - maxMergedResults
		results = results[:maxMergedResults]
	}

	span.SetAttributes(
		attribute.Int("results.total", len(results)),
		attribute.Int("results.dropped", dropped),
	)
	if dropped > 0 {
		// Said out loud: a silent cap reads as "this is everything there is".
		slog.InfoContext(ctx,
			"indexer results truncated to the highest-seeded",
			"kept", len(results),
			"dropped", dropped,
		)
	}
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
	if errors.Is(err, ErrFeedUnsupported) {
		// Not a failure and not a result: the protocol has no feed. Reported
		// as an empty scan so callers stay unchanged, at debug so an install
		// running Prowlarr does not log two lines per indexer per tick
		// forever.
		slog.DebugContext(ctx, "indexer has no feed endpoint, skipping",
			"indexer.name", row.Name)
		return nil, nil
	}
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
		trace.WithAttributes(attribute.String("indexer.url", redactURL(baseURL))),
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
