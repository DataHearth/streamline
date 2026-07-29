package api

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// oidcDiscoveryStub serves an in-process issuer. config.AddOIDCProvider probes
// <issuer>/.well-known/openid-configuration and only checks for HTTP 200, so a
// bare status write is a complete stand-in.
func oidcDiscoveryStub() string {
	GinkgoHelper()
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/.well-known/openid-configuration" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		},
	))
	DeferCleanup(srv.Close)
	return srv.URL
}

// createOIDCProvider registers a provider backed by an in-process issuer and
// schedules its removal. Cleanup is registered before the first assertion so a
// later failure cannot leak the entity; 404 covers specs that delete it
// themselves.
func createOIDCProvider(name string) string {
	GinkgoHelper()
	issuer := oidcDiscoveryStub()
	resp := post("/api/v1/config/oidc", adminAuth, map[string]any{
		"name":          name,
		"issuer":        issuer,
		"client_id":     "e2e-client",
		"client_secret": "e2e-secret",
	})
	defer resp.Body.Close()
	DeferCleanup(func() {
		cleanup := del("/api/v1/config/oidc/"+name, adminAuth, nil)
		defer cleanup.Body.Close()
		Expect(cleanup.StatusCode).To(BeElementOf(
			http.StatusNoContent, http.StatusNotFound,
		))
	})
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	return issuer
}

var _ = Describe("REST API config", Label("e2e"), func() {
	Describe("/config/auth", func() {
		It("returns the runtime auth configuration", func() {
			resp := get("/api/v1/config/auth", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var cfg struct {
				RegistrationMode string `json:"registration_mode"`
				SessionTtl       string `json:"session_ttl"`
				OidcDefaultRole  string `json:"oidc_default_role"`
			}
			decode(resp, &cfg)
			Expect(cfg.RegistrationMode).To(BeElementOf(
				"disabled", "open", "invite",
			))
			Expect(cfg.SessionTtl).NotTo(BeEmpty())
			Expect(cfg.OidcDefaultRole).NotTo(BeEmpty())
		})

		It("patches the registration mode", func() {
			before := get("/api/v1/config/auth", adminAuth)
			defer before.Body.Close()
			var prev struct {
				RegistrationMode string `json:"registration_mode"`
			}
			decode(before, &prev)
			DeferCleanup(func() {
				restore := patch("/api/v1/config/auth", adminAuth, map[string]any{
					"registration_mode": prev.RegistrationMode,
				})
				defer restore.Body.Close()
				Expect(restore.StatusCode).To(Equal(http.StatusOK))
			})

			resp := patch("/api/v1/config/auth", adminAuth, map[string]any{
				"registration_mode": "invite",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var cfg struct {
				RegistrationMode string `json:"registration_mode"`
			}
			decode(resp, &cfg)
			Expect(cfg.RegistrationMode).To(Equal("invite"))
		})

		It("422s an unparseable session_ttl", func() {
			resp := patch("/api/v1/config/auth", adminAuth, map[string]any{
				"session_ttl": "not-a-duration",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("403s for a non-admin", func() {
			read := get("/api/v1/config/auth", viewerAuth)
			defer read.Body.Close()
			Expect(read.StatusCode).To(Equal(http.StatusForbidden))

			write := patch("/api/v1/config/auth", viewerAuth, map[string]any{
				"registration_mode": "open",
			})
			defer write.Body.Close()
			Expect(write.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("/config/oidc", func() {
		// restart.Mark is one-way for the life of the process, so creating a
		// provider first makes restart_required deterministically true no
		// matter what order the specs run in.
		It("lists providers with the restart flag", func() {
			createOIDCProvider("e2e-oidc-list")

			resp := get("/api/v1/config/oidc", adminAuth)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var list struct {
				Providers []struct {
					Name string `json:"name"`
				} `json:"providers"`
				RestartRequired bool `json:"restart_required"`
			}
			decode(resp, &list)
			Expect(list.Providers).To(ContainElement(
				HaveField("Name", "e2e-oidc-list"),
			))
			Expect(list.RestartRequired).To(BeTrue())
		})

		It("creates, reads, patches and deletes a provider", func() {
			issuer := createOIDCProvider("e2e-oidc")

			read := get("/api/v1/config/oidc/e2e-oidc", adminAuth)
			defer read.Body.Close()
			Expect(read.StatusCode).To(Equal(http.StatusOK))
			var view struct {
				Name            string `json:"name"`
				Issuer          string `json:"issuer"`
				ClientId        string `json:"client_id"`
				ClientSecretSet bool   `json:"client_secret_set"`
			}
			decode(read, &view)
			Expect(view.Name).To(Equal("e2e-oidc"))
			Expect(view.Issuer).To(Equal(issuer))
			Expect(view.ClientId).To(Equal("e2e-client"))
			Expect(view.ClientSecretSet).To(BeTrue())

			patched := patch(
				"/api/v1/config/oidc/e2e-oidc",
				adminAuth,
				map[string]any{"client_id": "e2e-client-2"},
			)
			defer patched.Body.Close()
			Expect(patched.StatusCode).To(Equal(http.StatusOK))
			decode(patched, &view)
			Expect(view.ClientId).To(Equal("e2e-client-2"))

			deleted := del("/api/v1/config/oidc/e2e-oidc", adminAuth, nil)
			defer deleted.Body.Close()
			Expect(deleted.StatusCode).To(Equal(http.StatusNoContent))
		})

		It("409s a duplicate provider name", func() {
			issuer := createOIDCProvider("e2e-oidc-dup")
			resp := post("/api/v1/config/oidc", adminAuth, map[string]any{
				"name":          "e2e-oidc-dup",
				"issuer":        issuer,
				"client_id":     "e2e-client",
				"client_secret": "e2e-secret",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("422s an unreachable issuer", func() {
			resp := post("/api/v1/config/oidc", adminAuth, map[string]any{
				"name":          "e2e-oidc-unreachable",
				"issuer":        "http://127.0.0.1:1",
				"client_id":     "e2e-client",
				"client_secret": "e2e-secret",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("404s an unknown provider", func() {
			read := get("/api/v1/config/oidc/e2e-missing", adminAuth)
			defer read.Body.Close()
			Expect(read.StatusCode).To(Equal(http.StatusNotFound))

			// No issuer in the patch: the issuer probe runs before the
			// name lookup, so omitting it is what surfaces the 404.
			patched := patch(
				"/api/v1/config/oidc/e2e-missing",
				adminAuth,
				map[string]any{"client_id": "x"},
			)
			defer patched.Body.Close()
			Expect(patched.StatusCode).To(Equal(http.StatusNotFound))

			deleted := del("/api/v1/config/oidc/e2e-missing", adminAuth, nil)
			defer deleted.Body.Close()
			Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("403s for a non-admin", func() {
			list := get("/api/v1/config/oidc", viewerAuth)
			defer list.Body.Close()
			Expect(list.StatusCode).To(Equal(http.StatusForbidden))

			created := post("/api/v1/config/oidc", viewerAuth, map[string]any{
				"name":          "e2e-nope",
				"issuer":        "http://127.0.0.1:1",
				"client_id":     "x",
				"client_secret": "y",
			})
			defer created.Body.Close()
			Expect(created.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
})
