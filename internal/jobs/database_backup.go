package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/dbbackup"
	"github.com/datahearth/streamline/internal/scheduler"
)

// DatabaseBackup returns a JobFunc that snapshots the database into the
// backups directory and prunes the scheduled backups past
// backup.retention.scheduled.
//
// Every job runs once at boot, and again whenever it is resumed or retimed,
// so an automatic run finding a scheduled backup taken within the last 90% of
// the interval skips: otherwise each restart would take one, and a pod
// restarting a few times in a day would rotate out the week of backups the
// retention promises. The slack keeps an ordinary tick, landing a moment short
// of a full interval after the last backup, from skipping too. interval reads
// the job's current interval, since a retime does not rebuild the JobFunc. A
// manual run always takes one.
func DatabaseBackup(
	dbPath, dir string,
	interval func() time.Duration,
) scheduler.JobFunc {
	return func(ctx context.Context) error {
		if !scheduler.Manual(ctx) {
			window := interval() / 10 * 9
			if last, ok := newestScheduled(dir); ok && time.Since(last) < window {
				slog.DebugContext(
					ctx,
					"database-backup: a recent backup exists, skipping",
					"backup.created_at",
					last,
				)
				return nil
			}
		}
		b, err := dbbackup.SnapshotFile(ctx, dbPath, dir, dbbackup.KindScheduled)
		if err != nil {
			return err
		}
		slog.InfoContext(ctx, "database-backup: database backed up",
			"backup.path", b.Path, "backup.size_bytes", b.Size)

		keep := int(config.Get().Backup.Retention.Scheduled)
		removed, err := dbbackup.Prune(dir, dbbackup.KindScheduled, keep)
		if err != nil {
			slog.WarnContext(ctx, "database-backup: prune failed", "error", err)
		}
		if len(removed) > 0 {
			slog.InfoContext(ctx, "database-backup: pruned old backups",
				"removed", removed)
		}
		return nil
	}
}

func newestScheduled(dir string) (time.Time, bool) {
	all, err := dbbackup.List(dir)
	if err != nil {
		return time.Time{}, false
	}
	for _, b := range all {
		if b.Kind == dbbackup.KindScheduled {
			return b.CreatedAt, true
		}
	}
	return time.Time{}, false
}
