// Command icons renders every app icon in web/static/images from logo.svg,
// the logo as one path on the 512 px canvas the icons share.
//
//	task generate:icons
//
// The outputs are committed. Edit logo.svg or a constant below, run the
// task, and commit what it writes; never edit an icon by hand.
// design-system/MASTER.md §8 describes each icon.
package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

// The two colours are the design tokens, copied by hand: --accent and
// --bg-deep in web/static/css/input.css. bg-deep is copied twice more, as
// background_color and theme_color in web/static/manifest.json and as the
// theme-color meta tag in web/app/index.html. A change to either token
// changes it here too, then `task generate:icons`.
var (
	accent = color.RGBA{0x7c, 0x8c, 0xff, 0xff} // --accent
	bgDeep = color.RGBA{0x0b, 0x0b, 0x10, 0xff} // --bg-deep
	white  = color.RGBA{0xff, 0xff, 0xff, 0xff}
)

const (
	// canvas is the side of the square logo.svg is drawn on. Every icon is
	// that canvas scaled, except the two favicons, which crop it.
	canvas = 512.0

	// The rounded tile of favicon-512.png: a browser tab and an <img> apply no
	// mask, so this icon brings its own shape.
	tileInset  = 12.0
	tileRadius = 59.0

	// faviconMargin is the room left around the logo when the favicons crop
	// the canvas, in canvas units.
	faviconMargin = 4.0

	// flatStep is the longest chord, in canvas units, a curve or an arc is cut
	// into: under a tenth of a pixel on the largest icon.
	flatStep = 0.1
)

func main() {
	src := flag.String("logo", "web/icons/logo.svg", "the logo source")
	out := flag.String("out", "web/static/images", "where the icons are written")
	flag.Parse()

	d, err := readLogo(*src)
	if err != nil {
		log.Fatal(err)
	}
	logo, err := flattenPath(d)
	if err != nil {
		log.Fatalf("%s: %v", *src, err)
	}
	crop := faviconCrop(logo)
	tile := roundedSquare(tileInset, canvas-2*tileInset, tileRadius)
	full := transform{k: 1}

	pngs := []struct {
		name   string
		px     int
		t      transform
		layers []layer
	}{
		{"favicon-512.png", 512, full, []layer{{tile, bgDeep}, {logo, accent}}},
		{"icon-maskable-512.png", 512, full, []layer{{nil, bgDeep}, {logo, accent}}},
		{
			"apple-touch-icon.png", 180,
			transform{k: 180 / canvas},
			[]layer{{nil, bgDeep}, {logo, accent}},
		},
		{"icon-monochrome-512.png", 512, full, []layer{{logo, white}}},
		{"favicon-48.png", 48, crop.scaleTo(48), []layer{{logo, accent}}},
	}
	for _, p := range pngs {
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, render(p.px, p.t, p.layers)); err != nil {
			log.Fatalf("%s: %v", p.name, err)
		}
		write(filepath.Join(*out, p.name), buf.Bytes())
	}
	write(filepath.Join(*out, "favicon.svg"), faviconSVG(d, crop))
}

func write(path string, data []byte) {
	// Committed assets, read by everyone who checks the repository out.
	err := os.WriteFile(path, data, 0o644) //nolint:gosec // see above
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", path)
}

// readLogo returns the path data of logo.svg, which must be exactly one path
// on the canvas.
func readLogo(path string) (string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // a path the task passes
	if err != nil {
		return "", err
	}
	var doc struct {
		ViewBox string `xml:"viewBox,attr"`
		Paths   []struct {
			D string `xml:"d,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if want := fmt.Sprintf("0 0 %g %g", canvas, canvas); doc.ViewBox != want {
		return "", fmt.Errorf("%s: viewBox is %q, want %q", path, doc.ViewBox, want)
	}
	if len(doc.Paths) != 1 {
		return "", fmt.Errorf("%s: %d paths, want exactly one", path, len(doc.Paths))
	}
	return strings.TrimSpace(doc.Paths[0].D), nil
}

// Rendering.

type point struct{ x, y float64 }

// transform maps canvas units to pixels: scale, then translate.
type transform struct{ k, dx, dy float64 }

// layer is a shape filled with one colour; a nil shape covers the whole icon.
type layer struct {
	shape [][]point
	color color.RGBA
}

// render paints the layers bottom to top onto a transparent square of side
// px. An icon whose bottom layer covers it whole is opaque, which png
// encodes without an alpha channel.
func render(px int, t transform, layers []layer) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	for _, l := range layers {
		src := image.NewUniform(l.color)
		if l.shape == nil {
			draw.Draw(img, img.Bounds(), src, image.Point{}, draw.Over)
			continue
		}
		r := vector.NewRasterizer(px, px)
		for _, poly := range l.shape {
			at := func(p point) (float32, float32) {
				return float32(p.x*t.k + t.dx), float32(p.y*t.k + t.dy)
			}
			r.MoveTo(at(poly[0]))
			for _, p := range poly[1:] {
				r.LineTo(at(p))
			}
			r.ClosePath()
		}
		r.Draw(img, img.Bounds(), src, image.Point{})
	}
	return img
}

// roundedSquare is a square of the given side, inset from the canvas
// origin, with rounded corners.
func roundedSquare(inset, side, r float64) [][]point {
	var poly []point
	lo, hi := inset+r, inset+side-r
	corners := []struct{ cx, cy, from float64 }{
		{
			hi,
			lo,
			-math.Pi / 2,
		},
		{hi, hi, 0},
		{lo, hi, math.Pi / 2},
		{lo, lo, math.Pi},
	}
	n := max(int(math.Ceil(math.Pi/2*r/flatStep)), 1)
	for _, c := range corners {
		for i := 0; i <= n; i++ {
			a := c.from + math.Pi/2*float64(i)/float64(n)
			poly = append(poly, point{c.cx + r*math.Cos(a), c.cy + r*math.Sin(a)})
		}
	}
	return [][]point{poly}
}

// Favicons.

// square is a square region of the canvas.
type square struct{ x, y, side float64 }

// faviconCrop is the smallest square centred on the canvas centre that holds
// the logo and its margin. Centring on the canvas, not on the logo, keeps the
// optical offset the canvas gives it (MASTER.md §8).
func faviconCrop(logo [][]point) square {
	c := canvas / 2
	half := 0.0
	for _, poly := range logo {
		for _, p := range poly {
			half = math.Max(half, math.Max(math.Abs(p.x-c), math.Abs(p.y-c)))
		}
	}
	half = math.Ceil(half + faviconMargin)
	return square{c - half, c - half, 2 * half}
}

func (s square) scaleTo(px float64) transform {
	k := px / s.side
	return transform{k, -s.x * k, -s.y * k}
}

// faviconSVG is logo.svg's own path under a viewBox cropped to the logo, in
// the accent colour.
func faviconSVG(d string, crop square) []byte {
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return fmt.Appendf(nil,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%s %s %s %s">`+
			`<path fill="#%02x%02x%02x" d="%s"/></svg>`+"\n",
		num(crop.x), num(crop.y), num(crop.side), num(crop.side),
		accent.R, accent.G, accent.B, d)
}

// Path data.

// flattenPath turns SVG path data into closed polygons, one per subpath. It
// reads every command SVGO writes for a filled outline (M L H V C S A Z, each
// absolute or relative, with implicit repeats) and refuses the rest.
func flattenPath(d string) ([][]point, error) {
	s := &scanner{src: d}
	var (
		polys      [][]point
		cur        []point
		pos, start point
		lastCtrl   point // second control point of the previous C or S
		prevCubic  bool
		cmd        byte
	)
	closeSub := func() {
		if len(cur) > 2 {
			polys = append(polys, cur)
		}
		cur = nil
	}
	for {
		s.skipSpace()
		if s.done() {
			break
		}
		if c := s.peek(); isCommand(c) {
			cmd = c
			s.i++
		} else if cmd == 0 {
			return nil, fmt.Errorf("path data starts with %q, not a command", c)
		}
		rel := cmd >= 'a'
		at := func(x, y float64) point {
			if rel {
				return point{pos.x + x, pos.y + y}
			}
			return point{x, y}
		}
		cubic := false
		switch cmd {
		case 'M', 'm':
			x, y, err := s.pair()
			if err != nil {
				return nil, err
			}
			closeSub()
			pos = at(x, y)
			start = pos
			cur = []point{pos}
			// Coordinates after a moveto are implicit linetos.
			cmd = map[byte]byte{'M': 'L', 'm': 'l'}[cmd]
		case 'L', 'l':
			x, y, err := s.pair()
			if err != nil {
				return nil, err
			}
			pos = at(x, y)
			cur = append(cur, pos)
		case 'H', 'h', 'V', 'v':
			v, err := s.number()
			if err != nil {
				return nil, err
			}
			horizontal := cmd == 'H' || cmd == 'h'
			switch {
			case horizontal && rel:
				pos.x += v
			case horizontal:
				pos.x = v
			case rel:
				pos.y += v
			default:
				pos.y = v
			}
			cur = append(cur, pos)
		case 'C', 'c', 'S', 's':
			// S's first control point mirrors the previous curve's second.
			c1 := pos
			if cmd == 'C' || cmd == 'c' {
				x, y, err := s.pair()
				if err != nil {
					return nil, err
				}
				c1 = at(x, y)
			} else if prevCubic {
				c1 = point{2*pos.x - lastCtrl.x, 2*pos.y - lastCtrl.y}
			}
			x2, y2, err := s.pair()
			if err != nil {
				return nil, err
			}
			x, y, err := s.pair()
			if err != nil {
				return nil, err
			}
			c2, end := at(x2, y2), at(x, y)
			cur = appendCubic(cur, pos, c1, c2, end)
			pos, lastCtrl, cubic = end, c2, true
		case 'A', 'a':
			var a [5]float64 // rx, ry, rotation, then the endpoint
			var flags [2]bool
			var err error
			for i := range 3 {
				if a[i], err = s.number(); err != nil {
					return nil, err
				}
			}
			for i := range flags {
				if flags[i], err = s.flag(); err != nil {
					return nil, err
				}
			}
			if a[3], a[4], err = s.pair(); err != nil {
				return nil, err
			}
			end := at(a[3], a[4])
			cur = appendArc(cur, pos, end, a[0], a[1], a[2], flags[0], flags[1])
			pos = end
		case 'Z', 'z':
			closeSub()
			pos = start
			cur = []point{pos}
		default:
			return nil, fmt.Errorf("path command %q is not supported", cmd)
		}
		prevCubic = cubic
	}
	closeSub()
	if len(polys) == 0 {
		return nil, errors.New("path data draws nothing")
	}
	return polys, nil
}

func isCommand(c byte) bool {
	return strings.IndexByte("MLHVCSQTAZmlhvcsqtaz", c) >= 0
}

func appendCubic(out []point, p0, p1, p2, p3 point) []point {
	// The control polygon is never shorter than the curve.
	length := dist(p0, p1) + dist(p1, p2) + dist(p2, p3)
	n := max(int(math.Ceil(length/flatStep)), 1)
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		out = append(out, point{
			a*p0.x + b*p1.x + c*p2.x + d*p3.x,
			a*p0.y + b*p1.y + c*p2.y + d*p3.y,
		})
	}
	return out
}

// appendArc follows the SVG spec's endpoint-to-centre conversion
// (SVG 1.1, appendix F.6.5 and F.6.6).
func appendArc(
	out []point, p0, p1 point, rx, ry, rotDeg float64, large, sweep bool,
) []point {
	if p0 == p1 {
		return out
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return append(out, p1)
	}
	phi := rotDeg * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	dx, dy := (p0.x-p1.x)/2, (p0.y-p1.y)/2
	x1 := cos*dx + sin*dy
	y1 := -sin*dx + cos*dy
	// Radii too small to reach the endpoint are scaled up until they do.
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		rx *= math.Sqrt(l)
		ry *= math.Sqrt(l)
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	k := math.Sqrt(math.Max(num, 0) / den)
	if large == sweep {
		k = -k
	}
	cx1, cy1 := k*rx*y1/ry, -k*ry*x1/rx
	cx := cos*cx1 - sin*cy1 + (p0.x+p1.x)/2
	cy := sin*cx1 + cos*cy1 + (p0.y+p1.y)/2
	angle := func(ux, uy, vx, vy float64) float64 {
		return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	}
	ux, uy := (x1-cx1)/rx, (y1-cy1)/ry
	vx, vy := (-x1-cx1)/rx, (-y1-cy1)/ry
	theta := angle(1, 0, ux, uy)
	delta := angle(ux, uy, vx, vy)
	if !sweep && delta > 0 {
		delta -= 2 * math.Pi
	} else if sweep && delta < 0 {
		delta += 2 * math.Pi
	}
	n := max(int(math.Ceil(math.Abs(delta)*math.Max(rx, ry)/flatStep)), 1)
	for i := 1; i < n; i++ {
		a := theta + delta*float64(i)/float64(n)
		ex, ey := rx*math.Cos(a), ry*math.Sin(a)
		out = append(out, point{cos*ex - sin*ey + cx, sin*ex + cos*ey + cy})
	}
	return append(out, p1)
}

func dist(a, b point) float64 { return math.Hypot(b.x-a.x, b.y-a.y) }

// scanner reads the numbers of SVG path data, which SVGO packs as tightly as
// the grammar allows: "1.5-2", ".5.5" and arc flags with no separator.
type scanner struct {
	src string
	i   int
}

func (s *scanner) done() bool { return s.i >= len(s.src) }
func (s *scanner) peek() byte { return s.src[s.i] }

func (s *scanner) skipSpace() {
	for !s.done() && strings.IndexByte(" \t\n\r,", s.peek()) >= 0 {
		s.i++
	}
}

func (s *scanner) number() (float64, error) {
	s.skipSpace()
	start := s.i
	if !s.done() && (s.peek() == '-' || s.peek() == '+') {
		s.i++
	}
	dot, exp := false, false
	for ; !s.done(); s.i++ {
		c := s.peek()
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' && !dot && !exp {
			dot = true
			continue
		}
		if (c == 'e' || c == 'E') && !exp && s.i > start {
			exp = true
			if n := s.i + 1; n < len(s.src) && (s.src[n] == '-' || s.src[n] == '+') {
				s.i++
			}
			continue
		}
		break
	}
	v, err := strconv.ParseFloat(s.src[start:s.i], 64)
	if err != nil {
		return 0, fmt.Errorf("path data: bad number at offset %d: %w", start, err)
	}
	return v, nil
}

func (s *scanner) pair() (float64, float64, error) {
	x, err := s.number()
	if err != nil {
		return 0, 0, err
	}
	y, err := s.number()
	return x, y, err
}

func (s *scanner) flag() (bool, error) {
	s.skipSpace()
	if s.done() || (s.peek() != '0' && s.peek() != '1') {
		return false, fmt.Errorf("path data: arc flag expected at offset %d", s.i)
	}
	s.i++
	return s.src[s.i-1] == '1', nil
}
