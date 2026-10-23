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

	raw, err := c.runProbe(ctx, path)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	info, err := parseProbeOutput(raw)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return info, nil
}

// ProbeAudio probes a music or audiobook file. Probe refuses a file without a
// video stream, which is exactly what an audio file is.
func (c *CLI) ProbeAudio(ctx context.Context, path string) (*AudioInfo, error) {
	ctx, span := tracer.Start(ctx, "ffmpeg.probe_audio",
		trace.WithAttributes(attribute.String("file.path", path)))
	defer span.End()

	raw, err := c.runProbe(ctx, path)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	info, err := parseAudioProbeOutput(raw)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return info, nil
}

func (c *CLI) runProbe(ctx context.Context, path string) ([]byte, error) {
	if !c.Available() {
		return nil, fmt.Errorf("ffprobe not available")
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
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	if out.truncated {
		return nil, fmt.Errorf(
			"%w: ffprobe wrote more than %d bytes", ErrUnreadable, maxProbeOutput,
		)
	}
	return out.buf.Bytes(), nil
}

// parseAudioProbeOutput reads the first audio stream. BitRateKbps prefers the
// stream's own rate and falls back to the container's, which is the only one
// an mp3 reports.
func parseAudioProbeOutput(raw []byte) (*AudioInfo, error) {
	var out probeOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	var stream *probeStream
	for i := range out.Streams {
		s := &out.Streams[i]
		if s.CodecType == "audio" {
			stream = s
			break
		}
	}
	if stream == nil {
		return nil, ErrNoAudioStream
	}
	info := &AudioInfo{Codec: stream.CodecName}
	if d, err := strconv.ParseFloat(out.Format.Duration, 64); err == nil {
		info.DurationSec = uint32(d)
	}
	if info.DurationSec == 0 {
		if d, err := strconv.ParseFloat(stream.Duration, 64); err == nil {
			info.DurationSec = uint32(d)
		}
	}
	if info.DurationSec == 0 {
		return nil, ErrZeroDuration
	}
	if n, err := strconv.ParseUint(stream.BitsPerRawSample, 10, 8); err == nil {
		info.BitDepth = uint8(n)
	} else if stream.BitsPerSample > 0 && stream.BitsPerSample <= 64 {
		info.BitDepth = stream.BitsPerSample
	}
	if n, err := strconv.ParseUint(stream.SampleRate, 10, 32); err == nil {
		info.SampleRateHz = uint32(n)
	}
	rate := stream.BitRate
	if rate == "" {
		rate = out.Format.BitRate
	}
	if n, err := strconv.ParseUint(rate, 10, 32); err == nil {
		info.BitrateKbps = uint32(n / 1000)
	}
	return info, nil
}

type probeStream struct {
	BitsPerRawSample string          `json:"bits_per_raw_sample"`
	BitsPerSample    uint8           `json:"bits_per_sample"`
	SampleRate       string          `json:"sample_rate"`
	CodecType        string          `json:"codec_type"`
	CodecName        string          `json:"codec_name"`
	Width            uint16          `json:"width"`
	Height           uint16          `json:"height"`
	PixFmt           string          `json:"pix_fmt"`
	Channels         uint8           `json:"channels"`
	Duration         string          `json:"duration"`
	BitRate          string          `json:"bit_rate"`
	ColorTransfer    string          `json:"color_transfer"`
	SideDataList     []probeSideData `json:"side_data_list"`
	Disposition      probeStreamDisp `json:"disposition"`
	Tags             probeStreamTags `json:"tags"`
}

type probeSideData struct {
	SideDataType string `json:"side_data_type"`
}

type probeStreamTags struct {
	Language string `json:"language"`
}

type probeStreamDisp struct {
	AttachedPic int `json:"attached_pic"`
	Forced      int `json:"forced"`
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

// isHDR reports a PQ/HLG transfer function or a Dolby Vision configuration
// record in the stream's side data.
func isHDR(s probeStream) bool {
	switch s.ColorTransfer {
	case "smpte2084", "arib-std-b67":
		return true
	}
	for _, sd := range s.SideDataList {
		if strings.Contains(sd.SideDataType, "DOVI") {
			return true
		}
	}
	return false
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
		videoStreamBitRate  string
		videoPixels         uint32
		// ffprobe omits codec_name for codecs without a descriptor, so
		// VideoCodec cannot double as the stream-chosen sentinel.
		videoFound bool
		audioLangs langSet
		subLangs   langSet
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
			info.HDR = isHDR(s)
			info.TenBit = strings.Contains(s.PixFmt, "10")
			videoStreamDuration = s.Duration
			videoStreamBitRate = s.BitRate
			videoPixels = pixels
			videoFound = true
		case "audio":
			if info.AudioCodec == "" {
				info.AudioCodec = s.CodecName
				info.AudioChannels = s.Channels
			}
			// Saturating: no real file has 255 audio streams, and a wrapped
			// count would read as a single-track release to a min-tracks
			// condition — the one direction that must not happen silently.
			if info.AudioTracks < 255 {
				info.AudioTracks++
			}
			info.AudioCodecs = append(info.AudioCodecs, s.CodecName)
			audioLangs.add(s.Tags.Language)
		case "subtitle":
			if s.Disposition.Forced == 0 {
				subLangs.add(s.Tags.Language)
			}
		}
	}
	if b, err := strconv.ParseUint(videoStreamBitRate, 10, 32); err == nil {
		info.VideoBitrateBPS = uint32(b)
	} else {
		info.VideoBitrateBPS = info.BitrateBPS
	}
	info.AudioLangs, info.SubLangs = audioLangs.join(), subLangs.join()
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
