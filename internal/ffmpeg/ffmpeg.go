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
	// AudioTracks counts audio streams. It is kept alongside AudioLangs
	// because the two disagree on 391 of 5425 files in a real library: 321
	// carry no language tag at all — where a language-derived count would say
	// "one track" instead of "no idea" — and 70 hold a commentary track in the
	// language they already have.
	AudioTracks uint8
	// AudioLangs and SubLangs hold ISO-639-2/T codes, deduped, sorted and
	// comma-joined ("eng,fra,jpn"). Comma strings rather than slices because
	// these land in plain text columns a list scan reads without decoding
	// anything — the mistake Movie.cast's JSON blob made.
	AudioLangs string
	// SubLangs excludes forced tracks: a forced subtitle carries signs and
	// foreign dialogue, not the script, so counting one would report a French
	// dub as French-subtitled.
	SubLangs string
}

// Prober is the consumer-facing surface (importer, media-probe job). *CLI
// implements it; tests use the mockery mock.
type Prober interface {
	Available() bool
	Probe(ctx context.Context, path string) (*Info, error)
	// ResolvedPath returns the absolute ffprobe path this process resolved at
	// construction, or "" when Available() is false.
	ResolvedPath() string
}

// CLI probes through the ffprobe executable. ffprobe is resolved once at
// construction: dir/ffprobe when dir is set, else $PATH. An empty ffprobe
// field means unavailable — callers degrade, never error at boot.
type CLI struct {
	ffprobe string
}

func NewCLI(dir string) *CLI {
	if dir != "" {
		if resolved, err := exec.LookPath(
			filepath.Join(dir, "ffprobe"),
		); err == nil {
			return &CLI{ffprobe: resolved}
		}
		return &CLI{}
	}
	if resolved, err := exec.LookPath("ffprobe"); err == nil {
		return &CLI{ffprobe: resolved}
	}
	return &CLI{}
}

func (c *CLI) Available() bool { return c.ffprobe != "" }

func (c *CLI) ResolvedPath() string { return c.ffprobe }

var _ Prober = (*CLI)(nil)
