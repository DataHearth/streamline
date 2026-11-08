package metadata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// SeriesRating is TMDB's vote average for one series.
type SeriesRating struct {
	TMDBID    uint32  // the TMDB series it was read from
	Rating    float32 // 0–10, zero when TMDB has no votes
	VoteCount uint32
}

// SeriesRatingProvider rates series. TVDB v4 dropped user ratings and its
// `score` is a popularity metric, so the rating of a series comes from
// another provider.
type SeriesRatingProvider interface {
	// SeriesRating reads the series under tmdbID, the TMDB id TVDB maps the
	// show to, or finds it by tvdbID when that is 0 or no longer exists. It
	// answers (nil, nil) when the provider knows no such series, and an error
	// only when it could not be asked.
	SeriesRating(ctx context.Context, tvdbID, tmdbID uint32) (*SeriesRating, error)
}

// Configured reports whether the client holds an API key. Without one every
// request is a 401, so callers with something optional to fetch skip it.
func (t *TMDB) Configured() bool { return t.apiKey != "" }

// SeriesRating is one request: `/3/tv/{tmdbID}` when TVDB named the TMDB
// series, so the rating is the one the series' TMDB link shows, else TMDB's
// own mapping through `/3/find/{tvdbID}?external_source=tvdb_id`. A TMDB id
// TMDB answers 404 for (TVDB's mapping gone stale) falls back to the find.
func (t *TMDB) SeriesRating(
	ctx context.Context,
	tvdbID, tmdbID uint32,
) (*SeriesRating, error) {
	ctx, span := tracer.Start(ctx, "metadata.tmdb.series_rating",
		trace.WithAttributes(
			attribute.Int64("tvdb.id", int64(tvdbID)),
			attribute.Int64("tmdb.id", int64(tmdbID)),
		),
	)
	defer span.End()

	if tmdbID != 0 {
		r, err := t.seriesByID(ctx, tmdbID)
		if !errors.Is(err, tmdbStatusError(http.StatusNotFound)) {
			return r, otelx.RecordSpanError(span, err)
		}
		slog.InfoContext(
			ctx,
			"tmdb has no series under the id tvdb maps the show to; finding it by tvdb id",
			"tvshow.tvdb_id",
			tvdbID,
			"tmdb.id",
			tmdbID,
		)
	}
	r, err := t.seriesByTVDBID(ctx, tvdbID)
	return r, otelx.RecordSpanError(span, err)
}

type tmdbSeriesVotes struct {
	ID          uint32  `json:"id"`
	VoteAverage float32 `json:"vote_average"`
	VoteCount   uint32  `json:"vote_count"`
}

func (v tmdbSeriesVotes) rating() *SeriesRating {
	return &SeriesRating{TMDBID: v.ID, Rating: v.VoteAverage, VoteCount: v.VoteCount}
}

func (t *TMDB) seriesByID(
	ctx context.Context,
	tmdbID uint32,
) (*SeriesRating, error) {
	start, outcome := time.Now(), "success"
	defer func() { recordTMDB(ctx, "get_tv", outcome, start) }()
	var resp tmdbSeriesVotes
	if err := t.get(ctx, fmt.Sprintf("/3/tv/%d", tmdbID), nil, &resp); err != nil {
		outcome = "error"
		if errors.Is(err, tmdbStatusError(http.StatusNotFound)) {
			outcome = "not_found"
		}
		return nil, fmt.Errorf("tmdb get series: %w", err)
	}
	return resp.rating(), nil
}

// seriesByTVDBID reads only `tv_results`: a TVDB id can also name an episode
// or a season there, which have votes of their own.
func (t *TMDB) seriesByTVDBID(
	ctx context.Context,
	tvdbID uint32,
) (*SeriesRating, error) {
	start, outcome := time.Now(), "success"
	defer func() { recordTMDB(ctx, "find_tv", outcome, start) }()
	var resp struct {
		TVResults []tmdbSeriesVotes `json:"tv_results"`
	}
	if err := t.get(
		ctx,
		fmt.Sprintf("/3/find/%d", tvdbID),
		url.Values{"external_source": {"tvdb_id"}},
		&resp,
	); err != nil {
		outcome = "error"
		return nil, fmt.Errorf("tmdb find series: %w", err)
	}
	if len(resp.TVResults) == 0 {
		outcome = "not_found"
		return nil, nil
	}
	return resp.TVResults[0].rating(), nil
}

// recordTMDB counts one TMDB request the way GetMovie does.
func recordTMDB(ctx context.Context, endpoint, outcome string, start time.Time) {
	tmdbDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
		attribute.String("endpoint", endpoint),
	))
	tmdbRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("endpoint", endpoint),
		attribute.String("outcome", outcome),
	))
}

// ratedTV is a TVProvider whose GetSeries also carries a rating.
type ratedTV struct {
	TVProvider
	ratings SeriesRatingProvider
}

// WithSeriesRatings wraps tv so every GetSeries — add, refresh, re-identify
// and the lookup detail alike — carries the rating ratings reports. A nil
// ratings returns tv unchanged. The rating is best effort: when it cannot be
// fetched the details are returned without one and RatingFetched stays false,
// which tells a refresh to keep the rating it stored rather than zero it.
func WithSeriesRatings(tv TVProvider, ratings SeriesRatingProvider) TVProvider {
	if ratings == nil {
		return tv
	}
	return &ratedTV{TVProvider: tv, ratings: ratings}
}

func (r *ratedTV) GetSeries(ctx context.Context, tvdbID uint32) (*TVDetails, error) {
	d, err := r.TVProvider.GetSeries(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	rating, err := r.ratings.SeriesRating(ctx, tvdbID, d.TMDBID)
	if err != nil {
		slog.WarnContext(ctx, "series rating fetch failed; keeping the stored one",
			"tvshow.tvdb_id", tvdbID, "error", err)
		return d, nil
	}
	d.RatingFetched = true
	if rating != nil {
		// TMDB's answer names the series it rated; that wins over a TVDB
		// mapping TMDB no longer knows, and fills one TVDB never had.
		d.TMDBID = rating.TMDBID
		d.Rating = rating.Rating
		d.VoteCount = rating.VoteCount
	}
	return d, nil
}
