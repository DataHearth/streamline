package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// shotSpec is one README screenshot: a page at desktop size, optionally
// driven into the state it shows.
type shotSpec struct {
	name string
	path func(*demo) string
	// prep drives the page after it loads (opens a dialog, picks a tab).
	prep func(*rod.Page) error
}

func clickText(selector, text string) func(*rod.Page) error {
	return func(p *rod.Page) error {
		el, err := p.Timeout(30*time.Second).ElementR(selector, text)
		if err != nil {
			return fmt.Errorf("no %s %q: %w", selector, text, err)
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return err
		}
		time.Sleep(1500 * time.Millisecond)
		return nil
	}
}

func idPath(format string, pick func(*demo) int) func(*demo) string {
	return func(d *demo) string { return fmt.Sprintf(format, pick(d)) }
}

var shotList = []shotSpec{
	{name: "dashboard", path: fixed("/")},
	{name: "activity-queue", path: fixed("/activity")},
	{name: "history", path: fixed("/activity?view=history")},
	{
		name: "import-verification",
		path: fixed("/activity"),
		prep: clickText("button", `^\s*Resolve\s*$`),
	},
	{name: "movies", path: fixed("/movies")},
	{
		name: "movie-detail",
		path: idPath(
			"/movies/%d",
			func(d *demo) int { return d.ids.movies["Metropolis"] },
		),
	},
	{
		name: "manual-search",
		path: idPath(
			"/movies/%d",
			func(d *demo) int { return d.ids.movies[manualSearchMovie] },
		),
		prep: clickText("button", `^\s*Manual search\s*$`),
	},
	{name: "series", path: fixed("/series")},
	{
		name: "series-episodes",
		path: idPath(
			"/series/%d?tab=episodes",
			func(d *demo) int { return d.ids.series["The Andy Griffith Show"] },
		),
	},
	{
		name: "library-import",
		path: func(d *demo) string { return fmt.Sprintf("/imports/%d", d.scanID) },
	},
	{name: "music", path: fixed("/music")},
	{name: "music-artist", path: func(d *demo) string {
		return fmt.Sprintf(
			"/music/%d?tab=discography&release=%d",
			d.ids.artists["Josh Woodward"],
			d.ids.albums["Josh Woodward/Ashes"],
		)
	}},
	{name: "books", path: fixed("/books")},
	{
		name: "book-detail",
		path: idPath(
			"/books/%d",
			func(d *demo) int { return d.ids.books["The Adventures of Tom Sawyer"] },
		),
	},
	{
		name: "book-series",
		path: idPath(
			"/books/series/%d?tab=volumes",
			func(d *demo) int { return d.ids.bseries["Oz"] },
		),
	},
	{name: "requests", path: fixed("/requests")},
	{name: "calendar", path: fixed("/calendar")},
	{name: "settings", path: fixed("/settings/general")},
}

// mobileShots are the three phone screens composed into mobile.png.
var mobileShots = []func(*demo) string{
	fixed("/"),
	idPath("/books/%d", func(d *demo) int { return d.ids.books["Dracula"] }),
	fixed("/activity"),
}

func fixed(p string) func(*demo) string { return func(*demo) string { return p } }

func (d *demo) want(name string) bool { return d.only == nil || d.only[name] }

func (d *demo) shoot(ctx context.Context) error {
	if err := os.MkdirAll(d.out, 0o755); err != nil {
		return err
	}
	l := launcher.New().Headless(true).NoSandbox(os.Geteuid() == 0)
	if bin := chromeBin(); bin != "" {
		l = l.Bin(bin)
	}
	defer l.Kill()
	ctrl, err := l.Launch()
	if err != nil {
		return err
	}
	browser := rod.New().ControlURL(ctrl).Context(ctx)
	if err := browser.Connect(); err != nil {
		return err
	}
	defer browser.Close()

	if d.want("login") {
		page, err := d.newPage(browser, 1440, 900, 1, false)
		if err != nil {
			return err
		}
		if err := d.capture(
			page,
			"/login",
			nil,
			filepath.Join(d.out, "login.png"),
		); err != nil {
			return err
		}
		_ = page.Close()
	}

	page, err := d.newPage(browser, 1440, 900, 1, false)
	if err != nil {
		return err
	}
	if err := d.signIn(page); err != nil {
		return err
	}
	for _, s := range shotList {
		if !d.want(s.name) {
			continue
		}
		if err := d.capture(
			page,
			s.path(d),
			s.prep,
			filepath.Join(d.out, s.name+".png"),
		); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}

	if d.want("mobile") {
		phone, err := d.newPage(browser, 390, 844, 1.5, true)
		if err != nil {
			return err
		}
		if err := d.signIn(phone); err != nil {
			return err
		}
		var frames []image.Image
		for i, p := range mobileShots {
			file := filepath.Join(d.root, fmt.Sprintf("mobile-%d.png", i))
			if err := d.capture(phone, p(d), nil, file); err != nil {
				return fmt.Errorf("mobile %d: %w", i, err)
			}
			img, err := readPNG(file)
			if err != nil {
				return err
			}
			frames = append(frames, img)
		}
		if err := composeMobile(
			frames,
			filepath.Join(d.out, "mobile.png"),
		); err != nil {
			return err
		}
	}
	return nil
}

func (d *demo) newPage(
	b *rod.Browser,
	w, h int,
	scale float64,
	mobile bool,
) (*rod.Page, error) {
	inc, err := b.Incognito()
	if err != nil {
		return nil, err
	}
	page, err := inc.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, err
	}
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width: w, Height: h, DeviceScaleFactor: scale, Mobile: mobile,
	}); err != nil {
		return nil, err
	}
	if mobile {
		if err := (proto.EmulationSetTouchEmulationEnabled{Enabled: true, MaxTouchPoints: gson(5)}).Call(
			page,
		); err != nil {
			return nil, err
		}
	}
	return page, nil
}

func gson(n int) *int { return &n }

func (d *demo) signIn(page *rod.Page) error {
	if err := page.Navigate(d.base + "/login"); err != nil {
		return err
	}
	if err := page.WaitLoad(); err != nil {
		return err
	}
	res, err := page.Eval(`(e, p) => fetch("/auth/login", {
		method: "POST",
		headers: {"Content-Type": "application/json"},
		body: JSON.stringify({email: e, password: p}),
	}).then(r => r.status)`, adminEmail, adminPassword)
	if err != nil {
		return err
	}
	if s := res.Value.Int(); s != 204 && s != 200 {
		return fmt.Errorf("sign in answered %d", s)
	}
	return nil
}

// capture loads path, waits for its data and artwork, runs prep and writes
// the viewport to file.
func (d *demo) capture(
	page *rod.Page,
	path string,
	prep func(*rod.Page) error,
	file string,
) error {
	if err := page.Navigate(d.base + path); err != nil {
		return err
	}
	if err := page.WaitLoad(); err != nil {
		return err
	}
	settle(page)
	if prep != nil {
		if err := prep(page); err != nil {
			return err
		}
		settle(page)
	}
	b, err := page.Screenshot(
		false,
		&proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng},
	)
	if err != nil {
		return err
	}
	slog.Info("screenshot", "file", file, "path", path)
	return writeFile(file, b)
}

// settle waits for the page to stop changing and every image on it to
// finish loading, within a bound: a page that never settles is still shot.
func settle(page *rod.Page) {
	p := page.Timeout(20 * time.Second)
	_ = p.WaitStable(800 * time.Millisecond)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		res, err := page.Eval(
			`() => [...document.images].every(i => i.complete) && !document.querySelector('[aria-busy="true"]')`,
		)
		if err == nil && res.Value.Bool() {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)
}

func readPNG(file string) (image.Image, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(b))
}

// composeMobile lays the phone screens side by side on the app's own
// background, each with rounded corners.
func composeMobile(frames []image.Image, out string) error {
	const gap, radius = 40, 36
	w, h := 0, 0
	for _, f := range frames {
		w += f.Bounds().Dx()
		h = max(h, f.Bounds().Dy())
	}
	w += gap * (len(frames) + 1)
	h += 2 * gap
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(
		canvas,
		canvas.Bounds(),
		&image.Uniform{color.RGBA{0x0b, 0x0b, 0x10, 0xff}},
		image.Point{},
		draw.Src,
	)
	x := gap
	for _, f := range frames {
		r := image.Rect(x, gap, x+f.Bounds().Dx(), gap+f.Bounds().Dy())
		draw.DrawMask(
			canvas,
			r,
			f,
			f.Bounds().Min,
			roundedMask{f.Bounds().Dx(), f.Bounds().Dy(), radius},
			image.Point{},
			draw.Over,
		)
		x += f.Bounds().Dx() + gap
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return err
	}
	return writeFile(out, buf.Bytes())
}

// roundedMask is opaque inside a rectangle with rounded corners.
type roundedMask struct{ w, h, r int }

func (m roundedMask) ColorModel() color.Model { return color.AlphaModel }
func (m roundedMask) Bounds() image.Rectangle { return image.Rect(0, 0, m.w, m.h) }
func (m roundedMask) At(x, y int) color.Color {
	cx, cy := x, y
	switch {
	case x < m.r:
		cx = m.r
	case x >= m.w-m.r:
		cx = m.w - m.r - 1
	}
	switch {
	case y < m.r:
		cy = m.r
	case y >= m.h-m.r:
		cy = m.h - m.r - 1
	}
	dx, dy := x-cx, y-cy
	if dx*dx+dy*dy > m.r*m.r {
		return color.Alpha{}
	}
	return color.Alpha{A: 0xff}
}

// chromeBin is CHROME_PATH, else a browser on PATH, else a Chromium under
// PLAYWRIGHT_BROWSERS_PATH; empty lets rod download its own.
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
