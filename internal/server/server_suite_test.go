package server

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func TestServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Server Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())

	// The HTTP access logger is constructed inside BuildApp with
	// output="stderr" (no injection seam), so its JSON lines would
	// interleave with spec progress. Repoint the stderr sink at
	// GinkgoWriter — surfaced only on failure / -v, like InstallSlog.
	prev := observability.StderrSink
	observability.StderrSink = GinkgoWriter
	DeferCleanup(func() { observability.StderrSink = prev })
})

// Seed the config singleton with defaults + sensible test values for every
// spec. Specs that need specific overrides (registration_mode toggles,
// seed_admin, oidc, etc.) call configtest.Setup again inside their own
// BeforeEach. Handlers that read config.Get() (TMDB client, auth middleware,
// web_auth handlers) otherwise see defaults from configtest.
var _ = BeforeEach(func() {
	configtest.Setup(map[string]any{
		"auth": map[string]any{
			"session_secret": "test-secret-key-for-jwt",
			"session_ttl":    "1h",
		},
		"metadata": map[string]any{
			"tmdb_api_key": "test-key",
		},
	})
})
