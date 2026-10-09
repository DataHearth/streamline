package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscanbook "github.com/datahearth/streamline/ent/importscanbook"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe("Handler: Import scan books",
	Label("unit", "server", "imports", "books"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		It(
			"GET /library/imports/{id}/books lists the scanned books with their candidates",
			func() {
				app.store.EXPECT().FindImportScan(mock.Anything, uint32(5)).
					Return(&ent.ImportScan{ID: 5}, nil).Once()
				app.store.EXPECT().ListImportScanBooks(mock.Anything,
					mock.MatchedBy(func(p db.ListImportScanBooksParams) bool {
						return p.ScanID == 5 &&
							p.Classification == entimportscanbook.ClassificationAmbiguous &&
							p.Query == "elan" &&
							p.Limit == 10 &&
							p.Offset == 10
					})).Return([]*ent.ImportScanBook{{
					ID: 1, FilePaths: []string{"/books/a.epub", "/books/a.mobi"},
					Slot:            entimportscanbook.SlotEbook,
					ParsedTitle:     "Elantris",
					ParsedAuthor:    "Brandon Sanderson",
					ParsedIsbn:      "9780765311771",
					Classification:  entimportscanbook.ClassificationAmbiguous,
					BookHardcoverID: 1,
					Candidates: []schema.ScannedBookCandidate{
						{
							BookHardcoverID: 1,
							Title:           "Elantris",
							Author:          "Brandon Sanderson",
							Year:            2005,
						},
						{BookHardcoverID: 2, Title: "Elantris II"},
					},
					Decision: entimportscanbook.DecisionPending,
					Outcome:  entimportscanbook.OutcomePending,
				}}, 11, nil).Once()

				resp := app.do(app.req(
					http.MethodGet,
					"/api/v1/library/imports/5/books?classification=ambiguous&q=elan&limit=10&page=2",
					app.adminKey,
					nil,
				))
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				var body ImportScanBookList
				Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
				Expect(body.Total).To(Equal(uint32(11)))
				Expect(body.Items).To(HaveLen(1))
				row := body.Items[0]
				Expect(row.FilePaths).To(HaveLen(2))
				Expect(*row.ParsedTitle).To(Equal("Elantris"))
				Expect(*row.BookHardcoverId).To(Equal(uint32(1)))
				Expect(*row.Candidates).To(HaveLen(2))
				Expect((*row.Candidates)[0]).To(Equal(ImportScanBookCandidate{
					BookHardcoverId: 1, Title: "Elantris",
					Author: new("Brandon Sanderson"), Year: new(uint16(2005)),
				}))
				Expect((*row.Candidates)[1].Author).To(BeNil())
			},
		)

		It("404s for an unknown scan and 400s for a bad page or limit", func() {
			app.store.EXPECT().FindImportScan(mock.Anything, uint32(9)).
				Return(nil, &ent.NotFoundError{}).Once()
			Expect(app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/9/books", app.adminKey, nil)).StatusCode).
				To(Equal(http.StatusNotFound))
			Expect(app.do(app.req(
				http.MethodGet,
				"/api/v1/library/imports/5/books?limit=101",
				app.adminKey,
				nil,
			)).StatusCode).
				To(Equal(http.StatusBadRequest))
			Expect(app.do(app.req(
				http.MethodGet,
				"/api/v1/library/imports/5/books?page=0",
				app.adminKey,
				nil,
			)).StatusCode).
				To(Equal(http.StatusBadRequest))
		})

		It("PATCH records a pick and answers the row", func() {
			hc := uint32(2)
			app.store.EXPECT().UpdateImportScanBookDecision(mock.Anything,
				uint32(
					5,
				), uint32(1), entimportscanbook.DecisionAccept, &hc).
				Return(nil).Once()
			app.store.EXPECT().
				FindImportScanBook(mock.Anything, uint32(5), uint32(1)).
				Return(&ent.ImportScanBook{
					ID:                      1,
					Slot:                    entimportscanbook.SlotEbook,
					Decision:                entimportscanbook.DecisionAccept,
					DecisionBookHardcoverID: 2,
				}, nil).
				Once()

			req := app.req(
				http.MethodPatch,
				"/api/v1/library/imports/5/books/1",
				app.adminKey,
				strings.NewReader(`{"decision":"accept","book_hardcover_id":2}`),
			)
			req.Header.Set("Content-Type", "application/json")
			resp := app.do(req)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var row ImportScanBook
			Expect(json.NewDecoder(resp.Body).Decode(&row)).To(Succeed())
			Expect(row.Decision).To(Equal(ImportScanBookDecisionAccept))
			Expect(*row.DecisionBookHardcoverId).To(Equal(uint32(2)))
		})

		It("PATCH 404s for a book of another scan", func() {
			app.store.EXPECT().UpdateImportScanBookDecision(mock.Anything,
				uint32(
					5,
				), uint32(1), entimportscanbook.DecisionSkip, (*uint32)(nil)).
				Return(db.ErrImportScanBookNotFound).Once()
			req := app.req(
				http.MethodPatch,
				"/api/v1/library/imports/5/books/1",
				app.adminKey,
				strings.NewReader(`{"decision":"skip"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			Expect(app.do(req).StatusCode).To(Equal(http.StatusNotFound))
		})

		It("is closed to non-admins", func() {
			app.addMember("m")
			Expect(app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/books", app.memberKey, nil)).StatusCode).
				To(Equal(http.StatusForbidden))
		})
	})
