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

	"github.com/datahearth/streamline/internal/config"
)

var _ = Describe("applyMemoryBounds", Label("unit", "bittorrent"), func() {
	It("lowers every peer-pool default it names when nothing is set", func() {
		cc := antorrent.NewDefaultClientConfig()
		before := *cc

		applyMemoryBounds(cc, resolveBounds(config.DownloadClientEntry{}))

		// Guards against an upstream bump quietly landing a default below
		// ours, which would make these settings a widening rather than a cap.
		// Only the default path: a configured value may well exceed upstream.
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
		applyMemoryBounds(cc, resolveBounds(config.DownloadClientEntry{}))

		Expect(cc.MaxAllocPeerRequestDataPerConn).
			To(BeNumerically(">=", 8*(16<<10)))
	})

	It("keeps the known-peer high water above the low water at every valid peer count", func() {
		// anacrolix drops known peers down to HighWater and keeps asking for
		// more while at or under LowWater; inverting them would make the
		// pruner run against the announcer. Low water is never set, so
		// anacrolix's default is the one to stay above.
		for peers := 0; peers <= 200; peers++ {
			cc := antorrent.NewDefaultClientConfig()
			applyMemoryBounds(cc, resolveBounds(config.DownloadClientEntry{
				MaxPeersPerTorrent: peers,
			}))

			Expect(cc.TorrentPeersHighWater).
				To(BeNumerically(">", cc.TorrentPeersLowWater),
					"max_peers_per_torrent %d", peers)
		}
	})
})

var _ = Describe("resolveBounds", Label("unit", "bittorrent"), func() {
	It("gives exactly the defaults for an entry that sets nothing", func() {
		Expect(resolveBounds(config.DownloadClientEntry{})).To(Equal(peerBounds{
			peersPerTorrent: 15,
			halfOpenConns:   20,
			knownPeers:      100,
			unverifiedBytes: 16 << 20,
			torrents:        500,
		}))
	})

	It("carries each set value through", func() {
		b := resolveBounds(config.DownloadClientEntry{
			MaxPeersPerTorrent: 60,
			MaxUnverifiedMB:    128,
			MaxTorrents:        2000,
		})

		Expect(b.peersPerTorrent).To(Equal(60))
		Expect(b.unverifiedBytes).To(Equal(int64(128 << 20)))
		Expect(b.torrents).To(Equal(2000))
	})

	It("scales half-open and known peers with the peer count", func() {
		b := resolveBounds(config.DownloadClientEntry{MaxPeersPerTorrent: 60})

		Expect(b.halfOpenConns).To(Equal(80))
		Expect(b.knownPeers).To(Equal(400))
	})

	It("never lowers half-open or known peers below their floors", func() {
		b := resolveBounds(config.DownloadClientEntry{MaxPeersPerTorrent: 5})

		Expect(b.peersPerTorrent).To(Equal(5))
		Expect(b.halfOpenConns).To(Equal(minHalfOpenConns))
		Expect(b.knownPeers).To(Equal(minKnownPeersPerTorrent))
	})

	It("reaches the anacrolix config through newClientConfig", func() {
		cc := newClientConfig(config.DownloadClientEntry{
			DownloadDir:        "/srv/media/downloads",
			MaxPeersPerTorrent: 60,
			MaxUnverifiedMB:    128,
		}, nil, nil, nil)

		Expect(cc.EstablishedConnsPerTorrent).To(Equal(60))
		Expect(cc.TotalHalfOpenConns).To(Equal(80))
		Expect(cc.TorrentPeersHighWater).To(Equal(400))
		Expect(cc.MaxUnverifiedBytes).To(Equal(int64(128 << 20)))
	})
})

var _ = Describe("Engine.torrentCap", Label("unit", "bittorrent"), func() {
	It("reads the resolved max_torrents", func() {
		Expect((&Engine{maxTorrents: 3}).torrentCap()).To(Equal(3))
	})

	It("falls back to the default on an Engine not built by New", func() {
		Expect((&Engine{}).torrentCap()).To(Equal(defaultMaxTorrents))
	})
})

var _ = Describe("engine content storage", Label("unit", "bittorrent"), func() {
	It("reports no capacity, so unverified bytes are bounded per torrent", func() {
		// anacrolix shares one request order, and so one MaxUnverifiedBytes
		// count, across every torrent whose storage reports the same capacity
		// (torrent-piece-request-order.go); with none, each torrent has its
		// own. bounds.go and the docs describe the bound as per torrent, and
		// would be wrong the day this storage starts reporting one.
		dir := GinkgoT().TempDir()
		src := filepath.Join(dir, "src")
		Expect(os.MkdirAll(src, 0o755)).To(Succeed())
		content := make([]byte, 64<<10)
		Expect(
			os.WriteFile(filepath.Join(src, "a.bin"), content, 0o644),
		).To(Succeed())
		info := metainfo.Info{PieceLength: 16 << 10}
		Expect(info.BuildFromFilePath(src)).To(Succeed())

		completion := storage.NewMapPieceCompletion()
		st, _ := newContentStorage(filepath.Join(dir, "dl"), completion)
		DeferCleanup(st.Close)
		ctx := context.Background()
		impl, err := st.OpenTorrent(ctx, &info, metainfo.Hash{1})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(impl.Close)

		Expect(impl.Capacity).To(BeNil())
	})
})
