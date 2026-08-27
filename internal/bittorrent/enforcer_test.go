package bittorrent

import (
	"time"

	"github.com/anacrolix/torrent/types"
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

var _ = Describe("ratio", Label("unit", "bittorrent"), func() {
	// enforceOnce scores ratio against wantedBytes, not t.Length(): a
	// skipped file's bytes would otherwise sit in the denominator forever,
	// so a seed_ratio target set against what was actually downloaded could
	// never be reached (spec §3.2).
	It("lets a skipped-file torrent reach a ratio target", func() {
		t := newTestTorrent()
		applyFilePriorities(t, "all", nil)
		t.Files()[0].SetPriority(types.PiecePriorityNone)
		wanted := wantedBytes(t)
		Expect(wanted).To(BeNumerically("<", t.Length()))

		const target = 1.0
		uploaded := wanted // fully seeded against what was actually wanted
		Expect(ratio(uploaded, wanted)).To(BeNumerically(">=", target))
		Expect(ratio(uploaded, t.Length())).To(BeNumerically("<", target))
	})
})
