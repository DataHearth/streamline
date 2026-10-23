package jobs

import (
	"context"
	"errors"

	"github.com/datahearth/streamline/internal/rss"
	"github.com/datahearth/streamline/internal/scheduler"
)

// RunnerFunc adapts a method with the Run shape, such as a vertical's
// SearchMissing, to rss.MissingSearchRunner.
type RunnerFunc func(ctx context.Context) error

func (f RunnerFunc) Run(ctx context.Context) error { return f(ctx) }

// MissingSearch returns a JobFunc that runs one missing-search pass per
// runner, in order: per-title indexer queries against every wanted row past
// cooldown. The errors are joined so one vertical failing does not starve the
// others.
func MissingSearch(runners ...rss.MissingSearchRunner) scheduler.JobFunc {
	return func(ctx context.Context) error {
		errs := make([]error, 0, len(runners))
		for _, r := range runners {
			errs = append(errs, r.Run(ctx))
		}
		return errors.Join(errs...)
	}
}
