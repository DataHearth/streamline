package quality

type ReleaseContext struct {
	Title      string
	Size       int64
	Seeders    int
	HasSeeders bool
	Resolution string
	Source     string
	Group      string
	Codec      string
}

type Format struct {
	Name       string
	Conditions []Condition
}

// Matches implements Radarr custom-format semantics: every required
// condition must pass, and when non-required conditions exist at least
// one must pass.
func (f Format) Matches(r ReleaseContext) bool {
	hasOptional, optionalHit := false, false
	for _, c := range f.Conditions {
		ok := c.eval(r)
		if c.Required {
			if !ok {
				return false
			}
			continue
		}
		hasOptional = true
		optionalHit = optionalHit || ok
	}
	return !hasOptional || optionalHit
}

// Explain returns each condition's post-negate verdict, index-aligned
// with f.Conditions. Powers the format tester endpoint.
func (f Format) Explain(r ReleaseContext) []bool {
	out := make([]bool, len(f.Conditions))
	for i, c := range f.Conditions {
		out[i] = c.eval(r)
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
