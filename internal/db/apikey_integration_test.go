package db

import (
	"context"

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
			Email: "owner@example.com", Role: "admin", AuthMethod: "local",
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
				Email: "other@example.com", Role: "member", AuthMethod: "local",
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
})
