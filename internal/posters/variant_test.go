package posters

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("poster variants", Label("unit", "posters"), func() {
	var svc Manager

	jpegOf := func(w, h int) []byte {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
			}
		}
		var buf bytes.Buffer
		Expect(jpeg.Encode(&buf, img, nil)).To(Succeed())
		return buf.Bytes()
	}

	serve := func(kind string, id uint32, query string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodGet,
			"/posters/x/1/poster.jpg"+query,
			nil,
		)
		svc.Serve(rec, req, kind, id)
		return rec
	}

	BeforeEach(func() {
		var err error
		svc, err = New(GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
	})

	It("serves a poster scaled to the asked width, keeping its proportions", func() {
		Expect(
			svc.Put(
				context.Background(),
				"movies",
				1,
				bytes.NewReader(jpegOf(780, 1170)),
			),
		).
			To(Succeed())

		rec := serve("movies", 1, "?w=320")

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Header().Get("Content-Type")).To(Equal("image/jpeg"))
		Expect(rec.Header().Get("Cache-Control")).To(ContainSubstring("max-age="))
		Expect(rec.Header().Get("Last-Modified")).NotTo(BeEmpty())
		cfg, err := jpeg.DecodeConfig(rec.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Width).To(Equal(320))
		Expect(cfg.Height).To(Equal(480))
	})

	It("keeps the variant beside the poster and reuses it", func() {
		Expect(
			svc.Put(
				context.Background(),
				"movies",
				1,
				bytes.NewReader(jpegOf(780, 1170)),
			),
		).
			To(Succeed())
		serve("movies", 1, "?w=200")

		path := variantPath(svc.Path("movies", 1), 200)
		Expect(os.WriteFile(path, []byte("kept"), 0o600)).To(Succeed())
		orig, err := os.Stat(svc.Path("movies", 1))
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Chtimes(path, orig.ModTime(), orig.ModTime())).To(Succeed())

		Expect(serve("movies", 1, "?w=200").Body.String()).To(Equal("kept"))
	})

	It("rebuilds a variant once the poster is replaced", func() {
		ctx := context.Background()
		Expect(
			svc.Put(ctx, "albums", 2, bytes.NewReader(jpegOf(500, 500))),
		).To(Succeed())
		serve("albums", 2, "?w=120")

		Expect(
			svc.Put(ctx, "albums", 2, bytes.NewReader(jpegOf(600, 300))),
		).To(Succeed())
		later := time.Now().Add(time.Hour)
		Expect(os.Chtimes(svc.Path("albums", 2), later, later)).To(Succeed())

		cfg, err := jpeg.DecodeConfig(serve("albums", 2, "?w=120").Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Height).To(Equal(60))
	})

	It("serves the poster itself when it is no wider than the asked width", func() {
		body := jpegOf(300, 450)
		Expect(
			svc.Put(context.Background(), "books", 3, bytes.NewReader(body)),
		).To(Succeed())

		rec := serve("books", 3, "?w=320")

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.Bytes()).To(Equal(body))
	})

	It("serves the poster itself when it cannot be decoded", func() {
		Expect(
			svc.Put(
				context.Background(),
				"movies",
				4,
				strings.NewReader("not an image"),
			),
		).
			To(Succeed())

		rec := serve("movies", 4, "?w=320")

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("not an image"))
	})

	It("serves the poster itself when it has transparency", func() {
		img := image.NewNRGBA(image.Rect(0, 0, 400, 400))
		var buf bytes.Buffer
		Expect(png.Encode(&buf, img)).To(Succeed())
		Expect(
			svc.Put(
				context.Background(),
				"artists",
				5,
				bytes.NewReader(buf.Bytes()),
			),
		).
			To(Succeed())

		rec := serve("artists", 5, "?w=200")

		Expect(rec.Header().Get("Content-Type")).To(Equal("image/png"))
		Expect(rec.Body.Bytes()).To(Equal(buf.Bytes()))
	})

	It("refuses a width outside the ladder", func() {
		Expect(
			svc.Put(
				context.Background(),
				"movies",
				6,
				bytes.NewReader(jpegOf(780, 1170)),
			),
		).
			To(Succeed())

		for _, q := range []string{"?w=321", "?w=0", "?w=-200", "?w=abc", "?w=99999"} {
			Expect(serve("movies", 6, q).Code).To(Equal(http.StatusBadRequest), q)
		}
		entries, err := os.ReadDir(
			strings.TrimSuffix(svc.Path("movies", 6), "poster.jpg"),
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(1))
	})

	It("answers 404 for a width of a poster that is not cached", func() {
		Expect(serve("movies", 404, "?w=320").Code).To(Equal(http.StatusNotFound))
	})

	It("ignores the retry parameter the SPA adds", func() {
		body := jpegOf(100, 150)
		Expect(
			svc.Put(context.Background(), "movies", 7, bytes.NewReader(body)),
		).To(Succeed())

		Expect(serve("movies", 7, "?retry=2").Body.Bytes()).To(Equal(body))
	})
})
