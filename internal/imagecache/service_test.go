package imagecache

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("imagecache.Manager", Label("unit", "imagecache"), func() {
	var (
		dataDir string
		svc     Manager
		src     *httptest.Server
		pngSrc  *httptest.Server
		payload []byte
	)

	BeforeEach(func() {
		dataDir = GinkgoT().TempDir()
		var err error
		svc, err = New(dataDir)
		Expect(err).NotTo(HaveOccurred())
		payload = []byte("fake-jpeg-bytes")
		pngSrc = httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
			}),
		)
		DeferCleanup(pngSrc.Close)
		src = httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(payload)
			}),
		)
		DeferCleanup(src.Close)
	})

	Describe("New", func() {
		It("rejects a dataDir it cannot create the posters folder in", func() {
			// A regular file where the folder should be refuses every user; a
			// 0o500 directory does not refuse root, so the spec would fail as root.
			bad := filepath.Join(GinkgoT().TempDir(), "not-a-dir")
			Expect(os.WriteFile(bad, nil, 0o644)).To(Succeed())
			_, err := New(bad)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Put", func() {
		It("writes the bytes at the poster path and replaces a cached one", func() {
			ctx := context.Background()
			Expect(svc.Posters().Fetch(ctx, "albums", 7, src.URL)).To(Succeed())

			Expect(
				svc.Posters().Put(ctx, "albums", 7, strings.NewReader("embedded")),
			).To(Succeed())

			got, err := os.ReadFile(svc.Posters().Path("albums", 7))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(got)).To(Equal("embedded"))
		})

		It("rejects an invalid kind", func() {
			err := svc.Posters().Put(
				context.Background(),
				"../escape",
				1,
				strings.NewReader("x"),
			)
			Expect(err).To(MatchError(ContainSubstring("kind")))
		})

		It("refuses a body over the size cap and leaves no file", func() {
			r := io.LimitReader(zeroReader{}, maxImageSize+1024)
			err := svc.Posters().Put(context.Background(), "albums", 8, r)
			Expect(err).To(MatchError(ContainSubstring("exceeds")))
			_, statErr := os.Stat(svc.Posters().Path("albums", 8))
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})
	})

	Describe("Follow", func() {
		var other *httptest.Server

		BeforeEach(func() {
			other = httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "image/jpeg")
					_, _ = w.Write([]byte("new-poster"))
				}),
			)
			DeferCleanup(other.Close)
		})

		poster := func() string {
			GinkgoHelper()
			got, err := os.ReadFile(svc.Posters().Path("movies", 5))
			Expect(err).NotTo(HaveOccurred())
			return string(got)
		}

		It("keeps the poster while the address stays the same", func() {
			ctx := context.Background()
			Expect(svc.Posters().Follow(ctx, "movies", 5, src.URL)).To(Succeed())
			payload = []byte("changed-behind-the-same-address")

			Expect(svc.Posters().Follow(ctx, "movies", 5, src.URL)).To(Succeed())
			Expect(poster()).To(Equal("fake-jpeg-bytes"))
		})

		It("replaces the poster when the address changes", func() {
			ctx := context.Background()
			Expect(svc.Posters().Follow(ctx, "movies", 5, src.URL)).To(Succeed())

			Expect(svc.Posters().Follow(ctx, "movies", 5, other.URL)).To(Succeed())
			Expect(poster()).To(Equal("new-poster"))
		})

		It("takes a poster cached without its address as the current one", func() {
			ctx := context.Background()
			Expect(svc.Posters().Fetch(ctx, "movies", 5, src.URL)).To(Succeed())

			Expect(
				svc.Posters().Follow(ctx, "movies", 5, "http://unreachable.invalid"),
			).To(Succeed())
			Expect(poster()).To(Equal("fake-jpeg-bytes"))

			Expect(svc.Posters().Follow(ctx, "movies", 5, other.URL)).To(Succeed())
			Expect(poster()).To(Equal("new-poster"))
		})

		It("drops the image and its address when offered none", func() {
			ctx := context.Background()
			Expect(svc.Posters().Follow(ctx, "movies", 5, src.URL)).To(Succeed())
			Expect(svc.Logos().Fetch(ctx, "movies", 5, pngSrc.URL)).To(Succeed())

			Expect(svc.Posters().Follow(ctx, "movies", 5)).To(Succeed())
			Expect(svc.Posters().Has("movies", 5)).To(BeFalse())
			Expect(
				filepath.Join(
					filepath.Dir(svc.Posters().Path("movies", 5)),
					"source",
				),
			).NotTo(BeAnExistingFile())
			Expect(svc.Logos().Has("movies", 5)).To(BeTrue())

			// Nothing cached is nothing to drop.
			Expect(svc.Posters().Follow(ctx, "movies", 5)).To(Succeed())
			Expect(svc.Posters().Follow(ctx, "movies", 77)).To(Succeed())
		})

		It("forgets the address with the poster", func() {
			ctx := context.Background()
			Expect(svc.Posters().Follow(ctx, "movies", 5, src.URL)).To(Succeed())
			Expect(svc.Remove("movies", 5)).To(Succeed())

			Expect(svc.Posters().Follow(ctx, "movies", 5, other.URL)).To(Succeed())
			Expect(poster()).To(Equal("new-poster"))
		})
	})

	Describe("Refresh and HasLogo", func() {
		hasLogo := func(ctx context.Context, kind string, id uint32) chan bool {
			out := make(chan bool, 1)
			go func() {
				defer GinkgoRecover()
				out <- svc.HasLogo(ctx, kind, id)
			}()
			return out
		}

		It("answers at once when nothing is in flight", func() {
			Eventually(hasLogo(context.Background(), "movies", 1)).
				Should(Receive(BeFalse()))
		})

		It("waits for the row's refresh, and for that row only", func() {
			ctx := context.Background()
			refresh := svc.Refresh("movies", 1, "", []string{pngSrc.URL})
			waiting := hasLogo(ctx, "movies", 1)
			Eventually(hasLogo(ctx, "movies", 2)).Should(Receive(BeFalse()))
			Consistently(waiting, "50ms").ShouldNot(Receive())

			refresh(ctx)
			Eventually(waiting).Should(Receive(BeTrue()))
		})

		It("gives up when the context ends", func() {
			svc.Refresh("movies", 1, "", []string{pngSrc.URL})
			ctx, cancel := context.WithTimeout(
				context.Background(), 20*time.Millisecond,
			)
			defer cancel()

			Expect(svc.HasLogo(ctx, "movies", 1)).To(BeFalse())
		})

		It("keeps a poster with no address and drops a logo with none", func() {
			ctx := context.Background()
			svc.Refresh("movies", 3, src.URL, []string{"", pngSrc.URL})(ctx)
			Expect(svc.Posters().Has("movies", 3)).To(BeTrue())
			Expect(svc.Logos().Has("movies", 3)).To(BeTrue())

			svc.Refresh("movies", 3, "", nil)(ctx)
			Expect(svc.Posters().Has("movies", 3)).To(BeTrue())
			Expect(svc.Logos().Has("movies", 3)).To(BeFalse())
		})

		It("follows the logo when the poster cannot be fetched", func() {
			svc.Refresh(
				"movies", 4, "http://unreachable.invalid", []string{pngSrc.URL},
			)(context.Background())
			Expect(svc.Posters().Has("movies", 4)).To(BeFalse())
			Expect(svc.Logos().Has("movies", 4)).To(BeTrue())
		})
	})

	Describe("Fetch", func() {
		It("keeps a poster that appeared while it was downloading", func() {
			ctx := context.Background()
			slow := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					Expect(
						svc.Posters().
							Put(ctx, "albums", 9, strings.NewReader("embedded")),
					).To(Succeed())
					w.Header().Set("Content-Type", "image/jpeg")
					_, _ = w.Write(payload)
				}),
			)
			DeferCleanup(slow.Close)

			Expect(svc.Posters().Fetch(ctx, "albums", 9, slow.URL)).To(Succeed())
			got, err := os.ReadFile(svc.Posters().Path("albums", 9))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(got)).To(Equal("embedded"))
			leftovers, err := filepath.Glob(
				filepath.Join(
					filepath.Dir(svc.Posters().Path("albums", 9)),
					"image-*.tmp",
				),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(leftovers).To(BeEmpty())
		})

		It("writes poster at deterministic path, idempotent on second call", func() {
			Expect(
				svc.Posters().Fetch(context.Background(), "movies", 42, src.URL),
			).To(Succeed())
			p := svc.Posters().Path("movies", 42)
			got, err := os.ReadFile(p)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(payload))

			Expect(
				svc.Posters().Fetch(
					context.Background(),
					"movies",
					42,
					"http://unreachable.invalid",
				),
			).To(Succeed())
			got2, err := os.ReadFile(p)
			Expect(err).NotTo(HaveOccurred())
			Expect(got2).To(Equal(payload))
		})

		It("names no outcome for a failure it cannot classify", func() {
			// A file where the row's folder should be: the write cannot start.
			st := svc.Posters().(*store)
			Expect(os.MkdirAll(st.dir+"/movies", 0o750)).To(Succeed())
			Expect(os.WriteFile(st.rowDir("movies", 3), nil, 0o600)).To(Succeed())

			outcome, err := st.sync(
				context.Background(), "movies", 3, []string{src.URL}, true,
			)
			Expect(err).To(HaveOccurred())
			Expect(outcome).To(BeEmpty())
		})

		It("rejects an invalid kind", func() {
			err := svc.Posters().Fetch(context.Background(), "../escape", 1, src.URL)
			Expect(err).To(MatchError(ContainSubstring("kind")))
		})

		It("accepts the artists and albums kinds", func() {
			for _, kind := range []string{"artists", "albums"} {
				Expect(
					svc.Posters().Fetch(context.Background(), kind, 7, src.URL),
				).To(Succeed())
				got, err := os.ReadFile(svc.Posters().Path(kind, 7))
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal(payload))
			}
		})

		It("accepts the authors and books kinds", func() {
			for _, kind := range []string{"authors", "books"} {
				Expect(
					svc.Posters().Fetch(context.Background(), kind, 7, src.URL),
				).To(Succeed())
				got, err := os.ReadFile(svc.Posters().Path(kind, 7))
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal(payload))
			}
		})

		It("returns error when source server unreachable", func() {
			closed := httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			closed.Close()
			err := svc.Posters().
				Fetch(context.Background(), "movies", 11, closed.URL)
			Expect(err).To(HaveOccurred())
			_, statErr := os.Stat(svc.Posters().Path("movies", 11))
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})

		It("returns error when ctx is canceled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := svc.Posters().Fetch(ctx, "movies", 22, src.URL)
			Expect(err).To(HaveOccurred())
			_, statErr := os.Stat(svc.Posters().Path("movies", 22))
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})

		It(
			"returns error and leaves no file when the source exceeds the size cap",
			func() {
				oversized := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "image/jpeg")
						_, _ = w.Write(make([]byte, maxImageSize+1))
					}),
				)
				DeferCleanup(oversized.Close)
				err := svc.Posters().
					Fetch(context.Background(), "movies", 55, oversized.URL)
				Expect(err).To(MatchError(ContainSubstring("exceeds")))
				_, statErr := os.Stat(svc.Posters().Path("movies", 55))
				Expect(os.IsNotExist(statErr)).To(BeTrue())
			},
		)

		It("returns error on non-200 source and leaves no file", func() {
			fail := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}),
			)
			DeferCleanup(fail.Close)
			err := svc.Posters().Fetch(context.Background(), "movies", 99, fail.URL)
			Expect(err).To(HaveOccurred())
			_, statErr := os.Stat(svc.Posters().Path("movies", 99))
			Expect(os.IsNotExist(statErr)).To(BeTrue())
			_, dirErr := os.Stat(filepath.Dir(svc.Posters().Path("movies", 99)))
			Expect(os.IsNotExist(dirErr)).To(BeTrue())
		})
	})

	Describe("logo", func() {
		const png = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
		var (
			body string
			hits int
		)

		BeforeEach(func() {
			body, hits = png, 0
			logo := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					hits++
					_, _ = io.WriteString(w, body)
				}),
			)
			DeferCleanup(logo.Close)
			src = logo
		})

		It("caches logo.png beside the poster and serves it as PNG", func() {
			ctx := context.Background()
			Expect(svc.Logos().Fetch(ctx, "movies", 5, src.URL)).To(Succeed())
			Expect(svc.Logos().Has("movies", 5)).To(BeTrue())
			Expect(
				filepath.Join(
					filepath.Dir(svc.Posters().Path("movies", 5)),
					"logo.png",
				),
			).To(BeAnExistingFile())
			Expect(svc.Posters().Path("movies", 5)).NotTo(BeAnExistingFile())

			rec := httptest.NewRecorder()
			svc.Logos().Serve(
				rec,
				httptest.NewRequest(
					http.MethodGet,
					"/posters/movies/5/logo.png",
					nil,
				),
				"movies",
				5,
			)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Header().Get("Content-Type")).To(Equal("image/png"))
			Expect(rec.Body.String()).To(Equal(png))
		})

		It("refuses a body that is not an image and leaves no file", func() {
			body = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
			err := svc.Logos().Fetch(context.Background(), "movies", 6, src.URL)
			Expect(err).To(MatchError(errNotImage))
			Expect(svc.Logos().Has("movies", 6)).To(BeFalse())
		})

		It("answers 404 when no logo is cached", func() {
			rec := httptest.NewRecorder()
			svc.Logos().Serve(
				rec,
				httptest.NewRequest(
					http.MethodGet,
					"/posters/movies/7/logo.png",
					nil,
				),
				"movies",
				7,
			)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("follows its own address, the poster's untouched", func() {
			ctx := context.Background()
			Expect(
				svc.Posters().Follow(ctx, "movies", 9, src.URL+"/poster"),
			).To(Succeed())
			Expect(svc.Logos().Follow(ctx, "movies", 9, src.URL+"/a")).To(Succeed())
			body = png + "second"

			Expect(svc.Logos().Follow(ctx, "movies", 9, src.URL+"/a")).To(Succeed())
			Expect(os.ReadFile(svc.Logos().Path("movies", 9))).To(
				BeEquivalentTo(png),
			)
			Expect(svc.Logos().Follow(ctx, "movies", 9, src.URL+"/b")).To(Succeed())
			Expect(os.ReadFile(svc.Logos().Path("movies", 9))).To(
				BeEquivalentTo(png + "second"),
			)
			Expect(
				svc.Posters().Follow(ctx, "movies", 9, src.URL+"/poster"),
			).To(Succeed())
			Expect(
				os.ReadFile(svc.Posters().Path("movies", 9)),
			).To(BeEquivalentTo(png))
		})

		It("keeps a logo that is still among the ones offered", func() {
			ctx := context.Background()
			logos := svc.Logos()
			Expect(logos.Follow(ctx, "movies", 11, src.URL+"/a")).To(Succeed())
			body = png + "second"

			// The provider reorders its logos: /a is no longer the first.
			Expect(
				logos.Follow(ctx, "movies", 11, src.URL+"/b", src.URL+"/a"),
			).To(Succeed())
			Expect(os.ReadFile(logos.Path("movies", 11))).To(BeEquivalentTo(png))

			// /a is gone from the offer: the first one replaces it.
			Expect(
				logos.Follow(ctx, "movies", 11, src.URL+"/b", src.URL+"/c"),
			).To(Succeed())
			Expect(os.ReadFile(logos.Path("movies", 11))).To(
				BeEquivalentTo(png + "second"),
			)
		})

		It("does not download a refused address twice", func() {
			ctx := context.Background()
			logos := svc.Logos()
			body = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
			Expect(logos.Follow(ctx, "movies", 13, src.URL+"/a")).To(
				MatchError(errNotImage),
			)
			before := hits

			Expect(logos.Follow(ctx, "movies", 13, src.URL+"/a")).To(Succeed())
			Expect(hits).To(Equal(before))

			// Another address is a new try, and a good one clears the refusal.
			body = png
			Expect(logos.Follow(ctx, "movies", 13, src.URL+"/b")).To(Succeed())
			Expect(logos.Has("movies", 13)).To(BeTrue())
			Expect(
				filepath.Join(
					filepath.Dir(logos.Path("movies", 13)),
					"logo.png.refused",
				),
			).NotTo(BeAnExistingFile())
		})

		It("keeps a good logo when its new address answers no image", func() {
			ctx := context.Background()
			Expect(svc.Logos().Follow(ctx, "movies", 10, src.URL+"/a")).To(Succeed())
			body = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`

			Expect(svc.Logos().Follow(ctx, "movies", 10, src.URL+"/b")).To(
				MatchError(errNotImage),
			)
			Expect(os.ReadFile(svc.Logos().Path("movies", 10))).To(
				BeEquivalentTo(png),
			)
		})

		It("goes with Remove, like the poster and both addresses", func() {
			ctx := context.Background()
			Expect(svc.Posters().Follow(ctx, "movies", 8, src.URL)).To(Succeed())
			Expect(svc.Logos().Follow(ctx, "movies", 8, src.URL)).To(Succeed())
			Expect(svc.Posters().Follow(ctx, "movies", 9, src.URL)).To(Succeed())

			Expect(svc.Remove("movies", 8)).To(Succeed())
			Expect(svc.Posters().Has("movies", 8)).To(BeFalse())
			Expect(svc.Logos().Has("movies", 8)).To(BeFalse())
			Expect(filepath.Dir(svc.Posters().Path("movies", 8))).NotTo(
				BeADirectory(),
			)
			Expect(svc.Posters().Has("movies", 9)).To(BeTrue())
		})

		It("removes a row that has a logo and no poster", func() {
			Expect(
				svc.Logos().Fetch(context.Background(), "movies", 12, src.URL),
			).To(Succeed())
			Expect(svc.Remove("movies", 12)).To(Succeed())
			Expect(svc.Logos().Has("movies", 12)).To(BeFalse())
		})
	})

	Describe("Serve", func() {
		It("serves the file with image/jpeg and cache headers", func() {
			Expect(
				svc.Posters().Fetch(context.Background(), "movies", 7, src.URL),
			).To(Succeed())
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodGet,
				"/posters/movies/7/poster.jpg",
				nil,
			)
			svc.Posters().Serve(rec, req, "movies", 7)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Header().Get("Content-Type")).To(Equal("image/jpeg"))
			Expect(rec.Header().Get("Cache-Control")).To(Equal("no-cache"))
			Expect(rec.Header().Get("Last-Modified")).ToNot(BeEmpty())
			body, _ := io.ReadAll(rec.Body)
			Expect(body).To(Equal(payload))
		})

		It("answers a poster unchanged since the browser's copy with 304", func() {
			Expect(
				svc.Posters().Fetch(context.Background(), "movies", 7, src.URL),
			).To(Succeed())
			first := httptest.NewRecorder()
			svc.Posters().Serve(
				first,
				httptest.NewRequest(
					http.MethodGet,
					"/posters/movies/7/poster.jpg",
					nil,
				),
				"movies",
				7,
			)
			req := httptest.NewRequest(
				http.MethodGet,
				"/posters/movies/7/poster.jpg",
				nil,
			)
			req.Header.Set("If-Modified-Since", first.Header().Get("Last-Modified"))
			rec := httptest.NewRecorder()
			svc.Posters().Serve(rec, req, "movies", 7)
			Expect(rec.Code).To(Equal(http.StatusNotModified))
		})

		It("serves the sniffed type of a PNG body", func() {
			png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
			Expect(
				svc.Posters().
					Put(context.Background(), "albums", 3, strings.NewReader(png)),
			).To(Succeed())
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodGet,
				"/posters/albums/3/poster.jpg",
				nil,
			)
			svc.Posters().Serve(rec, req, "albums", 3)
			Expect(rec.Header().Get("Content-Type")).To(Equal("image/png"))
		})

		It("returns 404 when missing", func() {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodGet,
				"/posters/movies/404/poster.jpg",
				nil,
			)
			svc.Posters().Serve(rec, req, "movies", 404)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("rejects an invalid kind", func() {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodGet,
				"/posters/x/1/poster.jpg",
				nil,
			)
			svc.Posters().Serve(rec, req, "x", 1)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("serves a cached authors and books poster", func() {
			for _, kind := range []string{"authors", "books"} {
				Expect(
					svc.Posters().Fetch(context.Background(), kind, 7, src.URL),
				).To(Succeed())
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(
					http.MethodGet,
					"/posters/"+kind+"/7/poster.jpg",
					nil,
				)
				svc.Posters().Serve(rec, req, kind, 7)
				Expect(rec.Code).To(Equal(http.StatusOK))
			}
		})

		It("serves a cached artists and albums poster", func() {
			for _, kind := range []string{"artists", "albums"} {
				Expect(
					svc.Posters().Fetch(context.Background(), kind, 7, src.URL),
				).To(Succeed())
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(
					http.MethodGet,
					"/posters/"+kind+"/7/poster.jpg",
					nil,
				)
				svc.Posters().Serve(rec, req, kind, 7)
				Expect(rec.Code).To(Equal(http.StatusOK))
			}
		})
	})

	Describe("Remove", func() {
		It("drops a cached albums poster", func() {
			Expect(
				svc.Posters().Fetch(context.Background(), "albums", 3, src.URL),
			).To(Succeed())
			Expect(svc.Remove("albums", 3)).To(Succeed())
			_, err := os.Stat(svc.Posters().Path("albums", 3))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("drops a cached books poster", func() {
			Expect(
				svc.Posters().Fetch(context.Background(), "books", 3, src.URL),
			).To(Succeed())
			Expect(svc.Remove("books", 3)).To(Succeed())
			_, err := os.Stat(svc.Posters().Path("books", 3))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
})

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
