// Package state persists scheduler run state to the scheduled_jobs table.
// It implements scheduler.StateHook plus boot-time helpers for seeding
// rows and replaying paused state into a freshly-started scheduler.
package state

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/scheduledjob"
	"github.com/datahearth/streamline/internal/scheduler"
)

// Hook persists scheduler run lifecycle events to the scheduled_jobs table.
// Errors are logged and swallowed — a misbehaving DB must never crash a
// scheduled job run.
type Hook struct {
	client *ent.Client
}

func NewHook(client *ent.Client) *Hook {
	return &Hook{client: client}
}

var _ scheduler.StateHook = (*Hook)(nil)

func (h *Hook) OnStart(ctx context.Context, name string, startedAt time.Time) {
	_, err := h.client.ScheduledJob.Update().
		Where(scheduledjob.Name(name)).
		SetLastStartedAt(startedAt).
		Save(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.WarnContext(
			ctx,
			"scheduled_job: OnStart update failed",
			"job",
			name,
			"error",
			err,
		)
	}
}

func (h *Hook) OnEnd(
	ctx context.Context,
	name string,
	endedAt time.Time,
	status string,
	runErr error,
	duration time.Duration,
) {
	upd := h.client.ScheduledJob.Update().Where(scheduledjob.Name(name))
	switch status {
	case "skipped":
		upd = upd.SetLastStatus(scheduledjob.LastStatusSkipped)
	case "success":
		upd = upd.SetLastStatus(scheduledjob.LastStatusSuccess).
			SetLastFinishedAt(endedAt).
			SetLastDurationMs(durationMs(duration)).
			SetLastError("")
	case "error":
		errMsg := ""
		if runErr != nil {
			errMsg = runErr.Error()
		}
		upd = upd.SetLastStatus(scheduledjob.LastStatusError).
			SetLastFinishedAt(endedAt).
			SetLastDurationMs(durationMs(duration)).
			SetLastError(errMsg)
	default:
		slog.WarnContext(
			ctx,
			"scheduled_job: unknown OnEnd status",
			"job",
			name,
			"status",
			status,
		)
		return
	}
	if _, err := upd.Save(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.WarnContext(
			ctx,
			"scheduled_job: OnEnd update failed",
			"job",
			name,
			"error",
			err,
		)
	}
}

// Seed inserts a default row for every job that doesn't already have one.
// Existing rows are left untouched. Idempotent across boots.
func Seed(ctx context.Context, client *ent.Client, jobs []scheduler.JobInfo) error {
	for _, j := range jobs {
		exists, err := client.ScheduledJob.Query().
			Where(scheduledjob.Name(j.Name)).
			Exist(ctx)
		if err != nil {
			return fmt.Errorf("scheduled_job: query %q: %w", j.Name, err)
		}
		if exists {
			continue
		}
		if _, err := client.ScheduledJob.Create().
			SetName(j.Name).
			Save(ctx); err != nil {
			return fmt.Errorf("scheduled_job: insert %q: %w", j.Name, err)
		}
	}
	return nil
}

// PausedNames returns the names of every job whose row has paused=true,
// sorted lexically. Used on boot to replay paused state into the scheduler.
func PausedNames(ctx context.Context, client *ent.Client) ([]string, error) {
	rows, err := client.ScheduledJob.Query().
		Where(scheduledjob.Paused(true)).
		Order(ent.Asc(scheduledjob.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("scheduled_job: list paused: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out, nil
}

func durationMs(d time.Duration) uint32 {
	ms := d.Milliseconds()
	if ms < 0 {
		return 0
	}
	if ms > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(ms)
}
