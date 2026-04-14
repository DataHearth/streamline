package db

import (
	"context"
	"errors"

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
				Email: "a@b", Role: "admin", AuthMethod: "local",
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
