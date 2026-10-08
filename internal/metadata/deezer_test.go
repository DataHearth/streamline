package metadata

import (
	"context"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/time/rate"
)

var _ = Describe("Deezer cover provider", Label("unit", "metadata"), func() {
	var (
		dz  *Deezer
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		dz = NewDeezer()
		dz.limiter = rate.NewLimiter(rate.Inf, 1)
	})

	Describe("CoverByUPC", func() {
		It("returns the xl cover of the album with that barcode", func() {
			var gotPath string
			dz.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					gotPath = r.URL.Path
					return jsonResponse(
						200,
						`{"id":1,"cover_xl":"https://cdn.example/xl.jpg"}`,
					), nil
				},
			)
			got, err := dz.CoverByUPC(ctx, "0720642442524")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal("https://cdn.example/xl.jpg"))
			Expect(gotPath).To(Equal("/album/upc:0720642442524"))
		})

		It("treats a 404 as no cover", func() {
			dz.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(404, `{}`), nil
				},
			)
			got, err := dz.CoverByUPC(ctx, "1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("treats Deezer's 200 error body as no cover", func() {
			dz.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(
						200,
						`{"error":{"type":"DataException","message":"no data","code":800}}`,
					), nil
				},
			)
			got, err := dz.CoverByUPC(ctx, "1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("skips the request for an empty barcode", func() {
			dz.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					Fail("no request expected")
					return nil, nil
				},
			)
			got, err := dz.CoverByUPC(ctx, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("fails on a server error", func() {
			dz.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(503, `{}`), nil
				},
			)
			_, err := dz.CoverByUPC(ctx, "1")
			Expect(err).To(HaveOccurred())
		})

		It("refuses an oversized body", func() {
			dz.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					big := make([]byte, maxDeezerResponse+10)
					for i := range big {
						big[i] = 'a'
					}
					return jsonResponse(200, `{"cover_xl":"`+string(big)+`"}`), nil
				},
			)
			_, err := dz.CoverByUPC(ctx, "1")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("SearchCover", func() {
		It("returns the first hit with its artist", func() {
			var gotQuery string
			dz.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					gotQuery = r.URL.Query().Get("q")
					return jsonResponse(
						200,
						`{"data":[{"cover_xl":"https://cdn.example/a.jpg","artist":{"name":"Nirvana"}},{"cover_xl":"https://cdn.example/b.jpg","artist":{"name":"Other"}}]}`,
					), nil
				},
			)
			hit, err := dz.SearchCover(ctx, "Nirvana", "Nevermind")
			Expect(err).NotTo(HaveOccurred())
			Expect(hit).To(Equal(&CoverHit{
				ArtistName: "Nirvana",
				CoverURL:   "https://cdn.example/a.jpg",
			}))
			Expect(gotQuery).To(Equal(`artist:"Nirvana" album:"Nevermind"`))
		})

		It("returns nil when there are no hits", func() {
			dz.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(200, `{"data":[],"total":0}`), nil
				},
			)
			hit, err := dz.SearchCover(ctx, "x", "y")
			Expect(err).NotTo(HaveOccurred())
			Expect(hit).To(BeNil())
		})

		It("treats a 404 as no hit", func() {
			dz.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(404, `{}`), nil
				},
			)
			hit, err := dz.SearchCover(ctx, "x", "y")
			Expect(err).NotTo(HaveOccurred())
			Expect(hit).To(BeNil())
		})
	})
})
