package bittorrent

import (
	"context"
	"math"
	"time"

	antorrent "github.com/anacrolix/torrent"

	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/observability"
)

// The engine measures transfer rates the way qBittorrent does on top of
// libtorrent, in two layers, because one reading answers two questions badly.
//
//   - The speed shown is libtorrent's payload rate: one sample a second, folded
//     into an exponential average that weighs the newest second a fifth
//     (stat_channel::second_tick, `avg = avg*4/5 + sample/5`), about the last
//     five seconds.
//   - The ETA divides what is left by the mean of the last etaWindow readings of
//     that speed (qBittorrent's SpeedMonitor: 30 samples, one per 1.5 s session
//     refresh, so about 45 s). A swarm's rate swings from second to second; an
//     estimate of minutes or hours read off the last five seconds swings with it.
//
// Sampling runs on its own ticker so every sample covers the same interval,
// and reads never take one: the views, the download monitor and the metrics
// callback all read live() at their own pace, and a reading that moved the
// baseline would leave the next one a delta over whatever gap separated them.
const (
	rateTickInterval = time.Second
	// rateSmoothing is libtorrent's divisor: the newest second's share.
	rateSmoothing = 5
	// etaWindow is qBittorrent's 45 s window, in rateTickInterval samples.
	etaWindow = 45
	// minSampleGap is the shortest interval a sample may cover. The ticker
	// keeps one tick while a pass is held up, so the next pass can reread a
	// torrent milliseconds after the last one, and one chunk over that gap
	// would read as a burst; its bytes go to the next sample instead.
	minSampleGap = rateTickInterval / 2
	// maxETA is qBittorrent's ceiling, 100 days; an estimate past it reads as
	// unknown.
	maxETA = 100 * 24 * 60 * 60
)

// rateMonitor carries one torrent's rate history between ticks.
type rateMonitor struct {
	// The counters at the previous tick, and when it ran.
	down, up int64
	at       time.Time
	// The smoothed speeds, bytes/sec.
	downRate, upRate float64
	// The last etaWindow download speeds, a ring.
	window [etaWindow]float64
	n      int
	next   int
	sum    float64
	// gen is the sampler tick that last saw the torrent.
	gen uint64
}

// observe takes one tick's counters. The first reading, the first after a
// stop and one stamped before the baseline only set the baseline; a reading
// less than minSampleGap after it leaves the baseline in place; a counter
// that went backwards counts as nothing received.
func (m *rateMonitor) observe(now time.Time, down, up int64) {
	elapsed := now.Sub(m.at)
	if m.at.IsZero() || elapsed < 0 {
		m.down, m.up, m.at = down, up, now
		return
	}
	if elapsed < minSampleGap {
		return
	}
	secs := elapsed.Seconds()
	m.downRate = smooth(m.downRate, delta(m.down, down), secs)
	m.upRate = smooth(m.upRate, delta(m.up, up), secs)
	m.down, m.up, m.at = down, up, now

	if m.n == etaWindow {
		m.sum -= m.window[m.next]
	} else {
		m.n++
	}
	m.window[m.next] = m.downRate
	m.sum += m.downRate
	m.next = (m.next + 1) % etaWindow
}

// stop starts a paused torrent's monitor over: it reads zero, as
// qBittorrent's does on stop, and loses its baseline too, so whatever the
// counters did meanwhile is never a sample.
func (m *rateMonitor) stop() {
	*m = rateMonitor{gen: m.gen}
}

// speeds is the smoothed download and upload rate, bytes/sec.
func (m *rateMonitor) speeds() (int64, int64) {
	return int64(math.Round(m.downRate)), int64(math.Round(m.upRate))
}

// eta is the seconds left to fetch remaining bytes at the window's mean
// download speed, 0 (unknown) when nothing is moving or the answer is past
// maxETA.
func (m *rateMonitor) eta(remaining int64) int64 {
	if m.n == 0 || remaining <= 0 {
		return 0
	}
	avg := m.sum / float64(m.n)
	if avg < 1 {
		return 0
	}
	secs := float64(remaining) / avg
	if secs > maxETA {
		return 0
	}
	return int64(secs)
}

func delta(prev, cur int64) int64 {
	return max(cur-prev, 0)
}

func smooth(avg float64, bytes int64, elapsed float64) float64 {
	sample := float64(bytes) / elapsed
	return avg*(rateSmoothing-1)/rateSmoothing + sample/rateSmoothing
}

// sampleRates ticks every rateTickInterval and records each torrent's
// counters. A panic costs one tick, not the sampler.
func (e *Engine) sampleRates() {
	tick := time.NewTicker(rateTickInterval)
	defer tick.Stop()
	var gen uint64
	for {
		select {
		case <-e.stop:
			return
		case <-tick.C:
			gen++
			e.sampleOnce(gen)
		}
	}
}

func (e *Engine) sampleOnce(gen uint64) {
	ctx := context.Background()
	defer observability.RecoverPanic(ctx, "bittorrent rate sampler", nil)
	e.sampleTorrents(e.client.Torrents(), gen, time.Now)
}

// sampleTorrents records one tick over the listed torrents and drops the
// monitor of every torrent the list no longer holds. A paused torrent moves
// no data and is not read; any other is, through t.Stats(), which walks the
// torrent's peer list under the client lock (up to about 18 µs and 300
// allocations at the known-peer cap). Each sample is stamped when its
// counters are read, since the client lock can hold a read back for longer
// than a tick.
func (e *Engine) sampleTorrents(
	torrents []*antorrent.Torrent,
	gen uint64,
	now func() time.Time,
) {
	for _, t := range torrents {
		hash := t.InfoHash().HexString()
		idle := e.getState(hash).paused
		var down, up int64
		var at time.Time
		if !idle {
			stats := t.Stats()
			down, up = payloadCounters(&stats.ConnStats)
			at = now()
		}
		e.record(hash, gen, idle, at, down, up)
	}
	// A torrent forgotten between the listing and its sample would otherwise
	// leave its monitor behind, and a detail nobody has read for
	// peerReadMaxAge its connections' baseline.
	at := now()
	e.mu.Lock()
	defer e.mu.Unlock()
	for hash, m := range e.sample {
		if m.gen != gen {
			delete(e.sample, hash)
		}
	}
	for hash, snap := range e.peerReads {
		if at.Sub(snap.at) > peerReadMaxAge {
			delete(e.peerReads, hash)
		}
	}
}

// payloadCounters is what every rate is measured on, a torrent's (its
// aggregate over all connections) and a connection's alike. Download is
// payload received that a piece needed, as it arrives: a verified-bytes
// counter grows a whole piece at a time, and a hash check, which reads from
// disk, would count as bytes received. Upload is payload sent.
func payloadCounters(stats *antorrent.ConnStats) (down, up int64) {
	return stats.BytesReadUsefulData.Int64(), stats.BytesWrittenData.Int64()
}

func (e *Engine) record(
	hash string,
	gen uint64,
	idle bool,
	at time.Time,
	down, up int64,
) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.sample[hash]
	if m == nil {
		m = &rateMonitor{}
		e.sample[hash] = m
	}
	m.gen = gen
	if idle {
		m.stop()
		return
	}
	m.observe(at, down, up)
}

// shownRates is what a torrent in this status shows of its rates. A paused
// one shows nothing, before the next tick has reset its monitor. One
// re-hashing what it found on disk shows no download speed and no ETA, as
// the queue shows it at 0 B/s: the check holds back every piece it covers,
// so what a peer sends meanwhile says nothing about when it will be done.
func shownRates(
	status download.TorrentStatus,
	down, up, eta int64,
) (int64, int64, int64) {
	switch status {
	case download.StatusPaused:
		return 0, 0, 0
	case download.StatusChecking:
		return 0, up, 0
	default:
		return down, up, eta
	}
}

// rates reads a torrent's smoothed speeds and its ETA for remaining bytes.
func (e *Engine) rates(hash string, remaining int64) (down, up, eta int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.sample[hash]
	if m == nil {
		return 0, 0, 0
	}
	down, up = m.speeds()
	return down, up, m.eta(remaining)
}
