package quality

import "slices"

// AudioTier is the bucket a music profile ticks. The order is the canonical
// one: TierLow < TierStandard < TierHigh < TierLossless < TierHiRes.
type AudioTier uint8

const (
	TierLow AudioTier = iota + 1
	TierStandard
	TierHigh
	TierLossless
	TierHiRes
)

var audioTierNames = map[AudioTier]string{
	TierLow:      "low",
	TierStandard: "standard",
	TierHigh:     "high",
	TierLossless: "lossless",
	TierHiRes:    "hires",
}

func (t AudioTier) String() string { return audioTierNames[t] }

// ParseAudioTier is the inverse of String; ok is false for anything that is
// not a tier name, an empty string included.
func ParseAudioTier(s string) (AudioTier, bool) {
	for t, name := range audioTierNames {
		if name == s {
			return t, true
		}
	}
	return 0, false
}

// AudioFacts is what a release name or a probe says about a recording. A zero
// field is unknown.
type AudioFacts struct {
	Codec        string // flac alac wav aiff wv ape mp3 aac m4a ogg opus
	Lossless     bool
	BitDepth     uint8
	SampleRateHz uint32
	BitrateKbps  uint32
	VBR          string // "V0", "V1", "V2" or ""
}

// Thresholds of the lossy tiers, in kbps.
const (
	lossyHighKbps     = 320
	lossyAACHighKbps  = 256
	lossyStandardKbps = 192
	hiResBitDepth     = 24
)

// AudioTierOf classifies facts. It is the single classifier: the SPA's
// releaseTier() is written to the same rule, so the client fallback never
// disagrees. ok is false when the facts cannot place the recording.
func AudioTierOf(f AudioFacts) (AudioTier, bool) {
	if f.Lossless {
		if f.BitDepth >= hiResBitDepth {
			return TierHiRes, true
		}
		return TierLossless, true
	}
	switch {
	case f.VBR == "V0",
		f.BitrateKbps == lossyHighKbps,
		f.Codec == "aac" && f.BitrateKbps >= lossyAACHighKbps:
		return TierHigh, true
	case f.BitrateKbps >= lossyStandardKbps, f.VBR == "V1", f.VBR == "V2":
		return TierStandard, true
	case f.BitrateKbps > 0:
		return TierLow, true
	}
	return 0, false
}

// maxFineScore keeps the within-tier bonus below the 100 points that separate
// two tiers.
const maxFineScore = 99

// AudioFine is the within-tier bonus, 0-99, that orders two releases of one
// tier: MP3 320 outranks V0, a 192 kHz file outranks a 96 kHz one.
func AudioFine(f AudioFacts) int {
	t, ok := AudioTierOf(f)
	if !ok {
		return 0
	}
	fine := 0
	switch t {
	case TierHiRes:
		switch {
		case f.SampleRateHz >= 176400:
			fine = 20
		case f.SampleRateHz >= 88200:
			fine = 10
		}
	case TierHigh:
		switch {
		case f.BitrateKbps == lossyHighKbps:
			fine = 30
		case f.Codec == "aac":
			fine = 25
		case f.VBR == "V0":
			fine = 20
		}
	case TierStandard:
		switch {
		case f.BitrateKbps >= lossyAACHighKbps:
			fine = 20
		case f.VBR == "V1", f.VBR == "V2":
			fine = 15
		case f.BitrateKbps >= lossyStandardKbps:
			fine = 10
		}
	case TierLow:
		fine = int(f.BitrateKbps / 10)
	case TierLossless:
	}
	return min(fine, maxFineScore)
}

// MusicProfile is a music quality profile over plain values: the tiers it
// accepts, the preferred one (the upgrade ceiling) and whether it upgrades.
type MusicProfile struct {
	Tiers          []AudioTier
	Preferred      AudioTier
	UpgradeAllowed bool
}

// Accepts reports whether t is one of the profile's ticked tiers.
func (p MusicProfile) Accepts(t AudioTier) bool {
	return slices.Contains(p.Tiers, t)
}

// Score ranks a release best-first over the ticked tiers: 500 for hires down
// to 100 for low, plus fine. A tier outside the profile is -1 (rejected).
// Preferred does not reorder results; it is only the upgrade ceiling.
func (p MusicProfile) Score(t AudioTier, fine int) int {
	if !p.Accepts(t) {
		return -1
	}
	return int(t)*100 + min(max(fine, 0), maxFineScore)
}

// Replaces is the single predicate deciding whether an incoming recording
// replaces the one held: never when the profile forbids upgrades or the held
// tier is unknown, never once the held tier has reached Preferred, otherwise
// when the incoming tier is accepted and better.
func (p MusicProfile) Replaces(
	have AudioTier,
	haveKnown bool,
	incoming AudioTier,
) bool {
	if !p.UpgradeAllowed || !haveKnown || have >= p.Preferred {
		return false
	}
	return p.Accepts(incoming) && incoming > have
}
