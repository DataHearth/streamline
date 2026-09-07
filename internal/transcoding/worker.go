package transcoding

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer = otel.Tracer("github.com/datahearth/streamline/internal/transcoding")
	meter  = otel.Meter("github.com/datahearth/streamline/internal/transcoding")

	jobsTotal   metric.Int64Counter
	jobDuration metric.Float64Histogram
	bytesSaved  metric.Int64Counter

	errNoPolicy = errors.New("no transcode policy")
	errNoOwner  = errors.New("transcode job has no media file")
	errNoConfig = errors.New("config not loaded")
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
		metric.WithDescription("Transcode job duration"),
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
	args := BuildArgs(mf.Path, outPath, info, *pol, action)
	err = run(
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

	// Verification runs on wctx so a cancel arriving mid-probe doesn't read as
	// a corrupt output; the checkpoint below is where it lands instead.
	if err := w.verify(wctx, outPath, info); err != nil {
		w.fail(wctx, job, err)
		return
	}

	// The last point a cancel can be honoured: nothing has moved yet, so the
	// deferred remove drops the encode and the library file is untouched. Past
	// the swap the work is done and a cancel is simply too late — completing it
	// is what keeps the row and the bytes on disk saying the same thing.
	if jctx.Err() != nil {
		w.markCanceled(wctx, job)
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
		wctx, mf.ID, finalPath, sizeAfter, mf.Size, pol.To.Container,
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

// verify checks the encode against the source before anything is swapped: the
// durations must agree within two seconds (containers round), and audio the
// source had must have survived.
func (w *Worker) verify(
	ctx context.Context,
	outPath string,
	src *ffmpeg.Info,
) error {
	out, err := w.prober.Probe(ctx, outPath)
	if err != nil {
		return fmt.Errorf("output verification: %w", err)
	}
	if math.Abs(float64(out.DurationSec)-float64(src.DurationSec)) > 2 {
		return fmt.Errorf(
			"output verification: duration %ds against source %ds",
			out.DurationSec, src.DurationSec,
		)
	}
	if len(src.AudioCodecs) > 0 && len(out.AudioCodecs) == 0 {
		return errors.New("output verification: no audio stream in the output")
	}
	return nil
}

// swap moves the finished encode over the library file and reports the new
// size. A container change lands on a new path, so the source is removed
// separately — that removal failing is not worth failing an import-complete
// job over, since the row already points at the file that exists.
func swap(ctx context.Context, outPath, finalPath, srcPath string) (int64, error) {
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
	if terminal {
		record(ctx, "failed")
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

func record(ctx context.Context, outcome string) {
	jobsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
	))
}
