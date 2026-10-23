package quality

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/langcode"
)

var errUnknownConditionType = errors.New("unknown condition type")

type ConditionType string

const (
	ConditionReleaseTitle ConditionType = "release_title"
	ConditionResolution   ConditionType = "resolution"
	ConditionSource       ConditionType = "source"
	ConditionReleaseGroup ConditionType = "release_group"
	ConditionCodec        ConditionType = "codec"
	ConditionSize         ConditionType = "size"
	ConditionSeeders      ConditionType = "seeders"
	// The three stream types. Each is answerable from a file's probe AND from
	// a release name's tokens, which is the property release_title lacks and
	// the reason these exist: a format pairing a release_title arm with one of
	// these matches the release by name and the file by measurement, so the
	// two can finally be compared.
	ConditionAudioTracks      ConditionType = "audio_tracks"
	ConditionAudioLanguage    ConditionType = "audio_language"
	ConditionSubtitleLanguage ConditionType = "subtitle_language"
)

type Condition struct {
	Type     ConditionType
	Pattern  string
	Value    string
	MinGB    float64
	MaxGB    float64
	Min      int
	Required bool
	Negate   bool

	re *regexp.Regexp
}

func NewFormat(name string, conds []Condition) (Format, error) {
	f := Format{Name: name, Conditions: make([]Condition, len(conds))}
	for i, c := range conds {
		where := conditionRef(name, i)
		switch c.Type {
		case ConditionReleaseTitle, ConditionReleaseGroup:
			if c.Pattern == "" {
				return Format{}, fmt.Errorf("%s: pattern required", where)
			}
			re, err := regexp.Compile(c.Pattern)
			if err != nil {
				return Format{}, fmt.Errorf("%s: %w", where, err)
			}
			c.re = re
		case ConditionResolution:
			switch c.Value {
			case "720p", "1080p", "2160p":
			default:
				return Format{}, fmt.Errorf(
					"%s: invalid resolution %q", where, c.Value)
			}
		case ConditionSource, ConditionCodec:
			if c.Value == "" {
				return Format{}, fmt.Errorf("%s: value required", where)
			}
		case ConditionAudioLanguage, ConditionSubtitleLanguage:
			if c.Value == "" {
				return Format{}, fmt.Errorf("%s: value required", where)
			}
			// Stored codes are canonicalised at probe time, so a condition
			// spelled "fre" or "FR" would silently never match. Fold it here
			// rather than at eval time: the compile is once per config
			// generation, the eval is once per release per format.
			c.Value = langcode.Canonical(c.Value)
		case ConditionSize, ConditionSeeders, ConditionAudioTracks:
		default:
			return Format{}, fmt.Errorf(
				"%s: %w %q", where, errUnknownConditionType, c.Type)
		}
		f.Conditions[i] = c
	}
	return f, nil
}

// conditionRef locates a condition in an error message. The format tester
// compiles an unsaved draft that has no name yet, and a `format ""` prefix
// shown to the operator quotes nothing — so an unnamed format is located by
// its condition index alone.
func conditionRef(name string, i int) string {
	if name == "" {
		return fmt.Sprintf("condition %d", i)
	}
	return fmt.Sprintf("format %q condition %d", name, i)
}

// eval reports the condition's post-negate verdict and whether the input it
// reads was recorded at all.
//
// The second return is what keeps "no" apart from "don't know". Negate turns a
// false into a match, so a condition reading a field nobody filled in scores a
// positive out of ignorance: `negate` on release_group — the idiomatic "this
// release carries no group" format — matched every library file whose group
// was never stored, which on a real library was every file, for -100 apiece.
// An unknown input matches nothing and negate-matches nothing.
func (c Condition) eval(r ReleaseContext) (ok, known bool) {
	switch c.Type {
	case ConditionReleaseTitle:
		if r.Title == "" {
			return false, false
		}
		ok = c.re.MatchString(r.Title)
		// For a row, Title is the basename the renamer wrote — a *subset* of
		// the release name, since the naming template keeps a fraction of its
		// tokens. So the two outcomes are not symmetric: a match is real
		// evidence the token was there, but a miss proves only that the
		// template dropped it. Reporting that miss as a confident false is how
		// a file scored 0 against a release's remux/vostfr/multi-audio while
		// the scanner read the whole library as upgradable; reporting it
		// unknown lets ReplacesFile drop the format from both sides instead.
		// A template that keeps the whole release name still scores normally,
		// which the blunter "unknown for every row" rule threw away.
		if r.EmptyIsUnknown && !ok {
			return false, false
		}
	case ConditionReleaseGroup:
		if r.Group == "" && r.EmptyIsUnknown {
			return false, false
		}
		ok = r.Group != "" && c.re.MatchString(r.Group)
	case ConditionResolution:
		if r.Resolution == "" && r.EmptyIsUnknown {
			return false, false
		}
		ok = r.Resolution != "" && strings.EqualFold(r.Resolution, c.Value)
	case ConditionSource:
		if r.Source == "" && r.EmptyIsUnknown {
			return false, false
		}
		ok = r.Source != "" && strings.EqualFold(r.Source, c.Value)
	case ConditionCodec:
		if r.Codec == "" && r.EmptyIsUnknown {
			return false, false
		}
		ok = r.Codec != "" && strings.EqualFold(r.Codec, c.Value)
	case ConditionSize:
		if r.Size <= 0 {
			return false, false
		}
		// Both bounds scale with the episode count rather than the size being
		// divided by it: same predicate, but the threshold stays the number the
		// operator typed, which is what a per-episode budget means.
		n := r.episodeScale()
		gb := float64(r.Size) / (1 << 30)
		ok = (c.MinGB == 0 || gb >= c.MinGB*n) &&
			(c.MaxGB == 0 || gb <= c.MaxGB*n)
	case ConditionSeeders:
		// HasSeeders is already the "was this reported" flag — Torznab leaves
		// the attribute at 0 when the indexer omits it, and a file has no
		// seeders to report in the first place.
		if !r.HasSeeders {
			return false, false
		}
		ok = r.Seeders >= c.Min
	case ConditionAudioTracks:
		if r.AudioTracks == nil {
			return false, false
		}
		ok = *r.AudioTracks >= c.Min
	case ConditionAudioLanguage:
		// nil is "nobody could say"; an empty slice is a probe that found no
		// tagged track, which is a real answer and may be negated.
		if r.AudioLangs == nil {
			return false, false
		}
		ok = slices.Contains(r.AudioLangs, c.Value)
	case ConditionSubtitleLanguage:
		if r.SubLangs == nil {
			return false, false
		}
		ok = slices.Contains(r.SubLangs, c.Value)
	}
	if c.Negate {
		ok = !ok
	}
	return ok, true
}
