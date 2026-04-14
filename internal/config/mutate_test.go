package config_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// startDiscoveryServer spins up an httptest.Server that serves a minimal
// OIDC discovery document at /.well-known/openid-configuration. Returns the
// server (for cleanup) and its base URL (the "issuer").
func startDiscoveryServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(
				[]byte(
					`{"issuer":"x","authorization_endpoint":"y","token_endpoint":"z","jwks_uri":"w"}`,
				),
			)
		})
	return httptest.NewServer(mux)
}

var _ = Describe("Config mutate", Label("unit", "config"), func() {
	BeforeEach(func() {
		configtest.SetupFile()
	})

	Describe("UpdateAuth", func() {
		It("changes registration_mode and persists to singleton", func() {
			mode := "open"
			got, err := config.UpdateAuth(context.Background(), config.AuthPatch{
				RegistrationMode: &mode,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(got.RegistrationMode).To(Equal("open"))
			Expect(config.Get().Auth.RegistrationMode).To(Equal("open"))
		})

		It("rejects invalid registration_mode via validator", func() {
			bad := "nope"
			_, err := config.UpdateAuth(context.Background(), config.AuthPatch{
				RegistrationMode: &bad,
			})
			Expect(err).To(HaveOccurred())
		})

		It("rejects session_ttl that can't parse as duration", func() {
			bad := "not-a-duration"
			_, err := config.UpdateAuth(
				context.Background(),
				config.AuthPatch{SessionTTL: &bad},
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("session_ttl"))
		})

		It("accepts a well-formed session_ttl", func() {
			ttl := "30m"
			got, err := config.UpdateAuth(
				context.Background(),
				config.AuthPatch{SessionTTL: &ttl},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.SessionTTL).To(Equal("30m"))
		})

		It("rejects invalid oidc_default_role via validator", func() {
			bad := "bogus"
			_, err := config.UpdateAuth(context.Background(), config.AuthPatch{
				OIDCDefaultRole: &bad,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("AddOIDCProvider", func() {
		var srv *httptest.Server

		BeforeEach(func() {
			srv = startDiscoveryServer()
			DeferCleanup(srv.Close)
		})

		It("persists a valid provider", func() {
			err := config.AddOIDCProvider(context.Background(), config.OIDCConfig{
				Name:         "test",
				Issuer:       srv.URL,
				ClientID:     "cid",
				ClientSecret: "secret",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Get().Auth.OIDC).To(HaveLen(1))
			Expect(config.Get().Auth.OIDC[0].Name).To(Equal("test"))
		})

		It("rejects duplicate name", func() {
			p := config.OIDCConfig{
				Name:         "dup",
				Issuer:       srv.URL,
				ClientID:     "cid",
				ClientSecret: "secret",
			}
			Expect(config.AddOIDCProvider(context.Background(), p)).To(Succeed())
			err := config.AddOIDCProvider(context.Background(), p)
			Expect(errors.Is(err, config.ErrOIDCProviderExists)).To(BeTrue())
		})

		It("rejects unreachable issuer", func() {
			err := config.AddOIDCProvider(context.Background(), config.OIDCConfig{
				Name:         "bad",
				Issuer:       "http://127.0.0.1:1",
				ClientID:     "cid",
				ClientSecret: "secret",
			})
			Expect(errors.Is(err, config.ErrOIDCDiscoveryFailed)).To(BeTrue())
		})
	})

	Describe("UpdateOIDCProvider", func() {
		var srv *httptest.Server

		BeforeEach(func() {
			srv = startDiscoveryServer()
			DeferCleanup(srv.Close)
			Expect(config.AddOIDCProvider(context.Background(), config.OIDCConfig{
				Name:         "acme",
				Issuer:       srv.URL,
				ClientID:     "cid",
				ClientSecret: "secret",
			})).To(Succeed())
		})

		It("keeps existing secret when patch leaves client_secret blank", func() {
			blank := ""
			newID := "new-cid"
			Expect(config.UpdateOIDCProvider(
				context.Background(),
				"acme",
				config.OIDCProviderPatch{
					ClientID:     &newID,
					ClientSecret: &blank,
				},
			)).To(Succeed())
			got := config.Get().Auth.OIDC[0]
			Expect(got.ClientID).To(Equal("new-cid"))
			Expect(got.ClientSecret).To(Equal("secret"))
		})

		It("rejects not-found name", func() {
			v := "x"
			err := config.UpdateOIDCProvider(
				context.Background(),
				"ghost",
				config.OIDCProviderPatch{ClientID: &v},
			)
			Expect(errors.Is(err, config.ErrOIDCProviderNotFound)).To(BeTrue())
		})

		It("probes new issuer when patched", func() {
			bad := "http://127.0.0.1:1"
			err := config.UpdateOIDCProvider(
				context.Background(),
				"acme",
				config.OIDCProviderPatch{Issuer: &bad},
			)
			Expect(errors.Is(err, config.ErrOIDCDiscoveryFailed)).To(BeTrue())
		})
	})

	Describe("DeleteOIDCProvider", func() {
		var srv *httptest.Server

		BeforeEach(func() {
			srv = startDiscoveryServer()
			DeferCleanup(srv.Close)
			Expect(config.AddOIDCProvider(context.Background(), config.OIDCConfig{
				Name:         "to-delete",
				Issuer:       srv.URL,
				ClientID:     "cid",
				ClientSecret: "secret",
			})).To(Succeed())
		})

		It("removes the named provider", func() {
			Expect(
				config.DeleteOIDCProvider(context.Background(), "to-delete"),
			).To(Succeed())
			Expect(config.Get().Auth.OIDC).To(BeEmpty())
		})

		It("returns not-found for missing name", func() {
			err := config.DeleteOIDCProvider(context.Background(), "nobody")
			Expect(errors.Is(err, config.ErrOIDCProviderNotFound)).To(BeTrue())
		})
	})
})
