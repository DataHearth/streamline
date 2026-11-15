package jobs

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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
//
// extensions names each running extension's database file; every one is
// snapshotted into <dir>/extensions/<name> under the same retention, and one
// that fails is logged without failing the job.
func DatabaseBackup(
	dbPath, dir string,
	interval func() time.Duration,
	extensions func() map[string]string,
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
		if extensions != nil {
			backUpExtensions(ctx, dir, keep, extensions())
		}
		return nil
	}
}

func backUpExtensions(
	ctx context.Context,
	dir string,
	keep int,
	paths map[string]string,
) {
	for name, path := range paths {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		extDir := filepath.Join(dir, "extensions", name)
		b, err := dbbackup.SnapshotFile(ctx, path, extDir, dbbackup.KindScheduled)
		if err != nil {
			slog.WarnContext(ctx, "database-backup: extension backup failed",
				"extension", name, "error", err)
			continue
		}
		slog.InfoContext(ctx, "database-backup: extension database backed up",
			"extension", name, "backup.path", b.Path, "backup.size_bytes", b.Size)
		if _, err := dbbackup.Prune(
			extDir,
			dbbackup.KindScheduled,
			keep,
		); err != nil {
			slog.WarnContext(ctx, "database-backup: extension prune failed",
				"extension", name, "error", err)
		}
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
