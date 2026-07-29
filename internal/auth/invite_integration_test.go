package auth

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
)

var _ = Describe("Invite end-to-end", Label("integration", "auth"), func() {
	var (
		ctx      context.Context
		svc      *auth
		dbClient *ent.Client
		adminID  uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		dbClient, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })
		svc = newTestService(dbClient)

		u, _, err := svc.Register(
			ctx,
			"admin@x.com",
			"password",
			"admin",
			SessionMeta{},
		)
		Expect(err).NotTo(HaveOccurred())
		adminID = u.ID
	})

	It(
		"CreateInvite + RegisterWithInvite happy path round-trips",
		func() {
			raw, inv, err := svc.CreateInvite(
				ctx,
				adminID,
				"guest@x.com",
				"member",
				time.Hour,
			)
			Expect(err).ToNot(HaveOccurred())

			u, tok, err := svc.RegisterWithInvite(
				ctx,
				raw,
				"guest@x.com",
				"password",
				"Guest",
				SessionMeta{},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Email).To(Equal("guest@x.com"))
			Expect(tok).ToNot(BeEmpty())

			inv2, err := dbClient.Invite.Get(ctx, inv.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(inv2.UsedAt).ToNot(BeNil())
		},
	)

	It(
		"RegisterWithInvite rejects a second use of an unbound invite",
		func() {
			raw, inv, err := svc.CreateInvite(ctx, adminID, "", "member", time.Hour)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = svc.RegisterWithInvite(
				ctx,
				raw,
				"first@x.com",
				"password",
				"",
				SessionMeta{},
			)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = svc.RegisterWithInvite(
				ctx,
				raw,
				"second@x.com",
				"password",
				"",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrInviteInvalid))

			_, err = dbClient.User.Query().
				Where(entuser.EmailEQ("second@x.com")).
				Only(ctx)
			Expect(ent.IsNotFound(err)).To(
				BeTrue(),
				"the losing registration must not create a user",
			)

			used, err := dbClient.Invite.Get(ctx, inv.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(used.UsedAt).ToNot(BeNil())
		},
	)

	It(
		"RegisterWithInvite rolls back when user email already exists (real unique constraint)",
		func() {
			_, _, err := svc.Register(ctx, "dup@x.com", "p", "member", SessionMeta{})
			Expect(err).ToNot(HaveOccurred())

			raw, inv, err := svc.CreateInvite(
				ctx,
				adminID,
				"dup@x.com",
				"member",
				time.Hour,
			)
			Expect(err).ToNot(HaveOccurred())

			_, _, err = svc.RegisterWithInvite(
				ctx,
				raw,
				"dup@x.com",
				"p",
				"",
				SessionMeta{},
			)
			Expect(err).To(HaveOccurred())

			// Invite must remain unused after rollback.
			inv2, err := dbClient.Invite.Get(ctx, inv.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(inv2.UsedAt).To(BeNil())
		},
	)
})
