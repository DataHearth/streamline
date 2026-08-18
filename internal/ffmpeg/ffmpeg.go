// Package ffmpeg owns the ffmpeg-suite dependency: locating the binaries and
// probing media files via ffprobe. The future player's transcode entry points
// belong here too. Leaf package — imports nothing from internal/ but otelx.
package ffmpeg

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"

	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/ffmpeg")

var (
	ErrUnreadable    = errors.New("ffprobe output unreadable")
	ErrNoVideoStream = errors.New("no video stream")
	ErrZeroDuration  = errors.New("zero duration")
)

type Info struct {
	Container     string
	DurationSec   uint32
	VideoCodec    string
	Width         uint16
	Height        uint16
	AudioCodec    string
	AudioChannels uint8
	BitrateBPS    uint32
}

// Prober is the consumer-facing surface (importer, media-probe job). *CLI
// implements it; tests use the mockery mock.
type Prober interface {
	Available() bool
	Probe(ctx context.Context, path string) (*Info, error)
}

// CLI probes through the ffprobe executable. ffprobe is resolved once at
// construction: dir/ffprobe when dir is set, else $PATH. An empty ffprobe
// field means unavailable — callers degrade, never error at boot.
type CLI struct {
	ffprobe string
}

func NewCLI(dir string) *CLI {
	if dir != "" {
		p := filepath.Join(dir, "ffprobe")
		if _, err := exec.LookPath(p); err == nil {
			return &CLI{ffprobe: p}
		}
		return &CLI{}
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		return &CLI{ffprobe: p}
	}
	return &CLI{}
}

func (c *CLI) Available() bool { return c.ffprobe != "" }

var _ Prober = (*CLI)(nil)
