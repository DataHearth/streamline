package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func splitHostPort(rawURL string) (string, uint16) {
	GinkgoHelper()
	u, err := url.Parse(rawURL)
	Expect(err).NotTo(HaveOccurred())
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	Expect(err).NotTo(HaveOccurred())
	return u.Hostname(), uint16(port)
}

var _ = Describe("Service", Label("integration", "indexers"), func() {
	Describe("SearchMovie", func() {
		It(
			"searches all enabled indexers in parallel and merges results sorted by seeders",
			func() {
				ctx := context.Background()

				ts1 := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(http.StatusOK)
						_, err := w.Write(torznabXML([]testRSSItem{
							{
								Title: "Interstellar 1080p BluRay-GROUP1",
								GUID:  "https://idx1.com/1",
								Link:  "https://idx1.com/dl/1",
								Size:  5000000000,
								Enclosure: testEnclosure{
									URL:    "https://idx1.com/dl/1",
									Length: 5000000000,
									Type:   "application/x-bittorrent",
								},
								ExtraXML: torznabAttrs(
									map[string]string{
										"seeders": "50",
										"peers":   "10",
									},
								),
							},
						}))
						Expect(err).NotTo(HaveOccurred())
					}),
				)
				defer ts1.Close()

				ts2 := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(http.StatusOK)
						_, err := w.Write(torznabXML([]testRSSItem{
							{
								Title: "Interstellar 2160p BluRay-GROUP2",
								GUID:  "https://idx2.com/1",
								Link:  "https://idx2.com/dl/1",
								Size:  10000000000,
								Enclosure: testEnclosure{
									URL:    "https://idx2.com/dl/1",
									Length: 10000000000,
									Type:   "application/x-bittorrent",
								},
								ExtraXML: torznabAttrs(
									map[string]string{
										"seeders": "200",
										"peers":   "20",
									},
								),
							},
						}))
						Expect(err).NotTo(HaveOccurred())
					}),
				)
				defer ts2.Close()

				host1, port1 := splitHostPort(ts1.URL)
				host2, port2 := splitHostPort(ts2.URL)
				configtest.Setup(map[string]any{
					"indexers": []map[string]any{
						{
							"name":     "Indexer1",
							"host":     host1,
							"port":     int(port1),
							"api_key":  "key1",
							"protocol": "torznab",
							"enabled":  true,
						},
						{
							"name":     "Indexer2",
							"host":     host2,
							"port":     int(port2),
							"api_key":  "key2",
							"protocol": "torznab",
							"enabled":  true,
						},
						// disabled — should be skipped
						{
							"name":     "Disabled",
							"host":     "nope",
							"port":     80,
							"api_key":  "key3",
							"protocol": "torznab",
							"enabled":  false,
						},
					},
				})

				svc := New()
				results, err := svc.SearchMovie(
					ctx,
					[]string{"Interstellar"},
					157336,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(HaveLen(2))
				// Sorted by seeders desc: 200, then 50
				Expect(results[0].Seeders).To(Equal(uint32(200)))
				Expect(results[1].Seeders).To(Equal(uint32(50)))
			},
		)

		It("drops the other films an indexer answers a keyword search with",
			func() {
				ctx := context.Background()

				ts := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(http.StatusOK)
						_, err := w.Write(torznabXML([]testRSSItem{
							{
								Title: "Nonnas.2025.AD.MULTI.VFF.1080p.WEB.EAC3.5.1.x264-FW",
								GUID:  "https://idx.com/1",
								Link:  "https://idx.com/dl/1",
								Size:  5600000000,
								Enclosure: testEnclosure{
									URL:    "https://idx.com/dl/1",
									Length: 5600000000,
									Type:   "application/x-bittorrent",
								},
								ExtraXML: torznabAttrs(
									map[string]string{"seeders": "29"},
								),
							},
							{
								Title: "Comme.un.chef.2012.MULTI.VFF.1080p.BluRay.x264-GRP",
								GUID:  "https://idx.com/2",
								Link:  "https://idx.com/dl/2",
								Size:  4000000000,
								Enclosure: testEnclosure{
									URL:    "https://idx.com/dl/2",
									Length: 4000000000,
									Type:   "application/x-bittorrent",
								},
								ExtraXML: torznabAttrs(
									map[string]string{"seeders": "5"},
								),
							},
						}))
						Expect(err).NotTo(HaveOccurred())
					}),
				)
				defer ts.Close()

				host, port := splitHostPort(ts.URL)
				configtest.Setup(map[string]any{
					"indexers": []map[string]any{
						{
							"name":     "Indexer1",
							"host":     host,
							"port":     int(port),
							"api_key":  "key1",
							"protocol": "torznab",
							"enabled":  true,
						},
					},
				})

				results, err := New().SearchMovie(
					ctx,
					[]string{"Comme un chef"},
					127585,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(results).To(HaveLen(1))
				Expect(results[0].Title).To(HavePrefix("Comme.un.chef"))
			},
		)
	})

	// The unit specs cover prowlarrQuery, filterProviderIDs and narrowed() one
	// at a time. What only shows up composed is searchAll's own wiring: that
	// the retry fires at all, that it carries the kind and drops the tokens,
	// and that a wrong-show release is gone before the caller's scope filters
	// ever see it.
	Describe("the Prowlarr fan-out", func() {
		It("retries on the bare title, keeping the category", func() {
			rec := newProwlarrRecorder(`[]`, `[
				{"title":"Show.S02E03.1080p.WEB-GRP","downloadUrl":"http://x/a.torrent",
				 "protocol":"torrent","seeders":10}
			]`)
			DeferCleanup(rec.Close)
			configtest.Setup(prowlarrIndexerConfig(rec.URL()))

			results, _, err := New().
				SearchEpisode(context.Background(), []string{"Show"}, 111, 2, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))

			queries := rec.Queries()
			Expect(queries).To(HaveLen(2), "the empty first query must be retried")

			// First pass: fully narrowed.
			Expect(queries[0].Get("type")).To(Equal("tvsearch"))
			Expect(queries[0].Get("query")).To(Equal("Show{season:2}{episode:3}"))
			Expect(queries[0].Get("categories")).To(Equal("5000"))

			// The retry: bare title, tokens gone — keeping them would re-issue
			// the query that just came back empty — but the category stays, or
			// the pass keyword-searches every indexer's whole catalogue.
			Expect(queries[1].Get("query")).To(Equal("Show"))
			Expect(queries[1].Get("categories")).To(Equal("5000"))
		})

		It("does not retry a query that was already bare", func() {
			// A movie search with no tmdbid names nothing beyond the title, so
			// there is no narrower form to fall back from.
			rec := newProwlarrRecorder(`[]`)
			DeferCleanup(rec.Close)
			configtest.Setup(prowlarrIndexerConfig(rec.URL()))

			_, err := New().SearchMovie(context.Background(), []string{"Dune"}, 0)
			Expect(err).NotTo(HaveOccurred())

			queries := rec.Queries()
			Expect(queries).To(HaveLen(1))
			Expect(queries[0].Get("type")).To(Equal("movie"))
			Expect(queries[0].Get("categories")).To(Equal("2000"))
		})

		It("drops a release the tracker labelled as another show", func() {
			// Both releases parse to the same title and the same SxxExx, so
			// neither filterToEpisode nor preferTitleMatches can separate
			// them — the tracker's own id is the only thing that does.
			rec := newProwlarrRecorder(`[
				{"title":"Show.S02E03.1080p.WEB-OURS","downloadUrl":"http://x/a.torrent",
				 "protocol":"torrent","seeders":5,"tvdbId":111},
				{"title":"Show.S02E03.2160p.WEB-OTHER","downloadUrl":"http://x/b.torrent",
				 "protocol":"torrent","seeders":900,"tvdbId":222}
			]`)
			DeferCleanup(rec.Close)
			configtest.Setup(prowlarrIndexerConfig(rec.URL()))

			results, _, err := New().
				SearchEpisode(context.Background(), []string{"Show"}, 111, 2, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			// The wrong show had 900 seeders and would have won on score.
			Expect(results[0].Title).To(HaveSuffix("OURS"))
			Expect(results[0].TVDBID).To(Equal(uint32(111)))
		})
	})
})

// prowlarrRecorder is a Prowlarr /api/v1/search stand-in that records every
// query it is asked and replies with the next canned body, repeating the last
// one once the list is spent.
type prowlarrRecorder struct {
	srv     *httptest.Server
	mu      sync.Mutex
	queries []url.Values
	bodies  []string
	calls   int
}

func newProwlarrRecorder(bodies ...string) *prowlarrRecorder {
	rec := &prowlarrRecorder{bodies: bodies}
	rec.srv = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			rec.mu.Lock()
			rec.queries = append(rec.queries, r.URL.Query())
			body := rec.bodies[min(rec.calls, len(rec.bodies)-1)]
			rec.calls++
			rec.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(body))
			Expect(err).NotTo(HaveOccurred())
		}))
	return rec
}

func (r *prowlarrRecorder) URL() string { return r.srv.URL }
func (r *prowlarrRecorder) Close()      { r.srv.Close() }

func (r *prowlarrRecorder) Queries() []url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]url.Values(nil), r.queries...)
}

func prowlarrIndexerConfig(rawURL string) map[string]any {
	GinkgoHelper()
	host, port := splitHostPort(rawURL)
	return map[string]any{
		"indexers": []map[string]any{
			{
				"name":     "Prowlarr",
				"host":     host,
				"port":     int(port),
				"api_key":  "key",
				"protocol": "prowlarr",
				"enabled":  true,
			},
		},
	}
}
