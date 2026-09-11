package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

func (s *Server) StreamSchedules(
	ctx context.Context,
	_ StreamSchedulesRequestObject,
) (StreamSchedulesResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return StreamSchedules403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return scheduleStream{ctx: ctx, server: s}, nil
}

const (
	// scheduleStreamCoalesce is the least time between two frames. A metadata
	// refresh nudges once per title; without it every nudge would be a
	// scheduled_jobs query and a frame.
	scheduleStreamCoalesce  = 250 * time.Millisecond
	scheduleStreamKeepalive = 15 * time.Second
)

// scheduleStream is the StreamSchedulesResponseObject that writes the event
// stream itself. The generated 200 type copies an io.Reader, which would need
// a pipe and a goroutine to say the same thing.
type scheduleStream struct {
	ctx    context.Context
	server *Server
}

func (st scheduleStream) VisitStreamSchedulesResponse(w http.ResponseWriter) error {
	rc := http.NewResponseController(w)
	// The listener's WriteTimeout is sized for request/response handlers and
	// would cut this connection off two minutes in.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("clear write deadline: %w", err)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	changes, unsubscribe := st.server.scheduler.Subscribe()
	defer unsubscribe()
	keepalive := time.NewTicker(scheduleStreamKeepalive)
	defer keepalive.Stop()

	for {
		items, err := st.server.collectSchedules(st.ctx)
		if err != nil {
			return st.clientGone(err)
		}
		data, err := json.Marshal(ScheduleList{Items: items})
		if err != nil {
			return fmt.Errorf("encode schedules: %w", err)
		}
		if _, err := fmt.Fprintf(
			w,
			"event: schedules\ndata: %s\n\n",
			data,
		); err != nil {
			return st.clientGone(err)
		}
		if err := rc.Flush(); err != nil {
			return st.clientGone(err)
		}

		if err := st.wait(rc, w, changes, keepalive.C); err != nil {
			return st.clientGone(err)
		}
	}
}

// clientGone swallows err once the request context is cancelled. A browser
// leaving the page is how every stream ends, and a query or write that fails
// only because of that is not an error for the handler to report.
func (st scheduleStream) clientGone(err error) error {
	if st.ctx.Err() != nil {
		return nil
	}
	return err
}

// wait blocks until the next frame is due: a change nudge, followed by the
// coalesce window during which further nudges fold into the same frame.
// Keepalives are written from here so a quiet stream still proves it is open.
func (st scheduleStream) wait(
	rc *http.ResponseController,
	w http.ResponseWriter,
	changes <-chan struct{},
	keepalive <-chan time.Time,
) error {
	for {
		select {
		case <-st.ctx.Done():
			return nil
		case <-changes:
			select {
			case <-st.ctx.Done():
				return nil
			case <-time.After(scheduleStreamCoalesce):
			}
			select {
			case <-changes:
			default:
			}
			return nil
		case <-keepalive:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return err
			}
			if err := rc.Flush(); err != nil {
				return err
			}
		}
	}
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
		out.LastFinishedAt = row.LastFinishedAt
		out.LastDurationMs = row.LastDurationMs
		if row.LastError != "" {
			msg := row.LastError
			out.LastError = &msg
		}
	}
	if info.Running && (info.Done > 0 || info.Total > 0) {
		out.Progress = &ScheduleProgress{Done: info.Done, Total: info.Total}
	}
	// last_started_at is derived rather than stored: persisting it cost an
	// UPDATE per run of every job. A run in flight reports the scheduler's
	// live start time; a finished one is its end minus how long it took.
	switch {
	case info.StartedAt != nil:
		out.LastStartedAt = info.StartedAt
	case row != nil && row.LastFinishedAt != nil:
		started := row.LastFinishedAt.Add(
			-time.Duration(row.LastDurationMs) * time.Millisecond,
		)
		out.LastStartedAt = &started
	}
	if !out.Paused && !out.Running && row != nil && row.LastFinishedAt != nil {
		next := row.LastFinishedAt.Add(info.Interval)
		out.NextRunAt = &next
	}
	return out
}

func isUserConfigurable(name string) bool {
	switch name {
	case "movie-rss-sync", "tv-rss-sync",
		"movie-missing-search", "tv-missing-search",
		"movie-metadata-refresh", "tv-metadata-refresh",
		"download-monitor", "import-scan", "cleanup":
		return true
	}
	return false
}

func assignScheduleField(c *config.Config, name, value string) {
	switch name {
	case "movie-rss-sync":
		c.Schedule.MovieRSSSync = value
	case "tv-rss-sync":
		c.Schedule.TVRSSSync = value
	case "movie-missing-search":
		c.Schedule.MovieMissingSearch = value
	case "tv-missing-search":
		c.Schedule.TVMissingSearch = value
	case "movie-metadata-refresh":
		c.Schedule.MovieMetadataRefresh = value
	case "tv-metadata-refresh":
		c.Schedule.TVMetadataRefresh = value
	case "download-monitor":
		c.Schedule.DownloadMonitor = value
	case "import-scan":
		c.Schedule.ImportScan = value
	case "cleanup":
		c.Schedule.Cleanup = value
	}
}
