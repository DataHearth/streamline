package extension

import (
	"context"
	"net/http"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("mergeChanges", Label("unit"), func() {
	It("joins the ids of each kind once, in order", func() {
		got := mergeChanges(
			LibraryChange{Movies: []uint32{3, 1}, Episodes: []uint32{9}},
			LibraryChange{Movies: []uint32{1, 2}, Series: []uint32{4}},
		)
		Expect(got).To(Equal(LibraryChange{
			Movies: []uint32{1, 2, 3}, Series: []uint32{4}, Episodes: []uint32{9},
		}))
	})

	It("asks for the whole folder past the id limit", func() {
		many := make([]uint32, maxHookIDs+1)
		for i := range many {
			many[i] = uint32(i + 1)
		}
		got := mergeChanges(
			LibraryChange{Movies: many, Series: []uint32{1}},
			LibraryChange{Episodes: many},
		)
		Expect(got).To(Equal(LibraryChange{Full: []string{"movies", "series"}}))
	})
})

var _ = Describe("library hook", Label("integration"), func() {
	var (
		ctx = context.Background()
		cfg *config.Config
		mgr *Manager
	)

	start := func(wasm string, services ...string) {
		GinkgoHelper()
		src, err := filepath.Abs(filepath.Join("testdata/echo", wasm))
		Expect(err).NotTo(HaveOccurred())
		cfg.Extensions = []config.ExtensionEntry{{
			Name: "echo", Source: src, Enabled: true,
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/", "/inner"},
				Views:    []string{"books"},
				Services: services,
				Folders:  folderGrantsFor(wasm),
			},
		}}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		for _, s := range mgr.Statuses() {
			Expect(s.State).To(Equal(StateRunning), s.Reason)
		}
	}

	hooked := func() string {
		GinkgoHelper()
		resp, ok, err := mgr.Call(ctx, "echo", Request{
			Method: http.MethodGet, Path: "/hooked",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		return string(resp.Body)
	}

	BeforeEach(func() {
		cfg = configtest.Setup()
		corePath := filepath.Join(cfg.DataDir, "core.db")
		client, err := db.Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		mgr = NewManager(cfg.DataDir, corePath, nil, &fakeScanner{})
		mgr.library.window = 0
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })
	})

	It("reaches a module that lists it, with what changed", func() {
		start("echo-full.wasm", "serve_file", "ffmpeg", "library_scan")
		mgr.LibraryChanged(LibraryChange{Movies: []uint32{12}})
		mgr.LibraryChanged(LibraryChange{})
		Expect(hooked()).To(Equal(`[["{\"movies\":[12]}"]]`))
	})

	It("is not sent to a module that does not list it", func() {
		start("echo.wasm", "serve_file")
		mgr.LibraryChanged(LibraryChange{Movies: []uint32{12}})
		Expect(hooked()).To(Equal("null"), "the table the hook creates is not there")
	})

	It("cannot be reached by a client", func() {
		start("echo-full.wasm", "serve_file", "ffmpeg", "library_scan")
		run, tgt, ok := mgr.route("/extensions/echo/hooks/library")
		Expect(ok).To(BeTrue())
		DeferCleanup(run.Release)
		Expect(tgt.mount).NotTo(BeEmpty(), "a client's request names its mount")
		_, _, ok = mgr.route("/api/v1/extensions/echo/hooks/library")
		Expect(ok).To(BeFalse())
	})
})
