package mediaserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// fakeMediaServer answers the Plex section list and the Jellyfin library list
// from the same locations, and records every rescan it is sent.
type fakeMediaServer struct {
	*httptest.Server

	mu    sync.Mutex
	calls []string
	// updated holds every path a /Library/Media/Updated body named.
	updated [][]string
}

func newFakeMediaServer(sections []map[string]any) *fakeMediaServer {
	f := &fakeMediaServer{}
	f.Server = httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			switch r.URL.Path {
			case "/library/sections":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"MediaContainer": map[string]any{"Directory": sections},
				})
				return
			case "/Library/VirtualFolders":
				var out []map[string]any
				for _, s := range sections {
					var locs []string
					for _, l := range s["Location"].([]map[string]any) {
						locs = append(locs, l["path"].(string))
					}
					out = append(
						out,
						map[string]any{"Name": s["key"], "Locations": locs},
					)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(out)
				return
			case "/Library/Media/Updated":
				var body jellyfinMediaUpdated
				Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
				var paths []string
				for _, u := range body.Updates {
					Expect(u.UpdateType).To(Equal("Modified"))
					paths = append(paths, u.Path)
				}
				f.updated = append(f.updated, paths)
			}
			call := r.URL.Path
			if r.URL.RawQuery != "" {
				call += "?" + r.URL.Query().Get("path")
			}
			f.calls = append(f.calls, call)
			if strings.HasPrefix(r.URL.Path, "/library/") {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	DeferCleanup(f.Close)
	return f
}

func (f *fakeMediaServer) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func section(key, typ string, locs ...string) map[string]any {
	l := make([]map[string]any, 0, len(locs))
	for _, p := range locs {
		l = append(l, map[string]any{"path": p})
	}
	return map[string]any{"key": key, "type": typ, "Location": l}
}

var _ = Describe("targeted refresh", Label("unit", "mediaserver"), func() {
	sections := []map[string]any{
		section("1", "movie", "/data/movies"),
		section("2", "show", "/data/tv"),
		section("7", "artist", "/data/music"),
	}

	Describe("Plex", func() {
		It("scans each folder inside the section holding it", func() {
			srv := newFakeMediaServer(sections)
			scope, err := NewPlex(srv.URL, "tok").RefreshLibrary(
				context.Background(),
				RefreshRequest{
					Root: "/data/movies",
					Folders: []string{
						"/data/movies/Heat (1995)",
						"/data/movies/Up (2009)",
					},
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(scope).To(Equal(ScopeFolders))
			Expect(srv.Calls()).To(Equal([]string{
				"/library/sections/1/refresh?/data/movies/Heat (1995)",
				"/library/sections/1/refresh?/data/movies/Up (2009)",
			}))
		})

		It("rescans the whole section when a folder lies outside every one", func() {
			srv := newFakeMediaServer(sections)
			scope, err := NewPlex(srv.URL, "tok").RefreshLibrary(
				context.Background(),
				RefreshRequest{
					Root:    "/srv/movies",
					Folders: []string{"/srv/movies/Heat (1995)"},
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(scope).To(Equal(ScopeLibrary))
			// No section holds the root either: every section, as before.
			Expect(srv.Calls()).To(ConsistOf(
				"/library/sections/1/refresh",
				"/library/sections/2/refresh",
				"/library/sections/7/refresh",
			))
		})

		It(
			"keeps a configured section: a folder of another one falls back to it",
			func() {
				srv := newFakeMediaServer(sections)
				scope, err := NewPlex(srv.URL, "tok").RefreshLibrary(
					context.Background(),
					RefreshRequest{
						Root:       "/data/tv",
						Folders:    []string{"/data/tv/Show"},
						SectionKey: "1",
					},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(scope).To(Equal(ScopeLibrary))
				Expect(
					srv.Calls(),
				).To(Equal([]string{"/library/sections/1/refresh"}))
			},
		)

		It("still rescans a configured section when the listing fails", func() {
			var calls []string
			srv := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls = append(calls, r.URL.Path)
					if r.URL.Path == "/library/sections" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					w.WriteHeader(http.StatusOK)
				}),
			)
			DeferCleanup(srv.Close)

			scope, err := NewPlex(srv.URL, "tok").RefreshLibrary(
				context.Background(),
				RefreshRequest{
					Root:       "/data/movies",
					Folders:    []string{"/data/movies/Heat (1995)"},
					SectionKey: "1",
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(scope).To(Equal(ScopeLibrary))
			Expect(calls).To(Equal([]string{
				"/library/sections",
				"/library/sections/1/refresh",
			}))
		})

		It("matches music folders against artist sections only", func() {
			srv := newFakeMediaServer([]map[string]any{
				section("1", "movie", "/data"),
				section("7", "artist", "/data/music"),
			})
			scope, err := NewPlex(srv.URL, "tok").RefreshLibrary(
				context.Background(),
				RefreshRequest{
					Root:        "/data/music",
					Folders:     []string{"/data/music/Artist/Album"},
					SectionType: plexMusicSectionType,
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(scope).To(Equal(ScopeFolders))
			Expect(srv.Calls()).To(Equal([]string{
				"/library/sections/7/refresh?/data/music/Artist/Album",
			}))
		})
	})

	Describe("Jellyfin and Emby", func() {
		It("reports every folder in one Media/Updated request", func() {
			srv := newFakeMediaServer(sections)
			scope, err := NewJellyfin(srv.URL, "tok").RefreshLibrary(
				context.Background(),
				RefreshRequest{
					Root:    "/data/tv",
					Folders: []string{"/data/tv/A/Season 01", "/data/tv/B"},
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(scope).To(Equal(ScopeFolders))
			Expect(srv.Calls()).To(Equal([]string{"/Library/Media/Updated"}))
			Expect(srv.updated).To(Equal([][]string{
				{"/data/tv/A/Season 01", "/data/tv/B"},
			}))
		})

		It("rescans every library when a folder lies outside them", func() {
			srv := newFakeMediaServer(sections)
			scope, err := NewJellyfin(srv.URL, "tok").RefreshLibrary(
				context.Background(),
				RefreshRequest{
					Root:    "/srv/tv",
					Folders: []string{"/data/tv/A", "/srv/tv/B"},
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(scope).To(Equal(ScopeLibrary))
			Expect(srv.Calls()).To(Equal([]string{"/Library/Refresh"}))
		})
	})

	Describe("Dispatcher", func() {
		var root string

		BeforeEach(func() {
			root = GinkgoT().TempDir()
			for _, d := range []string{"Heat (1995)", "Up (2009)"} {
				Expect(os.MkdirAll(filepath.Join(root, d), 0o755)).To(Succeed())
			}
		})

		It("maps folders through the entry's path_mappings", func() {
			srv := newFakeMediaServer(sections)
			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "plex", "server_type": "plex",
				"host": srv.URL, "api_key": "tok", "enabled": true,
				"path_mappings": []map[string]any{
					{"from": root, "to": "/data/movies"},
				},
			}))
			d := newDispatcher(0)

			Expect(d.RefreshAll(context.Background(), "movie", root,
				filepath.Join(root, "Heat (1995)", "Heat.mkv"),
			)).To(Succeed())
			Expect(srv.Calls()).To(Equal([]string{
				"/library/sections/1/refresh?/data/movies/Heat (1995)",
			}))

			st, ok := d.LastRefresh("plex")
			Expect(ok).To(BeTrue())
			Expect(st.Outcome).To(Equal("ok"))
			Expect(st.Scope).To(Equal(ScopeFolders))
			Expect(st.Folders).To(Equal(1))
		})

		It("matches the mapped root to its section when no folder is given", func() {
			srv := newFakeMediaServer(sections)
			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "plex", "server_type": "plex",
				"host": srv.URL, "api_key": "tok", "enabled": true,
				"path_mappings": []map[string]any{
					{"from": root, "to": "/data/movies"},
				},
			}))

			Expect(newDispatcher(0).RefreshAll(context.Background(), "movie", root)).
				To(Succeed())
			Expect(srv.Calls()).To(Equal([]string{"/library/sections/1/refresh"}))
		})

		It("coalesces a burst into one send per server", func() {
			srv := newFakeMediaServer(sections)
			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "jf", "server_type": "jellyfin",
				"host": srv.URL, "api_key": "tok", "enabled": true,
				"path_mappings": []map[string]any{
					{"from": root, "to": "/data/movies"},
				},
			}))
			d := newDispatcher(time.Hour)

			for _, f := range []string{"Heat (1995)", "Up (2009)", "Heat (1995)"} {
				Expect(d.RefreshAll(context.Background(), "movie", root,
					filepath.Join(root, f, "file.mkv"),
				)).To(Succeed())
			}
			Expect(srv.Calls()).To(BeEmpty())

			d.Flush(context.Background())
			Expect(srv.Calls()).To(Equal([]string{"/Library/Media/Updated"}))
			Expect(srv.updated).To(Equal([][]string{
				{"/data/movies/Heat (1995)", "/data/movies/Up (2009)"},
			}))

			d.Flush(context.Background())
			Expect(srv.Calls()).To(HaveLen(1), "nothing is left to send")
		})

		It("sends on its own once the window ends", func() {
			srv := newFakeMediaServer(sections)
			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "jf", "server_type": "jellyfin",
				"host": srv.URL, "api_key": "tok", "enabled": true,
			}))
			d := newDispatcher(10 * time.Millisecond)

			Expect(d.RefreshAll(context.Background(), "movie", root)).To(Succeed())
			Eventually(srv.Calls).Should(Equal([]string{"/Library/Refresh"}))
		})

		It(
			"turns a batch into a library rescan once a request asks for the whole",
			func() {
				srv := newFakeMediaServer(sections)
				configtest.Setup(mediaServerConfig(map[string]any{
					"name": "jf", "server_type": "jellyfin",
					"host": srv.URL, "api_key": "tok", "enabled": true,
				}))
				d := newDispatcher(time.Hour)

				Expect(d.RefreshAll(context.Background(), "movie", root,
					filepath.Join(root, "Heat (1995)"),
				)).To(Succeed())
				Expect(
					d.RefreshAll(context.Background(), "movie", root),
				).To(Succeed())
				d.Flush(context.Background())
				Expect(srv.Calls()).To(Equal([]string{"/Library/Refresh"}))
			},
		)

		It("turns a batch past the folder cap into a library rescan", func() {
			srv := newFakeMediaServer(sections)
			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "jf", "server_type": "jellyfin",
				"host": srv.URL, "api_key": "tok", "enabled": true,
			}))
			d := newDispatcher(time.Hour)

			for i := range maxBatchFolders + 1 {
				dir := filepath.Join(
					root,
					"Movie",
					string(rune('a'+i%26))+string(rune('a'+i/26)),
				)
				Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
				Expect(d.RefreshAll(context.Background(), "movie", root, dir)).
					To(Succeed())
			}
			d.Flush(context.Background())
			Expect(srv.Calls()).To(Equal([]string{"/Library/Refresh"}))
		})

		It("records a failed refresh for the settings page", func() {
			down := httptest.NewServer(http.NotFoundHandler())
			down.Close()
			configtest.Setup(mediaServerConfig(map[string]any{
				"name": "jf", "server_type": "jellyfin",
				"host": down.URL, "api_key": "tok", "enabled": true,
			}))
			d := newDispatcher(0)

			Expect(d.RefreshAll(context.Background(), "movie", root)).
				To(HaveOccurred())
			st, ok := d.LastRefresh("jf")
			Expect(ok).To(BeTrue())
			Expect(st.Outcome).To(Equal("error"))
			Expect(st.Error).NotTo(BeEmpty())
			Expect(st.At).NotTo(BeZero())
		})
	})
})
