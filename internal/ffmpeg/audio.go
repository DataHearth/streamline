package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/otelx"
)

// ErrNoFFmpeg is returned by ConvertToFLAC when no ffmpeg binary resolved.
var ErrNoFFmpeg = errors.New("ffmpeg not available")

// maxTranscodeStderr bounds what is kept of ffmpeg's stderr for the error.
const maxTranscodeStderr = 4 << 10

// FLACConvertArgs is the ffmpeg command line converting src to a FLAC file at
// dst, minus the binary. Tags are carried over.
func FLACConvertArgs(src, dst string) []string {
	return []string{
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-i", src,
		"-map", "0:a:0",
		"-vn", "-sn", "-dn",
		"-c:a", "flac",
		"-f", "flac",
		dst,
	}
}

// ConvertToFLAC writes src's first audio stream to dst as FLAC. A failed
// conversion leaves no file at dst.
func (c *CLI) ConvertToFLAC(ctx context.Context, src, dst string) error {
	ctx, span := tracer.Start(ctx, "ffmpeg.convert_flac", trace.WithAttributes(
		attribute.String("file.path", src),
	))
	defer span.End()

	if c.ffmpeg == "" {
		return otelx.RecordSpanError(span, ErrNoFFmpeg)
	}
	//nolint:gosec // c.ffmpeg is resolved at boot; src and dst are the importer's
	cmd := exec.CommandContext(ctx, c.ffmpeg, FLACConvertArgs(src, dst)...)
	stderr := &cappedBuffer{limit: maxTranscodeStderr}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if rmErr := os.Remove(
			dst,
		); rmErr != nil &&
			!errors.Is(rmErr, os.ErrNotExist) {
			err = errors.Join(err, rmErr)
		}
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("ffmpeg convert to flac: %w: %s", err, stderr.buf.String()),
		)
	}
	return nil
}
