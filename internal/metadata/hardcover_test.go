package metadata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/time/rate"
)

func newTestHardcover(token string) *Hardcover {
	c := *http.DefaultClient
	return &Hardcover{
		client:  &c,
		token:   token,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}
}

var _ = Describe("Hardcover provider", Label("unit", "metadata"), func() {
	var (
		hc  *Hardcover
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		hc = newTestHardcover("test-token")
	})

	Describe("SearchAuthors", func() {
		It("POSTs a GraphQL query with the bearer token and maps hits", func() {
			var gotAuth string
			var gotBody map[string]any
			hc.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					gotAuth = r.Header.Get("Authorization")
					raw, err := io.ReadAll(r.Body)
					Expect(err).NotTo(HaveOccurred())
					Expect(json.Unmarshal(raw, &gotBody)).To(Succeed())
					return jsonResponse(
						200,
						`{"data":{"search":{"results":{"hits":[{"document":{"id":"219851","name":"Brandon Sanderson","books_count":90,"image":{"url":"https://img/x.jpg"}}}]}}}}`,
					), nil
				},
			)
			res, err := hc.SearchAuthors(ctx, "sanderson")
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(HaveLen(1))
			Expect(res[0].HardcoverID).To(Equal(uint32(219851)))
			Expect(res[0].Name).To(Equal("Brandon Sanderson"))
			Expect(res[0].ImageURL).To(Equal("https://img/x.jpg"))
			Expect(gotAuth).To(Equal("Bearer test-token"))
			Expect(gotBody["query"]).To(ContainSubstring("search("))
		})
	})

	Describe("GetAuthor", func() {
		It("fetches the author row then pages the bibliography", func() {
			calls := 0
			hc.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					calls++
					raw, err := io.ReadAll(r.Body)
					Expect(err).NotTo(HaveOccurred())
					if calls == 1 {
						Expect(string(raw)).To(ContainSubstring("authors("))
						return jsonResponse(
							200,
							`{"data":{"authors":[{"id":219851,"name":"Brandon Sanderson","bio":"...","cached_image":{"url":"https://img/a.jpg"}}]}}`,
						), nil
					}
					Expect(string(raw)).To(ContainSubstring("books("))
					return jsonResponse(
						200,
						`{"data":{"books":[{"id":1,"title":"Elantris","release_date":"2005-04-21","cached_image":{"url":"https://img/b.jpg"},"book_series":[{"position":1,"series":{"name":"Elantris"}}]}]}}`,
					), nil
				},
			)
			a, err := hc.GetAuthor(ctx, 219851)
			Expect(err).NotTo(HaveOccurred())
			Expect(a.Name).To(Equal("Brandon Sanderson"))
			Expect(a.ImageURL).To(Equal("https://img/a.jpg"))
			Expect(a.Books).To(HaveLen(1))
			Expect(a.Books[0].ReleaseDate.Year()).To(Equal(2005))
			Expect(a.Books[0].SeriesName).To(Equal("Elantris"))
			Expect(a.Books[0].SeriesPosition).To(Equal("1"))
			Expect(a.Books[0].CoverURL).To(Equal("https://img/b.jpg"))
		})

		It(
			"accepts a string cached_image, a year-only date and a fractional position",
			func() {
				calls := 0
				hc.client.Transport = mbRoundTripper(
					func(*http.Request) (*http.Response, error) {
						calls++
						if calls == 1 {
							return jsonResponse(
								200,
								`{"data":{"authors":[{"id":1,"name":"A","bio":"","cached_image":"https://img/a.jpg"}]}}`,
							), nil
						}
						return jsonResponse(
							200,
							`{"data":{"books":[{"id":2,"title":"T","release_date":"1999","cached_image":"https://img/b.jpg","book_series":[{"position":1.5,"series":{"name":"S"}}]}]}}`,
						), nil
					},
				)
				a, err := hc.GetAuthor(ctx, 1)
				Expect(err).NotTo(HaveOccurred())
				Expect(a.ImageURL).To(Equal("https://img/a.jpg"))
				Expect(a.Books[0].ReleaseDate.Year()).To(Equal(1999))
				Expect(a.Books[0].SeriesPosition).To(Equal("1.5"))
			},
		)

		It("errors when the author does not exist", func() {
			hc.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(200, `{"data":{"authors":[]}}`), nil
				},
			)
			_, err := hc.GetAuthor(ctx, 1)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetBook", func() {
		It("maps editions with reading formats", func() {
			hc.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(
						200,
						`{"data":{"books":[{"id":1,"title":"Elantris","description":"d","release_date":"2005-04-21","cached_image":{"url":"u"},"contributions":[{"author_id":219851}],"editions":[{"isbn_13":"9780765311771","asin":"","reading_format_id":1,"pages":492},{"isbn_13":"","asin":"B000UZQI0Q","reading_format_id":2,"audio_seconds":98220}]}]}}`,
					), nil
				},
			)
			b, err := hc.GetBook(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(b.AuthorHardcover).To(Equal(uint32(219851)))
			Expect(b.Overview).To(Equal("d"))
			Expect(b.Editions).To(HaveLen(2))
			Expect(b.Editions[0].Format).To(Equal("physical"))
			Expect(b.Editions[0].Pages).To(Equal(uint16(492)))
			Expect(b.Editions[1].Format).To(Equal("audiobook"))
			Expect(b.Editions[1].AudioSeconds).To(Equal(uint32(98220)))
		})

		It("errors when the book does not exist", func() {
			hc.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(200, `{"data":{"books":[]}}`), nil
				},
			)
			_, err := hc.GetBook(ctx, 1)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("SearchBooks", func() {
		It("searches by combined title/author query", func() {
			var gotBody map[string]any
			hc.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					raw, err := io.ReadAll(r.Body)
					Expect(err).NotTo(HaveOccurred())
					Expect(json.Unmarshal(raw, &gotBody)).To(Succeed())
					return jsonResponse(
						200,
						`{"data":{"search":{"results":{"hits":[{"document":{"id":"1","title":"Elantris","author_names":["Brandon Sanderson"],"release_year":2005}}]}}}}`,
					), nil
				},
			)
			res, err := hc.SearchBooks(ctx, "Elantris Brandon Sanderson")
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(HaveLen(1))
			Expect(res[0].HardcoverID).To(Equal(uint32(1)))
			Expect(res[0].Title).To(Equal("Elantris"))
			Expect(res[0].Author).To(Equal("Brandon Sanderson"))
			Expect(res[0].Year).To(Equal(uint16(2005)))
			Expect(gotBody["query"]).To(ContainSubstring(`"Book"`))
		})
	})

	Describe("BookByISBN", func() {
		It("resolves an edition ISBN to its book id", func() {
			hc.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(
						200,
						`{"data":{"editions":[{"book_id":42}]}}`,
					), nil
				},
			)
			id, err := hc.BookByISBN(ctx, "9780765311771")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(uint32(42)))
		})

		It("returns 0 without error when unknown", func() {
			hc.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(200, `{"data":{"editions":[]}}`), nil
				},
			)
			id, err := hc.BookByISBN(ctx, "9780000000000")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(BeZero())
		})
	})

	Describe("expired token", func() {
		It("maps a 401 to ErrHardcoverUnauthorized and trips AuthRejected", func() {
			hc.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(401, `{}`), nil
				},
			)
			Expect(hc.AuthRejected()).To(BeFalse())
			_, err := hc.SearchAuthors(ctx, "x")
			Expect(err).To(MatchError(ErrHardcoverUnauthorized))
			Expect(hc.AuthRejected()).To(BeTrue())
		})
	})

	Describe("NewHardcover", func() {
		It("fails with ErrHardcoverKeyMissing when no key is configured", func() {
			_, err := NewHardcover()
			Expect(err).To(MatchError(ErrHardcoverKeyMissing))
		})
	})
})
