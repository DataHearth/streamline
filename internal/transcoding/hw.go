package transcoding

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/datahearth/streamline/internal/config"
)

// HW is a probed hardware backend: the policy codecs it can encode, mapped to
// the ffmpeg encoder that does it. A nil *HW means software.
type HW struct {
	Backend  string
	Device   string
	Encoders map[string]string
	// TenBit is whether the device accepted a p010 upload, i.e. whether a
	// 10-bit source can keep its depth through the encoder.
	TenBit bool
}

var vaapiEncoders = map[string]string{
	"h264": "h264_vaapi",
	"hevc": "hevc_vaapi",
	"av1":  "av1_vaapi",
}

// vaapiLevels maps the policy's preset names onto -compression_level, which
// runs 1 (slowest, best) to 7 (fastest) on Intel. AMD's driver accepts the
// range and mostly ignores it, so the mapping only has to be monotone.
var vaapiLevels = map[string]string{
	"ultrafast": "7",
	"superfast": "7",
	"veryfast":  "6",
	"faster":    "5",
	"fast":      "4",
	"medium":    "4",
	"slow":      "3",
	"slower":    "2",
	"veryslow":  "1",
}

// hwProbeTimeout bounds the two probe commands. The test encode is half a
// second of video; anything near this is a wedged driver, not a slow one.
const hwProbeTimeout = 30 * time.Second

// vaapiEncodersIn reads ffmpeg's `-encoders` listing for the VAAPI encoders a
// policy can name. Each line is ` V....D hevc_vaapi  H.265/HEVC (VAAPI)`, so
// the padded name is what is matched.
func vaapiEncodersIn(listing string) map[string]string {
	found := make(map[string]string, len(vaapiEncoders))
	for codec, enc := range vaapiEncoders {
		if strings.Contains(listing, " "+enc+" ") {
			found[codec] = enc
		}
	}
	return found
}

// probeVAAPI checks that ffmpeg carries VAAPI encoders and that device can
// run one, with the same half-second synthetic encode a job would issue.
func probeVAAPI(ctx context.Context, bin, device string) (*HW, error) {
	ctx, cancel := context.WithTimeout(ctx, hwProbeTimeout)
	defer cancel()
	listing, err := exec.CommandContext(ctx, bin, "-hide_banner", "-encoders").
		Output()
	if err != nil {
		return nil, fmt.Errorf("list encoders: %w", err)
	}
	found := vaapiEncodersIn(string(listing))
	if len(found) == 0 {
		return nil, errors.New(
			"ffmpeg has no VAAPI encoders (a static build cannot load libva)",
		)
	}
	enc := found["hevc"]
	if enc == "" {
		for _, e := range found {
			enc = e
		}
	}
	if err := testEncode(ctx, bin, device, enc, "nv12"); err != nil {
		return nil, err
	}
	hw := &HW{Backend: "vaapi", Device: device, Encoders: found}
	// 10-bit is a driver capability, not an encoder one: Main10 is missing on
	// older Intel and some AMD generations while hevc_vaapi still lists. A
	// refusal here means those sources encode at 8 bits, not that jobs fail.
	if err := testEncode(ctx, bin, device, enc, "p010"); err != nil {
		slog.DebugContext(ctx, "10-bit hardware encoding unavailable", "error", err)
	} else {
		hw.TenBit = true
	}
	return hw, nil
}

func testEncode(ctx context.Context, bin, device, enc, format string) error {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-vaapi_device", device,
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25", "-t", "0.5",
		"-vf", "format=" + format + ",hwupload", "-c:v", enc,
	}
	if format == "p010" && enc == "hevc_vaapi" {
		args = append(args, "-profile:v", "main10")
	}
	args = append(args, "-f", "null", "-")
	tail := &tailBuffer{max: stderrTail}
	//nolint:gosec // bin is the prober's ffmpeg; device is transcoding.hw_device
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stderr = tail
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"test encode with %s on %s: %w: %s",
			enc,
			device,
			err,
			tail,
		)
	}
	return nil
}

// hwProbe memoises one probe per (hw_accel, hw_device) pair so the key stays
// hot-editable: a change re-probes on the next job, and nothing re-runs a
// half-second encode per job otherwise.
type hwProbe struct {
	key string
	hw  *HW
	err error
}

// hardware resolves the backend the current config asks for, probing it on
// first use. A failed probe is logged once per config value and answers nil,
// so every job falls back to software.
func (w *Worker) hardware(ctx context.Context) *HW {
	cfg := config.Get()
	if cfg == nil || cfg.Transcoding.HWAccel == "none" {
		return nil
	}
	key := cfg.Transcoding.HWAccel + "\x00" + cfg.Transcoding.HWDevice
	w.hwMu.Lock()
	defer w.hwMu.Unlock()
	if w.hw.key == key {
		return w.hw.hw
	}
	// auto has one backend to pick from on Linux today, so it is vaapi.
	// ponytail: a second backend (videotoolbox) branches here on runtime.GOOS.
	hw, err := probeVAAPI(ctx, w.prober.FFmpegPath(), cfg.Transcoding.HWDevice)
	w.hw = hwProbe{key: key, hw: hw, err: err}
	if err != nil {
		slog.WarnContext(
			ctx,
			"hardware encoding unavailable, falling back to software",
			"transcoding.hw_accel",
			cfg.Transcoding.HWAccel,
			"transcoding.hw_device",
			cfg.Transcoding.HWDevice,
			"error",
			err,
		)
		return nil
	}
	slog.InfoContext(ctx, "hardware encoding ready",
		"transcoding.backend", hw.Backend,
		"transcoding.hw_device", hw.Device,
		"transcoding.encoders", len(hw.Encoders))
	return hw
}

// HWStatus reports the probe outcome for the config view: off, ready or
// unavailable, with the probe error in the last case. Probes if needed.
func (w *Worker) HWStatus(ctx context.Context) (string, string) {
	if hw := w.hardware(ctx); hw != nil {
		return "ready", ""
	}
	w.hwMu.Lock()
	defer w.hwMu.Unlock()
	if w.hw.err != nil {
		return "unavailable", w.hw.err.Error()
	}
	return "off", ""
}
