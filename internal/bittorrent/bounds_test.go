package bittorrent

import (
	"context"
	"os"
	"path/filepath"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
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
		Expect(cc.MaxAllocPeerRequestDataPerConn).
			To(BeNumerically("<", before.MaxAllocPeerRequestDataPerConn))
	})

	It("still buffers several standard-size peer requests per connection", func() {
		// anacrolix refuses a peer request longer than this bound outright,
		// and peers request 16 KiB blocks; a bound near the block size would
		// refuse clients asking for larger ones and serialise every upload.
		cc := antorrent.NewDefaultClientConfig()
		applyMemoryBounds(cc)

		Expect(cc.MaxAllocPeerRequestDataPerConn).
			To(BeNumerically(">=", 8*(16<<10)))
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

var _ = Describe("the engine's content storage", Label("unit", "bittorrent"), func() {
	It("reports no capacity, so the unverified-bytes bound applies per torrent", func() {
		// anacrolix shares one request order, and so one MaxUnverifiedBytes
		// count, across every torrent whose storage reports the same capacity
		// (torrent-piece-request-order.go); with none, each torrent has its
		// own. bounds.go and the docs describe the bound as per torrent, and
		// would be wrong the day this storage starts reporting one.
		dir := GinkgoT().TempDir()
		src := filepath.Join(dir, "src")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(src, "a.bin"), make([]byte, 64<<10), 0o644)).
			To(Succeed())
		info := metainfo.Info{PieceLength: 16 << 10}
		Expect(info.BuildFromFilePath(src)).To(Succeed())

		st, _ := newContentStorage(filepath.Join(dir, "dl"), storage.NewMapPieceCompletion())
		DeferCleanup(st.Close)
		impl, err := st.OpenTorrent(context.Background(), &info, metainfo.Hash{1})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(impl.Close)

		Expect(impl.Capacity).To(BeNil())
	})
})
