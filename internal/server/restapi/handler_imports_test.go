package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscan "github.com/datahearth/streamline/ent/importscan"
	entimportscanalbum "github.com/datahearth/streamline/ent/importscanalbum"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/media/book"
)

var _ = Describe("Handler: Import scan shows",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		It("GET /library/imports/{id}/shows lists series rows", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(5)).
				Return(&ent.ImportScan{ID: 5}, nil).Once()
			app.store.EXPECT().ListImportScanShows(mock.Anything,
				mock.MatchedBy(func(p db.ListImportScanShowsParams) bool {
					return p.ScanID == 5
				})).Return([]*ent.ImportScanShow{
				{
					ID: 1, FolderPath: "/tv/Breaking Bad",
					ParsedTitle:    "Breaking Bad",
					Classification: entimportscanshow.ClassificationConfirmed,
					FileCount:      2,
					Decision:       entimportscanshow.DecisionPending,
					Outcome:        entimportscanshow.OutcomePending,
				},
			}, 1, nil).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/shows", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body ImportScanShowList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Total).To(Equal(uint32(1)))
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].FolderPath).To(Equal("/tv/Breaking Bad"))
		})

		It(
			"GET /library/imports/{id}/shows returns an empty page for a known, empty scan",
			func() {
				app.store.EXPECT().FindImportScan(mock.Anything, uint32(5)).
					Return(&ent.ImportScan{ID: 5}, nil).Once()
				app.store.EXPECT().ListImportScanShows(mock.Anything,
					mock.MatchedBy(func(p db.ListImportScanShowsParams) bool {
						return p.ScanID == 5
					})).Return([]*ent.ImportScanShow{}, 0, nil).Once()

				resp := app.do(app.req(http.MethodGet,
					"/api/v1/library/imports/5/shows", app.adminKey, nil))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body ImportScanShowList
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Total).To(BeZero())
				Expect(body.Items).To(BeEmpty())
			},
		)

		It("404s GET /library/imports/{id}/shows for an unknown scan", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(999999)).
				Return(nil, &ent.NotFoundError{}).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/999999/shows", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It(
			"PATCH /library/imports/{id}/shows/{showId} records the decision",
			func() {
				app.store.EXPECT().UpdateImportScanShowDecision(mock.Anything,
					uint32(5), uint32(1), entimportscanshow.DecisionAccept,
					mock.Anything).
					Return(nil).Once()
				app.store.EXPECT().
					FindImportScanShow(mock.Anything, uint32(5), uint32(1)).
					Return(&ent.ImportScanShow{
						ID: 1, FolderPath: "/tv/Breaking Bad",
						Classification: entimportscanshow.ClassificationConfirmed,
						Decision:       entimportscanshow.DecisionAccept,
						Outcome:        entimportscanshow.OutcomePending,
					}, nil).
					Once()

				resp := app.do(app.req(http.MethodPatch,
					"/api/v1/library/imports/5/shows/1", app.adminKey,
					strings.NewReader(`{"decision":"accept"}`)))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body ImportScanShow
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Decision).To(Equal(ImportScanShowDecision("accept")))
			},
		)

		// An unknown show id and a show id owned by another scan reach the store
		// as the same scan-scoped UPDATE and come back as the same sentinel. No
		// FindImportScanShow expectation: the strict mock fails the spec if the
		// handler still reads back after the write reported a miss.
		It(
			"404s PATCH /library/imports/{id}/shows/{showId} for a rejected pair",
			func() {
				app.store.EXPECT().UpdateImportScanShowDecision(mock.Anything,
					uint32(5), uint32(999), entimportscanshow.DecisionSkip,
					(*uint32)(nil)).
					Return(db.ErrImportScanShowNotFound).Once()

				resp := app.do(app.req(http.MethodPatch,
					"/api/v1/library/imports/5/shows/999", app.adminKey,
					strings.NewReader(`{"decision":"skip"}`)))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				var body struct {
					Message string `json:"message"`
				}
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Message).To(Equal("import scan show not found"))
			},
		)

		It("forbids non-admins", func() {
			app.addMember("")
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/shows", app.memberKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

var _ = Describe("Handler: Import scan files",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		It(
			"404s PATCH /library/imports/{id}/files/{fileId} for an unknown file",
			func() {
				app.bulkImports.EXPECT().UpdateFileDecision(mock.Anything,
					uint32(5), uint32(999), entimportscanfile.DecisionSkip,
					(*uint32)(nil)).
					Return(nil, bulkimport.ErrScanFileNotFound).Once()

				resp := app.do(app.req(http.MethodPatch,
					"/api/v1/library/imports/5/files/999", app.adminKey,
					strings.NewReader(`{"decision":"skip"}`)))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			},
		)

		It("404s GET /library/imports/{id}/files for an unknown scan", func() {
			app.bulkImports.EXPECT().Files(mock.Anything,
				mock.MatchedBy(func(p bulkimport.FilesParams) bool {
					return p.ScanID == 999999
				})).
				Return(nil, 0, bulkimport.ErrScanNotFound).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/999999/files", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

var _ = Describe("Handler: Import scan lookup",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		// The body used to echo the raw ent error ("ent: import_scan not
		// found"), leaking ORM vocabulary to API clients.
		It("404s GET /library/imports/{id} with a client-safe message", func() {
			app.bulkImports.EXPECT().Get(mock.Anything, uint32(999999)).
				Return(nil, &ent.NotFoundError{}).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/999999", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			var body struct {
				Message string `json:"message"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Message).To(Equal("scan not found"))
		})
	})

var _ = Describe("Handler: Import scan albums",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		It("GET /library/imports/{id}/albums lists album rows", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(5)).
				Return(&ent.ImportScan{ID: 5}, nil).Once()
			app.store.EXPECT().ListImportScanAlbums(mock.Anything,
				mock.MatchedBy(func(p db.ListImportScanAlbumsParams) bool {
					return p.ScanID == 5
				})).Return([]*ent.ImportScanAlbum{
				{
					ID: 1, FolderPath: "/music/Nirvana/Nevermind",
					TaggedArtist:   "Nirvana",
					TaggedAlbum:    "Nevermind",
					Classification: entimportscanalbum.ClassificationConfirmed,
					FileCount:      12,
					Decision:       entimportscanalbum.DecisionPending,
					Outcome:        entimportscanalbum.OutcomePending,
				},
			}, 1, nil).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/albums", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body ImportScanAlbumList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Total).To(Equal(uint32(1)))
			Expect(body.Items).To(HaveLen(1))
			Expect(body.Items[0].FolderPath).To(Equal("/music/Nirvana/Nevermind"))
			Expect(*body.Items[0].TaggedAlbum).To(Equal("Nevermind"))
		})

		It("returns an empty page for a known, empty scan", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(5)).
				Return(&ent.ImportScan{ID: 5}, nil).Once()
			app.store.EXPECT().ListImportScanAlbums(mock.Anything,
				mock.MatchedBy(func(p db.ListImportScanAlbumsParams) bool {
					return p.ScanID == 5
				})).Return([]*ent.ImportScanAlbum{}, 0, nil).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/albums", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body ImportScanAlbumList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Total).To(BeZero())
			Expect(body.Items).To(BeEmpty())
		})

		It("404s GET /library/imports/{id}/albums for an unknown scan", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(999999)).
				Return(nil, &ent.NotFoundError{}).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/999999/albums", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It(
			"PATCH /library/imports/{id}/albums/{albumId} records the decision",
			func() {
				mbid := "rg-1"
				app.store.EXPECT().UpdateImportScanAlbumDecision(mock.Anything,
					uint32(5), uint32(1), entimportscanalbum.DecisionAccept, &mbid).
					Return(nil).Once()
				app.store.EXPECT().
					FindImportScanAlbum(mock.Anything, uint32(5), uint32(1)).
					Return(&ent.ImportScanAlbum{
						ID:                       1,
						FolderPath:               "/music/Nirvana/Nevermind",
						Classification:           entimportscanalbum.ClassificationAmbiguous,
						Decision:                 entimportscanalbum.DecisionAccept,
						DecisionReleaseGroupMbid: "rg-1",
						Outcome:                  entimportscanalbum.OutcomePending,
					}, nil).Once()

				resp := app.do(app.req(
					http.MethodPatch,
					"/api/v1/library/imports/5/albums/1",
					app.adminKey,
					strings.NewReader(
						`{"decision":"accept","release_group_mbid":"rg-1"}`,
					),
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body ImportScanAlbum
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Decision).To(Equal(ImportScanAlbumDecision("accept")))
				Expect(*body.DecisionReleaseGroupMbid).To(Equal("rg-1"))
			},
		)

		It("404s PATCH for a rejected scan and album pair", func() {
			app.store.EXPECT().UpdateImportScanAlbumDecision(mock.Anything,
				uint32(5), uint32(999), entimportscanalbum.DecisionSkip,
				(*string)(nil)).
				Return(db.ErrImportScanAlbumNotFound).Once()

			resp := app.do(app.req(http.MethodPatch,
				"/api/v1/library/imports/5/albums/999", app.adminKey,
				strings.NewReader(`{"decision":"skip"}`)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("forbids non-admins", func() {
			app.addMember("")
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/albums", app.memberKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("401s with an invalid token", func() {
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/albums", "invalid-token", nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})
	})

var _ = Describe("Handler: Import start for music",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		start := func() *http.Response {
			GinkgoHelper()
			return app.do(app.req(http.MethodPost, "/api/v1/library/imports",
				app.adminKey, strings.NewReader(
					`{"source_path":"/music","mode":"in_place","kind":"music"}`)))
		}

		It("starts a music scan", func() {
			app.bulkImports.EXPECT().StartScan(mock.Anything,
				mock.MatchedBy(func(p bulkimport.StartScanParams) bool {
					return p.Kind == entimportscan.KindMusic
				})).
				Return(&ent.ImportScan{ID: 3, Kind: entimportscan.KindMusic}, nil).
				Once()
			resp := start()
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})

		DescribeTable("422s a refused scan",
			func(refusal error) {
				app.bulkImports.EXPECT().StartScan(mock.Anything, mock.Anything).
					Return(nil, refusal).Once()
				resp := start()
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			},
			Entry("rename mode", bulkimport.ErrRenameUnsupported),
			Entry("unsupported kind", bulkimport.ErrUnsupportedKind),
		)
	})

var _ = Describe("Handler: Import scan books",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		It("GET /library/imports/{id}/books lists book rows", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(5)).
				Return(&ent.ImportScan{ID: 5}, nil).Once()
			app.store.EXPECT().ListImportScanBooks(mock.Anything,
				mock.MatchedBy(func(p db.ListImportScanBooksParams) bool {
					return p.ScanID == 5
				})).Return([]*ent.ImportScanBook{
				{
					ID: 1, FilePaths: []string{"/books/Elantris.epub"},
					Slot:           entimportscanbook.SlotEbook,
					ParsedTitle:    "Elantris",
					Classification: entimportscanbook.ClassificationConfirmed,
					Decision:       entimportscanbook.DecisionPending,
					Outcome:        entimportscanbook.OutcomePending,
				},
			}, 1, nil).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/books", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body ImportScanBookList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Total).To(Equal(uint32(1)))
			Expect(body.Items).To(HaveLen(1))
			Expect(
				body.Items[0].FilePaths,
			).To(Equal([]string{"/books/Elantris.epub"}))
			Expect(body.Items[0].Slot).To(Equal(ImportScanBookSlot("ebook")))
		})

		It("returns an empty page for a known, empty scan", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(5)).
				Return(&ent.ImportScan{ID: 5}, nil).Once()
			app.store.EXPECT().ListImportScanBooks(mock.Anything,
				mock.MatchedBy(func(p db.ListImportScanBooksParams) bool {
					return p.ScanID == 5
				})).Return([]*ent.ImportScanBook{}, 0, nil).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/books", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body ImportScanBookList
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Total).To(BeZero())
			Expect(body.Items).To(BeEmpty())
		})

		It("404s GET /library/imports/{id}/books for an unknown scan", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(999999)).
				Return(nil, &ent.NotFoundError{}).Once()

			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/999999/books", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It(
			"PATCH /library/imports/{id}/books/{bookId} records the decision",
			func() {
				hc := uint32(42)
				app.store.EXPECT().UpdateImportScanBookDecision(mock.Anything,
					uint32(5), uint32(1), entimportscanbook.DecisionAccept, &hc).
					Return(nil).Once()
				app.store.EXPECT().
					FindImportScanBook(mock.Anything, uint32(5), uint32(1)).
					Return(&ent.ImportScanBook{
						ID:                      1,
						FilePaths:               []string{"/books/a.epub"},
						Slot:                    entimportscanbook.SlotEbook,
						Classification:          entimportscanbook.ClassificationAmbiguous,
						Decision:                entimportscanbook.DecisionAccept,
						DecisionBookHardcoverID: 42,
						Outcome:                 entimportscanbook.OutcomePending,
					}, nil).Once()

				resp := app.do(app.req(
					http.MethodPatch,
					"/api/v1/library/imports/5/books/1",
					app.adminKey,
					strings.NewReader(
						`{"decision":"accept","book_hardcover_id":42}`,
					),
				))
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body ImportScanBook
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Decision).To(Equal(ImportScanBookDecision("accept")))
				Expect(*body.DecisionBookHardcoverId).To(Equal(uint32(42)))
			},
		)

		It("404s PATCH for a rejected scan and book pair", func() {
			app.store.EXPECT().UpdateImportScanBookDecision(mock.Anything,
				uint32(5), uint32(999), entimportscanbook.DecisionSkip,
				(*uint32)(nil)).
				Return(db.ErrImportScanBookNotFound).Once()

			resp := app.do(app.req(http.MethodPatch,
				"/api/v1/library/imports/5/books/999", app.adminKey,
				strings.NewReader(`{"decision":"skip"}`)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("forbids non-admins", func() {
			app.addMember("")
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/books", app.memberKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("401s with an invalid token", func() {
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/books", "invalid-token", nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("starts a book scan", func() {
			app.bulkImports.EXPECT().StartScan(mock.Anything,
				mock.MatchedBy(func(p bulkimport.StartScanParams) bool {
					return p.Kind == entimportscan.KindBook
				})).
				Return(&ent.ImportScan{ID: 3, Kind: entimportscan.KindBook}, nil).
				Once()
			resp := app.do(app.req(http.MethodPost, "/api/v1/library/imports",
				app.adminKey, strings.NewReader(
					`{"source_path":"/books","mode":"in_place","kind":"book"}`)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})

		It("503s a book scan start without a Hardcover key", func() {
			app.bulkImports.EXPECT().StartScan(mock.Anything, mock.Anything).
				Return(nil, book.ErrNotConfigured).Once()
			resp := app.do(app.req(http.MethodPost, "/api/v1/library/imports",
				app.adminKey, strings.NewReader(
					`{"source_path":"/books","mode":"in_place","kind":"book"}`)))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
			var body struct {
				Message string `json:"message"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Message).To(Equal(errBookProviderMissing.Error()))
		})

		It("503s a book scan commit without a Hardcover key", func() {
			app.bulkImports.EXPECT().Commit(mock.Anything, uint32(3)).
				Return(book.ErrNotConfigured).Once()
			resp := app.do(app.req(http.MethodPost,
				"/api/v1/library/imports/3/commit", app.adminKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
			var body struct {
				Message string `json:"message"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Message).To(Equal(errBookProviderMissing.Error()))
		})
	})
