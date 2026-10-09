package importer

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/library/ebookmeta"
	"github.com/datahearth/streamline/internal/quality"
)

// bookQuality is what MediaFile.quality stores for a book file: the upper-case
// format when the ladder names it, empty otherwise.
func bookQuality(ext string) string {
	f := strings.ToUpper(strings.TrimPrefix(ext, "."))
	if slices.Contains(quality.EbookLadder, f) ||
		slices.Contains(quality.AudiobookLadder, f) {
		return f
	}
	return ""
}

// audiobookFiles lists the audio files under path (a file or a directory
// walked recursively), sorted by path.
func audiobookFiles(path string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(
		path,
		func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if p == path {
					return walkErr
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if _, ok := ebookmeta.AudiobookExtensions[strings.ToLower(filepath.Ext(p))]; ok {
				out = append(out, p)
			}
			return nil
		},
	)
	slices.Sort(out)
	return out, err
}

// dominantFormat is the profile format most of files are in, the first on the
// ladder winning a tie. An extension off the ladder (ogg, opus) has no format
// a profile could tick, so it is not counted: the profile cannot speak for it.
func dominantFormat(files []string) string {
	counts := map[string]int{}
	for _, f := range files {
		if q := bookQuality(
			filepath.Ext(f),
		); slices.Contains(
			quality.AudiobookLadder,
			q,
		) {
			counts[q]++
		}
	}
	best, bestN := "", 0
	for _, f := range quality.AudiobookLadder {
		if counts[f] > bestN {
			best, bestN = f, counts[f]
		}
	}
	return best
}

// audiobookHoldReasons checks a downloaded audiobook against its profile
// before anything is placed: the dominant audio format must be one the
// profile ticks, and the first file's measured bit rate must reach the
// profile's floor. The bit-rate check needs ffprobe and is skipped, with a
// warning, when it is unavailable or fails. The returned Info is that first
// file's measurement, nil when none was made, for the media file row.
func (w *Worker) audiobookHoldReasons(
	ctx context.Context,
	savePath string,
	profile config.BookQualityProfileEntry,
) ([]schema.HoldReason, *ffmpeg.Info) {
	files, err := audiobookFiles(savePath)
	if err != nil || len(files) == 0 {
		return nil, nil
	}
	var reasons []schema.HoldReason
	if format := dominantFormat(files); format != "" &&
		!slices.Contains(profile.Audiobook.Formats, format) {
		reasons = append(reasons, schema.HoldReason{
			File:     files[0],
			Check:    "format",
			Expected: strings.Join(profile.Audiobook.Formats, "/"),
			Actual:   format,
		})
	}
	if !config.Get().FFmpeg.Enabled || w.probe == nil || !w.probe.Available() {
		return reasons, nil
	}
	info, err := w.probe.ProbeAudio(ctx, files[0])
	if err != nil {
		slog.WarnContext(
			ctx,
			"audiobook import: probe failed, bit rate not verified",
			"file",
			filepath.Base(files[0]),
			"error",
			err,
		)
		return reasons, nil
	}
	if floor := uint32(profile.Audiobook.MinBitrate); floor > 0 &&
		info.BitrateKbps > 0 && info.BitrateKbps < floor {
		reasons = append(reasons, schema.HoldReason{
			File:     files[0],
			Check:    "bitrate",
			Expected: fmt.Sprintf("≥ %d kbps", floor),
			Actual:   fmt.Sprintf("%d kbps", info.BitrateKbps),
		})
	}
	return reasons, &ffmpeg.Info{
		DurationSec: info.DurationSec,
		AudioCodec:  info.Codec,
		AudioTracks: 1,
		BitrateBPS:  info.BitrateKbps * 1000,
	}
}
