package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/transcodejob"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// entityGauge pairs a gauge with the count behind it. Registration is
// table-driven because the interesting part of each entry is the query, and
// fifteen hand-written register-then-observe pairs bury it.
type entityGauge struct {
	name  string
	desc  string
	attrs []attribute.KeyValue
	count func(context.Context, *ent.Client) (int, error)
}

// statusAttr labels a gauge with the status it counts, so the flat totals can
// be broken down without inventing a metric name per state.
func statusAttr(v string) []attribute.KeyValue {
	return []attribute.KeyValue{attribute.String("status", v)}
}

// entityGauges is what the collector reports every cycle.
//
// The flat totals answer "how big is the library"; the status-scoped ones
// answer the questions an operator actually alerts on — a request backlog that
// keeps growing, a wanted count that never drains, a transcode queue nothing
// is claiming. Those splits were already computable (CountMoviesByStatus and
// friends exist) but were wired only to the on-demand UI endpoint, so nothing
// could be graphed over time.
var entityGauges = []entityGauge{
	{
		name: "streamline_movies_total",
		desc: "Number of movies tracked by streamline.",
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.Movie.Query().Count(ctx)
		},
	},
	{
		name:  "streamline_movies_by_status",
		desc:  "Movies by library status.",
		attrs: statusAttr(string(movie.StatusWanted)),
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.Movie.Query().
				Where(movie.StatusEQ(movie.StatusWanted)).Count(ctx)
		},
	},
	{
		name: "streamline_tvshows_total",
		desc: "Number of TV shows tracked by streamline.",
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.TVShow.Query().Count(ctx)
		},
	},
	{
		name: "streamline_seasons_total",
		desc: "Number of TV seasons tracked by streamline.",
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.Season.Query().Count(ctx)
		},
	},
	{
		name: "streamline_episodes_total",
		desc: "Number of TV episodes tracked by streamline.",
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.Episode.Query().Count(ctx)
		},
	},
	{
		name:  "streamline_episodes_by_status",
		desc:  "Episodes by library status.",
		attrs: statusAttr(string(episode.StatusWanted)),
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.Episode.Query().
				Where(episode.StatusEQ(episode.StatusWanted)).Count(ctx)
		},
	},
	{
		name:  "streamline_episodes_by_status",
		desc:  "Episodes by library status.",
		attrs: statusAttr(string(episode.StatusDownloading)),
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.Episode.Query().
				Where(episode.StatusEQ(episode.StatusDownloading)).Count(ctx)
		},
	},
	{
		name: "streamline_users_total",
		desc: "Number of users registered in streamline.",
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.User.Query().Count(ctx)
		},
	},
	{
		name: "streamline_requests_total",
		desc: "Number of media requests in streamline.",
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.Request.Query().Count(ctx)
		},
	},
	{
		name:  "streamline_requests_by_status",
		desc:  "Media requests by status.",
		attrs: statusAttr(string(request.StatusPending)),
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.Request.Query().
				Where(request.StatusEQ(request.StatusPending)).Count(ctx)
		},
	},
	{
		name: "streamline_downloads_total",
		desc: "Number of download records in streamline.",
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.DownloadRecord.Query().Count(ctx)
		},
	},
	{
		name:  "streamline_transcode_jobs_by_status",
		desc:  "Transcode jobs by status.",
		attrs: statusAttr(string(transcodejob.StatusQueued)),
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.TranscodeJob.Query().
				Where(transcodejob.StatusEQ(transcodejob.StatusQueued)).
				Count(ctx)
		},
	},
	{
		name:  "streamline_transcode_jobs_by_status",
		desc:  "Transcode jobs by status.",
		attrs: statusAttr(string(transcodejob.StatusRunning)),
		count: func(ctx context.Context, c *ent.Client) (int, error) {
			return c.TranscodeJob.Query().
				Where(transcodejob.StatusEQ(transcodejob.StatusRunning)).
				Count(ctx)
		},
	},
}

// RegisterEntityMetrics registers observable gauges reporting the current
// number of domain entities in the database. The callbacks run on each metric
// collection cycle and query the database directly.
func RegisterEntityMetrics(meter metric.Meter, db *ent.Client) error {
	// One instrument per distinct name; entries sharing a name differ only by
	// attribute, which is how a status split is meant to be modelled.
	instruments := make(map[string]metric.Int64ObservableGauge, len(entityGauges))
	observed := make([]metric.Observable, 0, len(entityGauges))
	for _, g := range entityGauges {
		if _, ok := instruments[g.name]; ok {
			continue
		}
		inst, err := meter.Int64ObservableGauge(
			g.name,
			metric.WithDescription(g.desc),
		)
		if err != nil {
			return fmt.Errorf("register %s gauge: %w", g.name, err)
		}
		instruments[g.name] = inst
		observed = append(observed, inst)
	}

	_, err := meter.RegisterCallback(
		func(ctx context.Context, obs metric.Observer) error {
			for _, g := range entityGauges {
				n, err := g.count(ctx, db)
				if err != nil {
					// Observing nothing leaves the last sample standing, so a
					// database that went away for a cycle reads as a library
					// that stopped changing. Say so rather than going quiet.
					slog.WarnContext(ctx, "entity metric not collected",
						"metric", g.name, "error", err)
					continue
				}
				obs.ObserveInt64(
					instruments[g.name],
					int64(n),
					metric.WithAttributes(g.attrs...),
				)
			}
			return nil
		},
		observed...,
	)
	if err != nil {
		return fmt.Errorf("register callback: %w", err)
	}
	return nil
}
