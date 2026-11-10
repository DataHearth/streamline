package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// release is one item the fake indexer serves and the fake client reports.
// Everything the queue shows about it (state, progress, speed, ETA) is fixed
// here, so a run always lands on the same picture.
type release struct {
	Name     string
	Category string // Torznab category: 2000 movies, 5000 TV, 3000 music, 3030 audiobooks, 7020 ebooks
	Size     int64
	// Match is the set of folded words a search query must hold for the
	// indexer to answer with this release. Streamline's own parser and
	// matcher filter what comes back, as they would on a real tracker.
	Match []string
	// Age is how long ago the indexer says it was published.
	Age time.Duration

	// The client's view once grabbed. A release whose files are on disk
	// under the download path is reported complete and imported for real.
	State    string // qBittorrent state: downloading, stalledDL, pausedDL, stalledUP
	Progress float64
	Speed    int64 // bytes/s
	ETA      int64 // seconds
	// Seeders is what the indexer claims; 0 reads as 12.
	Seeders int
}

func (r *release) hash() string {
	sum := sha1.Sum([]byte(r.Name))
	return hex.EncodeToString(sum[:])
}

func (r *release) magnet() string {
	return "magnet:?xt=urn:btih:" + r.hash() + "&dn=" + url.QueryEscape(r.Name)
}

var foldRe = regexp.MustCompile(`[^a-z0-9]+`)

// words folds a string to its lower-case alphanumeric words.
func words(s string) []string {
	return strings.Fields(foldRe.ReplaceAllString(strings.ToLower(s), " "))
}

// torznab answers every search with the releases whose Match words the query
// holds, in the requested categories. An empty query (an RSS poll) answers
// nothing, so the instance's own RSS job never grabs on its own.
type torznab struct {
	releases []*release
}

func (t *torznab) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch q.Get("t") {
	case "caps":
		w.Header().Set("Content-Type", "application/xml")
		_, _ = fmt.Fprint(w, capsXML)
		return
	case "search", "tvsearch", "movie", "music", "book":
	default:
		http.Error(w, "unsupported", http.StatusBadRequest)
		return
	}
	have := map[string]bool{}
	for _, w := range words(q.Get("q") + " " + q.Get("artist") + " " + q.Get("album") + " " + q.Get("title") + " " + q.Get("author")) {
		have[w] = true
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(
		`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>` + "\n",
	)
	if len(have) > 0 {
		for _, rel := range t.releases {
			if !inCategories(rel.Category, q.Get("cat")) ||
				!holdsAll(have, rel.Match) {
				continue
			}
			writeItem(&b, rel)
		}
	}
	b.WriteString("</channel></rss>\n")
	w.Header().Set("Content-Type", "application/rss+xml")
	_, _ = fmt.Fprint(w, b.String())
}

func holdsAll(have map[string]bool, want []string) bool {
	if len(want) == 0 {
		return false
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

func inCategories(cat, filter string) bool {
	if filter == "" {
		return true
	}
	for f := range strings.SplitSeq(filter, ",") {
		if f == cat ||
			(len(f) == 4 && strings.HasSuffix(f, "000") && cat[:1] == f[:1]) {
			return true
		}
	}
	return false
}

func writeItem(b *strings.Builder, rel *release) {
	esc := func(s string) string {
		var sb strings.Builder
		_ = xml.EscapeText(&sb, []byte(s))
		return sb.String()
	}
	seeders := rel.Seeders
	if seeders == 0 {
		seeders = 12
	}
	link := esc(rel.magnet())
	fmt.Fprintf(
		b,
		`<item><title>%s</title><guid>demo-%s</guid><link>%s</link><pubDate>%s</pubDate><size>%d</size>`+
			`<enclosure url="%s" length="%d" type="application/x-bittorrent"/>`+
			`<torznab:attr name="category" value="%s"/><torznab:attr name="seeders" value="%d"/><torznab:attr name="peers" value="%d"/></item>`+"\n",
		esc(
			rel.Name,
		),
		rel.hash(),
		link,
		time.Now().Add(-rel.Age).UTC().Format(time.RFC1123Z),
		rel.Size,
		link,
		rel.Size,
		rel.Category,
		seeders,
		seeders+3,
	)
}

const capsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="demo"/>
  <limits max="100" default="100"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="q,season,ep"/>
    <movie-search available="yes" supportedParams="q"/>
    <music-search available="yes" supportedParams="q"/>
    <book-search available="yes" supportedParams="q"/>
  </searching>
  <categories>
    <category id="2000" name="Movies"/>
    <category id="3000" name="Audio"><subcat id="3030" name="Audio/Audiobook"/></category>
    <category id="5000" name="TV"/>
    <category id="7000" name="Books"><subcat id="7020" name="Books/EBook"/></category>
  </categories>
</caps>`

// qbittorrent fakes the slice of qBittorrent's WebUI API streamline speaks.
// A torrent added by magnet is looked up by name in the catalog and reported
// with the state the catalog gives it; its files are whatever lies under
// downloadDir/<name>, or one synthetic file when nothing does.
type qbittorrent struct {
	downloadDir string
	byName      map[string]*release

	mu       sync.Mutex
	torrents map[string]*qbTorrent // by hash
	order    []string
}

type qbTorrent struct {
	rel     *release
	hash    string
	added   time.Time
	stopped bool
}

func newQBittorrent(downloadDir string, releases []*release) *qbittorrent {
	q := &qbittorrent{
		downloadDir: downloadDir,
		byName:      map[string]*release{},
		torrents:    map[string]*qbTorrent{},
	}
	for _, r := range releases {
		q.byName[r.Name] = r
	}
	return q
}

func (q *qbittorrent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v2/auth/login":
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "demo"})
		_, _ = fmt.Fprint(w, "Ok.")
	case "/api/v2/app/version":
		_, _ = fmt.Fprint(w, "v5.1.2")
	case "/api/v2/torrents/createCategory":
		w.WriteHeader(http.StatusOK)
	case "/api/v2/sync/maindata":
		writeJSON(w, map[string]any{
			"rid":         1,
			"full_update": true,
			"server_state": map[string]any{
				"free_space_on_disk": int64(12_094_627_905_536),
			},
		})
	case "/api/v2/torrents/add":
		q.add(w, r)
	case "/api/v2/torrents/info":
		q.info(w, r)
	case "/api/v2/torrents/files":
		q.files(w, r)
	case "/api/v2/torrents/delete":
		_ = r.ParseForm()
		q.mu.Lock()
		for h := range strings.SplitSeq(r.FormValue("hashes"), "|") {
			delete(q.torrents, h)
		}
		q.mu.Unlock()
	case "/api/v2/torrents/stop", "/api/v2/torrents/pause":
		q.setStopped(r, true)
	case "/api/v2/torrents/start", "/api/v2/torrents/resume":
		q.setStopped(r, false)
	case "/api/v2/torrents/filePrio":
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func (q *qbittorrent) add(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	//nolint:gosec // the body is capped by MaxBytesReader just above
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := url.Parse(r.FormValue("urls"))
	if err != nil || u.Scheme != "magnet" {
		http.Error(w, "magnets only", http.StatusUnsupportedMediaType)
		return
	}
	name := u.Query().Get("dn")
	rel, ok := q.byName[name]
	if !ok {
		http.Error(w, "unknown release "+name, http.StatusUnsupportedMediaType)
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	h := rel.hash()
	if _, dup := q.torrents[h]; dup {
		w.WriteHeader(http.StatusConflict)
		return
	}
	q.torrents[h] = &qbTorrent{rel: rel, hash: h, added: time.Now()}
	q.order = append(q.order, h)
	writeJSON(
		w,
		map[string]any{"added_torrent_ids": []string{h}, "success_count": 1},
	)
}

func (q *qbittorrent) setStopped(r *http.Request, stopped bool) {
	_ = r.ParseForm()
	q.mu.Lock()
	defer q.mu.Unlock()
	for h := range strings.SplitSeq(r.FormValue("hashes"), "|") {
		if t := q.torrents[h]; t != nil {
			t.stopped = stopped
		}
	}
}

func (q *qbittorrent) info(w http.ResponseWriter, r *http.Request) {
	want := map[string]bool{}
	if hs := r.URL.Query().Get("hashes"); hs != "" {
		for h := range strings.SplitSeq(hs, "|") {
			want[h] = true
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	out := []map[string]any{}
	for _, h := range q.order {
		t := q.torrents[h]
		if t == nil || (len(want) > 0 && !want[h]) {
			continue
		}
		rel := t.rel
		state, progress, speed, eta := rel.State, rel.Progress, rel.Speed, rel.ETA
		if q.onDisk(rel) {
			state, progress, speed, eta = "stalledUP", 1, 0, 8640000
		}
		if t.stopped {
			state, speed, eta = "stoppedDL", 0, 8640000
			if progress >= 1 {
				state = "stoppedUP"
			}
		}
		out = append(out, map[string]any{
			"hash":      h,
			"name":      rel.Name,
			"state":     state,
			"progress":  progress,
			"size":      rel.Size,
			"save_path": q.downloadDir,
			"dlspeed":   speed,
			"eta":       eta,
			"category":  "streamline",
			"added_on":  t.added.Unix(),
		})
	}
	writeJSON(w, out)
}

func (q *qbittorrent) onDisk(rel *release) bool {
	_, err := os.Stat(filepath.Join(q.downloadDir, rel.Name))
	return err == nil
}

func (q *qbittorrent) files(w http.ResponseWriter, r *http.Request) {
	h := r.URL.Query().Get("hash")
	q.mu.Lock()
	t := q.torrents[h]
	q.mu.Unlock()
	out := []map[string]any{}
	if t == nil {
		writeJSON(w, out)
		return
	}
	root := filepath.Join(q.downloadDir, t.rel.Name)
	var names []string
	sizes := map[string]int64{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, _ := filepath.Rel(q.downloadDir, p)
		names = append(names, filepath.ToSlash(rel))
		sizes[filepath.ToSlash(rel)] = info.Size()
		return nil
	})
	if len(names) == 0 {
		name := t.rel.Name + "/" + t.rel.Name + syntheticExt(t.rel.Category)
		names, sizes[name] = []string{name}, t.rel.Size
	}
	sort.Strings(names)
	for i, n := range names {
		out = append(
			out,
			map[string]any{
				"index":    i,
				"name":     n,
				"size":     sizes[n],
				"priority": 1,
				"progress": t.rel.Progress,
			},
		)
	}
	writeJSON(w, out)
}

func syntheticExt(category string) string {
	switch category {
	case "3000":
		return ".flac"
	case "3030":
		return ".m4b"
	case "7020":
		return ".epub"
	}
	return ".mkv"
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// serve listens on a free port of host and serves h until the process exits.
func serve(host string, h http.Handler) (string, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return "", err
	}
	go func() { _ = http.Serve(ln, h) }() //nolint:gosec // a throwaway fake for the length of the run
	return ln.Addr().String(), nil
}
