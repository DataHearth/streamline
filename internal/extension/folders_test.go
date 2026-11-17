package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

const secret = "TOPSECRET-CONTENT"

var _ = Describe("Folders", Label("integration"), func() {
	var (
		ctx     = context.Background()
		cfg     *config.Config
		mgr     *Manager
		dataDir string
		music   string
		outside string
	)

	write := func(path, content string) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(path), 0o750)).To(Succeed())
		Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	}

	start := func(wasm string, folders ...string) {
		GinkgoHelper()
		src, err := filepath.Abs(filepath.Join("testdata/echo", wasm))
		Expect(err).NotTo(HaveOccurred())
		entry := config.ExtensionEntry{
			Name: "echo", Source: src, Enabled: true,
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/", "/inner"},
				Views:    []string{"books"},
				Services: []string{"serve_file"},
				Folders:  folders,
			},
		}
		if wasm == "echo-full.wasm" {
			entry.Grants.Services = append(
				entry.Grants.Services, "ffmpeg", "library_scan",
			)
		}
		cfg.Extensions = []config.ExtensionEntry{entry}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		for _, s := range mgr.Statuses() {
			Expect(s.State).To(Equal(StateRunning), s.Reason)
		}
	}

	call := func(method, path string, q url.Values, body string) Response {
		GinkgoHelper()
		resp, ok, err := mgr.Call(ctx, "echo", Request{
			Method: method, Path: path, Query: q.Encode(), Body: []byte(body),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		return resp
	}

	read := func(path string) Response {
		GinkgoHelper()
		return call(http.MethodGet, "/fs/read", url.Values{"path": {path}}, "")
	}

	BeforeEach(func() {
		root := GinkgoT().TempDir()
		music = filepath.Join(root, "music")
		outside = filepath.Join(root, "outside")
		write(filepath.Join(music, "song.txt"), "la la la")
		write(filepath.Join(outside, "secret"), secret)

		cfg = configtest.SetupFile(map[string]any{
			"library": map[string]any{"music_path": music},
		})
		dataDir = cfg.DataDir
		corePath := filepath.Join(dataDir, "core.db")
		client, err := db.Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })

		mgr = NewManager(dataDir, corePath, nil, nil)
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })
	})

	Context("read-only folders", func() {
		BeforeEach(func() { start("echo-full.wasm", "music", "artwork", "cache") })

		It("reads a file under a granted read-only folder", func() {
			resp := read("/folders/music/song.txt")
			Expect(resp.Status).To(Equal(200))
			Expect(string(resp.Body)).To(Equal("la la la"))
		})

		It("refuses a link that leaves the read-only folder", func() {
			Expect(os.MkdirAll(filepath.Join(music, "lib"), 0o750)).To(Succeed())
			Expect(os.Symlink(
				filepath.Join(outside, "secret"),
				filepath.Join(music, "lib", "escape"),
			)).To(Succeed())

			resp := read("/folders/music/lib/escape")
			Expect(resp.Status).To(Equal(404))
			Expect(string(resp.Body)).NotTo(ContainSubstring(secret))
		})

		It("refuses a dot-dot path out of the read-only folder", func() {
			resp := read("/folders/music/../../outside/secret")
			Expect(resp.Status).To(Equal(404))
			Expect(string(resp.Body)).NotTo(ContainSubstring(secret))
		})

		It("cannot write under a read-only folder", func() {
			resp := call(
				http.MethodPost, "/fs/write",
				url.Values{"path": {"/folders/music/new.txt"}}, "x",
			)
			Expect(resp.Status).To(Equal(500))
			Expect(filepath.Join(music, "new.txt")).NotTo(BeAnExistingFile())
		})

		It("maps a host path from a view to the guest path", func() {
			resp := call(
				http.MethodGet, "/fs/guest",
				url.Values{"path": {filepath.Join(music, "a", "b.flac")}}, "",
			)
			Expect(resp.Status).To(Equal(200))
			Expect(string(resp.Body)).To(Equal("/folders/music/a/b.flac"))

			resp = call(
				http.MethodGet, "/fs/guest",
				url.Values{"path": {filepath.Join(outside, "secret")}}, "",
			)
			Expect(resp.Status).To(Equal(404))
		})

		It("leaves out a folder whose directory is gone and still answers", func() {
			Expect(os.RemoveAll(music)).To(Succeed())

			resp := call(http.MethodGet, "/fs/folders", nil, "")
			Expect(resp.Status).To(Equal(200))
			var folders []Folder
			Expect(json.Unmarshal(resp.Body, &folders)).To(Succeed())
			Expect(folders).NotTo(BeEmpty())
			for _, f := range folders {
				Expect(f.Name).NotTo(Equal("music"))
			}
		})

		It("mounts the new music root after a config change", func() {
			other := filepath.Join(GinkgoT().TempDir(), "other")
			write(filepath.Join(other, "song.txt"), "elsewhere")

			Expect(
				string(read("/folders/music/song.txt").Body),
			).To(Equal("la la la"))
			_, err := config.UpdateLibrary(
				ctx,
				config.LibraryPatch{MusicPath: &other},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				string(read("/folders/music/song.txt").Body),
			).To(Equal("elsewhere"))
		})
	})

	Context("the cache folder", func() {
		BeforeEach(func() { start("echo-full.wasm", "music", "artwork", "cache") })

		It("writes and reads back under the cache", func() {
			w := call(
				http.MethodPost, "/fs/write",
				url.Values{"path": {"/folders/cache/a.bin"}}, "cached",
			)
			Expect(w.Status).To(Equal(204))
			Expect(string(read("/folders/cache/a.bin").Body)).To(Equal("cached"))
			Expect(filepath.Join(dataDir, "extensions", "echo", "cache", "a.bin")).
				To(BeARegularFile())
		})

		It("refuses a host-planted link in the cache", func() {
			cache := filepath.Join(dataDir, "extensions", "echo", "cache")
			Expect(os.MkdirAll(cache, 0o750)).To(Succeed())
			Expect(os.Symlink(
				filepath.Join(outside, "secret"), filepath.Join(cache, "planted"),
			)).To(Succeed())

			resp := read("/folders/cache/planted")
			Expect(resp.Status).To(Equal(404))
			Expect(string(resp.Body)).NotTo(ContainSubstring(secret))
		})

		It("refuses to create a link in the cache", func() {
			resp := call(
				http.MethodPost, "/fs/symlink",
				url.Values{
					"old": {filepath.Join(outside, "secret")},
					"new": {"/folders/cache/link"},
				}, "",
			)
			Expect(resp.Status).To(Equal(500))
			Expect(filepath.Join(dataDir, "extensions", "echo", "cache", "link")).
				NotTo(BeAnExistingFile())
		})
	})

	Context("the store folder", func() {
		store := func() string {
			return filepath.Join(dataDir, "extensions", "echo", "store")
		}
		write := func(name, body string) Response {
			GinkgoHelper()
			return call(
				http.MethodPost, "/fs/write",
				url.Values{"path": {"/folders/store/" + name}}, body,
			)
		}

		BeforeEach(func() {
			start("echo-full.wasm", "music", "artwork", "cache", "store")
		})

		It("is not there without store_bytes", func() {
			Expect(write("a.bin", "kept").Status).To(Equal(500))
			Expect(store()).NotTo(BeADirectory())
		})

		It("keeps what is written through a cache sweep", func() {
			config.Get().Extensions[0].StoreBytes = 1 << 20
			Expect(write("a.bin", "kept").Status).To(Equal(204))
			Expect(mgr.SweepCaches(ctx)).To(Succeed())
			Expect(string(read("/folders/store/a.bin").Body)).To(Equal("kept"))
			Expect(filepath.Join(store(), "a.bin")).To(BeARegularFile())
		})

		It("turns read-only at the limit, and writable again below it", func() {
			config.Get().Extensions[0].StoreBytes = 8
			Expect(write("a.bin", "12345678").Status).To(Equal(204))
			Expect(write("b.bin", "x").Status).To(Equal(500))
			Expect(filepath.Join(store(), "b.bin")).NotTo(BeAnExistingFile())
			Expect(string(read("/folders/store/a.bin").Body)).To(Equal("12345678"))

			resp := call(http.MethodGet, "/fs/folders", nil, "")
			Expect(string(resp.Body)).To(ContainSubstring(
				`"guest_path":"/folders/store","read_only":true`,
			))

			Expect(os.Remove(filepath.Join(store(), "a.bin"))).To(Succeed())
			Expect(write("b.bin", "x").Status).To(Equal(204))
		})

		It("refuses a link out of the store", func() {
			config.Get().Extensions[0].StoreBytes = 1 << 20
			resp := call(
				http.MethodPost, "/fs/symlink",
				url.Values{
					"old": {filepath.Join(outside, "secret.txt")},
					"new": {"/folders/store/link"},
				}, "",
			)
			Expect(resp.Status).To(Equal(500))
			Expect(filepath.Join(store(), "link")).NotTo(BeAnExistingFile())
		})

		It("fails an entry asking for more than the data volume holds", func() {
			config.Get().Extensions[0].StoreBytes = 1 << 62
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			st := mgr.Statuses()[0]
			Expect(st.State).To(Equal(StateFailed))
			Expect(st.Reason).To(ContainSubstring("store_bytes"))
		})
	})

	Context("a folder that was not granted", func() {
		BeforeEach(func() { start("echo.wasm") })

		It("mounts nothing", func() {
			Expect(read("/folders/music/song.txt").Status).To(Equal(404))

			resp := call(http.MethodGet, "/fs/folders", nil, "")
			Expect(string(resp.Body)).To(Equal("[]"))
		})
	})
})
