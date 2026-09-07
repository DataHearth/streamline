// Package transcoding runs the rule-driven post-import transcode pipeline:
// probe, evaluate quality-profile rules, re-encode or remux with the ffmpeg
// binary, verify, and atomically swap the library file.
package transcoding

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

type Action int

const (
	ActionNone Action = iota
	ActionRemux
	ActionTranscode
)

// Evaluate decides what a job must do for the probed file to comply with pol.
// HDR/DV video is never re-encoded (v1): codec and bitrate rules are suspended
// for it, container remux still applies.
func Evaluate(
	path string,
	info *ffmpeg.Info,
	pol config.TranscodePolicy,
) (Action, string) {
	container := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")

	if !info.HDR {
		if len(pol.If.VideoCodecs) > 0 &&
			!slices.Contains(pol.If.VideoCodecs, info.VideoCodec) {
			return ActionTranscode, fmt.Sprintf(
				"video codec %q not in %v",
				info.VideoCodec,
				pol.If.VideoCodecs,
			)
		}
		if pol.If.MaxVideoBitrate != "" {
			max, err := config.ParseBitrate(pol.If.MaxVideoBitrate)
			if err == nil && int64(info.VideoBitrateBPS) > max {
				return ActionTranscode, fmt.Sprintf(
					"video bitrate %d above %s",
					info.VideoBitrateBPS,
					pol.If.MaxVideoBitrate,
				)
			}
		}
	}

	if len(pol.If.Containers) > 0 && !slices.Contains(pol.If.Containers, container) {
		return ActionRemux, fmt.Sprintf(
			"container %q not in %v",
			container,
			pol.If.Containers,
		)
	}

	return ActionNone, ""
}
