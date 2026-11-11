package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

type fakeFiles struct{ client *ent.Client }

func (f fakeFiles) BookFile(
	ctx context.Context,
	id uint32,
) (string, time.Time, error) {
	mf, err := f.client.MediaFile.Get(ctx, id)
	if err != nil {
		return "", time.Time{}, err
	}
	if _, err := os.Stat(mf.Path); err != nil {
		return "", time.Time{}, err
	}
	return mf.Path, mf.UpdateTime, nil
}

type fakePosters struct {
	kind string
	id   uint32
}

func (p *fakePosters) Serve(
	w http.ResponseWriter,
	_ *http.Request,
	kind string,
	id uint32,
) {
	p.kind, p.id = kind, id
	_, _ = w.Write([]byte("poster"))
}

var _ = Describe("Manager.Middleware", Label("integration"), func() {
	var (
		ctx     = context.Background()
		cfg     *config.Config
		client  *ent.Client
		mgr     *Manager
		entry   config.ExtensionEntry
		posters *fakePosters
		h       http.Handler
	)

	do := func(method, target string, mod ...func(*http.Request)) *httptest.ResponseRecorder {
		GinkgoHelper()
		req := httptest.NewRequest(method, target, nil)
		for _, m := range mod {
			m(req)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	asCaller := func(r *http.Request) {
		*r = *r.WithContext(auth.ContextWithClaims(
			r.Context(),
			&auth.Claims{UserID: 4, Role: "member"},
		))
	}

	bookFile := func(content string) uint32 {
		GinkgoHelper()
		path := filepath.Join(GinkgoT().TempDir(), "x.epub")
		Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
		book := client.Book.Create().
			SetHardcoverID(1).
			SetTitle("T").
			SetAuthorName("A").
			SaveX(ctx)
		return client.MediaFile.Create().
			SetPath(path).
			SetSize(int64(len(content))).
			SetQuality("EPUB").
			SetBookKind(mediafile.BookKindEbook).
			SetBook(book).
			SaveX(ctx).ID
	}

	BeforeEach(func() {
		cfg = configtest.Setup()
		dir := GinkgoT().TempDir()
		corePath := filepath.Join(dir, "core.db")
		var err error
		client, err = db.Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })

		mgr = NewManager(dir, corePath, nil, nil)
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })

		wasm, err := filepath.Abs("testdata/echo/echo.wasm")
		Expect(err).NotTo(HaveOccurred())
		entry = config.ExtensionEntry{
			Name:    "echo",
			Source:  wasm,
			Enabled: true,
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/echo"},
				Views:    []string{"books", "book_files", "authors"},
				Services: []string{"serve_file", "serve_poster"},
			},
		}
		cfg.Extensions = []config.ExtensionEntry{entry}
		Expect(mgr.Reconcile(ctx)).To(Succeed())

		posters = &fakePosters{}
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})
		h = mgr.Middleware(fakeFiles{client: client}, posters)(next)
	})

	DescribeTable(
		"gives the guest the path relative to its mount",
		func(target, want string) {
			rr := do(http.MethodGet, target)
			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(rr.Body.String()).To(Equal(want))
		},
		Entry("bare prefix", "/echo", "/"),
		Entry("trailing slash", "/echo/", "/"),
		Entry("nested with a query", "/echo/a/b?x=1", "/a/b"),
	)

	It("passes the raw query string", func() {
		Expect(do(http.MethodGet, "/echo/query?x=1&y=2").Body.String()).
			To(Equal("x=1&y=2"))
	})

	It("falls through for a path no extension holds", func() {
		Expect(do(http.MethodGet, "/other").Code).To(Equal(http.StatusTeapot))
		Expect(do(http.MethodGet, "/echoes").Code).To(Equal(http.StatusTeapot))
		Expect(do(http.MethodGet, "/api/v1/extensions").Code).
			To(Equal(http.StatusTeapot))
		Expect(do(http.MethodGet, "/api/v1/extensions/echo").Code).
			To(Equal(http.StatusTeapot))
	})

	It("reports a self-authenticated mount only while it is live", func() {
		Expect(mgr.SelfAuthenticated("/echo/x")).To(BeTrue())
		Expect(mgr.SelfAuthenticated("/echo")).To(BeTrue())
		Expect(mgr.SelfAuthenticated("/other/x")).To(BeFalse())
		Expect(mgr.SelfAuthenticated("/api/v1/extensions/echo/api/x")).To(BeFalse())

		entry.Enabled = false
		cfg.Extensions = []config.ExtensionEntry{entry}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(mgr.SelfAuthenticated("/echo/x")).To(BeFalse())
	})

	It("answers 404 on the mount of an extension that is not running", func() {
		entry.Enabled = false
		cfg.Extensions = []config.ExtensionEntry{entry}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(do(http.MethodGet, "/echo/x").Code).To(Equal(http.StatusNotFound))

		cfg.Extensions = nil
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(do(http.MethodGet, "/echo/x").Code).To(Equal(http.StatusTeapot))
	})

	It("never claims a reserved prefix for a stopped extension", func() {
		entry.Enabled = false
		entry.Grants.Mounts = []string{"/movies"}
		cfg.Extensions = []config.ExtensionEntry{entry}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(do(http.MethodGet, "/movies").Code).To(Equal(http.StatusTeapot))
	})

	It("forces its own content policy on what a guest serves", func() {
		rr := do(http.MethodGet, "/echo/x")
		Expect(rr.Header().Get("Content-Security-Policy")).To(Equal(guestCSP))
		Expect(guestCSP).To(Equal(
			"sandbox; default-src 'none'; img-src 'self' data:; " +
				"media-src 'self'; style-src 'unsafe-inline'",
		))
		Expect(rr.Header().Get("X-Content-Type-Options")).To(Equal("nosniff"))
	})

	It("answers 404 for an extension subtree behind a dot segment", func() {
		Expect(do(http.MethodGet, "/api/v1/extensions/echo/api/../cards/x").Code).
			To(Equal(http.StatusNotFound))
	})

	It("withholds credentials from a guest", func() {
		rr := do(http.MethodGet, "/echo/headers", func(r *http.Request) {
			r.Header.Set("Cookie", "streamline_session=secret")
			r.Header.Set("Authorization", "Basic abc")
		})
		var got map[string][]string
		Expect(json.Unmarshal(rr.Body.Bytes(), &got)).To(Succeed())
		Expect(got).NotTo(HaveKey("Cookie"))
		Expect(got).To(HaveKey("Authorization"))
		Expect(do(http.MethodGet, "/echo/headers", asCaller).Body.String()).
			NotTo(ContainSubstring("Caller"))
	})

	Context("under /api/v1/extensions", func() {
		It("hands the guest the caller", func() {
			rr := do(http.MethodGet, "/api/v1/extensions/echo/api/ping", asCaller)
			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(rr.Body.String()).To(MatchJSON(`{"id":4,"role":"member"}`))

			rr = do(http.MethodGet, "/api/v1/extensions/echo/cards/token", asCaller)
			Expect(rr.Body.String()).To(Equal("/cards/token"))
		})

		It("keeps a settings card off the signed-in subtree", func() {
			Expect(
				do(
					http.MethodPut,
					"/api/v1/extensions/echo/cards/prefs",
					asCaller,
				).Code,
			).
				To(Equal(http.StatusNotFound))
			Expect(
				do(
					http.MethodGet,
					"/api/v1/extensions/echo/cards/prefs/x",
					asCaller,
				).Code,
			).
				To(Equal(http.StatusNotFound))
			Expect(
				do(
					http.MethodGet,
					"/api/v1/extensions/echo/cards/secret",
					asCaller,
				).Code,
			).
				NotTo(Equal(http.StatusNotFound))
		})

		It("strips the credentials the API accepted", func() {
			rr := do(
				http.MethodGet,
				"/api/v1/extensions/echo/api/headers",
				func(r *http.Request) {
					r.Header.Set("X-API-Key", "k")
					r.Header.Set("Authorization", "Bearer t")
					r.Header.Set("Cookie", "c=1")
				},
			)
			Expect(rr.Body.String()).NotTo(ContainSubstring("Bearer"))
			Expect(rr.Body.String()).NotTo(ContainSubstring("X-Api-Key"))
			Expect(rr.Body.String()).NotTo(ContainSubstring("Cookie"))
		})

		It("answers 404 for an unknown or stopped extension", func() {
			Expect(do(http.MethodGet, "/api/v1/extensions/nope/api/x").Code).
				To(Equal(http.StatusNotFound))

			entry.Enabled = false
			cfg.Extensions = []config.ExtensionEntry{entry}
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			Expect(do(http.MethodGet, "/api/v1/extensions/echo/api/x").Code).
				To(Equal(http.StatusNotFound))
		})
	})

	Context("a failing guest", func() {
		It("answers 500 and serves the next request", func() {
			rr := do(http.MethodGet, "/echo/trap")
			Expect(rr.Code).To(Equal(http.StatusInternalServerError))
			Expect(rr.Body.String()).To(ContainSubstring("internal error"))
			Expect(do(http.MethodGet, "/echo/ok").Code).To(Equal(http.StatusOK))
		})

		It("refuses a body over the limit", func() {
			rr := do(http.MethodGet, "/echo/oversize")
			Expect(rr.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Context("serve_file", func() {
		It("serves a present book file with the directive's headers", func() {
			id := bookFile("0123456789")
			rr := do(http.MethodGet, fmt.Sprintf("/echo/file/%d", id))
			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(rr.Body.String()).To(Equal("0123456789"))
			Expect(rr.Header().Get("Content-Type")).To(Equal("application/epub+zip"))
			Expect(rr.Header().Get("Content-Disposition")).
				To(Equal(`attachment; filename="ab.epub"`))
		})

		It("honours a Range header", func() {
			id := bookFile("0123456789")
			rr := do(
				http.MethodGet,
				fmt.Sprintf("/echo/file/%d", id),
				func(r *http.Request) {
					r.Header.Set("Range", "bytes=0-3")
				},
			)
			Expect(rr.Code).To(Equal(http.StatusPartialContent))
			body, err := io.ReadAll(rr.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body).To(Equal([]byte("0123")))
		})

		It("answers 404 for a file gone from disk", func() {
			id := bookFile("x")
			mf := client.MediaFile.GetX(ctx, id)
			Expect(os.Remove(mf.Path)).To(Succeed())
			Expect(do(http.MethodGet, fmt.Sprintf("/echo/file/%d", id)).Code).
				To(Equal(http.StatusNotFound))
		})

		It("answers 404 for a movie file", func() {
			path := filepath.Join(GinkgoT().TempDir(), "m.mkv")
			Expect(os.WriteFile(path, []byte("m"), 0o600)).To(Succeed())
			id := client.MediaFile.Create().
				SetPath(path).SetSize(1).SetQuality("1080p").SaveX(ctx).ID
			Expect(do(http.MethodGet, fmt.Sprintf("/echo/file/%d", id)).Code).
				To(Equal(http.StatusNotFound))
		})

		It("answers 404 without the grant", func() {
			id := bookFile("x")
			entry.Grants.Services = []string{"serve_file"}
			entry.Grants.Views = []string{"books"}
			cfg.Extensions = []config.ExtensionEntry{entry}
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			Expect(do(http.MethodGet, fmt.Sprintf("/echo/file/%d", id)).Code).
				To(Equal(http.StatusNotFound))
		})
	})

	Context("serve_poster", func() {
		It("hands a granted kind to the poster server", func() {
			rr := do(http.MethodGet, "/echo/poster/books/9")
			Expect(rr.Code).To(Equal(http.StatusOK))
			Expect(rr.Body.String()).To(Equal("poster"))
			Expect(posters.kind).To(Equal("books"))
			Expect(posters.id).To(Equal(uint32(9)))
		})

		It("answers 404 for a kind it has no view for", func() {
			Expect(do(http.MethodGet, "/echo/poster/movies/9").Code).
				To(Equal(http.StatusNotFound))
			Expect(do(http.MethodGet, "/echo/poster/artists/9").Code).
				To(Equal(http.StatusNotFound))
			Expect(posters.kind).To(BeEmpty())

			entry.Grants.Views = []string{"books"}
			cfg.Extensions = []config.ExtensionEntry{entry}
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			Expect(do(http.MethodGet, "/echo/poster/authors/9").Code).
				To(Equal(http.StatusNotFound))
		})
	})
})

var _ = DescribeTable("attachmentName", Label("unit"),
	func(in, want string) { Expect(attachmentName(in)).To(Equal(want)) },
	Entry("plain", "a.epub", "a.epub"),
	Entry("quotes and slashes", "a\"/b\\c.epub", "abc.epub"),
	Entry("control characters", "a\r\nb.epub", "ab.epub"),
	Entry("nothing left", "\"/", "download"),
)

var _ = Describe("send_file", Label("integration"), func() {
	var (
		ctx     = context.Background()
		music   string
		outside string
		logs    bytes.Buffer
		mgr     *Manager
		h       http.Handler
	)

	send := func(path string) *httptest.ResponseRecorder {
		GinkgoHelper()
		target := "/echo/send?" + url.Values{
			"path": {path}, "type": {"audio/flac"},
		}.Encode()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	BeforeEach(func() {
		root := GinkgoT().TempDir()
		music = filepath.Join(root, "music")
		outside = filepath.Join(root, "outside")
		Expect(os.MkdirAll(music, 0o750)).To(Succeed())
		Expect(os.MkdirAll(outside, 0o750)).To(Succeed())
		Expect(
			os.WriteFile(
				filepath.Join(music, "a.flac"),
				[]byte("0123456789"),
				0o600,
			),
		).To(Succeed())
		Expect(
			os.WriteFile(
				filepath.Join(outside, "secret"),
				[]byte("TOPSECRET"),
				0o600,
			),
		).To(Succeed())

		logs.Reset()
		GinkgoWriter.TeeTo(&logs)
		DeferCleanup(GinkgoWriter.ClearTeeWriters)

		cfg := configtest.Setup(map[string]any{
			"library": map[string]any{"music_path": music},
		})
		dir := cfg.DataDir
		corePath := filepath.Join(dir, "core.db")
		client, err := db.Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })

		mgr = NewManager(dir, corePath, nil, nil)
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })

		wasm, err := filepath.Abs("testdata/echo/echo.wasm")
		Expect(err).NotTo(HaveOccurred())
		cfg.Extensions = []config.ExtensionEntry{{
			Name: "echo", Source: wasm, Enabled: true,
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/echo"},
				Views:    []string{"books"},
				Services: []string{"serve_file"},
				Folders:  []string{"music"},
			},
		}}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		h = mgr.Middleware(fakeFiles{client: client}, &fakePosters{})(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}),
		)
	})

	It("serves a granted file with ranges", func() {
		req := httptest.NewRequest(
			http.MethodGet,
			"/echo/send?"+url.Values{
				"path": {filepath.Join(music, "a.flac")},
			}.Encode(),
			nil,
		)
		req.Header.Set("Range", "bytes=2-4")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		Expect(rr.Code).To(Equal(http.StatusPartialContent))
		Expect(rr.Body.String()).To(Equal("234"))
		Expect(rr.Header().Get("Content-Disposition")).To(BeEmpty())
	})

	It("names the download when the guest gives a filename", func() {
		req := httptest.NewRequest(
			http.MethodGet,
			"/echo/send?"+url.Values{
				"path": {filepath.Join(music, "a.flac")}, "name": {"x.flac"},
			}.Encode(),
			nil,
		)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		Expect(rr.Header().Get("Content-Disposition")).
			To(Equal(`attachment; filename="x.flac"`))
	})

	It("answers 404 for a path outside every granted folder", func() {
		rr := send(filepath.Join(outside, "secret"))
		Expect(rr.Code).To(Equal(http.StatusNotFound))
		Expect(rr.Body.String()).NotTo(ContainSubstring("TOPSECRET"))
	})

	It("answers 404 for a dot-dot path", func() {
		rr := send(music + "/../outside/secret")
		Expect(rr.Code).To(Equal(http.StatusNotFound))
		Expect(rr.Body.String()).NotTo(ContainSubstring("TOPSECRET"))
	})

	It("answers 404 for a link leaving the folder", func() {
		Expect(os.Symlink(
			filepath.Join(outside, "secret"), filepath.Join(music, "escape"),
		)).To(Succeed())

		rr := send(filepath.Join(music, "escape"))
		Expect(rr.Code).To(Equal(http.StatusNotFound))
		Expect(rr.Body.String()).NotTo(ContainSubstring("TOPSECRET"))
	})

	It("answers 404 for a directory and for a relative path", func() {
		Expect(send(music).Code).To(Equal(http.StatusNotFound))
		Expect(send("a.flac").Code).To(Equal(http.StatusNotFound))
	})

	It("answers 404 and warns when the folder is not granted", func() {
		config.Get().Extensions[0].Grants.Folders = nil
		Expect(mgr.Reconcile(ctx)).To(Succeed())

		rr := send(filepath.Join(music, "a.flac"))
		Expect(rr.Code).To(Equal(http.StatusNotFound))
		Expect(logs.String()).To(ContainSubstring("outside its folders"))
	})

	It("returns the store connection before streaming", func() {
		gate := make(chan struct{})
		const slow = 4
		var started sync.WaitGroup
		for range slow {
			started.Add(1)
			go func() {
				defer GinkgoRecover()
				req := httptest.NewRequest(
					http.MethodGet,
					"/echo/send?"+url.Values{
						"path": {filepath.Join(music, "a.flac")},
					}.Encode(),
					nil,
				)
				h.ServeHTTP(&blockedWriter{
					ResponseRecorder: httptest.NewRecorder(),
					gate:             gate,
					started:          &started,
				}, req)
			}()
		}
		started.Wait()

		answered := make(chan int, 1)
		go func() {
			defer GinkgoRecover()
			answered <- send(filepath.Join(music, "a.flac")).Code
		}()
		Eventually(answered).Should(Receive())
		close(gate)
	})
})

// blockedWriter stalls its first write until the gate opens, like a client
// that reads slowly.
type blockedWriter struct {
	*httptest.ResponseRecorder
	gate    chan struct{}
	started *sync.WaitGroup
	once    sync.Once
}

func (b *blockedWriter) Write(p []byte) (int, error) {
	b.once.Do(func() {
		b.started.Done()
		<-b.gate
	})
	return b.ResponseRecorder.Write(p)
}
