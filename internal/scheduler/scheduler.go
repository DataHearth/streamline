package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/scheduler")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/scheduler")

	jobRuns     metric.Int64Counter
	jobDuration metric.Float64Histogram
	jobSkipped  metric.Int64Counter
)

func init() {
	jobRuns = otelx.Must(meter.Int64Counter(
		"streamline.scheduler.job.runs",
		metric.WithDescription("Job executions by name + outcome"),
	))
	jobDuration = otelx.Must(meter.Float64Histogram(
		"streamline.scheduler.job.duration",
		metric.WithDescription("Job execution duration"),
		metric.WithUnit("s"),
	))
	jobSkipped = otelx.Must(meter.Int64Counter(
		"streamline.scheduler.job.skipped",
		metric.WithDescription(
			"Job ticks skipped because previous run still active",
		),
	))

	ctx := context.Background()
	jobRuns.Add(ctx, 0)
	jobSkipped.Add(ctx, 0)
	jobDuration.Record(ctx, 0)
}

type JobFunc func(ctx context.Context) error

// StateHook receives lifecycle events for every job run. Implementations
// must be safe for concurrent calls and must not block long; errors are
// logged and otherwise ignored — a misbehaving hook never stops a run.
type StateHook interface {
	OnStart(ctx context.Context, name string, startedAt time.Time)
	OnEnd(
		ctx context.Context,
		name string,
		endedAt time.Time,
		status string,
		runErr error,
		duration time.Duration,
	)
}

// Error sentinels surfaced by Pause / Resume / Reschedule / RunNow.
var (
	ErrJobUnknown       = errors.New("scheduler: unknown job")
	ErrJobSystem        = errors.New("scheduler: job is read-only (system)")
	ErrJobAlreadyPaused = errors.New("scheduler: job already paused")
	ErrJobNotPaused     = errors.New("scheduler: job not paused")
	ErrJobBusy          = errors.New("scheduler: job currently running")
)

// Option mutates a registered job at registration time.
type Option func(*registeredJob)

// WithSystem marks the job as a read-only system job. UI/API reject
// Pause/Resume/Reschedule/RunNow on system jobs.
func WithSystem() Option {
	return func(j *registeredJob) { j.system = true }
}

// SchedulerOption configures the scheduler at construction time.
type SchedulerOption func(*Scheduler)

// WithStateHook installs a StateHook for run lifecycle events. A nil hook
// is a no-op.
func WithStateHook(h StateHook) SchedulerOption {
	return func(s *Scheduler) { s.hook = h }
}

// JobInfo is a snapshot of a registered job.
type JobInfo struct {
	Name     string
	Interval time.Duration
	System   bool
	Running  bool
	Paused   bool
}

type registeredJob struct {
	name     string
	interval time.Duration
	fn       JobFunc
	system   bool
	running  atomic.Bool

	// mu guards interval, paused, and stopCh.
	mu     sync.Mutex
	paused bool
	// stopCh is non-nil iff a runJob goroutine is currently scheduling
	// ticks for this job. Pause closes and nils it; Resume creates a new
	// one and launches the goroutine. Before Start runs, stopCh is always
	// nil regardless of paused state.
	stopCh chan struct{}
}

// Scheduler runs registered jobs on fixed intervals.
// Each job runs in its own goroutine. If a job is still running
// when its next tick fires, the tick is skipped.
type Scheduler struct {
	mu      sync.Mutex
	jobs    map[string]*registeredJob
	hook    StateHook
	rootCtx context.Context
}

func New(opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{jobs: make(map[string]*registeredJob)}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register adds a job. Must be called before Start. Re-registering the same
// name overwrites the previous entry.
func (s *Scheduler) Register(
	name string,
	interval time.Duration,
	fn JobFunc,
	opts ...Option,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := &registeredJob{name: name, interval: interval, fn: fn}
	for _, opt := range opts {
		opt(j)
	}
	s.jobs[name] = j
}

// Get returns a snapshot of the named job. Returns ErrJobUnknown if absent.
func (s *Scheduler) Get(name string) (JobInfo, error) {
	job, err := s.job(name)
	if err != nil {
		return JobInfo{}, err
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return JobInfo{
		Name:     job.name,
		Interval: job.interval,
		System:   job.system,
		Running:  job.running.Load(),
		Paused:   job.paused,
	}, nil
}

// List returns a snapshot of every registered job, sorted by name.
func (s *Scheduler) List() []JobInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobInfo, 0, len(s.jobs))
	for _, j := range s.jobs {
		j.mu.Lock()
		out = append(out, JobInfo{
			Name:     j.name,
			Interval: j.interval,
			System:   j.system,
			Running:  j.running.Load(),
			Paused:   j.paused,
		})
		j.mu.Unlock()
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out
}

// Start launches every registered job that is not paused. Each job runs
// immediately once, then repeats at its interval. Blocks until ctx is
// cancelled. Jobs marked paused (via Pause prior to Start) stay dormant
// until Resume is called.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.rootCtx = ctx
	jobs := make([]*registeredJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	s.mu.Unlock()
	for _, j := range jobs {
		j.mu.Lock()
		paused := j.paused
		j.mu.Unlock()
		if !paused {
			s.startJob(j)
		}
	}
	<-ctx.Done()
}

// startJob assigns a fresh stopCh to job and launches the goroutine.
func (s *Scheduler) startJob(job *registeredJob) {
	stop := make(chan struct{})
	job.mu.Lock()
	job.stopCh = stop
	interval := job.interval
	job.mu.Unlock()
	go s.runJob(stop, interval, job)
}

func (s *Scheduler) runJob(
	stopCh <-chan struct{},
	interval time.Duration,
	job *registeredJob,
) {
	go s.executeJob(s.rootCtx, job)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.rootCtx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
			go s.executeJob(s.rootCtx, job)
		}
	}
}

func (s *Scheduler) executeJob(ctx context.Context, job *registeredJob) {
	if !job.running.CompareAndSwap(false, true) {
		jobSkipped.Add(ctx, 1, metric.WithAttributes(
			attribute.String("job.name", job.name),
		))
		slog.DebugContext(ctx, "job still running, skipping", "job", job.name)
		if s.hook != nil {
			s.hook.OnEnd(ctx, job.name, time.Now(), "skipped", nil, 0)
		}
		return
	}
	defer job.running.Store(false)

	ctx, span := tracer.Start(ctx, "scheduler.job",
		trace.WithAttributes(attribute.String("job.name", job.name)),
	)
	defer span.End()

	start := time.Now()
	if s.hook != nil {
		s.hook.OnStart(ctx, job.name, start)
	}

	outcome := "success"
	runErr := job.fn(ctx)
	end := time.Now()
	dur := end.Sub(start)

	if runErr != nil {
		outcome = "error"
		otelx.RecordSpanError(span, runErr)
		slog.ErrorContext(ctx, "job failed", "job", job.name, "error", runErr)
	} else {
		slog.DebugContext(ctx, "job completed", "job", job.name)
	}

	attrs := metric.WithAttributes(
		attribute.String("job.name", job.name),
		attribute.String("outcome", outcome),
	)
	jobDuration.Record(ctx, dur.Seconds(), attrs)
	jobRuns.Add(ctx, 1, attrs)

	if s.hook != nil {
		s.hook.OnEnd(ctx, job.name, end, outcome, runErr, dur)
	}
}

func (s *Scheduler) job(name string) (*registeredJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[name]
	if !ok {
		return nil, ErrJobUnknown
	}
	return j, nil
}

// Pause stops scheduling new ticks for name. An in-flight run completes
// naturally. Safe to call before Start (the job stays dormant when Start
// runs). Returns ErrJobUnknown / ErrJobSystem / ErrJobAlreadyPaused.
func (s *Scheduler) Pause(name string) error {
	job, err := s.job(name)
	if err != nil {
		return err
	}
	if job.system {
		return ErrJobSystem
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.paused {
		return ErrJobAlreadyPaused
	}
	job.paused = true
	if job.stopCh != nil {
		close(job.stopCh)
		job.stopCh = nil
	}
	slog.InfoContext(s.rootCtx, "scheduler job paused", "job", name)
	return nil
}

// Resume re-arms a paused job. If Start has already run, a fresh goroutine
// is launched. Otherwise the paused flag is cleared and Start will pick the
// job up. Returns ErrJobUnknown / ErrJobSystem / ErrJobNotPaused.
func (s *Scheduler) Resume(name string) error {
	job, err := s.job(name)
	if err != nil {
		return err
	}
	if job.system {
		return ErrJobSystem
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if !job.paused {
		return ErrJobNotPaused
	}
	job.paused = false
	if s.rootCtx != nil {
		stop := make(chan struct{})
		job.stopCh = stop
		go s.runJob(stop, job.interval, job)
	}
	slog.InfoContext(s.rootCtx, "scheduler job resumed", "job", name)
	return nil
}

// Reschedule updates the job's interval and restarts its goroutine. If the
// job is paused or Start has not yet been called, only the interval is
// updated — the next active run picks it up.
func (s *Scheduler) Reschedule(name string, interval time.Duration) error {
	job, err := s.job(name)
	if err != nil {
		return err
	}
	if job.system {
		return ErrJobSystem
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	job.interval = interval
	if job.stopCh != nil {
		close(job.stopCh)
		stop := make(chan struct{})
		job.stopCh = stop
		go s.runJob(stop, interval, job)
	}
	slog.InfoContext(
		s.rootCtx,
		"scheduler job rescheduled",
		"job",
		name,
		"interval",
		interval.String(),
	)
	return nil
}

// RunNow triggers a one-off execution. Returns ErrJobBusy if the job is
// already running, ErrJobSystem on system jobs, ErrJobUnknown otherwise.
// Allowed while the job is paused — manual override is the whole point.
//
// The job runs on a context derived from the scheduler's root context with
// the caller's cancel signal detached, so a short HTTP request timeout
// doesn't kill a long-running job.
func (s *Scheduler) RunNow(name string) error {
	job, err := s.job(name)
	if err != nil {
		return err
	}
	if job.system {
		return ErrJobSystem
	}
	if job.running.Load() {
		return ErrJobBusy
	}
	runCtx := context.WithoutCancel(s.rootCtx)
	slog.InfoContext(runCtx, "scheduler job run-now triggered", "job", name)
	go s.executeJob(runCtx, job)
	return nil
}

// Controller is the consumer-facing surface used by REST/web handlers.
// *Scheduler implements it.
type Controller interface {
	List() []JobInfo
	Get(name string) (JobInfo, error)
	Pause(name string) error
	Resume(name string) error
	Reschedule(name string, interval time.Duration) error
	RunNow(name string) error
}

var _ Controller = (*Scheduler)(nil)
