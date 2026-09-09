package bittorrent

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("github.com/datahearth/streamline/internal/bittorrent")

	seedStops = otelx.Must(meter.Int64Counter(
		"streamline.bittorrent.seed_stops",
		metric.WithDescription(
			"Torrents whose upload was stopped by a seed ratio or time limit",
		),
	))
)

// registerEngineGauges publishes the live view of the swarm the engine already
// computes for the UI.
//
// live() has per-torrent speeds, peer counts and status on every call, but the
// only consumer was the REST endpoint the SPA polls — so "are all torrents
// stalled", "how much is this box actually moving" and "is the engine even
// running" were answerable from a browser and from nowhere else. An alerting
// stack watching OTel had nothing.
func (e *Engine) registerEngineGauges(ctx context.Context) {
	active, err := meter.Int64ObservableGauge(
		"streamline.bittorrent.active_torrents",
		metric.WithDescription("Torrents currently downloading or seeding"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "torrent gauges unavailable", "error", err)
		return
	}
	stalled, err := meter.Int64ObservableGauge(
		"streamline.bittorrent.stalled_torrents",
		metric.WithDescription("Incomplete torrents with no connected peers"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "torrent gauges unavailable", "error", err)
		return
	}
	downRate, err := meter.Int64ObservableGauge(
		"streamline.bittorrent.download_rate",
		metric.WithDescription("Aggregate download rate across all torrents"),
		metric.WithUnit("By/s"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "torrent gauges unavailable", "error", err)
		return
	}
	upRate, err := meter.Int64ObservableGauge(
		"streamline.bittorrent.upload_rate",
		metric.WithDescription("Aggregate upload rate across all torrents"),
		metric.WithUnit("By/s"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "torrent gauges unavailable", "error", err)
		return
	}

	if _, err := meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			var torrents, stalledCount, down, up int64
			for _, t := range e.client.Torrents() {
				l := e.live(t)
				torrents++
				down += l.downloadSpeed
				up += l.uploadSpeed
				if l.progress < 1 && l.activePeers == 0 {
					stalledCount++
				}
			}
			o.ObserveInt64(active, torrents)
			o.ObserveInt64(stalled, stalledCount)
			o.ObserveInt64(downRate, down)
			o.ObserveInt64(upRate, up)
			return nil
		},
		active, stalled, downRate, upRate,
	); err != nil {
		slog.ErrorContext(ctx, "torrent gauges not registered", "error", err)
	}
}
