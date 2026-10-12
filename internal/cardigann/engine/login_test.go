package engine

import (
	"context"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const loginDefinition = `---
id: fixture
name: Fixture
description: A tracker that logs in
language: en-US
type: private
encoding: UTF-8
links:
  - https://tracker.example/
caps:
  categorymappings:
    - {id: 1, cat: Movies, desc: Movies}
  modes:
    search: [q]
settings:
  - {name: username, type: text, label: Username}
  - {name: password, type: password, label: Password}
login:
  path: login.php
  method: form
  form: form#login
  inputs:
    username: "{{ .Config.username }}"
    password: "{{ .Config.password }}"
  selectorinputs:
    token:
      selector: "input[name=csrf]"
      attribute: value
  error:
    - selector: div.error
  test:
    path: browse.php
    selector: a[href="logout.php"]
search:
  paths:
    - path: browse.php
  rows:
    selector: tr.row
  fields:
    title:
      selector: a
    download:
      selector: a
      attribute: href
`

const captchaLogin = `  captcha:
    type: image
    selector: img#captcha
    input: captcha
`

const loginPage = `<html><body>
<form id="login" action="takelogin.php">
  <input name="csrf" value="abc"/>
  <input type="checkbox" name="remember" value="1"/>
  <input name="ghost" value="x" disabled/>
  <input name="username"/><input name="password"/>
  %CAPTCHA%
</form></body></html>`

var _ = Describe("a definition that logs in", Label("unit", "cardigann"), func() {
	var (
		trk         *tracker
		forms       []string
		withCaptcha bool
	)

	newEngine := func(src string, s Session) *Engine {
		GinkgoHelper()
		eng, err := New(convertFixture(src), Options{
			Name: "login-" + CurrentSpecReport().LeafNodeText,
			Settings: map[string]string{
				"username": "u", "password": "p",
			},
			Client:  trk.client(),
			Session: s,
		})
		Expect(err).NotTo(HaveOccurred())
		return eng
	}

	BeforeEach(func() {
		forms, withCaptcha = nil, false
		trk = newTracker(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/login.php":
				http.SetCookie(w, &http.Cookie{Name: "pre", Value: "1"})
				captcha := ""
				if withCaptcha {
					captcha = `<img id="captcha" src="captcha.png"/><input name="captcha"/>`
				}
				page := strings.Replace(loginPage, "%CAPTCHA%", captcha, 1)
				_, _ = w.Write([]byte(page))
			case "/captcha.png":
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte("PNGDATA"))
			case "/takelogin.php":
				Expect(r.ParseForm()).To(Succeed())
				forms = append(forms, r.PostForm.Encode())
				if c, err := r.Cookie("pre"); err != nil || c.Value != "1" {
					_, _ = w.Write(
						[]byte(`<div class="error">No landing cookie</div>`),
					)
					return
				}
				if r.PostForm.Get("password") != "p" {
					_, _ = w.Write([]byte(`<div class="error"> Bad password </div>`))
					return
				}
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "s1"})
				http.Redirect(w, r, "/browse.php", http.StatusFound)
			case "/browse.php":
				if c, err := r.Cookie("session"); err != nil || c.Value != "s1" {
					http.Redirect(w, r, "/login.php", http.StatusFound)
					return
				}
				_, _ = w.Write([]byte(`<a href="logout.php">out</a><table>
<tr class="row"><td><a href="/dl/1.torrent">Dune 2021</a></td></tr></table>`))
			}
		})
	})

	It("asks for a login, logs in through the form and searches", func() {
		ctx := context.Background()
		eng := newEngine(loginDefinition, Session{})
		_, err := eng.Search(ctx, Query{Term: "dune"})
		Expect(err).To(MatchError(ErrLoginRequired))

		Expect(eng.Login(ctx, nil, "")).To(Succeed())
		// The form's hidden field came through the selector input, its
		// unchecked checkbox and disabled field did not.
		Expect(forms).To(ConsistOf(
			"csrf=abc&password=p&token=abc&username=u",
		))
		Expect(eng.Session().Cookies).To(HaveKeyWithValue("session", "s1"))

		releases, err := eng.Search(ctx, Query{Term: "dune"})
		Expect(err).NotTo(HaveOccurred())
		Expect(releases).To(HaveLen(1))
	})

	It("carries a session into a new engine", func() {
		eng := newEngine(loginDefinition, Session{
			Cookies: map[string]string{"session": "s1"},
		})
		releases, err := eng.Search(context.Background(), Query{Term: "dune"})
		Expect(err).NotTo(HaveOccurred())
		Expect(releases).To(HaveLen(1))
	})

	It("reports the tracker's own error", func() {
		eng, err := New(convertFixture(loginDefinition), Options{
			Name:     "login-bad",
			Settings: map[string]string{"username": "u", "password": "wrong"},
			Client:   trk.client(),
		})
		Expect(err).NotTo(HaveOccurred())
		err = eng.Login(context.Background(), nil, "")
		Expect(err).To(MatchError(ErrLoginFailed))
		Expect(err).To(MatchError(ContainSubstring("Bad password")))
	})

	It("stops at a captcha, then logs in with a person's answer", func() {
		ctx := context.Background()
		withCaptcha = true
		src := loginDefinition[:strings.Index(loginDefinition, "  inputs:")] +
			captchaLogin + loginDefinition[strings.Index(loginDefinition, "  inputs:"):]
		Expect(newEngine(src, Session{}).Login(ctx, nil, "")).
			To(MatchError(ErrCaptchaRequired))

		landing, err := newEngine(src, Session{}).Landing(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(landing.Captcha.ContentType).To(Equal("image/png"))
		Expect(string(landing.Captcha.Data)).To(Equal("PNGDATA"))
		Expect(landing.Cookies).To(HaveKeyWithValue("pre", "1"))

		// Another engine, as another request would build: the landing page's
		// cookies come with it.
		eng := newEngine(src, Session{})
		Expect(eng.Login(ctx, landing, "XYZ")).To(Succeed())
		Expect(forms[len(forms)-1]).To(ContainSubstring("captcha=XYZ"))
		Expect(eng.Session().Cookies).To(HaveKeyWithValue("session", "s1"))
	})

	DescribeTable("the other login methods",
		func(block string, wantPath, wantQuery string, cookies map[string]string) {
			start := "\nlogin:"
			settings := map[string]string{"username": "u", "password": "p"}
			if strings.HasPrefix(block, "settings:") {
				start = "\nsettings:"
				settings["cookie"] = "a=1; b=2"
			}
			src := loginDefinition[:strings.Index(loginDefinition, start)+1] +
				block + loginDefinition[strings.Index(loginDefinition, "\nsearch:")+1:]
			eng, err := New(convertFixture(src), Options{
				Name:     "login-" + wantPath,
				Settings: settings,
				Client:   trk.client(),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(eng.Login(context.Background(), nil, "")).To(Succeed())
			if wantPath != "" {
				reqs := trk.Requests()
				Expect(reqs[len(reqs)-1].Path).To(Equal(wantPath))
				Expect(reqs[len(reqs)-1].RawQuery).To(Equal(wantQuery))
			}
			for k, v := range cookies {
				Expect(eng.Session().Cookies).To(HaveKeyWithValue(k, v))
			}
		},
		Entry("get sends the inputs as a query", `login:
  method: get
  path: auth.php
  inputs:
    user: "{{ .Config.username }}"
`, "/auth.php", "user=u", nil),
		Entry("oneurl appends its one input to the path", `login:
  method: oneurl
  path: rss/
  inputs:
    oneurl: "{{ .Config.password }}"
`, "/rss/p", "", nil),
		Entry("cookie takes the cookie setting", `settings:
  - {name: cookie, type: text, label: Cookie}
  - {name: username, type: text, label: Username}
  - {name: password, type: password, label: Password}
login:
  method: cookie
`, "", "", map[string]string{"a": "1", "b": "2"}),
	)
})
