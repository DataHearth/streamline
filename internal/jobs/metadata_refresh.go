package jobs

import (
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/scheduler"
)

// MetadataRefresh returns a JobFunc that re-fetches TMDB data for movies
// whose update_time is older than the metadata-refresh interval.
func MetadataRefresh(r movie.MetadataRefresher) scheduler.JobFunc {
	return r.RefreshStale
}
