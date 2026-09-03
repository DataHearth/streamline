package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// probeTimeout bounds one ffprobe run: a healthy file answers in under a
// second; a hung network mount must not wedge the importer worker.
const probeTimeout = time.Minute

// maxProbeOutput bounds ffprobe's stdout. A real probe is a few KB of JSON —
// even a file with dozens of streams stays well under this — so exceeding it
// means the output is not the document we asked for.
const maxProbeOutput = 1 << 20

// cappedBuffer keeps the first limit bytes and throws the rest away, always
// reporting a full write. Reporting short would make the child see a write
// error; refusing to read at all would block it on a full pipe. Discarding is
// what lets the process finish normally while the memory stays bounded.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	if c.buf.Len() >= c.limit {
		c.truncated = true
	}
	return len(p), nil
}

func (c *CLI) Probe(ctx context.Context, path string) (*Info, error) {
	ctx, span := tracer.Start(ctx, "ffmpeg.probe",
		trace.WithAttributes(attribute.String("file.path", path)))
	defer span.End()

	if !c.Available() {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("ffprobe not available"))
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	//nolint:gosec // c.ffprobe is resolved from the operator's ffmpeg.path (or $PATH) at boot; the flags are fixed and path is a library file
	cmd := exec.CommandContext(ctx, c.ffprobe,
		"-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams",
		path,
	)
	// Not .Output(): that buffers however much ffprobe decides to write, and
	// what it writes is a function of the file, which arrives from a torrent.
	// A capped writer keeps consuming past the limit rather than refusing, so
	// the child never blocks on a full pipe waiting for a reader that stopped.
	var out cappedBuffer
	out.limit = maxProbeOutput
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("%w: %w", ErrUnreadable, err),
		)
	}
	if out.truncated {
		return nil, otelx.RecordSpanError(span, fmt.Errorf(
			"%w: ffprobe wrote more than %d bytes", ErrUnreadable, maxProbeOutput,
		))
	}
	info, err := parseProbeOutput(out.buf.Bytes())
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return info, nil
}

type probeStream struct {
	CodecType   string          `json:"codec_type"`
	CodecName   string          `json:"codec_name"`
	Width       uint16          `json:"width"`
	Height      uint16          `json:"height"`
	Channels    uint8           `json:"channels"`
	Duration    string          `json:"duration"`
	Disposition probeStreamDisp `json:"disposition"`
}

type probeStreamDisp struct {
	AttachedPic int `json:"attached_pic"`
}

type probeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
	BitRate    string `json:"bit_rate"`
}

type probeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

func parseProbeOutput(raw []byte) (*Info, error) {
	var out probeOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	info := &Info{
		// format_name is a comma list of demuxer aliases; the first is the one
		// users recognize (matroska,webm → matroska).
		Container: strings.SplitN(out.Format.FormatName, ",", 2)[0],
	}
	if d, err := strconv.ParseFloat(out.Format.Duration, 64); err == nil {
		info.DurationSec = uint32(d)
	}
	if b, err := strconv.ParseUint(out.Format.BitRate, 10, 32); err == nil {
		info.BitrateBPS = uint32(b)
	}
	var (
		videoStreamDuration string
		videoPixels         uint32
		// ffprobe omits codec_name for codecs without a descriptor, so
		// VideoCodec cannot double as the stream-chosen sentinel.
		videoFound bool
	)
	for _, s := range out.Streams {
		switch s.CodecType {
		case "video":
			// Embedded cover art (mjpeg/png thumbnail) is a video stream too.
			// disposition.attached_pic flags it — but only the MP4 family sets
			// that: Matroska treats artwork as an attachment, and a cover muxed
			// into an mkv as a stream arrives unflagged. So the feature is
			// chosen by size as well, which is what stops a 320x240 png ordered
			// ahead of the film from being read as the video track.
			if s.Disposition.AttachedPic != 0 {
				continue
			}
			pixels := uint32(s.Width) * uint32(s.Height)
			// Ties keep the earlier stream, so a multi-angle release still
			// reports its first track. A stream reporting no dimensions still
			// wins when it is the only candidate.
			if videoFound && pixels <= videoPixels {
				continue
			}
			info.VideoCodec = s.CodecName
			info.Width, info.Height = s.Width, s.Height
			videoStreamDuration = s.Duration
			videoPixels = pixels
			videoFound = true
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec = s.CodecName
				info.AudioChannels = s.Channels
			}
		}
	}
	if !videoFound {
		return nil, ErrNoVideoStream
	}
	// Some containers (MPEG-TS, some remuxes) carry no format.duration; the
	// selected video stream's own duration field is the same decimal-seconds
	// format and is the next best source before giving up on the file.
	if info.DurationSec == 0 {
		if d, err := strconv.ParseFloat(videoStreamDuration, 64); err == nil {
			info.DurationSec = uint32(d)
		}
	}
	if info.DurationSec == 0 {
		return nil, ErrZeroDuration
	}
	return info, nil
}
