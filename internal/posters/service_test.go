package posters

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("posters.Manager", Label("unit", "posters"), func() {
	var (
		dataDir string
		svc     Manager
		src     *httptest.Server
		payload []byte
	)

	BeforeEach(func() {
		dataDir = GinkgoT().TempDir()
		var err error
		svc, err = New(dataDir)
		Expect(err).NotTo(HaveOccurred())
		payload = []byte("fake-jpeg-bytes")
		src = httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(payload)
			}),
		)
		DeferCleanup(src.Close)
	})

	Describe("New", func() {
		It("rejects a non-writable dataDir", func() {
			bad := filepath.Join(GinkgoT().TempDir(), "ro")
			Expect(os.MkdirAll(bad, 0o500)).To(Succeed())
			_, err := New(bad)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Fetch", func() {
		It("writes poster at deterministic path, idempotent on second call", func() {
			Expect(
				svc.Fetch(context.Background(), "movies", 42, src.URL),
			).To(Succeed())
			p := svc.Path("movies", 42)
			got, err := os.ReadFile(p)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(payload))

			Expect(
				svc.Fetch(
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

		It("rejects an invalid kind", func() {
			err := svc.Fetch(context.Background(), "../escape", 1, src.URL)
			Expect(err).To(MatchError(ContainSubstring("kind")))
		})

		It("returns error when source server unreachable", func() {
			closed := httptest.NewServer(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			)
			closed.Close()
			err := svc.Fetch(context.Background(), "movies", 11, closed.URL)
			Expect(err).To(HaveOccurred())
			_, statErr := os.Stat(svc.Path("movies", 11))
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})

		It("returns error when ctx is canceled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := svc.Fetch(ctx, "movies", 22, src.URL)
			Expect(err).To(HaveOccurred())
			_, statErr := os.Stat(svc.Path("movies", 22))
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})

		It("returns error on non-200 source and leaves no file", func() {
			fail := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}),
			)
			DeferCleanup(fail.Close)
			err := svc.Fetch(context.Background(), "movies", 99, fail.URL)
			Expect(err).To(HaveOccurred())
			_, statErr := os.Stat(svc.Path("movies", 99))
			Expect(os.IsNotExist(statErr)).To(BeTrue())
		})
	})

	Describe("Serve", func() {
		It("serves the file with image/jpeg and cache headers", func() {
			Expect(
				svc.Fetch(context.Background(), "movies", 7, src.URL),
			).To(Succeed())
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodGet,
				"/posters/movies/7/poster.jpg",
				nil,
			)
			svc.Serve(rec, req, "movies", 7)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Header().Get("Content-Type")).To(Equal("image/jpeg"))
			Expect(
				rec.Header().Get("Cache-Control"),
			).To(ContainSubstring("max-age="))
			body, _ := io.ReadAll(rec.Body)
			Expect(body).To(Equal(payload))
		})

		It("returns 404 when missing", func() {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodGet,
				"/posters/movies/404/poster.jpg",
				nil,
			)
			svc.Serve(rec, req, "movies", 404)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("rejects an invalid kind", func() {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(
				http.MethodGet,
				"/posters/x/1/poster.jpg",
				nil,
			)
			svc.Serve(rec, req, "x", 1)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})
})
