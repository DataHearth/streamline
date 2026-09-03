// faketracker is a dev-only stand-in for a torrent indexer: it generates
// multi-file season packs with real bytes, seeds them from an in-process
// anacrolix client, answers tracker announces, and serves a Torznab feed
// streamline can be pointed at. Built for exercising selective file download
// against a live dev server without touching a real tracker.
//
// Usage:
//
//	go run ./e2e/faketracker -pack "Breaking Bad:1:10" -pack "Kaamelott:2:8"
//
// Name packs after shows already in the dev library — the keep-set matcher
// resolves episode files against library rows, so an unknown title zero-
// matches and the release is dropped by design. Register the printed indexer
// entry (or POST it to /api/v1/indexers), enable download.selective_files,
// then run an episode search and grab the pack.
//
//	-selftest downloads the first pack through the tracker with a throwaway
//	client and exits; it is the smoke test that announce, seeding and the
//	metainfo are all coherent.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

const (
	pieceLength     = 1 << 20
	episodeSize     = 8 << 20 // above library.MinEpisodeSize so files are skip candidates
	announceSeconds = 60
)

type pack struct {
	title    string // human title, e.g. "Breaking Bad"
	seasonLo int
	seasonHi int // == seasonLo for a single-season pack
	episodes int // per season

	release string // dotted release name, e.g. Breaking.Bad.S01.1080p.WEB-DL.x264-FAKE
	magnet  string
	torrent []byte
	size    int64
}

type packFlags []string

func (p *packFlags) String() string { return strings.Join(*p, ",") }

func (p *packFlags) Set(v string) error {
	*p = append(*p, v)
	return nil
}

func main() {
	var packSpecs packFlags
	listen := flag.String(
		"listen",
		":9117",
		"address for torznab + tracker + torrent downloads",
	)
	dataDir := flag.String(
		"data",
		"/tmp/streamline/faketracker",
		"directory for generated content",
	)
	apiKey := flag.String("apikey", "fake-key", "torznab api key to require")
	selftest := flag.Bool(
		"selftest",
		false,
		"download the first pack via the tracker, then exit",
	)
	magnets := flag.Bool(
		"magnets",
		false,
		"serve magnet links instead of .torrent enclosures",
	)
	flag.Var(
		&packSpecs,
		"pack",
		`repeatable "Title:season:episodes", e.g. "Breaking Bad:1:10"`,
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if len(packSpecs) == 0 {
		fmt.Fprintln(
			os.Stderr,
			`at least one -pack "Title:season:episodes" is required`,
		)
		flag.Usage()
		os.Exit(2)
	}
	if err := run(
		ctx,
		packSpecs,
		*listen,
		*dataDir,
		*apiKey,
		*selftest,
		*magnets,
	); err != nil {
		slog.ErrorContext(ctx, "faketracker failed", "err", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	specs []string,
	listen, dataDir, apiKey string,
	selftest, magnets bool,
) error {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	baseURL := "http://" + hostAddr(ln.Addr())

	packs := make([]*pack, 0, len(specs))
	for _, spec := range specs {
		p, perr := parsePack(spec)
		if perr != nil {
			return perr
		}
		if berr := p.build(dataDir, baseURL+"/announce"); berr != nil {
			return berr
		}
		packs = append(packs, p)
		slog.InfoContext(
			ctx,
			"pack ready",
			"release",
			p.release,
			"files",
			(p.seasonHi-p.seasonLo+1)*p.episodes+2,
			"size",
			p.size,
		)
	}

	tr := &tracker{peers: map[string]map[string]peer{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /announce", tr.announce)
	for _, p := range packs {
		mux.Handle("GET /torrents/"+p.release+".torrent", torrentHandler(p.torrent))
	}
	mux.HandleFunc("/", torznabHandler(packs, baseURL, apiKey, magnets))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if serr := srv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
			slog.ErrorContext(ctx, "http server", "err", serr)
		}
	}()

	seeder, err := newSeeder(ctx, dataDir, packs)
	if err != nil {
		return err
	}
	defer seeder.Close()

	if selftest {
		return runSelftest(ctx, packs[0])
	}

	fmt.Printf(`
faketracker up at %s — indexer entry for the config (or POST /api/v1/indexers):

  indexers:
    - name: faketracker
      host: %s
      port: %s
      api_key: %s
      protocol: torznab
      enabled: true

Content + .torrent files under %s. Ctrl-C to stop.
`, baseURL, hostOnly(ln.Addr()), portOnly(ln.Addr()), apiKey, dataDir)

	<-ctx.Done()
	return srv.Close()
}

func parsePack(spec string) (*pack, error) {
	parts := strings.Split(spec, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("bad -pack %q: want Title:season:episodes", spec)
	}
	lo, hi, ok := strings.Cut(parts[1], "-")
	seasonLo, err := strconv.Atoi(lo)
	if err != nil || seasonLo < 1 {
		return nil, fmt.Errorf(
			"bad -pack %q: season must be N or N-M",
			spec,
		)
	}
	seasonHi := seasonLo
	if ok {
		seasonHi, err = strconv.Atoi(hi)
		if err != nil || seasonHi < seasonLo {
			return nil, fmt.Errorf(
				"bad -pack %q: season range must be N-M with M >= N",
				spec,
			)
		}
	}
	episodes, err := strconv.Atoi(parts[2])
	if err != nil || episodes < 1 {
		return nil, fmt.Errorf(
			"bad -pack %q: episodes must be a positive number",
			spec,
		)
	}
	return &pack{
		title:    strings.TrimSpace(parts[0]),
		seasonLo: seasonLo,
		seasonHi: seasonHi,
		episodes: episodes,
	}, nil
}

// build writes the pack's files to disk and authors its metainfo. Episode
// files carry per-file byte patterns so no two pieces hash alike.
func (p *pack) build(dataDir, announce string) error {
	dotted := strings.ReplaceAll(p.title, " ", ".")
	span := fmt.Sprintf("S%02d", p.seasonLo)
	if p.seasonHi > p.seasonLo {
		// library.ParseSeasonSpan reads S01-S05 as a multi-season integral.
		span = fmt.Sprintf("S%02d-S%02d", p.seasonLo, p.seasonHi)
	}
	p.release = fmt.Sprintf("%s.%s.1080p.WEB-DL.x264-FAKE", dotted, span)
	dir := filepath.Join(dataDir, p.release)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir pack dir: %w", err)
	}

	for season := p.seasonLo; season <= p.seasonHi; season++ {
		for ep := 1; ep <= p.episodes; ep++ {
			name := fmt.Sprintf(
				"%s.S%02dE%02d.1080p.WEB-DL.x264-FAKE.mkv",
				dotted,
				season,
				ep,
			)
			if err := writePattern(
				filepath.Join(dir, name),
				episodeSize,
				byte(uint(season*16+ep)&0xFF),
			); err != nil {
				return err
			}
		}
	}
	sidecars := map[string]string{
		fmt.Sprintf("%s.%s.nfo", dotted, span):                "faketracker release\n",
		fmt.Sprintf("%s.S%02dE01.en.srt", dotted, p.seasonLo): "1\n00:00:00,000 --> 00:00:01,000\nfake\n",
	}
	for name, body := range sidecars {
		if err := os.WriteFile(
			filepath.Join(dir, name),
			[]byte(body),
			0o644,
		); err != nil {
			return fmt.Errorf("write sidecar: %w", err)
		}
	}

	info := metainfo.Info{PieceLength: pieceLength}
	if err := info.BuildFromFilePath(dir); err != nil {
		return fmt.Errorf("build metainfo: %w", err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal info: %w", err)
	}
	mi := metainfo.MetaInfo{Announce: announce, InfoBytes: infoBytes}
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		return fmt.Errorf("write torrent: %w", err)
	}
	p.torrent = buf.Bytes()
	p.size = info.TotalLength()
	p.magnet = fmt.Sprintf(
		"magnet:?xt=urn:btih:%s&dn=%s&tr=%s",
		metainfo.HashBytes(infoBytes).HexString(),
		url.QueryEscape(p.release),
		url.QueryEscape(announce),
	)
	if err := os.WriteFile(
		filepath.Join(dataDir, p.release+".torrent"),
		p.torrent,
		0o644,
	); err != nil {
		return fmt.Errorf("save torrent file: %w", err)
	}
	return nil
}

// writePattern skips rewriting an existing full-size file so restarts reuse
// content — the infohash must stay stable across runs for live records.
func writePattern(path string, size int, seed byte) error {
	if st, err := os.Stat(path); err == nil && st.Size() == int64(size) {
		return nil
	}
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i)*31 + seed
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("write episode file: %w", err)
	}
	return nil
}

func newSeeder(
	ctx context.Context,
	dataDir string,
	packs []*pack,
) (*antorrent.Client, error) {
	cc := antorrent.NewDefaultClientConfig()
	cc.DataDir = dataDir
	cc.Seed = true
	cc.NoDHT = true
	cc.ListenPort = 0
	client, err := antorrent.NewClient(cc)
	if err != nil {
		return nil, fmt.Errorf("seeder client: %w", err)
	}
	for _, p := range packs {
		mi, merr := metainfo.Load(bytes.NewReader(p.torrent))
		if merr != nil {
			client.Close()
			return nil, fmt.Errorf("reload torrent: %w", merr)
		}
		t, terr := client.AddTorrent(mi)
		if terr != nil {
			client.Close()
			return nil, fmt.Errorf("seed %s: %w", p.release, terr)
		}
		<-t.GotInfo()
		if verr := t.VerifyDataContext(ctx); verr != nil {
			client.Close()
			return nil, fmt.Errorf("verify %s: %w", p.release, verr)
		}
		slog.InfoContext(
			ctx,
			"seeding",
			"release",
			p.release,
			"infohash",
			t.InfoHash().HexString(),
		)
	}
	return client, nil
}

// runSelftest leeches the first pack with a throwaway client, proving the
// tracker hands out the seeder and the seeder hands out the bytes.
func runSelftest(ctx context.Context, p *pack) error {
	tmp, err := os.MkdirTemp("/tmp/streamline", "faketracker-selftest-")
	if err != nil {
		return fmt.Errorf("selftest tmp: %w", err)
	}
	defer func() {
		if rerr := os.RemoveAll(tmp); rerr != nil {
			slog.DebugContext(ctx, "selftest cleanup", "err", rerr)
		}
	}()

	cc := antorrent.NewDefaultClientConfig()
	cc.DataDir = tmp
	cc.NoDHT = true
	cc.ListenPort = 0
	client, err := antorrent.NewClient(cc)
	if err != nil {
		return fmt.Errorf("selftest client: %w", err)
	}
	defer client.Close()

	mi, err := metainfo.Load(bytes.NewReader(p.torrent))
	if err != nil {
		return fmt.Errorf("selftest load: %w", err)
	}
	t, err := client.AddTorrent(mi)
	if err != nil {
		return fmt.Errorf("selftest add: %w", err)
	}
	<-t.GotInfo()
	t.DownloadAll()

	deadline := time.After(90 * time.Second)
	for t.BytesMissing() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf(
				"selftest timed out: %d of %d bytes missing",
				t.BytesMissing(), t.Length(),
			)
		case <-time.After(500 * time.Millisecond):
		}
	}
	slog.InfoContext(ctx, "selftest ok", "release", p.release, "bytes", t.Length())
	return nil
}

// ---- tracker ----

type peer struct {
	ip   net.IP
	port uint16
}

type tracker struct {
	mu    sync.Mutex
	peers map[string]map[string]peer // infohash -> "ip:port" -> peer
}

func (tr *tracker) announce(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	infoHash := q.Get("info_hash")
	port, err := strconv.ParseUint(q.Get("port"), 10, 16)
	if infoHash == "" || err != nil {
		http.Error(w, "bad announce", http.StatusBadRequest)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, "bad remote", http.StatusBadRequest)
		return
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		// The seeder and dev server are IPv4-local; a v6 peer has no slot in
		// the compact format this tracker speaks.
		http.Error(w, "ipv4 only", http.StatusBadRequest)
		return
	}

	tr.mu.Lock()
	if tr.peers[infoHash] == nil {
		tr.peers[infoHash] = map[string]peer{}
	}
	self := peer{ip: ip, port: uint16(port)}
	if q.Get("event") == "stopped" {
		delete(tr.peers[infoHash], fmt.Sprintf("%s:%d", ip, port))
	} else {
		tr.peers[infoHash][fmt.Sprintf("%s:%d", ip, port)] = self
	}
	var compact []byte
	for _, p := range tr.peers[infoHash] {
		compact = append(compact, p.ip...)
		compact = binary.BigEndian.AppendUint16(compact, p.port)
	}
	tr.mu.Unlock()

	body, err := bencode.Marshal(map[string]any{
		"interval": announceSeconds,
		"peers":    compact,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	if _, werr := w.Write(body); werr != nil {
		slog.DebugContext(r.Context(), "announce write", "err", werr)
	}
}

// ---- torznab ----

func torrentHandler(body []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		if _, err := w.Write(body); err != nil {
			slog.DebugContext(r.Context(), "torrent write", "err", err)
		}
	})
}

const capsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="faketracker"/>
  <limits max="100" default="50"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="q,season,ep"/>
    <movie-search available="yes" supportedParams="q"/>
  </searching>
  <categories><category id="5000" name="TV"/></categories>
</caps>`

// torznabHandler answers every search flavor with every pack — streamline's
// own parser and matcher do the filtering, which is exactly the code path
// under test.
func torznabHandler(
	packs []*pack,
	baseURL, apiKey string,
	magnets bool,
) http.HandlerFunc {
	var feed strings.Builder
	feed.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	feed.WriteString(
		`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">` + "\n<channel>\n",
	)
	for _, p := range packs {
		var title bytes.Buffer
		if err := xml.EscapeText(&title, []byte(p.release)); err != nil {
			continue
		}
		link := baseURL + "/torrents/" + p.release + ".torrent"
		if magnets {
			// resolveTorrentSource keys its magnet arm on the URI scheme, so a
			// magnet feed is just a different enclosure string.
			var esc bytes.Buffer
			if err := xml.EscapeText(&esc, []byte(p.magnet)); err != nil {
				continue
			}
			link = esc.String()
		}
		fmt.Fprintf(&feed, `  <item>
    <title>%[1]s</title>
    <guid>faketracker-%[1]s</guid>
    <link>%[2]s</link>
    <pubDate>%[3]s</pubDate>
    <size>%[4]d</size>
    <enclosure url="%[2]s" length="%[4]d" type="application/x-bittorrent"/>
    <torznab:attr name="category" value="5000"/>
    <torznab:attr name="seeders" value="5"/>
    <torznab:attr name="peers" value="5"/>
  </item>
`, title.String(), link, time.Now().UTC().Format(time.RFC1123Z), p.size)
	}
	feed.WriteString("</channel>\n</rss>\n")

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != apiKey {
			http.Error(w, "bad apikey", http.StatusUnauthorized)
			return
		}
		switch mode := r.URL.Query().Get("t"); mode {
		case "caps":
			writeXML(w, r, capsXML)
		case "search", "tvsearch", "movie":
			writeXML(w, r, feed.String())
		default:
			http.Error(w, "unsupported t="+mode, http.StatusBadRequest)
		}
	}
}

func writeXML(w http.ResponseWriter, r *http.Request, body string) {
	w.Header().Set("Content-Type", "application/xml")
	if _, err := fmt.Fprint(w, body); err != nil {
		slog.DebugContext(r.Context(), "xml write", "err", err)
	}
}

// ---- address helpers ----

func hostAddr(a net.Addr) string {
	t := a.(*net.TCPAddr)
	if t.IP.IsUnspecified() || t.IP == nil {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(t.Port))
	}
	return t.String()
}

func hostOnly(a net.Addr) string {
	h, _, _ := net.SplitHostPort(hostAddr(a))
	return h
}

func portOnly(a net.Addr) string {
	_, p, _ := net.SplitHostPort(hostAddr(a))
	return p
}
