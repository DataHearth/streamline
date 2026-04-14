package restapi

import (
	"encoding/json"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	entimportscanshow "github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe("Handler: Import scan shows",
	Label("unit", "server", "imports"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		It("GET /library/imports/{id}/shows lists series rows", func() {
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
			}, uint32(1), nil).Once()

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
			"PATCH /library/imports/{id}/shows/{showId} records the decision",
			func() {
				app.store.EXPECT().UpdateImportScanShowDecision(mock.Anything,
					uint32(1), entimportscanshow.DecisionAccept, mock.Anything).
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

		It("forbids non-admins", func() {
			app.addMember("")
			resp := app.do(app.req(http.MethodGet,
				"/api/v1/library/imports/5/shows", app.memberKey, nil))
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
