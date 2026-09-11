package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/internal/observability"
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
//
// There is deliberately no OnStart: persisting a start time cost one UPDATE
// per run of every job — roughly half of all writes an idle install makes —
// to record something a reader can have for free. A finished run's start is
// last_finished_at minus last_duration_ms, and a running one's is
// JobInfo.StartedAt, which is live rather than a row that may be a tick stale.
type StateHook interface {
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
	ErrNotStarted       = errors.New("scheduler: not started")

	// errJobPanicked stands in for a job that panicked, so the run is counted
	// and persisted like any other failure instead of vanishing with the
	// process. Not exported: no caller decides anything on it.
	errJobPanicked = errors.New("scheduler: job panicked")
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
	// StartedAt is set only while Running — the start of the run in flight.
	// Nothing persists it: a run that never finishes leaves no trace after a
	// restart, which is the same thing a stale last_started_at column said.
	StartedAt *time.Time
	// Done and Total are the in-flight run's Progress, set only while Running
	// and only for a job that reports one. A job that counts as it goes
	// without knowing the size of the work reports Total 0.
	Done, Total int
}

// snapshot builds a JobInfo. The caller must hold j.mu.
func (j *registeredJob) snapshot() JobInfo {
	info := JobInfo{
		Name:     j.name,
		Interval: j.interval,
		System:   j.system,
		Running:  j.running.Load(),
		Paused:   j.paused,
	}
	if info.Running {
		if ns := j.startedAt.Load(); ns != 0 {
			t := time.Unix(0, ns)
			info.StartedAt = &t
		}
		if p := j.progress.Load(); p != nil {
			info.Done, info.Total = p[0], p[1]
		}
	}
	return info
}

type runKey struct{}

// manualKey marks a run started by RunNow rather than the ticker.
type manualKey struct{}

type run struct {
	s   *Scheduler
	job *registeredJob
}

// Manual reports whether the current run was started by RunNow. A job that
// throttles its own work — a search cooldown, a refresh interval — waives it
// on a manual run: the operator asked for the work now, and a run that walks
// the list and touches nothing is indistinguishable from one that never
// started. False on any ctx a job body did not receive from the scheduler.
func Manual(ctx context.Context) bool {
	manual, _ := ctx.Value(manualKey{}).(bool)
	return manual
}

// Progress records how far the current run has got, for the API to show
// alongside Running. It only does anything on the ctx a job body receives
// from the scheduler; anywhere else it is a no-op, so a service method can
// call it unconditionally whether a job or a request invoked it.
func Progress(ctx context.Context, done, total int) {
	r, ok := ctx.Value(runKey{}).(run)
	if !ok {
		return
	}
	r.job.progress.Store(&[2]int{done, total})
	r.s.notify()
}

// Subscribe returns a channel that receives a nudge whenever any job's
// observable state changes — a run starting, progressing or ending, a pause,
// resume or reschedule. Nudges coalesce: a subscriber that has not drained
// the last one is not sent another, so a slow reader sees "something
// changed" rather than a backlog. The cancel func drops the subscription.
func (s *Scheduler) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

// notify nudges every subscriber. Takes s.mu, so never call it under job.mu:
// List locks s.mu then job.mu, and the reverse order deadlocks.
func (s *Scheduler) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

type registeredJob struct {
	name     string
	interval time.Duration
	fn       JobFunc
	system   bool
	running  atomic.Bool
	// startedAt is the current (or most recent) run's start, in unix nanos.
	// It is what replaces the persisted last_started_at while a job runs.
	startedAt atomic.Int64
	// lastSuccess is the last run that returned no error, in unix seconds, or
	// 0 for a job that has not yet succeeded this process. Read by the
	// last-success gauge; see registerJobGauge for why a counter is not enough.
	lastSuccess atomic.Int64
	// progress is the in-flight run's {done, total}, one pointer so a reader
	// never pairs the done of one Progress call with the total of the next.
	progress atomic.Pointer[[2]int]

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
	subs    map[chan struct{}]struct{}
	hook    StateHook
	rootCtx context.Context
}

func New(opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{
		jobs: make(map[string]*registeredJob),
		subs: make(map[chan struct{}]struct{}),
	}
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
	return job.snapshot(), nil
}

// List returns a snapshot of every registered job, sorted by name.
func (s *Scheduler) List() []JobInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobInfo, 0, len(s.jobs))
	for _, j := range s.jobs {
		j.mu.Lock()
		out = append(out, j.snapshot())
		j.mu.Unlock()
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out
}

// bootStagger spreads the first run of each job at startup. Every job used to
// fire the instant Start ran, which made the boot minute the heaviest period
// the profiler saw — a full metadata sweep, both orphan scans and both missing
// searches at once, all contending for the single SQLite connection while the
// server was also trying to answer its first requests.
const bootStagger = 2 * time.Second

// Start launches every registered job that is not paused. Each job runs once
// shortly after start — staggered, see bootStagger — then repeats at its
// interval. Blocks until ctx is cancelled. Jobs marked paused (via Pause prior
// to Start) stay dormant until Resume is called.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.rootCtx = ctx
	jobs := make([]*registeredJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	s.mu.Unlock()

	s.registerJobGauge(ctx, jobs)
	// Sorted so the spread is reproducible: map order would shuffle which job
	// waits longest on every boot, which makes a slow start hard to read.
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].name < jobs[k].name })

	for i, j := range jobs {
		j.mu.Lock()
		paused := j.paused
		interval := j.interval
		j.mu.Unlock()
		if !paused {
			s.startJob(ctx, j, bootDelay(i, interval))
		}
	}
	<-ctx.Done()
}

// registerJobGauge publishes the last successful run of each job as a unix
// timestamp.
//
// The run counters answer "did it run" only for as long as the backend keeps
// the samples, and say nothing at all once a job stops ticking — which is the
// failure that matters here: a background job (missing search, RSS feed,
// orphan scan) that quietly stops running forever, with no error to count
// because nothing is running to fail. The alert an operator actually wants is
// "orphan_scan has not succeeded in 24h", and that needs a timestamp.
func (s *Scheduler) registerJobGauge(ctx context.Context, jobs []*registeredJob) {
	if len(jobs) == 0 {
		return
	}
	gauge, err := meter.Int64ObservableGauge(
		"streamline.scheduler.job.last_success_unixtime",
		metric.WithDescription(
			"Unix time of a job's last successful run; 0 if never since boot",
		),
		metric.WithUnit("s"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "scheduler last-success gauge unavailable",
			"error", err)
		return
	}
	_, err = meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			for _, j := range jobs {
				o.ObserveInt64(gauge, j.lastSuccess.Load(), metric.WithAttributes(
					attribute.String("job.name", j.name),
				))
			}
			return nil
		},
		gauge,
	)
	if err != nil {
		slog.ErrorContext(ctx, "scheduler last-success gauge not registered",
			"error", err)
	}
}

// bootDelay is how long job number index waits before its first run. Capped at
// half the job's own interval so a frequent job is never held past the point
// where it would have ticked anyway — a 5s job must not sit out 20s of stagger
// meant for hourly ones. The first job always runs immediately.
func bootDelay(index int, interval time.Duration) time.Duration {
	d := time.Duration(index) * bootStagger
	return min(d, interval/2)
}

// root returns the context Start was called with, and whether Start has run.
// Before Start the returned context is context.Background(), so it is always
// safe to log with. Call it before taking a registeredJob lock: List holds
// s.mu while locking job.mu, so the reverse order can deadlock.
func (s *Scheduler) root() (context.Context, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rootCtx == nil {
		return context.Background(), false
	}
	return s.rootCtx, true
}

// startJob assigns a fresh stopCh to job and launches the goroutine. delay
// defers only the first run; the ticker period is unaffected.
func (s *Scheduler) startJob(
	ctx context.Context,
	job *registeredJob,
	delay time.Duration,
) {
	stop := make(chan struct{})
	job.mu.Lock()
	job.stopCh = stop
	interval := job.interval
	job.mu.Unlock()
	go s.runJob(ctx, stop, interval, job, delay)
}

func (s *Scheduler) runJob(
	ctx context.Context,
	stopCh <-chan struct{},
	interval time.Duration,
	job *registeredJob,
	firstDelay time.Duration,
) {
	if firstDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-time.After(firstDelay):
		}
	}
	go s.executeJob(ctx, job)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
			go s.executeJob(ctx, job)
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
		s.notify()
		return
	}
	// Subscribers read the persisted row, so the end nudge waits for the hook.
	defer s.notify()
	defer job.running.Store(false)

	ctx, span := tracer.Start(ctx, "scheduler.job",
		trace.WithAttributes(attribute.String("job.name", job.name)),
	)
	defer span.End()

	start := time.Now()
	job.startedAt.Store(start.UnixNano())
	job.progress.Store(nil)
	s.notify()
	ctx = context.WithValue(ctx, runKey{}, run{s: s, job: job})

	outcome := "success"
	runErr := runGuarded(ctx, job)
	end := time.Now()
	dur := end.Sub(start)

	switch {
	case errors.Is(runErr, errJobPanicked):
		outcome = "panic"
		otelx.RecordSpanError(span, runErr)
	case runErr != nil:
		outcome = "error"
		otelx.RecordSpanError(span, runErr)
		slog.ErrorContext(ctx, "job failed", "job", job.name, "error", runErr)
	default:
		job.lastSuccess.Store(end.Unix())
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

// runGuarded calls a job body with a panic guard.
//
// Every job runs on its own goroutine, so an unrecovered panic in one of them
// — a nil map in an RSS scan, a bad index in a rename — is not that job
// failing, it is the whole process exiting: scheduler, downloads, transcodes
// and HTTP server with it. Converted to an error, the failure stays inside the
// job that caused it and the next tick retries.
func runGuarded(ctx context.Context, job *registeredJob) error {
	var err error
	func() {
		defer observability.RecoverPanic(ctx, "scheduled job "+job.name, func() {
			err = errJobPanicked
		})
		err = job.fn(ctx)
	}()
	return err
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
	rootCtx, _ := s.root()
	defer s.notify()
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
	slog.InfoContext(rootCtx, "scheduler job paused", "job", name)
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
	rootCtx, started := s.root()
	defer s.notify()
	job.mu.Lock()
	defer job.mu.Unlock()
	if !job.paused {
		return ErrJobNotPaused
	}
	job.paused = false
	if started {
		stop := make(chan struct{})
		job.stopCh = stop
		go s.runJob(rootCtx, stop, job.interval, job, 0)
	}
	slog.InfoContext(rootCtx, "scheduler job resumed", "job", name)
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
	rootCtx, _ := s.root()
	defer s.notify()
	job.mu.Lock()
	defer job.mu.Unlock()
	job.interval = interval
	if job.stopCh != nil {
		close(job.stopCh)
		stop := make(chan struct{})
		job.stopCh = stop
		go s.runJob(rootCtx, stop, interval, job, 0)
	}
	slog.InfoContext(
		rootCtx,
		"scheduler job rescheduled",
		"job",
		name,
		"interval",
		interval.String(),
	)
	return nil
}

// RunNow triggers a one-off execution. Returns ErrJobBusy if the job is
// already running, ErrJobSystem on system jobs, ErrNotStarted before Start
// has run, ErrJobUnknown otherwise. Allowed while the job is paused — manual
// override is the whole point. The run is marked for Manual, so a job that
// throttles itself does the work it would otherwise defer.
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
	rootCtx, started := s.root()
	if !started {
		return ErrNotStarted
	}
	if job.running.Load() {
		return ErrJobBusy
	}
	runCtx := context.WithValue(context.WithoutCancel(rootCtx), manualKey{}, true)
	slog.InfoContext(runCtx, "scheduler job run-now triggered", "job", name)
	go s.executeJob(runCtx, job)
	return nil
}

// Controller is the consumer-facing surface used by REST/web handlers.
// *Scheduler implements it.
type Controller interface {
	List() []JobInfo
	Subscribe() (<-chan struct{}, func())
	Get(name string) (JobInfo, error)
	Pause(name string) error
	Resume(name string) error
	Reschedule(name string, interval time.Duration) error
	RunNow(name string) error
}

var _ Controller = (*Scheduler)(nil)
