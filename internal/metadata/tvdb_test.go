package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TVDB provider", Label("unit", "metadata"), func() {
	var srv *httptest.Server
	var client *TVDB
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		mux := http.NewServeMux()
		mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":{"token":"test-token"}}`))
		})
		mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Header.Get("Authorization")).To(Equal("Bearer test-token"))
			Expect(r.URL.Query().Get("query")).To(Equal("black sea"))
			Expect(r.URL.Query().Get("type")).To(Equal("series"))
			// Base record is in the original language; eng translation wins.
			_, _ = w.Write(
				[]byte(
					`{"data":[{"tvdb_id":"123","name":"Kara Deniz","year":"2023","network":"Halcyon","overview":"orig","image_url":"/p.jpg","translations":{"eng":"The Black Sea"},"overviews":{"eng":"O"}}]}`,
				),
			)
		})
		mux.HandleFunc(
			"/series/123/extended",
			func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Query().Get("meta")).To(Equal("translations"))
				_, _ = w.Write(
					[]byte(
						`{"data":{"id":123,"name":"Kara Deniz","year":"2023","overview":"orig","status":{"name":"Continuing"},"averageRuntime":52,"score":84,"genres":[{"name":"Drama"},{"name":"Mystery"}],"latestNetwork":{"name":"Halcyon"},"seasons":[{"number":1,"type":{"type":"official"}}],"translations":{"nameTranslations":[{"language":"eng","name":"The Black Sea"}],"overviewTranslations":[{"language":"eng","overview":"O"}]}}}`,
					),
				)
			},
		)
		mux.HandleFunc(
			"/series/123/episodes/default/eng",
			func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(
					[]byte(
						`{"data":{"episodes":[{"seasonNumber":1,"number":1,"absoluteNumber":1,"name":"Pilot","aired":"2023-01-01"}]},"links":{"next":null}}`,
					),
				)
			},
		)
		srv = httptest.NewServer(mux)
		DeferCleanup(srv.Close)

		client = NewTVDB()
		client.BaseURL = srv.URL
		client.apiKey = "x"
	})

	It("searches series", func() {
		res, err := client.SearchSeries(ctx, "black sea")
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].TVDBID).To(Equal(uint32(123)))
		Expect(
			res[0].Title,
		).To(Equal("The Black Sea"))
		// eng translation, not "Kara Deniz"
		Expect(res[0].Overview).To(Equal("O"))
		Expect(res[0].Year).To(Equal(uint16(2023)))
	})

	It("gets a series with seasons and episodes", func() {
		d, err := client.GetSeries(ctx, 123)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			d.Title,
		).To(Equal("The Black Sea"))
		// eng translation, not "Kara Deniz"
		Expect(d.Overview).To(Equal("O"))
		Expect(d.Status).To(Equal("continuing"))
		Expect(d.Runtime).To(Equal(uint16(52)))
		// TVDB v4 has no user rating; `score` is popularity, so Rating is unset.
		Expect(d.Rating).To(BeZero())
		Expect(d.Genres).To(ConsistOf("Drama", "Mystery"))
		Expect(d.Seasons).To(HaveLen(1))
		Expect(d.Episodes).To(HaveLen(1))
		Expect(d.Episodes[0].AbsoluteNumber).To(Equal(uint16(1)))
		Expect(d.Episodes[0].AirDate).NotTo(BeNil())
	})
})
