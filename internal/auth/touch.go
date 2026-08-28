package auth

import (
	"maps"
	"sync"
	"time"
)

const (
	// touchInterval is the resolution last_seen_at is kept to. It feeds a
	// "when was this last used" column in the sessions and API-keys lists and
	// nothing else — nothing branches on it — so minutes are as much as it
	// has ever needed. Per-request accuracy meant one full row rewrite per
	// authenticated request: a single open activity tab polling every two
	// seconds is ~1,800 UPDATEs an hour, all serialized through the one
	// SQLite connection every other request queues behind.
	touchInterval = 5 * time.Minute

	// touchSweepAt is when a write sweeps expired entries. The map is bounded
	// by the number of distinct credentials used within touchInterval, which
	// is small, but jtis are per-login and would otherwise accumulate for the
	// lifetime of the process.
	touchSweepAt = 1024
)

// touchDebounce answers whether a credential's last_seen_at is stale enough to
// be worth writing again. Safe for concurrent use.
type touchDebounce struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newTouchDebounce() *touchDebounce {
	return &touchDebounce{last: make(map[string]time.Time)}
}

// allow reports whether the caller should write, recording the write when it
// says yes. An unknown key always writes, so a credential's first use after a
// restart is stamped immediately.
func (d *touchDebounce) allow(key string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.last[key]; ok && now.Sub(prev) < touchInterval {
		return false
	}
	if len(d.last) >= touchSweepAt {
		// An entry older than the interval can only ever answer "allow", so
		// dropping it changes nothing but the map's size.
		maps.DeleteFunc(d.last, func(_ string, t time.Time) bool {
			return now.Sub(t) >= touchInterval
		})
	}
	d.last[key] = now
	return true
}
