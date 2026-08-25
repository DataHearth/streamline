package importer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/library"
)

// widthBucket classifies by width, not height: scope aspect ratios crop
// height (1920×800 is 1080p), so height under-reports the class.
func widthBucket(w uint16) string {
	switch {
	case w >= 3200:
		return "2160p"
	case w >= 1800:
		return "1080p"
	case w >= 1200:
		return "720p"
	default:
		return "480p"
	}
}

var bucketRank = map[string]int{"480p": 0, "720p": 1, "1080p": 2, "2160p": 3}

// claimedBucket normalizes a release's parsed resolution to a bucket name.
// The parser returns the filename's own spelling, so "4K" and "1080P" both
// reach here verbatim.
func claimedBucket(resolution string) (int, bool) {
	claim := strings.ToLower(resolution)
	if claim == "4k" {
		claim = "2160p"
	}
	rank, ok := bucketRank[claim]
	return rank, ok
}

// verifyFile returns the hold reasons for one source file, empty when clean.
// probeErr is the error the probe returned; a nil info with a nil error means
// the file was never probed, which verifies nothing rather than holding.
// runtimeMinutes is the metadata runtime; 0 = unknown, skips the duration
// check.
func verifyFile(
	file string,
	parsed library.ParseResult,
	info *ffmpeg.Info,
	probeErr error,
	runtimeMinutes uint32,
	allowedCodecs []string,
	minDurationRatio float64,
) []schema.HoldReason {
	if probeErr != nil {
		return []schema.HoldReason{{
			File:     file,
			Check:    "corrupt",
			Expected: "readable media",
			Actual:   probeErr.Error(),
		}}
	}
	if info == nil {
		return nil
	}

	var out []schema.HoldReason
	if claimed, ok := claimedBucket(parsed.Resolution); ok {
		actual := widthBucket(info.Width)
		if bucketRank[actual] < claimed {
			out = append(out, schema.HoldReason{
				File:     file,
				Check:    "resolution",
				Expected: parsed.Resolution,
				Actual:   actual,
			})
		}
	}
	if runtimeMinutes > 0 && minDurationRatio > 0 {
		minSec := uint32(float64(runtimeMinutes) * 60 * minDurationRatio)
		if info.DurationSec < minSec {
			out = append(out, schema.HoldReason{
				File:     file,
				Check:    "duration",
				Expected: fmt.Sprintf("≥ %dm", minSec/60),
				Actual:   fmt.Sprintf("%dm", info.DurationSec/60),
			})
		}
	}
	if len(allowedCodecs) > 0 {
		allowed := slices.ContainsFunc(allowedCodecs, func(c string) bool {
			return strings.EqualFold(c, info.VideoCodec)
		})
		if !allowed {
			out = append(out, schema.HoldReason{
				File:     file,
				Check:    "codec",
				Expected: strings.Join(allowedCodecs, "/"),
				Actual:   info.VideoCodec,
			})
		}
	}
	return out
}
