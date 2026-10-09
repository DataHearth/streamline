package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

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

// serve answers every request with the next body of the list (the last one
// repeats) and records the decoded GraphQL requests.
type served struct {
	queries []string
	vars    []map[string]any
}

func (s *served) transport(bodies ...string) mbRoundTripper {
	return func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		Expect(err).NotTo(HaveOccurred())
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		Expect(json.Unmarshal(raw, &req)).To(Succeed())
		s.queries = append(s.queries, req.Query)
		s.vars = append(s.vars, req.Variables)
		i := min(len(s.queries)-1, len(bodies)-1)
		return jsonResponse(200, bodies[i]), nil
	}
}

const bookRow = `{
  "id": 1, "title": "Elantris", "description": "A fallen city.",
  "release_date": "2005-04-21", "release_year": 2005, "rating": 4.17,
  "users_count": 5000, "compilation": false,
  "cached_image": {"url": "https://img/b.jpg"},
  "cached_contributors": [
    {"author": {"id": 10, "name": "Brandon Sanderson", "cachedImage": {"url": "https://img/a.jpg"}}, "contribution": null},
    {"author": {"id": 11, "name": "A Cover Person"}, "contribution": "Cover Artist"},
    {"author": {"id": 12, "name": "An Editor"}, "contribution": "Editor"},
    {"author": {"id": 13, "name": "A Translator"}, "contribution": "Translator"}
  ],
  "cached_tags": {"Genre": [{"tag": "Fantasy", "count": 90}, {"tag": "Epic", "count": 120}, {"tag": "Fiction", "count": 10}, {"tag": "Magic", "count": 5}]},
  "book_series": [{"position": 1, "series": {"id": 77, "name": "Elantris"}}],
  "digital": [
    {"id": 101, "title": "Elantris", "release_year": 2006, "users_count": 900, "reading_format_id": 4, "isbn_13": "9780765311771",
     "language": {"code2": "en"}, "publisher": {"name": "Tor"}},
    {"id": 102, "title": "Elantris (livre audio)", "audio_seconds": 98220, "users_count": 300, "reading_format_id": 2,
     "language": {"code2": "fr"}, "publisher": {"name": "Audible"},
     "cached_contributors": [{"author": {"id": 50, "name": "A Narrator"}, "contribution": "Narrator"}]}
  ],
  "physical": [
    {"id": 103, "title": "Elantris", "pages": 492, "users_count": 4000, "reading_format_id": 1,
     "language": {"code2": "en"}, "publisher": {"name": "Tor"}},
    {"id": 104, "title": "Elantris (broche)", "pages": 600, "users_count": 700, "reading_format_id": 1,
     "language": {"code2": "fr"}, "publisher": {"name": "Mnemos"},
     "cached_contributors": [{"author": {"id": 60, "name": "Une Traductrice"}, "contribution": "Translator"}]}
  ],
  "earliest": [
    {"id": 105, "title": "Elantris", "release_date": "2005-04-21", "users_count": 10, "reading_format_id": 1,
     "language": {"code2": "en"}, "publisher": {"name": "Tor"}}
  ]
}`

var _ = Describe("Hardcover provider", Label("unit", "metadata"), func() {
	var (
		hc  *Hardcover
		ctx context.Context
		srv *served
	)

	BeforeEach(func() {
		ctx = context.Background()
		hc = newTestHardcover("test-token")
		srv = &served{}
	})

	Describe("SearchBooks", func() {
		It("POSTs a GraphQL query with the bearer token and maps hits", func() {
			var gotAuth string
			hc.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					gotAuth = r.Header.Get("Authorization")
					return srv.transport(
						`{"data":{"search":{"results":{"hits":[{"document":{"id":"1","title":"Elantris","author_names":["Brandon Sanderson"],"release_year":2005}}]}}}}`,
					)(
						r,
					)
				},
			)
			res, err := hc.SearchBooks(ctx, "Elantris Brandon Sanderson")
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(HaveLen(1))
			Expect(res[0].HardcoverID).To(Equal(uint32(1)))
			Expect(res[0].Author).To(Equal("Brandon Sanderson"))
			Expect(res[0].Year).To(Equal(uint16(2005)))
			Expect(gotAuth).To(Equal("Bearer test-token"))
			Expect(srv.queries[0]).To(ContainSubstring(`"Book"`))
		})
	})

	Describe("LookupBooks", func() {
		const body = `{"data":{"search":{"results":{"hits":[
		  {"document":{"id":"7","title":"Elantris","author_names":["Brandon Sanderson","Second","Third","Fourth"],
		   "release_year":2005,"image":{"url":"https://img/7.jpg"},"genres":["Fantasy"]}},
		  {"document":{"id":"x","title":"Skipped"}}]}}}}`

		It(
			"maps a hit, joins up to three authors and keeps the image and genres",
			func() {
				hc.client.Transport = srv.transport(body)

				hits, err := hc.LookupBooks(ctx, "Elantris")

				Expect(err).NotTo(HaveOccurred())
				Expect(hits).To(HaveLen(1))
				Expect(hits[0]).To(Equal(BookLookupHit{
					HardcoverID: 7, Type: "book", Title: "Elantris",
					Author: "Brandon Sanderson & Second & Third", Year: 2005,
					Genres: []string{"Fantasy"}, ImageURL: "https://img/7.jpg",
				}))
				Expect(srv.vars[0]["per"]).To(BeEquivalentTo(12))
			},
		)

		It(
			"memoises a query by its normalised form, one request for all spellings",
			func() {
				hc.client.Transport = srv.transport(body)

				_, err := hc.LookupBooks(ctx, "Elantris")
				Expect(err).NotTo(HaveOccurred())
				_, err = hc.LookupBooks(ctx, "  elantris ")
				Expect(err).NotTo(HaveOccurred())

				Expect(srv.queries).To(HaveLen(1))
			},
		)

		It(
			"does not mix a book search with a series search of the same words",
			func() {
				hc.client.Transport = srv.transport(
					body,
					`{"data":{"search":{"results":{"hits":[{"document":{"id":"3","name":"Elantris","author_name":"BS","books_count":5,"primary_books_count":3,"is_completed":false}}]}}}}`,
				)

				books, err := hc.LookupBooks(ctx, "Elantris")
				Expect(err).NotTo(HaveOccurred())
				series, err := hc.LookupSeries(ctx, "Elantris")
				Expect(err).NotTo(HaveOccurred())

				Expect(books).To(HaveLen(1))
				Expect(series).To(HaveLen(1))
				Expect(srv.queries).To(HaveLen(2))
			},
		)
	})

	Describe("LookupSeries", func() {
		It("prefers the primary book count and reads is_completed", func() {
			hc.client.Transport = srv.transport(
				`{"data":{"search":{"results":{"hits":[
				  {"document":{"id":"3","name":"One Piece","author_name":"Oda","books_count":120,"primary_books_count":105,"is_completed":false}},
				  {"document":{"id":"4","name":"Done","author_name":"X","books_count":9}}]}}}}`,
			)

			hits, err := hc.LookupSeries(ctx, "one")

			Expect(err).NotTo(HaveOccurred())
			Expect(hits).To(HaveLen(2))
			Expect(hits[0].Volumes).To(Equal(uint32(105)))
			Expect(*hits[0].Ongoing).To(BeTrue())
			Expect(hits[0].Type).To(Equal("series"))
			Expect(hits[1].Volumes).To(Equal(uint32(9)))
			Expect(hits[1].Ongoing).To(BeNil())
			Expect(srv.queries[0]).To(ContainSubstring(`"Series"`))
		})
	})

	Describe("GetBooks", func() {
		wrap := func(rows ...string) string {
			return `{"data":{"books":[` + strings.Join(rows, ",") + `]}}`
		}

		It("builds a book from one batch request", func() {
			hc.client.Transport = srv.transport(wrap(bookRow))

			recs, err := hc.GetBooks(ctx, []uint32{1})

			Expect(err).NotTo(HaveOccurred())
			Expect(srv.queries).To(HaveLen(1))
			Expect(srv.queries[0]).To(ContainSubstring("digital: editions"))
			Expect(srv.queries[0]).To(ContainSubstring("physical: editions"))
			Expect(srv.queries[0]).To(ContainSubstring("earliest: editions"))
			Expect(recs).To(HaveLen(1))
			b := recs[0]
			Expect(b.HardcoverID).To(Equal(uint32(1)))
			Expect(b.Description).To(Equal("A fallen city."))
			Expect(b.ReleaseYear).To(Equal(uint16(2005)))
			Expect(b.ReleaseDate.Year()).To(Equal(2005))
			Expect(b.Rating).To(BeNumerically("~", 4.17))
			Expect(b.UsersCount).To(Equal(uint32(5000)))
			Expect(b.CoverURL).To(Equal("https://img/b.jpg"))
			Expect(b.Series).To(HaveLen(1))
			Expect(b.Series[0].SeriesHardcoverID).To(Equal(uint32(77)))
			Expect(*b.Series[0].Position).To(Equal(1.0))
		})

		It(
			"keeps the makers only, null counting as author, and drops the rest",
			func() {
				hc.client.Transport = srv.transport(wrap(bookRow))

				recs, err := hc.GetBooks(ctx, []uint32{1})

				Expect(err).NotTo(HaveOccurred())
				Expect(recs[0].Credits).To(Equal([]BookCredit{
					{
						AuthorHardcoverID: 10,
						Name:              "Brandon Sanderson",
						ImageURL:          "https://img/a.jpg",
						Role:              RoleAuthor,
					},
					{AuthorHardcoverID: 11, Name: "A Cover Person", Role: RoleCover},
				}))
			},
		)

		It("orders genres by count and takes the top three", func() {
			hc.client.Transport = srv.transport(wrap(bookRow))

			recs, err := hc.GetBooks(ctx, []uint32{1})

			Expect(err).NotTo(HaveOccurred())
			Expect(recs[0].Genre).To(Equal("Epic"))
			Expect(recs[0].Genres).To(Equal([]string{"Epic", "Fantasy", "Fiction"}))
		})

		It(
			"selects editions per language, publisher and format and marks the original",
			func() {
				hc.client.Transport = srv.transport(wrap(bookRow))

				recs, err := hc.GetBooks(ctx, []uint32{1})

				Expect(err).NotTo(HaveOccurred())
				b := recs[0]
				byID := map[uint32]EditionRecord{}
				for _, e := range b.Editions {
					byID[e.HardcoverID] = e
				}
				// en/Tor ebook: the digital edition outranks the paperback and the
				// earliest, which share its key.
				Expect(byID).To(HaveKey(uint32(101)))
				Expect(byID).NotTo(HaveKey(uint32(103)))
				Expect(byID).NotTo(HaveKey(uint32(105)))
				Expect(byID[101].Format).To(Equal(FormatEbook))
				Expect(byID[101].Original).To(BeTrue())
				Expect(byID[101].ISBN13).To(Equal("9780765311771"))
				// The audiobook keeps its narrator and length.
				Expect(byID[102].Format).To(Equal(FormatAudiobook))
				Expect(byID[102].Narrator).To(Equal("A Narrator"))
				Expect(byID[102].DurationSeconds).To(Equal(uint32(98220)))
				// French has no digital ebook, so its paperback is the ebook candidate.
				Expect(byID[104].Format).To(Equal(FormatEbook))
				Expect(byID[104].Translator).To(Equal("Une Traductrice"))
				Expect(byID[104].TranslatorID).To(Equal(uint32(60)))
				Expect(byID[104].Pages).To(Equal(uint16(600)))
				Expect(b.OriginalLanguage).To(Equal("en"))
			},
		)

		It("classifies the kind from the genres and original language", func() {
			manga := strings.Replace(
				bookRow,
				`{"tag": "Fantasy", "count": 90}`,
				`{"tag": "Manga", "count": 900}`,
				1,
			)
			hc.client.Transport = srv.transport(wrap(manga))

			recs, err := hc.GetBooks(ctx, []uint32{1})

			Expect(err).NotTo(HaveOccurred())
			Expect(recs[0].Kind).To(Equal(BookKindManga))
		})

		It(
			"memoises a book so a second ask costs nothing, a fresh read does not",
			func() {
				hc.client.Transport = srv.transport(wrap(bookRow))

				_, err := hc.GetBooks(ctx, []uint32{1})
				Expect(err).NotTo(HaveOccurred())
				_, err = hc.GetBooks(ctx, []uint32{1})
				Expect(err).NotTo(HaveOccurred())
				Expect(srv.queries).To(HaveLen(1))

				_, err = hc.GetBooksFresh(ctx, []uint32{1})
				Expect(err).NotTo(HaveOccurred())
				Expect(srv.queries).To(HaveLen(2))
			},
		)

		It("batches by twenty and answers in the order asked", func() {
			ids := make([]uint32, 45)
			for i := range ids {
				ids[i] = uint32(i + 1)
			}
			hc.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					raw, _ := io.ReadAll(r.Body)
					var req struct {
						Variables struct {
							IDs []uint32 `json:"ids"`
						} `json:"variables"`
					}
					Expect(json.Unmarshal(raw, &req)).To(Succeed())
					srv.queries = append(srv.queries, "")
					srv.vars = append(
						srv.vars,
						map[string]any{"n": len(req.Variables.IDs)},
					)
					rows := make([]string, 0, len(req.Variables.IDs))
					// Hardcover answers in no particular order.
					for _, id := range slices.Backward(req.Variables.IDs) {
						rows = append(
							rows,
							fmt.Sprintf(
								`{"id": %d, "title": "T%d", "cached_contributors": []}`,
								id,
								id,
							),
						)
					}
					return jsonResponse(200, wrap(rows...)), nil
				},
			)

			recs, err := hc.GetBooks(ctx, ids)

			Expect(err).NotTo(HaveOccurred())
			Expect(srv.queries).To(HaveLen(3))
			Expect(srv.vars[0]["n"]).To(Equal(20))
			Expect(srv.vars[2]["n"]).To(Equal(5))
			Expect(recs).To(HaveLen(45))
			for i, r := range recs {
				Expect(r.HardcoverID).To(Equal(ids[i]))
			}
		})

		It(
			"skips an id Hardcover does not know and deduplicates the ids asked",
			func() {
				hc.client.Transport = srv.transport(
					wrap(`{"id": 2, "title": "Known", "cached_contributors": []}`),
				)

				recs, err := hc.GetBooks(ctx, []uint32{9, 2, 9})

				Expect(err).NotTo(HaveOccurred())
				Expect(recs).To(HaveLen(1))
				Expect(recs[0].HardcoverID).To(Equal(uint32(2)))
				Expect(srv.vars[0]["ids"]).To(HaveLen(2))
			},
		)

		It("halves a batch Hardcover cuts off and retries each half", func() {
			calls := 0
			hc.client.Transport = mbRoundTripper(
				func(r *http.Request) (*http.Response, error) {
					raw, _ := io.ReadAll(r.Body)
					var req struct {
						Variables struct {
							IDs []uint32 `json:"ids"`
						} `json:"variables"`
					}
					Expect(json.Unmarshal(raw, &req)).To(Succeed())
					calls++
					if len(req.Variables.IDs) > 2 {
						return jsonResponse(
							200,
							`{"errors":[{"message":"statement timeout"}]}`,
						), nil
					}
					rows := []string{}
					for _, id := range req.Variables.IDs {
						rows = append(
							rows,
							fmt.Sprintf(
								`{"id": %d, "title": "T", "cached_contributors": []}`,
								id,
							),
						)
					}
					return jsonResponse(200, wrap(rows...)), nil
				},
			)

			recs, err := hc.GetBooks(ctx, []uint32{1, 2, 3, 4})

			Expect(err).NotTo(HaveOccurred())
			Expect(recs).To(HaveLen(4))
			Expect(calls).To(Equal(3))
		})

		It(
			"reads the makers of a book whose blob carried none through the join",
			func() {
				bare := `{"id": 5, "title": "Bare", "cached_contributors": [{"author": {"name": "No Id"}, "contribution": null}]}`
				join := `{"data":{"books":[{"id":5,"contributions":[
			  {"contribution":"Author","author":{"id":70,"name":"Joined Author","cached_image":"https://img/j.jpg"}},
			  {"contribution":"Narrator","author":{"id":71,"name":"Voice"}}]}]}}`
				hc.client.Transport = srv.transport(wrap(bare), join)

				recs, err := hc.GetBooks(ctx, []uint32{5})

				Expect(err).NotTo(HaveOccurred())
				Expect(srv.queries).To(HaveLen(2))
				Expect(srv.queries[1]).To(ContainSubstring("contributions"))
				Expect(recs[0].Credits).To(Equal([]BookCredit{{
					AuthorHardcoverID: 70, Name: "Joined Author",
					ImageURL: "https://img/j.jpg", Role: RoleAuthor,
				}}))
			},
		)

		It("surfaces a Hardcover error", func() {
			hc.client.Transport = srv.transport(`{"errors":[{"message":"boom"}]}`)
			_, err := hc.GetBooks(ctx, []uint32{1})
			Expect(err).To(MatchError(ContainSubstring("boom")))
		})

		It("gives up on a cancelled context instead of halving", func() {
			c, cancel := context.WithCancel(ctx)
			cancel()
			hc.client.Transport = srv.transport(wrap())
			_, err := hc.GetBooks(c, []uint32{1, 2, 3, 4})
			Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		})
	})

	Describe("GetSeries", func() {
		It("reads the skeleton in one request", func() {
			hc.client.Transport = srv.transport(`{"data":{"series":[{
			  "id": 3, "name": "One Piece", "description": "Pirates", "is_completed": false,
			  "primary_books_count": 105, "books_count": 120,
			  "author": {"id": 9, "name": "Eiichiro Oda", "cached_image": {"url": "https://img/oda.jpg"}},
			  "book_series": [
			    {"position": 2, "book_id": 202}, {"position": 1, "book_id": 201},
			    {"position": null, "book_id": 300}, {"position": 3, "book_id": 202}]}]}}`)

			sk, err := hc.GetSeries(ctx, 3)

			Expect(err).NotTo(HaveOccurred())
			Expect(srv.queries).To(HaveLen(1))
			Expect(srv.queries[0]).To(ContainSubstring("book_series("))
			Expect(sk.Name).To(Equal("One Piece"))
			Expect(sk.Completed).To(BeFalse())
			Expect(sk.PrimaryBooks).To(Equal(uint32(105)))
			Expect(sk.AuthorName).To(Equal("Eiichiro Oda"))
			Expect(sk.AuthorImageURL).To(Equal("https://img/oda.jpg"))
			// A book listed twice is one volume; an entry with no position is not.
			Expect(sk.Volumes).To(Equal([]SeriesVolumeRef{
				{Position: 2, BookHardcoverID: 202},
				{Position: 1, BookHardcoverID: 201},
			}))
		})

		It("returns nil for an unknown series", func() {
			hc.client.Transport = srv.transport(`{"data":{"series":[]}}`)
			sk, err := hc.GetSeries(ctx, 404)
			Expect(err).NotTo(HaveOccurred())
			Expect(sk).To(BeNil())
		})
	})

	Describe("BookByISBN", func() {
		It("resolves an edition ISBN to its book id", func() {
			hc.client.Transport = srv.transport(
				`{"data":{"editions":[{"book_id":42}]}}`,
			)
			id, err := hc.BookByISBN(ctx, "9780765311771")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(uint32(42)))
		})

		It("returns 0 without error when unknown", func() {
			hc.client.Transport = srv.transport(`{"data":{"editions":[]}}`)
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
			_, err := hc.SearchBooks(ctx, "x")
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
