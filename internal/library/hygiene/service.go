// Package hygiene runs three periodic library jobs:
//
//   - orphan_scan walks library.movie_path and reconciles untracked files
//     against the movies table, auto-importing high-confidence matches and
//     queueing ambiguous ones into the bulk-import review surface.
//   - drift_check confirms every tracked MediaFile is still present on disk
//     and reverts its owning movie or episode to "wanted" once a grace window
//     elapses.
//   - media_probe backfills media info onto MediaFile rows that were created
//     without an inline ffprobe pass (adoption, orphan scan, bulk import).
package hygiene

import (
	"context"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/library/hygiene")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/library/hygiene")

	orphanAutoImported metric.Int64Counter
	orphanQueued       metric.Int64Counter
	orphanSkipped      metric.Int64Counter
	orphanWalkErrors   metric.Int64Counter
	orphanMatchErrors  metric.Int64Counter
	orphanImportFailed metric.Int64Counter

	driftVerified       metric.Int64Counter
	driftDrifted        metric.Int64Counter
	driftReverted       metric.Int64Counter
	driftOrphansDeleted metric.Int64Counter
	driftStatErrors     metric.Int64Counter

	mediaProbeProbed  metric.Int64Counter
	mediaProbeFailed  metric.Int64Counter
	mediaProbeMissing metric.Int64Counter
)

func init() {
	orphanAutoImported = otelx.Must(
		meter.Int64Counter("streamline.hygiene.orphan_scan.auto_imported"),
	)
	orphanQueued = otelx.Must(
		meter.Int64Counter("streamline.hygiene.orphan_scan.queued"),
	)
	orphanSkipped = otelx.Must(
		meter.Int64Counter("streamline.hygiene.orphan_scan.skipped"),
	)
	orphanWalkErrors = otelx.Must(
		meter.Int64Counter("streamline.hygiene.orphan_scan.walk_errors"),
	)
	orphanMatchErrors = otelx.Must(
		meter.Int64Counter("streamline.hygiene.orphan_scan.match_errors"),
	)
	orphanImportFailed = otelx.Must(
		meter.Int64Counter("streamline.hygiene.orphan_scan.auto_import_failed"),
	)

	driftVerified = otelx.Must(
		meter.Int64Counter("streamline.hygiene.drift_check.verified"),
	)
	driftDrifted = otelx.Must(
		meter.Int64Counter("streamline.hygiene.drift_check.drifted"),
	)
	driftReverted = otelx.Must(
		meter.Int64Counter("streamline.hygiene.drift_check.reverted"),
	)
	driftOrphansDeleted = otelx.Must(
		meter.Int64Counter("streamline.hygiene.drift_check.orphans_deleted"),
	)
	driftStatErrors = otelx.Must(
		meter.Int64Counter("streamline.hygiene.drift_check.stat_errors"),
	)

	mediaProbeProbed = otelx.Must(
		meter.Int64Counter("streamline.hygiene.media_probe.probed"),
	)
	mediaProbeFailed = otelx.Must(
		meter.Int64Counter("streamline.hygiene.media_probe.failed"),
	)
	mediaProbeMissing = otelx.Must(
		meter.Int64Counter("streamline.hygiene.media_probe.missing"),
	)

	ctx := context.Background()
	for _, c := range []metric.Int64Counter{
		orphanAutoImported, orphanQueued, orphanSkipped,
		orphanWalkErrors, orphanMatchErrors, orphanImportFailed,
		driftVerified, driftDrifted, driftReverted, driftOrphansDeleted,
		driftStatErrors,
		mediaProbeProbed, mediaProbeFailed, mediaProbeMissing,
	} {
		c.Add(ctx, 0)
	}
}

// Service coordinates orphan_scan, drift_check and media_probe passes against
// the on-disk library and the database. It is stateless aside from its
// dependencies; one instance is shared between the scheduler jobs.
type Service struct {
	store    db.Store
	metadata metadata.Provider
	tvmeta   metadata.TVProvider
	importer library.Importer
	cfg      *config.LibraryConfig

	// Probe is optional: media_probe no-ops when unset, matching the
	// importer's own degrade-without-ffmpeg behavior. Set separately from
	// New because wire.go constructs the shared prober after the Service.
	Probe ffmpeg.Prober
}

func New(
	store db.Store,
	meta metadata.Provider,
	tvmeta metadata.TVProvider,
	importer library.Importer,
	cfg *config.LibraryConfig,
) *Service {
	return &Service{
		store:    store,
		metadata: meta,
		tvmeta:   tvmeta,
		importer: importer,
		cfg:      cfg,
	}
}
