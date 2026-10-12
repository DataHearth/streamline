package jobs

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/internal/cardigann/update"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

// CardigannUpdate returns a scheduler JobFunc bringing the built-in
// indexers' definitions up to upstream. A change to one a configured indexer
// runs that touches its domains or its login waits for an administrator.
func CardigannUpdate() scheduler.JobFunc {
	return func(ctx context.Context) error {
		c := config.Get()
		used := map[string]bool{}
		for _, ix := range c.Indexers {
			if ix.Protocol == "cardigann" && ix.Cardigann != nil {
				used[ix.Cardigann.Definition] = true
			}
		}
		res, err := update.Run(ctx, c.CardigannDir(), otelx.HTTPClient,
			func(file string) bool { return used[file] })
		if err != nil {
			return err
		}
		if res.Warning != "" {
			slog.WarnContext(ctx, "cardigann definitions: "+res.Warning)
		}
		if res.Current {
			slog.DebugContext(ctx, "cardigann definitions are current",
				"upstream.commit", res.Commit)
			return nil
		}
		slog.InfoContext(ctx, "cardigann definitions updated",
			"upstream.commit", res.Commit,
			"changed", len(res.Changed),
			"added", len(res.Added),
			"removed", len(res.Removed),
			"held", res.Held,
		)
		return nil
	}
}
