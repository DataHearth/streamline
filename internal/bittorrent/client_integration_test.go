package bittorrent

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"time"

	analog "github.com/anacrolix/log"
	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/testutil/configtest"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

// freePort grabs an OS-assigned TCP port for the engine listener.
func freePort() uint16 {
	GinkgoHelper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}

// newSeeder builds a 2 MiB payload, its .torrent bytes, and a local
// anacrolix client seeding it. Returns the torrent bytes and seeder port.
func newSeeder(dir string) ([]byte, int) {
	GinkgoHelper()
	content := make([]byte, 2<<20)
	for i := range content {
		content[i] = byte(i % 251)
	}
	Expect(os.WriteFile(
		filepath.Join(dir, "payload.bin"), content, 0o644,
	)).To(Succeed())

	info := metainfo.Info{PieceLength: 256 << 10}
	Expect(info.BuildFromFilePath(filepath.Join(dir, "payload.bin"))).To(Succeed())
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
	cc.ListenPort = 0
	cc.Logger = analog.Default.WithFilterLevel(analog.Error)
	seeder, err := antorrent.NewClient(cc)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { seeder.Close() })
	st, err := seeder.AddTorrent(&mi)
	Expect(err).NotTo(HaveOccurred())
	<-st.GotInfo()
	return buf.Bytes(), seeder.LocalPort()
}

// newEngine spins an Engine on a temp dir wired to an in-memory store.
func newEngine(ctx context.Context, dlDir string, store db.Store) *Engine {
	GinkgoHelper()
	configtest.Setup(map[string]any{
		"download_clients": []map[string]any{{
			"name": "embedded", "client_type": "builtin",
			"download_dir": dlDir, "listen_port": int(freePort()),
			"enabled": true,
		}},
	})
	e, err := New(ctx, store)
	Expect(err).NotTo(HaveOccurred())
	return e
}

// connectToSeeder points the engine's torrent at the local seeder.
func connectToSeeder(e *Engine, hash string, seederPort int) {
	GinkgoHelper()
	t, err := e.torrent(hash)
	Expect(err).NotTo(HaveOccurred())
	t.AddPeers([]antorrent.PeerInfo{{
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: seederPort},
	}})
}

var _ = Describe("Engine download flow", Label("integration", "bittorrent"), func() {
	var (
		ctx          context.Context
		store        db.Store
		engine       *Engine
		dlDir        string
		torrentBytes []byte
		seederPort   int
	)

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

		torrentBytes, seederPort = newSeeder(seedDir)
		engine = newEngine(ctx, dlDir, store)
		DeferCleanup(func() { Expect(engine.Close()).To(Succeed()) })
	})

	It("downloads a torrent to completion and reports status", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(hash).To(HaveLen(40))
		connectToSeeder(engine, hash, seederPort)

		Eventually(func() download.TorrentStatus {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Status
		}).WithTimeout(60 * time.Second).WithPolling(200 * time.Millisecond).
			Should(Equal(download.StatusSeeding))

		got, err := os.ReadFile(filepath.Join(dlDir, "payload.bin"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(2 << 20))

		t, err := engine.GetTorrent(ctx, hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(t.Progress).To(BeNumerically("==", 1))
		Expect(t.SavePath).To(Equal(dlDir))

		list, err := engine.ListTorrents(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(1))

		sessions, err := store.ListTorrentSessions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(sessions).To(HaveLen(1))
		Expect(sessions[0].InfoHash).To(Equal(hash))
	})

	It("returns ErrTorrentNotFound for unknown hashes", func() {
		_, err := engine.GetTorrent(ctx,
			"0000000000000000000000000000000000000000")
		Expect(err).To(MatchError(download.ErrTorrentNotFound))
	})

	It("is a functioning download.Client for TestConnection", func() {
		Expect(engine.TestConnection(ctx)).To(Succeed())
	})

	It("pauses and resumes with persisted state", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(engine.PauseTorrent(ctx, hash)).To(Succeed())
		t, err := engine.GetTorrent(ctx, hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(t.Status).To(Equal(download.StatusPaused))
		sessions, err := store.ListTorrentSessions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(sessions[0].Paused).To(BeTrue())

		Expect(engine.ResumeTorrent(ctx, hash)).To(Succeed())
		connectToSeeder(engine, hash, seederPort)
		Eventually(func() download.TorrentStatus {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Status
		}).WithTimeout(60 * time.Second).WithPolling(200 * time.Millisecond).
			Should(Equal(download.StatusSeeding))
	})

	It("reports fetching while a magnet's metadata is unresolved", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Magnet: "magnet:?xt=urn:btih:" +
				"aabbccddeeff00112233445566778899aabbccdd&dn=test",
		})
		Expect(err).NotTo(HaveOccurred())
		t, err := engine.GetTorrent(ctx, hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(t.Status).To(Equal(download.StatusFetching))
	})

	It("reports stalled while downloading with no connected peers", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes,
		})
		Expect(err).NotTo(HaveOccurred())
		// Metadata is known immediately (.torrent source) but no seeder is
		// connected, so there is data missing and zero active peers.
		Eventually(func() download.TorrentStatus {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Status
		}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).
			Should(Equal(download.StatusStalled))
	})

	It("deletes the incomplete .part file for single-file torrents", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes,
		})
		Expect(err).NotTo(HaveOccurred())
		// Mirror anacrolix's on-disk layout for an in-progress single-file
		// torrent, whose partial data lives at "<name>.part".
		partPath := filepath.Join(dlDir, "payload.bin.part")
		Expect(os.WriteFile(partPath, []byte("partial"), 0o644)).To(Succeed())

		Expect(engine.RemoveTorrent(ctx, hash, true)).To(Succeed())
		_, err = os.Stat(partPath)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("removes a torrent and deletes its data on request", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes,
		})
		Expect(err).NotTo(HaveOccurred())
		connectToSeeder(engine, hash, seederPort)
		Eventually(func() download.TorrentStatus {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Status
		}).WithTimeout(60 * time.Second).WithPolling(200 * time.Millisecond).
			Should(Equal(download.StatusSeeding))

		Expect(engine.RemoveTorrent(ctx, hash, true)).To(Succeed())
		_, err = engine.GetTorrent(ctx, hash)
		Expect(err).To(MatchError(download.ErrTorrentNotFound))
		_, err = os.Stat(filepath.Join(dlDir, "payload.bin"))
		Expect(os.IsNotExist(err)).To(BeTrue())
		sessions, err := store.ListTorrentSessions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(sessions).To(BeEmpty())
	})

	It("restores completed torrents across restarts without redownload", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes,
		})
		Expect(err).NotTo(HaveOccurred())
		connectToSeeder(engine, hash, seederPort)
		Eventually(func() download.TorrentStatus {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Status
		}).WithTimeout(60 * time.Second).WithPolling(200 * time.Millisecond).
			Should(Equal(download.StatusSeeding))
		Expect(engine.Close()).To(Succeed())

		// Second engine boots from the same store + download dir; the seeder
		// is gone from its peer list, so completion must come from disk.
		engine = newEngine(ctx, dlDir, store)
		Eventually(func() download.TorrentStatus {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Status
		}).WithTimeout(30 * time.Second).WithPolling(200 * time.Millisecond).
			Should(Equal(download.StatusSeeding))
	})

	It("exposes files, trackers, and peers via Details", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes,
		})
		Expect(err).NotTo(HaveOccurred())
		connectToSeeder(engine, hash, seederPort)

		Eventually(func() int {
			d, derr := engine.Details(ctx, hash)
			Expect(derr).NotTo(HaveOccurred())
			return len(d.Files)
		}).WithTimeout(30 * time.Second).WithPolling(200 * time.Millisecond).
			Should(Equal(1))

		d, err := engine.Details(ctx, hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(d.Files[0].Path).To(Equal("payload.bin"))
		// anacrolix files default to PiecePriorityNone at the file-priority
		// layer; DownloadAll() drives downloads via piece priorities, not
		// File.SetPriority, so a freshly added file reports "skip" here.
		Expect(d.Files[0].Priority).To(Equal("skip"))

		Expect(engine.SetFilePriorities(ctx, hash, []FilePriority{
			{Index: 0, Priority: "high"},
		})).To(Succeed())
		d, err = engine.Details(ctx, hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(d.Files[0].Priority).To(Equal("high"))

		views := engine.ListViews(ctx)
		Expect(views).To(HaveLen(1))
		Expect(views[0].Hash).To(Equal(hash))
	})
})
