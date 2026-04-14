package jobs

import (
	"github.com/datahearth/streamline/internal/rss"
	"github.com/datahearth/streamline/internal/scheduler"
)

// MissingSearch returns a JobFunc that runs one missing-search pass: per-title
// indexer queries against every wanted movie past cooldown.
func MissingSearch(r rss.MissingSearchRunner) scheduler.JobFunc {
	return r.Run
}
