package bittorrent

import (
	"time"

	antorrent "github.com/anacrolix/torrent"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("per-connection rates", Label("unit", "bittorrent"), func() {
	start := time.Date(2026, 11, 16, 22, 0, 0, 0, time.UTC)
	at := func(d time.Duration) time.Time { return start.Add(d) }

	var fast, slow *antorrent.PeerConn
	BeforeEach(func() {
		fast, slow = new(antorrent.PeerConn), new(antorrent.PeerConn)
	})

	It("measures nothing on a first read", func() {
		s := nextPeerSnapshot(nil, start, map[*antorrent.PeerConn]peerCounts{
			fast: {down: 1 << 30},
		})
		Expect(s.rates).To(BeEmpty())
		Expect(s.at).To(Equal(start))
	})

	It("measures each connection over the time between two reads", func() {
		s := nextPeerSnapshot(nil, start, map[*antorrent.PeerConn]peerCounts{
			fast: {down: 0, up: 0},
			slow: {down: 0, up: 0},
		})
		s = nextPeerSnapshot(
			s,
			at(2*time.Second),
			map[*antorrent.PeerConn]peerCounts{
				fast: {down: 12000, up: 400},
				slow: {down: 2000},
			},
		)
		Expect(s.rates).To(Equal(map[*antorrent.PeerConn]peerCounts{
			fast: {down: 6000, up: 200},
			slow: {down: 1000},
		}))
	})

	It("reads a connection that stopped sending as zero", func() {
		s := nextPeerSnapshot(nil, start, map[*antorrent.PeerConn]peerCounts{
			fast: {down: 50 << 20},
		})
		s = nextPeerSnapshot(
			s,
			at(2*time.Second),
			map[*antorrent.PeerConn]peerCounts{
				fast: {down: 50 << 20},
			},
		)
		r, ok := s.rates[fast]
		Expect(ok).To(BeTrue(), "measured, not missing")
		Expect(r).To(Equal(peerCounts{}))
	})

	It("keeps the last measure for a read too soon after it", func() {
		s := nextPeerSnapshot(
			nil,
			start,
			map[*antorrent.PeerConn]peerCounts{fast: {}},
		)
		s = nextPeerSnapshot(
			s,
			at(2*time.Second),
			map[*antorrent.PeerConn]peerCounts{
				fast: {down: 4000},
			},
		)
		// A second viewer polls 10 ms later: one chunk over that gap would
		// read as 1.6 MB/s.
		again := nextPeerSnapshot(s, at(2*time.Second+10*time.Millisecond),
			map[*antorrent.PeerConn]peerCounts{fast: {down: 4000 + 16<<10}})
		Expect(again).To(BeIdenticalTo(s))
		Expect(again.rates[fast]).To(Equal(peerCounts{down: 2000}))
	})

	It("measures nothing against a baseline left from a closed detail", func() {
		s := nextPeerSnapshot(
			nil,
			start,
			map[*antorrent.PeerConn]peerCounts{fast: {}},
		)
		s = nextPeerSnapshot(
			s,
			at(5*time.Minute),
			map[*antorrent.PeerConn]peerCounts{
				fast: {down: 300 << 20},
			},
		)
		Expect(s.rates).To(BeEmpty())
		Expect(s.at).To(Equal(at(5 * time.Minute)))
	})

	It("keeps the last measure for a read stamped before it", func() {
		// Two reads queued on the client lock can reach the baseline out of
		// their stamps' order.
		s := nextPeerSnapshot(
			nil,
			start,
			map[*antorrent.PeerConn]peerCounts{fast: {}},
		)
		s = nextPeerSnapshot(
			s,
			at(2*time.Second),
			map[*antorrent.PeerConn]peerCounts{
				fast: {down: 4000},
			},
		)
		late := nextPeerSnapshot(
			s,
			at(time.Second),
			map[*antorrent.PeerConn]peerCounts{
				fast: {down: 3000},
			},
		)
		Expect(late).To(BeIdenticalTo(s))
		Expect(late.rates[fast]).To(Equal(peerCounts{down: 2000}))
	})

	It("reads a connection new since the last read, or reset, as zero", func() {
		s := nextPeerSnapshot(nil, start, map[*antorrent.PeerConn]peerCounts{
			fast: {down: 9000},
		})
		s = nextPeerSnapshot(
			s,
			at(2*time.Second),
			map[*antorrent.PeerConn]peerCounts{
				fast: {down: 1000},
				slow: {down: 5000},
			},
		)
		// Looked up directly: Gomega's key matchers compare pointers by
		// what they point to, and both connections are zero values.
		r, ok := s.rates[fast]
		Expect(ok).To(BeTrue())
		Expect(r).To(Equal(peerCounts{}))
		_, ok = s.rates[slow]
		Expect(ok).To(BeFalse())
	})

	It("keeps a baseline per torrent", func() {
		e := &Engine{}
		e.peerRates("a", start, map[*antorrent.PeerConn]peerCounts{fast: {}})
		e.peerRates("b", start, map[*antorrent.PeerConn]peerCounts{slow: {}})

		rates := e.peerRates("a", at(2*time.Second),
			map[*antorrent.PeerConn]peerCounts{fast: {down: 4000}})
		Expect(rates[fast]).To(Equal(peerCounts{down: 2000}))
		Expect(e.peerReads).To(HaveKey("b"))
	})

	It("drops on the sampler's tick a baseline nobody has read for a while", func() {
		e := &Engine{sample: map[string]*rateMonitor{}}
		e.peerRates("closed", start, map[*antorrent.PeerConn]peerCounts{fast: {}})
		e.peerRates(
			"open",
			at(peerReadMaxAge),
			map[*antorrent.PeerConn]peerCounts{slow: {}},
		)

		e.sampleTorrents(
			nil,
			1,
			func() time.Time { return at(peerReadMaxAge + time.Second) },
		)
		Expect(e.peerReads).NotTo(HaveKey("closed"))
		Expect(e.peerReads).To(HaveKey("open"))
	})

	It("drops a forgotten torrent's baseline", func() {
		e := &Engine{
			state:  map[string]*torrentState{},
			sample: map[string]*rateMonitor{},
			paths:  newContentPaths(),
		}
		hash := "aabbccddeeff00112233445566778899aabbccdd"
		e.peerRates(hash, start, map[*antorrent.PeerConn]peerCounts{fast: {}})
		e.forget(hash)
		Expect(e.peerReads).NotTo(HaveKey(hash))
	})
})
