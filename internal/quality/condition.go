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

func (c Condition) eval(r ReleaseContext) bool {
	var ok bool
	switch c.Type {
	case ConditionReleaseTitle:
		ok = c.re.MatchString(r.Title)
	case ConditionReleaseGroup:
		ok = r.Group != "" && c.re.MatchString(r.Group)
	case ConditionResolution:
		ok = r.Resolution != "" && strings.EqualFold(r.Resolution, c.Value)
	case ConditionSource:
		ok = r.Source != "" && strings.EqualFold(r.Source, c.Value)
	case ConditionCodec:
		ok = r.Codec != "" && strings.EqualFold(r.Codec, c.Value)
	case ConditionSize:
		// Both bounds scale with the episode count rather than the size being
		// divided by it: same predicate, but the threshold stays the number the
		// operator typed, which is what a per-episode budget means.
		n := r.episodeScale()
		gb := float64(r.Size) / (1 << 30)
		ok = r.Size > 0 &&
			(c.MinGB == 0 || gb >= c.MinGB*n) &&
			(c.MaxGB == 0 || gb <= c.MaxGB*n)
	case ConditionSeeders:
		ok = r.HasSeeders && r.Seeders >= c.Min
	}
	if c.Negate {
		return !ok
	}
	return ok
}
