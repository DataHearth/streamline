package bittorrent

import (
	"math"
	"time"

	antorrent "github.com/anacrolix/torrent"

	"github.com/datahearth/streamline/internal/download"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("rateMonitor", Label("unit", "bittorrent"), func() {
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	// feed observes one tick per rate (bytes/sec) after a baseline tick at
	// start, and returns the time of the last tick.
	feed := func(m *rateMonitor, rates ...int64) time.Time {
		var down int64
		at := start
		m.observe(at, down, 0)
		for _, r := range rates {
			at = at.Add(rateTickInterval)
			down += r
			m.observe(at, down, 0)
		}
		return at
	}

	It("reports nothing before a second tick", func() {
		m := &rateMonitor{}
		m.observe(start, 1<<30, 1<<30)
		down, up := m.speeds()
		Expect(down).To(BeZero())
		Expect(up).To(BeZero())
		Expect(m.eta(1000)).To(BeZero())
	})

	It("folds each second into a five-second exponential average", func() {
		m := &rateMonitor{}
		feed(m, 1000)
		down, _ := m.speeds()
		Expect(down).To(Equal(int64(200)))
		feed(m, 1000, 1000)
		// Restarted at start: the baseline tick keeps the average, so this
		// is two more seconds on top of the first: 200 → 360 → 488.
		down, _ = m.speeds()
		Expect(down).To(Equal(int64(488)))
	})

	It("converges on a steady rate and keeps the ETA on it", func() {
		m := &rateMonitor{}
		rates := make([]int64, 200)
		for i := range rates {
			rates[i] = 1000
		}
		feed(m, rates...)
		down, _ := m.speeds()
		Expect(down).To(BeNumerically("~", 1000, 1))
		Expect(m.eta(60_000)).To(BeNumerically("~", 60, 1))
	})

	It("keeps the ETA steady through a short burst and a short stall", func() {
		m := &rateMonitor{}
		steady := make([]int64, etaWindow*2)
		for i := range steady {
			steady[i] = 1000
		}
		at := feed(m, steady...)
		before := m.eta(600_000)

		down := m.down
		step := func(r int64) {
			at = at.Add(rateTickInterval)
			down += r
			m.observe(at, down, 0)
		}

		// A ten-fold burst: the shown speed jumps within the second...
		step(10_000)
		speed, _ := m.speeds()
		Expect(speed).To(BeNumerically(">", 2500))
		// ...while the estimate, read over the window, barely moves.
		Expect(m.eta(600_000)).To(BeNumerically("~", before, before/20))

		// Then five seconds with nothing: the speed falls under the steady
		// rate, the estimate stays within a tenth of where it was.
		for range 5 {
			step(0)
		}
		speed, _ = m.speeds()
		Expect(speed).To(BeNumerically("<", 1000))
		Expect(m.eta(600_000)).To(BeNumerically("~", before, before/10))
	})

	It("averages the ETA over the window only", func() {
		m := &rateMonitor{}
		rates := make([]int64, 0, etaWindow*5)
		for range etaWindow * 2 {
			rates = append(rates, 100)
		}
		for range etaWindow * 3 {
			rates = append(rates, 1000)
		}
		feed(m, rates...)
		Expect(m.n).To(Equal(etaWindow))
		Expect(m.eta(100_000)).To(BeNumerically("~", 100, 1))
	})

	It("averages the smoothed speed, not the raw samples", func() {
		m := &rateMonitor{}
		feed(m, 1000, 0, 0, 0)
		// Smoothed: 200, 160, 128, 102.4, a mean of 147.6 B/s; the raw
		// samples would average 250 B/s and read 1476 bytes as 5 s.
		Expect(m.eta(1476)).To(Equal(int64(10)))
	})

	It("starts over from a fresh baseline after a stop", func() {
		m := &rateMonitor{}
		at := feed(m, 1000, 1000, 1000)
		m.stop()
		down, up := m.speeds()
		Expect(down).To(BeZero())
		Expect(up).To(BeZero())
		Expect(m.n).To(BeZero())
		Expect(m.eta(1000)).To(BeZero())

		// Whatever the counters did while stopped is no sample: the first
		// tick back only sets the baseline.
		at = at.Add(time.Minute)
		m.observe(at, m.down+1<<30, m.up+1<<30)
		down, up = m.speeds()
		Expect(down).To(BeZero())
		Expect(up).To(BeZero())
		m.observe(at.Add(rateTickInterval), m.down+5000, m.up)
		down, _ = m.speeds()
		Expect(down).To(Equal(int64(1000)))
	})

	It("reads a counter that went backwards as nothing received", func() {
		m := &rateMonitor{}
		m.observe(start, 5000, 5000)
		m.observe(start.Add(rateTickInterval), 0, 0)
		down, up := m.speeds()
		Expect(down).To(BeZero())
		Expect(up).To(BeZero())
	})

	It("tracks upload alongside download", func() {
		m := &rateMonitor{}
		m.observe(start, 0, 0)
		m.observe(start.Add(rateTickInterval), 0, 5000)
		_, up := m.speeds()
		Expect(up).To(Equal(int64(1000)))
	})

	It("leaves a reading too soon after the last to the next sample", func() {
		m := &rateMonitor{}
		m.observe(start, 0, 0)
		m.observe(start.Add(rateTickInterval), 1000, 0)
		// A catch-up pass rereads the torrent a millisecond later: one
		// 16 KiB chunk over that gap would read as 16 MB/s.
		m.observe(start.Add(rateTickInterval+time.Millisecond), 1000+16<<10, 0)
		down, _ := m.speeds()
		Expect(down).To(Equal(int64(200)))
		Expect(m.n).To(Equal(1))
		// The chunk lands in the next full sample.
		m.observe(start.Add(2*rateTickInterval), 2000+16<<10, 0)
		Expect(m.n).To(Equal(2))
		down, _ = m.speeds()
		Expect(down).To(Equal(int64(math.Round(200*4/5 + float64(1000+16<<10)/5))))
	})

	It("reads an estimate past 100 days as unknown", func() {
		m := &rateMonitor{downRate: 1, n: 1, sum: 1}
		Expect(m.eta(maxETA)).To(Equal(int64(maxETA)))
		Expect(m.eta(maxETA + 1)).To(BeZero())
	})

	It("scales a late tick by the time it covers", func() {
		m := &rateMonitor{}
		m.observe(start, 0, 0)
		m.observe(start.Add(2*rateTickInterval), 2000, 0)
		down, _ := m.speeds()
		Expect(down).To(Equal(int64(200)))
	})
})

var _ = Describe("Engine rate sampling", Label("unit", "bittorrent"), func() {
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	newEngine := func() *Engine {
		return &Engine{
			state:  map[string]*torrentState{},
			sample: map[string]*rateMonitor{},
			paths:  newContentPaths(),
		}
	}
	clock := func(at time.Time) func() time.Time {
		return func() time.Time { return at }
	}

	It("samples a listed torrent and drops one the list no longer holds", func() {
		e := newEngine()
		t := newTestTorrent()
		e.prioritize(t, "all", nil)
		hash := t.InfoHash().HexString()

		e.sampleTorrents([]*antorrent.Torrent{t}, 1, clock(start))
		Expect(e.sample).To(HaveKey(hash))
		Expect(e.sample[hash].at).To(Equal(start))

		e.sampleTorrents([]*antorrent.Torrent{t}, 2, clock(start.Add(time.Second)))
		Expect(e.sample).To(HaveKey(hash))
		Expect(e.sample[hash].n).To(Equal(1))

		e.sampleTorrents(nil, 3, clock(start.Add(2*time.Second)))
		Expect(e.sample).NotTo(HaveKey(hash))
	})

	It("does not read a paused torrent and forgets its baseline", func() {
		e := newEngine()
		t := newTestTorrent()
		e.prioritize(t, "all", nil)
		hash := t.InfoHash().HexString()
		e.sampleTorrents([]*antorrent.Torrent{t}, 1, clock(start))

		e.setState(hash, func(s *torrentState) { s.paused = true })
		e.sampleTorrents([]*antorrent.Torrent{t}, 2, clock(start.Add(time.Second)))
		Expect(e.sample).To(HaveKey(hash))
		Expect(e.sample[hash].at.IsZero()).To(BeTrue())
	})

	It(
		"reads a seed-stopped torrent, which a widened selection can set downloading",
		func() {
			e := newEngine()
			t := newPartialTorrent()
			e.prioritize(t, "all", nil)
			hash := t.InfoHash().HexString()
			e.setState(hash, func(s *torrentState) { s.seedStopped = true })

			e.sampleTorrents([]*antorrent.Torrent{t}, 1, clock(start))
			Expect(e.sample[hash].at).To(Equal(start))
		},
	)

	It("gives live() the ETA for the wanted bytes still missing", func() {
		e := newEngine()
		t := newPartialTorrent()
		e.prioritize(t, "all", nil)
		hash := t.InfoHash().HexString()
		remaining := wantedMissing(t)
		Expect(remaining).To(BeNumerically(">", 0))

		// A window holding a steady 1 B/s.
		m := &rateMonitor{downRate: 1, n: 1, sum: 1}
		e.sample[hash] = m

		l := e.live(t)
		Expect(l.downloadSpeed).To(Equal(int64(1)))
		Expect(l.eta).To(Equal(remaining))
	})

	It("shows nothing for a paused torrent before the next tick", func() {
		e := newEngine()
		t := newPartialTorrent()
		e.prioritize(t, "all", nil)
		hash := t.InfoHash().HexString()
		e.sample[hash] = &rateMonitor{downRate: 1000, upRate: 50, n: 1, sum: 1000}
		e.setState(hash, func(s *torrentState) { s.paused = true })

		l := e.live(t)
		Expect(l.status).To(Equal(download.StatusPaused))
		Expect([]int64{l.downloadSpeed, l.uploadSpeed, l.eta}).
			To(Equal([]int64{0, 0, 0}))
	})
})

var _ = Describe("payloadCounters", Label("unit", "bittorrent"), func() {
	It("measures download on useful payload and upload on payload sent", func() {
		var stats antorrent.TorrentStats
		stats.BytesRead.Add(9000)
		stats.BytesReadData.Add(7000)
		stats.BytesReadUsefulData.Add(5000)
		stats.BytesWritten.Add(800)
		stats.BytesWrittenData.Add(600)

		down, up := payloadCounters(stats)
		Expect(down).To(Equal(int64(5000)))
		Expect(up).To(Equal(int64(600)))
	})
})

var _ = Describe("shownRates", Label("unit", "bittorrent"), func() {
	It("shows nothing for a paused torrent", func() {
		down, up, eta := shownRates(download.StatusPaused, 500, 70, 60)
		Expect([]int64{down, up, eta}).To(Equal([]int64{0, 0, 0}))
	})

	It(
		"shows no download speed or ETA while checking, but keeps the upload",
		func() {
			down, up, eta := shownRates(download.StatusChecking, 500, 70, 60)
			Expect([]int64{down, up, eta}).To(Equal([]int64{0, 70, 0}))
		},
	)

	It("passes a downloading torrent's rates through", func() {
		down, up, eta := shownRates(download.StatusDownloading, 500, 70, 60)
		Expect([]int64{down, up, eta}).To(Equal([]int64{500, 70, 60}))
	})
})
