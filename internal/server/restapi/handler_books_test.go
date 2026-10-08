package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/author"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Handler: Books", Label("unit", "server", "books"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		configtest.SetupFile(map[string]any{
			"ebook_quality_profiles": []map[string]any{
				{
					"name":    "epub-first",
					"formats": []string{"epub"},
					"cutoff":  "epub",
				},
			},
			"ebook_quality_default_profile": "epub-first",
			"audiobook_quality_profiles": []map[string]any{
				{"name": "m4b-first", "formats": []string{"m4b"}, "cutoff": "m4b"},
			},
			"audiobook_quality_default_profile": "m4b-first",
		})
		app = newAPIKeyApp()
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

	seededAuthor := func() *ent.Author {
		release := time.Date(2010, 8, 31, 0, 0, 0, 0, time.UTC)
		return &ent.Author{
			ID:                      7,
			HardcoverID:             204214,
			Name:                    "Brandon Sanderson",
			SortName:                "Sanderson, Brandon",
			Monitored:               true,
			MonitorPolicy:           author.MonitorPolicyAll,
			WantKinds:               author.WantKindsBoth,
			EbookQualityProfile:     "epub-first",
			AudiobookQualityProfile: "m4b-first",
			Edges: ent.AuthorEdges{Books: []*ent.Book{{
				ID:                 3,
				HardcoverID:        1,
				Title:              "The Way of Kings",
				ReleaseDate:        &release,
				SeriesName:         "The Stormlight Archive",
				SeriesPosition:     "1",
				EbookMonitored:     true,
				EbookStatus:        entbook.EbookStatusAvailable,
				AudiobookMonitored: true,
				AudiobookStatus:    entbook.AudiobookStatusWanted,
				Edges: ent.BookEdges{MediaFiles: []*ent.MediaFile{
					{BookKind: mediafile.BookKindEbook},
					{BookKind: mediafile.BookKindEbook},
					{BookKind: mediafile.BookKindAudiobook},
				}},
			}}},
		}
	}

	Describe("SearchBookAuthors", func() {
		It("flags authors already in the library", func() {
			app.metadataBook.EXPECT().SearchAuthors(mock.Anything, "sanderson").
				Return([]metadata.AuthorResult{
					{HardcoverID: 1, Name: "Brandon Sanderson", BooksCount: 90},
					{
						HardcoverID: 2,
						Name:        "Other",
						BooksCount:  3,
						ImageURL:    "http://img",
					},
				}, nil).Once()
			app.store.EXPECT().FindAuthorByHardcoverID(mock.Anything, uint32(1)).
				Return(&ent.Author{ID: 1}, nil).Once()
			app.store.EXPECT().FindAuthorByHardcoverID(mock.Anything, uint32(2)).
				Return(nil, nil).Once()

			resp := send(
				http.MethodGet,
				"/api/v1/books/search?query=sanderson",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out BookAuthorSearchResultList
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.Items).To(HaveLen(2))
			Expect(out.Items[0].AlreadyAdded).To(BeTrue())
			Expect(out.Items[0].BooksCount).To(Equal(uint32(90)))
			Expect(out.Items[1].AlreadyAdded).To(BeFalse())
			Expect(*out.Items[1].ImageUrl).To(Equal("http://img"))
		})

		It("answers 503 when no provider is configured", func() {
			r := chi.NewRouter()
			r.Use(app.identityMiddleware())
			Mount(r, New(Deps{}))
			ts := httptest.NewServer(r)
			DeferCleanup(ts.Close)

			req, err := http.NewRequest(
				http.MethodGet,
				ts.URL+"/api/v1/books/search?query=x",
				nil,
			)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("X-API-Key", app.adminKey)
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
			var out Error
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.Message).To(ContainSubstring("hardcover"))
		})
	})

	Describe("ListBookAuthors", func() {
		DescribeTable("rejects out-of-range pagination",
			func(query string) {
				resp := send(
					http.MethodGet,
					"/api/v1/books/authors?"+query,
					app.adminKey,
					"",
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			},
			Entry("limit=0", "limit=0"),
			Entry("limit=101", "limit=101"),
			Entry("page=0", "page=0"),
		)

		It("returns book_count without the nested books", func() {
			app.books.EXPECT().List(mock.Anything, uint16(2), uint16(10)).
				Return([]*ent.Author{seededAuthor()}, uint32(11), nil).Once()
			resp := send(
				http.MethodGet,
				"/api/v1/books/authors?page=2&limit=10",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out PaginatedBookAuthors
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.Total).To(Equal(uint32(11)))
			Expect(out.Page).To(Equal(uint32(2)))
			Expect(out.Limit).To(Equal(uint16(10)))
			Expect(out.Items).To(HaveLen(1))
			Expect(out.Items[0].BookCount).To(Equal(uint32(1)))
			Expect(out.Items[0].Books).To(BeNil())
		})
	})

	Describe("AddBookAuthor", func() {
		It("adds the author and answers 201", func() {
			app.books.EXPECT().Add(mock.Anything, book.AddParams{
				HardcoverID:         204214,
				Monitored:           true,
				MonitorPolicy:       "future",
				WantKinds:           "both",
				EbookQualityProfile: "epub-first",
			}).Return(seededAuthor(), nil).Once()

			resp := send(
				http.MethodPost,
				"/api/v1/books/authors",
				app.adminKey,
				`{"hardcover_id":204214,"monitor_policy":"future","want_kinds":"both","ebook_quality_profile":"epub-first"}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			var out BookAuthor
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.Name).To(Equal("Brandon Sanderson"))
		})

		It("answers 409 for an author already in the library", func() {
			app.books.EXPECT().Add(mock.Anything, mock.Anything).
				Return(nil, book.ErrAuthorExists).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/authors",
				app.adminKey,
				`{"hardcover_id":1}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("answers 503 when the provider is not configured", func() {
			app.books.EXPECT().Add(mock.Anything, mock.Anything).
				Return(nil, book.ErrNotConfigured).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/authors",
				app.adminKey,
				`{"hardcover_id":1}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})

		It("answers 503 when Hardcover rejects the key", func() {
			app.books.EXPECT().Add(mock.Anything, mock.Anything).
				Return(nil, metadata.ErrHardcoverUnauthorized).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/authors",
				app.adminKey,
				`{"hardcover_id":1}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})

		DescribeTable(
			"answers 422 for an invalid value",
			func(body string) {
				resp := send(
					http.MethodPost,
					"/api/v1/books/authors",
					app.adminKey,
					body,
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			},
			Entry("monitor_policy", `{"hardcover_id":1,"monitor_policy":"nope"}`),
			Entry("want_kinds", `{"hardcover_id":1,"want_kinds":"nope"}`),
			Entry(
				"ebook profile",
				`{"hardcover_id":1,"ebook_quality_profile":"nope"}`,
			),
			Entry(
				"audiobook profile",
				`{"hardcover_id":1,"audiobook_quality_profile":"nope"}`,
			),
		)

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/books/authors",
				app.requestOnlyKey,
				`{"hardcover_id":1}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("GetBookAuthor", func() {
		It("returns the books with both slots and a file count per kind", func() {
			app.books.EXPECT().Get(mock.Anything, uint32(7)).
				Return(seededAuthor(), nil).Once()
			resp := send(http.MethodGet, "/api/v1/books/authors/7", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out BookAuthor
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.WantKinds).To(Equal(BookAuthorWantKinds("both")))
			Expect(out.Books).NotTo(BeNil())
			b := (*out.Books)[0]
			Expect(b.ReleaseDate.Format("2006-01-02")).To(Equal("2010-08-31"))
			Expect(b.Ebook).To(Equal(BookSlot{
				Monitored: true, Status: "available", FileCount: 2,
			}))
			Expect(b.Audiobook).To(Equal(BookSlot{
				Monitored: true, Status: "wanted", FileCount: 1,
			}))
		})

		It("404s for an unknown id", func() {
			app.books.EXPECT().Get(mock.Anything, uint32(99)).
				Return(nil, book.ErrAuthorNotFound).Once()
			resp := send(
				http.MethodGet,
				"/api/v1/books/authors/99",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("PatchBookAuthor", func() {
		It("updates only the supplied fields then re-reads", func() {
			monitored := false
			ebook := "epub-first"
			app.books.EXPECT().
				UpdateAuthor(mock.Anything, uint32(7), db.UpdateAuthorParams{
					Monitored:           &monitored,
					EbookQualityProfile: &ebook,
				}).
				Return(nil).
				Once()
			app.books.EXPECT().Get(mock.Anything, uint32(7)).
				Return(seededAuthor(), nil).Once()

			resp := send(http.MethodPatch, "/api/v1/books/authors/7", app.adminKey,
				`{"monitored":false,"ebook_quality_profile":"epub-first"}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		DescribeTable("answers 422 for an invalid value",
			func(body string) {
				resp := send(
					http.MethodPatch,
					"/api/v1/books/authors/7",
					app.adminKey,
					body,
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			},
			Entry("ebook profile", `{"ebook_quality_profile":"nope"}`),
			Entry("audiobook profile", `{"audiobook_quality_profile":"nope"}`),
			Entry("monitor_policy", `{"monitor_policy":"nope"}`),
			Entry("want_kinds", `{"want_kinds":"nope"}`),
		)

		It("404s for an unknown author", func() {
			app.books.EXPECT().
				UpdateAuthor(mock.Anything, uint32(99), mock.Anything).
				Return(book.ErrAuthorNotFound).
				Once()
			resp := send(
				http.MethodPatch,
				"/api/v1/books/authors/99",
				app.adminKey,
				`{"monitored":true}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPatch,
				"/api/v1/books/authors/7",
				app.requestOnlyKey,
				`{"monitored":true}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("DeleteBookAuthor", func() {
		It("passes delete_files through and answers 204", func() {
			app.books.EXPECT().
				Delete(mock.Anything, uint32(7), true).
				Return(nil).
				Once()
			resp := send(
				http.MethodDelete,
				"/api/v1/books/authors/7?delete_files=true",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("404s for an unknown author", func() {
			app.books.EXPECT().Delete(mock.Anything, uint32(99), false).
				Return(book.ErrAuthorNotFound).Once()
			resp := send(
				http.MethodDelete,
				"/api/v1/books/authors/99",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodDelete,
				"/api/v1/books/authors/7",
				app.requestOnlyKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("RefreshBookAuthor", func() {
		It("returns the refreshed author", func() {
			app.books.EXPECT().RefreshOne(mock.Anything, uint32(7)).
				Return(seededAuthor(), nil).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/authors/7/refresh",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("404s for an unknown author", func() {
			app.books.EXPECT().RefreshOne(mock.Anything, uint32(99)).
				Return(nil, book.ErrAuthorNotFound).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/authors/99/refresh",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("answers 503 when the provider is not configured", func() {
			app.books.EXPECT().RefreshOne(mock.Anything, uint32(7)).
				Return(nil, book.ErrNotConfigured).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/authors/7/refresh",
				app.adminKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})
	})

	Describe("GetBook", func() {
		It("returns the book with its author id and slots", func() {
			b := seededAuthor().Edges.Books[0]
			b.Edges.Author = &ent.Author{ID: 7}
			app.books.EXPECT().
				GetBook(mock.Anything, uint32(3)).
				Return(b, nil).
				Once()
			resp := send(http.MethodGet, "/api/v1/books/3", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out Book
			Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
			Expect(out.AuthorId).To(Equal(uint32(7)))
			Expect(out.Ebook.FileCount).To(Equal(uint32(2)))
		})

		It("404s for an unknown book", func() {
			app.books.EXPECT().GetBook(mock.Anything, uint32(99)).
				Return(nil, book.ErrBookNotFound).Once()
			resp := send(http.MethodGet, "/api/v1/books/99", app.adminKey, "")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("PatchBook", func() {
		It("sets each supplied slot flag once then re-reads", func() {
			app.books.EXPECT().SetBookSlot(mock.Anything, uint32(3), "ebook", false).
				Return(nil).Once()
			app.books.EXPECT().
				SetBookSlot(mock.Anything, uint32(3), "audiobook", true).
				Return(nil).
				Once()
			app.books.EXPECT().GetBook(mock.Anything, uint32(3)).
				Return(seededAuthor().Edges.Books[0], nil).Once()
			resp := send(http.MethodPatch, "/api/v1/books/3", app.adminKey,
				`{"ebook_monitored":false,"audiobook_monitored":true}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("only touches the supplied slot", func() {
			app.books.EXPECT().
				SetBookSlot(mock.Anything, uint32(3), "audiobook", false).
				Return(nil).
				Once()
			app.books.EXPECT().GetBook(mock.Anything, uint32(3)).
				Return(seededAuthor().Edges.Books[0], nil).Once()
			resp := send(
				http.MethodPatch,
				"/api/v1/books/3",
				app.adminKey,
				`{"audiobook_monitored":false}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("404s for an unknown book", func() {
			app.books.EXPECT().SetBookSlot(mock.Anything, uint32(99), "ebook", true).
				Return(book.ErrBookNotFound).Once()
			resp := send(
				http.MethodPatch,
				"/api/v1/books/99",
				app.adminKey,
				`{"ebook_monitored":true}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPatch,
				"/api/v1/books/3",
				app.requestOnlyKey,
				`{"ebook_monitored":true}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	It("lets a request-only caller read", func() {
		app.books.EXPECT().
			Get(mock.Anything, uint32(7)).
			Return(seededAuthor(), nil).
			Once()
		resp := send(
			http.MethodGet,
			"/api/v1/books/authors/7",
			app.requestOnlyKey,
			"",
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	Describe("SearchBookReleases", func() {
		for _, kind := range []string{"ebook", "audiobook"} {
			It(
				"returns the ranked "+kind+" releases, rejected rows flagged",
				func() {
					accepted := indexer.SearchResult{
						Title:    "Dune " + kind,
						Download: "magnet:?xt=urn:btih:aaaa",
						Seeders:  5,
					}
					rejected := indexer.SearchResult{
						Title:    "Dune pdf",
						Download: "magnet:?xt=urn:btih:bbbb",
					}
					app.books.EXPECT().
						SearchBookReleases(mock.Anything, uint32(3), kind).
						Return([]book.ReleaseResult{
							{
								SearchResult: accepted,
								Format:       "epub",
								Score:        30,
							},
							{
								SearchResult: rejected,
								Format:       "pdf",
								Rejected:     true,
								Reason:       "format not accepted by the quality profile",
							},
						}, nil).
						Once()
					resp := send(
						http.MethodPost,
						"/api/v1/books/3/search?kind="+kind,
						app.memberKey,
						"",
					)
					defer resp.Body.Close()
					Expect(resp.StatusCode).To(Equal(http.StatusOK))

					var body BookReleaseList
					Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
					Expect(body.Items).To(HaveLen(2))
					Expect(body.Items[0].Format).To(Equal("epub"))
					Expect(body.Items[0].Release.Title).To(Equal("Dune " + kind))
					Expect(body.Items[0].Release.Score).To(HaveValue(Equal(30)))
					Expect(body.Items[0].Release.Rejected).To(HaveValue(BeFalse()))
					Expect(body.Items[0].Release.RejectReason).To(BeNil())
					Expect(body.Items[0].Release.DownloadUrl).To(HavePrefix("slr1."))
					Expect(body.Items[1].Release.Rejected).To(HaveValue(BeTrue()))
					Expect(body.Items[1].Release.RejectReason).To(HaveValue(
						Equal("format not accepted by the quality profile"),
					))
				},
			)
		}

		DescribeTable("400s a missing or invalid kind without calling the service",
			func(query string) {
				resp := send(
					http.MethodPost,
					"/api/v1/books/3/search"+query,
					app.memberKey,
					"",
				)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			},
			Entry("missing", ""),
			Entry("unknown", "?kind=comic"),
		)

		It("404s for an unknown book", func() {
			app.books.EXPECT().
				SearchBookReleases(mock.Anything, uint32(99), "ebook").
				Return(nil, book.ErrBookNotFound).
				Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/99/search?kind=ebook",
				app.memberKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("422s with no_quality_profile", func() {
			app.books.EXPECT().SearchBookReleases(mock.Anything, uint32(3), "ebook").
				Return(nil, book.ErrNoQualityProfile).Once()
			resp := send(
				http.MethodPost,
				"/api/v1/books/3/search?kind=ebook",
				app.memberKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			var body struct {
				Code string `json:"code"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Code).To(Equal("no_quality_profile"))
		})

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/books/3/search?kind=ebook",
				app.requestOnlyKey,
				"",
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("GrabBookRelease", func() {
		const plain = `{"title":"Dune","download_url":"magnet:?xt=urn:btih:aaaa","size":1,"seeders":1}`
		grab := func(kind, body string) *http.Response {
			GinkgoHelper()
			return send(
				http.MethodPost,
				"/api/v1/books/3/grab?kind="+kind,
				app.memberKey,
				body,
			)
		}
		code := func(resp *http.Response) string {
			GinkgoHelper()
			var body struct {
				Code string `json:"code"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			return body.Code
		}

		for _, kind := range []string{"ebook", "audiobook"} {
			It("dispatches a "+kind+" grab with the slot kind", func() {
				app.books.EXPECT().
					GrabBookRelease(mock.Anything, uint32(3), mock.MatchedBy(
						func(p book.GrabParams) bool {
							return p.Kind == kind &&
								p.Result.Download == "magnet:?xt=urn:btih:aaaa"
						},
					)).
					Return(nil).Once()
				resp := grab(kind, plain)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			})
		}

		It("opens a sealed handle", func() {
			app.books.EXPECT().
				GrabBookRelease(mock.Anything, uint32(3), mock.MatchedBy(
					func(p book.GrabParams) bool {
						return p.Result.Download == "magnet:?xt=urn:btih:aaaa"
					},
				)).
				Return(nil).Once()
			resp := grab("ebook", `{"title":"Dune","download_url":"`+
				sealReleaseLink(
					"magnet:?xt=urn:btih:aaaa",
				)+`","size":1,"seeders":1}`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
		})

		It("400s an invalid kind", func() {
			resp := grab("comic", plain)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("tells the caller to search again on a bad handle", func() {
			resp := grab(
				"ebook",
				`{"title":"Dune","download_url":"slr1.bm90LWEtaGFuZGxl","size":1,"seeders":1}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			Expect(code(resp)).To(Equal("grab_rejected"))
		})

		It("422s an empty title or url without a code", func() {
			resp := grab(
				"ebook",
				`{"title":"","download_url":"","size":1,"seeders":1}`,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			Expect(code(resp)).To(BeEmpty())
		})

		DescribeTable("maps a refused grab to grab_rejected",
			func(sentinel error) {
				app.books.EXPECT().
					GrabBookRelease(mock.Anything, uint32(3), mock.Anything).
					Return(fmt.Errorf("grab book: %w", sentinel)).Once()
				resp := grab("ebook", plain)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				Expect(code(resp)).To(Equal("grab_rejected"))
			},
			Entry("untrusted source", download.ErrUntrustedSource),
			Entry("client full", download.ErrClientFull),
			Entry("unsafe torrent name", download.ErrUnsafeTorrentName),
		)

		DescribeTable(
			"maps a service sentinel",
			func(sentinel error, status int, wantCode string) {
				app.books.EXPECT().
					GrabBookRelease(mock.Anything, uint32(3), mock.Anything).
					Return(fmt.Errorf("grab book: %w", sentinel)).Once()
				resp := grab("ebook", plain)
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(status))
				if wantCode != "" {
					Expect(code(resp)).To(Equal(wantCode))
				}
			},
			Entry("unknown book", book.ErrBookNotFound, http.StatusNotFound, ""),
			Entry(
				"invalid slot kind",
				book.ErrInvalidSlotKind,
				http.StatusBadRequest,
				"",
			),
			Entry("no quality profile", book.ErrNoQualityProfile,
				http.StatusUnprocessableEntity, "no_quality_profile"),
			Entry("anything else", errors.New("boom"),
				http.StatusInternalServerError, ""),
		)

		It("answers 403 to a request-only caller", func() {
			resp := send(
				http.MethodPost,
				"/api/v1/books/3/grab?kind=ebook",
				app.requestOnlyKey,
				plain,
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
})
