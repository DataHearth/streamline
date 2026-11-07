package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// shot is one README image.
type shot struct {
	name   string
	path   string // empty: skipped (its title is not in the library)
	width  int
	height int
	// before runs after the page settles: opening a dialog, mostly.
	before func(p *rod.Page) error
	// anonymous shots are taken signed out.
	anonymous bool
}

// clickVisible clicks the first match that is on screen: a page often holds a
// phone and a desktop copy of one control, and clicking the hidden one waits
// for it to show until the page times out.
func clickVisible(
	what string,
	find func() (rod.Elements, error),
	settleFor time.Duration,
) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		els, err := find()
		if err != nil {
			return err
		}
		for _, el := range els {
			if ok, verr := el.Visible(); verr == nil && ok {
				if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
					return err
				}
				time.Sleep(settleFor)
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no visible %s", what)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func clickText(selector, text string) func(p *rod.Page) error {
	return func(p *rod.Page) error {
		re := regexp.MustCompile(text)
		return clickVisible(
			fmt.Sprintf("%s %q", selector, text),
			func() (rod.Elements, error) {
				all, err := p.Elements(selector)
				if err != nil {
					return nil, err
				}
				var out rod.Elements
				for _, el := range all {
					if t, err := el.Text(); err == nil &&
						re.MatchString(strings.TrimSpace(t)) {
						out = append(out, el)
					}
				}
				return out, nil
			},
			4*time.Second,
		)
	}
}

func first[K comparable](m map[K]int, keys ...K) int {
	for _, k := range keys {
		if id, ok := m[k]; ok {
			return id
		}
	}
	return 0
}

func idPath(prefix string, id int, suffix string) string {
	if id == 0 {
		return ""
	}
	return prefix + strconv.Itoa(id) + suffix
}

func (s *seeder) shots() []shot {
	movie := first(s.movieIDs, "Big Buck Bunny", "Sintel", "Metropolis")
	search := first(s.movieIDs, wantedFilms[0].Title)
	show := first(s.showIDs, "The Cisco Kid", "The Lucy Show")
	artist := first(s.artistIDs, "Jonathan Coulton", "Brad Sucks")
	book := first(s.bookIDs, grabEbook.Title, "Pride and Prejudice")
	series := first(s.seriesIDs, "Sherlock Holmes", "Oz")
	const w, h = 1440, 900
	return []shot{
		{name: "dashboard", path: "/", width: w, height: h},
		{name: "activity-queue", path: "/activity", width: w, height: h},
		{name: "history", path: "/activity?view=history", width: w, height: h},
		{
			name:   "import-verification",
			path:   "/activity",
			width:  w,
			height: h,
			before: clickText("button", "^Resolve$"),
		},
		{name: "movies", path: "/movies", width: w, height: h},
		{
			name:   "movie-detail",
			path:   idPath("/movies/", movie, ""),
			width:  w,
			height: h,
		},
		{
			name:   "manual-search",
			path:   idPath("/movies/", search, ""),
			width:  w,
			height: h,
			before: clickText("button", "^Manual search$"),
		},
		{name: "series", path: "/series", width: w, height: h},
		{
			name:   "series-episodes",
			path:   idPath("/series/", show, "?tab=episodes"),
			width:  w,
			height: h,
		},
		{
			name:   "library-import",
			path:   idPath("/imports/", s.pendingImp, ""),
			width:  w,
			height: h,
		},
		{name: "music", path: "/music", width: w, height: h},
		{
			name:   "music-artist",
			path:   idPath("/music/", artist, "?tab=discography"),
			width:  w,
			height: h,
		},
		{name: "books", path: "/books", width: w, height: h},
		{
			name:   "book-detail",
			path:   idPath("/books/", book, ""),
			width:  w,
			height: h,
		},
		{
			name:   "book-series",
			path:   idPath("/books/series/", series, ""),
			width:  w,
			height: h,
		},
		{name: "requests", path: "/requests", width: w, height: h},
		{name: "calendar", path: "/calendar", width: w, height: h},
		{name: "settings", path: "/settings", width: w, height: h},
		{name: "login", path: "/login", width: w, height: h, anonymous: true},
	}
}

// phoneShots make up mobile.png, side by side.
func (s *seeder) phoneShots() []shot {
	book := first(s.bookIDs, grabEbook.Title, "Pride and Prejudice")
	return []shot{
		{name: "phone-dashboard", path: "/", width: 390, height: 780},
		{
			name:   "phone-book",
			path:   idPath("/books/", book, ""),
			width:  390,
			height: 780,
		},
		{name: "phone-activity", path: "/activity", width: 390, height: 780},
	}
}

func shoot(ctx context.Context, in *instance, s *seeder, o options) error {
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	l := launcher.New().Headless(true).NoSandbox(os.Geteuid() == 0)
	if bin := chromeBin(); bin != "" {
		l = l.Bin(bin)
	}
	defer l.Kill()
	u, err := l.Launch()
	if err != nil {
		return fmt.Errorf("launch chromium: %w", err)
	}
	browser := rod.New().ControlURL(u).Context(ctx)
	if err := browser.Connect(); err != nil {
		return err
	}
	defer browser.Close()

	want := func(name string) bool { return o.only == nil || o.only[name] }
	for _, sh := range s.shots() {
		if !want(sh.name) {
			continue
		}
		if sh.path == "" {
			slog.WarnContext(
				ctx,
				"shot skipped: its title is not in the library",
				"shot",
				sh.name,
			)
			continue
		}
		data, err := capture(browser, in, sh, o.scale)
		if err != nil {
			slog.WarnContext(ctx, "shot failed", "shot", sh.name, "error", err)
			continue
		}
		file := filepath.Join(o.out, sh.name+".png")
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return err
		}
		slog.InfoContext(ctx, "shot", "file", file)
	}
	if want("mobile") {
		var phones [][]byte
		for _, sh := range s.phoneShots() {
			if sh.path == "" {
				continue
			}
			data, err := capture(browser, in, sh, 2)
			if err != nil {
				slog.WarnContext(ctx, "shot failed", "shot", sh.name, "error", err)
				continue
			}
			phones = append(phones, data)
		}
		if len(phones) > 0 {
			data, err := composite(browser, phones, o.scale)
			if err != nil {
				return err
			}
			file := filepath.Join(o.out, "mobile.png")
			if err := os.WriteFile(file, data, 0o644); err != nil {
				return err
			}
			slog.InfoContext(ctx, "shot", "file", file)
		}
	}
	return nil
}

// capture opens one page in a fresh incognito context, signed in unless the
// shot says otherwise, and returns the viewport as PNG.
func capture(
	browser *rod.Browser,
	in *instance,
	sh shot,
	scale float64,
) ([]byte, error) {
	inc, err := browser.Incognito()
	if err != nil {
		return nil, err
	}
	defer inc.Close()
	p, err := inc.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, err
	}
	p = p.Timeout(90 * time.Second)
	mobile := sh.width < 768
	if err := p.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width: sh.width, Height: sh.height, DeviceScaleFactor: scale, Mobile: mobile,
	}); err != nil {
		return nil, err
	}
	if mobile {
		touch := 5
		_ = proto.EmulationSetTouchEmulationEnabled{
			Enabled:        true,
			MaxTouchPoints: &touch,
		}.Call(
			p,
		)
	}
	if !sh.anonymous {
		if err := p.Navigate(in.base + "/login"); err != nil {
			return nil, err
		}
		if err := p.WaitLoad(); err != nil {
			return nil, err
		}
		res, err := p.Eval(
			`(e, pw) => fetch("/auth/login", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({email: e, password: pw})}).then(r => r.status)`,
			adminEmail,
			demoPassword,
		)
		if err != nil {
			return nil, err
		}
		if st := res.Value.Int(); st != 204 {
			return nil, fmt.Errorf("login answered %d", st)
		}
	}
	if err := p.Navigate(in.base + sh.path); err != nil {
		return nil, err
	}
	if err := settle(p); err != nil {
		return nil, err
	}
	if sh.before != nil {
		if err := sh.before(p); err != nil {
			return nil, err
		}
		if err := settle(p); err != nil {
			return nil, err
		}
	}
	return p.Screenshot(
		false,
		&proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng},
	)
}

// settle waits for the network to go quiet, every image to finish loading
// and the fonts to be in, then a beat for transitions.
func settle(p *rod.Page) error {
	if err := p.WaitLoad(); err != nil {
		return err
	}
	wait := p.WaitRequestIdle(
		800*time.Millisecond,
		nil,
		[]string{"/schedules/events"},
		nil,
	)
	wait()
	_, err := p.Eval(`() => Promise.all([
		document.fonts.ready,
		...Array.from(document.images).filter(i => !i.complete).map(i => new Promise(r => { i.onload = i.onerror = r; setTimeout(r, 8000); })),
	])`)
	if err != nil {
		return err
	}
	time.Sleep(700 * time.Millisecond)
	return nil
}

// composite lays the phone shots side by side on the app's background.
func composite(
	browser *rod.Browser,
	phones [][]byte,
	scale float64,
) ([]byte, error) {
	var imgs strings.Builder
	for _, b := range phones {
		imgs.WriteString(
			`<img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(
				b,
			) + `">`,
		)
	}
	html := `<!doctype html><html><body style="margin:0;background:#0b0b10;display:flex;gap:30px;align-items:center;justify-content:center;width:1845px;height:1280px">
<style>img{width:555px;border-radius:28px;box-shadow:0 24px 64px rgb(0 0 0/.55);outline:1px solid rgb(255 255 255/.08)}</style>` + imgs.String() + `</body></html>`
	p, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, err
	}
	defer p.Close()
	if err := p.SetViewport(
		&proto.EmulationSetDeviceMetricsOverride{
			Width:             1845,
			Height:            1280,
			DeviceScaleFactor: scale,
		},
	); err != nil {
		return nil, err
	}
	if err := p.SetDocumentContent(html); err != nil {
		return nil, err
	}
	if _, err := p.Eval(
		`() => Promise.all(Array.from(document.images).map(i => i.decode()))`,
	); err != nil {
		return nil, err
	}
	return p.Screenshot(
		false,
		&proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng},
	)
}

// chromeBin is CHROME_PATH, else a browser on PATH, else a Chromium under
// PLAYWRIGHT_BROWSERS_PATH; empty lets rod download its own. Same lookup as
// .claude/skills/verify/shot.
func chromeBin() string {
	if bin := os.Getenv("CHROME_PATH"); bin != "" {
		return bin
	}
	if bin, ok := launcher.LookPath(); ok {
		return bin
	}
	if dir := os.Getenv("PLAYWRIGHT_BROWSERS_PATH"); dir != "" {
		for _, pattern := range []string{
			"chromium*/chrome-linux*/chrome",
			"chromium*/chrome-mac*/Chromium.app/Contents/MacOS/Chromium",
			"chromium*/chrome-win*/chrome.exe",
		} {
			if m, _ := filepath.Glob(filepath.Join(dir, pattern)); len(m) > 0 {
				return m[0]
			}
		}
	}
	return ""
}
