package transcoding

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/transcoding")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/transcoding")

	jobsTotal      metric.Int64Counter
	jobDuration    metric.Float64Histogram
	encodeDuration metric.Float64Histogram
	bytesSaved     metric.Int64Counter

	errJobPanicked = errors.New("transcode job panicked")
	errNoPolicy    = errors.New("no transcode policy")
	errNoOwner     = errors.New("transcode job has no media file")
	errNoConfig    = errors.New("config not loaded")
	// errAfterSwap marks a failure that happened once the library file had
	// already been replaced, which is what makes the job terminal: a retry
	// would re-probe a source that no longer exists.
	errAfterSwap = errors.New("the library file was already replaced")

	// ErrScanRunning is returned by Scan while an earlier call's goroutine is
	// still walking the library.
	ErrScanRunning = errors.New("transcode scan already running")
)

func init() {
	jobsTotal = otelx.Must(meter.Int64Counter(
		"streamline.transcoding.jobs",
		metric.WithDescription("Transcode jobs by outcome"),
	))
	jobDuration = otelx.Must(meter.Float64Histogram(
		"streamline.transcoding.duration",
		metric.WithDescription(
			"Transcode job duration, encode through verification and swap",
		),
		metric.WithUnit("s"),
	))
	// Split out because jobDuration covers verification too, and VMAF alone
	// runs up to three extra decode passes — "how long does this codec take to
	// encode" was not answerable from a number that includes them.
	encodeDuration = otelx.Must(meter.Float64Histogram(
		"streamline.transcoding.encode_duration",
		metric.WithDescription("ffmpeg encode duration, verification excluded"),
		metric.WithUnit("s"),
	))
	bytesSaved = otelx.Must(meter.Int64Counter(
		"streamline.transcoding.bytes_saved",
		metric.WithDescription("Bytes reclaimed by completed transcodes"),
		metric.WithUnit("By"),
	))

	ctx := context.Background()
	jobsTotal.Add(ctx, 0)
	jobDuration.Record(ctx, 0)
	bytesSaved.Add(ctx, 0)
}

// tempMarker names the half-written output while ffmpeg is filling it. It sits
// in the library beside the file it replaces (a rename across filesystems is a
// copy) and is what recover sweeps after a crash.
const tempMarker = ".streamline-tmp."

// pollInterval is how often an idle worker looks for queued work.
const pollInterval = 5 * time.Second // ponytail: DB poll, no wake plumbing; add an enqueue signal if latency ever matters

// MediaServerRefresher asks the configured media servers to rescan a library
// root — a transcode rewrites a file in place, so Plex/Jellyfin/Emby have to
// re-read it or they keep serving the old stream details.
type MediaServerRefresher interface {
	RefreshAll(ctx context.Context, kind, libraryPath string) error
}

type Deps struct {
	DB          db.Store
	Prober      ffmpeg.Prober
	MediaServer MediaServerRefresher
}

type Worker struct {
	db     db.Store
	prober ffmpeg.Prober
	ms     MediaServerRefresher

	// wake carries "a slot freed" so a finished job claims the next one
	// immediately instead of waiting out the poll interval. Buffered at one:
	// the signal is a level, not a count — a pending wake already says
	// "look again", so a second one has nothing to add and is dropped.
	wake chan struct{}
	jobs sync.WaitGroup

	mu       sync.Mutex
	progress map[uint32]Snapshot
	cancels  map[uint32]context.CancelFunc
	running  int

	// hwMu is its own lock because the probe runs ffmpeg for up to
	// hwProbeTimeout and nothing under mu may wait that long.
	hwMu sync.Mutex
	hw   hwProbe

	// scanning guards Scan against a second retroactive-library scan running
	// concurrently with the first.
	scanning atomic.Bool
}

func NewWorker(deps Deps) *Worker {
	return &Worker{
		db:       deps.DB,
		prober:   deps.Prober,
		ms:       deps.MediaServer,
		wake:     make(chan struct{}, 1),
		progress: make(map[uint32]Snapshot),
		cancels:  make(map[uint32]context.CancelFunc),
	}
}

// Start recovers whatever a previous process left mid-flight, then claims work
// until ctx is canceled. It returns only once every running job has stopped.
func (w *Worker) Start(ctx context.Context) {
	if err := w.recover(ctx); err != nil {
		slog.ErrorContext(ctx, "transcode recovery failed", "error", err)
	}
	w.registerConcurrencyGauge(ctx)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			w.jobs.Wait()
			return
		}
		w.fill(ctx)
		select {
		case <-ctx.Done():
			w.jobs.Wait()
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

// registerConcurrencyGauge publishes encodes in flight against the ceiling
// they are competing for. Queue depth comes from the job rows (see
// RegisterEntityMetrics); what only the worker knows is how many of its slots
// are actually busy, which is the difference between "saturated" and "stalled"
// when the queue is not draining.
func (w *Worker) registerConcurrencyGauge(ctx context.Context) {
	running, err := meter.Int64ObservableGauge(
		"streamline.transcoding.running",
		metric.WithDescription("Encodes currently in flight"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "transcode running gauge unavailable",
			"error", err)
		return
	}
	limit, err := meter.Int64ObservableGauge(
		"streamline.transcoding.max_concurrent",
		metric.WithDescription("Configured ceiling on concurrent encodes"),
	)
	if err != nil {
		slog.ErrorContext(ctx, "transcode ceiling gauge unavailable",
			"error", err)
		return
	}
	if _, err := meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			o.ObserveInt64(running, int64(w.active()))
			if cfg := config.Get(); cfg != nil {
				o.ObserveInt64(limit, int64(cfg.Transcoding.MaxConcurrent))
			}
			return nil
		},
		running, limit,
	); err != nil {
		slog.ErrorContext(ctx, "transcode concurrency gauges not registered",
			"error", err)
	}
}

// fill claims jobs until the queue is empty or every slot is busy, running
// each in its own goroutine.
func (w *Worker) fill(ctx context.Context) {
	for {
		cfg := config.Get()
		// Read live rather than once at Start: enabled and max_concurrent are
		// runtime-editable, and a worker holding the boot-time values would
		// keep encoding after an operator turned the feature off.
		if !w.enabled(cfg) || w.active() >= int(cfg.Transcoding.MaxConcurrent) {
			return
		}
		c, err := w.claim(ctx)
		if err != nil || c == nil {
			return
		}
		w.jobs.Go(func() {
			defer w.release()
			w.runJob(ctx, c)
		})
	}
}

// tick claims one job and runs it inline, reporting whether it found one. It
// is fill's synchronous counterpart, for callers that want the job finished
// before they continue.
func (w *Worker) tick(ctx context.Context) bool {
	if !w.enabled(config.Get()) {
		return false
	}
	c, err := w.claim(ctx)
	if err != nil || c == nil {
		return false
	}
	defer w.release()
	w.runJob(ctx, c)
	return true
}

func (w *Worker) enabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Transcoding.Enabled && cfg.FFmpeg.Enabled &&
		w.prober.FFmpegPath() != ""
}

// Ready reports whether this worker would claim work right now. A scan that
// queues rows the worker will never claim is worse than a refusal — nothing
// drains them and nothing says why.
func (w *Worker) Ready() bool { return w.enabled(config.Get()) }

// claimed is a job this process has taken, with the context that cancels it.
type claimed struct {
	job    *ent.TranscodeJob
	ctx    context.Context
	cancel context.CancelFunc
}

// claim takes the next queued job, books a slot for it and registers its
// cancel — all three under the one lock, so a job is cancelable from the
// instant its row says running. Registering in runJob instead left a window
// where Cancel took the not-running branch: the row went canceled while the
// encode carried on and completed over it.
func (w *Worker) claim(ctx context.Context) (*claimed, error) {
	job, err := w.db.ClaimNextTranscodeJob(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "could not claim a transcode job", "error", err)
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	jctx, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	w.running++
	w.cancels[job.ID] = cancel
	w.mu.Unlock()
	return &claimed{job: job, ctx: jctx, cancel: cancel}, nil
}

func (w *Worker) release() {
	w.mu.Lock()
	w.running--
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) active() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// Progress reports the last snapshot emitted by a job running in this process.
func (w *Worker) Progress(id uint32) (Snapshot, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	snap, ok := w.progress[id]
	return snap, ok
}

// Cancel stops a job: running here, its context is canceled and runJob marks
// the row; otherwise the row is marked directly, which is also how a queued
// job is dropped before anyone claims it.
func (w *Worker) Cancel(ctx context.Context, id uint32) error {
	w.mu.Lock()
	cancel, running := w.cancels[id]
	w.mu.Unlock()
	if running {
		cancel()
		return nil
	}
	return w.db.MarkTranscodeJobCanceled(ctx, id)
}

func (w *Worker) runJob(ctx context.Context, c *claimed) {
	jctx, span := tracer.Start(c.ctx, "transcoding.run")
	defer span.End()
	defer c.cancel()

	job := c.job
	defer func() {
		w.mu.Lock()
		delete(w.cancels, job.ID)
		delete(w.progress, job.ID)
		w.mu.Unlock()
	}()

	// Three contexts, and they are not interchangeable. jctx dies with the
	// job's own cancel and is what ffmpeg runs under; wctx keeps the span but
	// not that cancellation, because a canceled job still has a row to mark;
	// ctx is the worker's, and only its Err means the process is shutting down.
	wctx := context.WithoutCancel(jctx)

	// A panic here would otherwise kill the process — the encode runs on its
	// own goroutine with nothing above it — and leave the row `running`, which
	// only the next boot's recover would clear. Terminal rather than retried:
	// the same input panicking again is a loop, and an operator can retry the
	// row by hand once the cause is fixed.
	defer observability.RecoverPanic(wctx, "transcode job", func() {
		w.failTerminal(wctx, job, errJobPanicked)
	})

	mf := job.Edges.MediaFile
	if mf == nil {
		w.failTerminal(wctx, job, errNoOwner)
		return
	}
	span.SetAttributes(attribute.Int64("media_file.id", int64(mf.ID)))

	pol := policyFor(mf)
	if pol == nil {
		w.failTerminal(wctx, job, errNoPolicy)
		return
	}

	info, err := w.prober.Probe(jctx, mf.Path)
	if err != nil {
		// Shutdown first: it cancels jctx too, and canceled is a terminal
		// state nothing retries and the dedupe ignores — a file recorded that
		// way on the way down would never be transcoded again. Leaving the row
		// running hands it to the next boot's recover.
		if ctx.Err() != nil {
			return
		}
		if jctx.Err() != nil {
			w.markCanceled(wctx, job)
			return
		}
		w.fail(wctx, job, fmt.Errorf("probe source: %w", err))
		return
	}

	action, reason := Evaluate(mf.Path, info, *pol)
	span.SetAttributes(
		attribute.String("transcode.action", actionName(action)),
		attribute.String("transcode.reason", reason),
	)
	if action == ActionNone {
		if err := w.db.CompleteTranscodeJob(
			wctx,
			job.ID,
			mf.Size,
			mf.Size,
		); err != nil {
			slog.ErrorContext(wctx, "could not complete a no-op transcode job",
				"transcode.job_id", job.ID, "error", err)
			return
		}
		record(wctx, "noop")
		return
	}

	outPath := swapPath(mf.Path, pol.To.Container)
	defer func() {
		if err := os.Remove(outPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.DebugContext(wctx, "could not remove the transcode temp file",
				"file.path", outPath, "error", err)
		}
	}()

	started := time.Now()
	args := BuildArgs(mf.Path, outPath, info, *pol, action, w.hardware(jctx))
	_, err = run(
		jctx,
		w.prober.FFmpegPath(),
		args,
		time.Duration(info.DurationSec)*time.Second,
		func(snap Snapshot) {
			w.mu.Lock()
			w.progress[job.ID] = snap
			w.mu.Unlock()
		},
	)
	if err != nil {
		// A cancel kills ffmpeg, so it always surfaces as a run error too —
		// the context is what tells the three apart, and both cancellations
		// are checked before the failure path so neither spends an attempt.
		// Shutdown leaves the row running on purpose: the next boot's recover
		// requeues it, and nothing can be written through a dead context.
		if ctx.Err() != nil {
			return
		}
		if jctx.Err() != nil {
			w.markCanceled(wctx, job)
			return
		}
		w.fail(wctx, job, err)
		return
	}
	encodeDuration.Record(wctx, time.Since(started).Seconds())

	// Verification runs on wctx so a cancel arriving mid-probe doesn't read as
	// a corrupt output; the opt-in exec checks inside it run on jctx instead,
	// since they launch ffmpeg and jctx is what a cancel can actually kill.
	// The checkpoints below are where a kill-induced verdict, and a genuine
	// one, both land.
	out, err := w.verify(wctx, jctx, outPath, mf.Path, info, mf.Size, action, *pol)
	rej, isRejection := errors.AsType[*rejection](err)
	if err != nil && !isRejection {
		// vmafScore doesn't reclassify a killed exec the way the health check
		// does, so a cancel or shutdown mid-window surfaces here as a plain
		// error rather than as rej — the same two checks the probe and encode
		// arms already run, for the same reason: neither cancellation may be
		// spent as a failed attempt.
		if ctx.Err() != nil {
			return
		}
		if jctx.Err() != nil {
			w.markCanceled(wctx, job)
			return
		}
		w.fail(wctx, job, err)
		return
	}

	// Shutdown ahead of the job's own cancel, for the reason the probe arm
	// states: canceled is terminal, and a process going down must not spend
	// the file's only chance at being transcoded. A rejection produced by a
	// check that shutdown itself killed is exactly as moot as any other
	// verdict recorded on the way down, so this guard covers it too.
	if ctx.Err() != nil {
		return
	}
	// The last point a cancel can be honoured: nothing has moved yet, so the
	// deferred remove drops the encode and the library file is untouched. Past
	// the swap the work is done and a cancel is simply too late — completing it
	// is what keeps the row and the bytes on disk saying the same thing. A
	// rejection is no exception: a cancel killing a health check or a VMAF
	// pass produces the same *exec.ExitError a real failure would, and the
	// operator asked for canceled, not a verdict.
	if jctx.Err() != nil {
		w.markCanceled(wctx, job)
		return
	}

	// An encode outlives plenty of writes to its own row: a replace import, a
	// rename, a re-identify. The encode then holds the *old* bytes, and on a
	// replace that renders the same name the swap would rename them over the
	// file that replaced them. Re-reading the row here is what catches that —
	// and the cascade means the row can also simply be gone. A rejection about
	// those stale bytes is exactly as stale, so this guard runs ahead of it too.
	if err := w.stillCurrent(wctx, mf); err != nil {
		slog.WarnContext(wctx, "dropped a transcode whose file changed under it",
			"transcode.job_id", job.ID,
			"media_file.id", mf.ID,
			"media_file.path", mf.Path,
			"error", err,
		)
		w.markCanceled(wctx, job)
		return
	}

	if rej != nil {
		w.reject(wctx, job, mf, rej)
		return
	}

	finalPath := strings.TrimSuffix(mf.Path, filepath.Ext(mf.Path)) +
		"." + pol.To.Container
	sizeAfter, err := swap(wctx, outPath, finalPath, mf.Path)
	if err != nil {
		if errors.Is(err, errAfterSwap) {
			w.abandon(wctx, job, mf, finalPath, err)
			return
		}
		w.fail(wctx, job, err)
		return
	}

	if err := w.db.UpdateMediaFileAfterTranscode(
		wctx, mf.ID, finalPath, sizeAfter, mf.Size, pol.To.Container, out,
	); err != nil {
		w.abandon(wctx, job, mf, finalPath,
			fmt.Errorf("%w: record transcode outcome: %w", errAfterSwap, err))
		return
	}
	if err := w.db.CompleteTranscodeJob(
		wctx, job.ID, mf.Size, sizeAfter,
	); err != nil {
		slog.ErrorContext(wctx, "could not complete a transcode job",
			"transcode.job_id", job.ID, "error", err)
		return
	}

	record(wctx, "succeeded")
	recordEvent(wctx, mf, events.TypeTranscodeCompleted, map[string]any{
		"path":        finalPath,
		"size_before": mf.Size,
		"size_after":  sizeAfter,
		"container":   pol.To.Container,
	})
	jobDuration.Record(wctx, time.Since(started).Seconds())
	if saved := mf.Size - sizeAfter; saved > 0 {
		bytesSaved.Add(wctx, saved)
	}

	w.refresh(wctx, mf)
	slog.InfoContext(wctx, "transcoded file",
		"media_file.path", finalPath,
		"transcode.reason", reason,
		"transcode.size_before", mf.Size,
		"transcode.size_after", sizeAfter,
	)
}

// verify judges the encode before anything is swapped. The probe it read is
// returned because the row is written from it — the encode's own streams,
// rather than a hole the media-probe backfill has to fill before the file can
// be scored again. A *rejection is a verdict on the output and terminal; any
// other error is the job's and retries. The probe and the size stat run on
// ctx (uncancellable, so a cancel arriving mid-probe never reads as a corrupt
// output); the health check and VMAF pass launch ffmpeg and run on execCtx
// instead, so a cancel — or shutdown — can actually kill them.
func (w *Worker) verify(
	ctx, execCtx context.Context,
	outPath, srcPath string,
	src *ffmpeg.Info,
	srcSize int64,
	action Action,
	pol config.TranscodePolicy,
) (*ffmpeg.Info, error) {
	out, err := w.prober.Probe(ctx, outPath)
	if err != nil {
		return nil, fmt.Errorf("output verification: %w", err)
	}
	st, err := os.Stat(outPath)
	if err != nil {
		return nil, fmt.Errorf("output verification: %w", err)
	}
	cfg := config.Get()
	if cfg == nil {
		return nil, errNoConfig
	}
	v := cfg.Transcoding.Verify
	if err := checkOutput(
		src,
		out,
		srcSize,
		st.Size(),
		action,
		pol.To.Container,
		v,
	); err != nil {
		return nil, err
	}
	total := time.Duration(src.DurationSec) * time.Second
	if v.HealthCheck {
		if err := healthCheck(
			execCtx,
			w.prober.FFmpegPath(),
			outPath,
			total,
			func(Snapshot) {},
		); err != nil {
			// A verdict on the output needs ffmpeg to have actually run and
			// exited non-zero; a failure to launch it at all (missing binary,
			// broken pipe) is the job's own and must stay retryable.
			if _, ok := errors.AsType[*exec.ExitError](err); !ok {
				return nil, fmt.Errorf("output verification: %w", err)
			}
			first, _, _ := strings.Cut(
				strings.TrimPrefix(err.Error(), "ffmpeg: "),
				"\n",
			)
			// The exit status prefix is noise here; the first stderr line names the frame.
			if _, after, ok := strings.Cut(first, ": "); ok {
				first = after
			}
			return nil, &rejection{
				reason:  "decode check failed: " + first,
				outSize: st.Size(),
			}
		}
	}
	if action == ActionTranscode && v.MinVMAF != 0 {
		score, ok, err := vmafScore(
			execCtx,
			w.prober.FFmpegPath(),
			outPath,
			srcPath,
			src.DurationSec,
		)
		if err != nil {
			return nil, fmt.Errorf("output verification: %w", err)
		}
		if ok && score < float64(v.MinVMAF) {
			return nil, &rejection{
				reason:  fmt.Sprintf("VMAF %.1f below %d", score, v.MinVMAF),
				outSize: st.Size(),
			}
		}
	}
	return out, nil
}

// reject parks the job on a verdict about its output. Terminal without
// counting an attempt: the same encode gives the same file, so nothing here
// is worth retrying on its own, and the retry endpoint is the operator's
// way of asking again after changing the policy or the band.
func (w *Worker) reject(
	ctx context.Context,
	job *ent.TranscodeJob,
	mf *ent.MediaFile,
	rej *rejection,
) {
	if err := w.db.RejectTranscodeJob(
		ctx, job.ID, rej.Error(), mf.Size, rej.outSize,
	); err != nil {
		slog.ErrorContext(ctx, "could not record a rejected transcode job",
			"transcode.job_id", job.ID, "error", err)
		return
	}
	recordEvent(ctx, mf, events.TypeTranscodeRejected, map[string]any{
		"path":        mf.Path,
		"reason":      rej.Error(),
		"size_before": mf.Size,
		"size_after":  rej.outSize,
	})
	slog.WarnContext(ctx, "transcode output rejected",
		"transcode.job_id", job.ID,
		"media_file.path", mf.Path,
		"transcode.size_before", mf.Size,
		"transcode.size_after", rej.outSize,
		"transcode.reason", rej.reason,
	)
	otelx.RecordSpanError(trace.SpanFromContext(ctx), rej)
	record(ctx, "rejected")
}

// stillCurrent reports whether the row the job was claimed with still names
// the file the encode was made from. Path and size together are what a replace
// import moves: the same rendered name over different bytes changes only the
// size, and a rename or re-identify changes only the path.
func (w *Worker) stillCurrent(ctx context.Context, mf *ent.MediaFile) error {
	got, err := w.db.FindMediaFileByID(ctx, mf.ID)
	if err != nil {
		return fmt.Errorf("re-read media_file %d: %w", mf.ID, err)
	}
	if got.Path != mf.Path {
		return fmt.Errorf("path moved to %s", got.Path)
	}
	if got.Size != mf.Size {
		return fmt.Errorf("size changed to %d bytes", got.Size)
	}
	return nil
}

// swap moves the finished encode over the library file and reports the new
// size. A container change lands on a new path, so the source is removed
// separately — that removal failing is not worth failing an import-complete
// job over, since the row already points at the file that exists.
func swap(ctx context.Context, outPath, finalPath, srcPath string) (int64, error) {
	// A container change lands the encode on a path the source does not hold,
	// and os.Rename destroys whatever is already there without a word. A
	// multi-file movie is enough for that path to belong to another row, and
	// nothing about this job licenses deleting it — so the job fails instead,
	// retryably, with the path in the reason.
	if finalPath != srcPath {
		switch _, err := os.Lstat(finalPath); {
		case err == nil:
			return 0, fmt.Errorf(
				"swap in the transcoded file: %s already exists", finalPath,
			)
		case !errors.Is(err, fs.ErrNotExist):
			return 0, fmt.Errorf(
				"swap in the transcoded file: stat %s: %w", finalPath, err,
			)
		}
	}
	if err := os.Rename(outPath, finalPath); err != nil {
		return 0, fmt.Errorf("swap in the transcoded file: %w", err)
	}
	if finalPath != srcPath {
		if err := os.Remove(srcPath); err != nil {
			slog.WarnContext(ctx, "could not remove the replaced source file",
				"file.path", srcPath, "error", err)
		}
	}
	st, err := os.Stat(finalPath)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: stat the transcoded file: %w", errAfterSwap, err,
		)
	}
	return st.Size(), nil
}

func (w *Worker) refresh(ctx context.Context, mf *ent.MediaFile) {
	cfg := config.Get()
	if cfg == nil {
		return
	}
	kind, root := "series", cfg.Library.SeriesPath
	if mf.Edges.Movie != nil {
		kind, root = "movie", cfg.Library.MoviePath
	}
	if err := w.ms.RefreshAll(ctx, kind, root); err != nil {
		slog.WarnContext(
			ctx,
			"could not refresh the media servers after a transcode",
			"media.kind",
			kind,
			"library.path",
			root,
			"error",
			err,
		)
	}
}

// recordEvent files a transcode outcome against the movie or episode the job's
// media file belongs to. A job whose file or owner edge is missing — the
// errNoOwner path, which is exactly the failure of having no owner — records
// nothing rather than inventing a scope.
func recordEvent(
	ctx context.Context,
	mf *ent.MediaFile,
	t events.Type,
	payload map[string]any,
) {
	if mf == nil {
		return
	}
	var (
		scope events.Scope
		id    uint32
	)
	switch {
	case mf.Edges.Movie != nil:
		scope, id = events.ScopeMovie, mf.Edges.Movie.ID
	case mf.Edges.Episode != nil:
		scope, id = events.ScopeEpisode, mf.Edges.Episode.ID
	default:
		return
	}
	if err := events.Record(ctx, nil, t, scope, id, payload); err != nil {
		slog.WarnContext(ctx, "could not record a transcode event",
			"media_file.id", mf.ID, "event.type", string(t), "error", err)
	}
}

func (w *Worker) markCanceled(ctx context.Context, job *ent.TranscodeJob) {
	err := w.db.MarkTranscodeJobCanceled(ctx, job.ID)
	// Not cancelable means an operator already marked the row and the running
	// job is only now noticing — the outcome asked for, not a failure.
	if err != nil && !errors.Is(err, db.ErrTranscodeJobNotCancelable) {
		slog.ErrorContext(ctx, "could not mark a transcode job canceled",
			"transcode.job_id", job.ID, "error", err)
		return
	}
	record(ctx, "canceled")
}

func (w *Worker) fail(ctx context.Context, job *ent.TranscodeJob, cause error) {
	cfg := config.Get()
	w.failWith(
		ctx,
		job,
		cause,
		cfg != nil && job.Attempts >= cfg.Transcoding.MaxFailures,
	)
}

func (w *Worker) failTerminal(
	ctx context.Context,
	job *ent.TranscodeJob,
	cause error,
) {
	w.failWith(ctx, job, cause, true)
}

// abandon fails a job terminally after the encode has already replaced the
// library file. Another attempt cannot succeed — the source it would re-probe
// is gone — and on an extension change the media_file row still names the
// deleted path, so this needs an operator rather than a retry.
func (w *Worker) abandon(
	ctx context.Context,
	job *ent.TranscodeJob,
	mf *ent.MediaFile,
	finalPath string,
	cause error,
) {
	//nolint:sloglint // LogAttrs takes slog.Attr by API design
	slog.LogAttrs(ctx, observability.LevelCritical,
		"transcoded file was swapped in but the library row was not updated",
		slog.Uint64("media_file.id", uint64(mf.ID)),
		slog.String("media_file.path", mf.Path),
		slog.String("transcode.final_path", finalPath),
		slog.String("error", cause.Error()),
	)
	w.failTerminal(ctx, job, cause)
}

func (w *Worker) failWith(
	ctx context.Context,
	job *ent.TranscodeJob,
	cause error,
	terminal bool,
) {
	if err := w.db.FailTranscodeJob(
		ctx, job.ID, cause.Error(), terminal,
	); err != nil {
		slog.ErrorContext(ctx, "could not record a failed transcode job",
			"transcode.job_id", job.ID, "error", err)
		return
	}
	slog.WarnContext(ctx, "transcode job failed",
		"transcode.job_id", job.ID,
		"transcode.attempts", job.Attempts,
		"transcode.terminal", terminal,
		"error", cause,
	)
	otelx.RecordSpanError(trace.SpanFromContext(ctx), cause)
	if terminal {
		record(ctx, "failed")
		recordEvent(ctx, job.Edges.MediaFile, events.TypeTranscodeFailed,
			map[string]any{
				"reason":   cause.Error(),
				"attempts": job.Attempts,
			})
	}
}

// recover restores the queue after a process that died mid-encode: jobs stuck
// running are requeued, and the half-written outputs they left are swept.
func (w *Worker) recover(ctx context.Context) error {
	n, err := w.db.ResetRunningTranscodeJobs(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		slog.InfoContext(
			ctx,
			"requeued transcode jobs left running by a previous process",
			"transcode.jobs",
			n,
		)
	}

	cfg := config.Get()
	if cfg == nil {
		return errNoConfig
	}
	var errs []error
	for _, root := range []string{cfg.Library.MoviePath, cfg.Library.SeriesPath} {
		if root == "" {
			continue
		}
		errs = append(errs, sweepTempFiles(root))
	}
	return errors.Join(errs...)
}

func sweepTempFiles(dir string) error {
	// os.Root rather than a plain walk: the paths come off the filesystem
	// rather than from the DB, and a root-scoped handle is what keeps a
	// symlink planted in the library from turning this sweep into a delete
	// anywhere on the host.
	root, err := os.OpenRoot(dir)
	if err != nil {
		// A library root that isn't mounted yet is not this job's problem;
		// pathmigrate.WarnOnDrift is what reports a missing root.
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open library root %s: %w", dir, err)
	}
	defer root.Close()

	err = fs.WalkDir(
		root.FS(),
		".",
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.Contains(d.Name(), tempMarker) {
				return nil
			}
			if err := root.Remove(
				path,
			); err != nil &&
				!errors.Is(err, fs.ErrNotExist) {
				return err
			}
			return nil
		},
	)
	if err != nil {
		return fmt.Errorf("sweep transcode temp files under %s: %w", dir, err)
	}
	return nil
}

// policyFor resolves the transcode policy of the profile the file's owner is
// on. Nil for a file with no owner, an unresolvable profile, or a profile that
// carries no transcode block.
func policyFor(mf *ent.MediaFile) *config.TranscodePolicy {
	var profile string
	switch {
	case mf.Edges.Movie != nil:
		profile = mf.Edges.Movie.QualityProfile
	case mf.Edges.Episode != nil &&
		mf.Edges.Episode.Edges.Season != nil &&
		mf.Edges.Episode.Edges.Season.Edges.TvShow != nil:
		profile = mf.Edges.Episode.Edges.Season.Edges.TvShow.QualityProfile
	default:
		return nil
	}
	entry, ok := config.ResolveQualityProfile(profile)
	if !ok {
		return nil
	}
	return entry.Transcode
}

func swapPath(path, container string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + tempMarker + container
}

func actionName(a Action) string {
	switch a {
	case ActionRemux:
		return "remux"
	case ActionTranscode:
		return "transcode"
	default:
		return "none"
	}
}

// record counts a finished job and stamps the outcome on the job's span.
//
// Every terminal path funnels through here, which is why the span marking
// lives here too: transcoding.run is opened in runJob and closed by a defer,
// and nothing between them touched it, so a job that failed, was rejected or
// was canceled still reported OK. Filtering a tracing backend for error spans
// found no failing encodes at all.
func record(ctx context.Context, outcome string) {
	jobsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
	))
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("transcode.outcome", outcome))
	if outcome == "failed" || outcome == "rejected" {
		span.SetStatus(codes.Error, outcome)
	}
}
