package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/cardigann/definitions"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// tracker stands in for every host a definition talks to: requests keep
// their own URL in Requests and are answered by the handler.
type tracker struct {
	mu       sync.Mutex
	srv      *httptest.Server
	requests []*url.URL
}

func newTracker(h http.HandlerFunc) *tracker {
	GinkgoHelper()
	t := &tracker{}
	t.srv = httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h(w, r)
		}),
	)
	DeferCleanup(t.srv.Close)
	return t
}

// client routes every request to the stand-in, keeping the original host in
// the recorded URL so a spec can assert where the engine meant to go.
func (t *tracker) client() *http.Client {
	target, err := url.Parse(t.srv.URL)
	Expect(err).NotTo(HaveOccurred())
	return &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			orig := *r.URL
			t.mu.Lock()
			t.requests = append(t.requests, &orig)
			t.mu.Unlock()
			r = r.Clone(r.Context())
			r.Header.Set("X-Original-Host", orig.Host)
			r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
			r.Host = target.Host
			return http.DefaultTransport.RoundTrip(r)
		}),
	}
}

func (t *tracker) Requests() []*url.URL {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*url.URL(nil), t.requests...)
}

const ytsBody = `{"status":"ok","data":{"movie_count":1,"movies":[{
  "id":10,"url":"https://yts.mx/movies/the-matrix-1999","imdb_code":"tt0133093",
  "title":"The Matrix","title_long":"The Matrix: Reloaded (1999)","year":1999,
  "large_cover_image":"https://yts.mx/assets/m.jpg",
  "torrents":[
    {"url":"https://yts.mx/torrent/download/AAA","hash":"0123456789ABCDEF0123456789ABCDEF01234567",
     "quality":"1080p","type":"bluray","is_repack":"0","video_codec":"x264","bit_depth":"8",
     "audio_channels":"5.1","seeds":120,"peers":7,"size":"1.9 GB","size_bytes":2040109466,
     "date_uploaded":"2018-01-01 00:00:00","date_uploaded_unix":1514764800},
    {"url":"https://yts.mx/torrent/download/BBB","hash":"FEDCBA9876543210FEDCBA9876543210FEDCBA98",
     "quality":"2160p","type":"web","is_repack":"0","video_codec":"x265","bit_depth":"10",
     "audio_channels":"2.0","seeds":30,"peers":2,"size":"5.1 GB","size_bytes":5476083302,
     "date_uploaded":"2020-01-01 00:00:00","date_uploaded_unix":1577836800}
  ]}]}}`

var _ = Describe(
	"a JSON definition end to end (yts)",
	Label("unit", "cardigann"),
	func() {
		var (
			trk *tracker
			eng *Engine
		)

		BeforeEach(func() {
			trk = newTracker(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(ytsBody))
			})
			def, err := definitions.Load("", "yts")
			Expect(err).NotTo(HaveOccurred())
			eng, err = New(def, Options{
				Name:   "yts-" + CurrentSpecReport().LeafNodeText,
				Client: trk.client(),
				Now:    func() time.Time { return time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC) },
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It(
			"searches by IMDb id when the mode lists it, and leaves the term out",
			func() {
				_, err := eng.Search(context.Background(), Query{
					Kind: KindMovie, Term: "The Matrix", IMDbID: "tt0133093",
					TMDBID: 603, Categories: []int{2000},
				})
				Expect(err).NotTo(HaveOccurred())
				reqs := trk.Requests()
				Expect(reqs).To(HaveLen(1))
				Expect(reqs[0].Host).To(Equal("movies-api.accel.li"))
				Expect(reqs[0].Path).To(Equal("/api/v2/list_movies.json"))
				Expect(reqs[0].Query().Get("query_term")).To(Equal("tt0133093"))
				Expect(reqs[0].Query().Get("limit")).To(Equal("50"))
			},
		)

		It("runs the keywords filters over the term of a keyword search", func() {
			_, err := eng.Search(context.Background(), Query{
				Kind:       KindMovie,
				Term:       "Spider-Man: No Way Home",
				Categories: []int{2000},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(trk.Requests()[0].Query().Get("query_term")).
				To(Equal("Spider Man No Way Home"))
		})

		It("expands a movie's torrents into one release each", func() {
			releases, err := eng.Search(context.Background(), Query{
				Kind: KindMovie, Term: "The Matrix", Categories: []int{2000},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(releases).To(HaveLen(2))

			hd := releases[0]
			Expect(
				hd.Title,
			).To(Equal("The Matrix Reloaded (1999) 1080p BRRip 5.1 x264 -YTS"))
			Expect(hd.Category).To(ConsistOf(2040))
			Expect(hd.Size).To(Equal(int64(2040109466)))
			Expect(hd.Seeders).To(Equal(int64(120)))
			Expect(hd.Leechers).To(Equal(int64(7)))
			Expect(hd.IMDbID).To(Equal(int64(133093)))
			Expect(hd.InfoHash).To(Equal("0123456789ABCDEF0123456789ABCDEF01234567"))
			Expect(
				hd.Published.UTC(),
			).To(Equal(time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)))
			// The definition rewrites the dead yts.mx host onto its site link.
			Expect(hd.Download).To(Equal("https://yts.gg/torrent/download/AAA"))
			Expect(hd.Details).To(Equal("https://yts.gg/movies/the-matrix-1999"))
			Expect(hd.Magnet).To(HavePrefix("magnet:?xt=urn:btih:0123456789ABCDEF"))

			uhd := releases[1]
			Expect(
				uhd.Title,
			).To(Equal("The Matrix Reloaded (1999) 2160p WEBRip 10Bit x265 -YTS"))
			Expect(uhd.Category).To(ConsistOf(2045))
		})

		It("owns links on its site and resolves them to themselves", func() {
			Expect(eng.Owns("https://yts.gg/torrent/download/AAA")).To(BeTrue())
			Expect(eng.Owns("https://example.org/x")).To(BeFalse())
			d, err := eng.Resolve(
				context.Background(),
				"https://yts.gg/torrent/download/AAA",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(d.URL).To(Equal("https://yts.gg/torrent/download/AAA"))
		})
	},
)
