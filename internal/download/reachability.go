package download

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// setReachable records the outcome of the last real call made to a download
// client. AdoptManualTorrents lists every enabled client on every monitor
// tick, so that sweep doubles as the reachability probe and no extra request
// is made on the metrics path.
func (d *download) setReachable(name string, ok bool) {
	d.reachable.Store(name, ok)
}

// registerClientGauges publishes per-client reachability.
//
// Reachability was only ever measured by the Settings page's "Test connection"
// button — a thing a person presses. The two sweeps that touch every client
// every tick logged their failures at debug and moved on, so a download client
// that had been unreachable for hours looked exactly like one nobody had
// asked about. This turns the sweep it already does into a signal.
//
// Registered from the first sweep rather than from New, which has no context
// to log a registration failure against.
func (d *download) registerClientGauges(ctx context.Context) {
	gauge, err := meter.Int64ObservableGauge(
		"streamline.download.client_reachable",
		metric.WithDescription(
			"1 when the last call to a download client succeeded, 0 when it failed",
		),
	)
	if err != nil {
		slog.ErrorContext(ctx, "download client gauge unavailable", "error", err)
		return
	}
	if _, err := meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			d.reachable.Range(func(k, v any) bool {
				name, _ := k.(string)
				up, _ := v.(bool)
				var val int64
				if up {
					val = 1
				}
				o.ObserveInt64(gauge, val, metric.WithAttributes(
					attribute.String("client", name),
				))
				return true
			})
			return nil
		},
		gauge,
	); err != nil {
		slog.ErrorContext(ctx, "download client gauge not registered", "error", err)
	}
}
