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

	Describe("LookupBooks series derivation fields", func() {
		It("reads alternative titles, series ids and the featured series", func() {
			hc.client.Transport = srv.transport(
				`{"data":{"search":{"results":{"hits":[
			  {"document":{"id":"188628","title":"Foundation","author_names":["Isaac Asimov"],
			   "alternative_titles":["Fondation","Fundação"],
			   "series_ids":[5450,"10331",60563],
			   "featured_series":{"position":1,"series":{"id":5450,"name":"Foundation","slug":"foundation","primary_books_count":7,"books_count":9}}}},
			  {"document":{"id":"5","title":"Standalone"}}]}}}}`,
			)

			hits, err := hc.LookupBooks(ctx, "Fondation")

			Expect(err).NotTo(HaveOccurred())
			Expect(hits).To(HaveLen(2))
			Expect(
				hits[0].AlternativeTitles,
			).To(Equal([]string{"Fondation", "Fundação"}))
			Expect(hits[0].SeriesIDs).To(Equal([]uint32{5450, 10331, 60563}))
			Expect(hits[0].FeaturedSeries).To(Equal(&LookupSeriesRef{
				ID: 5450, Name: "Foundation", Slug: "foundation",
				Position: 1, PrimaryBooks: 7, Books: 9,
			}))
			Expect(hits[1].AlternativeTitles).To(BeEmpty())
			Expect(hits[1].SeriesIDs).To(BeEmpty())
			Expect(hits[1].FeaturedSeries).To(BeNil())
		})
	})

	Describe("LookupSeries", func() {
		It("reads the slug", func() {
			hc.client.Transport = srv.transport(
				`{"data":{"search":{"results":{"hits":[
				  {"document":{"id":"5450","name":"Foundation","slug":"foundation","primary_books_count":7}}]}}}}`,
			)

			hits, err := hc.LookupSeries(ctx, "foundation")

			Expect(err).NotTo(HaveOccurred())
			Expect(hits[0].Slug).To(Equal("foundation"))
		})
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
			"asks for the book language's editions and keeps them beside the most popular ones",
			func() {
				hc.client.Transport = srv.transport(wrap(`{
  "id": 2, "title": "1984", "cached_contributors": [],
  "digital": [
    {"id": 201, "title": "1984", "users_count": 900, "reading_format_id": 4,
     "language": {"code2": "en"}, "publisher": {"name": "Penguin"}}
  ],
  "localDigital": [
    {"id": 202, "title": "1984", "users_count": 0, "reading_format_id": 2,
     "language": {"code2": "fr"}, "publisher": {"name": "SAGA Egmont"}}
  ],
  "localPhysical": [
    {"id": 203, "title": "1984", "users_count": 11, "reading_format_id": 1,
     "language": {"code2": "fr"}, "publisher": {"name": "Gallimard"}}
  ]
}`))

				recs, err := hc.GetBooks(ctx, []uint32{2})

				Expect(err).NotTo(HaveOccurred())
				Expect(srv.queries[0]).To(ContainSubstring("localDigital: editions"))
				Expect(
					srv.queries[0],
				).To(ContainSubstring("localPhysical: editions"))
				Expect(srv.vars[0]["lang"]).NotTo(BeEmpty())
				formats := map[uint32]string{}
				for _, e := range recs[0].Editions {
					formats[e.HardcoverID] = e.Format
				}
				// The French paperback stands in for the ebook French lacks.
				Expect(formats).To(Equal(map[uint32]string{
					201: FormatEbook, 202: FormatAudiobook, 203: FormatEbook,
				}))
			},
		)

		It("dates a book by its original edition, not a fragment's year", func() {
			hc.client.Transport = srv.transport(wrap(`{
  "id": 9, "title": "Exhalation", "release_date": "2014-01-01", "release_year": 2014,
  "digital": [],
  "physical": [
    {"id": 1, "title": "Exhalation", "release_date": "2019-05-07", "release_year": 2019, "pages": 208, "reading_format_id": 1, "language": {"code2": "en"}},
    {"id": 2, "title": "Exhalation", "release_date": "2019-09-01", "release_year": 2019, "pages": 464, "reading_format_id": 1, "language": {"code2": "fr"}},
    {"id": 3, "title": "Exhalation", "release_date": "2020-01-01", "release_year": 2020, "pages": 380, "reading_format_id": 1, "language": {"code2": "de"}}
  ],
  "earliest": [
    {"id": 4, "title": "Exhalation", "release_date": "2014-01-01", "release_year": 2014, "pages": 12, "reading_format_id": 1, "language": {"code2": "he"}}
  ]
}`))

			recs, err := hc.GetBooks(ctx, []uint32{9})

			Expect(err).NotTo(HaveOccurred())
			Expect(recs[0].ReleaseYear).To(Equal(uint16(2019)))
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

		It("keeps the books without credits when the join query fails", func() {
			bare := `{"id": 5, "title": "Bare", "cached_contributors": [{"author": {"name": "No Id"}, "contribution": null}]}`
			hc.client.Transport = srv.transport(
				wrap(bare),
				`{"errors":[{"message":"field not found"}]}`,
			)

			recs, err := hc.GetBooks(ctx, []uint32{5})

			Expect(err).NotTo(HaveOccurred())
			Expect(recs).To(HaveLen(1))
			Expect(recs[0].Credits).To(BeEmpty())
		})

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

	Describe("GetSeriesLookup", func() {
		const body = `{"data":{"series":[
		  {"id": 60563, "name": "Greater Foundation Universe", "slug": "greater-foundation-universe",
		   "primary_books_count": 15, "books_count": 20,
		   "author": {"id": 9, "name": "Isaac Asimov"},
		   "book_series": [{"position": 1, "book": {"id": 11, "users_count": 50, "compilation": false, "cached_image": {"url": "https://img/11.jpg"}}}]},
		  {"id": 5450, "name": "Foundation", "slug": "foundation",
		   "primary_books_count": 7, "books_count": 9,
		   "author": {"id": 9, "name": "Isaac Asimov"},
		   "book_series": [
		     {"position": 1, "book": {"id": 21, "users_count": 10, "compilation": true, "cached_image": "https://img/21.jpg"}},
		     {"position": 1, "book": {"id": 22, "users_count": null, "compilation": null, "cached_image": null}},
		     {"position": 2, "book": null}]}]}}`

		It("reads many series in one request, ordered by id", func() {
			hc.client.Transport = srv.transport(body)

			recs, err := hc.GetSeriesLookup(ctx, []uint32{60563, 5450, 5450})

			Expect(err).NotTo(HaveOccurred())
			Expect(srv.queries).To(HaveLen(1))
			Expect(srv.queries[0]).To(ContainSubstring("book_series("))
			Expect(srv.queries[0]).To(ContainSubstring("slug"))
			Expect(
				srv.vars[0]["ids"],
			).To(Equal([]any{float64(5450), float64(60563)}))
			Expect(recs).To(HaveLen(2))
			Expect(recs[0]).To(Equal(SeriesLookupRecord{
				HardcoverID: 5450, Name: "Foundation", Slug: "foundation",
				PrimaryBooks: 7, Books: 9, AuthorName: "Isaac Asimov",
				Volumes: []SeriesLookupVolume{
					{
						Position:        1,
						BookHardcoverID: 21,
						UsersCount:      10,
						Compilation:     true,
						ImageURL:        "https://img/21.jpg",
					},
					{Position: 1, BookHardcoverID: 22},
				},
			}))
			Expect(recs[1].HardcoverID).To(Equal(uint32(60563)))
			Expect(recs[1].Slug).To(Equal("greater-foundation-universe"))
			Expect(recs[1].Volumes).To(Equal([]SeriesLookupVolume{
				{
					Position:        1,
					BookHardcoverID: 11,
					UsersCount:      50,
					ImageURL:        "https://img/11.jpg",
				},
			}))
		})

		It("memoises per sorted id set", func() {
			hc.client.Transport = srv.transport(body)

			_, err := hc.GetSeriesLookup(ctx, []uint32{60563, 5450})
			Expect(err).NotTo(HaveOccurred())
			_, err = hc.GetSeriesLookup(ctx, []uint32{5450, 60563})
			Expect(err).NotTo(HaveOccurred())
			Expect(srv.queries).To(HaveLen(1))

			_, err = hc.GetSeriesLookup(ctx, []uint32{5450})
			Expect(err).NotTo(HaveOccurred())
			Expect(srv.queries).To(HaveLen(2))
		})

		It("sends nothing for no ids", func() {
			hc.client.Transport = srv.transport(body)
			recs, err := hc.GetSeriesLookup(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(recs).To(BeEmpty())
			Expect(srv.queries).To(BeEmpty())
		})

		It("surfaces a Hardcover error without memoising it", func() {
			hc.client.Transport = mbRoundTripper(
				func(*http.Request) (*http.Response, error) {
					return jsonResponse(200, `{"errors":[{"message":"boom"}]}`), nil
				},
			)
			_, err := hc.GetSeriesLookup(ctx, []uint32{1})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetSeries", func() {
		It("reads the slug and each volume's readers and compilation flag", func() {
			hc.client.Transport = srv.transport(`{"data":{"series":[{
			  "id": 3, "name": "One Piece", "slug": "one-piece",
			  "book_series": [
			    {"position": 1, "book_id": 201, "book": {"users_count": 40, "compilation": true}},
			    {"position": 2, "book_id": 202, "book": null}]}]}}`)

			sk, err := hc.GetSeries(ctx, 3)

			Expect(err).NotTo(HaveOccurred())
			Expect(srv.queries[0]).To(ContainSubstring("slug"))
			Expect(srv.queries[0]).To(ContainSubstring("users_count"))
			Expect(sk.Slug).To(Equal("one-piece"))
			Expect(sk.Volumes).To(Equal([]SeriesVolumeRef{
				{
					Position:        1,
					BookHardcoverID: 201,
					UsersCount:      40,
					Compilation:     true,
				},
				{Position: 2, BookHardcoverID: 202},
			}))
		})

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
