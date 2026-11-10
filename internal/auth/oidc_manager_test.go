package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

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

		It("asks for openid email profile plus each configured scope once", func() {
			var issuer string
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"issuer":                 issuer,
						"authorization_endpoint": issuer + "/authorize",
						"token_endpoint":         issuer + "/token",
						"jwks_uri":               issuer + "/jwks",
					})
				},
			))
			DeferCleanup(srv.Close)
			issuer = srv.URL

			provider := func(name string, scopes ...string) map[string]any {
				return map[string]any{
					"name":          name,
					"issuer":        issuer,
					"client_id":     "test",
					"client_secret": "test",
					"scopes":        scopes,
				}
			}
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret": "test-secret-key-for-jwt",
					"session_ttl":    "1h",
					"oidc": []map[string]any{
						provider("dex", "groups", "email", "groups"),
						provider("google"),
					},
				},
			})

			mgr.Init(context.Background(), "http://localhost:8080")

			scopeParam := func(name string) []string {
				GinkgoHelper()
				p, ok := mgr.Get(name)
				Expect(ok).To(BeTrue())
				u, err := url.Parse(p.OAuth2.AuthCodeURL("state"))
				Expect(err).NotTo(HaveOccurred())
				return strings.Fields(u.Query().Get("scope"))
			}
			By("sending the extra scope on the provider that names it")
			Expect(scopeParam("dex")).
				To(Equal([]string{"openid", "email", "profile", "groups"}))
			By("sending only the base scopes on the provider that names none")
			Expect(scopeParam("google")).
				To(Equal([]string{"openid", "email", "profile"}))
		})
	})
})
