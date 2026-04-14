package mediaserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Dispatcher", Label("unit", "mediaserver"), func() {
	var d *Dispatcher

	BeforeEach(func() {
		d = NewDispatcher()
	})

	It("returns nil when no enabled servers", func() {
		configtest.Setup()
		Expect(d.RefreshAll(context.Background(), "/lib")).To(Succeed())
	})

	It("calls RefreshLibrary on each enabled server and joins errors", func() {
		var plexHit, jfHit bool
		plex := newPlexHTTPServer(
			map[string]string{"/lib": "1"},
			"1",
			http.StatusOK,
			&plexHit,
		)
		defer plex.Close()
		jf := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				jfHit = true
				w.WriteHeader(http.StatusUnauthorized)
			}),
		)
		defer jf.Close()

		configtest.Setup(mediaServerConfig(
			map[string]any{
				"name": "plex", "server_type": "plex",
				"host": plex.URL, "api_key": "tok", "enabled": true,
			},
			map[string]any{
				"name": "jf", "server_type": "jellyfin",
				"host": jf.URL, "api_key": "tok", "enabled": true,
			},
		))

		err := d.RefreshAll(context.Background(), "/lib")
		Expect(err).To(MatchError(ContainSubstring("jf")))
		Expect(
			err,
		).To(MatchError(ContainSubstring("jellyfin refresh: unexpected status 401")))
		Expect(plexHit).To(BeTrue())
		Expect(jfHit).To(BeTrue())
	})

	It("threads LibrarySection through to Plex when set", func() {
		var refreshedKey string
		ts := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(HavePrefix("/library/sections/"))
				refreshedKey = r.URL.Path[len("/library/sections/") : len(r.URL.Path)-len("/refresh")]
				w.WriteHeader(http.StatusOK)
			}),
		)
		defer ts.Close()

		configtest.Setup(mediaServerConfig(map[string]any{
			"name": "plex", "server_type": "plex",
			"host": ts.URL, "api_key": "tok", "enabled": true,
			"library_section": "42",
		}))

		Expect(d.RefreshAll(context.Background(), "/lib")).To(Succeed())
		Expect(refreshedKey).To(Equal("42"))
	})

	It("falls back to path-match when LibrarySection is nil", func() {
		var refreshedKey string
		// Wrap the handler to capture the refresh key.
		mux := http.NewServeMux()
		mux.HandleFunc(
			"/library/sections",
			func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write(jsonBytes(map[string]any{
					"MediaContainer": map[string]any{
						"Directory": []map[string]any{
							{
								"key":      "9",
								"Location": []map[string]any{{"path": "/lib"}},
							},
						},
					},
				}))
				Expect(err).NotTo(HaveOccurred())
			},
		)
		mux.HandleFunc(
			"/library/sections/",
			func(w http.ResponseWriter, r *http.Request) {
				refreshedKey = r.URL.Path[len("/library/sections/") : len(r.URL.Path)-len("/refresh")]
				w.WriteHeader(http.StatusOK)
			},
		)
		ts2 := httptest.NewServer(mux)
		defer ts2.Close()

		configtest.Setup(mediaServerConfig(map[string]any{
			"name": "plex", "server_type": "plex",
			"host": ts2.URL, "api_key": "tok", "enabled": true,
		}))

		Expect(d.RefreshAll(context.Background(), "/lib")).To(Succeed())
		Expect(refreshedKey).To(Equal("9"))
	})

	It("aggregates errors when all servers fail", func() {
		jf1 := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			}),
		)
		defer jf1.Close()
		jf2 := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}),
		)
		defer jf2.Close()

		configtest.Setup(mediaServerConfig(
			map[string]any{
				"name": "jf1", "server_type": "jellyfin",
				"host": jf1.URL, "api_key": "tok", "enabled": true,
			},
			map[string]any{
				"name": "jf2", "server_type": "jellyfin",
				"host": jf2.URL, "api_key": "tok", "enabled": true,
			},
		))
		err := d.RefreshAll(context.Background(), "/lib")
		Expect(err).To(MatchError(ContainSubstring("jf1")))
		Expect(err).To(MatchError(ContainSubstring("jf2")))
	})

	It("propagates ctx cancellation to per-server calls", func() {
		ts := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		)
		defer ts.Close()
		configtest.Setup(mediaServerConfig(map[string]any{
			"name": "jf", "server_type": "jellyfin",
			"host": ts.URL, "api_key": "tok", "enabled": true,
		}))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := d.RefreshAll(ctx, "/lib")
		Expect(err).To(MatchError(ContainSubstring("context canceled")))
	})
})

// newPlexHTTPServer returns an httptest.Server that responds to
// /library/sections with the given path→key mapping and accepts
// /library/sections/<expectKey>/refresh with the given status.
// If hit != nil, it is set to true once /refresh is called.
func newPlexHTTPServer(
	pathToKey map[string]string,
	expectKey string,
	refreshStatus int,
	hit *bool,
) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc(
		"/library/sections",
		func(w http.ResponseWriter, _ *http.Request) {
			dirs := make([]map[string]any, 0, len(pathToKey))
			for p, k := range pathToKey {
				dirs = append(dirs, map[string]any{
					"key":      k,
					"Location": []map[string]any{{"path": p}},
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"MediaContainer": map[string]any{"Directory": dirs},
			})
		},
	)
	mux.HandleFunc(
		"/library/sections/"+expectKey+"/refresh",
		func(w http.ResponseWriter, _ *http.Request) {
			if hit != nil {
				*hit = true
			}
			w.WriteHeader(refreshStatus)
		},
	)
	return httptest.NewServer(mux)
}
