// Package dbtest provides database-layer test helpers. Kept separate from
// internal/testutil to avoid an import cycle: testutil is imported by
// auth/config/db suites, but this helper depends on internal/db.
package dbtest

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
)

// SetupTestDB opens an in-memory SQLite database with auto-migration applied.
// Caller is responsible for closing the returned client (typically via DeferCleanup).
func SetupTestDB(ctx context.Context) *ent.Client {
	GinkgoHelper()
	client, err := db.Open(ctx, ":memory:")
	Expect(err).NotTo(HaveOccurred())
	return client
}
