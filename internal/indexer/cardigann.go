package indexer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/engine"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/otelx"
)

var (
	// ErrLoginFailed is a tracker refusing a built-in indexer's login. Its
	// text is the tracker's own message, which names no URL or secret.
	ErrLoginFailed = errors.New("tracker login failed")
	// ErrCaptchaRequired is a login only a person can finish: the tracker's
	// form shows an image captcha.
	ErrCaptchaRequired = errors.New(
		"the tracker's login form asks for a captcha; log in from the indexer's settings",
	)
	// ErrLoginBackoff is a login held back after recent failures, so a wrong
	// password is not retried into an account ban.
	ErrLoginBackoff = errors.New("tracker login failed recently")
	// ErrCaptchaExpired is a captcha answer whose page is no longer held.
	ErrCaptchaExpired = errors.New("the captcha expired; load a new one")
	// ErrChallenge is a tracker behind a browser challenge no solver got
	// past; the text says why, naming the setting when none is configured.
	ErrChallenge = errors.New("tracker challenge not solved")
	// ErrNotBuiltin is a login asked of an indexer streamline does not run
	// itself.
	ErrNotBuiltin = errors.New("not a built-in indexer")
)

// builtin finds a configured cardigann entry by name.
func builtin(name string) (config.IndexerEntry, error) {
	ix, ok := config.FindIndexer(name)
	switch {
	case !ok:
		return ix, config.ErrIndexerNotFound
	case ix.Protocol != "cardigann":
		return ix, ErrNotBuiltin
	}
	return ix, nil
}

// loginBackoff is Prowlarr's EscalationBackOff: how long the n-th consecutive
// login failure holds the next automatic attempt back.
var loginBackoff = []time.Duration{
	0, time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute,
	30 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	24 * time.Hour,
}

// loginLocks serialises one indexer's logins, so a search and the RSS tick
// needing one at once do not both submit the form.
var loginLocks sync.Map // name → *sync.Mutex

func loginLock(name string) *sync.Mutex {
	m, _ := loginLocks.LoadOrStore(name, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Cardigann runs a tracker from a Cardigann definition — the native indexer
// that stands in for a Prowlarr or Jackett entry. Its session lives in the
// store between calls; a draft has no store and keeps nothing.
type Cardigann struct {
	name    string
	engine  *engine.Engine
	store   db.Store
	session *sessionRow
}

type sessionRow struct {
	params db.IndexerSessionParams
}

func (s *sessionRow) live(now time.Time) bool {
	return s != nil && len(s.params.Cookies) > 0 &&
		(s.params.ExpiresAt == nil || s.params.ExpiresAt.After(now))
}

func newCardigann(
	ctx context.Context,
	ix config.IndexerEntry,
	store db.Store,
) (*Cardigann, error) {
	if ix.Cardigann == nil {
		return nil, fmt.Errorf("indexer %q has no cardigann block", ix.Name)
	}
	def, err := definitions.Load(
		config.Get().CardigannDir(),
		ix.Cardigann.Definition,
	)
	if err != nil {
		return nil, err
	}
	c := &Cardigann{name: ix.Name, store: store}
	var session engine.Session
	if store != nil {
		row, err := store.IndexerSession(ctx, ix.Name)
		if err != nil {
			return nil, err
		}
		if row != nil {
			c.session = &sessionRow{params: db.IndexerSessionParams{
				Cookies:       row.Cookies,
				UserAgent:     row.UserAgent,
				ExpiresAt:     row.ExpiresAt,
				LoginFailures: row.LoginFailures,
				RetryAt:       row.RetryAt,
				LastError:     row.LastError,
			}}
			if c.session.live(time.Now()) {
				session = engine.Session{
					Cookies:   row.Cookies,
					UserAgent: row.UserAgent,
				}
			}
		}
	}
	client := otelx.HTTPClient
	var proxy *url.URL
	if p := ix.Cardigann.Proxy; p != "" {
		if proxy, err = url.Parse(p); err != nil {
			return nil, fmt.Errorf("indexer %q: cardigann.proxy: %w", ix.Name, err)
		}
		client = otelx.ProxiedHTTPClient(proxy)
	}
	var solver *engine.Solver
	if cc := config.Get().Cardigann; cc.SolverURL != "" {
		timeout, err := time.ParseDuration(cc.SolverTimeout)
		if err != nil {
			return nil, fmt.Errorf("cardigann.solver_timeout: %w", err)
		}
		solver = &engine.Solver{
			URL:     cc.SolverURL,
			Timeout: timeout,
			Client:  otelx.HTTPClient,
			Proxy:   proxy,
		}
	}
	c.engine, err = engine.New(def, engine.Options{
		Name:     ix.Name,
		Site:     ix.Cardigann.Site,
		Settings: ix.Cardigann.Settings,
		Client:   client,
		Session:  session,
		Solver:   solver,
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// run is HttpIndexerBase.FetchIndexerResponse's login handling around one
// engine call: log in first when there is no live session, again when a
// response says the session lapsed, then try once more. The session is kept
// after every call that worked, its lifetime extended as Prowlarr's is.
func (c *Cardigann) run(ctx context.Context, call func() error) error {
	if c.engine.NeedsLogin() && !c.session.live(time.Now()) {
		if err := c.login(ctx, false, nil, ""); err != nil {
			return err
		}
	}
	err := call()
	if errors.Is(err, engine.ErrLoginRequired) {
		if err = c.login(ctx, false, nil, ""); err != nil {
			return err
		}
		if err = call(); errors.Is(err, engine.ErrLoginRequired) {
			return c.loginFailed(
				ctx,
				fmt.Errorf(
					"%w: still asked to log in after logging in",
					ErrLoginFailed,
				),
			)
		}
	}
	if err != nil {
		return cardigannError(err)
	}
	return c.keep(ctx)
}

// login runs the definition's login. An automatic one respects the backoff
// a failure left; manual, a person asked for it and it always goes ahead.
func (c *Cardigann) login(
	ctx context.Context,
	manual bool,
	landing *engine.Landing,
	captcha string,
) error {
	lock := loginLock(c.name)
	lock.Lock()
	defer lock.Unlock()
	if !manual && c.store != nil {
		row, err := c.store.IndexerSession(ctx, c.name)
		if err != nil {
			return err
		}
		if row != nil && row.RetryAt != nil && row.RetryAt.After(time.Now()) {
			return fmt.Errorf("%w; next attempt at %s: %s", ErrLoginBackoff,
				row.RetryAt.Format(time.RFC3339), row.LastError)
		}
	}
	err := c.engine.Login(ctx, landing, captcha)
	if err != nil {
		return c.loginFailed(ctx, cardigannError(err))
	}
	slog.InfoContext(ctx, "tracker login succeeded", "indexer.name", c.name)
	c.session = &sessionRow{}
	return c.keep(ctx)
}

// keep stores the engine's session as live for another SessionLifetime and
// clears any failure.
func (c *Cardigann) keep(ctx context.Context) error {
	s := c.engine.Session()
	if c.store == nil || (!c.engine.NeedsLogin() && len(s.Cookies) == 0 &&
		s.UserAgent == "") {
		return nil
	}
	exp := time.Now().Add(engine.SessionLifetime)
	p := db.IndexerSessionParams{
		Cookies: s.Cookies, UserAgent: s.UserAgent, ExpiresAt: &exp,
	}
	if c.session != nil && maps.Equal(c.session.params.Cookies, p.Cookies) &&
		c.session.params.UserAgent == p.UserAgent &&
		c.session.params.LoginFailures == 0 && c.session.params.ExpiresAt != nil &&
		c.session.params.ExpiresAt.After(exp.Add(-24*time.Hour)) {
		return nil
	}
	c.session = &sessionRow{params: p}
	return c.store.PutIndexerSession(ctx, c.name, p)
}

// loginFailed records a failed login and the backoff it earns, dropping the
// session it was meant to replace.
func (c *Cardigann) loginFailed(ctx context.Context, cause error) error {
	slog.WarnContext(ctx, "tracker login failed",
		"indexer.name", c.name, "error", cause)
	if c.store == nil {
		return cause
	}
	row, err := c.store.IndexerSession(ctx, c.name)
	if err != nil {
		return errors.Join(cause, err)
	}
	var failures uint8
	if row != nil {
		failures = row.LoginFailures
	}
	if failures < math.MaxUint8 {
		failures++
	}
	retry := time.Now().Add(loginBackoff[min(int(failures), len(loginBackoff)-1)])
	p := db.IndexerSessionParams{
		LoginFailures: failures,
		RetryAt:       &retry,
		LastError:     cause.Error(),
	}
	c.session = &sessionRow{params: p}
	if err := c.store.PutIndexerSession(ctx, c.name, p); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func cardigannKind(k MediaKind) (engine.Kind, []int) {
	switch k {
	case KindMovie:
		return engine.KindMovie, []int{2000}
	case KindTV:
		return engine.KindTV, []int{5000}
	case KindMusic:
		return engine.KindMusic, []int{3000}
	case KindEbook:
		return engine.KindBook, []int{7000, 7020}
	case KindAudiobook:
		return engine.KindBook, []int{3030}
	default:
		return engine.KindSearch, nil
	}
}

func (c *Cardigann) Search(
	ctx context.Context,
	params SearchParams,
) ([]SearchResult, error) {
	kind, cats := cardigannKind(params.Kind)
	var releases []engine.Release
	err := c.run(ctx, func() error {
		var err error
		releases, err = c.engine.Search(ctx, engine.Query{
			Kind:       kind,
			Term:       params.Query,
			Season:     int(params.Season),
			Episode:    int(params.Episode),
			IMDbID:     params.IMDbID,
			TMDBID:     int(params.TMDBID),
			TVDBID:     int(params.TVDBID),
			Categories: cats,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return mapReleases(releases), nil
}

// Feed is a search with no term, which the definitions answer with their
// latest releases — what Prowlarr's RSS for a Cardigann tracker is.
func (c *Cardigann) Feed(ctx context.Context) ([]SearchResult, error) {
	var releases []engine.Release
	err := c.run(ctx, func() error {
		var err error
		releases, err = c.engine.Search(ctx, engine.Query{})
		return err
	})
	if err != nil {
		return nil, err
	}
	return mapReleases(releases), nil
}

// TestConnection runs the feed query: a tracker that answers it in the shape
// its definition describes is reachable and readable, results or not.
func (c *Cardigann) TestConnection(ctx context.Context) error {
	_, err := c.Feed(ctx)
	return err
}

// cardigannError files an engine failure under the sentinel the handlers and
// metrics read; a transport failure has none of the engine's own.
func cardigannError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, ErrLoginFailed),
		errors.Is(err, ErrCaptchaRequired),
		errors.Is(err, ErrLoginBackoff):
		return err
	case errors.Is(err, engine.ErrCaptchaRequired):
		return ErrCaptchaRequired
	case errors.Is(err, engine.ErrChallenge):
		return fmt.Errorf("%w: %w", ErrChallenge, err)
	case errors.Is(err, engine.ErrLoginFailed):
		msg := strings.TrimPrefix(err.Error(), engine.ErrLoginFailed.Error())
		return fmt.Errorf("%w%s", ErrLoginFailed, msg)
	case errors.Is(err, engine.ErrStatus):
		return fmt.Errorf("%w: %w", ErrUnexpectedStatus, err)
	case errors.Is(err, engine.ErrParse):
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	default:
		var transport interface{ Timeout() bool }
		if errors.As(err, &transport) {
			return fmt.Errorf(
				"%w: %w", ErrUnreachable, otelx.RedactTransportError(err),
			)
		}
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
}

func mapReleases(releases []engine.Release) []SearchResult {
	out := make([]SearchResult, 0, len(releases))
	for _, r := range releases {
		link := r.Download
		if link == "" {
			link = r.Magnet
		}
		if r.Title == "" || link == "" {
			continue
		}
		out = append(out, SearchResult{
			Title:       r.Title,
			InfoURL:     r.Details,
			Download:    link,
			Size:        r.Size,
			Seeders:     clampU32(r.Seeders),
			Leechers:    clampU32(r.Leechers),
			Category:    releaseCategory(r.Category),
			PublishDate: r.Published,
			InfoHash:    infoHash(r.InfoHash, r.Magnet, link),
			TMDBID:      clampU32(r.TMDBID),
			TVDBID:      clampU32(r.TVDBID),
			IMDbID:      clampU32(r.IMDbID),
		})
	}
	return out
}

// releaseCategory picks the most specific newznab category a row mapped to,
// the subcategory being what tells an audiobook (3030) from music.
func releaseCategory(cats []int) string {
	if len(cats) == 0 {
		return ""
	}
	i := max(slices.IndexFunc(cats, func(c int) bool { return c%1000 != 0 }), 0)
	return strconv.Itoa(cats[i])
}

func clampU32(n int64) uint32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxUint32:
		return math.MaxUint32
	default:
		return uint32(n)
	}
}

// CardigannSite is where a cardigann entry's tracker lives — the host its
// release and download links point at.
func CardigannSite(ix config.IndexerEntry) (string, error) {
	if ix.Cardigann == nil {
		return "", fmt.Errorf("indexer %q has no cardigann block", ix.Name)
	}
	def, err := definitions.Load(
		config.Get().CardigannDir(),
		ix.Cardigann.Definition,
	)
	if err != nil {
		return "", err
	}
	u, err := engine.Site(def, ix.Cardigann.Site)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// DownloadTarget is what a release link resolved to: a magnet, or an
// http(s) URL fetched with Method and Header — the session's cookies and
// user agent among them, for a tracker that only serves logged-in clients.
type DownloadTarget struct {
	URL    string
	Method string
	Header http.Header
}

// ResolveDownload finds the enabled cardigann indexer whose site link points
// at and resolves it through that definition's download block. ok is false
// when no cardigann indexer owns the link, which is then fetched as it is.
func ResolveDownload(
	ctx context.Context,
	store db.Store,
	link string,
) (DownloadTarget, bool, error) {
	for _, ix := range config.EnabledIndexers() {
		if ix.Protocol != "cardigann" {
			continue
		}
		c, err := newCardigann(ctx, ix, store)
		if err != nil {
			return DownloadTarget{}, false, err
		}
		if !c.engine.Owns(link) {
			continue
		}
		var d engine.Download
		err = c.run(ctx, func() error {
			var err error
			d, err = c.engine.Resolve(ctx, link)
			return err
		})
		if err != nil {
			return DownloadTarget{}, true, err
		}
		h := d.Header.Clone()
		s := c.engine.Session()
		if len(s.Cookies) > 0 && !strings.HasPrefix(d.URL, "magnet:") {
			parts := make([]string, 0, len(s.Cookies))
			for _, k := range slices.Sorted(maps.Keys(s.Cookies)) {
				parts = append(parts, k+"="+s.Cookies[k])
			}
			h.Set("Cookie", strings.Join(parts, "; "))
		}
		if s.UserAgent != "" {
			h.Set("User-Agent", s.UserAgent)
		}
		return DownloadTarget{URL: d.URL, Method: d.Method, Header: h}, true, nil
	}
	return DownloadTarget{}, false, nil
}

// SessionState is what the settings page shows of a built-in indexer's
// login.
type SessionState struct {
	// LoginRequired says the definition logs in at all; Captcha, that its
	// form may ask a person to read an image.
	LoginRequired bool
	Captcha       bool
	LoggedIn      bool
	ExpiresAt     *time.Time
	RetryAt       *time.Time
	LastError     string
}

func (i *indexer) Session(ctx context.Context, name string) (SessionState, error) {
	ix, err := builtin(name)
	if err != nil {
		return SessionState{}, err
	}
	c, err := newCardigann(ctx, ix, i.store)
	if err != nil {
		return SessionState{}, err
	}
	st := SessionState{
		LoginRequired: c.engine.NeedsLogin(),
		Captcha:       c.engine.NeedsCaptcha(),
	}
	if c.session != nil {
		st.LoggedIn = c.session.live(time.Now())
		st.ExpiresAt = c.session.params.ExpiresAt
		st.RetryAt = c.session.params.RetryAt
		st.LastError = c.session.params.LastError
	}
	return st, nil
}

// captchas holds the login pages a person was shown a captcha from, until
// they answer it.
var captchas = struct {
	sync.Mutex
	pages map[string]heldLanding
}{pages: map[string]heldLanding{}}

type heldLanding struct {
	indexer string
	landing *engine.Landing
	expires time.Time
}

// captchaTTL bounds how long a captcha page is held; trackers expire theirs
// within minutes anyway.
const captchaTTL = 10 * time.Minute

// Captcha fetches a built-in indexer's login page and the captcha on it.
// token names the page for the Login that answers it; image is nil when
// the form shows none this time.
func (i *indexer) Captcha(
	ctx context.Context,
	name string,
) (string, *engine.Captcha, error) {
	ix, err := builtin(name)
	if err != nil {
		return "", nil, err
	}
	c, err := newCardigann(ctx, ix, i.store)
	if err != nil {
		return "", nil, err
	}
	landing, err := c.engine.Landing(ctx)
	if err != nil {
		return "", nil, cardigannError(err)
	}
	if landing == nil || landing.Captcha == nil {
		return "", nil, nil
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	captchas.Lock()
	defer captchas.Unlock()
	now := time.Now()
	maps.DeleteFunc(captchas.pages, func(_ string, h heldLanding) bool {
		return h.expires.Before(now)
	})
	captchas.pages[token] = heldLanding{
		indexer: name, landing: landing, expires: now.Add(captchaTTL),
	}
	return token, landing.Captcha, nil
}

// Login logs a built-in indexer in now, whatever backoff a failure left —
// a person asked. A captcha answer comes with the token its page was held
// under.
func (i *indexer) Login(ctx context.Context, name, token, answer string) error {
	ix, err := builtin(name)
	if err != nil {
		return err
	}
	c, err := newCardigann(ctx, ix, i.store)
	if err != nil {
		return err
	}
	var landing *engine.Landing
	if token != "" {
		captchas.Lock()
		h, ok := captchas.pages[token]
		delete(captchas.pages, token)
		captchas.Unlock()
		if !ok || h.indexer != name || h.expires.Before(time.Now()) {
			return ErrCaptchaExpired
		}
		landing = h.landing
	}
	return c.login(ctx, true, landing, answer)
}

// Forget drops a built-in indexer's session, as an edit to its credentials
// or its removal must.
func (i *indexer) Forget(ctx context.Context, name string) error {
	return i.store.DeleteIndexerSession(ctx, name)
}
