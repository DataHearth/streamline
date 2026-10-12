package engine

import (
	"slices"
	"strings"

	"github.com/datahearth/streamline/internal/cardigann"
)

type category struct {
	id   int
	name string
}

// mapping is one tracker category → newznab category pair, as Prowlarr's
// IndexerCapabilitiesCategories holds them. desc is the tracker's own label,
// matched by a categorydesc field.
type mapping struct {
	tracker string
	desc    string
	newznab int
}

func newznabByName(name string) (int, bool) {
	for _, c := range newznabCategories {
		if c.name == name {
			return c.id, true
		}
	}
	return 0, false
}

// categoryMap reads a definition's caps. A name that is not a standard
// newznab category is dropped, as Prowlarr drops it.
type categoryMap struct {
	mappings []mapping
	defaults []string
}

func newCategoryMap(caps cardigann.Caps) categoryMap {
	var m categoryMap
	for _, e := range caps.Categories {
		if id, ok := newznabByName(string(e.Value)); ok {
			m.mappings = append(m.mappings, mapping{tracker: e.Key, newznab: id})
		}
	}
	for _, cm := range caps.CategoryMappings {
		id, ok := newznabByName(cm.Cat)
		if !ok {
			continue
		}
		m.mappings = append(m.mappings, mapping{
			tracker: string(cm.ID), desc: cm.Desc, newznab: id,
		})
		if cm.Default {
			m.defaults = append(m.defaults, string(cm.ID))
		}
	}
	return m
}

// trackers maps newznab categories to the tracker's, a root standing for all
// of its children (MapTorznabCapsToTrackers). No match falls back to the
// definition's default categories.
func (m categoryMap) trackers(newznab []int) []string {
	var out []string
	for _, mp := range m.mappings {
		for _, q := range newznab {
			if mp.newznab == q || (q%1000 == 0 && mp.newznab/1000*1000 == q) {
				if !slices.Contains(out, mp.tracker) {
					out = append(out, mp.tracker)
				}
				break
			}
		}
	}
	if len(out) == 0 {
		return slices.Clone(m.defaults)
	}
	return out
}

// newznab maps a tracker category id (byDesc false) or label (true) to the
// newznab categories it was declared under, compared case-insensitively.
func (m categoryMap) newznab(value string, byDesc bool) []int {
	var out []int
	if strings.TrimSpace(value) == "" {
		return nil
	}
	for _, mp := range m.mappings {
		key := mp.tracker
		if byDesc {
			key = mp.desc
		}
		if key != "" && strings.EqualFold(key, value) &&
			!slices.Contains(out, mp.newznab) {
			out = append(out, mp.newznab)
		}
	}
	return out
}
