package bittorrent

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"
)

var _ = Describe("uncachedPiece", Label("integration", "bittorrent"), func() {
	const pieceLen = 256 << 10

	var (
		dir     string
		info    metainfo.Info
		content []byte
		piece   storage.PieceImpl
	)

	residentPages := func(path string) uint64 {
		GinkgoHelper()
		f, err := os.Open(path)
		Expect(err).NotTo(HaveOccurred())
		defer f.Close()
		var st unix.Cachestat_t
		err = unix.Cachestat(uint(f.Fd()), &unix.CachestatRange{}, &st, 0)
		if errors.Is(err, unix.ENOSYS) {
			Skip("cachestat needs Linux 6.5")
		}
		Expect(err).NotTo(HaveOccurred())
		return st.Cache
	}

	evict := func(path string) {
		GinkgoHelper()
		f, err := os.Open(path)
		Expect(err).NotTo(HaveOccurred())
		defer f.Close()
		Expect(f.Sync()).To(Succeed())
		Expect(unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED)).To(Succeed())
		Expect(residentPages(path)).To(BeZero())
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		var fs unix.Statfs_t
		Expect(unix.Statfs(dir, &fs)).To(Succeed())
		if fs.Type == unix.TMPFS_MAGIC {
			Skip("tmpfs pages are the file itself and cannot be dropped")
		}
		torrentDir := filepath.Join(dir, "release")
		Expect(os.MkdirAll(torrentDir, 0o755)).To(Succeed())
		content = make([]byte, 600<<10)
		_, err := rand.Read(content)
		Expect(err).NotTo(HaveOccurred())
		// Piece 1 (256-512 KiB) straddles the two files.
		Expect(
			os.WriteFile(
				filepath.Join(torrentDir, "a.bin"),
				content[:300<<10],
				0o644,
			),
		).
			To(Succeed())
		Expect(
			os.WriteFile(
				filepath.Join(torrentDir, "b.bin"),
				content[300<<10:],
				0o644,
			),
		).
			To(Succeed())

		info = metainfo.Info{PieceLength: pieceLen}
		Expect(info.BuildFromFilePath(torrentDir)).To(Succeed())
		// After the build, which hashes every piece and so reads both files in.
		evict(filepath.Join(torrentDir, "a.bin"))
		evict(filepath.Join(torrentDir, "b.bin"))
		st, _ := newContentStorage(dir, storage.NewMapPieceCompletion())
		DeferCleanup(st.Close)
		impl, err := st.OpenTorrent(context.Background(), &info, metainfo.Hash{1})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(impl.Close)
		piece = impl.Piece(info.Piece(1))
	})

	It("reads the piece and leaves none of it cached", func() {
		b := make([]byte, pieceLen)
		n, err := piece.ReadAt(b, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(pieceLen))
		Expect(b).To(Equal(content[pieceLen : 2*pieceLen]))
		Expect(residentPages(filepath.Join(dir, "release", "a.bin"))).To(BeZero())
		Expect(residentPages(filepath.Join(dir, "release", "b.bin"))).To(BeZero())

		By("reading the same range plainly, which the cache does keep")
		f, err := os.Open(filepath.Join(dir, "release", "b.bin"))
		Expect(err).NotTo(HaveOccurred())
		defer f.Close()
		_, err = f.ReadAt(make([]byte, 64<<10), 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			residentPages(filepath.Join(dir, "release", "b.bin")),
		).To(BeNumerically(">", 0))
	})

	It("hashes through WriteTo without caching it", func() {
		var buf bytes.Buffer
		n, err := piece.(io.WriterTo).WriteTo(&buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(BeEquivalentTo(pieceLen))
		Expect(buf.Bytes()).To(Equal(content[pieceLen : 2*pieceLen]))
		Expect(residentPages(filepath.Join(dir, "release", "a.bin"))).To(BeZero())
		Expect(residentPages(filepath.Join(dir, "release", "b.bin"))).To(BeZero())
	})

	It("reads a missing file as the end of the data", func() {
		Expect(os.Remove(filepath.Join(dir, "release", "b.bin"))).To(Succeed())
		b := make([]byte, pieceLen)
		n, err := piece.ReadAt(b, 0)
		Expect(err).To(MatchError(io.EOF))
		Expect(n).To(Equal(300<<10 - pieceLen))
	})

	It("prefers the .part file of a file still downloading", func() {
		b := filepath.Join(dir, "release", "b.bin")
		Expect(os.Rename(b, b+".part")).To(Succeed())
		buf := make([]byte, pieceLen)
		n, err := piece.ReadAt(buf, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(pieceLen))
		Expect(buf).To(Equal(content[pieceLen : 2*pieceLen]))
	})
})
