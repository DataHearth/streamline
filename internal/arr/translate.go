package arr

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
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

// prowlarrPathRe matches Prowlarr's per-indexer Torznab endpoint, /<id>/
// under Prowlarr's own URL base. Every indexer Prowlarr syncs into an *arr has
// this shape and shares one host and key, so they are one streamline indexer
// rather than N — streamline searches Prowlarr natively, across all of them.
var prowlarrPathRe = regexp.MustCompile(`^(.*?)/(\d+)/?$`)

// maskedSecretRe matches the placeholder newer *arr releases return in place
// of a password or API key. Copying it across would store eight asterisks as
// the credential and fail every request with a misleading auth error.
var maskedSecretRe = regexp.MustCompile(`^\*+$`)

const (
	IndexerTorznab     = "torznab"
	IndexerProwlarr    = "prowlarr"
	IndexerUnsupported = "unsupported"
)

type TranslatedIndexer struct {
	Name string
	// Kind is IndexerTorznab, IndexerProwlarr or IndexerUnsupported.
	Kind   string
	Reason string
	// Collapses counts the source indexers a prowlarr entry stands for.
	Collapses   uint32
	NeedsSecret bool
	Entry       config.IndexerEntry
}

type TranslatedClient struct {
	Name string
	// Reason is set exactly when the client cannot be carried across.
	Reason      string
	NeedsSecret bool
	Entry       config.DownloadClientEntry
}

func secretField(fields []Field, name string) string {
	v, _ := FieldValue(fields, name)
	if maskedSecretRe.MatchString(v) {
		return ""
	}
	return v
}

// streamlinePriority inverts the source's scale: an *arr ranks 1 (best) to 50,
// streamline picks the highest number.
func streamlinePriority(p int) uint8 {
	return uint8(min(max(51-p, 0), 50))
}

type baseURL struct {
	host string
	port uint16
	path string
	ssl  bool
}

func splitBaseURL(raw string) (baseURL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" ||
		(u.Scheme != "http" && u.Scheme != "https") {
		return baseURL{}, fmt.Errorf("unreadable base url %q", raw)
	}
	out := baseURL{
		host: u.Hostname(),
		path: strings.TrimRight(u.Path, "/"),
		ssl:  u.Scheme == "https",
		port: 80,
	}
	if out.ssl {
		out.port = 443
	}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || n == 0 {
			return baseURL{}, fmt.Errorf("unreadable port in %q", raw)
		}
		out.port = uint16(n)
	}
	return out, nil
}

// TranslateIndexers renders the source's indexers as streamline ones, in the
// source's order. Unsupported entries stay in the list with a reason, so the
// operator sees what was left behind rather than a shorter list.
func TranslateIndexers(ps []Provider) []TranslatedIndexer {
	var out []TranslatedIndexer
	prowlarrAt := map[string]int{}

	for _, p := range ps {
		name := strings.TrimSuffix(p.Name, " (Prowlarr)")
		unsupported := func(reason string) {
			out = append(out, TranslatedIndexer{
				Name: name, Kind: IndexerUnsupported, Reason: reason,
			})
		}

		if strings.EqualFold(p.Protocol, "usenet") {
			unsupported("streamline does not support usenet indexers")
			continue
		}
		if !strings.EqualFold(p.Implementation, "Torznab") {
			unsupported(fmt.Sprintf(
				"%s indexers are not supported; streamline speaks Torznab and Prowlarr",
				p.Implementation,
			))
			continue
		}
		raw, _ := FieldValue(p.Fields, "baseUrl")
		if raw == "" {
			unsupported("the indexer has no base URL")
			continue
		}
		base, err := splitBaseURL(raw)
		if err != nil {
			unsupported(err.Error())
			continue
		}
		// The source splits the endpoint into baseUrl + apiPath (default /api);
		// streamline stores it whole in Path.
		apiPath, _ := FieldValue(p.Fields, "apiPath")
		key := secretField(p.Fields, "apiKey")
		enabled := p.EnableRSS || p.EnableAutomaticSearch

		if m := prowlarrPathRe.FindStringSubmatch(base.path); m != nil {
			instance := fmt.Sprintf("%s:%d%s", base.host, base.port, m[1])
			if i, seen := prowlarrAt[instance]; seen {
				out[i].Collapses++
				out[i].Entry.Enabled = out[i].Entry.Enabled || enabled
				continue
			}
			prowlarrAt[instance] = len(out)
			out = append(out, TranslatedIndexer{
				Name:        "Prowlarr",
				Kind:        IndexerProwlarr,
				Collapses:   1,
				NeedsSecret: key == "",
				Entry: config.IndexerEntry{
					Name:     "Prowlarr",
					Host:     base.host,
					Port:     base.port,
					Path:     m[1],
					UseSSL:   base.ssl,
					APIKey:   key,
					Protocol: IndexerProwlarr,
					Priority: streamlinePriority(p.Priority),
					Enabled:  enabled,
				},
			})
			continue
		}

		out = append(out, TranslatedIndexer{
			Name:        name,
			Kind:        IndexerTorznab,
			NeedsSecret: key == "",
			Entry: config.IndexerEntry{
				Name:     name,
				Host:     base.host,
				Port:     base.port,
				Path:     base.path + apiPath,
				UseSSL:   base.ssl,
				APIKey:   key,
				Protocol: IndexerTorznab,
				Priority: streamlinePriority(p.Priority),
				Enabled:  enabled,
			},
		})
	}
	return out
}

// TranslateDownloadClients renders the source's download clients. As with
// indexers, an unsupported client stays in the list carrying its reason.
func TranslateDownloadClients(ps []Provider) []TranslatedClient {
	out := make([]TranslatedClient, 0, len(ps))
	for _, p := range ps {
		t := TranslatedClient{Name: p.Name}

		if strings.EqualFold(p.Protocol, "usenet") {
			t.Reason = "streamline does not support usenet download clients"
			out = append(out, t)
			continue
		}
		clientType := strings.ToLower(p.Implementation)
		switch clientType {
		case "qbittorrent", "transmission", "deluge":
		default:
			t.Reason = fmt.Sprintf(
				"%s is not one of qBittorrent, Transmission or Deluge",
				p.Implementation)
			out = append(out, t)
			continue
		}

		host, _ := FieldValue(p.Fields, "host")
		portStr, _ := FieldValue(p.Fields, "port")
		sslStr, _ := FieldValue(p.Fields, "useSsl")
		user, _ := FieldValue(p.Fields, "username")
		pass := secretField(p.Fields, "password")

		port, err := strconv.ParseUint(portStr, 10, 16)
		if host == "" || err != nil || port == 0 {
			t.Reason = "the client has no usable host and port"
			out = append(out, t)
			continue
		}
		// Deluge authenticates with a password alone; carrying a username it
		// never had would fail validation on a field the operator cannot see.
		if clientType == "deluge" {
			user = ""
		}

		t.NeedsSecret = pass == ""
		t.Entry = config.DownloadClientEntry{
			Name:       p.Name,
			ClientType: clientType,
			Host:       host,
			Port:       uint16(port),
			AuthMethod: "password",
			Username:   user,
			Password:   pass,
			UseSSL:     sslStr == "true",
			Priority:   streamlinePriority(p.Priority),
			Enabled:    p.Enable,
		}
		out = append(out, t)
	}
	return out
}
