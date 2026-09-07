package transcoding

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

// rejection is a verdict on the encode, not a failure of the job: the same
// input re-encoded gives the same output, so nothing about it is retryable.
type rejection struct {
	reason  string
	outSize int64
}

func (r *rejection) Error() string { return "output rejected: " + r.reason }

// durationTolerance absorbs container rounding of the stream duration.
const durationTolerance = 2

// checkOutput compares the probed encode against the probed source. Every
// check here is a pure comparison; the exec-backed checks live beside it.
func checkOutput(
	src, out *ffmpeg.Info,
	srcSize, outSize int64,
	action Action,
	container string,
	v config.TranscodeVerifyConfig,
) error {
	reject := func(format string, a ...any) error {
		return &rejection{reason: fmt.Sprintf(format, a...), outSize: outSize}
	}
	if math.Abs(
		float64(out.DurationSec)-float64(src.DurationSec),
	) > durationTolerance {
		return reject(
			"duration %ds against source %ds",
			out.DurationSec,
			src.DurationSec,
		)
	}
	if out.AudioTracks != src.AudioTracks {
		return reject(
			"audio tracks %d against source %d",
			out.AudioTracks,
			src.AudioTracks,
		)
	}
	if out.Width != src.Width || out.Height != src.Height {
		return reject("resolution %dx%d against source %dx%d",
			out.Width, out.Height, src.Width, src.Height)
	}
	// mp4 maps video and audio only (BuildArgs), so its subtitles are gone by
	// design rather than by accident.
	if container == "mkv" && out.SubLangs != src.SubLangs {
		return reject(
			"subtitle languages %q against source %q",
			out.SubLangs,
			src.SubLangs,
		)
	}
	if srcSize <= 0 {
		return nil
	}
	percent := outSize * 100 / srcSize
	// A remux copies the streams and can legitimately grow by container
	// overhead; only a transcode is asked to shrink.
	if action == ActionTranscode && v.MaxSizePercent != 0 &&
		outSize*100 > srcSize*int64(v.MaxSizePercent) {
		return reject(
			"output is %d%% of the source (max %d%%)",
			percent,
			v.MaxSizePercent,
		)
	}
	if v.MinSizePercent != 0 && outSize*100 < srcSize*int64(v.MinSizePercent) {
		return reject(
			"output is %d%% of the source (min %d%%)",
			percent,
			v.MinSizePercent,
		)
	}
	return nil
}

// healthCheck fully decodes the output. -xerror makes ffmpeg exit non-zero on
// the first decode error, which a container probe never sees.
func healthCheck(
	ctx context.Context,
	bin, outPath string,
	total time.Duration,
	emit func(Snapshot),
) error {
	args := []string{
		"-hide_banner", "-nostdin", "-v", "error", "-xerror",
		"-i", outPath,
		"-progress", "pipe:1",
		"-f", "null", "-",
	}
	if _, err := run(ctx, bin, args, total, emit); err != nil {
		return err
	}
	return nil
}

type vmafWindow struct{ start, length uint32 }

const vmafWindowSec = 60

// vmafWindows picks three windows at 10, 50 and 90 % of the file — clear of
// title cards and credits — each up to a minute, shrunk so three still fit
// a short file. Scoring whole films would cost a second decode of both.
func vmafWindows(durationSec uint32) []vmafWindow {
	length := min(uint32(vmafWindowSec), durationSec/3)
	if length == 0 {
		return nil
	}
	var out []vmafWindow
	for _, pct := range []uint32{10, 50, 90} {
		start := durationSec * pct / 100
		if start+length > durationSec {
			start = durationSec - length
		}
		out = append(out, vmafWindow{start: start, length: length})
	}
	return out
}

var vmafScoreRe = regexp.MustCompile(`VMAF score: ([0-9.]+)`)

func parseVMAF(stderr string) (float64, bool) {
	m := vmafScoreRe.FindStringSubmatch(stderr)
	if m == nil {
		return 0, false
	}
	score, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return score, true
}

var vmafUnavailableOnce sync.Once

// vmafScore returns the mean VMAF of the encode against its source over
// vmafWindows. ok is false when ffmpeg carries no libvmaf filter or the file
// is too short to window — both are "cannot measure", logged, not a verdict.
func vmafScore(
	ctx context.Context,
	bin, outPath, srcPath string,
	durationSec uint32,
) (float64, bool, error) {
	windows := vmafWindows(durationSec)
	if len(windows) == 0 {
		return 0, false, nil
	}
	// setpts realigns both seeks to t=0; format=yuv420p lets a 10-bit encode
	// be scored against an 8-bit source, which libvmaf refuses otherwise.
	graph := fmt.Sprintf(
		"[0:v]setpts=PTS-STARTPTS,format=yuv420p[d];"+
			"[1:v]setpts=PTS-STARTPTS,format=yuv420p[r];"+
			"[d][r]libvmaf=n_threads=%d",
		runtime.NumCPU(),
	)
	var sum float64
	for _, w := range windows {
		start, length := strconv.Itoa(int(w.start)), strconv.Itoa(int(w.length))
		args := []string{
			"-hide_banner", "-nostdin", "-v", "info",
			"-ss", start, "-t", length, "-i", outPath,
			"-ss", start, "-t", length, "-i", srcPath,
			"-lavfi", graph,
			"-progress", "pipe:1",
			"-f", "null", "-",
		}
		stderr, err := run(
			ctx,
			bin,
			args,
			time.Duration(w.length)*time.Second,
			func(Snapshot) {},
		)
		if err != nil {
			if strings.Contains(err.Error(), "No such filter: 'libvmaf'") {
				vmafUnavailableOnce.Do(func() {
					slog.WarnContext(
						ctx,
						"ffmpeg has no libvmaf filter; min_vmaf is not enforced",
					)
				})
				return 0, false, nil
			}
			return 0, false, fmt.Errorf("vmaf window at %ss: %w", start, err)
		}
		score, ok := parseVMAF(stderr)
		if !ok {
			return 0, false, fmt.Errorf(
				"vmaf window at %ss: no score in ffmpeg output",
				start,
			)
		}
		sum += score
	}
	return sum / float64(len(windows)), true, nil
}
