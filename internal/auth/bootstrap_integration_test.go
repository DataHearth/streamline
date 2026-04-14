package auth

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// seedAdminConfig overlays seed_admin fields onto the default auth config.
// Used by BootstrapSeedAdmin specs whose assertions depend on specific
// email/password/password_file values.
func seedAdminConfig(email, password, passwordFile string) {
	GinkgoHelper()
	configtest.Setup(map[string]any{
		"auth": map[string]any{
			"session_secret": "test-secret-key-for-jwt",
			"session_ttl":    "1h",
			"seed_admin": map[string]any{
				"email":         email,
				"password":      password,
				"password_file": passwordFile,
			},
		},
	})
}

var _ = Describe("Bootstrap end-to-end", Label("integration", "auth"), func() {
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
	})

	It("BootstrapSeedAdmin reads password from file + trims whitespace", func() {
		dir := GinkgoT().TempDir()
		pwPath := filepath.Join(dir, "pw")
		Expect(os.WriteFile(pwPath, []byte("hunter22\n"), 0o600)).To(Succeed())

		seedAdminConfig("admin@example.com", "", pwPath)
		svc = newTestService(dbClient)
		Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

		u, err := dbClient.User.Query().Only(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(u.Email).To(Equal("admin@example.com"))
	})

	It(
		"mints a default admin and writes its credentials to the config file",
		func() {
			// File-backed config so the credential write-back actually persists.
			configtest.SetupFile(map[string]any{
				"auth": map[string]any{
					"session_secret": "test-secret-key-for-jwt",
					"session_ttl":    "1h",
				},
			})
			svc = newTestService(dbClient)

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			u, err := dbClient.User.Query().Only(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Email).To(Equal("admin@streamline.local"))
			Expect(u.Role).To(Equal(user.RoleAdmin))

			// The generated password was persisted back to auth.seed_admin and
			// authenticates the new admin.
			seed := config.Get().Auth.SeedAdmin
			Expect(seed.Email).To(Equal("admin@streamline.local"))
			Expect(seed.Password).ToNot(BeEmpty())
			Expect(bcrypt.CompareHashAndPassword(
				[]byte(u.PasswordHash), []byte(seed.Password),
			)).To(Succeed())
		},
	)

	It("RegisterOpen rejects duplicate email via real unique constraint", func() {
		_, _, err := svc.RegisterOpen(
			ctx,
			"dup@x.com",
			"pw",
			"",
			"member",
			SessionMeta{},
		)
		Expect(err).ToNot(HaveOccurred())

		_, _, err = svc.RegisterOpen(
			ctx,
			"dup@x.com",
			"pw",
			"",
			"member",
			SessionMeta{},
		)
		Expect(err).To(HaveOccurred())

		count, err := dbClient.User.Query().Count(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(Equal(1))
	})
})
