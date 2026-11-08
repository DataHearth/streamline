package jobs

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/internal/cardigann/update"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

// CardigannUpdate returns a scheduler JobFunc reading upstream's Cardigann
// definitions into the catalog and moving the built-in indexers to the new
// version of theirs. A change that touches an indexer's domains or its
// login waits for an administrator.
func CardigannUpdate() scheduler.JobFunc {
	return func(ctx context.Context) error {
		res, err := update.Run(ctx, otelx.HTTPClient)
		if err != nil {
			return err
		}
		if res.Warning != "" {
			slog.WarnContext(ctx, "cardigann definitions: "+res.Warning)
		}
		if res.Current && len(res.Applied) == 0 && len(res.Held) == 0 {
			slog.DebugContext(ctx, "cardigann definitions are current",
				"upstream.commit", res.Commit)
			return nil
		}
		slog.InfoContext(ctx, "cardigann definitions updated",
			"upstream.commit", res.Commit,
			"applied", res.Applied,
			"held", res.Held,
		)
		return nil
	}
}
