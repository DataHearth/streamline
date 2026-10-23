package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/role"

	"github.com/DATA-DOG/go-sqlmock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/testutil"
)

var _ = Describe("User store driver-error paths", Label("unit", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		mock   sqlmock.Sqlmock
		store  *DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, mock = testutil.MockEntClient()
		DeferCleanup(func() { client.Close() })
		DeferCleanup(func() { Expect(mock.ExpectationsWereMet()).To(Succeed()) })
		store = New(client)
	})

	Describe("FindUserByEmail", func() {
		It("propagates the driver error", func() {
			driverErr := errors.New("select fail")
			mock.ExpectQuery(`SELECT .* FROM .users.`).
				WillReturnError(driverErr)
			_, err := store.FindUserByEmail(ctx, "a@b")
			Expect(err).To(MatchError(driverErr))
		})
	})

	Describe("CreateUser", func() {
		It("propagates the driver error", func() {
			driverErr := errors.New("insert fail")
			mock.ExpectQuery(`INSERT INTO .users.`).
				WillReturnError(driverErr)
			_, err := store.CreateUser(ctx, CreateUserParams{
				Email: "a@b", Role: role.Seed(user.RoleAdmin), AuthMethod: "local",
			})
			Expect(err).To(MatchError(driverErr))
		})
	})

	Describe("CountUsers", func() {
		It("propagates the driver error", func() {
			driverErr := errors.New("count fail")
			mock.ExpectQuery(`SELECT COUNT.* FROM .users.`).
				WillReturnError(driverErr)
			_, err := store.CountUsers(ctx)
			Expect(err).To(MatchError(driverErr))
		})
	})

	Describe("ListUsers", func() {
		When("the count query errors", func() {
			It("propagates the error", func() {
				driverErr := errors.New("count fail")
				mock.ExpectQuery(`SELECT COUNT.* FROM .users.`).
					WillReturnError(driverErr)
				_, _, err := store.ListUsers(ctx, ListUsersParams{})
				Expect(err).To(MatchError(driverErr))
			})
		})

		When("the count succeeds but list query errors", func() {
			It("propagates the list error", func() {
				driverErr := errors.New("list fail")
				mock.ExpectQuery(`SELECT COUNT.* FROM .users.`).
					WillReturnRows(
						sqlmock.NewRows([]string{"count"}).AddRow(0),
					)
				mock.ExpectQuery(`SELECT .* FROM .users.`).
					WillReturnError(driverErr)
				_, _, err := store.ListUsers(ctx, ListUsersParams{})
				Expect(err).To(MatchError(driverErr))
			})
		})
	})
})

// Guards against password_hash and token_hash leaking through fmt/json on a
// raw ent row (e.g. a stray slog.Any or %+v) the way ApiKey.key_hash is
// already guarded via field.Sensitive().
var _ = Describe("hash field redaction", Label("unit", "db"), func() {
	It("omits User.password_hash from String() and JSON", func() {
		u := &ent.User{PasswordHash: "bcrypt$secret-hash"}
		Expect(u.String()).NotTo(ContainSubstring("secret-hash"))

		data, err := json.Marshal(u)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).NotTo(ContainSubstring("secret-hash"))
		Expect(string(data)).NotTo(ContainSubstring("password_hash"))
	})

	It("omits Invite.token_hash from String() and JSON", func() {
		inv := &ent.Invite{TokenHash: "sha256-secret-token"}
		Expect(inv.String()).NotTo(ContainSubstring("secret-token"))

		data, err := json.Marshal(inv)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).NotTo(ContainSubstring("secret-token"))
		Expect(string(data)).NotTo(ContainSubstring("token_hash"))
	})
})
