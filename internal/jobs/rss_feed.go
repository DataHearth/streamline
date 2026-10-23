package jobs

import (
	"github.com/datahearth/streamline/internal/rss"
	"github.com/datahearth/streamline/internal/scheduler"
)

// RSSFeed returns a JobFunc that runs one forward-feed scan over every
// enabled indexer.
func RSSFeed(r rss.FeedRunner) scheduler.JobFunc {
	return r.Run
}
