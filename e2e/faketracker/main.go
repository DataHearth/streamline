// faketracker is a dev-only stand-in for a torrent indexer: it generates
// season packs, albums, ebooks and audiobooks with real bytes, seeds them
// from an in-process anacrolix client, answers tracker announces, and serves
// a Torznab feed streamline can be pointed at. Built for exercising grabs and
// imports against a live dev server without touching a real tracker.
//
// Usage:
//
//	go run ./e2e/faketracker -pack "Breaking Bad:1:10" -pack "Kaamelott:2:8" \
//	    -album "Boards of Canada:Hi Scores:6" -ebook "Ted Chiang:Exhalation" \
//	    -audiobook "Ted Chiang:Exhalation:3" -movie "Sintel:2010:1080p"
//
// A movie is one MKV whose header states the resolution, codecs and a
// feature-length duration (ffmpeg on PATH required), padded to its size with
// a sparse hole: it passes the importer's probe, or fails it on purpose when
// the spec names a different actual resolution. -rate caps what the seeder
// uploads, so large releases stay in the queue for a while.
//
// Name packs after titles already in the dev library — the keep-set matcher
// resolves episode files against library rows, and the feed matchers resolve
// "Creator - Title" against monitored albums and books, so an unknown title
// zero-matches and the release is dropped by design. Album tracks are tagged
// (artist, album, track number) so the album importer can bind them. Register
// the printed indexer entry (or POST it to /api/v1/indexers), then search and
// grab, or run the rss-sync job.
//
//	-selftest downloads the first pack through the tracker with a throwaway
//	client and exits; it is the smoke test that announce, seeding and the
//	metainfo are all coherent.
//
//	-print-host prints the address the tracker would advertise and exits, so
//	a script can write the indexer entry before the tracker is up.
//
// It runs on any host: the seeding clients skip IPv6 when the host has none,
// content goes under the OS temp directory unless -data says otherwise, and a
// -listen with no host (the default) advertises a non-loopback address of
// this machine — the builtin engine refuses a tracker on loopback or
// link-local, so 127.0.0.1 would leave every torrent fetching forever.
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
	"golang.org/x/time/rate"

	"github.com/datahearth/streamline/e2e/demomedia"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/testutil/nettest"
)

const (
	pieceLength     = 1 << 20
	episodeSize     = 8 << 20 // above library.MinEpisodeSize so files are skip candidates
	patternLimit    = 8 << 20
	announceSeconds = 60
)

type packKind int

const (
	kindTV packKind = iota
	kindAlbum
	kindEbook
	kindAudiobook
	kindMovie
)

type pack struct {
	kind     packKind
	title    string // show, album or book title
	creator  string // artist or author; empty for a show
	seasonLo int
	seasonHi int      // == seasonLo for a single-season pack
	episodes int      // per season; tracks for an album, parts for an audiobook
	tracks   []string // album track titles, when the spec names them
	year     int      // movie
	claimed  int      // movie: the resolution the release name states
	actual   int      // movie: the resolution the file holds
	fileSize int64    // movie file; per episode for a pack (0: the default)

	release  string // release name, e.g. Breaking.Bad.S01.1080p.WEB-DL.x264-FAKE
	category string // torznab category attr
	files    int
	magnet   string
	torrent  []byte
	size     int64
}

type packFlags []string

func (p *packFlags) String() string { return strings.Join(*p, ",") }

func (p *packFlags) Set(v string) error {
	*p = append(*p, v)
	return nil
}

func main() {
	var packSpecs, albumSpecs, ebookSpecs, audiobookSpecs, movieSpecs packFlags
	listen := flag.String(
		"listen",
		":9117",
		"address for torznab + tracker + torrent downloads",
	)
	dataDir := flag.String(
		"data",
		filepath.Join(os.TempDir(), "streamline", "faketracker"),
		"directory for generated content",
	)
	printHost := flag.Bool(
		"print-host",
		false,
		"print the address a host-less -listen advertises, then exit",
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
		`repeatable "Title:season:episodes[:MB per episode]", e.g. "Breaking Bad:1:10"`,
	)
	flag.Var(
		&albumSpecs,
		"album",
		`repeatable "Artist:Album:tracks", e.g. "Boards of Canada:Hi Scores:6"; tracks is a count or "Title|Title|..." to name them`,
	)
	flag.Var(
		&ebookSpecs,
		"ebook",
		`repeatable "Author:Title", e.g. "Ted Chiang:Exhalation"`,
	)
	flag.Var(
		&audiobookSpecs,
		"audiobook",
		`repeatable "Author:Title:parts", e.g. "Ted Chiang:Exhalation:3"`,
	)
	flag.Var(
		&movieSpecs,
		"movie",
		`repeatable "Title:Year:resolution[:MB[:actual resolution]]", e.g. "Sintel:2010:2160p:1500:1080p"`,
	)
	rateKiB := flag.Int(
		"rate",
		0,
		"cap on what the seeder uploads, in KiB/s across every torrent (0: no cap)",
	)
	flag.Parse()

	if *printHost {
		ip, err := nettest.AdvertiseIP()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(ip)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	packs, err := parsePacks(packSpecs, albumSpecs, ebookSpecs, audiobookSpecs, movieSpecs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(2)
	}
	if err := run(
		ctx,
		packs,
		*listen,
		*dataDir,
		*apiKey,
		*selftest,
		*magnets,
		*rateKiB,
	); err != nil {
		slog.ErrorContext(ctx, "faketracker failed", "err", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	packs []*pack,
	listen, dataDir, apiKey string,
	selftest, magnets bool,
	rateKiB int,
) error {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	host, err := hostAddr(ln.Addr())
	if err != nil {
		return err
	}
	baseURL := "http://" + host

	for _, p := range packs {
		if berr := p.build(dataDir, baseURL+"/announce"); berr != nil {
			return berr
		}
		slog.InfoContext(
			ctx,
			"pack ready",
			"release",
			p.release,
			"files",
			p.files,
			"size",
			p.size,
		)
	}

	tr := &tracker{peers: map[string]map[string]peer{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /announce", tr.announce)
	mux.Handle("GET /torrents/{name}", torrentHandler(packs))
	mux.HandleFunc("/", torznabHandler(packs, baseURL, apiKey, magnets))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if serr := srv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
			slog.ErrorContext(ctx, "http server", "err", serr)
		}
	}()

	seeder, err := newSeeder(ctx, dataDir, packs, rateKiB)
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
`, baseURL, hostOnly(host), portOnly(host), apiKey, dataDir)

	<-ctx.Done()
	return srv.Close()
}

func parsePacks(tv, albums, ebooks, audiobooks, movies []string) ([]*pack, error) {
	var packs []*pack
	for _, spec := range tv {
		p, err := parsePack(spec)
		if err != nil {
			return nil, err
		}
		packs = append(packs, p)
	}
	for _, spec := range albums {
		p, err := parseCreatorPack(kindAlbum, "-album", spec, true)
		if err != nil {
			return nil, err
		}
		packs = append(packs, p)
	}
	for _, spec := range ebooks {
		p, err := parseCreatorPack(kindEbook, "-ebook", spec, false)
		if err != nil {
			return nil, err
		}
		packs = append(packs, p)
	}
	for _, spec := range audiobooks {
		p, err := parseCreatorPack(kindAudiobook, "-audiobook", spec, true)
		if err != nil {
			return nil, err
		}
		packs = append(packs, p)
	}
	for _, spec := range movies {
		p, err := parseMovie(spec)
		if err != nil {
			return nil, err
		}
		packs = append(packs, p)
	}
	if len(packs) == 0 {
		return nil, fmt.Errorf(
			"at least one -pack, -album, -ebook, -audiobook or -movie is required",
		)
	}
	return packs, nil
}

// parseCreatorPack reads "Creator:Title[:count]"; counted packs (album
// tracks, audiobook parts) require the third field, an ebook takes none.
func parseCreatorPack(
	kind packKind,
	flagName, spec string,
	counted bool,
) (*pack, error) {
	parts := strings.Split(spec, ":")
	want := 2
	if counted {
		want = 3
	}
	if len(parts) != want {
		shape := "Creator:Title"
		if counted {
			shape += ":count"
		}
		return nil, fmt.Errorf("bad %s %q: want %s", flagName, spec, shape)
	}
	p := &pack{
		kind:    kind,
		creator: strings.TrimSpace(parts[0]),
		title:   strings.TrimSpace(parts[1]),
	}
	if p.creator == "" || p.title == "" {
		return nil, fmt.Errorf(
			"bad %s %q: creator and title must be set",
			flagName,
			spec,
		)
	}
	if counted && kind == kindAlbum && strings.Contains(parts[2], "|") {
		// Named tracks: the album importer holds a file whose title names
		// another track than its number, so a demo album carries the real ones.
		p.tracks = strings.Split(parts[2], "|")
		p.episodes = len(p.tracks)
		return p, nil
	}
	if counted {
		n, err := strconv.Atoi(parts[2])
		if err != nil || n < 1 {
			return nil, fmt.Errorf(
				"bad %s %q: count must be a positive number",
				flagName,
				spec,
			)
		}
		p.episodes = n
	}
	return p, nil
}

// parseMovie reads "Title:Year:resolution[:MB[:actual resolution]]".
func parseMovie(spec string) (*pack, error) {
	parts := strings.Split(spec, ":")
	if len(parts) < 3 || len(parts) > 5 {
		return nil, fmt.Errorf("bad -movie %q: want Title:Year:resolution[:MB[:actual]]", spec)
	}
	p := &pack{kind: kindMovie, title: strings.TrimSpace(parts[0])}
	year, err := strconv.Atoi(parts[1])
	if err != nil || year < 1800 || p.title == "" {
		return nil, fmt.Errorf("bad -movie %q: want a title and a year", spec)
	}
	p.year = year
	if p.claimed, err = parseHeight(parts[2]); err != nil {
		return nil, fmt.Errorf("bad -movie %q: %w", spec, err)
	}
	p.actual = p.claimed
	if len(parts) > 3 && parts[3] != "" {
		mb, err := strconv.Atoi(parts[3])
		if err != nil || mb < 1 {
			return nil, fmt.Errorf("bad -movie %q: size must be a positive number of MB", spec)
		}
		p.fileSize = int64(mb) << 20
	}
	if len(parts) > 4 {
		if p.actual, err = parseHeight(parts[4]); err != nil {
			return nil, fmt.Errorf("bad -movie %q: %w", spec, err)
		}
	}
	return p, nil
}

func parseHeight(s string) (int, error) {
	h, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "p"))
	if err != nil || (h != 2160 && h != 1080 && h != 720 && h != 480) {
		return 0, fmt.Errorf("resolution %q is not one of 2160p, 1080p, 720p, 480p", s)
	}
	return h, nil
}

func parsePack(spec string) (*pack, error) {
	parts := strings.Split(spec, ":")
	if len(parts) != 3 && len(parts) != 4 {
		return nil, fmt.Errorf("bad -pack %q: want Title:season:episodes[:MB]", spec)
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
	var size int64
	if len(parts) == 4 {
		mb, err := strconv.Atoi(parts[3])
		if err != nil || mb < 1 {
			return nil, fmt.Errorf("bad -pack %q: size must be a positive number of MB", spec)
		}
		size = int64(mb) << 20
	}
	return &pack{
		title:    strings.TrimSpace(parts[0]),
		seasonLo: seasonLo,
		seasonHi: seasonHi,
		episodes: episodes,
		fileSize: size,
	}, nil
}

// build writes the pack's files to disk and authors its metainfo. Episode
// files carry per-file byte patterns so no two pieces hash alike.
func (p *pack) build(dataDir, announce string) error {
	var write func(dir string) error
	switch p.kind {
	case kindAlbum:
		// "Creator - Title" is what splitCreatorTitle wants, [MP3 320] is the
		// format tier of the embedded template.
		p.release = fmt.Sprintf("%s - %s [MP3 320]", p.creator, p.title)
		p.category = "3000"
		write = p.writeAlbum
	case kindEbook:
		p.release = fmt.Sprintf("%s - %s (EPUB)", p.creator, p.title)
		p.category = "7020"
		write = p.writeEbook
	case kindAudiobook:
		p.release = fmt.Sprintf(
			"%s - %s (Unabridged) [MP3] Audiobook",
			p.creator,
			p.title,
		)
		p.category = "3030"
		write = p.writeAudiobook
	case kindMovie:
		codec := "x265"
		if p.claimed < 1080 {
			codec = "x264"
		}
		p.release = fmt.Sprintf(
			"%s.%d.%dp.WEB-DL.DDP5.1.%s-DEMO",
			strings.ReplaceAll(p.title, " ", "."), p.year, p.claimed, codec,
		)
		p.category = "2000"
		write = p.writeMovie
	default:
		dotted := strings.ReplaceAll(p.title, " ", ".")
		span := fmt.Sprintf("S%02d", p.seasonLo)
		if p.seasonHi > p.seasonLo {
			// library.ParseSeasonSpan reads S01-S05 as a multi-season integral.
			span = fmt.Sprintf("S%02d-S%02d", p.seasonLo, p.seasonHi)
		}
		p.release = fmt.Sprintf("%s.%s.1080p.WEB-DL.x264-FAKE", dotted, span)
		p.category = "5000"
		write = p.writeSeasons
	}
	dir := filepath.Join(dataDir, p.release)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir pack dir: %w", err)
	}
	if err := write(dir); err != nil {
		return err
	}
	return p.author(dataDir, dir, announce)
}

func (p *pack) writeSeasons(dir string) error {
	dotted := strings.ReplaceAll(p.title, " ", ".")
	span := fmt.Sprintf("S%02d", p.seasonLo)
	if p.seasonHi > p.seasonLo {
		span = fmt.Sprintf("S%02d-S%02d", p.seasonLo, p.seasonHi)
	}
	for season := p.seasonLo; season <= p.seasonHi; season++ {
		for ep := 1; ep <= p.episodes; ep++ {
			name := fmt.Sprintf(
				"%s.S%02dE%02d.1080p.WEB-DL.x264-FAKE.mkv",
				dotted,
				season,
				ep,
			)
			size := int64(episodeSize)
			if p.fileSize > 0 {
				size = p.fileSize
			}
			if err := writePattern(
				filepath.Join(dir, name),
				size,
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
	return nil
}

// writeTrack tags a copy of the MP3 template so the album importer can bind
// the file to a track by number (and by title, when the spec names them).
func (p *pack) writeTrack(dir, name string, n int) error {
	title := fmt.Sprintf("Track %02d", n)
	if len(p.tracks) >= n {
		title = p.tracks[n-1]
	}
	return demomedia.Track(filepath.Join(dir, name), audiotags.Tags{
		audiotags.KeyArtist:      {p.creator},
		audiotags.KeyAlbumArtist: {p.creator},
		audiotags.KeyAlbum:       {p.title},
		audiotags.KeyTitle:       {title},
		audiotags.KeyTrackNumber: {strconv.Itoa(n)},
		audiotags.KeyDiscNumber:  {"1"},
	})
}

func (p *pack) writeAlbum(dir string) error {
	for n := 1; n <= p.episodes; n++ {
		name := fmt.Sprintf("%02d - Track %02d.mp3", n, n)
		if len(p.tracks) >= n {
			name = fmt.Sprintf("%02d - %s.mp3", n, safeName(p.tracks[n-1]))
		}
		if err := p.writeTrack(dir, name, n); err != nil {
			return err
		}
	}
	return nil
}

func (p *pack) writeAudiobook(dir string) error {
	for n := 1; n <= p.episodes; n++ {
		name := fmt.Sprintf("%s - Part %02d.mp3", p.title, n)
		if err := p.writeTrack(dir, name, n); err != nil {
			return err
		}
	}
	return nil
}

// writeEbook authors a minimal EPUB carrying the title and author.
func (p *pack) writeEbook(dir string) error {
	return demomedia.Ebook(
		filepath.Join(dir, fmt.Sprintf("%s - %s.epub", p.creator, p.title)),
		demomedia.EbookMeta{
			Title:  p.title,
			Author: p.creator,
			ID:     "urn:faketracker:" + p.title,
		},
	)
}

// writeMovie writes one MKV that probes at the actual resolution with a
// feature-length duration, padded to the release's size.
func (p *pack) writeMovie(dir string) error {
	size := p.fileSize
	if size == 0 {
		size = 64 << 20
	}
	return demomedia.Video(filepath.Join(dir, p.release+".mkv"), demomedia.VideoSpec{
		Height: p.actual,
		HDR:    p.actual == 2160,
		Size:   size,
	})
}

// safeName drops the characters a file name cannot carry on any platform.
func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, s)
}

// author builds the metainfo over dir, seeds the magnet and saves the
// .torrent beside the content.
func (p *pack) author(dataDir, dir, announce string) error {
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
	p.files = len(info.Files)
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
// Content past the first patternLimit bytes is a sparse hole, so a pack of
// large episodes costs no disk on the seeding side.
func writePattern(path string, size int64, seed byte) error {
	if st, err := os.Stat(path); err == nil && st.Size() == size {
		return nil
	}
	buf := make([]byte, min(size, patternLimit))
	for i := range buf {
		buf[i] = byte(i)*31 + seed
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("write episode file: %w", err)
	}
	if err := os.Truncate(path, size); err != nil {
		return fmt.Errorf("pad episode file: %w", err)
	}
	return nil
}

func newSeeder(
	ctx context.Context,
	dataDir string,
	packs []*pack,
	rateKiB int,
) (*antorrent.Client, error) {
	cc := antorrent.NewDefaultClientConfig()
	cc.DataDir = dataDir
	cc.Seed = true
	cc.NoDHT = true
	cc.ListenPort = 0
	cc.DisableIPv6 = !nettest.IPv6()
	if rateKiB > 0 {
		// The burst must hold a whole chunk (16 KiB), or no request is ever
		// served.
		cc.UploadRateLimiter = rate.NewLimiter(rate.Limit(rateKiB<<10), max(rateKiB<<10, 1<<16))
	}
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
	root := filepath.Join(os.TempDir(), "streamline")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("selftest tmp: %w", err)
	}
	tmp, err := os.MkdirTemp(root, "faketracker-selftest-")
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
	cc.DisableIPv6 = !nettest.IPv6()
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

func torrentHandler(packs []*pack) http.Handler {
	byName := make(map[string][]byte, len(packs))
	for _, p := range packs {
		byName[p.release+".torrent"] = p.torrent
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := byName[r.PathValue("name")]
		if !ok {
			http.NotFound(w, r)
			return
		}
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
  <categories>
    <category id="2000" name="Movies"/>
    <category id="3000" name="Audio"><subcat id="3030" name="Audio/Audiobook"/></category>
    <category id="5000" name="TV"/>
    <category id="7000" name="Books"><subcat id="7020" name="Books/EBook"/></category>
  </categories>
</caps>`

// torznabHandler answers every search flavor with every pack in the requested
// categories (all of them without cat=) — streamline's own parser and matcher
// do the rest of the filtering, which is exactly the code path under test.
func torznabHandler(
	packs []*pack,
	baseURL, apiKey string,
	magnets bool,
) http.HandlerFunc {
	items := make(map[*pack]string, len(packs))
	for _, p := range packs {
		var title bytes.Buffer
		if err := xml.EscapeText(&title, []byte(p.release)); err != nil {
			continue
		}
		raw := baseURL + "/torrents/" + url.PathEscape(p.release+".torrent")
		if magnets {
			// resolveTorrentSource keys its magnet arm on the URI scheme, so a
			// magnet feed is just a different enclosure string.
			raw = p.magnet
		}
		// PathEscape leaves "&" alone (it is legal in a path), so a release
		// named "Feathers & Flesh" needs the XML escape as much as a magnet.
		var esc bytes.Buffer
		if err := xml.EscapeText(&esc, []byte(raw)); err != nil {
			continue
		}
		link := esc.String()
		items[p] = fmt.Sprintf(`  <item>
    <title>%[1]s</title>
    <guid>faketracker-%[1]s</guid>
    <link>%[2]s</link>
    <pubDate>%[3]s</pubDate>
    <size>%[4]d</size>
    <enclosure url="%[2]s" length="%[4]d" type="application/x-bittorrent"/>
    <torznab:attr name="category" value="%[5]s"/>
    <torznab:attr name="seeders" value="5"/>
    <torznab:attr name="peers" value="5"/>
  </item>
`, title.String(), link, time.Now().UTC().Format(time.RFC1123Z), p.size, p.category)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != apiKey {
			http.Error(w, "bad apikey", http.StatusUnauthorized)
			return
		}
		switch mode := r.URL.Query().Get("t"); mode {
		case "caps":
			writeXML(w, r, capsXML)
		case "search", "tvsearch", "movie":
			var feed strings.Builder
			feed.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
			feed.WriteString(
				`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">` + "\n<channel>\n",
			)
			for _, p := range packs {
				if inCategories(p.category, r.URL.Query().Get("cat")) {
					feed.WriteString(items[p])
				}
			}
			feed.WriteString("</channel>\n</rss>\n")
			writeXML(w, r, feed.String())
		default:
			http.Error(w, "unsupported t="+mode, http.StatusBadRequest)
		}
	}
}

// inCategories mirrors a real tracker's cat= handling: no filter matches all,
// a parent id (3000) matches its subcategories (3030), an exact id itself.
func inCategories(cat, filter string) bool {
	if filter == "" {
		return true
	}
	for c := range strings.SplitSeq(filter, ",") {
		c = strings.TrimSpace(c)
		if c == cat || (strings.HasSuffix(c, "000") && c[:1] == cat[:1]) {
			return true
		}
	}
	return false
}

func writeXML(w http.ResponseWriter, r *http.Request, body string) {
	w.Header().Set("Content-Type", "application/xml")
	if _, err := fmt.Fprint(w, body); err != nil {
		slog.DebugContext(r.Context(), "xml write", "err", err)
	}
}

// ---- address helpers ----

// hostAddr is the host:port the tracker advertises in its announce URLs and
// feed links. A listener on every interface advertises this machine's own
// non-loopback address rather than 127.0.0.1, which the builtin engine
// refuses to announce to.
func hostAddr(a net.Addr) (string, error) {
	t := a.(*net.TCPAddr)
	if t.IP == nil || t.IP.IsUnspecified() {
		ip, err := nettest.AdvertiseIP()
		if err != nil {
			return "", fmt.Errorf(
				"pick an address to advertise (pass -listen host:port): %w",
				err,
			)
		}
		return net.JoinHostPort(ip.String(), strconv.Itoa(t.Port)), nil
	}
	return t.String(), nil
}

func hostOnly(hostPort string) string {
	h, _, _ := net.SplitHostPort(hostPort)
	return h
}

func portOnly(hostPort string) string {
	_, p, _ := net.SplitHostPort(hostPort)
	return p
}
