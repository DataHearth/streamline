package ffmpeg

import (
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

func (c *CLI) Probe(ctx context.Context, path string) (*Info, error) {
	ctx, span := tracer.Start(ctx, "ffmpeg.probe",
		trace.WithAttributes(attribute.String("file.path", path)))
	defer span.End()

	if !c.Available() {
		return nil, otelx.RecordSpanError(span, fmt.Errorf("ffprobe not available"))
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, c.ffprobe,
		"-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams",
		path,
	).Output()
	if err != nil {
		return nil, otelx.RecordSpanError(
			span,
			fmt.Errorf("%w: %w", ErrUnreadable, err),
		)
	}
	info, err := parseProbeOutput(out)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return info, nil
}

type probeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     uint16 `json:"width"`
	Height    uint16 `json:"height"`
	Channels  uint8  `json:"channels"`
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
	for _, s := range out.Streams {
		switch s.CodecType {
		case "video":
			if info.VideoCodec == "" {
				info.VideoCodec = s.CodecName
				info.Width, info.Height = s.Width, s.Height
			}
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec = s.CodecName
				info.AudioChannels = s.Channels
			}
		}
	}
	if info.VideoCodec == "" {
		return nil, ErrNoVideoStream
	}
	if info.DurationSec == 0 {
		return nil, ErrZeroDuration
	}
	return info, nil
}
