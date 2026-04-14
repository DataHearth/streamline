package restapi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/scheduledjob"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/scheduler"
)

const minScheduleInterval = 10 * time.Second

func (s *Server) ListSchedules(
	ctx context.Context,
	_ ListSchedulesRequestObject,
) (ListSchedulesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListSchedules403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	out, err := s.collectSchedules(ctx)
	if err != nil {
		return nil, err
	}
	return ListSchedules200JSONResponse{
		ScheduleListJSONResponse: ScheduleListJSONResponse{Items: out},
	}, nil
}

func (s *Server) GetSchedule(
	ctx context.Context,
	req GetScheduleRequestObject,
) (GetScheduleResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetSchedule403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	out, err := s.collectSchedules(ctx)
	if err != nil {
		return nil, err
	}
	for _, sch := range out {
		if sch.Name == req.Name {
			return GetSchedule200JSONResponse{
				ScheduleJSONResponse: ScheduleJSONResponse(sch),
			}, nil
		}
	}
	return GetSchedule404JSONResponse{
		NotFoundJSONResponse: errNotFound("schedule not found"),
	}, nil
}

func (s *Server) UpdateSchedule(
	ctx context.Context,
	req UpdateScheduleRequestObject,
) (UpdateScheduleResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateSchedule403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if req.Body == nil {
		return UpdateSchedule422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable("missing body"),
		}, nil
	}
	d, err := time.ParseDuration(req.Body.Interval)
	if err != nil {
		return UpdateSchedule422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				fmt.Sprintf("invalid interval: %v", err),
			),
		}, nil
	}
	if d < minScheduleInterval {
		return UpdateSchedule422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				"interval must be at least 10s",
			),
		}, nil
	}
	if !isUserConfigurable(req.Name) {
		for _, j := range s.scheduler.List() {
			if j.Name == req.Name && j.System {
				return UpdateSchedule403JSONResponse{
					ForbiddenJSONResponse: forbiddenResp(
						"system jobs cannot be edited",
					),
				}, nil
			}
		}
		return UpdateSchedule404JSONResponse{
			NotFoundJSONResponse: errNotFound("schedule not found"),
		}, nil
	}
	if err := config.Update(ctx, func(c *config.Config) error {
		assignScheduleField(c, req.Name, req.Body.Interval)
		return nil
	}); err != nil {
		if configLocked(err) {
			return UpdateSchedule403JSONResponse{
				ForbiddenJSONResponse: forbiddenResp(err.Error()),
			}, nil
		}
		return nil, fmt.Errorf("config update: %w", err)
	}
	if err := s.scheduler.Reschedule(req.Name, d); err != nil {
		return nil, fmt.Errorf("reschedule: %w", err)
	}
	sch, err := s.findSchedule(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	return UpdateSchedule200JSONResponse{
		ScheduleJSONResponse: ScheduleJSONResponse(sch),
	}, nil
}

func (s *Server) PauseSchedule(
	ctx context.Context,
	req PauseScheduleRequestObject,
) (PauseScheduleResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return PauseSchedule403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := s.scheduler.Pause(req.Name); {
	case err == nil:
	case errors.Is(err, scheduler.ErrJobUnknown):
		return PauseSchedule404JSONResponse{
			NotFoundJSONResponse: errNotFound("schedule not found"),
		}, nil
	case errors.Is(err, scheduler.ErrJobSystem):
		return PauseSchedule403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp("system jobs cannot be paused"),
		}, nil
	case errors.Is(err, scheduler.ErrJobAlreadyPaused):
		return PauseSchedule409JSONResponse{
			ConflictJSONResponse: errConflict("job already paused"),
		}, nil
	default:
		return nil, err
	}
	if _, err := s.ent.ScheduledJob.Update().
		Where(scheduledjob.Name(req.Name)).
		SetPaused(true).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("persist paused: %w", err)
	}
	sch, err := s.findSchedule(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	return PauseSchedule200JSONResponse{
		ScheduleJSONResponse: ScheduleJSONResponse(sch),
	}, nil
}

func (s *Server) ResumeSchedule(
	ctx context.Context,
	req ResumeScheduleRequestObject,
) (ResumeScheduleResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ResumeSchedule403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := s.scheduler.Resume(req.Name); {
	case err == nil:
	case errors.Is(err, scheduler.ErrJobUnknown):
		return ResumeSchedule404JSONResponse{
			NotFoundJSONResponse: errNotFound("schedule not found"),
		}, nil
	case errors.Is(err, scheduler.ErrJobSystem):
		return ResumeSchedule403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp("system jobs cannot be resumed"),
		}, nil
	case errors.Is(err, scheduler.ErrJobNotPaused):
		return ResumeSchedule409JSONResponse{
			ConflictJSONResponse: errConflict("job not paused"),
		}, nil
	default:
		return nil, err
	}
	if _, err := s.ent.ScheduledJob.Update().
		Where(scheduledjob.Name(req.Name)).
		SetPaused(false).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("persist resumed: %w", err)
	}
	sch, err := s.findSchedule(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	return ResumeSchedule200JSONResponse{
		ScheduleJSONResponse: ScheduleJSONResponse(sch),
	}, nil
}

func (s *Server) RunSchedule(
	ctx context.Context,
	req RunScheduleRequestObject,
) (RunScheduleResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return RunSchedule403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := s.scheduler.RunNow(req.Name); {
	case err == nil:
	case errors.Is(err, scheduler.ErrJobUnknown):
		return RunSchedule404JSONResponse{
			NotFoundJSONResponse: errNotFound("schedule not found"),
		}, nil
	case errors.Is(err, scheduler.ErrJobSystem):
		return RunSchedule403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp("system jobs cannot be triggered"),
		}, nil
	case errors.Is(err, scheduler.ErrJobBusy):
		return RunSchedule409JSONResponse{
			ConflictJSONResponse: errConflict("job is currently running"),
		}, nil
	default:
		return nil, err
	}
	sch, err := s.findSchedule(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	return RunSchedule200JSONResponse{
		ScheduleJSONResponse: ScheduleJSONResponse(sch),
	}, nil
}

func (s *Server) collectSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.ent.ScheduledJob.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query scheduled_jobs: %w", err)
	}
	byName := make(map[string]*ent.ScheduledJob, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}
	infos := s.scheduler.List()
	out := make([]Schedule, 0, len(infos))
	for _, i := range infos {
		out = append(out, buildSchedule(i, byName[i.Name]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Server) findSchedule(ctx context.Context, name string) (Schedule, error) {
	info, err := s.scheduler.Get(name)
	if err != nil {
		return Schedule{}, fmt.Errorf("scheduler get %q: %w", name, err)
	}
	row, err := s.ent.ScheduledJob.Query().Where(scheduledjob.Name(name)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return Schedule{}, fmt.Errorf("query scheduled_job %q: %w", name, err)
	}
	return buildSchedule(info, row), nil
}

func buildSchedule(info scheduler.JobInfo, row *ent.ScheduledJob) Schedule {
	out := Schedule{
		Name:           info.Name,
		Interval:       info.Interval.String(),
		System:         info.System,
		Running:        info.Running,
		Status:         ScheduleStatus("never"),
		LastDurationMs: 0,
	}
	if row != nil {
		out.Paused = row.Paused
		out.Status = ScheduleStatus(row.LastStatus)
		out.LastStartedAt = row.LastStartedAt
		out.LastFinishedAt = row.LastFinishedAt
		out.LastDurationMs = int32(row.LastDurationMs)
		if row.LastError != "" {
			msg := row.LastError
			out.LastError = &msg
		}
	}
	if !out.Paused && !out.Running {
		switch {
		case row != nil && row.LastFinishedAt != nil:
			next := row.LastFinishedAt.Add(info.Interval)
			out.NextRunAt = &next
		case row != nil && row.LastStartedAt != nil:
			next := row.LastStartedAt.Add(info.Interval)
			out.NextRunAt = &next
		}
	}
	return out
}

func isUserConfigurable(name string) bool {
	switch name {
	case "rss-sync", "missing-search", "metadata-refresh",
		"download-monitor", "import-scan", "cleanup":
		return true
	}
	return false
}

func assignScheduleField(c *config.Config, name, value string) {
	switch name {
	case "rss-sync":
		c.Schedule.RSSSync = value
	case "missing-search":
		c.Schedule.MissingSearch = value
	case "metadata-refresh":
		c.Schedule.MetadataRefresh = value
	case "download-monitor":
		c.Schedule.DownloadMonitor = value
	case "import-scan":
		c.Schedule.ImportScan = value
	case "cleanup":
		c.Schedule.Cleanup = value
	}
}
