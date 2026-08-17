package db

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	approle "github.com/datahearth/streamline/internal/role"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
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
			Email:        email,
			Role:         approle.Seed(user.Role(role)),
			AuthMethod:   user.AuthMethodLocal,
			DisplayName:  "Disp",
			PasswordHash: "h",
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
					Role:       approle.Seed(user.RoleMember),
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
			role := approle.Operator(user.RoleAdmin)
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

	Describe("UpdateUserRole", func() {
		It("demotes an admin while another admin remains", func() {
			a := create("a@example.com", "admin")
			create("b@example.com", "admin")

			updated, err := store.UpdateUserRole(
				ctx, a.ID, approle.Seed(user.RoleMember),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Role).To(Equal(user.RoleMember))

			got, err := store.FindUserByID(ctx, a.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Role).To(Equal(user.RoleMember))
		})

		It("promotes a member without consulting the guard", func() {
			m := create("m@example.com", "member")
			updated, err := store.UpdateUserRole(
				ctx, m.ID, approle.Seed(user.RoleAdmin),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Role).To(Equal(user.RoleAdmin))
		})

		When("the target is the only admin left", func() {
			It("refuses the demotion and leaves the row untouched", func() {
				a := create("a@example.com", "admin")
				create("m@example.com", "member")

				_, err := store.UpdateUserRole(
					ctx, a.ID, approle.Seed(user.RoleMember),
				)
				Expect(err).To(MatchError(ErrLastAdmin))

				got, err := store.FindUserByID(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.Role).To(Equal(user.RoleAdmin))
			})
		})

		// The count is read by the UPDATE itself, so the second demotion sees
		// the first one's effect rather than a value captured before it.
		It("re-reads the admin count on every write", func() {
			a := create("a@example.com", "admin")
			b := create("b@example.com", "admin")

			_, err := store.UpdateUserRole(
				ctx, a.ID, approle.Seed(user.RoleMember),
			)
			Expect(err).NotTo(HaveOccurred())

			_, err = store.UpdateUserRole(
				ctx, b.ID, approle.Seed(user.RoleMember),
			)
			Expect(err).To(MatchError(ErrLastAdmin))
			_, admins, err := store.ListUsers(ctx, ListUsersParams{
				Role: user.RoleAdmin,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(admins).To(Equal(1))
		})

		It("reports an unknown id as NotFound, not as the guard", func() {
			_, err := store.UpdateUserRole(
				ctx, 99999, approle.Seed(user.RoleMember),
			)
			Expect(ent.IsNotFound(err)).To(BeTrue())
			Expect(errors.Is(err, ErrLastAdmin)).To(BeFalse())
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

	Describe("last-admin guarded writes", func() {
		demote := func() UpdateUserParams {
			r := approle.Operator(user.RoleMember)
			return UpdateUserParams{Role: &r}
		}

		When("another admin exists", func() {
			It("updates the target", func() {
				a := create("admin1@example.com", "admin")
				create("admin2@example.com", "admin")

				got, err := store.UpdateUserUnlessLastAdmin(ctx, a.ID, demote())
				Expect(err).NotTo(HaveOccurred())
				Expect(got.Role).To(Equal(user.RoleMember))
			})

			It("deletes the target", func() {
				a := create("admin1@example.com", "admin")
				create("admin2@example.com", "admin")

				n, err := store.DeleteUserUnlessLastAdmin(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(Equal(1))
			})
		})

		When("the target is the only admin", func() {
			It("writes nothing", func() {
				a := create("solo@example.com", "admin")
				create("member@example.com", "member")

				_, err := store.UpdateUserUnlessLastAdmin(ctx, a.ID, demote())
				Expect(ent.IsNotFound(err)).To(BeTrue())

				n, err := store.DeleteUserUnlessLastAdmin(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(n).To(BeZero())

				still, err := store.FindUserByID(ctx, a.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(still.Role).To(Equal(user.RoleAdmin))
			})
		})
	})

	Describe("DeleteUser", func() {
		It("removes the row", func() {
			u := create("a@example.com", "admin")
			Expect(store.DeleteUser(ctx, u.ID)).To(Succeed())
			_, err := store.FindUserByID(ctx, u.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})

		// Runs against a file-backed DB so the versioned migrations — not
		// ent's auto-migrate — define the invites FK under test.
		It("cascades to invites the user created, on the migrated schema", func() {
			migrated, err := Open(
				ctx,
				filepath.Join(GinkgoT().TempDir(), "streamline.db"),
			)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { migrated.Close() })
			migratedStore := New(migrated)

			creator, err := migratedStore.CreateUser(ctx, CreateUserParams{
				Email:      "creator@example.com",
				Role:       approle.Seed(user.RoleAdmin),
				AuthMethod: user.AuthMethodLocal,
			})
			Expect(err).NotTo(HaveOccurred())
			inv, err := migratedStore.CreateInvite(ctx, CreateInviteParams{
				TokenHash:   "h1",
				Role:        invite.RoleMember,
				ExpiresAt:   time.Now().Add(time.Hour),
				CreatedByID: creator.ID,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(migratedStore.DeleteUser(ctx, creator.ID)).To(Succeed())

			_, err = migrated.Invite.Get(ctx, inv.ID)
			Expect(ent.IsNotFound(err)).To(BeTrue())
		})
	})
})
