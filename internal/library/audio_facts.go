package library

import (
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/quality"
)

// cbrRatesKbps are the constant rates an encoder offers. A file reporting one
// of them exactly is constant-rate; ffprobe cannot read the LAME preset, so
// only a rate that is none of them is taken to be a VBR average.
var cbrRatesKbps = []uint32{64, 96, 128, 160, 192, 224, 256, 320}

// Average bit rates of the LAME presets, in kbps.
const (
	vbrV0MinKbps = 220
	vbrV2MinKbps = 175
)

// AudioFactsFromProbe turns a measured audio file into the facts the tier
// ladder reads.
func AudioFactsFromProbe(info *ffmpeg.AudioInfo) quality.AudioFacts {
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
