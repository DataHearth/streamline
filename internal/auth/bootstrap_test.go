package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
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
			// Default suite config has no seed email and no backing file, so
			// the credential write-back hits ErrNoPath and falls back to a log.
			storeMock.CountUsers(ctx).Return(0, nil).Once()
			storeMock.CreateUser(ctx, mock.MatchedBy(func(p db.CreateUserParams) bool {
				return p.Email == "admin@streamline.local" &&
					p.Role == user.RoleAdmin
			})).
				Return(&ent.User{ID: 1, Email: "admin@streamline.local"}, nil).
				Once()
			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())
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
				return p.Email == "admin@x.com" && p.Role == user.RoleAdmin
			})).
				Return(&ent.User{ID: 1}, nil).
				Once()

			Expect(svc.BootstrapSeedAdmin(ctx)).To(Succeed())
		})

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
					p.Role == user.RoleMember
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
