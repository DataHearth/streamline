// Package ffmpeg owns the ffmpeg-suite dependency: locating the binaries and
// probing media files via ffprobe. The future player's transcode entry points
// belong here too. Leaf package — imports nothing from internal/ but otelx.
package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	// VideoBitrateBPS is the chosen video stream's bit_rate, falling back to the
	// format's when the container omits it (mkv usually does). BitrateBPS stays
	// the format-level figure the probe columns store.
	VideoBitrateBPS uint32
	// HDR is true for a PQ/HLG transfer (smpte2084 / arib-std-b67) or a Dolby
	// Vision configuration record in the video stream's side data.
	HDR bool
	// AudioCodecs lists every audio stream's codec in stream order — the encoder
	// decides copy-vs-encode per stream, so the summary AudioCodec is not enough.
	AudioCodecs []string
}

// Prober is the consumer-facing surface (importer, media-probe job). *CLI
// implements it; tests use the mockery mock.
type Prober interface {
	Available() bool
	Probe(ctx context.Context, path string) (*Info, error)
	// ResolvedPath returns the absolute ffprobe path this process resolved at
	// construction, or "" when Available() is false.
	ResolvedPath() string
	// FFmpegPath returns the absolute ffmpeg path this process resolved at
	// construction, or "" when it could not be found.
	FFmpegPath() string
	// Version runs `ffmpeg -version` and returns the version token from its
	// first line. Errors when ffmpeg is unresolved.
	Version(ctx context.Context) (string, error)
}

// CLI probes through the ffprobe executable and drives ffmpeg. Both binaries
// are resolved once at construction: dir/<bin> when dir is set, else $PATH. An
// empty field means unavailable — callers degrade, never error at boot.
// Probing only needs ffprobe, so Available() does not require ffmpeg too.
type CLI struct {
	ffprobe string
	ffmpeg  string

	versionMu sync.Mutex
	version   string
}

func NewCLI(dir string) *CLI {
	return &CLI{
		ffprobe: resolveBinary(dir, "ffprobe"),
		ffmpeg:  resolveBinary(dir, "ffmpeg"),
	}
}

func resolveBinary(dir, name string) string {
	if dir != "" {
		if resolved, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return resolved
		}
		return ""
	}
	if resolved, err := exec.LookPath(name); err == nil {
		return resolved
	}
	return ""
}

func (c *CLI) Available() bool { return c.ffprobe != "" }

func (c *CLI) ResolvedPath() string { return c.ffprobe }

func (c *CLI) FFmpegPath() string { return c.ffmpeg }

// versionTimeout bounds `ffmpeg -version`: it prints and exits immediately, so
// this only guards against a wedged process.
const versionTimeout = 10 * time.Second

func (c *CLI) Version(ctx context.Context) (string, error) {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if c.version != "" {
		return c.version, nil
	}
	version, err := c.probeVersion(ctx)
	if err != nil {
		// Not cached: a transient failure must not stick forever.
		return "", err
	}
	c.version = version
	return c.version, nil
}

func (c *CLI) probeVersion(ctx context.Context) (string, error) {
	if c.ffmpeg == "" {
		return "", fmt.Errorf("ffmpeg not available")
	}
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	//nolint:gosec // c.ffmpeg is resolved from the operator's ffmpeg.path (or $PATH) at boot
	out, err := exec.CommandContext(ctx, c.ffmpeg, "-version").Output()
	if err != nil {
		return "", fmt.Errorf("ffmpeg -version: %w", err)
	}
	firstLine, _, _ := strings.Cut(string(out), "\n")
	// "ffmpeg version 7.1.1 Copyright …" — the version number is the third
	// whitespace-separated token.
	fields := strings.Fields(firstLine)
	if len(fields) < 3 {
		return "", fmt.Errorf("ffmpeg -version: unrecognized output %q", firstLine)
	}
	return fields[2], nil
}

var _ Prober = (*CLI)(nil)
