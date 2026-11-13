package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/internal/otelx"
)

// ErrNoFFmpeg is returned by TranscodeAudio when no ffmpeg binary resolved.
var ErrNoFFmpeg = errors.New("ffmpeg not available")

// ErrUnknownAudioFormat is an AudioTranscode.Format outside AudioFormats.
var ErrUnknownAudioFormat = errors.New("unknown audio format")

// AudioTranscode is one on-the-fly conversion of an audio file to a stream.
type AudioTranscode struct {
	// Format is one of AudioFormats.
	Format string
	// BitrateKbps is the target bitrate.
	BitrateKbps int
	// OffsetSec starts the output that many seconds into the source.
	OffsetSec int
}

// AudioFormat describes a stream format TranscodeAudio can produce.
type AudioFormat struct {
	ContentType string
	codec       string
	muxer       string
}

// AudioFormats are the formats TranscodeAudio produces, keyed by the name a
// Subsonic client asks for. Each muxer writes a stream that plays as it
// arrives, with no index to seek back and patch at the end.
var AudioFormats = map[string]AudioFormat{
	"mp3":  {ContentType: "audio/mpeg", codec: "libmp3lame", muxer: "mp3"},
	"opus": {ContentType: "audio/ogg", codec: "libopus", muxer: "ogg"},
	"aac":  {ContentType: "audio/aac", codec: "aac", muxer: "adts"},
}

// maxTranscodeStderr bounds what is kept of ffmpeg's stderr for the error.
const maxTranscodeStderr = 4 << 10

// AudioTranscodeArgs is the ffmpeg command line for o, minus the binary.
func AudioTranscodeArgs(src string, o AudioTranscode) ([]string, error) {
	f, ok := AudioFormats[o.Format]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownAudioFormat, o.Format)
	}
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error"}
	if o.OffsetSec > 0 {
		// Before -i: an input seek, which jumps instead of decoding up to it.
		args = append(args, "-ss", strconv.Itoa(o.OffsetSec))
	}
	return append(args,
		"-i", src,
		"-map", "0:a:0",
		"-vn", "-sn", "-dn",
		"-map_metadata", "-1",
		"-c:a", f.codec,
		"-b:a", strconv.Itoa(o.BitrateKbps)+"k",
		"-f", f.muxer,
		"pipe:1",
	), nil
}

// TranscodeAudio converts src as o describes and writes the stream to w. A
// cancelled ctx (the listener went away) kills ffmpeg and returns ctx.Err(),
// which is not recorded as a failure.
func (c *CLI) TranscodeAudio(
	ctx context.Context,
	src string,
	o AudioTranscode,
	w io.Writer,
) error {
	ctx, span := tracer.Start(ctx, "ffmpeg.transcode_audio", trace.WithAttributes(
		attribute.String("file.path", src),
		attribute.String("transcode.format", o.Format),
		attribute.Int("transcode.bitrate_kbps", o.BitrateKbps),
	))
	defer span.End()

	if c.ffmpeg == "" {
		return otelx.RecordSpanError(span, ErrNoFFmpeg)
	}
	args, err := AudioTranscodeArgs(src, o)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	//nolint:gosec // c.ffmpeg is resolved at boot; src is a library row's path
	cmd := exec.CommandContext(ctx, c.ffmpeg, args...)
	stderr := &cappedBuffer{limit: maxTranscodeStderr}
	cmd.Stdout = w
	cmd.Stderr = stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		span.SetAttributes(attribute.Bool("transcode.cancelled", true))
		return ctx.Err()
	}
	if err != nil {
		return otelx.RecordSpanError(
			span,
			fmt.Errorf("ffmpeg transcode: %w: %s", err, stderr.buf.String()),
		)
	}
	return nil
}

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
