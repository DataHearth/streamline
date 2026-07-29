package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
)

var _ = Describe("Invite store", Label("integration", "db"), func() {
	var (
		ctx     context.Context
		client  *ent.Client
		store   *DB
		adminID uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)

		admin, err := store.CreateUser(ctx, CreateUserParams{
			Email: "admin@example.com", Role: "admin", AuthMethod: "local",
		})
		Expect(err).NotTo(HaveOccurred())
		adminID = admin.ID
	})

	create := func(hash, email string, exp time.Time) *ent.Invite {
		GinkgoHelper()
		inv, err := store.CreateInvite(ctx, CreateInviteParams{
			TokenHash: hash, Email: email, Role: invite.RoleMember,
			ExpiresAt: exp, CreatedByID: adminID,
		})
		Expect(err).NotTo(HaveOccurred())
		return inv
	}

	Describe("CreateInvite + FindInviteByTokenHash", func() {
		It("persists and looks up by hash", func() {
			create("h1", "guest@example.com", time.Now().Add(time.Hour))
			got, err := store.FindInviteByTokenHash(ctx, "h1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.TokenHash).To(Equal("h1"))
		})

		It("returns NotFound for a missing hash", func() {
			_, err := store.FindInviteByTokenHash(ctx, "missing")
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("FindUnusedInviteForEmail", func() {
		It("returns the earliest unused, unexpired invite for the email", func() {
			now := time.Now()
			create("h-expired", "guest@example.com", now.Add(-time.Hour))
			a := create("h-a", "guest@example.com", now.Add(time.Hour))
			create("h-b", "guest@example.com", now.Add(2*time.Hour))

			got, err := store.FindUnusedInviteForEmail(ctx, "guest@example.com", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(a.ID))
		})

		It("returns NotFound when none match", func() {
			_, err := store.FindUnusedInviteForEmail(
				ctx,
				"nobody@example.com",
				time.Now(),
			)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("ListInvites", func() {
		It("returns all invites with edges loaded", func() {
			create("h1", "g@example.com", time.Now().Add(time.Hour))
			items, err := store.ListInvites(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Edges.CreatedBy).NotTo(BeNil())
			Expect(items[0].Edges.CreatedBy.ID).To(Equal(adminID))
		})
	})

	Describe("ConsumeInvite", func() {
		var consumerID uint32

		BeforeEach(func() {
			consumer, err := store.CreateUser(ctx, CreateUserParams{
				Email: "g@example.com", Role: "member", AuthMethod: "local",
			})
			Expect(err).NotTo(HaveOccurred())
			consumerID = consumer.ID
		})

		It("sets used_at and used_by", func() {
			inv := create("h1", "g@example.com", time.Now().Add(time.Hour))

			Expect(store.ConsumeInvite(ctx, inv.ID, consumerID, time.Now())).
				To(Succeed())

			updated, err := client.Invite.Get(ctx, inv.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.UsedAt).NotTo(BeNil())

			usedBy, err := updated.QueryUsedBy().Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(usedBy.ID).To(Equal(consumerID))
		})

		It("returns ErrInviteUsed on a second consumption", func() {
			inv := create("h1", "g@example.com", time.Now().Add(time.Hour))
			Expect(store.ConsumeInvite(ctx, inv.ID, consumerID, time.Now())).
				To(Succeed())

			other, err := store.CreateUser(ctx, CreateUserParams{
				Email: "other@example.com", Role: "member", AuthMethod: "local",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(store.ConsumeInvite(ctx, inv.ID, other.ID, time.Now())).
				To(MatchError(ErrInviteUsed))

			updated, err := client.Invite.Get(ctx, inv.ID)
			Expect(err).NotTo(HaveOccurred())
			usedBy, err := updated.QueryUsedBy().Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(usedBy.ID).To(
				Equal(consumerID),
				"first consumer keeps the invite",
			)
		})

		It("returns ErrInviteUsed for an unknown invite id", func() {
			Expect(store.ConsumeInvite(ctx, 4242, consumerID, time.Now())).
				To(MatchError(ErrInviteUsed))
		})
	})

	Describe("RevokeInvite", func() {
		It("expires the invite", func() {
			inv := create("h1", "g@example.com", time.Now().Add(time.Hour))
			now := time.Now()
			Expect(store.RevokeInvite(ctx, inv.ID, now)).To(Succeed())

			got, err := store.FindInviteByTokenHash(ctx, "h1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ExpiresAt.Unix()).To(Equal(now.Unix()))
		})
	})
})
