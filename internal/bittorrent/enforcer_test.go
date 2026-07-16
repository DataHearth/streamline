package bittorrent

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("shouldStopSeeding", Label("unit", "bittorrent"), func() {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)

	It("never stops when both limits are zero", func() {
		Expect(shouldStopSeeding(99, 0, now.Add(-999*time.Hour), 0, now)).
			To(BeFalse())
	})

	It("stops at the ratio limit", func() {
		Expect(shouldStopSeeding(2.0, 2.0, now, 0, now)).To(BeTrue())
		Expect(shouldStopSeeding(1.99, 2.0, now, 0, now)).To(BeFalse())
	})

	It("stops at the seed-time limit", func() {
		completed := now.Add(-73 * time.Hour)
		Expect(shouldStopSeeding(0, 0, completed, 72*time.Hour, now)).To(BeTrue())
		Expect(shouldStopSeeding(0, 0, now.Add(-time.Hour), 72*time.Hour, now)).
			To(BeFalse())
	})

	It("ignores seed-time when completion is unknown", func() {
		Expect(shouldStopSeeding(0, 0, time.Time{}, time.Hour, now)).To(BeFalse())
	})
})
