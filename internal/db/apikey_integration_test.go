package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent/apikey"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/role"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("API key store", Label("integration", "db"), func() {
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
			Email:      "owner@example.com",
			Role:       role.Seed(user.RoleAdmin),
			AuthMethod: "local",
		})
		Expect(err).NotTo(HaveOccurred())
		userID = u.ID
	})

	Describe("CreateAPIKey", func() {
		It("persists the row with the given owner", func() {
			key, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
				Name: "ci", KeyHash: "h1", OwnerID: userID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(key.Name).To(Equal("ci"))
		})

		It("defaults to a full key with no expiry", func() {
			key, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
				Name: "ci", KeyHash: "h0", OwnerID: userID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(key.Scope).To(Equal(apikey.ScopeFull))
			Expect(key.ExpiresAt).To(BeNil())
		})

		It("stores the scope and expiry it is given", func() {
			at := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)
			key, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
				Name: "ha", KeyHash: "h3", OwnerID: userID,
				Scope: apikey.ScopeRead, ExpiresAt: &at,
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := store.FindAPIKeyByHash(ctx, "h3")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(key.ID))
			Expect(got.Scope).To(Equal(apikey.ScopeRead))
			Expect(got.ExpiresAt).NotTo(BeNil())
			Expect(*got.ExpiresAt).To(BeTemporally("==", at))
		})
	})

	Describe("CountAPIKeysByUser", func() {
		It("leaves expired keys out of the quota", func() {
			past := time.Now().Add(-time.Hour)
			future := time.Now().Add(time.Hour)
			for h, exp := range map[string]*time.Time{
				"c1": nil, "c2": &future, "c3": &past,
			} {
				_, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
					Name: h, KeyHash: h, OwnerID: userID, ExpiresAt: exp,
				})
				Expect(err).NotTo(HaveOccurred())
			}

			n, err := store.CountAPIKeysByUser(ctx, userID)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(2))
		})
	})

	Describe("PurgeExpiredAPIKeys", func() {
		It("deletes keys expired before the cutoff and nothing else", func() {
			longAgo := time.Now().Add(-30 * 24 * time.Hour)
			recent := time.Now().Add(-time.Hour)
			future := time.Now().Add(time.Hour)
			for h, exp := range map[string]*time.Time{
				"p-old": &longAgo, "p-recent": &recent,
				"p-future": &future, "p-never": nil,
			} {
				_, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
					Name: h, KeyHash: h, OwnerID: userID, ExpiresAt: exp,
				})
				Expect(err).NotTo(HaveOccurred())
			}

			n, err := store.PurgeExpiredAPIKeys(
				ctx, time.Now().Add(-7*24*time.Hour),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))

			keys, err := store.ListAPIKeysByUser(ctx, userID)
			Expect(err).NotTo(HaveOccurred())
			names := make([]string, 0, len(keys))
			for _, k := range keys {
				names = append(names, k.Name)
			}
			Expect(names).To(ConsistOf("p-recent", "p-future", "p-never"))
		})
	})

	Describe("FindAPIKeyByHash", func() {
		It("returns the key with owner preloaded", func() {
			_, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
				Name: "ci", KeyHash: "h2", OwnerID: userID,
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := store.FindAPIKeyByHash(ctx, "h2")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Edges.Owner).NotTo(BeNil())
			Expect(got.Edges.Owner.ID).To(Equal(userID))
		})

		It("returns NotFound when absent", func() {
			_, err := store.FindAPIKeyByHash(ctx, "missing")
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("ListAPIKeysByUser", func() {
		It("returns only keys for the given owner, newest first", func() {
			_, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
				Name: "a", KeyHash: "ha", OwnerID: userID,
			})
			Expect(err).NotTo(HaveOccurred())

			other, err := store.CreateUser(ctx, CreateUserParams{
				Email:      "other@example.com",
				Role:       role.Seed(user.RoleMember),
				AuthMethod: "local",
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = store.CreateAPIKey(ctx, CreateAPIKeyParams{
				Name: "b", KeyHash: "hb", OwnerID: other.ID,
			})
			Expect(err).NotTo(HaveOccurred())

			keys, err := store.ListAPIKeysByUser(ctx, userID)
			Expect(err).NotTo(HaveOccurred())
			Expect(keys).To(HaveLen(1))
			Expect(keys[0].Name).To(Equal("a"))
		})
	})

	Describe("DeleteAPIKeyByID", func() {
		It("deletes only when the userID matches", func() {
			key, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
				Name: "ci", KeyHash: "hd", OwnerID: userID,
			})
			Expect(err).NotTo(HaveOccurred())

			n, err := store.DeleteAPIKeyByID(ctx, userID+1, key.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(0))

			n, err = store.DeleteAPIKeyByID(ctx, userID, key.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})
	})

	Describe("DeleteAPIKeysByUser", func() {
		It("deletes every key of the owner and leaves other owners alone", func() {
			for _, h := range []string{"hm1", "hm2"} {
				_, err := store.CreateAPIKey(ctx, CreateAPIKeyParams{
					Name: h, KeyHash: h, OwnerID: userID,
				})
				Expect(err).NotTo(HaveOccurred())
			}

			other, err := store.CreateUser(ctx, CreateUserParams{
				Email:      "keeper@example.com",
				Role:       role.Seed(user.RoleMember),
				AuthMethod: "local",
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = store.CreateAPIKey(ctx, CreateAPIKeyParams{
				Name: "keep", KeyHash: "hkeep", OwnerID: other.ID,
			})
			Expect(err).NotTo(HaveOccurred())

			n, err := store.DeleteAPIKeysByUser(ctx, userID)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(2))

			keys, err := store.ListAPIKeysByUser(ctx, userID)
			Expect(err).NotTo(HaveOccurred())
			Expect(keys).To(BeEmpty())

			kept, err := store.FindAPIKeyByHash(ctx, "hkeep")
			Expect(err).NotTo(HaveOccurred())
			Expect(kept.Edges.Owner.ID).To(Equal(other.ID))
		})

		It("returns 0 for a user with no keys", func() {
			n, err := store.DeleteAPIKeysByUser(ctx, userID)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(0))
		})
	})
})
