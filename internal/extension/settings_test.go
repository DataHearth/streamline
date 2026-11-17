package extension

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("settings from the config file", Label("integration"), func() {
	var (
		ctx    = context.Background()
		cfg    *config.Config
		mgr    *Manager
		secret string
	)

	entry := func(wasm string) config.ExtensionEntry {
		GinkgoHelper()
		src, err := filepath.Abs(filepath.Join("testdata/echo", wasm))
		Expect(err).NotTo(HaveOccurred())
		return config.ExtensionEntry{
			Name: "echo", Source: src, Enabled: true,
			Hosts: []string{"engine.lan:8097"},
			Grants: config.ExtensionGrants{
				Mounts:   []string{"/", "/inner"},
				Views:    []string{"books"},
				Services: servicesFor(wasm),
				Folders:  folderGrantsFor(wasm),
			},
			Settings: map[string]any{
				"engine.url": "http://engine.lan:8097",
				"origins":    []any{"https://player.lan"},
			},
			SecretFiles: map[string]string{"engine": secret},
		}
	}

	status := func() Status {
		GinkgoHelper()
		Expect(mgr.Reconcile(ctx)).To(Succeed())
		Expect(mgr.Statuses()).To(HaveLen(1))
		return mgr.Statuses()[0]
	}

	given := func() string {
		GinkgoHelper()
		resp, ok, err := mgr.Call(ctx, "echo", Request{
			Method: http.MethodGet, Path: "/given",
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
		DeferCleanup(func() { Expect(mgr.Close(ctx)).To(Succeed()) })
		secret = filepath.Join(GinkgoT().TempDir(), "engine-secret")
		Expect(os.WriteFile(secret, []byte("s3cret\n"), 0o600)).To(Succeed())
	})

	It("hands settings and secrets to the guest on every call", func() {
		cfg.Extensions = []config.ExtensionEntry{entry("echo-full.wasm")}
		Expect(status().State).To(Equal(StateRunning))
		Expect(given()).To(MatchJSON(`{
			"settings": {
				"engine.url": "http://engine.lan:8097",
				"origins": ["https://player.lan"]
			},
			"secrets": {"engine": "s3cret"}
		}`))
	})

	It("picks up a changed setting and secret without a new module", func() {
		cfg.Extensions = []config.ExtensionEntry{entry("echo-full.wasm")}
		Expect(status().State).To(Equal(StateRunning))
		run, ok := mgr.AcquireNamed("echo")
		Expect(ok).To(BeTrue())
		run.Release()

		cfg.Extensions[0].Settings = map[string]any{"engine.url": "http://b:1"}
		Expect(os.WriteFile(secret, []byte("other"), 0o600)).To(Succeed())
		Expect(status().State).To(Equal(StateRunning))
		Expect(given()).To(MatchJSON(`{
			"settings": {"engine.url": "http://b:1"},
			"secrets": {"engine": "other"}
		}`))
		again, ok := mgr.AcquireNamed("echo")
		Expect(ok).To(BeTrue())
		again.Release()
		Expect(again).To(BeIdenticalTo(run))
	})

	It("fails the entry on a missing or empty secret file", func() {
		cfg.Extensions = []config.ExtensionEntry{entry("echo-full.wasm")}
		Expect(os.WriteFile(secret, []byte("\n"), 0o600)).To(Succeed())
		st := status()
		Expect(st.State).To(Equal(StateFailed))
		Expect(st.Reason).To(ContainSubstring(`secret "engine"`))

		Expect(os.Remove(secret)).To(Succeed())
		st = status()
		Expect(st.State).To(Equal(StateFailed))
		Expect(st.Reason).To(ContainSubstring("cannot read"))
	})

	It("fails the entry with the guest's reason when it refuses", func() {
		e := entry("echo-full.wasm")
		e.Settings["refused"] = true
		cfg.Extensions = []config.ExtensionEntry{e}
		st := status()
		Expect(st.State).To(Equal(StateFailed))
		Expect(st.Reason).To(ContainSubstring("refused is not a setting"))
		Expect(st.Reason).NotTo(ContainSubstring("s3cret"))
		_, ok := mgr.AcquireNamed("echo")
		Expect(ok).To(BeFalse())
	})

	It("stops a running extension whose new settings it refuses", func() {
		cfg.Extensions = []config.ExtensionEntry{entry("echo-full.wasm")}
		Expect(status().State).To(Equal(StateRunning))
		cfg.Extensions[0].Settings = map[string]any{"refused": 1}
		Expect(status().State).To(Equal(StateFailed))
		_, ok := mgr.AcquireNamed("echo")
		Expect(ok).To(BeFalse())
	})

	It("runs a module that does not ask to check them", func() {
		e := entry("echo.wasm")
		e.Settings["refused"] = true
		cfg.Extensions = []config.ExtensionEntry{e}
		Expect(status().State).To(Equal(StateRunning))
		Expect(given()).To(ContainSubstring(`"refused":true`))
	})
})

func servicesFor(wasm string) []string {
	if wasm == "echo-full.wasm" {
		return []string{"serve_file", "ffmpeg", "library_scan"}
	}
	return []string{"serve_file"}
}
