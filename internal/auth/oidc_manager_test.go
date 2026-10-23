package auth

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("OIDCManager", Label("unit", "auth"), func() {
	var mgr OIDCManager

	BeforeEach(func() {
		mgr = NewOIDCManager()
	})

	Describe("NewOIDCManager", func() {
		It("creates empty manager", func() {
			_, ok := mgr.Get("nonexistent")
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Register and Get", func() {
		It("registers and retrieves a provider by name", func() {
			By("Registering a test provider")
			mgr.(*oidcManager).Register(&OIDCProvider{Name: "test-provider"})

			By("Retrieving it")
			p, ok := mgr.Get("test-provider")
			Expect(ok).To(BeTrue())
			Expect(p.Name).To(Equal("test-provider"))
		})

		It("returns false for unknown provider", func() {
			_, ok := mgr.Get("unknown")
			Expect(ok).To(BeFalse())
		})

		It("overwrites existing provider on duplicate Register", func() {
			mgr.(*oidcManager).Register(&OIDCProvider{Name: "p1"})
			mgr.(*oidcManager).Register(&OIDCProvider{Name: "p1"})
			_, ok := mgr.Get("p1")
			Expect(ok).To(BeTrue())
		})
	})

	Describe("Init", func() {
		It("silently skips providers with unreachable issuer", func() {
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret": "test-secret-key-for-jwt",
					"session_ttl":    "1h",
					"oidc": []map[string]any{
						{
							"name":          "broken",
							"issuer":        "http://127.0.0.1:1/nonexistent",
							"client_id":     "test",
							"client_secret": "test",
						},
					},
				},
			})

			mgr.Init(context.Background(), "http://localhost:8080")

			_, ok := mgr.Get("broken")
			Expect(ok).To(BeFalse())
		})
	})
})
