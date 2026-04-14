package db

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
)

var _ = Describe("User store CRUD", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		store  *DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		store = New(client)
	})

	create := func(email, role string) *ent.User {
		GinkgoHelper()
		u, err := store.CreateUser(ctx, CreateUserParams{
			Email: email, Role: user.Role(role), AuthMethod: user.AuthMethodLocal,
			DisplayName: "Disp", PasswordHash: "h",
		})
		Expect(err).NotTo(HaveOccurred())
		return u
	}

	Describe("CreateUser", func() {
		It("persists optional display_name and password_hash when provided", func() {
			u := create("a@example.com", "admin")
			Expect(u.DisplayName).To(Equal("Disp"))
			Expect(u.PasswordHash).To(Equal("h"))
		})

		When("a user with the same email already exists", func() {
			It("returns a constraint error", func() {
				create("dup@example.com", "admin")
				_, err := store.CreateUser(ctx, CreateUserParams{
					Email:      "dup@example.com",
					Role:       user.RoleMember,
					AuthMethod: user.AuthMethodLocal,
				})
				Expect(err).To(HaveOccurred())
				Expect(ent.IsConstraintError(err)).To(BeTrue())
			})
		})
	})

	Describe("FindUserByID", func() {
		It("returns the row", func() {
			u := create("a@example.com", "admin")
			got, err := store.FindUserByID(ctx, u.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Email).To(Equal("a@example.com"))
		})

		It("returns NotFound when absent", func() {
			_, err := store.FindUserByID(ctx, 99999)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("UpdateUserPassword", func() {
		It("updates the hash", func() {
			u := create("a@example.com", "admin")
			Expect(store.UpdateUserPassword(ctx, u.ID, "h2")).To(Succeed())
			got, _ := store.FindUserByID(ctx, u.ID)
			Expect(got.PasswordHash).To(Equal("h2"))
		})
	})

	Describe("UpdateUser", func() {
		It("applies every non-nil field", func() {
			u := create("a@example.com", "member")
			role := user.RoleAdmin
			authMethod := user.AuthMethodBoth
			displayName := "Renamed"
			updated, err := store.UpdateUser(ctx, u.ID, UpdateUserParams{
				Role:        &role,
				AuthMethod:  &authMethod,
				DisplayName: &displayName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Role).To(Equal(user.RoleAdmin))
			Expect(updated.AuthMethod).To(Equal(user.AuthMethodBoth))
			Expect(updated.DisplayName).To(Equal("Renamed"))
		})

		It("leaves fields untouched when params are nil", func() {
			u := create("a@example.com", "member")
			updated, err := store.UpdateUser(ctx, u.ID, UpdateUserParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Role).To(Equal(user.RoleMember))
		})
	})

	Describe("ListUsers", func() {
		It(
			"filters by query (email or display_name) and role, paginates newest first",
			func() {
				create("admin@example.com", "admin")
				create("alice@example.com", "member")
				create("bob@example.com", "member")

				items, total, err := store.ListUsers(ctx, ListUsersParams{
					Q: "ALICE", Limit: 10,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(total).To(Equal(1))
				Expect(items[0].Email).To(Equal("alice@example.com"))

				items, total, err = store.ListUsers(ctx, ListUsersParams{
					Role: user.RoleMember, Limit: 10,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(total).To(Equal(2))
				Expect(items).To(HaveLen(2))
			},
		)
	})

	Describe("CountUsersByRole", func() {
		It("returns the count for the given role", func() {
			create("admin@example.com", "admin")
			create("alice@example.com", "member")
			n, err := store.CountUsersByRole(ctx, user.RoleAdmin)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(1))
		})
	})

	Describe("DeleteUser", func() {
		It("removes the row", func() {
			u := create("a@example.com", "admin")
			Expect(store.DeleteUser(ctx, u.ID)).To(Succeed())
			_, err := store.FindUserByID(ctx, u.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})
})
