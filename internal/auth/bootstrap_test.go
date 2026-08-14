package auth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("Bootstrap service unit", Label("unit", "auth"), func() {
	const ctxType = "*context.valueCtx"

	var (
		ctx       context.Context
		storeMock *dbmocks.MockStore_Expecter
		svc       *auth
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMock = store.EXPECT()
		m, err := New(store)
		svc = m.(*auth)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("IsFirstUser", func() {
		It("returns true when count is zero", func() {
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			first, err := svc.IsFirstUser(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(BeTrue())
		})

		It("returns false when count is non-zero", func() {
			storeMock.CountUsers(ctx).Return(2, nil).Once()
			first, err := svc.IsFirstUser(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(BeFalse())
		})

		It("propagates store errors", func() {
			storeMock.CountUsers(ctx).Return(0, errors.New("count fail")).Once()
			_, err := svc.IsFirstUser(ctx)
			Expect(err).To(MatchError("count fail"))
		})
	})

	Describe("BootstrapSeedAdmin", func() {
		It("mints a default admin when seed_admin.email is unset", func() {
			stdout := captureStdout()
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			storeMock.CreateUser(ctx, mock.MatchedBy(func(p db.CreateUserParams) bool {
				return p.Email == "admin@streamline.local" &&
					p.Role.String() == string(user.RoleAdmin)
			})).
				Return(&ent.User{ID: 1, Email: "admin@streamline.local"}, nil).
				Once()
			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())
			Expect(stdout()).To(ContainSubstring("admin@streamline.local"))
		})

		It("prints the generated password to stdout and persists no copy", func() {
			cfgPath := seedAdminFileConfig("", "")
			stdout := captureStdout()

			var created db.CreateUserParams
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			storeMock.CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Run(func(_ context.Context, p db.CreateUserParams) { created = p }).
				Return(&ent.User{ID: 1}, nil).
				Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			out := stdout()
			Expect(out).To(ContainSubstring("SHOWN ONCE"))
			Expect(out).To(ContainSubstring(defaultAdminEmail))
			pw := printedPassword(out)
			Expect(bcrypt.CompareHashAndPassword(
				[]byte(created.PasswordHash), []byte(pw),
			)).To(Succeed())

			Expect(config.Get().Auth.SeedAdmin.Password).To(BeEmpty())
			persisted, err := os.ReadFile(cfgPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(persisted)).NotTo(ContainSubstring(pw))
		})

		It("keeps the generated password out of the logs", func() {
			// The suite config is reader-backed, so every config write-back
			// fails with ErrNoPath — the read-only/GitOps case that used to
			// log the generated password verbatim.
			var logs bytes.Buffer
			GinkgoWriter.TeeTo(&logs)
			DeferCleanup(GinkgoWriter.ClearTeeWriters)
			stdout := captureStdout()

			storeMock.CountUsers(ctx).Return(0, nil).Once()
			storeMock.CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).
				Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			pw := printedPassword(stdout())
			Expect(logs.String()).NotTo(ContainSubstring(pw))
		})

		It("prints the credential on the line the Helm NOTES grep for", func() {
			stdout := captureStdout()
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			storeMock.CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).
				Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			out := stdout()
			Expect(grepDefaultAdmin(out)).
				To(ContainElement(SatisfyAll(
					ContainSubstring(defaultAdminEmail),
					ContainSubstring(printedPassword(out)),
				)))
		})

		It("does not claim the credential stays out of the log stream", func() {
			stdout := captureStdout()
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			storeMock.CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).
				Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			// stdout and stderr land in the same `kubectl logs` stream, so the
			// closed win is the OTLP pipeline, not "logs".
			Expect(stdout()).To(ContainSubstring("not sent to the log pipeline"))
		})

		It("leaves an operator's seed password on the default admin email", func() {
			cfgPath := seedAdminFileConfig(defaultAdminEmail, "hunter22")
			storeMock.CountUsers(ctx).Return(1, nil).Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			Expect(config.Get().Auth.SeedAdmin.Password).To(Equal("hunter22"))
			persisted, err := os.ReadFile(cfgPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(persisted)).To(ContainSubstring("hunter22"))
		})

		It("stays quiet about seed config it cannot rewrite", func() {
			// Reader-backed config: every config.Update fails with ErrNoPath,
			// the GitOps case that used to nag on every single boot.
			seedAdminConfig(defaultAdminEmail, "hunter22", "")
			var logs bytes.Buffer
			GinkgoWriter.TeeTo(&logs)
			DeferCleanup(GinkgoWriter.ClearTeeWriters)
			storeMock.CountUsers(ctx).Return(1, nil).Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			Expect(logs.String()).NotTo(ContainSubstring("auth.seed_admin.password"))
		})

		It("leaves an operator-configured seed password in place", func() {
			cfgPath := seedAdminFileConfig("admin@x.com", "hunter22")
			storeMock.CountUsers(ctx).Return(1, nil).Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

			Expect(config.Get().Auth.SeedAdmin.Password).To(Equal("hunter22"))
			persisted, err := os.ReadFile(cfgPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(persisted)).To(ContainSubstring("hunter22"))
		})

		It("wraps IsFirstUser errors", func() {
			seedAdminConfig("admin@x.com", "hunter22", "")
			storeMock.CountUsers(ctx).Return(0, errors.New("count fail")).Once()
			Expect(svc.BootstrapSeedAdmin(ctx)).
				To(MatchError(ContainSubstring("count users")))
		})

		It("is a no-op when users already exist", func() {
			seedAdminConfig("admin@x.com", "hunter22", "")
			storeMock.CountUsers(ctx).Return(1, nil).Once()
			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())
		})

		It("returns error when password_file is missing", func() {
			seedAdminConfig("admin@x.com", "", "/nonexistent/streamline-pw")
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			Expect(svc.BootstrapSeedAdmin(ctx)).
				To(MatchError(ContainSubstring("read seed password file")))
		})

		It("trims password file content and creates the admin", func() {
			dir := GinkgoT().TempDir()
			pwPath := filepath.Join(dir, "pw")
			Expect(os.WriteFile(pwPath, []byte("hunter22\n"), 0o600)).To(Succeed())

			seedAdminConfig("admin@x.com", "", pwPath)
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			storeMock.CreateUser(ctx, mock.MatchedBy(func(p db.CreateUserParams) bool {
				return p.Email == "admin@x.com" &&
					p.Role.String() == string(user.RoleAdmin)
			})).
				Return(&ent.User{ID: 1}, nil).
				Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())
		})

		It(
			"skips the default admin when its password_file reads back empty",
			func() {
				// Secret not materialised yet / empty mount. Minting a random
				// password here would create the admin and flip IsFirstUser, so the
				// real file could never take effect on a later boot.
				dir := GinkgoT().TempDir()
				pwPath := filepath.Join(dir, "pw")
				Expect(os.WriteFile(pwPath, []byte("   \n"), 0o600)).To(Succeed())

				seedAdminConfig(defaultAdminEmail, "", pwPath)
				stdout := captureStdout()
				var logs bytes.Buffer
				GinkgoWriter.TeeTo(&logs)
				DeferCleanup(GinkgoWriter.ClearTeeWriters)
				storeMock.CountUsers(ctx).Return(0, nil).Once()

				Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())

				Expect(stdout()).To(BeEmpty())
				// The operator's only signal that the secret never landed.
				Expect(logs.String()).To(SatisfyAll(
					ContainSubstring("no usable password"),
					ContainSubstring(pwPath),
				))
			},
		)

		It("is a no-op when email set but password is missing", func() {
			seedAdminConfig("admin@x.com", "", "")
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())
		})

		It("wraps bcrypt errors for oversized password", func() {
			seedAdminConfig("admin@x.com", strings.Repeat("p", 80), "")
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			Expect(svc.BootstrapSeedAdmin(ctx)).
				To(MatchError(ContainSubstring("hash seed password")))
		})

		It("wraps store CreateUser errors", func() {
			seedAdminConfig("admin@x.com", "hunter22", "")
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			storeMock.CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Return(nil, errors.New("create fail")).Once()
			Expect(svc.BootstrapSeedAdmin(ctx)).
				To(MatchError(ContainSubstring("create seed admin")))
		})
	})

	Describe("RegisterOpen", func() {
		It("creates user with given role and returns token", func() {
			storeMock.CreateUser(ctx, mock.MatchedBy(func(p db.CreateUserParams) bool {
				return p.Email == "a@x.com" && p.DisplayName == "Alice" &&
					p.Role.String() == string(user.RoleMember)
			})).
				Return(&ent.User{ID: 1, Email: "a@x.com"}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, tok, err := svc.RegisterOpen(
				ctx,
				"A@X.COM",
				"password",
				"Alice",
				"member",
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Email).To(Equal("a@x.com"))
			Expect(tok).NotTo(BeEmpty())
		})

		It("wraps bcrypt failures", func() {
			_, _, err := svc.RegisterOpen(
				ctx,
				"a@x.com",
				strings.Repeat("p", 80),
				"",
				"member",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("hash password")))
		})

		It("wraps store create errors", func() {
			storeMock.CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Return(nil, errors.New("create fail")).Once()
			_, _, err := svc.RegisterOpen(
				ctx,
				"a@x.com",
				"pw",
				"",
				"member",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("create user")))
		})
	})
})

var _ = Describe("GeneratePassword", Label("unit", "auth"), func() {
	It("returns a URL-safe password with the seed-admin entropy", func() {
		pw, err := GeneratePassword()
		Expect(err).ToNot(HaveOccurred())
		Expect(len(pw)).To(BeNumerically(">=", 20))
		Expect(pw).To(MatchRegexp(`^[A-Za-z0-9_-]+$`))
	})

	It("never repeats a password across calls", func() {
		seen := make(map[string]struct{}, 100)
		for range 100 {
			pw, err := GeneratePassword()
			Expect(err).ToNot(HaveOccurred())
			Expect(pw).ToNot(BeEmpty())
			Expect(seen).ToNot(HaveKey(pw))
			seen[pw] = struct{}{}
		}
		Expect(seen).To(HaveLen(100))
	})
})
