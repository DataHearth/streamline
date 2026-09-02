package quality

import "cmp"

type ReleaseContext struct {
	Title      string
	Size       int64
	Seeders    int
	HasSeeders bool
	Resolution string
	Source     string
	Group      string
	Codec      string
	// EpisodeCount is how many episodes the release carries, and it scales
	// every size bound: a season pack is compared against MinGB/MaxGB times
	// this, so one threshold reads the same for an episode, a season and a
	// whole-series pack. Zero means unknown and is treated as 1 — the count
	// comes from the library, not from the release, so a show whose episodes
	// aren't tracked yet must not have its releases judged on a divisor we
	// guessed. Indexers never report a torrent's file count; it is only
	// knowable after the grab, which is too late to gate one.
	EpisodeCount int
	// EmptyIsUnknown distinguishes a context assembled from stored columns
	// from one parsed out of a release name. A parser looked at the whole
	// name, so an empty Group there is a real absence the release states; a
	// column is empty both when the release had none and when nothing ever
	// filled it in, and a negate condition cannot be allowed to read the
	// second as the first. Set it on any context built from a row.
	EmptyIsUnknown bool
	// Stream facts, nil meaning "nobody could say". A file answers all three
	// from its probe; a release answers only what its name happens to claim —
	// MULTi fills AudioTracks, VOSTFR fills SubLangs, and a name saying
	// neither leaves them nil rather than asserting a zero. An empty non-nil
	// slice is a real answer: the probe found no track of any language.
	//
	// They are what makes a format like vostfr comparable at all. Written as
	// release_title alone it can only ever match the release, since the
	// renamer destroys the name of the file it is being compared against.
	AudioTracks *int
	AudioLangs  []string
	SubLangs    []string
}

// episodeScale is the multiplier a size bound is measured in. It never
// returns 0, so an unknown count degrades to a plain whole-release bound
// instead of collapsing every threshold to zero and rejecting everything.
func (r ReleaseContext) episodeScale() float64 {
	if r.EpisodeCount < 1 {
		return 1
	}
	return float64(r.EpisodeCount)
}

type Format struct {
	Name        string
	Description string
	Conditions  []Condition
}

// Matches implements Radarr custom-format semantics: every required
// condition must pass, and when non-required conditions exist at least
// one must pass. A condition whose input was never recorded satisfies
// neither test — see Condition.eval on why unknown is not false.
func (f Format) Matches(r ReleaseContext) bool {
	hasOptional, optionalHit := false, false
	for _, c := range f.Conditions {
		ok, known := c.eval(r)
		if c.Required {
			if !known || !ok {
				return false
			}
			continue
		}
		hasOptional = true
		optionalHit = optionalHit || (known && ok)
	}
	return !hasOptional || optionalHit
}

// Explain returns each condition's post-negate verdict, index-aligned
// with f.Conditions. Powers the format tester endpoint, which always
// evaluates a typed release name, so an unknown input reports false.
func (f Format) Explain(r ReleaseContext) []bool {
	out := make([]bool, len(f.Conditions))
	for i, c := range f.Conditions {
		ok, known := c.eval(r)
		out[i] = known && ok
	}
	return out
}

type ScoredFormat struct {
	Format Format
	Score  int
}

type Profile struct {
	MinResolution     string
	MaxResolution     string
	UpgradeAllowed    bool
	MinScore          int
	UpgradeUntilScore int
	Formats           []ScoredFormat
}

type Result struct {
	Score        int
	Rejected     bool
	RejectReason string
	Matched      []string
}

func Evaluate(p Profile, r ReleaseContext) Result {
	got := resolutionRank(r.Resolution)
	if got == 0 || got < resolutionRank(p.MinResolution) ||
		got > resolutionRank(p.MaxResolution) {
		return Result{
			Rejected:     true,
			RejectReason: "resolution outside profile band",
		}
	}
	res := Result{}
	for _, sf := range p.Formats {
		if sf.Format.Matches(r) {
			res.Score += sf.Score
			res.Matched = append(res.Matched, sf.Format.Name)
		}
	}
	if res.Score < p.MinScore {
		res.Rejected = true
		res.RejectReason = "score below profile minimum"
	}
	return res
}

func (p Profile) ShouldUpgrade(current, candidate int) bool {
	if !p.UpgradeAllowed {
		return false
	}
	if p.UpgradeUntilScore > 0 && current >= p.UpgradeUntilScore {
		return false
	}
	return candidate > current
}

// UpgradableFrom reports whether a file at resolution res may be replaced by a
// higher-scoring release. In-band files qualify — their score is the whole
// comparison. A file *below* the band qualifies too, and its Evaluate score of
// 0 is honest: the band rejected it before any format was summed, which is
// exactly the case an upgrade exists for. A file ABOVE the band, or one whose
// resolution could not be determined, never qualifies: it scores 0 for the
// same mechanical reason while being the better file, so replacing it deletes
// what the profile was protecting.
func (p Profile) UpgradableFrom(res string) bool {
	got := resolutionRank(res)
	return got > 0 && got <= resolutionRank(p.MaxResolution)
}

// ReplacesFile reports whether incoming should replace existing under p.
// The band guard runs before the score comparison because a file below the
// band and a file above it both score 0, and only the first is worth
// replacing — see UpgradableFrom.
func ReplacesFile(p Profile, existing, incoming ReleaseContext) bool {
	if !p.UpgradeAllowed || !p.UpgradableFrom(existing.Resolution) {
		return false
	}
	cmp := p.comparableTo(existing)
	return p.ShouldUpgrade(
		Evaluate(cmp, existing).Score, Evaluate(cmp, incoming).Score)
}

// comparableTo drops the formats the existing file cannot answer, so both
// sides of an upgrade are scored on the same evidence.
//
// Without it the comparison is rigged: a format written only in release_title
// is unanswerable for a row, so the file scores 0 for it while the release
// scores its full weight, and every file in the library reads as upgradable by
// exactly that margin. Measured on a real install: an anime profile paying
// vostfr 1000 and multi-audio 50 left every episode ~1050 below an
// upgrade_until_score of 1200 — permanently, since no file could ever earn
// those points back.
//
// Dropping a format is not the same as forgiving it. A "never grab this"
// negative (junk-source, banned-groups) still rejects the incoming release in
// Evaluate, which qualityctx.Replaces runs before ever calling this.
func (p Profile) comparableTo(existing ReleaseContext) Profile {
	out := p
	out.Formats = make([]ScoredFormat, 0, len(p.Formats))
	for _, sf := range p.Formats {
		if sf.Format.answerable(existing) {
			out.Formats = append(out.Formats, sf)
		}
	}
	return out
}

// answerable reports whether f can reach a verdict for r on evidence r
// actually holds: every required condition known, and at least one condition
// of any kind known.
//
// "At least one", not "all", because the formats worth comparing are exactly
// the ones pairing a release_title arm with a measurable arm — vostfr as
// title-or-subtitle_language, multi-audio as title-or-audio_tracks. A row can
// never answer the title arm, so demanding all of them dropped precisely those
// formats and left a file that genuinely lacked the French subtitles unable to
// be upgraded by a release that had them. A format written *only* in
// release_title still drops, which is the point: there the file's non-match is
// an artifact of the renamer, not a fact about the file.
//
// A required condition is different — Matches fails the whole format when one
// is unknown, so an unprovable required condition makes the file score 0 for a
// reason the release never has to face.
func (f Format) answerable(r ReleaseContext) bool {
	anyKnown := false
	for _, c := range f.Conditions {
		_, known := c.eval(r)
		if c.Required && !known {
			return false
		}
		anyKnown = anyKnown || known
	}
	return anyKnown
}

// CompareResolutions orders two resolution buckets the way the profile band
// does: negative when a sits below b, zero when they are the same bucket,
// positive when a sits above it. An unrecognised value ranks below every
// known bucket, and two of them compare equal.
func CompareResolutions(a, b string) int {
	return cmp.Compare(resolutionRank(a), resolutionRank(b))
}

func resolutionRank(r string) uint8 {
	switch r {
	case "480p":
		return 1
	case "720p":
		return 2
	case "1080p":
		return 3
	case "2160p", "4K":
		return 4
	default:
		return 0
	}
}

// ResolutionFromWidth buckets by WIDTH, not height — scope aspect
// ratios crop height (1920x800 is 1080p). Same thresholds as the
// import verifier.
func ResolutionFromWidth(w int) string {
	switch {
	case w >= 3200:
		return "2160p"
	case w >= 1800:
		return "1080p"
	case w >= 1200:
		return "720p"
	case w > 0:
		return "480p"
	default:
		return ""
	}
}
