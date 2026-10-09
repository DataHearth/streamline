package restapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	entbookedition "github.com/datahearth/streamline/ent/bookedition"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
)

var _ = Describe("Handler: books", Label("unit", "server", "books"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
		app.addMember("member")
	})

	send := func(method, path, key, body string) *http.Response {
		GinkgoHelper()
		var req *http.Request
		if body == "" {
			req = app.req(method, path, key, nil)
		} else {
			req = app.req(method, path, key, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		return app.do(req)
	}
	decode := func(resp *http.Response, out any) {
		GinkgoHelper()
		defer resp.Body.Close()
		Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
	}
	errorOf := func(resp *http.Response) Error {
		GinkgoHelper()
		var e Error
		decode(resp, &e)
		return e
	}

	now := time.Now()
	released := time.Date(2005, 4, 21, 0, 0, 0, 0, time.UTC)

	edition := func(id uint32, lang, format, title string) *ent.BookEdition {
		return &ent.BookEdition{
			ID: id, Language: lang, Title: title, Publisher: "Tor", Year: 2005,
			Format: entbookedition.Format(format),
		}
	}
	aBook := func() *ent.Book {
		en := edition(1, "en", "ebook", "Elantris")
		fr := edition(2, "fr", "ebook", "Elantris (fr)")
		fr.Original = false
		en.Original = true
		b := &ent.Book{
			ID: 7, HardcoverID: 70, Title: "Elantris", OriginalTitle: "Elantris VO",
			AuthorName: "Brandon Sanderson", Kind: "novel", Genre: "Fantasy",
			Overview: "A fallen city.", PreferredLanguage: "en",
			ReleaseDate: &released, ReleaseYear: new(uint16(2005)),
			RatingTenths: new(uint8(42)), CreateTime: now,
			EbookMonitored: true, EbookStatus: "wanted",
			AudiobookStatus: "skipped",
		}
		b.Edges.Editions = []*ent.BookEdition{fr, en}
		b.Edges.EbookEdition = en
		b.Edges.Contributions = []*ent.BookContribution{
			{
				Role: "author",
				Edges: ent.BookContributionEdges{
					Author: &ent.Author{ID: 3, Name: "Brandon Sanderson"},
				},
			},
			{
				Role: "translator",
				Edges: ent.BookContributionEdges{
					Author: &ent.Author{ID: 4, Name: "Someone"},
				},
			},
			{
				Role: "colorist",
				Edges: ent.BookContributionEdges{
					Author: &ent.Author{ID: 5, Name: "Colors"},
				},
			},
		}
		return b
	}

	Describe("GET /books", func() {
		It(
			"renders a merged page of books and series and passes the filters on",
			func() {
				app.books.EXPECT().
					List(mock.Anything, mock.MatchedBy(func(p book.ListParams) bool {
						filters := p.Status == "wanted" &&
							p.Author == "Brandon Sanderson"
						kinds := len(p.Kinds) == 2 && p.Kinds[0] == "bd"
						paging := p.Page == 2 && p.Limit == 5
						sorting := p.Sort == "title" && !p.Desc
						return filters && p.Format == "ebook" && kinds &&
							p.Query == "elan" && sorting && paging
					})).
					Return(book.ShelfPage{
						Rows: []db.ShelfRow{
							{
								Type:           "book",
								ID:             7,
								CoverID:        7,
								Title:          "Elantris",
								Author:         "BS",
								Kind:           "novel",
								Year:           2005,
								Status:         "downloading",
								AddedAt:        now,
								EbookState:     "downloading",
								AudiobookState: "unmonitored",
								QualityProfile: "std",
							},
							{
								Type:        "series",
								ID:          9,
								CoverID:     31,
								Title:       "One Piece",
								Author:      "Oda",
								Kind:        "manga",
								Status:      "wanted",
								AddedAt:     now,
								VolumesHave: 3,
								VolumesOut:  10,
							},
						},
						Total:    12,
						Progress: map[uint32]float64{7: 40},
					}, nil).
					Once()

				resp := send(
					http.MethodGet,
					"/api/v1/books?status=wanted&author=Brandon+Sanderson&format=ebook&kind=bd,comic&query=elan&sort=title&order=asc&page=2&limit=5",
					app.requestOnlyKey,
					"",
				)

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var page PaginatedShelf
				decode(resp, &page)
				Expect(page.Total).To(Equal(uint32(12)))
				Expect(page.Page).To(Equal(uint32(2)))
				Expect(page.Limit).To(Equal(uint16(5)))
				Expect(page.Items).To(HaveLen(2))
				b, s := page.Items[0], page.Items[1]
				Expect(b.Type).To(Equal(BookShelfTypeBook))
				Expect(*b.Year).To(Equal(uint16(2005)))
				Expect(*b.Progress).To(BeNumerically("==", 40))
				Expect(*b.Formats).To(Equal([]BookShelfFormat{
					{Format: BookFormatEbook, State: BookFormatStateDownloading},
					{Format: BookFormatAudiobook, State: BookFormatStateUnmonitored},
				}))
				Expect(*b.QualityProfile).To(Equal("std"))
				Expect(s.Type).To(Equal(BookShelfTypeSeries))
				Expect(s.CoverId).To(Equal(uint32(31)))
				Expect(*s.VolumesHave).To(Equal(uint32(3)))
				Expect(*s.VolumesOut).To(Equal(uint32(10)))
				Expect(s.Formats).To(BeNil())
				Expect(s.Year).To(BeNil())
			},
		)

		It(
			"sorts newest first by default and oldest first for the other keys",
			func() {
				app.books.EXPECT().
					List(mock.Anything, mock.MatchedBy(func(p book.ListParams) bool {
						return p.Sort == "added" && p.Desc && p.Page == 1 &&
							p.Limit == 20
					})).
					Return(book.ShelfPage{}, nil).
					Once()
				Expect(
					send(
						http.MethodGet,
						"/api/v1/books",
						app.requestOnlyKey,
						"",
					).StatusCode,
				).
					To(Equal(http.StatusOK))

				app.books.EXPECT().
					List(mock.Anything, mock.MatchedBy(func(p book.ListParams) bool {
						return p.Sort == "year" && !p.Desc
					})).
					Return(book.ShelfPage{}, nil).
					Once()
				Expect(
					send(
						http.MethodGet,
						"/api/v1/books?sort=year",
						app.requestOnlyKey,
						"",
					).StatusCode,
				).
					To(Equal(http.StatusOK))
			},
		)

		DescribeTable("answers 400 for",
			func(query string) {
				resp := send(
					http.MethodGet,
					"/api/v1/books?"+query,
					app.requestOnlyKey,
					"",
				)
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			},
			Entry("a limit over 100", "limit=101"),
			Entry("a zero limit", "limit=0"),
			Entry("a zero page", "page=0"),
			Entry("an unknown status", "status=lost"),
			Entry("an unknown kind", "kind=zine"),
			Entry("an unknown format", "format=paper"),
		)
	})

	Describe("GET /books/counts", func() {
		It("renders the faceted counts", func() {
			app.books.EXPECT().
				Counts(mock.Anything, mock.MatchedBy(func(p book.ListParams) bool {
					return p.Status == "wanted"
				})).
				Return(db.ShelfCounts{
					Total:       9,
					StatusTotal: 5,
					Available:   2,
					Wanted:      2,
					Downloading: 1,
					AuthorTotal: 5,
					Authors:     []db.ShelfAuthorCount{{Name: "Oda", Count: 3}},
					FormatTotal: 5,
					Ebook:       4,
					Audiobook:   1,
				}, nil).
				Once()

			resp := send(
				http.MethodGet,
				"/api/v1/books/counts?status=wanted",
				app.requestOnlyKey,
				"",
			)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var c BookCounts
			decode(resp, &c)
			Expect(c).To(Equal(BookCounts{
				Total: 9, StatusTotal: 5, Available: 2, Wanted: 2, Downloading: 1,
				AuthorTotal: 5, Authors: []BookAuthorCount{{Name: "Oda", Count: 3}},
				FormatTotal: 5, Ebook: 4, Audiobook: 1,
			}))
		})

		It("answers 400 for a bad filter", func() {
			Expect(
				send(
					http.MethodGet,
					"/api/v1/books/counts?kind=zine",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusBadRequest))
		})
	})

	Describe("lookup", func() {
		It("searches Hardcover and renders the hits", func() {
			ongoing := true
			app.books.EXPECT().Lookup(mock.Anything, "one piece", "all").
				Return([]book.LookupHit{
					{
						HardcoverID:  3,
						Type:         "series",
						Title:        "One Piece",
						Author:       "Oda",
						Volumes:      105,
						Ongoing:      &ongoing,
						AlreadyAdded: true,
						LibraryID:    9,
						CoverID:      31,
						Year:         1997,
					},
					{
						HardcoverID: 4,
						Type:        "book",
						Title:       "Guide",
						Author:      "X",
						Kind:        "novel",
						Year:        2001,
					},
				}, nil).Once()

			resp := send(
				http.MethodGet,
				"/api/v1/books/search?query=one+piece",
				app.requestOnlyKey,
				"",
			)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var list BookLookupList
			decode(resp, &list)
			Expect(list.Items).To(HaveLen(2))
			s, b := list.Items[0], list.Items[1]
			Expect(s.AlreadyAdded).To(BeTrue())
			Expect(*s.LibraryId).To(Equal(uint32(9)))
			Expect(*s.CoverId).To(Equal(uint32(31)))
			Expect(*s.Volumes).To(Equal(uint32(105)))
			Expect(s.Kind).To(BeNil())
			Expect(*b.Kind).To(Equal(BookKindNovel))
			Expect(b.LibraryId).To(BeNil())
		})

		It("restricts the lookup to one kind", func() {
			app.books.EXPECT().Lookup(mock.Anything, "x1", "series").
				Return(nil, nil).Once()
			Expect(
				send(
					http.MethodGet,
					"/api/v1/books/search?query=x1&type=series",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusOK))
		})

		DescribeTable("answers 400 for",
			func(query string) {
				Expect(
					send(
						http.MethodGet,
						"/api/v1/books/search?"+query,
						app.requestOnlyKey,
						"",
					).StatusCode,
				).
					To(Equal(http.StatusBadRequest))
			},
			Entry("a one-letter query", "query=x"),
			Entry("a missing query", "type=book"),
			Entry("an unknown type", "query=xx&type=comic"),
		)

		It(
			"maps a rate limit to 429 with Retry-After and a missing key to 503",
			func() {
				app.books.EXPECT().Lookup(mock.Anything, "xx", "all").
					Return(nil, &metadata.RateLimitedError{RetryAfter: 30 * time.Second}).
					Once()
				resp := send(
					http.MethodGet,
					"/api/v1/books/search?query=xx",
					app.requestOnlyKey,
					"",
				)
				Expect(resp.StatusCode).To(Equal(http.StatusTooManyRequests))
				Expect(resp.Header.Get("Retry-After")).To(Equal("30"))
				Expect(*errorOf(resp).Code).To(Equal("rate_limited"))

				app.books.EXPECT().Lookup(mock.Anything, "xx", "all").
					Return(nil, book.ErrNotConfigured).Once()
				Expect(
					send(
						http.MethodGet,
						"/api/v1/books/search?query=xx",
						app.requestOnlyKey,
						"",
					).StatusCode,
				).
					To(Equal(http.StatusServiceUnavailable))
			},
		)

		It("renders a detail with its extras", func() {
			app.books.EXPECT().LookupDetail(mock.Anything, "series", uint32(3)).
				Return(&book.LookupDetail{
					HardcoverID: 3,
					Type:        "series",
					Title:       "One Piece",
					Author:      "Oda",
					Kind:        "manga",
					Volumes:     105,
					Overview:    "Pirates",
					Genres:      []string{"Manga"},
					Editions: []book.LookupEdition{
						{
							Language:  "fr",
							Format:    "ebook",
							Publisher: "Glenat",
							Year:      2000,
							Original:  true,
						},
					},
					VolumeBookIDs: []uint32{1001, 1002},
				}, nil).Once()

			resp := send(
				http.MethodGet,
				"/api/v1/books/search/3?type=series",
				app.requestOnlyKey,
				"",
			)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var d BookLookupDetail
			decode(resp, &d)
			Expect(d.Overview).To(Equal(new("Pirates")))
			Expect(*d.VolumeBookIds).To(Equal([]uint32{1001, 1002}))
			Expect(*d.Editions).To(HaveLen(1))
			Expect(*(*d.Editions)[0].Original).To(BeTrue())
		})

		It("answers 404, 400, 429 and 503 on a detail", func() {
			app.books.EXPECT().LookupDetail(mock.Anything, "book", uint32(404)).
				Return(nil, book.ErrHardcoverNotFound).Once()
			Expect(
				send(
					http.MethodGet,
					"/api/v1/books/search/404?type=book",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))
			Expect(
				send(
					http.MethodGet,
					"/api/v1/books/search/4?type=comic",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusBadRequest))
			app.books.EXPECT().LookupDetail(mock.Anything, "book", uint32(5)).
				Return(nil, metadata.ErrRateLimited).Once()
			Expect(
				send(
					http.MethodGet,
					"/api/v1/books/search/5?type=book",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusTooManyRequests))
			app.books.EXPECT().LookupDetail(mock.Anything, "book", uint32(6)).
				Return(nil, metadata.ErrHardcoverUnauthorized).Once()
			Expect(
				send(
					http.MethodGet,
					"/api/v1/books/search/6?type=book",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusServiceUnavailable))
		})
	})

	Describe("POST /books", func() {
		It("adds a book for a member", func() {
			app.books.EXPECT().AddBook(mock.Anything, book.AddBookParams{
				HardcoverID: 70, Monitor: "ebook", QualityProfile: "std",
			}).Return(aBook(), nil).Once()
			app.books.EXPECT().
				Progress(mock.Anything, mock.Anything).
				Maybe().
				Return(nil)

			resp := send(http.MethodPost, "/api/v1/books", app.memberKey,
				`{"hardcover_id":70,"monitor":"ebook","quality_profile":"std"}`)

			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var b Book
			decode(resp, &b)
			Expect(b.Id).To(Equal(uint32(7)))
		})

		It("is closed to a request-only user", func() {
			Expect(send(http.MethodPost, "/api/v1/books", app.requestOnlyKey,
				`{"hardcover_id":70}`).StatusCode).To(Equal(http.StatusForbidden))
		})

		DescribeTable(
			"maps the service's refusals",
			func(err error, status int) {
				app.books.EXPECT().
					AddBook(mock.Anything, mock.Anything).
					Return(nil, err).
					Once()
				resp := send(
					http.MethodPost,
					"/api/v1/books",
					app.memberKey,
					`{"hardcover_id":70}`,
				)
				Expect(resp.StatusCode).To(Equal(status))
			},
			Entry("already in the library", book.ErrBookExists, http.StatusConflict),
			Entry(
				"unknown to Hardcover",
				book.ErrHardcoverNotFound,
				http.StatusNotFound,
			),
			Entry(
				"unknown monitor",
				book.ErrInvalidMonitor,
				http.StatusUnprocessableEntity,
			),
			Entry(
				"unknown profile",
				book.ErrUnknownProfile,
				http.StatusUnprocessableEntity,
			),
			Entry(
				"rate limited",
				metadata.ErrRateLimited,
				http.StatusTooManyRequests,
			),
			Entry("no key", book.ErrNotConfigured, http.StatusServiceUnavailable),
			Entry(
				"anything else",
				errors.New("boom"),
				http.StatusInternalServerError,
			),
		)
	})

	Describe("POST /books/series", func() {
		It("adds a series and answers hydrating while volumes are stubs", func() {
			refreshed := now
			s := &ent.BookSeries{
				ID:               9,
				HardcoverID:      3,
				Title:            "One Piece",
				AuthorName:       "Oda",
				Kind:             "manga",
				Monitor:          "all",
				Ongoing:          true,
				CreateTime:       now,
				EditionLanguage:  "en",
				EditionPublisher: "Viz",
			}
			one := &ent.Book{
				ID:              31,
				SeriesPosition:  new(1.0),
				LastRefreshedAt: &refreshed,
				EbookMonitored:  true,
				EbookStatus:     "wanted",
			}
			stub := &ent.Book{
				ID:             32,
				SeriesPosition: new(2.0),
				EbookMonitored: true,
				EbookStatus:    "wanted",
			}
			s.Edges.Volumes = []*ent.Book{one, stub}
			app.books.EXPECT().AddSeries(mock.Anything, book.AddSeriesParams{
				HardcoverID: 3, Monitor: "future",
			}).Return(s, nil).Once()

			resp := send(http.MethodPost, "/api/v1/books/series", app.memberKey,
				`{"hardcover_id":3,"monitor":"future"}`)

			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var out BookSeries
			decode(resp, &out)
			Expect(out.Hydrating).To(BeTrue())
			Expect(out.Edition).To(Equal("English · Viz"))
			Expect(out.Volumes).To(HaveLen(2))
			Expect(out.Volumes[1].Status).To(Equal(BookVolumeStatusWanted))
			// A stub is left out of the series' status while it hydrates.
			Expect(out.Status).To(Equal(BookItemStatusWanted))
		})

		DescribeTable(
			"maps the service's refusals",
			func(err error, status int) {
				app.books.EXPECT().
					AddSeries(mock.Anything, mock.Anything).
					Return(nil, err).
					Once()
				resp := send(
					http.MethodPost,
					"/api/v1/books/series",
					app.memberKey,
					`{"hardcover_id":3}`,
				)
				Expect(resp.StatusCode).To(Equal(status))
			},
			Entry(
				"already in the library",
				book.ErrSeriesExists,
				http.StatusConflict,
			),
			Entry(
				"unknown to Hardcover",
				book.ErrHardcoverNotFound,
				http.StatusNotFound,
			),
			Entry(
				"unknown policy",
				book.ErrInvalidMonitor,
				http.StatusUnprocessableEntity,
			),
			Entry(
				"rate limited",
				metadata.ErrRateLimited,
				http.StatusTooManyRequests,
			),
			Entry(
				"no key",
				metadata.ErrHardcoverKeyMissing,
				http.StatusServiceUnavailable,
			),
		)
	})

	Describe("GET /books/{id}", func() {
		It(
			"renders the book detail with makers, editions and the live progress",
			func() {
				b := aBook()
				b.EbookStatus = "downloading"
				app.books.EXPECT().
					GetBook(mock.Anything, uint32(7)).
					Return(b, nil).
					Once()
				app.books.EXPECT().Progress(mock.Anything, []uint32{7}).
					Return(map[uint32]map[string]float64{7: {"ebook": 61}}).Once()

				resp := send(
					http.MethodGet,
					"/api/v1/books/7",
					app.requestOnlyKey,
					"",
				)

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var out Book
				decode(resp, &out)
				Expect(out.Title).To(Equal("Elantris"))
				Expect(*out.OriginalTitle).To(Equal("Elantris VO"))
				Expect(out.Author).To(Equal("Brandon Sanderson"))
				Expect(out.FirstPublished).To(Equal(uint16(2005)))
				Expect(*out.Rating).To(BeNumerically("~", 4.2, 0.001))
				Expect(out.Monitor).To(Equal(BookMonitorEbook))
				Expect(out.Status).To(Equal(BookItemStatusDownloading))
				// Makers only: the translator is not listed, the colorist is.
				Expect(out.Contributors).To(HaveLen(2))
				Expect(
					out.Contributors[0],
				).To(Equal(BookCredit{AuthorId: 3, Name: "Brandon Sanderson", Role: BookRoleAuthor}))
				Expect(out.Contributors[1].Role).To(Equal(BookRoleColorist))
				Expect(out.Formats).To(HaveLen(2))
				Expect(out.Formats[0].Format).To(Equal(BookFormatEbook))
				Expect(out.Formats[0].State).To(Equal(BookFormatStateDownloading))
				Expect(*out.Formats[0].Progress).To(BeNumerically("==", 61))
				Expect(*out.Formats[0].EditionId).To(Equal(uint32(1)))
				Expect(out.Formats[1].State).To(Equal(BookFormatStateUnmonitored))
				Expect(out.Formats[1].EditionId).To(BeNil())
				// The preferred language first.
				Expect(out.Editions[0].Language).To(Equal("en"))
				Expect(*out.Editions[0].Original).To(BeTrue())
				Expect(out.Editions[1].Original).To(BeNil())
			},
		)

		It(
			"reports a file's container and size, and a replacement under way",
			func() {
				b := aBook()
				b.EbookReplacingLanguage = "fr"
				b.Edges.MediaFiles = []*ent.MediaFile{
					{BookKind: "ebook", Path: "/lib/Elantris.epub", Size: 100},
					{BookKind: "audiobook", Path: "/lib/a/02.mp3", Size: 5},
					{BookKind: "audiobook", Path: "/lib/a/01.mp3", Size: 6},
				}
				b.AudiobookMonitored, b.AudiobookStatus = true, "available"
				app.books.EXPECT().
					GetBook(mock.Anything, uint32(7)).
					Return(b, nil).
					Once()
				app.books.EXPECT().
					Progress(mock.Anything, mock.Anything).
					Return(nil).
					Once()

				resp := send(
					http.MethodGet,
					"/api/v1/books/7",
					app.requestOnlyKey,
					"",
				)

				var out Book
				decode(resp, &out)
				Expect(
					*out.Formats[0].File,
				).To(Equal(BookSlotFile{Container: "EPUB", Size: 100}))
				Expect(
					out.Formats[0].Replacing,
				).To(Equal(&BookReplacing{Language: "fr"}))
				audio := out.Formats[1].File
				Expect(audio.Container).To(Equal("MP3"))
				Expect(audio.Size).To(Equal(int64(11)))
				Expect(*audio.FileCount).To(Equal(uint32(2)))
			},
		)

		It("links a volume up to its series", func() {
			b := aBook()
			b.SeriesPosition = new(1.5)
			b.Edges.Series = &ent.BookSeries{ID: 9, Title: "One Piece"}
			app.books.EXPECT().
				GetBook(mock.Anything, uint32(7)).
				Return(b, nil).
				Once()
			app.books.EXPECT().
				Progress(mock.Anything, mock.Anything).
				Return(nil).
				Once()

			resp := send(http.MethodGet, "/api/v1/books/7", app.requestOnlyKey, "")

			var out Book
			decode(resp, &out)
			Expect(
				*out.Series,
			).To(Equal(BookSeriesRef{Id: 9, Title: "One Piece", Number: 1.5}))
		})

		It("answers 404", func() {
			app.books.EXPECT().
				GetBook(mock.Anything, uint32(404)).
				Return(nil, book.ErrBookNotFound).
				Once()
			Expect(
				send(
					http.MethodGet,
					"/api/v1/books/404",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /books/series/{id}", func() {
		It(
			"renders volumes, derived rating and since, contributors and edition choices",
			func() {
				up := now.Add(48 * time.Hour)
				s := &ent.BookSeries{
					ID:               9,
					HardcoverID:      3,
					Title:            "One Piece",
					OriginalTitle:    "ワンピース",
					AuthorName:       "Oda",
					Kind:             "manga",
					Monitor:          "all",
					Ongoing:          true,
					CreateTime:       now,
					EditionLanguage:  "fr",
					EditionPublisher: "Glenat",
					QualityProfile:   "comics",
				}
				refreshed := now
				mk := func(id uint32, pos float64, status string, tenths uint8, year uint16) *ent.Book {
					v := &ent.Book{
						ID:              id,
						SeriesPosition:  &pos,
						LastRefreshedAt: &refreshed,
						EbookMonitored:  true,
						EbookStatus:     entbook.EbookStatus(status),
						RatingTenths:    &tenths,
						ReleaseYear:     &year,
					}
					v.Edges.Editions = []*ent.BookEdition{
						{Language: "fr", Publisher: "Glenat", Format: "ebook"},
						{Language: "en", Publisher: "Viz", Format: "ebook"},
						{Language: "en", Publisher: "", Format: "ebook"},
					}
					return v
				}
				s.Edges.Volumes = []*ent.Book{
					mk(31, 1, "available", 40, 1999),
					mk(32, 2, "downloading", 45, 1997),
					mk(33, 3, "wanted", 0, 2001),
				}
				s.Edges.Volumes[2].ReleaseDate = &up
				s.Edges.Contributions = []*ent.BookContribution{
					{
						Role: "author",
						Edges: ent.BookContributionEdges{
							Author: &ent.Author{ID: 3, Name: "Oda"},
						},
					},
					{
						Role:     "translator",
						Language: "fr",
						Edges: ent.BookContributionEdges{
							Author: &ent.Author{ID: 4, Name: "Trad"},
						},
					},
				}
				app.books.EXPECT().
					GetSeries(mock.Anything, uint32(9)).
					Return(s, nil).
					Once()

				resp := send(
					http.MethodGet,
					"/api/v1/books/series/9",
					app.requestOnlyKey,
					"",
				)

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var out BookSeries
				decode(resp, &out)
				Expect(out.Status).To(Equal(BookItemStatusDownloading))
				Expect(*out.Rating).To(BeNumerically("~", 4.3, 0.001))
				Expect(out.Since).To(Equal(uint16(1997)))
				Expect(out.Hydrating).To(BeFalse())
				Expect(out.Edition).To(Equal("Français · Glenat"))
				Expect(
					out.Editions,
				).To(Equal([]string{"English · Viz", "Français · Glenat"}))
				Expect(out.Volumes).To(HaveLen(3))
				Expect(out.Volumes[0].Status).To(Equal(BookVolumeStatusAvailable))
				Expect(out.Volumes[1].Status).To(Equal(BookVolumeStatusDownloading))
				Expect(out.Volumes[2].Status).To(Equal(BookVolumeStatusUpcoming))
				Expect(out.Volumes[2].ReleaseDate).NotTo(BeNil())
				Expect(out.Contributors).To(HaveLen(2))
				Expect(*out.Contributors[1].Language).To(Equal("fr"))
				Expect(*out.QualityProfile).To(Equal("comics"))
			},
		)

		It("answers 404", func() {
			app.books.EXPECT().
				GetSeries(mock.Anything, uint32(404)).
				Return(nil, book.ErrSeriesNotFound).
				Once()
			Expect(
				send(
					http.MethodGet,
					"/api/v1/books/series/404",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))
		})
	})

	Describe("PATCH", func() {
		It("passes the changes to the service and renders the book", func() {
			app.books.EXPECT().
				PatchBook(mock.Anything, uint32(7), mock.MatchedBy(func(p book.PatchBookParams) bool {
					return *p.Monitor == "both" && *p.PreferredLanguage == "fr" &&
						*p.QualityProfile == "" && *p.Kind == "manga" &&
						*p.Format == "ebook" && *p.EditionID == 2
				})).
				Return(aBook(), nil).
				Once()
			app.books.EXPECT().
				Progress(mock.Anything, mock.Anything).
				Return(nil).
				Once()

			resp := send(
				http.MethodPatch,
				"/api/v1/books/7",
				app.memberKey,
				`{"monitor":"both","preferred_language":"fr","quality_profile":"","kind":"manga","format":"ebook","edition_id":2}`,
			)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		DescribeTable(
			"maps the service's refusals",
			func(err error, status int) {
				app.books.EXPECT().
					PatchBook(mock.Anything, uint32(7), mock.Anything).
					Return(nil, err).
					Once()
				Expect(
					send(
						http.MethodPatch,
						"/api/v1/books/7",
						app.memberKey,
						`{}`,
					).StatusCode,
				).
					To(Equal(status))
			},
			Entry("unknown book", book.ErrBookNotFound, http.StatusNotFound),
			Entry(
				"unknown monitor",
				book.ErrInvalidMonitor,
				http.StatusUnprocessableEntity,
			),
			Entry(
				"unknown profile",
				book.ErrUnknownProfile,
				http.StatusUnprocessableEntity,
			),
			Entry(
				"a lone format",
				book.ErrEditionMismatch,
				http.StatusUnprocessableEntity,
			),
			Entry(
				"a foreign edition",
				book.ErrUnknownEdition,
				http.StatusUnprocessableEntity,
			),
			Entry(
				"bad language",
				book.ErrInvalidLanguage,
				http.StatusUnprocessableEntity,
			),
		)

		It("patches a series", func() {
			app.books.EXPECT().
				PatchSeries(mock.Anything, uint32(9), mock.MatchedBy(func(p book.PatchSeriesParams) bool {
					return *p.Monitor == "future" &&
						*p.Edition == "Français · Glenat"
				})).
				Return(&ent.BookSeries{ID: 9, Title: "One Piece", Kind: "manga", Monitor: "future"}, nil).
				Once()

			resp := send(http.MethodPatch, "/api/v1/books/series/9", app.memberKey,
				`{"monitor":"future","edition":"Français · Glenat"}`)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			app.books.EXPECT().PatchSeries(mock.Anything, uint32(9), mock.Anything).
				Return(nil, book.ErrUnknownEdition).Once()
			Expect(
				send(
					http.MethodPatch,
					"/api/v1/books/series/9",
					app.memberKey,
					`{"edition":"x"}`,
				).StatusCode,
			).
				To(Equal(http.StatusUnprocessableEntity))
		})
	})

	Describe("DELETE", func() {
		It("deletes a book, with its files when asked", func() {
			app.books.EXPECT().
				DeleteBook(mock.Anything, uint32(7), true).
				Return(nil).
				Once()
			Expect(
				send(
					http.MethodDelete,
					"/api/v1/books/7?delete_files=true",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNoContent))
			app.books.EXPECT().
				DeleteBook(mock.Anything, uint32(7), false).
				Return(nil).
				Once()
			Expect(
				send(
					http.MethodDelete,
					"/api/v1/books/7",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNoContent))
		})

		It("refuses a volume with a coded 409", func() {
			app.books.EXPECT().
				DeleteBook(mock.Anything, uint32(31), false).
				Return(book.ErrSeriesVolume).
				Once()
			resp := send(http.MethodDelete, "/api/v1/books/31", app.memberKey, "")
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			Expect(*errorOf(resp).Code).To(Equal("series_volume"))
		})

		It("answers 404 and deletes a series", func() {
			app.books.EXPECT().
				DeleteBook(mock.Anything, uint32(404), false).
				Return(book.ErrBookNotFound).
				Once()
			Expect(
				send(
					http.MethodDelete,
					"/api/v1/books/404",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))
			app.books.EXPECT().
				DeleteSeries(mock.Anything, uint32(9), true).
				Return(nil).
				Once()
			Expect(
				send(
					http.MethodDelete,
					"/api/v1/books/series/9?delete_files=true",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNoContent))
		})
	})

	Describe("refresh", func() {
		It("refreshes a book and a series", func() {
			app.books.EXPECT().
				RefreshBook(mock.Anything, uint32(7)).
				Return(aBook(), nil).
				Once()
			app.books.EXPECT().
				Progress(mock.Anything, mock.Anything).
				Return(nil).
				Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/7/refresh-metadata",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusOK))

			app.books.EXPECT().RefreshSeries(mock.Anything, uint32(9)).
				Return(&ent.BookSeries{ID: 9, Title: "S", Kind: "manga", Monitor: "all"}, nil).
				Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/series/9/refresh-metadata",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusOK))
		})

		DescribeTable(
			"maps the failures",
			func(err error, status int) {
				app.books.EXPECT().
					RefreshBook(mock.Anything, uint32(7)).
					Return(nil, err).
					Once()
				Expect(
					send(
						http.MethodPost,
						"/api/v1/books/7/refresh-metadata",
						app.memberKey,
						"",
					).StatusCode,
				).
					To(Equal(status))
			},
			Entry("unknown book", book.ErrBookNotFound, http.StatusNotFound),
			Entry(
				"gone from Hardcover",
				book.ErrHardcoverNotFound,
				http.StatusNotFound,
			),
			Entry(
				"rate limited",
				metadata.ErrRateLimited,
				http.StatusTooManyRequests,
			),
			Entry("no key", book.ErrNotConfigured, http.StatusServiceUnavailable),
		)
	})

	Describe("manual search", func() {
		It("renders flat releases with their slot, container and verdict", func() {
			app.books.EXPECT().SearchBookReleases(mock.Anything, uint32(7), "").
				Return([]book.ReleaseResult{
					{
						Title:    "Elantris EPUB",
						Download: "magnet:?xt=urn:btih:abc",
						Seeders:  4,
						Slot:     "ebook", Container: "EPUB", Score: 400,
					},
					{
						Title:       "Elantris Unabridged M4B 32kbps",
						Download:    "magnet:?xt=urn:btih:def",
						Slot:        "audiobook",
						Container:   "M4B",
						BitrateKbps: 32,
						Rejected:    true,
						Reason:      "32 kbps is below the profile's minimum of 64",
					},
				}, nil).Once()

			resp := send(
				http.MethodPost,
				"/api/v1/books/7/search",
				app.memberKey,
				"",
			)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var list SearchResultList
			decode(resp, &list)
			Expect(list.Items).To(HaveLen(2))
			ebook, audio := list.Items[0], list.Items[1]
			Expect(*ebook.Slot).To(Equal(BookFormatEbook))
			Expect(*ebook.Source).To(Equal("EPUB"))
			Expect(ebook.Rejected).To(BeNil())
			Expect(*ebook.Score).To(Equal(400))
			Expect(*ebook.MatchedFormats).To(BeEmpty())
			Expect(*audio.Slot).To(Equal(BookFormatAudiobook))
			Expect(*audio.Rejected).To(BeTrue())
			Expect(
				*audio.RejectReason,
			).To(ContainSubstring("below the profile's minimum"))
			Expect(*audio.BitrateKbps).To(Equal(uint32(32)))
			Expect(ebook.DownloadUrl).To(HavePrefix("slr1."))
		})

		It("searches one slot and maps the failures", func() {
			app.books.EXPECT().
				SearchBookReleases(mock.Anything, uint32(7), "audiobook").
				Return(nil, book.ErrNoQualityProfile).
				Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/7/search?kind=audiobook",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			Expect(*errorOf(resp).Code).To(Equal("no_quality_profile"))

			app.books.EXPECT().SearchBookReleases(mock.Anything, uint32(404), "").
				Return(nil, book.ErrBookNotFound).Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/404/search",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))

			app.books.EXPECT().SearchBookReleases(mock.Anything, uint32(7), "paper").
				Return(nil, book.ErrInvalidSlotKind).Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/7/search?kind=paper",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusBadRequest))
		})

		It("is closed to a request-only user", func() {
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/7/search",
					app.requestOnlyKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusForbidden))
		})
	})

	Describe("grab", func() {
		item := func() string {
			return `{"title":"Elantris EPUB","download_url":"magnet:?xt=urn:btih:abc","size":10,"seeders":3` + `,"slot":"ebook"}`
		}

		It(
			"hands the release, the slot and the replace flag to the service",
			func() {
				app.books.EXPECT().
					GrabBookRelease(mock.Anything, uint32(7), mock.MatchedBy(func(p book.GrabParams) bool {
						return p.Slot == "ebook" && p.Kind == "ebook" &&
							p.ReplaceExisting &&
							p.Result.Title == "Elantris EPUB" &&
							p.Result.Download == "magnet:?xt=urn:btih:abc"
					})).
					Return(nil).
					Once()

				resp := send(
					http.MethodPost,
					"/api/v1/books/7/grab?kind=ebook",
					app.memberKey,
					strings.TrimSuffix(
						item(),
						"}",
					)+`,"replace_existing":true}`,
				)

				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			},
		)

		It("takes the slot from the body alone", func() {
			app.books.EXPECT().
				GrabBookRelease(mock.Anything, uint32(7), mock.MatchedBy(func(p book.GrabParams) bool {
					return p.Slot == "ebook" && p.Kind == ""
				})).
				Return(nil).
				Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/7/grab",
					app.memberKey,
					item(),
				).StatusCode,
			).
				To(Equal(http.StatusAccepted))
		})

		It("answers 400 when the query and the body name different slots", func() {
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/7/grab?kind=audiobook",
					app.memberKey,
					item(),
				).StatusCode,
			).
				To(Equal(http.StatusBadRequest))
		})

		DescribeTable("maps a refused grab to grab_rejected",
			func(err error) {
				app.books.EXPECT().
					GrabBookRelease(mock.Anything, uint32(7), mock.Anything).
					Return(err).
					Once()
				resp := send(
					http.MethodPost,
					"/api/v1/books/7/grab",
					app.memberKey,
					item(),
				)
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				Expect(*errorOf(resp).Code).To(Equal("grab_rejected"))
			},
			Entry("a forged slot", book.ErrSlotMismatch),
			Entry("a book not read yet", book.ErrNotHydrated),
			Entry("an untrusted source", download.ErrUntrustedSource),
			Entry("a full client", download.ErrClientFull),
			Entry("an unsafe name", download.ErrUnsafeTorrentName),
		)

		It("maps the other failures", func() {
			app.books.EXPECT().
				GrabBookRelease(mock.Anything, uint32(7), mock.Anything).
				Return(book.ErrNoQualityProfile).
				Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/7/grab",
				app.memberKey,
				item(),
			)
			Expect(*errorOf(resp).Code).To(Equal("no_quality_profile"))

			app.books.EXPECT().
				GrabBookRelease(mock.Anything, uint32(404), mock.Anything).
				Return(book.ErrBookNotFound).
				Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/404/grab",
					app.memberKey,
					item(),
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))
		})

		It("refuses a body without a release and a handle it cannot open", func() {
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/7/grab",
					app.memberKey,
					`{"title":"x"}`,
				).StatusCode,
			).
				To(Equal(http.StatusUnprocessableEntity))
			resp := send(
				http.MethodPost,
				"/api/v1/books/7/grab",
				app.memberKey,
				`{"title":"x","download_url":"slr1.not-a-handle","size":1,"seeders":1}`,
			)
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			Expect(*errorOf(resp).Code).To(Equal("grab_rejected"))
		})
	})

	Describe("search now", func() {
		It("reports how many slots and volumes will be searched", func() {
			app.books.EXPECT().
				SearchNowBook(mock.Anything, uint32(7), "ebook").
				Return(uint32(1), nil).
				Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/7/search-now?kind=ebook",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			var out BookSearchAccepted
			decode(resp, &out)
			Expect(out.Queued).To(Equal(uint32(1)))

			app.books.EXPECT().
				SearchNowSeries(mock.Anything, uint32(9)).
				Return(uint32(4), nil).
				Once()
			resp = send(
				http.MethodPost,
				"/api/v1/books/series/9/search-now",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			decode(resp, &out)
			Expect(out.Queued).To(Equal(uint32(4)))
		})

		It("answers 400 for an unknown slot kind", func() {
			app.books.EXPECT().
				SearchNowBook(mock.Anything, uint32(7), "paper").
				Return(uint32(0), book.ErrInvalidSlotKind).
				Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/7/search-now?kind=paper",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusBadRequest))
		})

		It("answers 404", func() {
			app.books.EXPECT().
				SearchNowBook(mock.Anything, uint32(404), "").
				Return(uint32(0), book.ErrBookNotFound).
				Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/404/search-now",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))
			app.books.EXPECT().
				SearchNowSeries(mock.Anything, uint32(404)).
				Return(uint32(0), book.ErrSeriesNotFound).
				Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/series/404/search-now",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))
		})
	})

	Describe("rename", func() {
		It("renders the plan of a book and of a series", func() {
			plan := library.RenamePlan{Operations: []library.RenameOperation{
				{MediaFileID: 1, From: "/a", To: "/b"},
			}}
			app.books.EXPECT().
				RenameBook(mock.Anything, uint32(7), true).
				Return(plan, nil).
				Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/7/rename?preview=true",
				app.memberKey,
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var bp BookRenamePlan
			decode(resp, &bp)
			Expect(bp).To(Equal(BookRenamePlan{
				BookId: 7,
				Operations: []RenameOperation{
					{MediaFileId: 1, From: "/a", To: "/b"},
				},
			}))

			app.books.EXPECT().
				RenameSeries(mock.Anything, uint32(9), false).
				Return(plan, nil).
				Once()
			resp = send(
				http.MethodPost,
				"/api/v1/books/series/9/rename",
				app.memberKey,
				"",
			)
			var sp BookSeriesRenamePlan
			decode(resp, &sp)
			Expect(sp.SeriesId).To(Equal(uint32(9)))
			Expect(sp.Operations).To(HaveLen(1))
		})

		It("answers 404", func() {
			app.books.EXPECT().RenameBook(mock.Anything, uint32(404), false).
				Return(library.RenamePlan{}, book.ErrBookNotFound).Once()
			Expect(
				send(
					http.MethodPost,
					"/api/v1/books/404/rename",
					app.memberKey,
					"",
				).StatusCode,
			).
				To(Equal(http.StatusNotFound))
		})
	})
})
