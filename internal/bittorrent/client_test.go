package bittorrent

import (
	"context"
	"net"

	"github.com/anacrolix/torrent/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/download"
)

var _ = Describe("specFromSource", Label("unit", "bittorrent"), func() {
	It("parses a magnet URI", func() {
		spec, magnet, raw, err := specFromSource(download.TorrentSource{
			Magnet: "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd&dn=test",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.InfoHash.HexString()).To(
			Equal("aabbccddeeff00112233445566778899aabbccdd"))
		Expect(magnet).NotTo(BeEmpty())
		Expect(raw).To(BeNil())
	})

	It("rejects an empty source", func() {
		_, _, _, err := specFromSource(download.TorrentSource{})
		Expect(err).To(HaveOccurred())
	})

	It("rejects garbage torrent bytes", func() {
		_, _, _, err := specFromSource(download.TorrentSource{
			Bytes: []byte("not bencode"),
		})
		Expect(err).To(HaveOccurred())
	})

	// An uploaded .torrent skips the indexer fetch, so this is the only place
	// the domain ceiling gets applied to it. The bytes are refused on size
	// before metainfo.Load ever looks at them, so garbage of the right size
	// still reports the size, not a parse failure.
	It("rejects torrent bytes over the fetch-path ceiling", func() {
		_, _, _, err := specFromSource(download.TorrentSource{
			Bytes: make([]byte, download.MaxTorrentFileSize+1),
		})
		Expect(err).To(MatchError(ContainSubstring("over the")))
	})

	It("accepts bytes exactly at the ceiling, failing only on parse", func() {
		_, _, _, err := specFromSource(download.TorrentSource{
			Bytes: make([]byte, download.MaxTorrentFileSize),
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("parse torrent file"))
	})
})

var _ = Describe("dropUnusableTrackers", Label("unit", "bittorrent"), func() {
	It("keeps announce URLs that survive a parse round-trip", func() {
		in := [][]string{{
			"http://127.0.0.1:9117/announce",
			"udp://tracker.example:1337/announce",
		}}
		out, dropped := dropUnusableTrackers(in)
		Expect(dropped).To(Equal(0))
		Expect(out).To(Equal(in))
	})

	// The real shape: a magnet whose tr= param swallowed the XML tag that
	// followed it in the feed. url.Parse accepts it and re-escapes on
	// String(), which is exactly the inequality anacrolix asserts on — the
	// panic reached both the request goroutine and the announcer's.
	It("drops an announce URL that does not round-trip", func() {
		out, dropped := dropUnusableTrackers([][]string{{
			"http://127.0.0.1:9117/announce</link>",
			"http://127.0.0.1:9117/announce",
		}})
		Expect(dropped).To(Equal(1))
		Expect(out).To(Equal([][]string{{"http://127.0.0.1:9117/announce"}}))
	})

	It("drops a tier that loses every URL, rather than leaving it empty", func() {
		out, dropped := dropUnusableTrackers([][]string{
			{"http://ok.example/announce"},
			{"http://bad.example/announce</link>"},
		})
		Expect(dropped).To(Equal(1))
		Expect(out).To(Equal([][]string{{"http://ok.example/announce"}}))
	})
})

var _ = Describe("Engine.status", Label("unit", "bittorrent"), func() {
	// A partial selection must never block completion: BytesMissing counts
	// the skipped file's pieces forever, which is exactly what wantedMissing
	// exists to route around (spec §3.2).
	It(
		"reports seeding, not downloading, when a skipped file is the only gap",
		func() {
			t := newPartialTorrent()
			e := &Engine{state: map[string]*torrentState{}}
			e.prioritize(t, "all", nil)
			t.Files()[0].SetPriority(types.PiecePriorityNone)
			Expect(t.BytesMissing()).NotTo(BeZero(),
				"fixture invariant: the skipped file's corrupted piece must still read as missing")

			Expect(
				e.status(t, t.InfoHash().HexString()),
			).To(Equal(download.StatusSeeding))
		},
	)

	// The window between AddTorrentSpec and startWhenReady's priority pass:
	// every file is still at PiecePriorityNone, so the wanted-file totals
	// the completed branch divides by are 0. Reporting seeding there hands
	// the download monitor an import-ready torrent that holds nothing, and
	// ratio and progress both come back 0 for the same reason.
	It("reports fetching until file priorities have been applied", func() {
		t := newTestTorrent()
		e := &Engine{state: map[string]*torrentState{}}
		hash := t.InfoHash().HexString()

		Expect(wantedBytes(t)).To(BeZero(),
			"fixture invariant: an unprioritized torrent wants no bytes")
		Expect(wantedMissing(t)).To(BeZero(),
			"fixture invariant: which leaves the completed branch nothing to miss")
		Expect(e.status(t, hash)).To(Equal(download.StatusFetching))

		e.prioritize(t, "all", nil)

		Expect(e.status(t, hash)).To(Equal(download.StatusSeeding))
		Expect(ratio(4096, wantedBytes(t))).To(BeNumerically(">", 0))
	})
})

var _ = Describe("Engine.SetListenPort", Label("unit", "bittorrent"), func() {
	newEngine := func() *Engine {
		GinkgoHelper()
		pc, err := newRebindablePacketConn(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		ln, err := newRebindableListener(net.IPv4(127, 0, 0, 1), 0)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(pc.Close()).To(Succeed())
			Expect(ln.Close()).To(Succeed())
		})
		return &Engine{packetConn: pc, listener: ln}
	}

	It("moves both sockets to the new port", func() {
		e := newEngine()
		free, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		Expect(err).NotTo(HaveOccurred())
		port := uint16(free.Addr().(*net.TCPAddr).Port)
		Expect(free.Close()).To(Succeed())

		Expect(e.SetListenPort(context.Background(), port)).To(Succeed())

		Expect(e.listener.Addr().(*net.TCPAddr).Port).To(Equal(int(port)))
		Expect(e.packetConn.LocalAddr().(*net.UDPAddr).Port).To(Equal(int(port)))
	})

	It("is a no-op when the port already matches", func() {
		e := newEngine()
		before := e.listener.Addr().String()
		port := uint16(e.listener.Addr().(*net.TCPAddr).Port)

		Expect(e.SetListenPort(context.Background(), port)).To(Succeed())
		Expect(e.listener.Addr().String()).To(Equal(before))
	})

	It("rejects port zero", func() {
		e := newEngine()
		Expect(e.SetListenPort(context.Background(), 0)).NotTo(Succeed())
	})

	// Every spec above already depends on this: a successful move ends in the
	// DHT announce, and these engines carry no anacrolix client at all. Named
	// so a regression reads as the guard it is rather than as an unrelated
	// panic in "moves both sockets to the new port".
	It("announces to no DHT server when the engine has no client", func() {
		e := newEngine()
		Expect(func() { e.announceToDHT(context.Background()) }).NotTo(Panic())
	})
})
