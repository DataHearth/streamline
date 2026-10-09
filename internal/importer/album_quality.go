package importer

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/quality"
)

// albumFileQuality is what the importer could establish about one source file.
// probed says the facts were measured rather than read off the extension or the
// release name: only a measurement may hold an import, since a claim cannot
// contradict itself.
type albumFileQuality struct {
	tier   quality.AudioTier
	known  bool
	probed bool
}

// cbrRatesKbps are the constant rates an encoder offers. A file reporting one
// of them exactly is constant-rate; ffprobe cannot read the LAME preset, so
// only a rate that is none of them is taken to be a VBR average.
var cbrRatesKbps = []uint32{64, 96, 128, 160, 192, 224, 256, 320}

// Average bit rates of the LAME presets, in kbps.
const (
	vbrV0MinKbps = 220
	vbrV2MinKbps = 175
)

func audioFactsFromProbe(info *ffmpeg.AudioInfo) quality.AudioFacts {
	codec := strings.ToLower(info.Codec)
	f := quality.AudioFacts{Codec: codec}
	switch {
	case slices.Contains([]string{"flac", "alac", "wavpack", "ape"}, codec),
		strings.HasPrefix(codec, "pcm_"):
		f.Lossless = true
		f.BitDepth = info.BitDepth
		f.SampleRateHz = info.SampleRateHz
	default:
		f.BitrateKbps = info.BitrateKbps
		if codec == "mp3" && !slices.Contains(cbrRatesKbps, info.BitrateKbps) {
			switch {
			case info.BitrateKbps >= vbrV0MinKbps:
				f.VBR = "V0"
			case info.BitrateKbps >= vbrV2MinKbps:
				f.VBR = "V2"
			}
		}
	}
	return f
}

// assessAlbumFile classifies one source file: measured when ffprobe is
// available and answers, otherwise read off the extension (a lossless one is
// lossless, never hi-res) or, for a lossy file, off what the grabbed release's
// name claimed.
func (w *Worker) assessAlbumFile(
	ctx context.Context,
	path string,
	claim library.ParsedMusicRelease,
) albumFileQuality {
	if config.Get().FFmpeg.Enabled && w.probe != nil && w.probe.Available() {
		info, err := w.probe.ProbeAudio(ctx, path)
		if err == nil {
			t, ok := quality.AudioTierOf(audioFactsFromProbe(info))
			return albumFileQuality{tier: t, known: ok, probed: true}
		}
		slog.WarnContext(ctx, "album import: probe failed, tier not verified",
			"file", filepath.Base(path), "error", err)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return albumFileQuality{tier: quality.TierLossless, known: true}
	}
	if claim.TierKnown && !claim.Facts.Lossless {
		return albumFileQuality{tier: claim.Tier, known: true}
	}
	return albumFileQuality{}
}

// tierHoldReasons lists the measured files whose tier the artist's profile
// does not tick: a "FLAC" that is a 128k transcode, or a 24-bit pack under a
// profile that left hi-res unticked.
func tierHoldReasons(
	plan []albumFile,
	profile config.MusicQualityProfileEntry,
) []schema.HoldReason {
	mp := profile.Profile()
	var out []schema.HoldReason
	for _, f := range plan {
		if !f.q.probed || !f.q.known || mp.Accepts(f.q.tier) {
			continue
		}
		out = append(out, schema.HoldReason{
			File:     f.path,
			Check:    "tier",
			Expected: strings.Join(profile.Tiers, "/"),
			Actual:   f.q.tier.String(),
		})
	}
	return out
}

// heldTrackTier is the tier of a track's current file, known only when every
// file the track holds names a tier: one unreadable file makes the track
// neither an upgrade target nor an obstacle.
func heldTrackTier(tr *ent.Track) (quality.AudioTier, bool) {
	var (
		worst quality.AudioTier
		first = true
	)
	for _, mf := range tr.Edges.MediaFiles {
		t, ok := quality.ParseAudioTier(mf.Quality)
		if !ok {
			return 0, false
		}
		if first || t < worst {
			worst, first = t, false
		}
	}
	return worst, !first
}
