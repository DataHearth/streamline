package bulkimport

import (
	"context"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Bulk import is the most destructive operation in the codebase — one commit
// renames and moves hundreds of files — and it was the one with no counters at
// all, so a rising commit-failure rate could only be found by reading logs
// while every neighbouring subsystem could be graphed.
var (
	meter = otel.Meter(
		"github.com/datahearth/streamline/internal/library/bulkimport",
	)

	scanClassified = otelx.Must(meter.Int64Counter(
		"streamline.bulkimport.scan.classified",
		metric.WithDescription("Scanned entries by classification and media kind"),
	))
	commits = otelx.Must(meter.Int64Counter(
		"streamline.bulkimport.commits",
		metric.WithDescription("Bulk import commit outcomes by media kind"),
	))
)

func countCommit(ctx context.Context, kind, outcome string, n int64) {
	if n <= 0 {
		return
	}
	commits.Add(ctx, n, metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.String("outcome", outcome),
	))
}
