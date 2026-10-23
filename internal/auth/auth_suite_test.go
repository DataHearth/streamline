package auth

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

func TestAuth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Auth Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())
})

// defaultAuthConfig seeds the config singleton with session credentials
// sufficient for New() construction. Specs that need different auth modes,
// trusted networks, or registration modes call configtest.Setup again with
// their own map inside a nested BeforeEach.
var _ = BeforeEach(func() {
	configtest.Setup(map[string]any{
		"auth": map[string]any{
			"session_secret": "test-secret-key-for-jwt",
			"session_ttl":    "1h",
			"lockout": map[string]any{
				"threshold": 10,
				"window":    "15m",
				"duration":  "15m",
			},
		},
	})
})

// newTestService constructs an auth under test, failing the spec on
// construction errors. Used in place of direct New() calls so tests don't
// need to handle construction errors inline. Returns the concrete *auth so
// in-package specs can exercise unexported methods (Register, CreateSession,
// etc.) that the public Manager interface doesn't expose.
func newTestService(client *ent.Client) *auth {
	GinkgoHelper()
	svc, err := New(db.New(client))
	Expect(err).NotTo(HaveOccurred())
	return svc.(*auth)
}
