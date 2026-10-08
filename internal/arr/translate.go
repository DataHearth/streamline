package arr

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/config"
)

// RootMapping translates one of the source's root folders into the path this
// process can open. The two sides differ whenever the *arr and streamline
// mount the same volume at different roots, which is the container default.
type RootMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// MapRoot rewrites path through the longest matching mapping. The comparison
// carries a trailing separator so /media/movies cannot swallow a path under
// /media/movies-4k. A path under no mapping comes back unchanged with false,
// which the caller surfaces rather than guessing at.
func MapRoot(path string, roots []RootMapping) (string, bool) {
	clean := filepath.Clean(path)
	best, bestLen := -1, -1
	for i, r := range roots {
		from := filepath.Clean(r.From)
		if !underRoot(clean, from) {
			continue
		}
		if len(from) > bestLen {
			best, bestLen = i, len(from)
		}
	}
	if best < 0 {
		return path, false
	}
	from := filepath.Clean(roots[best].From)
	to := filepath.Clean(roots[best].To)
	if clean == from {
		return to, true
	}
	rel := strings.TrimPrefix(clean, from)
	return filepath.Join(
		to,
		strings.TrimPrefix(rel, string(filepath.Separator)),
	), true
}

func underRoot(path, root string) bool {
	if path == root || root == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// FieldValue reads one provider setting by name. The value is untyped in the
// wire schema — a port arrives as a JSON number, a flag as a bool.
func FieldValue(fields []Field, name string) (string, bool) {
	for _, f := range fields {
		if !strings.EqualFold(f.Name, name) {
			continue
		}
		switch v := f.Value.(type) {
		case nil:
			return "", true
		case string:
			return v, true
		case bool:
			return strconv.FormatBool(v), true
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), true
		default:
			return fmt.Sprint(v), true
		}
	}
	return "", false
}

// bandFor collapses the source's integer resolution onto streamline's three
// bands. Everything below 720 floors to 720p, because the profile schema has
// no lower value — the caller records that as a note rather than dropping it.
func bandFor(resolution int) string {
	switch {
	case resolution >= 2160:
		return "2160p"
	case resolution >= 1080:
		return "1080p"
	default:
		return "720p"
	}
}

// maxResolution walks a profile item, which is either a leaf carrying a
// quality or a named group carrying more items. A profile's cutoff id can
// name either, so both shapes have to resolve to a number.
func maxResolution(it QualityItem) int {
	best := 0
	if it.Quality != nil {
		best = it.Quality.Resolution
	}
	for _, sub := range it.Items {
		best = max(best, maxResolution(sub))
	}
	return best
}

// findItem resolves a cutoff id. Groups carry their own id in a separate
// space (Radarr numbers them from 1000), leaves are named by quality id.
func findItem(items []QualityItem, id uint32) (QualityItem, bool) {
	for _, it := range items {
		if (it.Quality == nil && it.ID == id) ||
			(it.Quality != nil && it.Quality.ID == id) {
			return it, true
		}
		if sub, ok := findItem(it.Items, id); ok {
			return sub, true
		}
	}
	return QualityItem{}, false
}

// allowedResolutions collects every resolution the profile permits,
// flattening groups. An allowed group permits its children.
func allowedResolutions(items []QualityItem, inherited bool) []int {
	var out []int
	for _, it := range items {
		allowed := inherited || it.Allowed
		if allowed && it.Quality != nil && it.Quality.Resolution > 0 {
			out = append(out, it.Quality.Resolution)
		}
		out = append(out, allowedResolutions(it.Items, allowed)...)
	}
	return out
}

// TranslateProfile renders an *arr profile as a streamline one. known reports
// whether a custom-format name resolves here; the caller passes a closure over
// quality.IsBuiltinName and config.FindCustomFormat. The second return lists
// every lossy step, for the operator to read before the profile is created.
func TranslateProfile(
	p QualityProfile,
	known func(string) bool,
) (config.QualityProfileEntry, []string) {
	var notes []string

	minRes := 0
	for _, r := range allowedResolutions(p.Items, false) {
		if minRes == 0 || r < minRes {
			minRes = r
		}
	}
	switch {
	case minRes == 0:
		minRes = 720
		notes = append(notes,
			"no quality was allowed in the source profile; minimum set to 720p")
	case minRes < 720:
		notes = append(notes, fmt.Sprintf(
			"the source allows %dp, which streamline cannot express; floored to 720p",
			minRes,
		))
	}

	cutoffRes := minRes
	if it, ok := findItem(p.Items, p.Cutoff); ok {
		cutoffRes = max(cutoffRes, maxResolution(it))
	} else {
		notes = append(
			notes,
			"the source cutoff names no listed quality; preferred resolution follows the minimum",
		)
	}

	entry := config.QualityProfileEntry{
		Name:                p.Name,
		MinResolution:       bandFor(minRes),
		PreferredResolution: bandFor(cutoffRes),
		UpgradeAllowed:      p.UpgradeAllowed,
		MinScore:            p.MinFormatScore,
		UpgradeUntilScore:   p.CutoffFormatScore,
	}

	var dropped []string
	for _, fi := range p.FormatItems {
		if fi.Score == 0 {
			continue
		}
		if !known(fi.Name) {
			dropped = append(dropped, fi.Name)
			continue
		}
		entry.Formats = append(entry.Formats, config.QualityProfileFormatScore{
			Name:  fi.Name,
			Score: fi.Score,
		})
	}
	if len(dropped) > 0 {
		notes = append(notes, fmt.Sprintf(
			"no custom format here matches: %s", strings.Join(dropped, ", ")))
	}

	notes = append(
		notes,
		"the source distinguishes release source (bluray, web-dl, hdtv); a streamline profile gates on resolution only",
	)

	return entry, notes
}
