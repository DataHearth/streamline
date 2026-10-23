package qualityctx

import (
	"regexp"

	"github.com/datahearth/streamline/internal/quality"
)

// Release names are the only evidence a release ever offers about its streams:
// nothing is probed before it is downloaded. Each token fills exactly the field
// it speaks to and nothing else, so a name that says MULTi says two tracks and
// stays silent about which languages, and a name that says nothing leaves all
// three nil rather than asserting a zero.
var (
	multiAudioRe = regexp.MustCompile(`(?i)\b(multi|dual[ ._-]?audio)\b`)
	frenchSubRe  = regexp.MustCompile(`(?i)\b(vostfr|subfrench)\b`)
	// VOSTFR is deliberately absent: it names French *subtitles* over the
	// original audio, and reading it as a French dub is the one mistake that
	// would make a subtitled release and a dubbed one score alike.
	frenchAudioRe = regexp.MustCompile(`(?i)\b(vff|vfq|vf2|vfi|vof|vq|truefrench)\b`)
)

// applyStreamHints fills the stream fields a release name can speak to. It only
// ever adds: a field the name says nothing about is left as the caller had it,
// which for a release is nil — unknown — and for a file inside a torrent is
// whatever its probe already reported.
func applyStreamHints(r *quality.ReleaseContext, title string) {
	if r.AudioTracks == nil && multiAudioRe.MatchString(title) {
		// A floor, not a count: MULTi promises at least two, and the
		// audio_tracks condition is a minimum, so a floor is the honest shape.
		two := 2
		r.AudioTracks = &two
	}
	if r.AudioLangs == nil && frenchAudioRe.MatchString(title) {
		r.AudioLangs = []string{"fra"}
	}
	if r.SubLangs == nil && frenchSubRe.MatchString(title) {
		r.SubLangs = []string{"fra"}
	}
}
