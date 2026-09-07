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

// svtPresets maps the policy's x264/x265 preset names onto SVT-AV1's numeric
// ladder. libsvtav1 takes `-preset <int>` (-2..13) and rejects a name outright,
// so every av1 job failed at the first frame until this existed. The names stay
// the config's surface because they are the only spelling shared by the three
// encoders; higher SVT numbers are faster, which is why the table runs the
// other way from how it reads.
var svtPresets = map[string]string{
	"ultrafast": "12",
	"superfast": "11",
	"veryfast":  "10",
	"faster":    "9",
	"fast":      "8",
	"medium":    "7",
	"slow":      "5",
	"slower":    "4",
	"veryslow":  "3",
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
	}
	// mp4 takes video and audio only. `-map 0` hands the muxer the source's
	// subtitle and attachment streams too, and mp4 refuses SRT/ASS/PGS and font
	// attachments outright — which is most mkv sources, so an mp4 target failed
	// on nearly everything it was pointed at. mkv holds all of it, so it keeps
	// the whole file.
	if pol.To.Container == "mp4" {
		args = append(args, "-map", "0:v", "-map", "0:a")
	} else {
		args = append(args, "-map", "0")
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
	preset := pol.To.Preset
	if pol.To.VideoCodec == "av1" {
		if n, ok := svtPresets[preset]; ok {
			preset = n
		}
	}
	args = append(args, "-preset", preset)
	for i, codec := range info.AudioCodecs {
		if passthroughSet[strings.ToLower(codec)] {
			args = append(args, fmt.Sprintf("-c:a:%d", i), "copy")
		} else {
			args = append(args, fmt.Sprintf("-c:a:%d", i), pol.To.AudioCodec)
		}
	}
	if pol.To.Container == "mkv" {
		args = append(args, "-c:s", "copy", "-c:t", "copy")
	}
	return append(args, out)
}
