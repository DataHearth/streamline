package db

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("Session store", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
		userID uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)

		u, err := store.CreateUser(ctx, CreateUserParams{
			Email: "owner@example.com", Role: "admin", AuthMethod: "local",
		})
		Expect(err).NotTo(HaveOccurred())
		userID = u.ID
	})

	create := func(jti string) *ent.Session {
		GinkgoHelper()
		s, err := store.CreateSession(ctx, CreateSessionParams{
			JTI: jti, UserID: userID,
			IP: "1.2.3.4", UserAgent: "test",
			ExpiresAt: time.Now().Add(time.Hour),
		})
		Expect(err).NotTo(HaveOccurred())
		return s
	}

	Describe("CreateSession + FindSessionByJTI", func() {
		It("persists and looks up by jti", func() {
			create("j1")
			got, err := store.FindSessionByJTI(ctx, "j1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.IP).To(Equal("1.2.3.4"))
		})
	})

	Describe("TouchSession", func() {
		It("updates last_seen_at", func() {
			create("j1")
			when := time.Now()
			Expect(store.TouchSession(ctx, "j1", when)).To(Succeed())
			got, _ := store.FindSessionByJTI(ctx, "j1")
			Expect(got.LastSeenAt).NotTo(BeNil())
		})
	})

	Describe("RevokeSessionByJTI", func() {
		It("sets revoked_at on the matching row", func() {
			create("j1")
			Expect(store.RevokeSessionByJTI(ctx, "j1", time.Now())).To(Succeed())
			got, _ := store.FindSessionByJTI(ctx, "j1")
			Expect(got.RevokedAt).NotTo(BeNil())
		})
	})

	Describe("RevokeUserSessionByID", func() {
		It("revokes when the userID matches and returns 1", func() {
			s := create("j1")
			n, err := store.RevokeUserSessionByID(ctx, userID, s.ID, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})

		It("returns 0 when ownership does not match", func() {
			s := create("j1")
			n, err := store.RevokeUserSessionByID(ctx, userID+999, s.ID, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(0))
		})
	})

	Describe("UserSessionExists", func() {
		It("returns true when the session belongs to the user", func() {
			s := create("j1")
			ok, err := store.UserSessionExists(ctx, userID, s.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
		})

		It("returns false when ownership does not match", func() {
			s := create("j1")
			ok, err := store.UserSessionExists(ctx, userID+999, s.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})
	})

	Describe("RevokeAllUserSessions", func() {
		It("revokes every active session for the user", func() {
			create("j1")
			create("j2")
			Expect(
				store.RevokeAllUserSessions(ctx, userID, time.Now()),
			).To(Succeed())
			items, _ := store.ListUserSessions(ctx, userID)
			for _, s := range items {
				Expect(s.RevokedAt).NotTo(BeNil())
			}
		})
	})

	Describe("RevokeOtherUserSessions", func() {
		It("revokes everything except the kept jti", func() {
			create("keep")
			create("drop1")
			create("drop2")
			Expect(
				store.RevokeOtherUserSessions(ctx, userID, "keep", time.Now()),
			).To(Succeed())

			kept, _ := store.FindSessionByJTI(ctx, "keep")
			Expect(kept.RevokedAt).To(BeNil())
			dropped, _ := store.FindSessionByJTI(ctx, "drop1")
			Expect(dropped.RevokedAt).NotTo(BeNil())
		})
	})

	Describe("ListUserSessions", func() {
		It("returns sessions newest-first", func() {
			a := create("j1")
			b := create("j2")
			items, err := store.ListUserSessions(ctx, userID)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))
			Expect(items[0].ID).To(Equal(b.ID))
			Expect(items[1].ID).To(Equal(a.ID))
		})
	})

	Describe("PurgeExpiredSessions", func() {
		It("deletes rows older than the cutoff", func() {
			_, err := store.CreateSession(ctx, CreateSessionParams{
				JTI: "old", UserID: userID,
				ExpiresAt: time.Now().Add(-time.Hour),
			})
			Expect(err).NotTo(HaveOccurred())
			create("future")

			n, err := store.PurgeExpiredSessions(ctx, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})
	})
})
