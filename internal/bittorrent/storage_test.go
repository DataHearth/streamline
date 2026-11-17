package bittorrent

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	g "github.com/anacrolix/generics"
	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
	"github.com/datahearth/streamline/internal/testutil/nettest"
)

const seasonName = "Show.S01"

// newSeasonSeeder seeds a folder of episodes whose lengths are not multiples
// of pieceLen, so each one shares its last piece with the next. No byte of
// the content is zero.
func newSeasonSeeder(dir string, sizes []int, pieceLen int64) ([]byte, int) {
	GinkgoHelper()
	root := filepath.Join(dir, seasonName)
	Expect(os.MkdirAll(root, 0o755)).To(Succeed())
	for i, size := range sizes {
		content := make([]byte, size)
		_, err := rand.NewChaCha8([32]byte{byte(i + 1)}).Read(content)
		Expect(err).NotTo(HaveOccurred())
		for j := range content {
			content[j] |= 1
		}
		Expect(os.WriteFile(
			filepath.Join(root, episodeName(i)), content, 0o644,
		)).To(Succeed())
	}
	info := metainfo.Info{PieceLength: pieceLen}
	Expect(info.BuildFromFilePath(root)).To(Succeed())
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

func episodeName(i int) string {
	return fmt.Sprintf("Show.S01E%02d.mkv", i+1)
}

var _ = Describe("Engine storage", Label("integration", "bittorrent"), func() {
	It("keeps a finished file whose shared piece the completion store lost", func() {
		ctx := context.Background()
		tmp := GinkgoT().TempDir()
		seedDir := filepath.Join(tmp, "seed")
		dlDir := filepath.Join(tmp, "dl")
		Expect(os.MkdirAll(seedDir, 0o755)).To(Succeed())
		entClient := dbtest.SetupTestDB(ctx)
		DeferCleanup(entClient.Close)
		store := db.New(entClient)

		const pieceLen = 256 << 10
		first := 4*pieceLen + 4321
		tb, port := newSeasonSeeder(seedDir, []int{first, 4 * pieceLen}, pieceLen)
		engine, stop := newEngine(ctx, dlDir, store)
		hash, err := engine.AddTorrent(ctx, download.TorrentSource{Bytes: tb})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(store.DeleteTorrentSessionByHash(ctx, hash)).To(Succeed())
		})
		connectToSeeder(engine, hash, port)
		Eventually(func() float64 {
			t, terr := engine.torrent(hash)
			Expect(terr).NotTo(HaveOccurred())
			return verifiedShare(t)
		}).WithTimeout(60 * time.Second).WithPolling(10 * time.Millisecond).
			Should(BeNumerically("==", 1))
		stop()

		// What a process killed mid-season leaves behind: E01 promoted off its
		// .part name, E02 still downloading, and the completion of the piece
		// they share never written — anacrolix persists a piece's false at
		// once but buffers its true until a checkpoint.
		dl := filepath.Join(dlDir, seasonName)
		Expect(os.Rename(
			filepath.Join(dl, episodeName(1)),
			filepath.Join(dl, episodeName(1)+".part"),
		)).To(Succeed())
		Expect(
			os.Chmod(filepath.Join(dl, episodeName(1)+".part"), 0o644),
		).To(Succeed())
		mi, err := metainfo.Load(bytes.NewReader(tb))
		Expect(err).NotTo(HaveOccurred())
		info, err := mi.UnmarshalInfo()
		Expect(err).NotTo(HaveOccurred())
		pc, err := storage.NewBoltPieceCompletion(sessionDir(dlDir))
		Expect(err).NotTo(HaveOccurred())
		for p := first / pieceLen; p < info.NumPieces(); p++ {
			Expect(pc.Set(
				metainfo.PieceKey{InfoHash: mi.HashInfoBytes(), Index: p},
				g.Some(false),
			)).To(Succeed())
		}
		Expect(pc.Close()).To(Succeed())

		engine, _ = newEngine(ctx, dlDir, store)
		connectToSeeder(engine, hash, port)
		Eventually(func() float64 {
			t, terr := engine.torrent(hash)
			Expect(terr).NotTo(HaveOccurred())
			return verifiedShare(t)
		}).WithTimeout(60 * time.Second).WithPolling(10 * time.Millisecond).
			Should(BeNumerically("==", 1))

		for i := range 2 {
			want, err := os.ReadFile(
				filepath.Join(seedDir, seasonName, episodeName(i)),
			)
			Expect(err).NotTo(HaveOccurred())
			got, err := os.ReadFile(filepath.Join(dl, episodeName(i)))
			Expect(err).NotTo(HaveOccurred())
			Expect(bytes.Equal(got, want)).To(BeTrue(),
				"%s: %d of %d bytes are zero",
				episodeName(i), bytes.Count(got, []byte{0}), len(got))
		}
	})
})
