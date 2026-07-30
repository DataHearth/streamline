package bittorrent

import (
	"bytes"
	"context"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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

// engineBindIP keeps the engine loopback-only. Binding it also switches off
// anacrolix's UPnP discovery (see New), so a run neither multicasts SSDP nor
// leaves transient sockets behind on ports this suite is about to hand out.
const engineBindIP = "127.0.0.1"

// reserveListenPort picks a port the engine can bind on both protocols.
//
// The engine binds TCP *and* UDP on its configured port, and anacrolix only
// retries a taken port when it was asked for a dynamic one — a configured
// port that is busy is a hard start failure. Ports are therefore drawn from
// below the kernel's ephemeral range (32768-60999 on Linux), which nothing
// on the box can be auto-assigned: an OS-assigned port would come out of
// that shared pool, and probing it over TCP says nothing about whether the
// UDP half is free.
func reserveListenPort() uint16 {
	GinkgoHelper()
	for range 100 {
		port := uint16(10000 + rand.IntN(20000))
		if portBindable(port) {
			return port
		}
	}
	Fail("no port below the ephemeral range was free for TCP and UDP")
	return 0
}

func portBindable(port uint16) bool {
	addr := net.JoinHostPort(engineBindIP, strconv.Itoa(int(port)))
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	defer l.Close()
	p, err := net.ListenPacket("udp", addr)
	if err != nil {
		return false
	}
	defer p.Close()
	return true
}

// newSeeder builds a 2 MiB payload, its .torrent bytes, and a local
// anacrolix client seeding it. Returns the torrent bytes and seeder port.
func newSeeder(dir string) ([]byte, int) {
	GinkgoHelper()
	return newSeederOfSize(dir, 2<<20, 256<<10)
}

func newSeederOfSize(dir string, size int, pieceLen int64) ([]byte, int) {
	GinkgoHelper()
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 251)
	}
	Expect(os.WriteFile(
		filepath.Join(dir, "payload.bin"), content, 0o644,
	)).To(Succeed())

	info := metainfo.Info{PieceLength: pieceLen}
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
	cc.NoDefaultPortForwarding = true
	cc.ListenPort = 0
	cc.Logger = analog.Default.WithFilterLevel(analog.Error)
	seeder, err := antorrent.NewClient(cc)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(seeder.Close()).To(BeEmpty()) })
	st, err := seeder.AddTorrent(&mi)
	Expect(err).NotTo(HaveOccurred())
	<-st.GotInfo()
	return buf.Bytes(), seeder.LocalPort()
}

// newEngine spins an Engine on a temp dir wired to an in-memory store and
// registers its shutdown before returning, so a spec that fails part-way
// can never leave the listener bound.
//
// DHT is off and the engine is pinned to loopback so the suite talks to
// nothing but its own seeder: with DHT on, the engine resolves and queries
// the global bootstrap nodes and announces an infohash that is identical on
// every machine running this suite, then competes the resulting internet
// peers against the local seeder for connection slots.
//
// The returned stop closes the engine early — the restart spec needs that —
// and turns the registered cleanup into a no-op, because Engine.Close closes
// its stop channel and panics if called twice.
func newEngine(
	ctx context.Context,
	dlDir string,
	store db.Store,
) (*Engine, func()) {
	GinkgoHelper()
	configtest.Setup(map[string]any{
		"download_clients": []map[string]any{{
			"name": "embedded", "client_type": "builtin",
			"download_dir": dlDir, "listen_port": int(reserveListenPort()),
			"bind_interface": engineBindIP, "disable_dht": true,
			"enabled": true,
		}},
	})
	e, err := New(ctx, store)
	Expect(err).NotTo(HaveOccurred())
	closed := false
	stop := func() {
		GinkgoHelper()
		if closed {
			return
		}
		closed = true
		Expect(e.Close()).To(Succeed())
	}
	DeferCleanup(stop)
	return e, stop
}

// logSink collects everything tee'd off GinkgoWriter. It serializes access
// because the writes this spec hunts for come from anacrolix goroutines that
// outlive the call under test, so the buffer is read while they may still be
// logging.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// teeEngineLogs routes both log streams the engine can fail through into sink:
// slog (the engine's own) and anacrolix's package logger, which the engine
// snapshots from analog.Default and which otherwise writes straight to stderr,
// bypassing GinkgoWriter entirely.
func teeEngineLogs(sink *logSink) {
	GinkgoHelper()
	GinkgoWriter.TeeTo(sink)
	DeferCleanup(GinkgoWriter.ClearTeeWriters)
	prev := analog.Default
	DeferCleanup(func() { analog.Default = prev })
	analog.Default.SetHandlers(analog.StreamHandler{
		W: GinkgoWriter, Fmt: analog.LineFormatter,
	})
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
		stopEngine   func()
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
		engine, stopEngine = newEngine(ctx, dlDir, store)
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
		stopEngine()

		// Second engine boots from the same store + download dir; the seeder
		// is gone from its peer list, so completion must come from disk.
		engine, stopEngine = newEngine(ctx, dlDir, store)
		Eventually(func() download.TorrentStatus {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Status
		}).WithTimeout(30 * time.Second).WithPolling(200 * time.Millisecond).
			Should(Equal(download.StatusSeeding))
	})

	It("never writes piece completion into a closing store", func() {
		var sink logSink
		teeEngineLogs(&sink)

		tmp := GinkgoT().TempDir()
		seedDir := filepath.Join(tmp, "seed")
		Expect(os.MkdirAll(seedDir, 0o755)).To(Succeed())
		// 256 pieces instead of the shared seeder's 8: every piece that passes
		// its hash costs one completion write, so a fine piece length keeps
		// marks continuously in flight and makes the close land inside one of
		// them often enough to be reproducible.
		cycleBytes, cyclePort := newSeederOfSize(seedDir, 8<<20, 32<<10)

		for cycle := range 30 {
			dlDir := filepath.Join(tmp, "dl", strconv.Itoa(cycle))
			Expect(os.MkdirAll(dlDir, 0o755)).To(Succeed())
			cycleEngine, closeCycle := newEngine(ctx, dlDir, store)

			hash, err := cycleEngine.AddTorrent(ctx, download.TorrentSource{
				Bytes: cycleBytes,
			})
			Expect(err).NotTo(HaveOccurred())
			connectToSeeder(cycleEngine, hash, cyclePort)

			// Close while hashers are hot rather than after completion: a
			// completed torrent has, by construction, no mark left to race —
			// anacrolix only flips a piece's reported completion after
			// MarkComplete has returned (torrent.go:2647-2657).
			Eventually(func() float64 {
				t, terr := cycleEngine.GetTorrent(ctx, hash)
				Expect(terr).NotTo(HaveOccurred())
				return t.Progress
			}).WithTimeout(60 * time.Second).WithPolling(time.Millisecond).
				Should(BeNumerically(">", 0))
			closeCycle()

			// The next cycle boots a fresh engine off the same store, so drop
			// this cycle's session rather than have it re-added elsewhere.
			Expect(store.DeleteTorrentSessionByHash(ctx, hash)).To(Succeed())
		}

		logs := sink.String()
		Expect(logs).NotTo(ContainSubstring("database not open"))
		Expect(logs).NotTo(ContainSubstring("error marking piece"))
		Expect(logs).NotTo(ContainSubstring("after the storage closed"))
	})

	It("excludes skipped files from downloading until re-wanted", func() {
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{
			Bytes: torrentBytes,
		})
		Expect(err).NotTo(HaveOccurred())
		// Wait for the async default prioritization, then skip the only file
		// BEFORE any peer is known.
		Eventually(func() string {
			d, derr := engine.Details(ctx, hash)
			Expect(derr).NotTo(HaveOccurred())
			if len(d.Files) == 0 {
				return ""
			}
			return d.Files[0].Priority
		}).WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
			Should(Equal("normal"))
		Expect(engine.SetFilePriorities(ctx, hash, []FilePriority{
			{Index: 0, Priority: "skip"},
		})).To(Succeed())

		connectToSeeder(engine, hash, seederPort)
		// With the only file skipped there is no demand: a live local seeder
		// (which otherwise completes this torrent in well under a second)
		// transfers nothing.
		Consistently(func() float64 {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Progress
		}).WithTimeout(2 * time.Second).WithPolling(200 * time.Millisecond).
			Should(BeZero())

		// Re-wanting the file resumes real transfer to completion.
		Expect(engine.SetFilePriorities(ctx, hash, []FilePriority{
			{Index: 0, Priority: "normal"},
		})).To(Succeed())
		Eventually(func() download.TorrentStatus {
			t, terr := engine.GetTorrent(ctx, hash)
			Expect(terr).NotTo(HaveOccurred())
			return t.Status
		}).WithTimeout(60 * time.Second).WithPolling(200 * time.Millisecond).
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
		// startWhenReady bumps fresh files to Normal once metadata resolves —
		// file priorities are the engine's single demand source, so the
		// reported default matches what actually downloads.
		Eventually(func() string {
			d, derr := engine.Details(ctx, hash)
			Expect(derr).NotTo(HaveOccurred())
			return d.Files[0].Priority
		}).WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).
			Should(Equal("normal"))

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
