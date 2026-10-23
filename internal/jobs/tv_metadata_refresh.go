package jobs

import (
	"context"

	"github.com/datahearth/streamline/internal/scheduler"
)

// TVMetadataRefresher re-pulls TVDB metadata for stale shows.
type TVMetadataRefresher interface {
	RefreshStale(ctx context.Context) error
}

// TVMetadataRefresh returns a JobFunc that re-fetches TVDB data for every
// tracked series.
func TVMetadataRefresh(r TVMetadataRefresher) scheduler.JobFunc {
	return r.RefreshStale
}
