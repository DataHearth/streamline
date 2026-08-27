package bittorrent

import (
	"context"
	"net"
	"os"
	"path/filepath"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
)

// newTestClient starts a network-disabled anacrolix client rooted so that
// its default file storage resolves back to dir. The default storage nests
// each torrent's data under baseDir/info.Name, and BuildFromFilePath sets
// info.Name to dir's own basename — so the base has to be dir's *parent*.
func newTestClient(dir string) *antorrent.Client {
	GinkgoHelper()
	cc := antorrent.NewDefaultClientConfig()
	cc.DataDir = filepath.Dir(dir)
	cc.NoDHT = true
	cc.DisableTrackers = true
	cc.DisablePEX = true
	cc.NoDefaultPortForwarding = true
	cc.ListenPort = 0
	cc.Slogger = engineSlogger()
	client, err := antorrent.NewClient(cc)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(client.Close()).To(BeEmpty()) })
	return client
}

// newTestTorrent builds a real single-piece, three-file torrent whose data
// already sits in the client's own DataDir, so it never needs a peer or a
// seeder: applyFilePriorities only reads/writes Torrent.Files(), and a
// locally-satisfied torrent resolves Info synchronously off the metainfo it
// was added with.
func newTestTorrent() *antorrent.Torrent {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	for i, name := range []string{"a.bin", "b.bin", "c.bin"} {
		Expect(os.WriteFile(
			filepath.Join(dir, name), []byte{byte(i), byte(i), byte(i)}, 0o644,
		)).To(Succeed())
	}
	// BuildFromFilePath overwrites Name with dir's own basename, so setting
	// it here first would be dead.
	info := metainfo.Info{PieceLength: 1 << 20}
	Expect(info.BuildFromFilePath(dir)).To(Succeed())
	ib, err := bencode.Marshal(info)
	Expect(err).NotTo(HaveOccurred())
	mi := metainfo.MetaInfo{InfoBytes: ib}

	t, err := newTestClient(dir).AddTorrent(&mi)
	Expect(err).NotTo(HaveOccurred())
	<-t.GotInfo()
	// AddTorrent never hash-checks by itself — pieces stay "unknown" until
	// something requests a check, so BytesCompleted reads 0 even though the
	// data is already on disk. Callers that read completion (not just
	// priority) need it verified up front.
	Expect(t.VerifyDataContext(context.Background())).To(Succeed())
	return t
}

// newPartialTorrent builds a three-file, three-piece torrent (one piece per
// file, via a piece length matching each file's size) where the first
// file's on-disk bytes are corrupted *after* the metainfo hash was computed
// from the correct content. Verifying then leaves that one piece genuinely
// incomplete — the shape a file whose data was never downloaded takes —
// while the other two verify complete, which newTestTorrent's single-piece
// layout can't produce: there, one shared piece is atomically complete or
// not, so a skip can never leave it partially missing.
func newPartialTorrent() *antorrent.Torrent {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	for i, name := range []string{"a.bin", "b.bin", "c.bin"} {
		Expect(os.WriteFile(
			filepath.Join(dir, name), []byte{byte(i), byte(i), byte(i)}, 0o644,
		)).To(Succeed())
	}
	info := metainfo.Info{PieceLength: 3}
	Expect(info.BuildFromFilePath(dir)).To(Succeed())
	ib, err := bencode.Marshal(info)
	Expect(err).NotTo(HaveOccurred())
	mi := metainfo.MetaInfo{InfoBytes: ib}

	Expect(os.WriteFile(
		filepath.Join(dir, "a.bin"), []byte{9, 9, 9}, 0o644,
	)).To(Succeed())

	t, err := newTestClient(dir).AddTorrent(&mi)
	Expect(err).NotTo(HaveOccurred())
	<-t.GotInfo()
	Expect(t.VerifyDataContext(context.Background())).To(Succeed())
	return t
}

var _ = Describe("applyFilePriorities", Label("unit", "bittorrent"), func() {
	var t *antorrent.Torrent

	BeforeEach(func() {
		t = newTestTorrent()
		Expect(t.Files()).To(HaveLen(3))
	})

	It("mode all bumps every undecided file", func() {
		applyFilePriorities(t, "all", nil)
		for _, f := range t.Files() {
			Expect(f.Priority()).To(Equal(types.PiecePriorityNormal))
		}
	})

	It("mode pending bumps nothing", func() {
		applyFilePriorities(t, "pending", nil)
		for _, f := range t.Files() {
			Expect(f.Priority()).To(Equal(types.PiecePriorityNone))
		}
	})

	It("mode explicit wants exactly the listed files, twice in a row", func() {
		applyFilePriorities(t, "explicit", []int{1})
		applyFilePriorities(t, "explicit", []int{1}) // the restart re-run
		Expect(t.Files()[0].Priority()).To(Equal(types.PiecePriorityNone))
		Expect(t.Files()[1].Priority()).To(Equal(types.PiecePriorityNormal))
		Expect(t.Files()[2].Priority()).To(Equal(types.PiecePriorityNone))
	})
})

var _ = Describe("parseHash", Label("unit", "bittorrent"), func() {
	It("parses a 40-char hex hash", func() {
		h, err := parseHash("aabbccddeeff00112233445566778899aabbccdd")
		Expect(err).NotTo(HaveOccurred())
		Expect(h.HexString()).To(
			Equal("aabbccddeeff00112233445566778899aabbccdd"))
	})

	It("rejects bad input as torrent-not-found", func() {
		for _, s := range []string{"", "zz", "aabb", "not-hex-at-all!"} {
			_, err := parseHash(s)
			Expect(err).To(MatchError(download.ErrTorrentNotFound), s)
		}
	})
})

var _ = Describe("resolveBindIP", Label("unit", "bittorrent"), func() {
	It("returns no IP for an empty interface (all interfaces)", func() {
		ip, err := resolveBindIP("")
		Expect(err).NotTo(HaveOccurred())
		Expect(ip).To(BeNil())
	})

	It("uses a literal IPv4 address verbatim", func() {
		ip, err := resolveBindIP("10.11.12.13")
		Expect(err).NotTo(HaveOccurred())
		Expect(ip.Equal(net.ParseIP("10.11.12.13"))).To(BeTrue())
	})

	It("uses a literal IPv6 address verbatim", func() {
		ip, err := resolveBindIP("2001:db8::1")
		Expect(err).NotTo(HaveOccurred())
		Expect(ip.Equal(net.ParseIP("2001:db8::1"))).To(BeTrue())
	})

	It("fails start when the named interface does not exist", func() {
		_, err := resolveBindIP("streamline-no-such-iface0")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("newClientConfig", Label("unit", "bittorrent"), func() {
	entry := config.DownloadClientEntry{DownloadDir: "/srv/media/downloads"}

	It("disables WebTorrent whether bound or not", func() {
		Expect(newClientConfig(entry, nil, nil).DisableWebtorrent).To(BeTrue())
		Expect(newClientConfig(entry, net.ParseIP("10.11.12.13"), nil).
			DisableWebtorrent).To(BeTrue())
	})

	It("leaves the default dialers alone when unbound", func() {
		cc := newClientConfig(entry, nil, nil)
		Expect(cc.DialForPeerConns).To(BeTrue())
		Expect(cc.DisableIPv6).To(BeFalse())
		Expect(cc.NoDefaultPortForwarding).To(BeFalse())
	})

	It("binds fail-closed when given an interface IP", func() {
		cc := newClientConfig(entry, net.ParseIP("10.11.12.13"), nil)
		Expect(cc.DialForPeerConns).To(BeFalse())
		Expect(cc.DisableIPv6).To(BeTrue())
		Expect(cc.NoDefaultPortForwarding).To(BeTrue())
	})

	It("carries the entry's knobs through", func() {
		bound := config.DownloadClientEntry{
			DownloadDir: entry.DownloadDir,
			DisableDHT:  true,
			ListenPort:  12345,
		}
		cc := newClientConfig(bound, nil, nil)
		Expect(cc.NoDHT).To(BeTrue())
		Expect(cc.ListenPort).To(Equal(12345))
		Expect(cc.DataDir).To(Equal(bound.DownloadDir))
	})
})
