package transcoding

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// stderrTail bounds what is kept of ffmpeg's stderr. ffmpeg names the reason
// it gave up in its last few lines; everything before that is banner and
// per-stream chatter nobody reads out of a queue row.
const stderrTail = 4096

// tailBuffer keeps the last max bytes written to it and discards the rest,
// always reporting a full write so the child never blocks on it.
type tailBuffer struct {
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	if len(p) >= t.max {
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		return len(p), nil
	}
	if overflow := len(t.buf) + len(p) - t.max; overflow > 0 {
		t.buf = t.buf[:copy(t.buf, t.buf[overflow:])]
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

func (t *tailBuffer) String() string { return strings.TrimSpace(string(t.buf)) }

// run executes ffmpeg, streaming progress snapshots to emit. The last 4 KiB
// of stderr are returned alongside the error (folded into it on a Wait
// failure) because libvmaf reports its score on stderr and nowhere else.
func run(
	ctx context.Context,
	bin string,
	args []string,
	total time.Duration,
	emit func(Snapshot),
) (string, error) {
	// A span of its own, because this is the only step that can run for hours.
	// Without it an encode that hangs shows up nowhere: the job's span stays
	// open with nothing under it, and progress lives in an in-memory map only
	// the API can read.
	ctx, span := tracer.Start(ctx, "transcoding.encode", trace.WithAttributes(
		attribute.Float64("transcode.source_duration_sec", total.Seconds()),
	))
	defer span.End()

	//nolint:gosec // bin is the prober's ffmpeg, resolved from ffmpeg.path (or $PATH) at boot; args are BuildArgs' fixed flags plus library paths
	cmd := exec.CommandContext(ctx, bin, args...)
	tail := &tailBuffer{max: stderrTail}
	cmd.Stderr = tail

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", otelx.RecordSpanError(
			span, fmt.Errorf("ffmpeg stdout pipe: %w", err),
		)
	}
	if err := cmd.Start(); err != nil {
		return "", otelx.RecordSpanError(
			span, fmt.Errorf("ffmpeg start: %w", err),
		)
	}
	// Drains the pipe to EOF, which is also what keeps a chatty ffmpeg from
	// blocking on a full pipe while Wait waits for it to exit.
	readProgress(ctx, stdout, total, logProgress(ctx, emit))
	if err := cmd.Wait(); err != nil {
		if code := cmd.ProcessState.ExitCode(); code >= 0 {
			span.SetAttributes(attribute.Int("process.exit_code", code))
		}
		return tail.String(), otelx.RecordSpanError(
			span, fmt.Errorf("ffmpeg: %w: %s", err, tail),
		)
	}
	return tail.String(), nil
}

// progressLogInterval is how often an encode says where it is. Long enough
// that an hour-long encode produces a handful of lines, short enough that a
// stall is visible as a gap rather than only as silence.
const progressLogInterval = 5 * time.Minute

// logProgress wraps a progress callback with a periodic log line.
//
// Progress otherwise only exists in the worker's in-memory map, readable
// through the API — so an encode stuck at 0% for six hours on a headless box
// produces no signal at all, and the process that eventually notices is a
// person wondering why the queue never moved.
func logProgress(ctx context.Context, emit func(Snapshot)) func(Snapshot) {
	last := time.Now()
	return func(s Snapshot) {
		emit(s)
		if time.Since(last) < progressLogInterval {
			return
		}
		last = time.Now()
		slog.InfoContext(ctx, "encoding",
			"transcode.percent", s.Percent,
			"transcode.speed", s.Speed,
			"transcode.eta_sec", s.ETA.Seconds(),
		)
	}
}
