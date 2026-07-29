package e2e

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/server"
	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

const (
	adminEmail    = "e2e-admin@streamline.local"
	adminPassword = "e2e-Admin-Passw0rd!"
)

var (
	baseURL string
	browser *rod.Browser
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	chromeBin := os.Getenv("CHROME_PATH")
	if chromeBin == "" {
		Skip("CHROME_PATH not set — run inside the nix devshell")
	}

	DeferCleanup(testutil.InstallSlog())
	// Same seam as the server suite: the HTTP access logger writes to
	// stderr with no injection point; repoint it at GinkgoWriter.
	prev := observability.StderrSink
	observability.StderrSink = GinkgoWriter
	DeferCleanup(func() { observability.StderrSink = prev })

	configtest.Setup(map[string]any{
		"auth": map[string]any{
			"session_secret": "e2e-session-secret",
			"seed_admin": map[string]any{
				"email":    adminEmail,
				"password": adminPassword,
			},
		},
	})

	app, err := server.NewFromConfig(context.Background())
	Expect(err).NotTo(HaveOccurred())
	srv := httptest.NewServer(app.Server.Router())
	baseURL = srv.URL
	DeferCleanup(func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		Expect(
			errors.Join(
				app.Auth.Shutdown(ctx),
				app.HTTPLogger.Close(),
				app.DB.Close(),
			),
		).To(Succeed())
	})

	// CHROME_PATH comes from the nix devshell; rod's auto-download
	// fallback produces a chromium that cannot run on NixOS.
	l := launcher.New().Bin(chromeBin)
	controlURL, err := l.Launch()
	Expect(err).NotTo(HaveOccurred())
	// Cleanup blocks until the browser process exits; Kill guarantees it does
	// even when Connect below fails and nothing ever closes the browser.
	DeferCleanup(func() { l.Kill(); l.Cleanup() })

	browser = rod.New().ControlURL(controlURL)
	Expect(browser.Connect()).To(Succeed())
	DeferCleanup(func() { Expect(browser.Close()).To(Succeed()) })
})

// newPage opens path in a fresh incognito context so the session cookie
// never leaks between specs. The 15s deadline is a whole-spec budget shared
// by every later call on the page — a hung step fails the spec instead of
// the suite timeout. The viewport is set before navigation and must stay
// >= 1024px wide: the sidebar (and its Sign out button) is lg:-gated and
// several components branch on window.innerWidth at mount.
func newPage(path string) *rod.Page {
	GinkgoHelper()
	b := browser.MustIncognito()
	DeferCleanup(func() { Expect(b.Close()).To(Succeed()) })
	page := b.MustPage().Timeout(15 * time.Second)
	page.MustSetViewport(1440, 900, 1, false)
	page.MustNavigate(baseURL + path)
	return page
}
