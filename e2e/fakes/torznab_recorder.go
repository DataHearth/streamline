package fakes

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TorznabRecorder hosts several independently configured Torznab endpoints on
// one server and records every query each is asked.
//
// It is a sibling of Torznab rather than an extension of it because the two
// answer different questions. Torznab serves one canned movie release with
// fixed caps, and the pipeline specs import its exact bytes. This one carries
// no payload at all: what it exists to capture is the request — which
// parameters actually arrived, having gone through a real Prowlarr — and to
// vary the caps document, which is the lever those specs turn on. Prowlarr
// drops an indexer from a search entirely when handed a parameter its caps
// omit, so what an endpoint claims to support decides whether it is queried.
//
// One server, several routes, because the container reaching back to this
// process is forwarded one host port for the life of that container.
type TorznabRecorder struct {
	srv *httptest.Server

	mu     sync.Mutex
	routes map[string]*TorznabEndpoint
}

// TorznabEndpoint is one mounted indexer: what it claims to support, what it
// answers with, and what it was asked.
type TorznabEndpoint struct {
	// Path is the route this endpoint answers on, e.g. "/tv-full".
	Path string

	caps     string
	feed     string
	recorder *TorznabRecorder

	mu      sync.Mutex
	queries []url.Values
}

// TorznabCaps describes the search modes an endpoint declares. An empty
// SupportedParams list means the mode is advertised as unavailable, which is
// how a spec builds an indexer Prowlarr will refuse to hand a given id.
type TorznabCaps struct {
	// TVSearchParams are the tv-search params the endpoint claims, e.g.
	// []string{"q", "season", "ep", "tvdbid"}. Empty disables tv-search.
	TVSearchParams []string
	// MovieSearchParams are the movie-search params, e.g. []string{"q"}.
	// Empty disables movie-search.
	MovieSearchParams []string
	// Categories are the newznab category ids the endpoint maps. Prowlarr
	// filters indexers by these before querying, so an endpoint mapping only
	// 2000 is never asked a TV search.
	Categories []int
}

// TorznabRelease is one item an endpoint answers with. The provider ids are
// emitted as torznab attrs only when non-zero, so a release can be left
// deliberately unlabelled — which is what most real releases are.
type TorznabRelease struct {
	Title   string
	GUID    string
	Size    int64
	Seeders int
	TVDBID  uint32
	TMDBID  uint32
	// Category is the newznab id stamped on the item, e.g. 5040.
	Category int
}

// NewTorznabRecorder starts the server and registers its shutdown. Mount the
// endpoints before pointing anything at it.
func NewTorznabRecorder() *TorznabRecorder {
	GinkgoHelper()
	r := &TorznabRecorder{routes: map[string]*TorznabEndpoint{}}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	DeferCleanup(r.srv.Close)
	return r
}

// Port is the host port the server listens on — what StartProwlarr must be
// told to forward, so the container can reach back to this process.
func (r *TorznabRecorder) Port() int {
	GinkgoHelper()
	_, port := splitHostPort(r.srv.URL)
	return port
}

// Mount adds an endpoint at path. Prowlarr appends its own apiPath ("/api"),
// so an endpoint mounted at "/tv" is fetched at "/tv/api".
func (r *TorznabRecorder) Mount(
	path string,
	caps TorznabCaps,
	releases ...TorznabRelease,
) *TorznabEndpoint {
	GinkgoHelper()
	Expect(strings.HasPrefix(path, "/")).To(BeTrue(), "path must be rooted")

	ep := &TorznabEndpoint{
		Path:     path,
		caps:     renderCaps(caps),
		feed:     renderFeed(releases),
		recorder: r,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	Expect(r.routes).NotTo(HaveKey(path), "endpoint already mounted at "+path)
	r.routes[path] = ep
	return ep
}

// FeedURL is the base URL Prowlarr should be configured with for this
// endpoint. host is how the container addresses this process — normally
// host.testcontainers.internal — since the server's own URL names a loopback
// address that means the container itself from inside it.
func (e *TorznabEndpoint) FeedURL(host string) string {
	GinkgoHelper()
	return fmt.Sprintf("http://%s:%d%s", host, e.recorder.Port(), e.Path)
}

// Queries returns every query this endpoint was asked, oldest first.
func (e *TorznabEndpoint) Queries() []url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]url.Values(nil), e.queries...)
}

// SearchQueries returns only the queries that were searches, dropping the
// t=caps probes Prowlarr issues when an indexer is saved and again before it
// queries — those are traffic every spec would otherwise have to skip past.
func (e *TorznabEndpoint) SearchQueries() []url.Values {
	out := []url.Values{}
	for _, q := range e.Queries() {
		if q.Get("t") != "caps" {
			out = append(out, q)
		}
	}
	return out
}

func (r *TorznabRecorder) serve(w http.ResponseWriter, req *http.Request) {
	defer GinkgoRecover()

	// Prowlarr fetches at <baseUrl><apiPath>, so the route is everything
	// ahead of the trailing /api.
	path := strings.TrimSuffix(req.URL.Path, "/api")

	r.mu.Lock()
	ep, ok := r.routes[path]
	r.mu.Unlock()
	if !ok {
		http.Error(w, "no endpoint at "+path, http.StatusNotFound)
		return
	}

	query := req.URL.Query()
	ep.mu.Lock()
	ep.queries = append(ep.queries, query)
	ep.mu.Unlock()

	if query.Get("apikey") != APIKey {
		http.Error(w, "bad apikey", http.StatusUnauthorized)
		return
	}
	switch mode := query.Get("t"); mode {
	case "caps":
		writeXML(w, ep.caps)
	case "search", "movie", "tvsearch":
		writeXML(w, ep.feed)
	default:
		http.Error(w, "unsupported t="+mode, http.StatusBadRequest)
	}
}

func renderCaps(c TorznabCaps) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n<caps>\n")
	b.WriteString(`  <server title="streamline-fake"/>` + "\n")
	b.WriteString(`  <limits max="100" default="100"/>` + "\n")
	b.WriteString("  <searching>\n")
	b.WriteString(`    <search available="yes" supportedParams="q"/>` + "\n")
	b.WriteString(searchMode("tv-search", c.TVSearchParams))
	b.WriteString(searchMode("movie-search", c.MovieSearchParams))
	b.WriteString("  </searching>\n  <categories>\n")
	for _, id := range c.Categories {
		fmt.Fprintf(&b, "    <category id=\"%d\" name=\"cat%d\"/>\n", id, id)
	}
	b.WriteString("  </categories>\n</caps>")
	return b.String()
}

// searchMode renders one <x-search> element. available="no" is what makes
// Prowlarr treat the mode as unsupported; an empty supportedParams on an
// available mode would instead read as "supports nothing but is usable".
func searchMode(name string, params []string) string {
	if len(params) == 0 {
		return fmt.Sprintf("    <%s available=\"no\" supportedParams=\"\"/>\n", name)
	}
	return fmt.Sprintf(
		"    <%s available=\"yes\" supportedParams=\"%s\"/>\n",
		name, strings.Join(params, ","),
	)
}

func renderFeed(releases []TorznabRelease) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(
		`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">` + "\n",
	)
	b.WriteString("<channel>\n")
	for _, r := range releases {
		guid := r.GUID
		if guid == "" {
			guid = r.Title
		}
		b.WriteString("  <item>\n")
		fmt.Fprintf(&b, "    <title>%s</title>\n", escapeXML(r.Title))
		fmt.Fprintf(&b, "    <guid>%s</guid>\n", escapeXML(guid))
		fmt.Fprintf(
			&b,
			"    <enclosure url=\"http://fake.invalid/%s.torrent\" length=\"%d\""+
				" type=\"application/x-bittorrent\"/>\n",
			escapeXML(guid), r.Size,
		)
		fmt.Fprintf(&b, "    <size>%d</size>\n", r.Size)
		b.WriteString(torznabAttr("seeders", fmt.Sprint(r.Seeders)))
		b.WriteString(torznabAttr("peers", fmt.Sprint(r.Seeders)))
		if r.Category != 0 {
			b.WriteString(torznabAttr("category", fmt.Sprint(r.Category)))
		}
		// Emitted only when set: a zero id must reach the client as an absent
		// attr, which is what "the tracker said nothing" means downstream.
		if r.TVDBID != 0 {
			b.WriteString(torznabAttr("tvdbid", fmt.Sprint(r.TVDBID)))
		}
		if r.TMDBID != 0 {
			b.WriteString(torznabAttr("tmdbid", fmt.Sprint(r.TMDBID)))
		}
		b.WriteString("  </item>\n")
	}
	b.WriteString("</channel>\n</rss>")
	return b.String()
}

func torznabAttr(name, value string) string {
	return fmt.Sprintf(
		"    <torznab:attr name=\"%s\" value=\"%s\"/>\n",
		name, escapeXML(value),
	)
}

func escapeXML(s string) string {
	var b strings.Builder
	Expect(xml.EscapeText(&b, []byte(s))).To(Succeed())
	return b.String()
}

func splitHostPort(rawURL string) (string, int) {
	GinkgoHelper()
	u, err := url.Parse(rawURL)
	Expect(err).NotTo(HaveOccurred())
	var port int
	_, err = fmt.Sscanf(u.Port(), "%d", &port)
	Expect(err).NotTo(HaveOccurred())
	return u.Hostname(), port
}
