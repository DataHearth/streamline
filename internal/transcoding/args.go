package transcoding

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

var encoders = map[string]string{
	"h264": "libx264",
	"hevc": "libx265",
	"av1":  "libsvtav1",
}

// BuildArgs assembles the ffmpeg invocation. action is ActionRemux or
// ActionTranscode. HDR video never reaches ActionTranscode (Evaluate exempts
// it), so no HDR metadata forwarding is needed.
func BuildArgs(
	in, out string,
	info *ffmpeg.Info,
	pol config.TranscodePolicy,
	action Action,
) []string {
	args := []string{
		"-hide_banner",
		"-nostats",
		"-progress",
		"pipe:1",
		"-y",
		"-i",
		in,
		"-map",
		"0",
	}

	if action == ActionRemux {
		args = append(args, "-c", "copy")
		return append(args, out)
	}

	passthroughSet := make(map[string]bool, len(pol.To.AudioPassthrough))
	for _, codec := range pol.To.AudioPassthrough {
		passthroughSet[strings.ToLower(codec)] = true
	}

	args = append(args, "-c:v", encoders[pol.To.VideoCodec])
	// 0 is "unset", not a CRF of zero: passing -crf 0 asks for a near-lossless
	// encode, so a policy that omits the key would produce a file larger than
	// the one it replaced. Omitting the flag is what leaves the encoder's own
	// default in force.
	if pol.To.CRF != 0 {
		args = append(args, "-crf", strconv.Itoa(int(pol.To.CRF)))
	}
	args = append(args, "-preset", pol.To.Preset)
	for i, codec := range info.AudioCodecs {
		if passthroughSet[strings.ToLower(codec)] {
			args = append(args, fmt.Sprintf("-c:a:%d", i), "copy")
		} else {
			args = append(args, fmt.Sprintf("-c:a:%d", i), pol.To.AudioCodec)
		}
	}
	args = append(args, "-c:s", "copy")
	if pol.To.Container == "mkv" {
		args = append(args, "-c:t", "copy")
	}
	return append(args, out)
}
