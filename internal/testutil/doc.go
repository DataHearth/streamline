// Package testutil provides shared test helpers for integration and e2e tests.
//
// Helpers in this package assume they are invoked from within a Ginkgo spec
// and fail the spec directly via Gomega assertions instead of returning errors.
// Each helper calls GinkgoHelper() so failures are attributed to the caller.
//
// Cycle-bound helpers (that depend on internal/db) live in the dbtest/
// subpackage so the root testutil package stays import-clean and can be
// used by auth/config/db test suites without cycles.
package testutil
