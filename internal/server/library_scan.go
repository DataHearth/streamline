package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/scheduledjob"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/extension"
	"github.com/datahearth/streamline/internal/scheduler"
)

const libraryScanJob = "drift-check"

// libraryScanner is what an extension holding library_scan reaches: the
// drift-check job, the one that stats every tracked file.
type libraryScanner struct {
	client *ent.Client
	jobs   scheduler.Controller
}

func (l libraryScanner) ScanStatus(
	ctx context.Context,
) (extension.ScanStatus, error) {
	info, err := l.jobs.Get(libraryScanJob)
	if err != nil {
		return extension.ScanStatus{}, fmt.Errorf("read the scan job: %w", err)
	}
	count, err := l.client.Track.Query().
		Where(track.HasMediaFilesWith(mediafile.MissingSinceIsNil())).
		Count(ctx)
	if err != nil {
		return extension.ScanStatus{}, fmt.Errorf("count tracks: %w", err)
	}
	st := extension.ScanStatus{Scanning: info.Running, Count: int64(count)}
	row, err := l.client.ScheduledJob.Query().
		Where(scheduledjob.Name(libraryScanJob)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
	case err != nil:
		return extension.ScanStatus{}, fmt.Errorf("read the scan state: %w", err)
	default:
		st.LastScan = row.LastFinishedAt
	}
	return st, nil
}

func (l libraryScanner) StartScan(context.Context) error {
	if err := l.jobs.RunNow(libraryScanJob); err != nil &&
		!errors.Is(err, scheduler.ErrJobBusy) {
		return fmt.Errorf("start the scan: %w", err)
	}
	return nil
}
