package auth

import (
	"context"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
)

// LoginOIDC integration smoke — exercise full pipeline (real DB user create +
// real OIDC identity persistence + real session/JWT issuance). Per-branch
// coverage lives in oidc_test.go via MockStore.
var _ = Describe("LoginOIDC end-to-end", Label("integration", "auth"), func() {
	var (
		ctx      context.Context
		svc      *auth
		dbClient *ent.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		dbClient, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })
		svc = newTestService(dbClient)

		config.ResetForTest()
		loadOIDCTestConfig("open", "member")
		DeferCleanup(config.ResetForTest)
	})

	It("creates user + OIDC identity + session row in open mode", func() {
		u, tok, err := svc.LoginOIDC(
			ctx,
			"google",
			"sub-1",
			"u@x.com",
			"U",
			true,
			nil,
			SessionMeta{},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(u.Email).To(Equal("u@x.com"))
		Expect(u.AuthMethod).To(Equal(entuser.AuthMethodOidc))
		Expect(tok).NotTo(BeEmpty())

		// Identity persisted with correct (provider, subject) pair.
		id, err := dbClient.OIDCIdentity.Query().Only(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(id.Provider).To(Equal("google"))
		Expect(id.Subject).To(Equal("sub-1"))

		// Session row was created so the JWT we returned is backed.
		claims, err := svc.ValidateToken(tok)
		Expect(err).ToNot(HaveOccurred())
		Expect(svc.ValidateSession(ctx, claims.JTI)).To(Succeed())
	})

	It(
		"auto-links a second OIDC login to the existing local user (case-insensitive email)",
		func() {
			u, err := dbClient.User.Create().
				SetEmail("case@x.com").
				SetPasswordHash("hash").
				SetRole(entuser.RoleMember).
				SetAuthMethod(entuser.AuthMethodLocal).
				Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			got, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-9",
				"CASE@x.com",
				"",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(u.ID))
			Expect(got.AuthMethod).To(Equal(entuser.AuthMethodBoth))
		},
	)

	It(
		"assigns the claim-mapped role to a new user, overriding the default",
		func() {
			config.ResetForTest()
			loadOIDCRoleMapConfig()

			u, _, err := svc.LoginOIDC(ctx, "kc", "sub-a", "a@x.com", "A", true,
				map[string]any{"groups": []any{"streamline-admins"}}, SessionMeta{})
			Expect(err).ToNot(HaveOccurred())
			// oidc_default_role is "member"; the admins group maps to admin.
			Expect(u.Role).To(Equal(entuser.RoleAdmin))
		},
	)

	It("re-syncs an existing user's role from claims on login", func() {
		u, err := dbClient.User.Create().
			SetEmail("b@x.com").SetPasswordHash("h").
			SetRole(entuser.RoleAdmin).SetAuthMethod(entuser.AuthMethodLocal).
			Save(ctx)
		Expect(err).ToNot(HaveOccurred())
		config.ResetForTest()
		loadOIDCRoleMapConfig()

		// Group now maps to member → the admin is demoted on login.
		got, _, err := svc.LoginOIDC(ctx, "kc", "sub-b", "b@x.com", "B", true,
			map[string]any{"groups": []any{"streamline-staff"}}, SessionMeta{})
		Expect(err).ToNot(HaveOccurred())
		Expect(got.ID).To(Equal(u.ID))
		Expect(got.Role).To(Equal(entuser.RoleMember))
	})
})

// loadOIDCRoleMapConfig seeds a provider "kc" with claim-based role mapping.
func loadOIDCRoleMapConfig() {
	GinkgoHelper()
	yaml := `
data_dir: ` + os.TempDir() + `
auth:
  mode: disabled
  trusted_role: admin
  session_ttl: 168h
  registration_mode: open
  oidc_default_role: member
  oidc:
    - name: kc
      issuer: https://kc.example.com
      client_id: streamline
      client_secret: secret
      role_claim: groups
      role_mapping:
        streamline-admins: admin
        streamline-staff: member
library:
  movie_path: /x
  movie_naming: m
  import_mode: hardlink
schedules:
  rss_sync: 15m
  metadata_refresh: 24h
  download_monitor: 30s
  missing_search: 12h
  cleanup: 24h
log:
  level: info
  format: text
`
	Expect(config.LoadReader(strings.NewReader(yaml))).To(Succeed())
}

// loadOIDCTestConfig populates the config singleton with the minimum required
// fields for LoginOIDC + helpers to run.
func loadOIDCTestConfig(regMode, oidcDefaultRole string) {
	GinkgoHelper()
	yaml := `
data_dir: ` + os.TempDir() + `
auth:
  mode: disabled
  trusted_role: admin
  session_ttl: 168h
  registration_mode: ` + regMode + `
  oidc_default_role: ` + oidcDefaultRole + `
library:
  movie_path: /x
  movie_naming: m
  import_mode: hardlink
  default_quality:
    preferred_resolution: 1080p
    min_resolution: 720p
    no_match_cooldown: 6h
    max_grab_failures: 3
schedules:
  rss_sync: 15m
  metadata_refresh: 24h
  download_monitor: 30s
  missing_search: 12h
  cleanup: 24h
log:
  level: info
  format: text
`
	err := config.LoadReader(strings.NewReader(yaml))
	Expect(err).ToNot(HaveOccurred())
}
