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

var _ = Describe("Movie store driver-error paths", Label("unit", "db"), func() {
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

	Describe("FilterMovies", func() {
		When("the count query errors", func() {
			It("propagates the error", func() {
				driverErr := errors.New("count fail")
				mock.ExpectQuery(`SELECT COUNT.* FROM .movies.`).
					WillReturnError(driverErr)
				_, _, err := store.FilterMovies(ctx, FilterMoviesParams{Limit: 10})
				Expect(err).To(MatchError(driverErr))
			})
		})

		When("the list query errors", func() {
			It("propagates the error", func() {
				driverErr := errors.New("list fail")
				mock.ExpectQuery(`SELECT COUNT.* FROM .movies.`).
					WillReturnRows(
						sqlmock.NewRows([]string{"count"}).AddRow(0),
					)
				mock.ExpectQuery(`SELECT .* FROM .movies.`).
					WillReturnError(driverErr)
				_, _, err := store.FilterMovies(ctx, FilterMoviesParams{Limit: 10})
				Expect(err).To(MatchError(driverErr))
			})
		})
	})

	Describe("FindMoviesByTMDBIDs", func() {
		When("the input slice is empty", func() {
			It("returns nil without querying", func() {
				movies, err := store.FindMoviesByTMDBIDs(ctx, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(movies).To(BeNil())
			})
		})
	})
})
