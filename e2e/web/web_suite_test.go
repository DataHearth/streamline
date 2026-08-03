package web

import (
	"os"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/testutil"
)

var (
	baseURL string
	browser *rod.Browser
)

// pageBudget is a whole-spec deadline shared by every later call on a page
// returned from newPage/newSessionPage — a hung step fails the spec instead of
// burning the suite timeout. A multi-step flow that legitimately needs more
// re-arms it at a step boundary with
// `page = page.CancelTimeout().Timeout(pageBudget)` rather than raising it.
const pageBudget = 15 * time.Second

func TestWeb(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Web Suite", Label("web"))
}

var _ = BeforeSuite(func() {
	chromeBin := os.Getenv("CHROME_PATH")
	if chromeBin == "" {
		Skip("CHROME_PATH not set — run inside the nix devshell")
	}

	DeferCleanup(testutil.InstallSlog())
	baseURL = apptest.Start()
	sessionJWT = mintSessionJWT()

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
// never leaks between specs.
func newPage(path string) *rod.Page {
	GinkgoHelper()
	return openPage(newContext(), path)
}

// newSessionPage is newPage with the seed admin's session cookie installed
// before the first navigation, so the SPA hydrates authenticated. Specs must
// use it instead of driving the login form: auth_test.go plus the BeforeSuite
// mint already spend 4 of the 5 attempts auth.Limiter allows per IP.
func newSessionPage(path string) *rod.Page {
	GinkgoHelper()
	b := newContext()
	Expect(b.SetCookies([]*proto.NetworkCookieParam{{
		Name:     auth.SessionCookie,
		Value:    sessionJWT,
		URL:      baseURL,
		Path:     "/",
		HTTPOnly: true,
	}})).To(Succeed())
	return openPage(b, path)
}

func newContext() *rod.Browser {
	GinkgoHelper()
	b := browser.MustIncognito()
	DeferCleanup(func() { Expect(b.Close()).To(Succeed()) })
	return b
}

// openPage sets the viewport before navigating; it must stay >= 1024px wide,
// as the sidebar (and its Sign out button) is lg:-gated and several components
// branch on window.innerWidth at mount.
func openPage(b *rod.Browser, path string) *rod.Page {
	GinkgoHelper()
	page := b.MustPage().Timeout(pageBudget)
	page.MustSetViewport(1440, 900, 1, false)
	page.MustNavigate(baseURL + path)
	return page
}
