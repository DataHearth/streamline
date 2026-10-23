package jobs

import (
	"context"
	"time"

	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/scheduler"
)

const (
	// selectionBusyInterval is how fast the pass re-checks from inside a run
	// while magnets are still resolving. A magnet downloads everything until
	// its keep-set lands, so the wasted bandwidth is bounded by this.
	selectionBusyInterval = 5 * time.Second
	// selectionBusyBudget caps one run so the scheduler stays in charge of
	// the cadence and a record that somehow never drains cannot spin forever.
	// Exceeding it just defers to the next tick.
	selectionBusyBudget = time.Minute
)

// FileSelection returns a JobFunc that resolves pending magnet file
// selections (spec §4.5) once their metadata becomes available in the
// download client, or gives up past download.selection_grace.
//
// The polling is inside the run rather than in the schedule because the two
// cadences the job wants are far apart. Resolving a magnet wants seconds; an
// install with nothing pending wants nothing at all, and a scheduler tick is
// never free — it commits a WAL frame through the one SQLite connection every
// API request queues behind, which at the resolution cadence is thousands a
// day finding no work. Registered slow, polling fast only while it keeps
// finding pending records, the job costs the fast rate exactly when a magnet
// is in flight and the slow rate otherwise.
func FileSelection(r download.SelectionResolver) scheduler.JobFunc {
	return func(ctx context.Context) error {
		deadline := time.Now().Add(selectionBusyBudget)
		for {
			pending, err := r.RunSelectionPass(ctx)
			if err != nil || pending == 0 || time.Now().After(deadline) {
				return err
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(selectionBusyInterval):
			}
		}
	}
}
