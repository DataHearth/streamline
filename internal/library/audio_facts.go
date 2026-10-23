package library

import (
	"fmt"
	"slices"
	"strconv"
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

// MusicFormatLabel is the short quality string an import row prints: "FLAC
// 24/96", "MP3 320", "MP3 V0". A file that was not measured is labelled by its
// extension alone.
func MusicFormatLabel(ext string, info *ffmpeg.AudioInfo) string {
	if info == nil || info.Codec == "" {
		return strings.ToUpper(strings.TrimPrefix(ext, "."))
	}
	f := AudioFactsFromProbe(info)
	name := strings.ToUpper(f.Codec)
	switch {
	case f.Lossless && f.BitDepth > 0 && f.SampleRateHz > 0:
		khz := strconv.FormatFloat(float64(f.SampleRateHz)/1000, 'f', -1, 64)
		return fmt.Sprintf("%s %d/%s", name, f.BitDepth, khz)
	case f.Lossless:
		return name
	case f.VBR != "":
		return name + " " + f.VBR
	case f.BitrateKbps > 0:
		return fmt.Sprintf("%s %d", name, f.BitrateKbps)
	}
	return name
}
