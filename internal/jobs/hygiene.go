package jobs

import (
	"context"
	"time"

	"github.com/datahearth/streamline/internal/library/hygiene"
	"github.com/datahearth/streamline/internal/scheduler"
)

// OrphanScan returns a JobFunc that runs one orphan_scan pass: walk
// library.movie_path, classify untracked media files, auto-import
// high-confidence matches, and queue the rest for human review.
func OrphanScan(s *hygiene.Service) scheduler.JobFunc {
	return s.RunOrphanScan
}

// SeriesOrphanScan returns a JobFunc that runs one series_orphan_scan pass:
// walk library.series_path, classify untracked show folders against TVDB, and
// queue them into the Imports review surface for adoption.
func SeriesOrphanScan(s *hygiene.Service) scheduler.JobFunc {
	return s.RunSeriesOrphanScan
}

// DriftCheck returns a JobFunc that runs one drift_check pass against the
// tracked MediaFile rows. interval is forwarded into the service so the
// grace window scales with the schedule cadence.
func DriftCheck(s *hygiene.Service, interval time.Duration) scheduler.JobFunc {
	return func(ctx context.Context) error {
		return s.RunDriftCheck(ctx, interval)
	}
}
