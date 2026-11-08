package metadata

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// SeriesRating is TMDB's vote average for one series, found by its TVDB id.
type SeriesRating struct {
	TMDBID    uint32
	Rating    float32 // 0–10, zero when TMDB has no votes
	VoteCount uint32
}

// SeriesRatingProvider rates series. TVDB v4 dropped user ratings and its
// `score` is a popularity metric, so the rating of a series comes from
// another provider, keyed on the TVDB id the library already holds.
type SeriesRatingProvider interface {
	// SeriesRating answers (nil, nil) when the provider knows no series under
	// tvdbID, and an error only when it could not be asked.
	SeriesRating(ctx context.Context, tvdbID uint32) (*SeriesRating, error)
}

// Configured reports whether the client holds an API key. Without one every
// request is a 401, so callers with something optional to fetch skip it.
func (t *TMDB) Configured() bool { return t.apiKey != "" }

// SeriesRating looks the series up by TVDB id through TMDB's `/find`, one
// request: TMDB maps external ids itself, so no TMDB id has to be known
// first. Only `tv_results` is read — a TVDB id can also name an episode or a
// season there, which have votes of their own.
func (t *TMDB) SeriesRating(
	ctx context.Context,
	tvdbID uint32,
) (*SeriesRating, error) {
	ctx, span := tracer.Start(ctx, "metadata.tmdb.series_rating",
		trace.WithAttributes(attribute.Int64("tvdb.id", int64(tvdbID))),
	)
	defer span.End()

	start := time.Now()
	outcome := "success"
	defer func() {
		tmdbDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("endpoint", "find_tv"),
		))
		tmdbRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", "find_tv"),
			attribute.String("outcome", outcome),
		))
	}()

	var resp struct {
		TVResults []struct {
			ID          uint32  `json:"id"`
			VoteAverage float32 `json:"vote_average"`
			VoteCount   uint32  `json:"vote_count"`
		} `json:"tv_results"`
	}
	if err := t.get(
		ctx,
		fmt.Sprintf("/3/find/%d", tvdbID),
		url.Values{"external_source": {"tvdb_id"}},
		&resp,
	); err != nil {
		outcome = "error"
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("tmdb find series: %w", err),
		)
	}
	if len(resp.TVResults) == 0 {
		outcome = "not_found"
		return nil, nil
	}
	r := resp.TVResults[0]
	span.SetAttributes(attribute.Int64("tmdb.id", int64(r.ID)))
	return &SeriesRating{
		TMDBID:    r.ID,
		Rating:    r.VoteAverage,
		VoteCount: r.VoteCount,
	}, nil
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
	rating, err := r.ratings.SeriesRating(ctx, tvdbID)
	if err != nil {
		slog.WarnContext(ctx, "series rating fetch failed; keeping the stored one",
			"tvshow.tvdb_id", tvdbID, "error", err)
		return d, nil
	}
	d.RatingFetched = true
	if rating != nil {
		d.TMDBID = rating.TMDBID
		d.Rating = rating.Rating
		d.VoteCount = rating.VoteCount
	}
	return d, nil
}
