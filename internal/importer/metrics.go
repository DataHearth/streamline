package importer

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("github.com/datahearth/streamline/internal/importer")

	// outcomes is the only aggregate signal for the import pipeline. The
	// held/failed/terminal transitions were logged and nothing else, so a
	// growing review queue or a spike in terminal failures after a library
	// change could only be found by grepping.
	outcomes = otelx.Must(meter.Int64Counter(
		"streamline.importer.outcomes",
		metric.WithDescription("Import outcomes by result"),
	))

	// dropped counts enqueues thrown away because the channel was full. A
	// backed-up importer degrades silently otherwise: each drop logs one line
	// with nothing aggregating them, so nobody notices until imports go
	// missing.
	dropped = otelx.Must(meter.Int64Counter(
		"streamline.importer.enqueue_dropped",
		metric.WithDescription("Enqueues dropped because the queue was full"),
	))
)

func recordOutcome(ctx context.Context, outcome string) {
	outcomes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
	))
}

// registerQueueGauges publishes queue depth and in-flight count, so "the
// importer is behind" is visible before the drops start.
func (w *Worker) registerQueueGauges(ctx context.Context) {
	queued, err := meter.Int64ObservableGauge(
		"streamline.importer.queued",
		metric.WithDescription("Records waiting in the import queue"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "importer queue gauges unavailable", "error", err)
		return
	}
	inFlight, err := meter.Int64ObservableGauge(
		"streamline.importer.in_flight",
		metric.WithDescription("Records currently being imported"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "importer queue gauges unavailable", "error", err)
		return
	}
	if _, err := meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			o.ObserveInt64(queued, int64(len(w.ch)))
			w.mu.Lock()
			n := len(w.inFlight)
			w.mu.Unlock()
			o.ObserveInt64(inFlight, int64(n))
			return nil
		},
		queued, inFlight,
	); err != nil {
		slog.ErrorContext(ctx, "importer queue gauges not registered", "error", err)
	}
}
