package db

import (
	"context"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"go.opentelemetry.io/otel/metric"
)

// RegisterEntityMetrics registers observable gauges reporting the current
// number of domain entities in the database. The callbacks run on each metric
// collection cycle and query the database directly.
func RegisterEntityMetrics(meter metric.Meter, db *ent.Client) error {
	movies, err := meter.Int64ObservableGauge(
		"streamline_movies_total",
		metric.WithDescription("Number of movies tracked by streamline."),
	)
	if err != nil {
		return fmt.Errorf("register movies gauge: %w", err)
	}

	tvshows, err := meter.Int64ObservableGauge(
		"streamline_tvshows_total",
		metric.WithDescription("Number of TV shows tracked by streamline."),
	)
	if err != nil {
		return fmt.Errorf("register tvshows gauge: %w", err)
	}

	seasons, err := meter.Int64ObservableGauge(
		"streamline_seasons_total",
		metric.WithDescription("Number of TV seasons tracked by streamline."),
	)
	if err != nil {
		return fmt.Errorf("register seasons gauge: %w", err)
	}

	episodes, err := meter.Int64ObservableGauge(
		"streamline_episodes_total",
		metric.WithDescription("Number of TV episodes tracked by streamline."),
	)
	if err != nil {
		return fmt.Errorf("register episodes gauge: %w", err)
	}

	users, err := meter.Int64ObservableGauge(
		"streamline_users_total",
		metric.WithDescription("Number of users registered in streamline."),
	)
	if err != nil {
		return fmt.Errorf("register users gauge: %w", err)
	}

	requests, err := meter.Int64ObservableGauge(
		"streamline_requests_total",
		metric.WithDescription("Number of media requests in streamline."),
	)
	if err != nil {
		return fmt.Errorf("register requests gauge: %w", err)
	}

	downloads, err := meter.Int64ObservableGauge(
		"streamline_downloads_total",
		metric.WithDescription("Number of download records in streamline."),
	)
	if err != nil {
		return fmt.Errorf("register downloads gauge: %w", err)
	}

	_, err = meter.RegisterCallback(
		func(ctx context.Context, obs metric.Observer) error {
			if n, e := db.Movie.Query().Count(ctx); e == nil {
				obs.ObserveInt64(movies, int64(n))
			}
			if n, e := db.TVShow.Query().Count(ctx); e == nil {
				obs.ObserveInt64(tvshows, int64(n))
			}
			if n, e := db.Season.Query().Count(ctx); e == nil {
				obs.ObserveInt64(seasons, int64(n))
			}
			if n, e := db.Episode.Query().Count(ctx); e == nil {
				obs.ObserveInt64(episodes, int64(n))
			}
			if n, e := db.User.Query().Count(ctx); e == nil {
				obs.ObserveInt64(users, int64(n))
			}
			if n, e := db.Request.Query().Count(ctx); e == nil {
				obs.ObserveInt64(requests, int64(n))
			}
			if n, e := db.DownloadRecord.Query().Count(ctx); e == nil {
				obs.ObserveInt64(downloads, int64(n))
			}
			return nil
		},
		movies, tvshows, seasons, episodes, users, requests, downloads,
	)
	if err != nil {
		return fmt.Errorf("register callback: %w", err)
	}
	return nil
}
