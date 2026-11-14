package bittorrent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"time"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
	"github.com/datahearth/streamline/internal/testutil/nettest"
)

// newPackSeeder seeds a two-file torrent, pack/a.bin and pack/b.bin, each
// four whole pieces, so skipping one file leaves pieces no wanted file spans.
func newPackSeeder(dir string) ([]byte, int) {
	GinkgoHelper()
	const fileSize, pieceLen = 1 << 20, 256 << 10
	pack := filepath.Join(dir, "pack")
	Expect(os.MkdirAll(pack, 0o755)).To(Succeed())
	for n, name := range []string{"a.bin", "b.bin"} {
		content := make([]byte, fileSize)
		for i := range content {
			content[i] = byte((i + n*7) % 251)
		}
		Expect(os.WriteFile(filepath.Join(pack, name), content, 0o644)).
			To(Succeed())
	}
	info := metainfo.Info{PieceLength: pieceLen}
	Expect(info.BuildFromFilePath(pack)).To(Succeed())
	ib, err := bencode.Marshal(info)
	Expect(err).NotTo(HaveOccurred())
	mi := metainfo.MetaInfo{InfoBytes: ib}
	var buf bytes.Buffer
	Expect(mi.Write(&buf)).To(Succeed())

	cc := antorrent.NewDefaultClientConfig()
	cc.DataDir = dir
	cc.Seed = true
	cc.NoDHT = true
	cc.DisableTrackers = true
	cc.DisablePEX = true
	cc.NoDefaultPortForwarding = true
	cc.ListenPort = 0
	cc.DisableIPv6 = !nettest.IPv6()
	cc.Slogger = engineSlogger()
	seeder, err := antorrent.NewClient(cc)
	Expect(err).NotTo(HaveOccurred())
	var st *antorrent.Torrent
	DeferCleanup(func() { closeSeeder(seeder, st) })
	st, err = seeder.AddTorrent(&mi)
	Expect(err).NotTo(HaveOccurred())
	<-st.GotInfo()
	return buf.Bytes(), seeder.LocalPort()
}

var _ = Describe("Redundant seeders", Label("integration", "bittorrent"), func() {
	var (
		ctx          context.Context
		store        db.Store
		dlDir        string
		sweepEvery   time.Duration
		engine       *Engine
		torrentBytes []byte
		seederPort   int
	)

	activePeers := func(hash string) int {
		t, err := engine.torrent(hash)
		Expect(err).NotTo(HaveOccurred())
		return t.Stats().ActivePeers
	}
	status := func(hash string) download.TorrentStatus {
		t, err := engine.GetTorrent(ctx, hash)
		Expect(err).NotTo(HaveOccurred())
		return t.Status
	}
	// addSkipping adds the pack wanting only its first file and downloads
	// that from the seeder.
	addSkipping := func() string {
		GinkgoHelper()
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes, WantedFiles: []int{0},
		})
		Expect(err).NotTo(HaveOccurred())
		connectToSeeder(engine, hash, seederPort)
		Eventually(status).WithArguments(hash).
			WithTimeout(60 * time.Second).WithPolling(100 * time.Millisecond).
			Should(Equal(download.StatusSeeding))
		return hash
	}

	BeforeEach(func() {
		ctx = context.Background()
		tmp := GinkgoT().TempDir()
		seedDir := filepath.Join(tmp, "seed")
		dlDir = filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(seedDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(dlDir, 0o755)).To(Succeed())

		entClient := dbtest.SetupTestDB(ctx)
		DeferCleanup(entClient.Close)
		store = db.New(entClient)

		// The engine's own ticker must not race the passes most of these specs
		// run by hand.
		sweepEvery = time.Hour
		torrentBytes, seederPort = newPackSeeder(seedDir)
	})

	JustBeforeEach(func() {
		// Restored after the engine is closed: cleanups run last in, first
		// out, and the engine reads the interval once, as it starts.
		interval := seederSweepInterval
		seederSweepInterval = sweepEvery
		DeferCleanup(func() { seederSweepInterval = interval })
		engine, _ = newEngine(ctx, dlDir, store)
	})

	Context("on the engine's own schedule", func() {
		BeforeEach(func() { sweepEvery = 50 * time.Millisecond })

		It("closes the seeder without being asked", func() {
			hash := addSkipping()
			Eventually(activePeers).WithArguments(hash).
				WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
				Should(BeZero())
		})
	})

	It("closes a seeder once every wanted file is held, skipped files or not",
		func() {
			hash := addSkipping()

			// anacrolix's own mutually-complete drop needs every piece, and
			// the skipped file's are missing: the seeder stays connected.
			Consistently(activePeers).WithArguments(hash).
				WithTimeout(time.Second).WithPolling(100 * time.Millisecond).
				Should(Equal(1))

			Expect(engine.dropSeedersOnce(ctx)).To(Equal(1))
			Eventually(activePeers).WithArguments(hash).
				WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
				Should(BeZero())
		})

	It("keeps a seeder while a wanted file is incomplete", func() {
		hash := addSkipping()
		Expect(engine.dropSeedersOnce(ctx)).To(Equal(1))
		Eventually(activePeers).WithArguments(hash).
			WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
			Should(BeZero())

		// The skipped file is wanted again: the torrent is a download once
		// more, a pass leaves its peers alone, and the seeder it reconnects to
		// stays until the file is in.
		Expect(engine.SetWantedFiles(ctx, hash, []int{0, 1})).To(Succeed())
		t, err := engine.torrent(hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(holdsWantedData(t)).To(BeFalse())
		Expect(engine.dropSeedersOnce(ctx)).To(BeZero())

		connectToSeeder(engine, hash, seederPort)
		Eventually(func() int64 { return t.BytesMissing() }).
			WithTimeout(60 * time.Second).WithPolling(100 * time.Millisecond).
			Should(BeZero())
		Eventually(status).WithArguments(hash).
			WithTimeout(60 * time.Second).WithPolling(100 * time.Millisecond).
			Should(Equal(download.StatusSeeding))
	})

	It("leaves a selective grab still waiting for its keep-set alone", func() {
		mi, err := metainfo.Load(bytes.NewReader(torrentBytes))
		Expect(err).NotTo(HaveOccurred())
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Magnet:    "magnet:?xt=urn:btih:" + mi.HashInfoBytes().HexString(),
			Selective: true,
		})
		Expect(err).NotTo(HaveOccurred())
		connectToSeeder(engine, hash, seederPort)
		t, err := engine.torrent(hash)
		Expect(err).NotTo(HaveOccurred())
		Eventually(t.GotInfo()).WithTimeout(30 * time.Second).Should(BeClosed())
		Eventually(activePeers).WithArguments(hash).
			WithTimeout(30 * time.Second).WithPolling(50 * time.Millisecond).
			Should(Equal(1))

		Expect(engine.dropSeedersOnce(ctx)).To(BeZero())
		Expect(activePeers(hash)).To(Equal(1))
	})
})
