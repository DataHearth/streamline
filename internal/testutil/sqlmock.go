package testutil

import (
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

// MockEntClient returns an *ent.Client backed by a go-sqlmock driver plus the
// sqlmock.Sqlmock handle used to register query/exec expectations.
//
// Use this for tests that need to drive specific DB-layer errors (Count fails,
// Create fails, Rollback fails, etc.) into ent-using services — scenarios that
// a real in-memory SQLite cannot produce on demand.
//
// The caller is responsible for closing the client (typically via
// DeferCleanup) and asserting mock.ExpectationsWereMet() at the end of the
// spec if strict ordering matters.
func MockEntClient() (*ent.Client, sqlmock.Sqlmock) {
	GinkgoHelper()
	rawDB, mock, err := sqlmock.New(
		sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp),
	)
	Expect(err).NotTo(HaveOccurred())

	drv := entsql.OpenDB(dialect.SQLite, rawDB)
	client := ent.NewClient(ent.Driver(drv))
	return client, mock
}
