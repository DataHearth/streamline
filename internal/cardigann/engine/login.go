package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/datahearth/streamline/internal/cardigann"
)

var (
	// ErrLoginRequired is a response that says the session is gone: a
	// redirect, an HTTP error, or a page without the definition's logged-in
	// marker (CheckIfLoginIsNeeded). The caller logs in and asks again.
	ErrLoginRequired = errors.New("the tracker wants a login")
	// ErrLoginFailed is a login the tracker refused, with its own message
	// when the definition says where to read it. Its text never carries a
	// URL, which a oneurl login keeps its secret in, so it can be shown.
	ErrLoginFailed = errors.New("login failed")
	// ErrCaptchaRequired is a form login whose page shows an image captcha:
	// a person has to read it (Landing, then Login with the answer).
	ErrCaptchaRequired = errors.New("the login form asks for a captcha")
)

// Landing is a form login's page, kept between fetching it and submitting
// it: the captcha shown on it is only good for the session that fetched it.
type Landing struct {
	URL     string
	Body    string
	Cookies map[string]string
	// Captcha is the image the form asks to be read; nil when it shows none.
	Captcha *Captcha
}

type Captcha struct {
	ContentType string
	Data        []byte
}

// needsLogin is CheckIfLoginIsNeeded: a definition that logs in reads a
// redirect, an HTTP error, or an HTML page missing its test selector as a
// lapsed session.
func (e *Engine) needsLogin(resp *response) bool {
	if e.def.Login == nil {
		return false
	}
	if resp.status >= 300 && resp.status < 400 || resp.status >= 400 {
		return true
	}
	t := e.def.Login.Test
	if t == nil || t.Selector == "" {
		return false
	}
	if ct := resp.header.Get("Content-Type"); ct != "" &&
		!strings.Contains(ct, "text/html") {
		return false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.body))
	if err != nil {
		return true
	}
	return doc.Find(t.Selector).Length() == 0
}

// NeedsLogin reports whether the definition logs in at all.
func (e *Engine) NeedsLogin() bool { return e.def.Login != nil }

// NeedsCaptcha reports whether the definition's login form may show an image
// captcha, which only a person can answer.
func (e *Engine) NeedsCaptcha() bool {
	return e.def.Login != nil && e.def.Login.Captcha != nil
}

func (e *Engine) loginURL(v *vars) (string, error) {
	path, err := render(e.def.Login.Path, v, plainText)
	if err != nil {
		return "", err
	}
	u, err := e.resolve(path, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// definitionCookies are login.cookies, the "k=v" pairs a definition always
// starts a login with.
func (e *Engine) definitionCookies() map[string]string {
	return cookieHeader(strings.Join(e.def.Login.Cookies, "; "))
}

// cookieHeader is CookieUtil.CookieHeaderToDictionary.
func cookieHeader(h string) map[string]string {
	out := map[string]string{}
	for part := range strings.SplitSeq(h, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && name != "" {
			out[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	return out
}

// Landing fetches a form login's page (GetConfigurationForSetup) and the
// captcha image on it, if any. It is nil for a definition that logs in some
// other way.
func (e *Engine) Landing(ctx context.Context) (*Landing, error) {
	login := e.def.Login
	if login == nil || login.Method != "form" {
		return nil, nil
	}
	v := e.baseVars()
	headers, err := e.headers(login.Headers, v)
	if err != nil {
		return nil, err
	}
	loginURL, err := e.loginURL(v)
	if err != nil {
		return nil, err
	}
	e.resetCookies(e.definitionCookies())
	resp, err := e.do(ctx, request{
		method:   http.MethodGet,
		url:      loginURL,
		headers:  headers,
		referer:  e.site.String(),
		redirect: e.def.FollowRedirect,
	})
	if err != nil {
		return nil, err
	}
	l := &Landing{URL: loginURL, Body: resp.body}
	if c := login.Captcha; c != nil {
		if c.Type != "image" {
			return nil, fmt.Errorf(
				"%w: captcha type %q is not supported",
				ErrLoginFailed,
				c.Type,
			)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.body))
		if err != nil {
			return nil, err
		}
		if img := doc.Find(c.Selector).First(); img.Length() > 0 {
			src, _ := img.Attr("src")
			target, err := e.resolve(src, resp.url)
			if err != nil {
				return nil, err
			}
			ir, err := e.do(ctx, request{
				method:  http.MethodGet,
				url:     target.String(),
				headers: headers,
				referer: loginURL,
			})
			if err != nil {
				return nil, err
			}
			l.Captcha = &Captcha{
				ContentType: ir.header.Get("Content-Type"),
				Data:        ir.raw,
			}
		}
	}
	l.Cookies = e.Session().Cookies
	return l, nil
}

// Login runs the definition's login (DoLogin). A form login submits landing
// when given — the page a person read the captcha off — and fetches its own
// otherwise, refusing with ErrCaptchaRequired when that page shows one.
// The session it leaves is Session's to report.
func (e *Engine) Login(ctx context.Context, landing *Landing, captcha string) error {
	login := e.def.Login
	if login == nil {
		return nil
	}
	v := e.baseVars()
	headers, err := e.headers(login.Headers, v)
	if err != nil {
		return err
	}
	inputs := func() ([][2]string, error) {
		var pairs [][2]string
		for _, in := range login.Inputs {
			val, err := render(string(in.Value), v, plainText)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, [2]string{in.Key, val})
		}
		return pairs, nil
	}

	var resp *response
	switch login.Method {
	case "post":
		pairs, err := inputs()
		if err != nil {
			return err
		}
		loginURL, err := e.loginURL(v)
		if err != nil {
			return err
		}
		form, err := e.queryString(pairs, "")
		if err != nil {
			return err
		}
		e.resetCookies(e.definitionCookies())
		resp, err = e.do(ctx, request{
			method:   http.MethodPost,
			url:      loginURL,
			form:     form,
			headers:  headers,
			referer:  e.site.String(),
			redirect: true,
		})
		if err != nil {
			return err
		}
	case "form":
		if landing == nil {
			if landing, err = e.Landing(ctx); err != nil {
				return err
			}
			if landing.Captcha != nil {
				return ErrCaptchaRequired
			}
		}
		if resp, err = e.submitForm(ctx, landing, captcha, headers, v); err != nil {
			return err
		}
	case "cookie":
		e.resetCookies(cookieHeader(e.config["cookie"]))
		return nil
	case "get", "oneurl":
		path, err := render(login.Path, v, plainText)
		if err != nil {
			return err
		}
		if login.Method == "get" {
			pairs, err := inputs()
			if err != nil {
				return err
			}
			qs, err := e.queryString(pairs, "")
			if err != nil {
				return err
			}
			path += "?" + qs
		} else {
			one, _ := login.Inputs.Get("oneurl")
			s, err := render(string(one), v, plainText)
			if err != nil {
				return err
			}
			path += s
		}
		target, err := e.resolve(path, nil)
		if err != nil {
			return err
		}
		e.resetCookies(nil)
		resp, err = e.do(ctx, request{
			method:  http.MethodGet,
			url:     target.String(),
			headers: headers,
			referer: e.site.String(),
		})
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf(
			"%w: login method %q is not supported",
			ErrLoginFailed,
			login.Method,
		)
	}
	return e.loginError(resp, login.Error, v)
}

// submitForm fills the landing page's form and posts it.
func (e *Engine) submitForm(
	ctx context.Context,
	landing *Landing,
	captcha string,
	headers map[string]string,
	v *vars,
) (*response, error) {
	login := e.def.Login
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(landing.Body))
	if err != nil {
		return nil, err
	}
	formSel := login.Form
	if formSel == "" {
		formSel = "form"
	}
	form := doc.Find(formSel).First()
	if form.Length() == 0 {
		return nil, fmt.Errorf(
			"%w: no form %q on the login page",
			ErrLoginFailed,
			formSel,
		)
	}
	submit, _ := form.Attr("action")
	if login.SubmitPath != "" {
		submit = login.SubmitPath
	}

	var pairs ordered
	form.Find("input").Each(func(_ int, in *goquery.Selection) {
		name, ok := in.Attr("name")
		if !ok {
			return
		}
		if _, disabled := in.Attr("disabled"); disabled {
			return
		}
		typ := strings.ToLower(in.AttrOr("type", ""))
		if _, checked := in.Attr("checked"); (typ == "checkbox" || typ == "radio") &&
			!checked {
			return
		}
		pairs.set(name, in.AttrOr("value", ""))
	})
	inputName := func(key string) (string, error) {
		if !login.Selectors {
			return key, nil
		}
		name, ok := doc.Find(key).First().Attr("name")
		if !ok {
			return "", fmt.Errorf("%w: no input %q", ErrLoginFailed, key)
		}
		return name, nil
	}
	for _, in := range login.Inputs {
		val, err := render(string(in.Value), v, plainText)
		if err != nil {
			return nil, err
		}
		name, err := inputName(in.Key)
		if err != nil {
			return nil, err
		}
		pairs.set(name, val)
	}
	root := doc.Selection.Children().First()
	for _, in := range login.SelectorInputs {
		val, found, err := e.htmlSelector(in.Value, root, v, !in.Value.Optional)
		if err != nil {
			return nil, fmt.Errorf("selector input %s: %w", in.Key, err)
		}
		if found {
			pairs.set(in.Key, val)
		}
	}
	var query [][2]string
	for _, in := range login.GetSelectorInputs {
		val, found, err := e.htmlSelector(in.Value, root, v, !in.Value.Optional)
		if err != nil {
			return nil, fmt.Errorf("get selector input %s: %w", in.Key, err)
		}
		if found {
			query = append(query, [2]string{in.Key, val})
		}
	}
	if len(query) > 0 {
		qs, err := e.queryString(query, "")
		if err != nil {
			return nil, err
		}
		submit += "?" + qs
	}
	landingURL, err := e.resolve(landing.URL, nil)
	if err != nil {
		return nil, err
	}
	target, err := e.resolve(submit, landingURL)
	if err != nil {
		return nil, err
	}
	e.resetCookies(landing.Cookies)

	if doc.Find(`script[src*="simpleCaptcha"]`).Length() > 0 {
		hash, err := e.simpleCaptcha(ctx, headers, landing.URL)
		if err != nil {
			return nil, err
		}
		pairs.set("captchaSelection", hash)
		pairs.set("submitme", "X")
	}
	if c := login.Captcha; c != nil && strings.TrimSpace(captcha) != "" {
		name, err := inputName(c.Input)
		if err != nil {
			return nil, err
		}
		pairs.set(name, captcha)
	}

	r := request{
		method:   http.MethodPost,
		url:      target.String(),
		headers:  headers,
		referer:  landing.URL,
		redirect: true,
	}
	if form.AttrOr("enctype", "") == "multipart/form-data" {
		boundary := "---------------------------" +
			strconv.FormatInt(e.now().UnixNano(), 10)
		var b strings.Builder
		for _, p := range pairs {
			fmt.Fprintf(&b,
				"--%s\r\nContent-Disposition: form-data; name=\"%s\"\r\n\r\n%s\r\n",
				boundary, p[0], p[1])
		}
		b.WriteString("--" + boundary + "--")
		r.form = b.String()
		r.contentType = "multipart/form-data; boundary=" + boundary
		r.referer = e.site.String()
	} else {
		if r.form, err = e.queryString(pairs, ""); err != nil {
			return nil, err
		}
	}
	return e.do(ctx, r)
}

// simpleCaptcha answers the "click the image" captcha some trackers embed,
// which needs no person: its endpoint names the image to pick.
func (e *Engine) simpleCaptcha(
	ctx context.Context,
	headers map[string]string,
	referer string,
) (string, error) {
	u, err := e.resolve("simpleCaptcha.php?numImages=1", nil)
	if err != nil {
		return "", err
	}
	resp, err := e.do(ctx, request{
		method: http.MethodGet, url: u.String(), headers: headers, referer: referer,
	})
	if err != nil {
		return "", err
	}
	var sc struct {
		Images []struct {
			Hash string `json:"hash"`
		} `json:"images"`
	}
	if err := json.Unmarshal(resp.raw, &sc); err != nil || len(sc.Images) == 0 {
		return "", fmt.Errorf("%w: unreadable simpleCaptcha answer", ErrLoginFailed)
	}
	return sc.Images[0].Hash, nil
}

// loginError is CheckForError: a 401, or the first error block whose
// selector matches the page, fails the login with the tracker's message.
func (e *Engine) loginError(
	resp *response,
	blocks []cardigann.ErrorBlock,
	v *vars,
) error {
	if resp.status == http.StatusUnauthorized {
		return fmt.Errorf("%w: status 401", ErrLoginFailed)
	}
	if len(blocks) == 0 {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.body))
	if err != nil {
		return nil //nolint:nilerr // an unparsable page shows no error block
	}
	for _, b := range blocks {
		sel := doc.Find(b.Selector).First()
		if sel.Length() == 0 {
			continue
		}
		msg := sel.Text()
		if b.Message != nil {
			m, _, err := e.htmlSelector(
				*b.Message, doc.Selection.Children().First(), v, false,
			)
			if err == nil {
				msg = m
			}
		}
		return fmt.Errorf("%w: %s", ErrLoginFailed, strings.TrimSpace(msg))
	}
	return nil
}

// ordered is a form's fields in first-set order, as a .NET Dictionary keeps
// them when nothing is removed.
type ordered [][2]string

func (o *ordered) set(k, v string) {
	for i := range *o {
		if (*o)[i][0] == k {
			(*o)[i][1] = v
			return
		}
	}
	*o = append(*o, [2]string{k, v})
}

// SessionLifetime is how long Prowlarr trusts a login's cookies before it
// logs in again unasked.
const SessionLifetime = 30 * 24 * time.Hour
