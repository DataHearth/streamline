package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	extism "github.com/extism/go-sdk"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	toolFFmpeg  = "ffmpeg"
	toolFFprobe = "ffprobe"

	ffmpegWaitDelay     = 5 * time.Second
	streamWriteDeadline = 2 * time.Minute
	streamChunk         = 32 << 10
	busyRetryAfter      = "5"
)

var (
	errFFmpegDenied      = errors.New("ffmpeg is not granted to this extension")
	errFFmpegUnavailable = errors.New("ffmpeg is not available on this server")
	errFFmpegBusy        = errors.New("every ffmpeg slot is in use")
	errFFmpegTool        = errors.New("the tool must be ffmpeg or ffprobe")
	errFFmpegOutput      = errors.New("the tool wrote more than the output limit")
)

const (
	outcomeOK          = "ok"
	outcomeFailed      = "failed"
	outcomeKilled      = "killed"
	outcomeUnavailable = "unavailable"
	outcomeBusy        = "busy"
	outcomeDenied      = "denied"
)

var ffmpegRuns = otelx.Must(
	otel.Meter("github.com/datahearth/streamline/internal/extension").
		Int64Counter(
			"streamline.extension.ffmpeg",
			metric.WithDescription("ffmpeg runs of extensions by outcome"),
		),
)

type runningKey struct{}

// command prepares one child process for the extension. The returned func
// frees the slot and must be called once the process has been waited on.
func (r *Running) command(
	ctx context.Context,
	tool string,
	args []string,
) (*exec.Cmd, context.Context, context.CancelFunc, func(), error) {
	if !slices.Contains(r.grants.Services, serviceFFmpeg) {
		return nil, nil, nil, nil, errFFmpegDenied
	}
	var path string
	switch tool {
	case toolFFmpeg:
		if r.mgr.tools != nil {
			path = r.mgr.tools.FFmpegPath()
		}
	case toolFFprobe:
		if r.mgr.tools != nil {
			path = r.mgr.tools.ResolvedPath()
		}
	default:
		return nil, nil, nil, nil, errFFmpegTool
	}
	if cfg := config.Get(); path == "" || cfg == nil || !cfg.FFmpeg.Enabled {
		return nil, nil, nil, nil, errFFmpegUnavailable
	}

	select {
	case r.ffmpegSlots <- struct{}{}:
	default:
		return nil, nil, nil, nil, errFFmpegBusy
	}
	free := func() { <-r.ffmpegSlots }

	dir := r.mgr.cacheDir(r.name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		free()
		return nil, nil, nil, nil, fmt.Errorf("create the cache folder: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer observability.RecoverPanic(ctx, "extension ffmpeg watcher", cancel)
		select {
		case <-r.retiring:
			cancel()
		case <-ctx.Done():
		}
	}()

	//nolint:gosec // the path is the binary this process resolved; args are the grant's point
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "STREAMLINE_")
	})
	cmd.WaitDelay = ffmpegWaitDelay
	return cmd, ctx, cancel, free, nil
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[:copy(t.buf, t.buf[over:])]
	}
	return len(p), nil
}

// limitWriter stops a run that writes past its cap.
type limitWriter struct {
	buf    []byte
	max    int
	cancel context.CancelFunc
	over   bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if len(l.buf)+len(p) > l.max {
		l.over = true
		l.cancel()
		return 0, errFFmpegOutput
	}
	l.buf = append(l.buf, p...)
	return len(p), nil
}

func (r *Running) observeFFmpeg(
	ctx context.Context,
	span trace.Span,
	tool string,
	args []string,
	outcome string,
	exit int,
	start time.Time,
	cause error,
) {
	ffmpegRuns.Add(ctx, 1, metric.WithAttributes(
		attribute.String("extension", r.name),
		attribute.String("tool", metricTool(tool)),
		attribute.String("outcome", outcome),
	))
	span.SetAttributes(attribute.String("ffmpeg.outcome", outcome))
	if outcome != outcomeOK {
		if cause == nil {
			cause = fmt.Errorf("ffmpeg %s", outcome)
		}
		span.RecordError(cause)
		span.SetStatus(codes.Error, cause.Error())
	}
	slog.InfoContext(ctx, "Extension ran ffmpeg",
		"extension", r.name, "tool", tool, "args", args,
		"exit_code", exit, "duration", time.Since(start))
}

// metricTool keeps a guest's free-form tool string out of a metric attribute.
func metricTool(tool string) string {
	if tool == toolFFmpeg || tool == toolFFprobe {
		return tool
	}
	return "unknown"
}

func refusal(err error) (string, string) {
	switch {
	case errors.Is(err, errFFmpegDenied), errors.Is(err, errFFmpegTool):
		return outcomeDenied, err.Error()
	case errors.Is(err, errFFmpegBusy):
		return outcomeBusy, err.Error()
	default:
		return outcomeUnavailable, err.Error()
	}
}

// runFFmpeg is the ffmpeg_run host function's work: one run to completion.
func (r *Running) runFFmpeg(ctx context.Context, call FFmpegCall) FFmpegResult {
	ctx, span := tracer.Start(ctx, "extension.ffmpeg", trace.WithAttributes(
		attribute.String("extension.name", r.name),
		attribute.String("ffmpeg.tool", call.Tool),
	))
	defer span.End()
	start := time.Now()

	cmd, runCtx, cancel, free, err := r.command(ctx, call.Tool, call.Args)
	if err != nil {
		outcome, msg := refusal(err)
		r.observeFFmpeg(ctx, span, call.Tool, call.Args, outcome, -1, start, err)
		return FFmpegResult{Error: msg}
	}
	defer free()
	defer cancel()

	stdout := &limitWriter{max: MaxResponseBody, cancel: cancel}
	stderr := &tailBuffer{max: MaxFFmpegStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()

	exitErr, exited := errors.AsType[*exec.ExitError](runErr)
	switch {
	case stdout.over:
		r.observeFFmpeg(
			ctx,
			span,
			call.Tool,
			call.Args,
			outcomeFailed,
			-1,
			start,
			errFFmpegOutput,
		)
		return FFmpegResult{Error: errFFmpegOutput.Error()}
	case runCtx.Err() != nil:
		r.observeFFmpeg(
			ctx,
			span,
			call.Tool,
			call.Args,
			outcomeKilled,
			-1,
			start,
			runCtx.Err(),
		)
		return FFmpegResult{Error: "the run was cut short: " + runCtx.Err().Error()}
	case runErr != nil && !exited:
		r.observeFFmpeg(
			ctx,
			span,
			call.Tool,
			call.Args,
			outcomeFailed,
			-1,
			start,
			runErr,
		)
		return FFmpegResult{Error: runErr.Error()}
	}
	code := 0
	outcome := outcomeOK
	if exited {
		code, outcome = exitErr.ExitCode(), outcomeFailed
	}
	r.observeFFmpeg(ctx, span, call.Tool, call.Args, outcome, code, start, exitErr)
	return FFmpegResult{ExitCode: code, Stdout: stdout.buf, Stderr: stderr.buf}
}

// ffmpegHost is the ffmpeg_run host function.
func ffmpegHost(
	ctx context.Context,
	p *extism.CurrentPlugin,
	stack []uint64,
) {
	offset := stack[0]
	stack[0] = 0
	run, _ := ctx.Value(runningKey{}).(*Running)
	if run == nil {
		return
	}
	raw, err := p.ReadBytes(offset)
	if err != nil {
		return
	}
	var call FFmpegCall
	if err := json.Unmarshal(raw, &call); err != nil {
		return
	}
	out, err := json.Marshal(run.runFFmpeg(ctx, call))
	if err != nil {
		return
	}
	if stack[0], err = p.WriteBytes(out); err != nil {
		stack[0] = 0
	}
}

// streamFFmpeg runs after the guest has returned. Nothing is written until
// the first byte of output is in hand, so a failure before it can still
// become the fallback or an error status.
func (d *dispatcher) streamFFmpeg(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	run *Running,
	folders []Folder,
	sf *StreamFFmpeg,
) string {
	ctx, span := tracer.Start(ctx, "extension.ffmpeg", trace.WithAttributes(
		attribute.String("extension.name", run.name),
		attribute.String("ffmpeg.tool", toolFFmpeg),
	))
	defer span.End()
	start := time.Now()

	fail := func(status int, outcome string) string {
		if sf.Fallback != nil {
			return d.sendFile(ctx, w, r, run, folders, sf.Fallback)
		}
		if status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", busyRetryAfter)
		}
		http.Error(w, http.StatusText(status), status)
		return outcome
	}

	cmd, runCtx, cancel, free, err := run.command(ctx, toolFFmpeg, sf.Args)
	if err != nil {
		outcome, _ := refusal(err)
		slog.WarnContext(ctx, "Extension ffmpeg stream refused",
			"extension", run.name, "error", err)
		run.observeFFmpeg(ctx, span, toolFFmpeg, sf.Args, outcome, -1, start, err)
		if errors.Is(err, errFFmpegDenied) {
			http.NotFound(w, r)
			return "not_found"
		}
		return fail(http.StatusServiceUnavailable, outcome)
	}
	// A fallback download can outlast the run by minutes; it must not hold
	// one of the extension's slots.
	free = sync.OnceFunc(free)
	defer free()
	defer cancel()

	stderr := &tailBuffer{max: MaxFFmpegStderr}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		slog.WarnContext(ctx, "Extension ffmpeg did not start",
			"extension", run.name, "error", err)
		run.observeFFmpeg(
			ctx, span, toolFFmpeg, sf.Args, outcomeUnavailable, -1, start, err,
		)
		free()
		return fail(http.StatusServiceUnavailable, "error")
	}

	buf := make([]byte, streamChunk)
	firstByte := time.AfterFunc(RequestTimeout, cancel)
	n, readErr := stdout.Read(buf)
	firstByte.Stop()
	if n == 0 {
		cancel()
		waitErr := cmd.Wait()
		code := -1
		if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
			code = exitErr.ExitCode()
		} else if waitErr == nil {
			code = 0
		}
		slog.WarnContext(ctx, "Extension ffmpeg produced no output",
			"extension", run.name, "exit_code", code,
			"stderr", string(stderr.buf), "error", readErr)
		run.observeFFmpeg(ctx, span, toolFFmpeg, sf.Args, outcomeFailed, code, start,
			fmt.Errorf("ffmpeg produced no output (exit %d)", code))
		free()
		return fail(http.StatusBadGateway, "error")
	}

	h := w.Header()
	for k, vs := range sf.Headers {
		if slices.ContainsFunc(droppedResponseHeaders, func(d string) bool {
			return strings.EqualFold(d, k)
		}) {
			continue
		}
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	ct := sf.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	h.Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	copyErr := writeChunk(ctx, rc, w, buf[:n])
	for copyErr == nil && readErr == nil {
		n, readErr = stdout.Read(buf)
		if n > 0 {
			copyErr = writeChunk(ctx, rc, w, buf[:n])
		}
	}
	if copyErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()

	outcome, code := outcomeOK, 0
	exitErr, exited := errors.AsType[*exec.ExitError](waitErr)
	switch {
	case copyErr != nil || runCtx.Err() != nil:
		outcome, code = outcomeKilled, -1
	case exited:
		outcome, code = outcomeFailed, exitErr.ExitCode()
	case waitErr != nil && !errors.Is(readErr, io.EOF):
		outcome, code = outcomeFailed, -1
	}
	run.observeFFmpeg(
		ctx,
		span,
		toolFFmpeg,
		sf.Args,
		outcome,
		code,
		start,
		streamCause(copyErr, waitErr),
	)
	return outcome
}

func streamCause(copyErr, waitErr error) error {
	if copyErr != nil {
		return copyErr
	}
	return waitErr
}

func writeChunk(
	ctx context.Context,
	rc *http.ResponseController,
	w http.ResponseWriter,
	p []byte,
) error {
	if err := rc.SetWriteDeadline(time.Now().Add(streamWriteDeadline)); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		slog.DebugContext(ctx, "Extension stream deadline not set", "error", err)
	}
	if _, err := w.Write(p); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}
