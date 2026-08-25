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
		switch c.Type {
		case ConditionReleaseTitle, ConditionReleaseGroup:
			if c.Pattern == "" {
				return Format{}, fmt.Errorf(
					"format %q condition %d: pattern required", name, i)
			}
			re, err := regexp.Compile(c.Pattern)
			if err != nil {
				return Format{}, fmt.Errorf(
					"format %q condition %d: %w", name, i, err)
			}
			c.re = re
		case ConditionResolution:
			switch c.Value {
			case "720p", "1080p", "2160p":
			default:
				return Format{}, fmt.Errorf(
					"format %q condition %d: invalid resolution %q",
					name, i, c.Value)
			}
		case ConditionSource, ConditionCodec:
			if c.Value == "" {
				return Format{}, fmt.Errorf(
					"format %q condition %d: value required", name, i)
			}
		case ConditionSize, ConditionSeeders:
		default:
			return Format{}, fmt.Errorf("format %q condition %d: %w %q",
				name, i, errUnknownConditionType, c.Type)
		}
		f.Conditions[i] = c
	}
	return f, nil
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
		gb := float64(r.Size) / (1 << 30)
		ok = r.Size > 0 &&
			(c.MinGB == 0 || gb >= c.MinGB) &&
			(c.MaxGB == 0 || gb <= c.MaxGB)
	case ConditionSeeders:
		ok = r.HasSeeders && r.Seeders >= c.Min
	}
	if c.Negate {
		return !ok
	}
	return ok
}
