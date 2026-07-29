package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

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

var _ = Describe("TVDB token handling", Label("unit", "metadata"), func() {
	var (
		ctx    context.Context
		client *TVDB
		logins atomic.Int32
		// search is swapped per spec to drive the /search response.
		search func(w http.ResponseWriter, r *http.Request)

		mu       sync.Mutex
		bearers  []string
		record   func(r *http.Request)
		snapshot func() []string
	)

	BeforeEach(func() {
		ctx = context.Background()
		logins.Store(0)
		bearers = nil
		record = func(r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			bearers = append(
				bearers,
				strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
			)
		}
		snapshot = func() []string {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(bearers)
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(
				w,
				`{"data":{"token":"token-%d"}}`,
				logins.Add(1),
			)
		})
		mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
			record(r)
			search(w, r)
		})
		srv := httptest.NewServer(mux)
		DeferCleanup(srv.Close)

		client = NewTVDB()
		client.BaseURL = srv.URL
		client.apiKey = "x"
	})

	It("logs in once when parallel requests share an empty token", func() {
		search = func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data":[]}`))
		}

		const parallel = 8
		errs := make([]error, parallel)
		var wg sync.WaitGroup
		for i := range parallel {
			wg.Go(func() {
				_, errs[i] = client.SearchSeries(ctx, "q")
			})
		}
		wg.Wait()

		for _, err := range errs {
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(logins.Load()).To(Equal(int32(1)))
		Expect(snapshot()).To(HaveLen(parallel))
		Expect(snapshot()).To(HaveEach("token-1"))
	})

	It("re-logs in and retries once when the token expired", func() {
		search = func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "Bearer token-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"tvdb_id":"7","name":"S"}]}`))
		}

		res, err := client.SearchSeries(ctx, "q")
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].TVDBID).To(Equal(uint32(7)))
		Expect(logins.Load()).To(Equal(int32(2)))
		Expect(snapshot()).To(Equal([]string{"token-1", "token-2"}))
	})

	It("fails when the freshly issued token is rejected too", func() {
		search = func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}

		_, err := client.SearchSeries(ctx, "q")
		Expect(err).To(MatchError(ContainSubstring("unexpected status 401")))
		Expect(logins.Load()).To(Equal(int32(2)))
		Expect(snapshot()).To(Equal([]string{"token-1", "token-2"}))
	})
})
