package jobs

import (
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/scheduler"
)

// FileSelection returns a JobFunc that resolves pending magnet file
// selections (spec §4.5) once their metadata becomes available in the
// download client, or gives up past download.selection_grace.
func FileSelection(r download.SelectionResolver) scheduler.JobFunc {
	return r.RunSelectionPass
}
