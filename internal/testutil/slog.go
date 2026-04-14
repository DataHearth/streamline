package testutil

import (
	"log/slog"

	"github.com/onsi/ginkgo/v2"
)

// InstallSlog swaps slog.Default to a text handler writing to GinkgoWriter
// (so records appear inline with spec output only on failure / -v mode) and
// returns a restore func. Intended for use in BeforeSuite:
//
//	var _ = BeforeSuite(func() { DeferCleanup(testutil.InstallSlog()) })
//
// Debug level is used so nothing in the package being tested gets filtered.
func InstallSlog() func() {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(
		ginkgo.GinkgoWriter,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	)))
	return func() { slog.SetDefault(prev) }
}
