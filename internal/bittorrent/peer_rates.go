package bittorrent

import (
	"math"
	"time"

	antorrent "github.com/anacrolix/torrent"
)

// A connection's rates are measured on demand, from the detail view's own
// reads: each read takes every connection's payload counters, and a
// connection's rate is what it moved since the previous read, over the time
// between the two. Nothing is read while nobody looks, and the SPA polls an
// open detail every 2 s, so the rate covers the last couple of seconds.
//
// anacrolix's own PeerStats.DownloadRate is no such rate: it divides
// everything a connection received since it opened by the time requests
// were outstanding only, so a peer that stopped sending keeps its best rate.
// A read under minSampleGap after the last one, from a second viewer, gets
// the rates that one measured and leaves the baseline in place: a delta
// over a few milliseconds is a burst or a zero, never a rate.
//
// peerReadMaxAge is how old a baseline may be. Past it the detail was
// closed, and the next read only takes the baseline: a rate over the
// minutes since would be an average, not what the peer does now.
const peerReadMaxAge = 10 * time.Second

// peerCounts is one connection's payload counters, or its rates in bytes/sec.
type peerCounts struct {
	down, up int64
}

// peerSnapshot is the last read of one torrent's connections and the rates
// it measured.
type peerSnapshot struct {
	at     time.Time
	counts map[*antorrent.PeerConn]peerCounts
	rates  map[*antorrent.PeerConn]peerCounts
}

// readPeerCounts lists a torrent's connections and reads each one's payload
// counters, the same two the torrent's rates are measured on.
func readPeerCounts(
	t *antorrent.Torrent,
) ([]*antorrent.PeerConn, map[*antorrent.PeerConn]peerCounts) {
	conns := t.PeerConns()
	counts := make(map[*antorrent.PeerConn]peerCounts, len(conns))
	for _, pc := range conns {
		stats := pc.Stats()
		down, up := payloadCounters(&stats.ConnStats)
		counts[pc] = peerCounts{down: down, up: up}
	}
	return conns, counts
}

// nextPeerSnapshot measures a read against the last one. A read too soon
// after it, or stamped before it (two reads that queued on the client lock
// can reach here out of order), returns it unchanged; a first read, or one
// after a baseline too old, measures nothing. A connection new since the
// last read, or whose counter went backwards, reads zero.
func nextPeerSnapshot(
	prev *peerSnapshot,
	now time.Time,
	counts map[*antorrent.PeerConn]peerCounts,
) *peerSnapshot {
	next := &peerSnapshot{
		at:     now,
		counts: counts,
		rates:  make(map[*antorrent.PeerConn]peerCounts, len(counts)),
	}
	if prev == nil {
		return next
	}
	elapsed := now.Sub(prev.at)
	switch {
	case elapsed > peerReadMaxAge:
		return next
	case elapsed < minSampleGap:
		return prev
	}
	secs := elapsed.Seconds()
	for pc, c := range counts {
		p, ok := prev.counts[pc]
		if !ok {
			continue
		}
		next.rates[pc] = peerCounts{
			down: perSecond(delta(p.down, c.down), secs),
			up:   perSecond(delta(p.up, c.up), secs),
		}
	}
	return next
}

func perSecond(bytes int64, secs float64) int64 {
	return int64(math.Round(float64(bytes) / secs))
}

// peerRates measures a torrent's connections from this read and the last,
// and keeps this read as the next one's baseline. The sampler drops a
// baseline nobody has read for peerReadMaxAge.
func (e *Engine) peerRates(
	hash string,
	now time.Time,
	counts map[*antorrent.PeerConn]peerCounts,
) map[*antorrent.PeerConn]peerCounts {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.peerReads == nil {
		e.peerReads = map[string]*peerSnapshot{}
	}
	snap := nextPeerSnapshot(e.peerReads[hash], now, counts)
	e.peerReads[hash] = snap
	return snap.rates
}
