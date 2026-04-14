package rss

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ErrNoEligibleRelease is returned by SearchOne when no indexer result passes
// the quality filter. last_search_at is still bumped so the cooldown counter
// advances.
var ErrNoEligibleRelease = errors.New("rss: no eligible release for movie")

// defaultRSSWorkers caps the number of parallel per-movie indexer searches
// inside one Run pass. Hard-coded: the value is dependent on upstream
// indexer rate limits rather than anything an operator tunes.
const defaultRSSWorkers = 5

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/rss")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/rss")

	syncRuns        metric.Int64Counter
	syncDuration    metric.Float64Histogram
	moviesProcessed metric.Int64Counter
)

func init() {
	syncRuns = otelx.Must(meter.Int64Counter(
		"streamline.rss.sync.runs",
		metric.WithDescription("Number of RSS sync passes executed"),
	))
	syncDuration = otelx.Must(meter.Float64Histogram(
		"streamline.rss.sync.duration",
		metric.WithDescription("RSS sync pass duration"),
		metric.WithUnit("s"),
	))
	moviesProcessed = otelx.Must(meter.Int64Counter(
		"streamline.rss.movies_processed",
		metric.WithDescription("Per-movie sync outcome"),
	))

	ctx := context.Background()
	syncRuns.Add(ctx, 0)
	moviesProcessed.Add(ctx, 0)
	syncDuration.Record(ctx, 0)
}

// MissingSearchRunner is the consumer-facing surface for triggering one
// missing-search pass. jobs.RSSSync accepts it so it can be driven by a fake
// in tests without standing up the full MissingSearcher.
type MissingSearchRunner interface {
	Run(ctx context.Context) error
}

// MissingSearcher periodically scans wanted movies, searches indexers, and
// grabs the best matching release via the download manager.
type MissingSearcher struct {
	db        db.Store
	indexers  IndexerSearcher
	downloads Downloader
	quality   QualityConfig
	workers   uint8
}

// NewMissingSearcher builds a MissingSearcher using library.default_quality
// from the config singleton. Returns an error if the NoMatchCooldown duration
// fails to parse.
func NewMissingSearcher(
	store db.Store,
	indexers IndexerSearcher,
	downloads Downloader,
) (*MissingSearcher, error) {
	q, err := loadQualityConfig()
	if err != nil {
		return nil, err
	}
	return &MissingSearcher{
		db:        store,
		indexers:  indexers,
		downloads: downloads,
		quality:   q,
		workers:   defaultRSSWorkers,
	}, nil
}

// loadQualityConfig builds a QualityConfig from the default quality profile
// plus the global library knobs, parsing the cooldown duration string.
func loadQualityConfig() (QualityConfig, error) {
	c := config.Get()
	p, ok := config.ResolveQualityProfile("")
	if !ok {
		return QualityConfig{}, fmt.Errorf("no quality profile configured")
	}
	cooldown, err := time.ParseDuration(c.Library.NoMatchCooldown)
	if err != nil {
		return QualityConfig{}, fmt.Errorf(
			"parse library.no_match_cooldown: %w",
			err,
		)
	}
	return QualityConfig{
		PreferredResolution: p.PreferredResolution,
		MinResolution:       p.MinResolution,
		UpgradeAllowed:      p.UpgradeAllowed,
		NoMatchCooldown:     cooldown,
		MaxGrabFailures:     c.Library.MaxGrabFailures,
	}, nil
}

// Run performs one sync pass over all eligible wanted movies.
// Returns a non-nil error only for ctx cancellation; per-movie errors are
// logged and counted but do not abort the run.
func (s *MissingSearcher) Run(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "rss.sync")
	defer span.End()

	start := time.Now()
	outcome := "success"
	defer func() {
		attrs := metric.WithAttributes(attribute.String("outcome", outcome))
		syncDuration.Record(ctx, time.Since(start).Seconds(), attrs)
		syncRuns.Add(ctx, 1, attrs)
	}()

	movies, err := s.eligibleMovies(ctx)
	if err != nil {
		outcome = "query_failed"
		return otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.Int("eligible.count", len(movies)))
	if len(movies) == 0 {
		wanted, countErr := s.db.CountMoviesByStatus(ctx, movie.StatusWanted)
		if countErr != nil {
			slog.WarnContext(ctx, "missing-search: wanted-count probe failed",
				"error", countErr,
			)
		}
		slog.InfoContext(ctx, "missing-search: no eligible movies",
			"wanted_total", wanted,
			"max_grab_failures", s.quality.MaxGrabFailures,
			"no_match_cooldown", s.quality.NoMatchCooldown.String(),
		)
		outcome = "no_eligible"
		return nil
	}

	var grabbed, noMatch, alreadyExists, errCount atomic.Int64
	sem := make(chan struct{}, s.workers)
	var wg sync.WaitGroup

	for _, m := range movies {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(m *ent.Movie) {
			defer wg.Done()
			defer func() { <-sem }()
			switch err := s.SearchOne(ctx, m); {
			case err == nil:
				grabbed.Add(1)
			case errors.Is(err, ErrNoEligibleRelease):
				noMatch.Add(1)
			case errors.Is(err, download.ErrTorrentAlreadyExists):
				alreadyExists.Add(1)
				slog.InfoContext(ctx,
					"missing-search: torrent already in client (state drift)",
					"movie_id", m.ID,
					"title", m.Title,
				)
			default:
				errCount.Add(1)
				slog.WarnContext(ctx, "missing-search per-movie failed",
					"movie_id", m.ID,
					"title", m.Title,
					"error", err,
				)
			}
		}(m)
	}

	wg.Wait()
	if err := ctx.Err(); err != nil {
		outcome = "cancelled"
		return otelx.RecordSpanError(span, err)
	}
	slog.InfoContext(ctx, "missing-search pass complete",
		"eligible", len(movies),
		"grabbed", grabbed.Load(),
		"no_match", noMatch.Load(),
		"already_exists", alreadyExists.Load(),
		"errors", errCount.Load(),
	)
	return nil
}

// eligibleMovies returns wanted movies that are not over the grab-failure
// cap and whose cooldown has expired (or has never run).
func (s *MissingSearcher) eligibleMovies(ctx context.Context) ([]*ent.Movie, error) {
	return s.db.ListEligibleMoviesForSync(
		ctx,
		s.quality.MaxGrabFailures,
		time.Now().Add(-s.quality.NoMatchCooldown),
	)
}

// SearchOne runs one indexer query for movie m, applies the quality filter,
// and dispatches a grab on the first acceptable result.
//
// Returns nil on a successful grab, ErrNoEligibleRelease when nothing matches
// the quality bar, or a wrapped error on indexer / downloader failure.
// last_search_at is bumped whenever the indexer responds (success or no
// match) so cooldown counters advance.
func (s *MissingSearcher) SearchOne(ctx context.Context, m *ent.Movie) error {
	ctx, span := tracer.Start(ctx, "rss.process_movie",
		trace.WithAttributes(
			attribute.Int64("movie.id", int64(m.ID)),
			attribute.String("movie.title", m.Title),
		),
	)
	defer span.End()

	movieOutcome := "grabbed"
	defer func() {
		moviesProcessed.Add(ctx, 1,
			metric.WithAttributes(attribute.String("outcome", movieOutcome)),
		)
	}()

	results, err := s.indexers.SearchMovie(
		ctx,
		[]string{m.Title, m.OriginalTitle},
		m.TmdbID,
	)
	if err != nil {
		movieOutcome = "search_failed"
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("indexer search %q: %w", m.Title, err),
		)
	}

	match, ok := s.pickBest(results)
	if e := s.db.SetMovieLastSearchAt(ctx, m.ID, time.Now()); e != nil {
		slog.WarnContext(ctx,
			"missing-search: failed to update last_search_at",
			"movie", m.Title,
			"error", e,
		)
	}
	if !ok {
		movieOutcome = "no_match"
		span.SetAttributes(attribute.String("outcome", "no_match"))
		return otelx.RecordSpanError(span, ErrNoEligibleRelease)
	}
	span.SetAttributes(attribute.String("release.title", match.Title))

	if _, err := s.downloads.Grab(ctx, match, m.ID); err != nil {
		if errors.Is(err, download.ErrTorrentAlreadyExists) {
			movieOutcome = "already_exists"
			span.SetAttributes(attribute.String("outcome", "already_exists"))
			return otelx.RecordSpanError(span, err)
		}
		movieOutcome = "grab_failed"
		span.SetAttributes(attribute.String("outcome", "grab_failed"))
		if e := s.db.IncrementMovieGrabFailures(ctx, m.ID); e != nil {
			slog.WarnContext(ctx,
				"missing-search: failed to bump grab_failures",
				"movie", m.Title,
				"error", e,
			)
		}
		return otelx.RecordSpanError(span, fmt.Errorf("grab %q: %w", m.Title, err))
	}

	if err := s.db.ResetMovieGrabFailures(ctx, m.ID); err != nil {
		slog.WarnContext(ctx,
			"missing-search: failed to reset grab_failures",
			"movie", m.Title,
			"error", err,
		)
	}

	span.SetAttributes(attribute.String("outcome", "grabbed"))
	slog.InfoContext(ctx,
		"missing-search: grabbed",
		"movie", m.Title,
		"release", match.Title,
	)
	return nil
}

// pickBest returns the first result that passes the quality filter.
// Results are assumed to be sorted by seeders desc by the caller
// (indexer.Service already does this).
func (s *MissingSearcher) pickBest(
	results []indexer.SearchResult,
) (indexer.SearchResult, bool) {
	for _, r := range results {
		if s.quality.Accepts(r.Title) {
			return r, true
		}
	}
	return indexer.SearchResult{}, false
}
