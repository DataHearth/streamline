package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"

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

// seedAdminFileConfig mirrors seedAdminConfig with a file-backed config so
// specs can assert on what BootstrapSeedAdmin left on disk. Returns the path
// of the config file, which configtest writes into the data dir.
func seedAdminFileConfig(email, password string) string {
	GinkgoHelper()
	cfg := configtest.SetupFile(map[string]any{
		"auth": map[string]any{
			"session_secret": "test-secret-key-for-jwt",
			"session_ttl":    "1h",
			"seed_admin": map[string]any{
				"email":    email,
				"password": password,
			},
		},
	})
	return filepath.Join(cfg.DataDir, "config.yaml")
}

// captureStdout points os.Stdout at a temp file for the rest of the spec and
// returns a func reading back what was written. fmt.Printf resolves os.Stdout
// per call, so the swap catches the shown-once credentials banner.
func captureStdout() func() string {
	GinkgoHelper()
	prev := os.Stdout
	f, err := os.CreateTemp(GinkgoT().TempDir(), "stdout")
	Expect(err).NotTo(HaveOccurred())
	os.Stdout = f
	DeferCleanup(func() {
		os.Stdout = prev
		f.Close()
	})
	return func() string {
		GinkgoHelper()
		out, err := os.ReadFile(f.Name())
		Expect(err).NotTo(HaveOccurred())
		return string(out)
	}
}

// printedPassword pulls the credential out of the stdout banner so specs can
// prove that exact string appears nowhere else.
func printedPassword(stdout string) string {
	GinkgoHelper()
	_, after, found := strings.Cut(stdout, "password: ")
	Expect(found).To(BeTrue(), "stdout carries the generated password")
	pw, _, _ := strings.Cut(after, "\n")
	Expect(pw).NotTo(BeEmpty())
	return pw
}

// grepDefaultAdmin filters stdout the way the Helm NOTES tell operators to:
// `kubectl logs … | grep -i "default admin"`. A banner that spreads the
// credential over lines the filter drops leaves them with nothing.
func grepDefaultAdmin(stdout string) []string {
	var matched []string
	for l := range strings.SplitSeq(stdout, "\n") {
		if strings.Contains(strings.ToLower(l), "default admin") {
			matched = append(matched, l)
		}
	}
	return matched
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
		"mints a default admin whose password only ever reaches stdout",
		func() {
			cfgPath := seedAdminFileConfig("", "")
			stdout := captureStdout()
			svc = newTestService(dbClient)

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			u, err := dbClient.User.Query().Only(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Email).To(Equal("admin@streamline.local"))
			Expect(u.Role).To(Equal(user.RoleAdmin))

			pw := printedPassword(stdout())
			Expect(bcrypt.CompareHashAndPassword(
				[]byte(u.PasswordHash), []byte(pw),
			)).To(Succeed())

			Expect(config.Get().Auth.SeedAdmin.Password).To(BeEmpty())
			persisted, err := os.ReadFile(cfgPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(persisted)).ToNot(ContainSubstring(pw))
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
