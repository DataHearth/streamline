package extension

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Manager", Label("integration"), func() {
	var (
		ctx     = context.Background()
		cfg     *config.Config
		mgr     *Manager
		echoWas string
		echo    config.ExtensionEntry
	)

	status := func(name string) Status {
		GinkgoHelper()
		for _, s := range mgr.Statuses() {
			if s.Name == name {
				return s
			}
		}
		Fail("no status for " + name)
		return Status{}
	}

	BeforeEach(func() {
		cfg = configtest.Setup()
		dir := GinkgoT().TempDir()
		corePath := filepath.Join(dir, "core.db")
		client, err := db.Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })

		mgr = NewManager(dir, corePath, nil, nil)
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })

		echoWas, err = filepath.Abs("testdata/echo/echo.wasm")
		Expect(err).NotTo(HaveOccurred())
		echo = config.ExtensionEntry{
			Name:    "echo",
			Source:  echoWas,
			Enabled: true,
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/echo"},
				Views:    []string{"books"},
				Services: []string{"serve_file"},
			},
		}
		cfg.Extensions = []config.ExtensionEntry{echo}
	})

	It("runs an extension and mounts it", func() {
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(status("echo").State).To(Equal(StateRunning))
		Expect(status("echo").Manifest).NotTo(BeNil())

		r, mount, ok := mgr.Acquire("/echo")
		Expect(ok).To(BeTrue())
		defer r.Release()
		Expect(mount.Auth).To(Equal("self"))
		_, _, ok = mgr.Acquire("/other")
		Expect(ok).To(BeFalse())
	})

	It("asks for approval when the manifest wants more than was granted", func() {
		echo.Grants.Views = nil
		cfg.Extensions = []config.ExtensionEntry{echo}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		st := status("echo")
		Expect(st.State).To(Equal(StateNeedsApproval))
		_, _, ok := mgr.Acquire("/echo")
		Expect(ok).To(BeFalse())
	})

	It("fails on a granted view or service that does not exist", func() {
		echo.Grants.Views = []string{"books", "sessions"}
		cfg.Extensions = []config.ExtensionEntry{echo}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(status("echo").State).To(Equal(StateFailed))

		echo.Grants.Views = []string{"books"}
		echo.Grants.Services = []string{"serve_file", "shell"}
		cfg.Extensions = []config.ExtensionEntry{echo}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(status("echo").State).To(Equal(StateFailed))
	})

	It("fails on a wrong sha256 and on http", func() {
		echo.SHA256 = sumHex([]byte("other"))
		bad := echo
		bad.Name = "bad-sha"
		plain := echo
		plain.Name = "plain"
		plain.SHA256 = sumHex(nil)
		plain.Source = "http://example.com/echo.wasm"
		cfg.Extensions = []config.ExtensionEntry{bad, plain}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(status("bad-sha").State).To(Equal(StateFailed))
		Expect(status("bad-sha").Reason).To(ContainSubstring("sha256"))
		Expect(status("plain").State).To(Equal(StateFailed))
	})

	It(
		"fails on a name that differs from the manifest's, and runs the others",
		func() {
			wrong := echo
			wrong.Name = "other"
			cfg.Extensions = []config.ExtensionEntry{wrong, echo}
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			Expect(status("other").State).To(Equal(StateFailed))
			Expect(status("echo").State).To(Equal(StateRunning))
		},
	)

	It("fails when the core database is in memory", func() {
		mem := NewManager(GinkgoT().TempDir(), ":memory:", nil, nil)
		DeferCleanup(func() { Expect(mem.Close(ctx)).To(Succeed()) })
		Expect(mem.Reconcile(ctx)).To(Succeed())
		st := mem.Statuses()[0]
		Expect(st.State).To(Equal(StateFailed))
		Expect(st.Reason).To(ContainSubstring("on-disk"))
	})

	It("keeps the database of a disabled extension", func() {
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		echo.Enabled = false
		cfg.Extensions = []config.ExtensionEntry{echo}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(status("echo").State).To(Equal(StateDisabled))
		Expect(mgr.DataPaths()["echo"]).To(BeAnExistingFile())
		_, _, ok := mgr.Acquire("/echo")
		Expect(ok).To(BeFalse())
	})

	It("keeps the running module when nothing changed", func() {
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		before := mgr.running["echo"]
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(mgr.running["echo"].module).To(BeIdenticalTo(before.module))
		Expect(before.Done()).NotTo(BeClosed())
	})

	It("replaces a module whose grants changed", func() {
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		before := mgr.running["echo"]
		echo.Grants.Mounts = []string{"/echo", "/extra"}
		cfg.Extensions = []config.ExtensionEntry{echo}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(mgr.running["echo"]).NotTo(BeIdenticalTo(before))
		Eventually(before.Done()).Should(BeClosed())
	})

	Describe("folder grants", func() {
		var full config.ExtensionEntry

		BeforeEach(func() {
			wasm, err := filepath.Abs("testdata/echo/echo-full.wasm")
			Expect(err).NotTo(HaveOccurred())
			full = echo
			full.Source = wasm
			full.Grants.Services = []string{"serve_file", "ffmpeg", "library_scan"}
			full.Grants.Folders = []string{"music", "artwork", "cache"}
		})

		It(
			"holds a module asking for a folder the entry lacks at needs_approval",
			func() {
				full.Grants.Folders = nil
				cfg.Extensions = []config.ExtensionEntry{full}
				Expect(mgr.Reconcile(ctx)).To(Succeed())
				Expect(status("echo").State).To(Equal(StateNeedsApproval))
			},
		)

		It("fails an entry granting an unknown folder", func() {
			full.Grants.Folders = []string{"music", "etc"}
			cfg.Extensions = []config.ExtensionEntry{full}
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			st := status("echo")
			Expect(st.State).To(Equal(StateFailed))
			Expect(
				st.Reason,
			).To(ContainSubstring(`granted folder "etc" does not exist`))
		})

		It("replaces the instance when only the folder grants change", func() {
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			before := mgr.running["echo"]
			Expect(before).NotTo(BeNil())

			echo.Grants.Folders = []string{"cache"}
			cfg.Extensions = []config.ExtensionEntry{echo}
			Expect(mgr.Reconcile(ctx)).To(Succeed())
			Expect(mgr.running["echo"]).NotTo(BeIdenticalTo(before))
		})
	})

	It("lets a held request finish before the old module closes", func() {
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		r, _, ok := mgr.Acquire("/echo")
		Expect(ok).To(BeTrue())

		held := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			c, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_, err := r.Module().
				Handle(c, fakeSQL{}, Request{Method: "GET", Path: "/spin"}, nil)
			held <- err
		}()
		time.Sleep(100 * time.Millisecond)

		echo.Enabled = false
		cfg.Extensions = []config.ExtensionEntry{echo}
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		_, _, again := mgr.Acquire("/echo")
		Expect(again).To(BeFalse())
		Expect(r.Done()).NotTo(BeClosed())

		Eventually(held, 5*time.Second).Should(Receive(HaveOccurred()))
		Expect(r.Done()).NotTo(BeClosed())
		r.Release()
		Eventually(r.Done()).Should(BeClosed())
	})
})

var _ = Describe("mount rules", Label("unit"), func() {
	DescribeTable("checkMounts",
		func(prefix string, table map[string]mountEntry, fails bool) {
			err := checkMounts(Manifest{Mounts: []Mount{{Prefix: prefix}}}, table)
			if fails {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
		},
		Entry("a plain prefix", "/opds", map[string]mountEntry{}, false),
		Entry("the Subsonic API", "/rest", map[string]mountEntry{}, false),
		Entry("Subsonic's public shares", "/share", map[string]mountEntry{}, false),
		Entry("a reserved prefix", "/api", map[string]mountEntry{}, true),
		Entry("an SPA route", "/movies", map[string]mountEntry{}, true),
		Entry("no leading slash", "opds", map[string]mountEntry{}, true),
		Entry("the root", "/", map[string]mountEntry{}, true),
		Entry("two segments", "/a/b", map[string]mountEntry{}, true),
		Entry("already held", "/opds", map[string]mountEntry{"/opds": {}}, true),
	)

	It("reserves every first-level SPA route", func() {
		entries, err := os.ReadDir("../../web/app/routes")
		Expect(err).NotTo(HaveOccurred())
		for _, e := range entries {
			if e.IsDir() {
				Expect(reservedPrefixes).To(ContainElement("/" + e.Name()))
			}
		}
	})
})
