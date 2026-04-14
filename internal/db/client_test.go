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

var _ = Describe("DB.Tx", Label("unit", "db"), func() {
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

	When("the driver fails to begin", func() {
		It("propagates the error", func() {
			mock.ExpectBegin().WillReturnError(errors.New("begin fail"))
			_, err := store.Tx(ctx)
			Expect(err).To(MatchError(ContainSubstring("begin fail")))
		})
	})

	When("begin succeeds", func() {
		It("returns a Tx that can roll back", func() {
			mock.ExpectBegin()
			mock.ExpectRollback()
			tx, err := store.Tx(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(tx.Rollback()).To(Succeed())
		})

		It("returns a Tx that can commit", func() {
			mock.ExpectBegin()
			mock.ExpectCommit()
			tx, err := store.Tx(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(tx.Commit()).To(Succeed())
		})
	})
})
