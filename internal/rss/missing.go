package rss

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/roothealth"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/quality"
	"github.com/datahearth/streamline/internal/scheduler"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ErrNoEligibleRelease is returned by SearchOne when no indexer result passes
// the quality filter, the user's rejected releases set aside. last_search_at is
// still bumped so the cooldown counter advances.
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
	workers   uint8
}

func NewMissingSearcher(
	store db.Store,
	indexers IndexerSearcher,
	downloads Downloader,
) *MissingSearcher {
	return &MissingSearcher{
		db:        store,
		indexers:  indexers,
		downloads: downloads,
		workers:   defaultRSSWorkers,
	}
}

// searchWindow holds the library knobs that gate which rows a search pass may
// touch. Read fresh at the start of every pass: config is hot-editable, so a
// snapshot taken at construction would pin the operator to boot-time values.
type searchWindow struct {
	MaxGrabFailures  uint8
	NotSearchedSince time.Time
}

func currentSearchWindow(ctx context.Context) (searchWindow, error) {
	if scheduler.Manual(ctx) {
		return unthrottledWindow(), nil
	}
	c := config.Get()
	cooldown, err := time.ParseDuration(c.Library.NoMatchCooldown)
	if err != nil {
		return searchWindow{}, fmt.Errorf(
			"parse library.no_match_cooldown: %w",
			err,
		)
	}
	return searchWindow{
		MaxGrabFailures:  c.Library.MaxGrabFailures,
		NotSearchedSince: time.Now().Add(-cooldown),
	}, nil
}

// transportFailure reports whether err is the indexer or the download client
// being unreachable, or the download disk being full, rather than anything
// about the release.
//
// grab_failures is a strike count against a *release choice*, and nothing
// resets it but a successful grab — so counting a timeout retires the item
// permanently after max_grab_failures (3 by default) unlucky ticks. A flaky
// Prowlarr would do exactly that to a library: episodes with clean, accepted
// releases stop being searched at all, since the eligibility
// query gates on the same counter, leaving no symptom beyond a row that sits
// "wanted" forever.
func transportFailure(err error) bool {
	return errors.Is(err, download.ErrUnreachable) ||
		errors.Is(err, download.ErrNoSpace) ||
		errors.Is(err, indexer.ErrUnreachable)
}

// unthrottledWindow waives both throttles for user-triggered searches and
// manual runs of the sync job: a cap
// no counter reaches and a cutoff every past search predates.
func unthrottledWindow() searchWindow {
	return searchWindow{
		MaxGrabFailures:  math.MaxUint8,
		NotSearchedSince: time.Now(),
	}
}

// Run performs one sync pass over all eligible wanted movies.
// Per-movie errors are logged and counted but do not abort the run, with one
// exception: a search no indexer answered stops the pass, which then returns
// that error. Every later movie would wait out the same timeout for the same
// non-answer, and with no cooldown stamped on a failed search the next tick
// would walk the whole list into it again.
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

	status, err := roothealth.Offline(ctx, db.RootMovies)
	if err != nil {
		outcome = "query_failed"
		return otelx.RecordSpanError(span, err)
	}
	if status != nil {
		outcome = "root_offline"
		return nil
	}

	window, err := currentSearchWindow(ctx)
	if err != nil {
		outcome = "config_invalid"
		return otelx.RecordSpanError(span, err)
	}
	movies, err := s.db.ListEligibleMoviesForSync(
		ctx,
		window.MaxGrabFailures,
		window.NotSearchedSince,
	)
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
			"max_grab_failures", window.MaxGrabFailures,
			"not_searched_since", window.NotSearchedSince,
		)
		outcome = "no_eligible"
		return nil
	}

	// Whether a download client exists is process-wide, not per-movie: without
	// one every grab below fails identically, after an indexer query per movie
	// and a grab_failures write per rejected release. Bail here rather than at
	// the top of the pass — the eligible count above is the number the operator
	// needs to see — and before the counters move, so nothing is thrown at the
	// throttles for an attempt that never happened.
	if _, ok := config.PickDownloadClient(); !ok {
		slog.InfoContext(ctx, "missing-search: no enabled download client",
			"eligible", len(movies),
		)
		outcome = "no_download_client"
		return nil
	}

	var grabbed, noMatch, alreadyExists, errCount, done atomic.Int64
	var unavailable atomic.Pointer[error]
	sem := make(chan struct{}, s.workers)
	var wg sync.WaitGroup

	for _, m := range movies {
		if err := ctx.Err(); err != nil {
			break
		}
		sem <- struct{}{}
		// Read once a worker slot is free, so the search that freed it has
		// already said whether any indexer answered.
		if unavailable.Load() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(m *ent.Movie) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() { scheduler.Progress(ctx, int(done.Add(1)), len(movies)) }()
			switch err := s.SearchOne(ctx, m); {
			case err == nil:
				grabbed.Add(1)
			case errors.Is(err, ErrNoEligibleRelease):
				noMatch.Add(1)
			case indexer.Unavailable(err):
				errCount.Add(1)
				unavailable.CompareAndSwap(nil, &err)
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
	if err := unavailable.Load(); err != nil {
		outcome = "indexers_unavailable"
		slog.WarnContext(ctx, "missing-search pass stopped: no indexer answered",
			"eligible", len(movies),
			"searched", done.Load(),
			"grabbed", grabbed.Load(),
			"error", *err,
		)
		return otelx.RecordSpanError(span, *err)
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

// SearchNow runs SearchOne for a user who asked for it, detached from their
// request: an indexer query can outlast the client, and the outcome reaches
// them as the movie's `searched` event either way.
func (s *MissingSearcher) SearchNow(ctx context.Context, m *ent.Movie) {
	bg := trace.ContextWithSpanContext(
		context.WithoutCancel(ctx),
		trace.SpanContext{},
	)
	go func() {
		defer observability.RecoverPanic(bg, "rss.search_movie_now", nil)
		err := s.SearchOne(bg, m)
		if err != nil && !errors.Is(err, ErrNoEligibleRelease) {
			slog.WarnContext(bg, "movie search-now failed",
				"movie.id", m.ID, "error", err)
		}
	}()
}

// SearchOne runs one indexer query for movie m, scores every result against
// its quality profile, and dispatches a grab on the highest-scoring one that
// the user has not rejected for this movie.
//
// Returns nil on a successful grab, ErrNoEligibleRelease when nothing matches
// the quality bar, or a wrapped error on indexer / downloader failure.
// last_search_at is bumped whenever an indexer answered (a grab or no match)
// so cooldown counters advance; a search no indexer answered stamps nothing.
func (s *MissingSearcher) SearchOne(ctx context.Context, m *ent.Movie) error {
	ctx, span := tracer.Start(ctx, "rss.process_movie",
		trace.WithAttributes(
			attribute.Int64("movie.id", int64(m.ID)),
			attribute.String("movie.title", m.Title),
		),
	)
	defer span.End()

	movieOutcome := "grabbed"
	resultCount := 0
	defer func() {
		moviesProcessed.Add(ctx, 1,
			metric.WithAttributes(attribute.String("outcome", movieOutcome)),
		)
		if err := events.Record(
			ctx, nil, events.TypeSearched, events.ScopeMovie, m.ID,
			map[string]any{"outcome": movieOutcome, "results": resultCount},
		); err != nil {
			slog.WarnContext(ctx, "missing-search: record searched event failed",
				"movie.id", m.ID, "error", err)
		}
	}()

	// Read before the query, so a failing database does not cost an indexer
	// request; without it the pick could be the release the user rejected.
	blocklist, err := s.db.RejectedReleases(ctx)
	if err != nil {
		movieOutcome = "blocklist_failed"
		return otelx.RecordSpanError(span, err)
	}

	results, err := s.indexers.SearchMovie(
		ctx,
		[]string{m.Title, m.OriginalTitle},
		m.Aliases,
		m.TmdbID,
		m.ImdbID,
	)
	if err != nil {
		movieOutcome = "search_failed"
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("indexer search %q: %w", m.Title, err),
		)
	}

	resultCount = len(results)
	match, ok := scoreBest(
		qualityFor(ctx, config.MediaMovie, m.QualityProfile),
		dropOtherYears(
			ctx,
			dropRejected(ctx, results, blocklist.Movie, m.ID),
			m,
		),
	)
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
		if !transportFailure(err) {
			if e := s.db.IncrementMovieGrabFailures(ctx, m.ID); e != nil {
				slog.WarnContext(ctx,
					"missing-search: failed to bump grab_failures",
					"movie", m.Title,
					"error", e,
				)
			}
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

// scoreBest returns the head of rankAccepted — the highest-scoring result p
// accepts, ties broken by seeders. Indexer order carries no quality signal, so
// the whole set is scored rather than stopping at the first acceptable
// release. Callers that want the fallbacks take rankAccepted directly.
func scoreBest(
	p quality.Profile,
	results []indexer.SearchResult,
) (indexer.SearchResult, bool) {
	if ranked := rankAccepted(p, results, singleRelease); len(ranked) > 0 {
		return ranked[0], true
	}
	return indexer.SearchResult{}, false
}

// releaseYearDrift is how far a release's stated year may sit from the
// movie's and still name it: providers date a film by its first screening and
// release groups by the one they ripped, so a festival premiere in December
// and a release the following year are the same film.
const releaseYearDrift = 1

// namesOtherYear reports whether a release states a year that places it on a
// different film than m. A title is not an identity: "Road House" is a 1989
// film and a 2024 one, a keyword search returns both, and a profile scores
// them alike.
//
// Silence is not a mismatch: a release stating no year stays eligible, and so
// does every release while the movie's own year is unknown.
//
// The film's year is the one stated right after its title (yearsAfterTitle),
// and when the name has one the release is judged on it alone. The parser's
// year is the fallback for a name whose title is not spelled exactly as the
// movie's: it is the last year-like token, so a release of "Blade Runner
// 2049" that states no year parses as "Blade Runner" of 2049. Such a token is
// the title's own when the parsed title followed by it names the movie under
// one of its titles, and then it is no evidence of another film.
func namesOtherYear(release string, m *ent.Movie) bool {
	parsed := library.Parse(release)
	if parsed.Year == 0 || m.Year == 0 {
		return false
	}
	titles := append([]string{m.Title, m.OriginalTitle}, m.Aliases...)
	if years := yearsAfterTitle(release, titles); len(years) > 0 {
		return !slices.ContainsFunc(years, func(y int) bool {
			return withinYearDrift(y, m)
		})
	}
	if withinYearDrift(int(parsed.Year), m) {
		return false
	}
	withYear := fmt.Sprintf("%s %d", parsed.Title, parsed.Year)
	return !slices.ContainsFunc(titles, func(t string) bool {
		return library.TitleNamesSameWork(withYear, t)
	})
}

// releaseYearRe finds every year-like token of a release name, the same shape
// library.Parse reads a year from.
var releaseYearRe = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)

func withinYearDrift(year int, m *ent.Movie) bool {
	drift := year - int(m.Year)
	return drift >= -releaseYearDrift && drift <= releaseYearDrift
}

// yearsAfterTitle returns the year-like tokens of a release that directly
// follow one of the movie's titles: what precedes the token is that title and
// nothing else. That is where a release states the film's year, wherever the
// parser's last-token rule lands: on the edition's year in
// "Blade.Runner.1982.The.Final.Cut.2007" or "Road House (1989) 1080p BluRay
// 2024 Remaster", whose film is the 1982 and the 1989 one.
//
// A token that is the title, or part of it, never follows the title, so it is
// never taken for the film's year: "1984.1956.1080p" states 1956 for a movie
// titled or aliased "1984", whatever year that movie is from. More than one
// token can qualify when one title of the movie extends another with a number
// ("Wonder Woman" and "Wonder Woman 1984"); the caller keeps the release when
// any of them is the movie's.
func yearsAfterTitle(release string, titles []string) []int {
	var years []int
	for _, loc := range releaseYearRe.FindAllStringIndex(release, -1) {
		before := release[:loc[0]]
		if !slices.ContainsFunc(titles, func(t string) bool {
			return library.TitleMatchesStrict(before, t)
		}) {
			continue
		}
		if y, err := strconv.Atoi(release[loc[0]:loc[1]]); err == nil {
			years = append(years, y)
		}
	}
	return years
}

// dropOtherYears removes the releases of another film sharing m's title, so
// an unattended pick is made among releases of the film that is wanted.
// Interactive search lists them: a person reads the year.
func dropOtherYears(
	ctx context.Context,
	results []indexer.SearchResult,
	m *ent.Movie,
) []indexer.SearchResult {
	return slices.DeleteFunc(
		slices.Clone(results),
		func(r indexer.SearchResult) bool {
			if !namesOtherYear(r.Title, m) {
				return false
			}
			slog.DebugContext(ctx, "skipping a release of another year",
				"movie.id", m.ID, "movie.year", m.Year,
				"release", r.Title, "indexer", r.Indexer)
			return true
		},
	)
}
