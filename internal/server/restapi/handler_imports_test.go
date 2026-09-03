package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscanfile "github.com/datahearth/streamline/ent/importscanfile"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library/bulkimport"
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
