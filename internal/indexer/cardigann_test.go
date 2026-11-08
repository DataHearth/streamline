package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/testutil/cardiganntest"
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
		cardiganntest.Install()
		configtest.Setup(map[string]any{
			"indexers": []map[string]any{{
				"name":     "YTS",
				"protocol": "cardigann",
				"enabled":  true,
				"cardigann": map[string]any{
					"definition": "yts", "commit": cardiganntest.Upstream.Commit,
				},
			}},
		})
	})

	It("searches through its definition and maps what comes back", func() {
		seen := routeAll(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(ytsBody))
		})

		results, err := New(nil).SearchMovie(context.Background(),
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
		Expect(New(nil).TestByName(context.Background(), "YTS")).To(Succeed())
	})

	It("files a response it cannot read under ErrBadResponse", func() {
		routeAll(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		})
		Expect(New(nil).TestByName(context.Background(), "YTS")).
			To(MatchError(ErrBadResponse))
	})

	It("fetches a link on its site itself, and leaves another host alone", func() {
		urls := routeAll(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("d8:announce0:e"))
		})
		target, ok, err := ResolveDownload(context.Background(), nil,
			"https://yts.gg/torrent/download/AAA")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(string(target.Bytes)).To(Equal("d8:announce0:e"))
		Expect(urls()).To(HaveLen(1))
		Expect(urls()[0].Path).To(Equal("/torrent/download/AAA"))

		_, ok, err = ResolveDownload(
			context.Background(),
			nil,
			"https://elsewhere.example/x",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe(
	"a cardigann indexer logging in by form",
	Label("unit", "indexers"),
	func() {
		It("logs in once for two calls that both found no session", func() {
			name := "3d-" + CurrentSpecReport().LeafNodeText
			cardiganntest.Install()
			configtest.Setup(map[string]any{
				"indexers": []map[string]any{{
					"name":     name,
					"protocol": "cardigann",
					"enabled":  true,
					"cardigann": map[string]any{
						"definition": "3dtorrents",
						"commit":     cardiganntest.Upstream.Commit,
						"settings": map[string]any{
							"username": "u", "password": "p",
						},
					},
				}},
			})
			store := dbmocks.NewMockStore(GinkgoT())
			rows := memSessions(store)
			var posts atomic.Int32
			routeAll(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					http.SetCookie(w, &http.Cookie{Name: "pass", Value: "xyz"})
				}
				if r.URL.Query().Get("page") == "login" {
					_, _ = w.Write([]byte(`<form action="takelogin.php">` +
						`<input name="uid"><input name="pwd"></form>`))
					return
				}
				_, _ = w.Write([]byte(`<a href="logout.php">out</a>`))
			})
			svc := New(store)
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i := range errs {
				wg.Go(func() {
					errs[i] = svc.TestByName(context.Background(), name)
				})
			}
			wg.Wait()
			Expect(errs).To(HaveEach(Succeed()))
			Expect(posts.Load()).To(Equal(int32(1)))
			Expect(rows[name].Cookies).To(HaveKeyWithValue("pass", "xyz"))
		})
	},
)

// memSessions backs a store mock's session methods with a map, so a spec
// reads what the indexer left behind the way the next call would.
func memSessions(store *dbmocks.MockStore) map[string]db.IndexerSessionParams {
	rows := map[string]db.IndexerSessionParams{}
	var mu sync.Mutex
	store.EXPECT().IndexerSession(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, name string) (*ent.IndexerSession, error) {
			mu.Lock()
			defer mu.Unlock()
			p, ok := rows[name]
			if !ok {
				return nil, nil
			}
			return &ent.IndexerSession{
				Name: name, Cookies: p.Cookies, UserAgent: p.UserAgent,
				ExpiresAt: p.ExpiresAt, LoginFailures: p.LoginFailures,
				RetryAt: p.RetryAt, LastError: p.LastError,
				ConfigHash: p.ConfigHash,
			}, nil
		},
	).Maybe()
	store.EXPECT().PutIndexerSession(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, name string, p db.IndexerSessionParams) error {
			mu.Lock()
			defer mu.Unlock()
			rows[name] = p
			return nil
		}).
		Maybe()
	return rows
}

var _ = Describe(
	"a cardigann indexer that logs in",
	Label("unit", "indexers"),
	func() {
		var (
			store *dbmocks.MockStore
			rows  map[string]db.IndexerSessionParams
			name  string
		)

		BeforeEach(func() {
			// The pacer is keyed by name: one per spec keeps them from waiting
			// on each other.
			name = "city-" + CurrentSpecReport().LeafNodeText
			cardiganntest.Install()
			configtest.Setup(map[string]any{
				"indexers": []map[string]any{{
					"name":     name,
					"protocol": "cardigann",
					"enabled":  true,
					"cardigann": map[string]any{
						"definition": "13city",
						"commit":     cardiganntest.Upstream.Commit,
						"settings":   map[string]any{"cookie": "uid=1; pass=abc"},
					},
				}},
			})
			store = dbmocks.NewMockStore(GinkgoT())
			rows = memSessions(store)
		})

		It("logs in with its cookie and keeps the session", func() {
			routeAll(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Header.Get("Cookie")).To(ContainSubstring("pass=abc"))
				_, _ = w.Write([]byte(`<a href="logout.php">out</a>`))
			})
			Expect(New(store).TestByName(context.Background(), name)).To(Succeed())
			Expect(rows[name].Cookies).To(Equal(map[string]string{
				"uid": "1", "pass": "abc",
			}))
			Expect(*rows[name].ExpiresAt).To(BeTemporally("~",
				time.Now().Add(30*24*time.Hour), time.Minute))
		})

		It(
			"backs off after a login that does not take, until a person retries",
			func() {
				seen := routeAll(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/login.php", http.StatusFound)
				})
				svc := New(store)
				Expect(svc.TestByName(context.Background(), name)).
					To(MatchError(ErrLoginFailed))
				Expect(rows[name].LoginFailures).To(Equal(uint8(1)))
				Expect(*rows[name].RetryAt).To(BeTemporally("~",
					time.Now().Add(time.Minute), 5*time.Second))
				Expect(rows[name].Cookies).To(BeEmpty())
				asked := len(seen())

				// Held back: no request leaves.
				Expect(svc.TestByName(context.Background(), name)).
					To(MatchError(ErrLoginBackoff))
				Expect(seen()).To(HaveLen(asked))

				// A person logging in is not held back.
				Expect(svc.Login(context.Background(), name, "", "")).To(Succeed())
				Expect(rows[name].LoginFailures).To(BeZero())
				Expect(rows[name].Cookies).To(HaveKeyWithValue("pass", "abc"))
			},
		)

		It("does not replay a session earned under other settings", func() {
			exp := time.Now().Add(time.Hour)
			rows[name] = db.IndexerSessionParams{
				Cookies:    map[string]string{"uid": "9", "pass": "old"},
				ExpiresAt:  &exp,
				ConfigHash: "earlier settings",
			}
			routeAll(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Header.Get("Cookie")).To(ContainSubstring("pass=abc"))
				Expect(r.Header.Get("Cookie")).NotTo(ContainSubstring("old"))
				_, _ = w.Write([]byte(`<a href="logout.php">out</a>`))
			})
			Expect(New(store).TestByName(context.Background(), name)).To(Succeed())
			ix, _ := config.FindIndexer(name)
			Expect(rows[name].ConfigHash).To(Equal(configHash(ix)))
			Expect(rows[name].Cookies).To(HaveKeyWithValue("pass", "abc"))
		})

		It("sends the session with a download it fetches", func() {
			exp := time.Now().Add(time.Hour)
			ix, _ := config.FindIndexer(name)
			rows[name] = db.IndexerSessionParams{
				Cookies: map[string]string{
					"uid":  "1",
					"pass": "abc",
				},
				ExpiresAt:  &exp,
				ConfigHash: configHash(ix),
			}
			routeAll(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Header.Get("Cookie")).To(ContainSubstring("pass=abc"))
				Expect(r.Header.Get("Cookie")).To(ContainSubstring("uid=1"))
				_, _ = w.Write([]byte("d8:announce0:e"))
			})
			target, ok, err := ResolveDownload(context.Background(), store,
				"https://13city.org/download.php?id=7")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(string(target.Bytes)).To(Equal("d8:announce0:e"))
		})

		It("answers the magnet a download link redirects to", func() {
			routeAll(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "magnet:?xt=urn:btih:abc")
				w.WriteHeader(http.StatusFound)
			})
			target, ok, err := ResolveDownload(context.Background(), store,
				"https://13city.org/download.php?id=7")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(target.Magnet).To(Equal("magnet:?xt=urn:btih:abc"))
		})
	},
)
