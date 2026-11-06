package restapi

import (
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe("GetStats", Label("unit", "restapi"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
		app.addMember("member@test.com")
	})

	It("rejects a non-admin with 403", func() {
		resp := app.do(app.req(http.MethodGet, "/api/v1/stats", app.memberKey, nil))
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("refuses a range outside the three", func() {
		resp := app.do(
			app.req(http.MethodGet, "/api/v1/stats?range=1y", app.adminKey, nil),
		)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("converts every section", func() {
		seen := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		out := toStats(db.Stats{
			Library: db.StatsLibrary{
				Media: []db.StatsMedia{
					{Kind: "movie", Titles: 2, Files: 3, Bytes: 4, Wanted: 1},
				},
				Resolution: []db.StatsSlice{{Key: "2160p", Count: 1, Bytes: 9}},
			},
			Pipeline: db.StatsPipeline{
				Daily:           []db.StatsDay{{Day: "2026-11-06", Grabbed: 1}},
				Grabbed:         1,
				GrabToImportP50: 60,
				Failures: []db.StatsFailure{
					{Reason: "stalled", Count: 1, LastAt: seen},
				},
			},
			Users: db.StatsUsers{
				Total: 1,
				People: []db.StatsPerson{
					{ID: 7, Name: "Ann", Role: "member", LastSeenAt: &seen},
				},
			},
			Previous: db.StatsTotals{Failed: 5},
		}, StatsRangeN7d, seen)

		Expect(out.Range).To(Equal(StatsRangeN7d))
		Expect(out.Library.Media[0].Kind).To(Equal(StatsKind("movie")))
		Expect(out.Library.Resolution[0].Key).To(Equal("2160p"))
		Expect(out.Pipeline.Funnel.Grabbed).To(BeEquivalentTo(1))
		Expect(out.Pipeline.GrabToImportP50S).To(BeEquivalentTo(60))
		Expect(out.Users.People[0].LastSeenAt).To(Equal(&seen))
		Expect(out.Previous.Failed).To(BeEquivalentTo(5))
	})
})
