package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// The proxy sits between the instance and every metadata provider and image
// CDN it calls. The instance trusts its CA (SSL_CERT_FILE) and routes HTTPS
// through it (HTTPS_PROXY), so it needs no seam of its own: TMDB, TVDB,
// MusicBrainz, Hardcover, Deezer, the Cover Art Archive and Wikimedia are all
// answered from the cassette, a directory of one JSON file per exchange.
//
// Replaying, a request the cassette lacks answers 502 and is counted; with
// -record it is sent upstream and kept. A request is keyed by its method, URL
// and body with the provider keys taken out, so a cassette recorded with one
// set of keys replays with none.
//
// The instance also makes calls that are no provider's (offCassette). Those
// are refused the same way in both modes and are neither kept nor counted.

// cassetteMeta is cassette.json: when the recording was started. An exchange
// kept before each carried its own date (exchange.RecordedAt) is taken to be
// from that day.
type cassetteMeta struct {
	RecordedAt time.Time `json:"recorded_at"`
}

// exchange is one recorded response.
type exchange struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	// RequestBody is kept for a reader of the cassette (GraphQL queries are
	// otherwise opaque); it plays no part in the lookup.
	RequestBody string            `json:"request_body,omitempty"`
	Status      int               `json:"status"`
	Header      map[string]string `json:"header,omitempty"`
	Body        string            `json:"body,omitempty"`
	// BodyFile names a sibling file holding a binary body (an image).
	BodyFile string `json:"body_file,omitempty"`
	// Scrubbed marks a body whose credential was replaced. Replaying, the
	// placeholder is all anyone needs; recording, the instance needs the
	// real one for the calls that follow, so the exchange is fetched again.
	Scrubbed bool `json:"scrubbed,omitempty"`
	// RecordedAt is the day this exchange was kept, which replay shifts its
	// recent dates from. A cassette is filled in over time (-record keeps
	// what it lacks), so one date for the whole of it would move a later
	// exchange too far. Zero means cassette.json's.
	RecordedAt time.Time `json:"recorded_at,omitzero"`
}

// offCassette lists the hosts the instance calls for something other than
// metadata or an image: the built-in indexer definitions, a commit lookup
// then a tarball of megabytes from GitHub (internal/cardigann/snapshot). The
// demo configures no built-in indexer, so they are refused in both modes
// rather than recorded, published with the cassette and counted as missing.
var offCassette = map[string]bool{
	"api.github.com":      true,
	"codeload.github.com": true,
}

// keptHeaders are the response headers worth replaying.
var keptHeaders = []string{
	"Content-Type",
	"Retry-After",
	"Location",
	"X-RateLimit-Zone",
}

type proxy struct {
	dir     string
	record  bool
	secrets []string // replaced by "REDACTED" in keys and recordings
	meta    cassetteMeta
	// now is the clock, a field so a spec can replay a cassette later.
	now func() time.Time

	ca     *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPEM  []byte
	certMu sync.Mutex
	certs  map[string]*tls.Certificate

	upstream *http.Client
	writeMu  sync.Mutex

	// paced holds the next moment a request may go upstream to a host
	// that publishes a rate limit; the instance runs unthrottled.
	paceMu sync.Mutex
	paced  map[string]time.Time

	hits, misses, recorded atomic.Int64
	missMu                 sync.Mutex
	missed                 []string
}

func newProxy(dir string, record bool, secrets []string) (*proxy, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := &proxy{
		dir:    dir,
		record: record,
		certs:  map[string]*tls.Certificate{},
		paced:  map[string]time.Time{},
		now:    time.Now,
		upstream: &http.Client{
			Timeout: 2 * time.Minute,
			// Redirects go back to the instance, which follows them through
			// the proxy, so each hop is its own exchange.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				ForceAttemptHTTP2: true,
			},
		},
	}
	for _, s := range secrets {
		if len(s) >= 6 {
			p.secrets = append(p.secrets, s)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "cassette.json")); err == nil {
		if err := json.Unmarshal(raw, &p.meta); err != nil {
			return nil, fmt.Errorf("cassette.json: %w", err)
		}
	}
	if p.meta.RecordedAt.IsZero() {
		if !record {
			return nil, fmt.Errorf(
				"%s holds no recording; run with -record and provider keys first",
				dir,
			)
		}
		p.meta.RecordedAt = p.today()
		raw, _ := json.MarshalIndent(p.meta, "", "  ")
		if err := os.WriteFile(
			filepath.Join(dir, "cassette.json"),
			append(raw, '\n'),
			0o644,
		); err != nil {
			return nil, err
		}
	}
	if err := p.makeCA(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *proxy) today() time.Time {
	return p.now().UTC().Truncate(24 * time.Hour)
}

func (p *proxy) makeCA() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "streamline demo cassette CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	p.ca, err = x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	p.caKey = key
	p.caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return nil
}

func (p *proxy) leaf(host string) (*tls.Certificate, error) {
	p.certMu.Lock()
	defer p.certMu.Unlock()
	if c, ok := p.certs[host]; ok {
		return c, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		tmpl,
		p.ca,
		&key.PublicKey,
		p.caKey,
	)
	if err != nil {
		return nil, err
	}
	c := &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	p.certs[host] = c
	return c, nil
}

// ServeHTTP answers CONNECT by terminating TLS with a certificate for the
// requested host, and plain proxied requests directly.
func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		p.handle(w, r, r.URL.Scheme)
		return
	}
	host := r.URL.Hostname()
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		return
	}
	if _, err := conn.Write(
		[]byte("HTTP/1.1 200 Connection Established\r\n\r\n"),
	); err != nil {
		conn.Close()
		return
	}
	raw := conn
	if buf.Reader.Buffered() > 0 {
		raw = &prefixConn{Conn: conn, r: io.MultiReader(buf.Reader, conn)}
	}
	tconn := tls.Server(raw, &tls.Config{
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name = host
			}
			return p.leaf(name)
		},
	})
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Scheme = "https"
			r.URL.Host = r.Host
			p.handle(w, r, "https")
		}),
		ReadHeaderTimeout: 30 * time.Second,
		ErrorLog:          nil,
	}
	go func() {
		_ = srv.Serve(&oneConnListener{conn: tconn})
	}()
}

func (p *proxy) handle(w http.ResponseWriter, r *http.Request, scheme string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	u := *r.URL
	if u.Scheme == "" {
		u.Scheme = scheme
	}
	key, shown := p.key(r.Method, &u, body)
	if offCassette[u.Hostname()] {
		slog.DebugContext(
			r.Context(),
			"not a provider call",
			"request",
			r.Method+" "+shown,
		)
		w.Header().Set("X-Demo-Cassette", "off")
		http.Error(w, "not a call the demo cassette answers", http.StatusBadGateway)
		return
	}
	file := filepath.Join(p.dir, safeHost(u.Host), key+".json")

	if ex, data, err := p.load(file); err == nil && (!p.record || !ex.Scrubbed) {
		p.hits.Add(1)
		p.write(w, ex, p.shiftDates(ex, data))
		return
	}
	if !p.record {
		p.misses.Add(1)
		p.missMu.Lock()
		p.missed = append(p.missed, r.Method+" "+shown)
		p.missMu.Unlock()
		slog.WarnContext(r.Context(), "cassette miss", "request", r.Method+" "+shown)
		w.Header().Set("X-Demo-Cassette", "miss")
		http.Error(w, "not in the demo cassette", http.StatusBadGateway)
		return
	}

	if err := p.pace(r.Context(), u.Hostname()); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// A forwarding proxy fetches what it is asked for; only the instance this
	// command started talks to it, on loopback.
	//nolint:gosec // G704: the instance's own provider calls, by design
	out, err := http.NewRequestWithContext(
		r.Context(),
		r.Method,
		u.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	for k, vs := range r.Header {
		if hopHeader(k) {
			continue
		}
		out.Header[k] = vs
	}
	resp, err := p.upstream.Do(out) //nolint:gosec // G704: see above
	if err != nil {
		slog.WarnContext(
			r.Context(),
			"upstream failed",
			"request",
			r.Method+" "+shown,
			"error",
			err,
		)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	ex := exchange{
		Method:     r.Method,
		URL:        shown,
		Status:     resp.StatusCode,
		Header:     map[string]string{},
		RecordedAt: p.today(),
	}
	for _, h := range keptHeaders {
		if v := resp.Header.Get(h); v != "" {
			ex.Header[h] = p.redact(v)
		}
	}
	if len(body) > 0 {
		ex.RequestBody = p.redact(string(body))
	}
	// A refusal or an outage is the provider's state at that moment, not the
	// demo's: answer it, keep nothing, and let the next run ask again.
	// Images are kept no wider than the screenshots need, and the instance
	// gets the same copy, so a recording run draws what its replay will.
	if resp.StatusCode == http.StatusOK {
		ct, small := shrinkImage(resp.Header.Get("Content-Type"), data)
		if len(small) < len(data) {
			data = small
			ex.Header["Content-Type"] = ct
		}
	}
	if keep := resp.StatusCode < 500 &&
		resp.StatusCode != http.StatusTooManyRequests &&
		resp.StatusCode != http.StatusUnauthorized &&
		resp.StatusCode != http.StatusForbidden; keep {
		// Only the kept copy is scrubbed: the instance needs the real token
		// for the rest of this recording run.
		kept := p.scrub(data)
		ex.Scrubbed = !bytes.Equal(kept, data)
		if err := p.save(file, &ex, kept); err != nil {
			slog.WarnContext(
				r.Context(),
				"cassette write failed",
				"file",
				file,
				"error",
				err,
			)
		} else {
			p.recorded.Add(1)
		}
	}
	p.write(w, &ex, data)
}

// upstreamGap is the least time between two requests sent upstream to a
// host that publishes a rate limit. The instance runs with no limiter of its
// own (musicbrainz_interval 0s) so that whatever the cassette already holds
// is answered at once, and only what goes upstream waits its turn.
var upstreamGap = map[string]time.Duration{
	"musicbrainz.org": 1100 * time.Millisecond, // one a second, with margin
}

// pace blocks until host may be sent another request.
func (p *proxy) pace(ctx context.Context, host string) error {
	gap, ok := upstreamGap[host]
	if !ok {
		return nil
	}
	p.paceMu.Lock()
	at := time.Now()
	if next := p.paced[host]; next.After(at) {
		at = next
	}
	p.paced[host] = at.Add(gap)
	p.paceMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(at)):
		return nil
	}
}

// key is the exchange's file name: a hash of the method, the URL with its
// query sorted, and the body, all with the provider keys removed. shown is
// the redacted URL, for the cassette and the logs.
func (p *proxy) key(method string, u *url.URL, body []byte) (string, string) {
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		vs := q[k]
		slices.Sort(vs)
		for _, v := range vs {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k) + "=" + url.QueryEscape(p.redact(v)))
		}
	}
	shown := u.Scheme + "://" + u.Host + u.EscapedPath()
	if b.Len() > 0 {
		shown += "?" + b.String()
	}
	shown = p.redact(shown)
	h := sha256.New()
	h.Write([]byte(method + " " + shown + "\n"))
	h.Write([]byte(p.redact(string(body))))
	return hex.EncodeToString(h.Sum(nil))[:24], shown
}

func (p *proxy) redact(s string) string {
	for _, sec := range p.secrets {
		s = strings.ReplaceAll(s, sec, "REDACTED")
		s = strings.ReplaceAll(s, url.QueryEscape(sec), "REDACTED")
	}
	return s
}

// tokenField matches a JSON string field that carries a credential, such as
// the token TVDB's login answers with.
var tokenField = regexp.MustCompile(
	`("(?:token|access_token|refresh_token)"\s*:\s*")[^"]*(")`,
)

func (p *proxy) scrub(data []byte) []byte {
	if !utf8.Valid(data) {
		return data
	}
	s := p.redact(string(data))
	s = tokenField.ReplaceAllString(s, "${1}demo-token${2}")
	return []byte(s)
}

func (p *proxy) save(file string, ex *exchange, data []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	// file is the cassette directory, safeHost's one-element name and a hex
	// hash: no path element comes from the request as it was sent.
	//nolint:gosec // G703: see above
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if utf8.Valid(data) && !bytes.ContainsRune(data, 0) {
		ex.Body = string(data)
		// Indent JSON so a cassette diff reads. Indent keeps every token as
		// it came; a round trip through any would turn large ids into floats.
		var pretty bytes.Buffer
		if json.Indent(&pretty, data, "", "  ") == nil {
			ex.Body = pretty.String()
		}
	} else {
		ex.BodyFile = strings.TrimSuffix(filepath.Base(file), ".json") + ".bin"
		//nolint:gosec // G703: BodyFile is the hash-named sibling of file
		if err := os.WriteFile(
			filepath.Join(filepath.Dir(file), ex.BodyFile),
			data,
			0o644,
		); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		return err
	}
	//nolint:gosec // G703: file is built as above
	return os.WriteFile(file, append(raw, '\n'), 0o644)
}

func (p *proxy) load(file string) (*exchange, []byte, error) {
	//nolint:gosec // G703: file is the cassette path handle built, never the request's
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}
	var ex exchange
	if err := json.Unmarshal(raw, &ex); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", file, err)
	}
	if ex.BodyFile != "" {
		//nolint:gosec // G703: BodyFile is read back from our own cassette entry
		data, err := os.ReadFile(filepath.Join(filepath.Dir(file), ex.BodyFile))
		if err != nil {
			return nil, nil, err
		}
		return &ex, data, nil
	}
	return &ex, []byte(ex.Body), nil
}

func (p *proxy) write(w http.ResponseWriter, ex *exchange, data []byte) {
	for k, v := range ex.Header {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(ex.Status)
	//nolint:gosec // G705: a proxy relays the provider's body to the instance, never to a browser
	_, _ = w.Write(data)
}

// isoDate matches a calendar date, alone or leading a timestamp.
var isoDate = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)

// shiftDates moves every date within two years of the exchange's recording
// forward by the whole weeks since it was made, so what was about to land
// when it was recorded is about to land now. That keeps a calendar entry
// current; it does not make one: a library with no such date has an empty
// calendar at any age (calendarEmpty in shots.go). Older dates are a film's
// or a book's own and stay put.
func (p *proxy) shiftDates(ex *exchange, data []byte) []byte {
	if ex.BodyFile != "" || !strings.Contains(ex.Header["Content-Type"], "json") {
		return data
	}
	recorded := ex.RecordedAt
	if recorded.IsZero() {
		recorded = p.meta.RecordedAt
	}
	// Whole weeks, so a weekly show still airs on its weekday.
	const week = 7 * 24 * time.Hour
	shift := p.now().Sub(recorded) / week * week
	if shift <= 0 {
		return data
	}
	window := 2 * 365 * 24 * time.Hour
	return isoDate.ReplaceAllFunc(data, func(m []byte) []byte {
		t, err := time.Parse("2006-01-02", string(m))
		if err != nil {
			return m
		}
		if d := t.Sub(recorded); d < -window || d > window {
			return m
		}
		return []byte(t.Add(shift).Format("2006-01-02"))
	})
}

func (p *proxy) report() string {
	var b strings.Builder
	fmt.Fprintf(
		&b,
		"cassette: %d answered, %d recorded, %d missing",
		p.hits.Load(),
		p.recorded.Load(),
		p.misses.Load(),
	)
	p.missMu.Lock()
	defer p.missMu.Unlock()
	seen := map[string]bool{}
	for _, m := range p.missed {
		if !seen[m] && len(seen) < 20 {
			seen[m] = true
			b.WriteString("\n  missing: " + m)
		}
	}
	return b.String()
}

// safeHost turns a request's host into one directory name: anything but
// letters, digits, dots and hyphens becomes an underscore, and a name made of
// dots only (".", "..") gets a prefix, so no host can reach outside the
// cassette.
func safeHost(h string) string {
	h = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.',
			r == '-':
			return r
		}
		return '_'
	}, h)
	if strings.Trim(h, ".") == "" {
		h = "_" + h
	}
	return h
}

func hopHeader(k string) bool {
	switch http.CanonicalHeaderKey(k) {
	case "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade", "Accept-Encoding":
		return true
	}
	return false
}

type prefixConn struct {
	net.Conn
	r io.Reader
}

func (c *prefixConn) Read(b []byte) (int, error) { return c.r.Read(b) }

// oneConnListener hands http.Server a single connection, then blocks until
// it is closed.
type oneConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
	mu   sync.Mutex
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.done == nil {
		l.done = make(chan struct{})
	}
	l.mu.Unlock()
	var c net.Conn
	l.once.Do(func() { c = &closeNotify{Conn: l.conn, done: l.done} })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, errors.New("listener closed")
}

func (l *oneConnListener) Close() error   { return nil }
func (l *oneConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

type closeNotify struct {
	net.Conn
	done chan struct{}
	once sync.Once
}

func (c *closeNotify) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}
