package bittorrent

import (
	antorrent "github.com/anacrolix/torrent"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("applyMemoryBounds", Label("unit", "bittorrent"), func() {
	It("lowers every peer-pool default it names", func() {
		cc := antorrent.NewDefaultClientConfig()
		before := *cc

		applyMemoryBounds(cc)

		// Guards against an upstream bump quietly landing a default below
		// ours, which would make these settings a widening rather than a cap.
		Expect(cc.EstablishedConnsPerTorrent).
			To(BeNumerically("<", before.EstablishedConnsPerTorrent))
		Expect(cc.TotalHalfOpenConns).
			To(BeNumerically("<", before.TotalHalfOpenConns))
		Expect(cc.TorrentPeersHighWater).
			To(BeNumerically("<", before.TorrentPeersHighWater))
		Expect(cc.MaxUnverifiedBytes).
			To(BeNumerically("<", before.MaxUnverifiedBytes))
	})

	It("keeps the known-peer high water above the low water", func() {
		// anacrolix drops peers down to LowWater once HighWater is crossed;
		// inverting them would make the pruner run against itself.
		cc := antorrent.NewDefaultClientConfig()
		applyMemoryBounds(cc)

		Expect(cc.TorrentPeersHighWater).
			To(BeNumerically(">", cc.TorrentPeersLowWater))
	})
})
