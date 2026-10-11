// Package engine runs a converted Cardigann definition: it renders the
// definition's search requests, fetches them and turns the responses into
// releases, and resolves a release's download link the way the definition's
// download block says to — the job Prowlarr's CardigannRequestGenerator and
// CardigannParser do, ported against the converted model.
//
// The definition reaches here already rewritten for Go (internal/cardigann/
// convert); what the converter could not bake in is the engine contract in
// docs/agents/cardigann.md, and this package is where it is honoured.
//
// Login flows are not implemented yet: New refuses a definition with a login
// block.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/unicode"

	"github.com/datahearth/streamline/internal/cardigann"
)

var (
	// ErrLoginUnsupported is a definition that needs a login flow, which the
	// engine does not run yet.
	ErrLoginUnsupported = errors.New("definitions that log in are not supported yet")
	// ErrStatus is a response the engine cannot read as results: an
	// unexpected status, or a redirect a search path does not follow —
	// Prowlarr's cue that a session expired.
	ErrStatus = errors.New("unexpected response")
	// ErrParse is a response that does not have the shape the definition
	// describes.
	ErrParse = errors.New("unreadable response")
)

const (
	// minRequestGap is Prowlarr's own floor between two requests to one
	// indexer; a definition's requestDelay only ever raises it.
	minRequestGap = 2 * time.Second
	// regexTimeout bounds a backtracking match against a page the tracker
	// controls.
	regexTimeout = 2 * time.Second
	// maxBody bounds one tracker response.
	maxBody = 16 << 20
	// userAgent is a desktop browser's, as Jackett sends: tracker front ends
	// commonly refuse a client that names itself.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:139.0) Gecko/20100101 Firefox/139.0"
)

// Options configures one indexer built from a definition.
type Options struct {
	// Name keys the per-indexer request pacing.
	Name string
	// Site overrides the definition's first link; it must be one of the
	// definition's links.
	Site string
	// Settings holds the operator's values by setting name. A setting left
	// out takes the definition's default.
	Settings map[string]string
	// Client is the HTTP client requests go through. Redirect handling and
	// the cookie jar are the engine's own, layered on a copy.
	Client *http.Client
	// Now is the clock dates are read against; nil is time.Now.
	Now func() time.Time
}

// Engine is one configured indexer.
type Engine struct {
	def      *cardigann.Definition
	site     *url.URL
	config   map[string]string
	cats     categoryMap
	client   *http.Client
	encoding encoding.Encoding
	pace     *pacer
	clock    func() time.Time
}

// New checks a definition and the operator's settings for it.
func New(def *cardigann.Definition, o Options) (*Engine, error) {
	if def.Login != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, ErrLoginUnsupported)
	}
	site, err := Site(def, o.Site)
	if err != nil {
		return nil, err
	}
	config, err := Config(def, o.Settings)
	if err != nil {
		return nil, err
	}
	config["sitelink"] = site.String()

	enc, err := htmlindex.Get(def.Encoding)
	if err != nil {
		return nil, fmt.Errorf("%s: encoding %q: %w", def.ID, def.Encoding, err)
	}
	if enc == unicode.UTF8 {
		enc = nil
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	base := o.Client
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	client.Jar = jar

	gap := minRequestGap
	if d := time.Duration(def.RequestDelay * float64(time.Second)); d > gap {
		gap = d
	}
	clock := o.Now
	if clock == nil {
		clock = time.Now
	}
	return &Engine{
		def:      def,
		site:     site,
		config:   config,
		cats:     newCategoryMap(def.Caps),
		client:   &client,
		encoding: enc,
		pace:     pacerFor(o.Name, gap),
		clock:    clock,
	}, nil
}

func (e *Engine) now() time.Time { return e.clock() }

// Site resolves the site a definition is reached at: the override when it is
// one of the definition's links, else the first link. It always ends in '/',
// so a relative path resolves beneath it.
func Site(def *cardigann.Definition, override string) (*url.URL, error) {
	link := def.Links[0]
	if override != "" {
		norm := strings.TrimSuffix(override, "/") + "/"
		if !slices.ContainsFunc(def.Links, func(l string) bool {
			return strings.TrimSuffix(l, "/")+"/" == norm
		}) {
			return nil, fmt.Errorf(
				"%s: site %q is not one of the definition's links %v",
				def.ID, override, def.Links,
			)
		}
		link = norm
	}
	u, err := url.Parse(strings.TrimSuffix(link, "/") + "/")
	if err != nil {
		return nil, fmt.Errorf("%s: link %q: %w", def.ID, link, err)
	}
	return u, nil
}

// Config resolves the operator's settings against the definition's, the way
// Prowlarr's GetBaseTemplateVariables does: a checkbox reads ".True" when
// ticked and "" when not, a select its option key, text and password as
// given. An unknown setting, an option no select offers and a checkbox value
// that is not a boolean are refused.
func Config(
	def *cardigann.Definition,
	values map[string]string,
) (map[string]string, error) {
	out := map[string]string{}
	var errs []error
	for name := range values {
		if !slices.ContainsFunc(def.Settings, func(s cardigann.Setting) bool {
			return s.Name == name
		}) {
			errs = append(errs, fmt.Errorf("unknown setting %q", name))
		}
	}
	for _, s := range def.Settings {
		value, set := values[s.Name]
		if !set && s.Default != nil {
			value = string(*s.Default)
		}
		switch s.Type {
		case "text", "password":
			out[s.Name] = value
		case "checkbox":
			on := false
			if value != "" {
				b, err := strconv.ParseBool(value)
				if err != nil {
					errs = append(errs, fmt.Errorf(
						"setting %q: %q is not true or false", s.Name, value,
					))
				}
				on = b
			}
			if on {
				out[s.Name] = ".True"
			} else {
				out[s.Name] = ""
			}
		case "select":
			if !set {
				value = selectDefault(s, value)
			}
			if _, ok := s.Options.Get(value); !ok {
				keys := make([]string, len(s.Options))
				for i, o := range s.Options {
					keys[i] = o.Key
				}
				errs = append(errs, fmt.Errorf(
					"setting %q: %q is not one of %v", s.Name, value, keys,
				))
			}
			out[s.Name] = value
		default:
			// info blocks and the like carry no value.
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s: %w", def.ID, errors.Join(errs...))
	}
	return out, nil
}

// selectDefault reads a select's default as one of its option keys. Two
// upstream definitions default to a key their options do not hold ("DESC"
// beside "desc", "t" beside nt/ts/sz/i), which Prowlarr indexes as -1 and
// fails on: a case-insensitive match is taken, else the first option.
func selectDefault(s cardigann.Setting, value string) string {
	if _, ok := s.Options.Get(value); ok || len(s.Options) == 0 {
		return value
	}
	for _, o := range s.Options {
		if strings.EqualFold(o.Key, value) {
			return o.Key
		}
	}
	return s.Options[0].Key
}

// pacer spaces requests to one indexer, shared by every Engine built under
// the same name so a config reload or a parallel search does not reset it.
type pacer struct {
	mu   sync.Mutex
	gap  time.Duration
	next time.Time
}

var pacers sync.Map // name → *pacer

func pacerFor(name string, gap time.Duration) *pacer {
	p, _ := pacers.LoadOrStore(name, &pacer{})
	pc := p.(*pacer)
	pc.mu.Lock()
	pc.gap = gap
	pc.mu.Unlock()
	return pc
}

func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	at := p.next
	if at.Before(now) {
		at = now
	}
	p.next = at.Add(p.gap)
	p.mu.Unlock()
	if d := at.Sub(now); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

// request is one HTTP request the engine sends.
type request struct {
	method   string
	url      string
	form     string
	headers  map[string]string
	referer  string
	redirect bool
}

type response struct {
	status   int
	location string
	header   http.Header
	body     string
	url      *url.URL
}

func (e *Engine) do(ctx context.Context, r request) (*response, error) {
	if err := e.pace.wait(ctx); err != nil {
		return nil, err
	}
	var body io.Reader
	if r.method == http.MethodPost {
		body = strings.NewReader(r.form)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, r.url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if r.method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	if r.referer != "" {
		req.Header.Set("Referer", r.referer)
	}
	client := *e.client
	if !r.redirect {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	text, err := e.decode(raw, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	// Links on the page resolve against the URL asked for, not wherever a
	// redirect landed, as upstream resolves them.
	return &response{
		status:   resp.StatusCode,
		location: resp.Header.Get("Location"),
		header:   resp.Header,
		body:     text,
		url:      req.URL,
	}, nil
}

// decode reads a body in the charset its Content-Type names, else the
// definition's encoding.
func (e *Engine) decode(raw []byte, contentType string) (string, error) {
	enc := e.encoding
	if _, params, err := mime.ParseMediaType(contentType); err == nil {
		if cs := params["charset"]; cs != "" {
			if named, err := htmlindex.Get(cs); err == nil {
				enc = named
			}
		}
	}
	if enc == nil || enc == unicode.UTF8 {
		return string(raw), nil
	}
	out, err := enc.NewDecoder().Bytes(raw)
	if err != nil {
		return "", fmt.Errorf("decode %s body: %w", e.def.Encoding, err)
	}
	return string(out), nil
}

// bytesIn encodes s in the definition's encoding.
func (e *Engine) bytesIn(s string) ([]byte, error) {
	if e.encoding == nil {
		return []byte(s), nil
	}
	return e.encoding.NewEncoder().Bytes([]byte(s))
}

// urlEncode is Prowlarr's UrlEncode(encoding): the value's bytes in the
// definition's encoding, escaped as WebUtility.UrlEncode escapes them.
func (e *Engine) urlEncode(s string) (string, error) {
	b, err := e.bytesIn(s)
	if err != nil {
		return "", err
	}
	return webURLEncode(b), nil
}

func (e *Engine) urlDecode(s string) (string, error) {
	var out bytes.Buffer
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '+':
			out.WriteByte(' ')
		case c == '%' && i+2 < len(s):
			n, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil {
				out.WriteByte(c)
				continue
			}
			out.WriteByte(byte(n))
			i += 2
		default:
			out.WriteByte(c)
		}
	}
	if e.encoding == nil {
		return out.String(), nil
	}
	b, err := e.encoding.NewDecoder().Bytes(out.Bytes())
	return string(b), err
}

// queryString is GetQueryString: keys as written, values encoded.
func (e *Engine) queryString(pairs [][2]string, sep string) (string, error) {
	if sep == "" {
		sep = "&"
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		v, err := e.urlEncode(p[1])
		if err != nil {
			return "", err
		}
		parts = append(parts, p[0]+"="+v)
	}
	return strings.Join(parts, sep), nil
}

// resolve is ResolvePath: a path relative to the site, or to base when given.
func (e *Engine) resolve(path string, base *url.URL) (*url.URL, error) {
	if base == nil {
		base = e.site
	}
	ref, err := url.Parse(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(ref), nil
}

// headers renders a header block's first value per name (ParseCustomHeaders).
func (e *Engine) headers(
	block cardigann.OrderedMap[cardigann.StrList],
	v *vars,
) (map[string]string, error) {
	out := make(map[string]string, len(block))
	for _, h := range block {
		if len(h.Value) == 0 {
			continue
		}
		val, err := render(h.Value[0], v, plainText)
		if err != nil {
			return nil, err
		}
		out[h.Key] = val
	}
	return out, nil
}

func (e *Engine) baseVars() *vars {
	return &vars{
		config: e.config,
		query:  map[string]string{},
		result: map[string]string{},
		year:   strconv.Itoa(e.now().Year()),
	}
}

func logDebug(
	ctx context.Context,
	def *cardigann.Definition,
	msg string,
	args ...any,
) {
	slog.DebugContext(ctx, msg, append([]any{"definition", def.ID}, args...)...)
}
