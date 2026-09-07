package transcoding

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
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
// of stderr are folded into the returned error — that tail is what the
// queue shows as the job's error.
func run(
	ctx context.Context,
	bin string,
	args []string,
	total time.Duration,
	emit func(Snapshot),
) error {
	//nolint:gosec // bin is the prober's ffmpeg, resolved from ffmpeg.path (or $PATH) at boot; args are BuildArgs' fixed flags plus library paths
	cmd := exec.CommandContext(ctx, bin, args...)
	tail := &tailBuffer{max: stderrTail}
	cmd.Stderr = tail

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	// Drains the pipe to EOF, which is also what keeps a chatty ffmpeg from
	// blocking on a full pipe while Wait waits for it to exit.
	readProgress(stdout, total, emit)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, tail)
	}
	return nil
}
