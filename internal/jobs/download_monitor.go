package jobs

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/importer"
	"github.com/datahearth/streamline/internal/scheduler"
)

// DownloadMonitor returns a scheduler.JobFunc that polls the download manager
// and forwards each completion to the importer, then runs the manual-torrent
// adoption pass and enqueues anything it auto-imported. Intended to run on a
// short interval (default 30s).
func DownloadMonitor(
	c download.Checker,
	a download.Adopter,
	imp importer.Enqueuer,
) scheduler.JobFunc {
	return func(ctx context.Context) error {
		completed, err := c.CheckStatus(ctx)
		if err != nil {
			return err
		}
		// Self-heal episodes left "downloading" by a cancelled/lost season pack.
		// Non-fatal: a reconcile failure must not abort the completion pass.
		if err := c.ReconcileEpisodeStatuses(ctx); err != nil {
			slog.WarnContext(ctx, "reconcile episode statuses failed", "error", err)
		}
		for _, cd := range completed {
			slog.InfoContext(
				ctx,
				"download completed, enqueue import",
				"record.id",
				cd.Record.ID,
			)
			imp.Enqueue(cd.Record.ID)
		}
		adopted, err := a.AdoptManualTorrents(ctx)
		if err != nil {
			// Adoption failure must not kill the completion pass.
			slog.WarnContext(ctx, "adopt manual torrents failed", "error", err)
			return nil
		}
		for _, id := range adopted {
			slog.InfoContext(ctx, "adopted manual torrent, enqueue import",
				"record.id", id)
			imp.Enqueue(id)
		}
		return nil
	}
}
