package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

const ytsBody = `{"status":"ok","data":{"movie_count":1,"movies":[{
  "url":"https://yts.mx/movies/dune-2021","imdb_code":"tt1160419",
  "title":"Dune","title_long":"Dune (2021)","year":2021,
  "torrents":[{"url":"https://yts.mx/torrent/download/AAA",
    "hash":"0123456789ABCDEF0123456789ABCDEF01234567","quality":"1080p",
    "type":"bluray","video_codec":"x264","bit_depth":"8","audio_channels":"5.1",
    "seeds":120,"peers":7,"size_bytes":2040109466,"date_uploaded_unix":1514764800}]}]}}`

// routeAll sends every request otelx.HTTPClient makes to h, recording the
// URL it was meant for: the embedded definitions name real tracker hosts.
func routeAll(h http.HandlerFunc) func() []*url.URL {
	GinkgoHelper()
	srv := httptest.NewServer(h)
	DeferCleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	Expect(err).NotTo(HaveOccurred())
	var (
		mu   sync.Mutex
		seen []*url.URL
	)
	orig := otelx.HTTPClient.Transport
	otelx.HTTPClient.Transport = roundTripFunc(
		func(r *http.Request) (*http.Response, error) {
			u := *r.URL
			mu.Lock()
			seen = append(seen, &u)
			mu.Unlock()
			r = r.Clone(r.Context())
			r.URL.Scheme, r.URL.Host, r.Host = target.Scheme, target.Host, target.Host
			return http.DefaultTransport.RoundTrip(r)
		},
	)
	DeferCleanup(func() { otelx.HTTPClient.Transport = orig })
	return func() []*url.URL {
		mu.Lock()
		defer mu.Unlock()
		return append([]*url.URL(nil), seen...)
	}
}

var _ = Describe("a cardigann indexer", Label("unit", "indexers"), func() {
	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"indexers": []map[string]any{{
				"name":      "YTS",
				"protocol":  "cardigann",
				"enabled":   true,
				"cardigann": map[string]any{"definition": "yts"},
			}},
		})
	})

	It("searches through its definition and maps what comes back", func() {
		seen := routeAll(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(ytsBody))
		})

		results, err := New().SearchMovie(context.Background(),
			[]string{"Dune"}, nil, 438631, "tt1160419")
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(1))
		r := results[0]
		Expect(r.Title).To(HavePrefix("Dune (2021) 1080p"))
		Expect(r.Download).To(Equal("https://yts.gg/torrent/download/AAA"))
		Expect(r.InfoHash).To(Equal("0123456789abcdef0123456789abcdef01234567"))
		Expect(r.Category).To(Equal("2040"))
		Expect(r.IMDbID).To(Equal(uint32(1160419)))
		Expect(r.Seeders).To(Equal(uint32(120)))
		Expect(r.Indexer).To(Equal("YTS"))

		urls := seen()
		Expect(urls).To(HaveLen(1))
		Expect(urls[0].Query().Get("query_term")).To(Equal("tt1160419"))
	})

	It("tests the connection with the definition's feed query", func() {
		routeAll(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(ytsBody))
		})
		Expect(New().TestByName(context.Background(), "YTS")).To(Succeed())
	})

	It("files a response it cannot read under ErrBadResponse", func() {
		routeAll(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		})
		Expect(New().TestByName(context.Background(), "YTS")).
			To(MatchError(ErrBadResponse))
	})

	It("reports a link on its site as its own, resolved as it is", func() {
		target, ok, err := ResolveDownload(context.Background(),
			"https://yts.gg/torrent/download/AAA")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(target.URL).To(Equal("https://yts.gg/torrent/download/AAA"))

		_, ok, err = ResolveDownload(
			context.Background(),
			"https://elsewhere.example/x",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
	})
})
