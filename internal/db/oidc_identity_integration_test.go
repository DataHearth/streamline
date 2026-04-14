package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("OIDC identity store", Label("integration", "db"), func() {
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
			Email: "owner@example.com", Role: "admin", AuthMethod: "oidc",
		})
		Expect(err).NotTo(HaveOccurred())
		userID = u.ID
	})

	Describe("CreateOIDCIdentity + FindOIDCIdentity", func() {
		It(
			"persists and looks up by (provider, subject) with owner preloaded",
			func() {
				_, err := store.CreateOIDCIdentity(ctx, CreateOIDCIdentityParams{
					Provider: "google", Subject: "sub-1",
					Email: "owner@example.com", OwnerID: userID,
				})
				Expect(err).NotTo(HaveOccurred())

				got, err := store.FindOIDCIdentity(ctx, "google", "sub-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(got.Edges.Owner).NotTo(BeNil())
				Expect(got.Edges.Owner.ID).To(Equal(userID))
			},
		)

		It("returns NotFound when the (provider, subject) is absent", func() {
			_, err := store.FindOIDCIdentity(ctx, "google", "missing")
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})
})
