package quality

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
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
		case ConditionSize, ConditionSeeders:
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
		// A context assembled from stored columns has no release name at all:
		// the renamer wrote the only name it has, and the naming template
		// keeps a fraction of the tokens a condition here matches on.
		if r.Title == "" {
			return false, false
		}
		ok = c.re.MatchString(r.Title)
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
	}
	if c.Negate {
		ok = !ok
	}
	return ok, true
}
