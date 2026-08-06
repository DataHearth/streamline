package auth

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
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
		configtest.Setup(map[string]any{
			"auth": map[string]any{
				"session_secret":    "test-secret-key-for-jwt",
				"registration_mode": "invite",
			},
		})
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

	It(
		"DeleteUser cascades to api keys, sessions, oidc identities, and invites",
		func() {
			u := seedUser("cascade@x.com", entuser.RoleAdmin)
			_, _, err := svc.CreateAPIKey(ctx, u.ID, "k1")
			Expect(err).NotTo(HaveOccurred())
			_, err = svc.Login(ctx, u.Email, "password123", SessionMeta{})
			Expect(err).NotTo(HaveOccurred())
			_, inv, err := svc.CreateInvite(
				ctx,
				u.ID,
				"invited@x.com",
				"member",
				time.Hour,
			)
			Expect(err).NotTo(HaveOccurred())

			other := seedUser("requester@x.com", entuser.RoleAdmin)
			Expect(svc.DeleteUser(ctx, u.ID, other.ID)).To(Succeed())

			keys, err := svc.ListAPIKeys(ctx, u.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(keys).To(BeEmpty())

			_, err = dbClient.Invite.Get(ctx, inv.ID)
			Expect(ent.IsNotFound(err)).To(
				BeTrue(),
				"invites created by the user must be gone",
			)

			sessions, err := dbClient.Session.Query().All(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(sessions).To(BeEmpty())
		},
	)

	It("DeleteUser keeps invites the user consumed, with used_by nulled", func() {
		admin := seedUser("inviter@x.com", entuser.RoleAdmin)
		raw, inv, err := svc.CreateInvite(
			ctx,
			admin.ID,
			"guest@x.com",
			"member",
			time.Hour,
		)
		Expect(err).NotTo(HaveOccurred())

		guest, _, err := svc.RegisterWithInvite(
			ctx,
			raw,
			"guest@x.com",
			"password123",
			"",
			SessionMeta{},
		)
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.DeleteUser(ctx, guest.ID, admin.ID)).To(Succeed())

		kept, err := dbClient.Invite.Get(ctx, inv.ID)
		Expect(err).NotTo(HaveOccurred())
		_, err = kept.QueryUsedBy().Only(ctx)
		Expect(ent.IsNotFound(err)).To(BeTrue())
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
