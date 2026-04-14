package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/scheduler"
)

// Cleanup returns a JobFunc that purges old download_records and movie
// events past their retention windows. The download purge runs first; an
// events-purge failure does not abort the tick — it logs and continues.
func Cleanup(c download.Cleaner) scheduler.JobFunc {
	return func(ctx context.Context) error {
		if err := c.PurgeOldRecords(ctx); err != nil {
			return err
		}
		if err := c.PurgeOrphanedTorrents(ctx); err != nil {
			slog.WarnContext(ctx, "cleanup: purge orphaned torrents failed",
				"error", err)
		}
		retention, err := time.ParseDuration(config.Get().Events.Retention)
		if err != nil {
			slog.WarnContext(ctx, "cleanup: invalid events.retention",
				"value", config.Get().Events.Retention, "error", err)
			return nil
		}
		n, err := events.PurgeOldEvents(ctx, retention)
		if err != nil {
			slog.WarnContext(ctx, "cleanup: purge old events failed",
				"error", err)
			return nil
		}
		if n > 0 {
			slog.InfoContext(ctx, "cleanup: purged old events", "count", n)
		}
		return nil
	}
}
