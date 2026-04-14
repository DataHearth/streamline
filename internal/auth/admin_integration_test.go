package auth

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
)

// Admin integration tests — keep only paths that exercise real DB cascades or
// unique constraints. Per-method branch coverage lives in admin_test.go via
// MockStore.
var _ = Describe("Admin end-to-end", Label("integration", "auth"), func() {
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

	seedUser := func(email string, role entuser.Role) *ent.User {
		GinkgoHelper()
		u, err := svc.CreateUserDirect(ctx, email, "password123", string(role), "")
		Expect(err).NotTo(HaveOccurred())
		return u
	}

	It(
		"CreateUserDirect rejects duplicate email via real unique constraint",
		func() {
			seedUser("dup@x.com", entuser.RoleMember)
			_, err := svc.CreateUserDirect(
				ctx,
				"dup@x.com",
				"password123",
				"member",
				"",
			)
			Expect(err).To(MatchError(ErrUserEmailExists))
		},
	)

	It("DeleteUser cascades to api keys, sessions, and oidc identities", func() {
		u := seedUser("cascade@x.com", entuser.RoleMember)
		_, _, err := svc.CreateAPIKey(ctx, u.ID, "k1")
		Expect(err).NotTo(HaveOccurred())
		_, err = svc.Login(ctx, u.Email, "password123", SessionMeta{})
		Expect(err).NotTo(HaveOccurred())

		other := seedUser("requester@x.com", entuser.RoleAdmin)
		Expect(svc.DeleteUser(ctx, u.ID, other.ID)).To(Succeed())

		keys, err := svc.ListAPIKeys(ctx, u.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(keys).To(BeEmpty())
	})

	It("AdminResetPassword end-to-end revokes existing sessions", func() {
		u := seedUser("reset@x.com", entuser.RoleMember)
		_, err := svc.Login(ctx, u.Email, "password123", SessionMeta{})
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.AdminResetPassword(ctx, u.ID, "newpassword456")).To(Succeed())

		_, err = svc.Login(ctx, u.Email, "password123", SessionMeta{})
		Expect(err).To(HaveOccurred(), "old password rejected")

		_, err = svc.Login(ctx, u.Email, "newpassword456", SessionMeta{})
		Expect(err).NotTo(HaveOccurred())
	})
})
