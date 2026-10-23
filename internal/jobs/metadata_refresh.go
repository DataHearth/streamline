package jobs

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/scheduler"
)

// MetadataRefresh returns a JobFunc that re-fetches provider data for every
// refresher's stale rows, in order. The errors are joined so one vertical
// failing does not starve the others.
func MetadataRefresh(refreshers ...movie.MetadataRefresher) scheduler.JobFunc {
	return func(ctx context.Context) error {
		errs := make([]error, 0, len(refreshers))
		for _, r := range refreshers {
			errs = append(errs, r.RefreshStale(ctx))
		}
		return errors.Join(errs...)
	}
}
