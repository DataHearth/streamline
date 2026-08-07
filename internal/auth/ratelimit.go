package auth

import (
	"maps"
	"slices"
	"sync"
	"time"
)

// Limiter is the consumer-facing surface used by login and OIDC handlers
// to throttle credential attempts per IP.
type Limiter interface {
	Allow(key string) bool
	RetryAfter(key string) time.Duration
}

// maxKeys caps how many distinct keys the limiter tracks at once. Login is
// pre-auth, so the key space is whatever an unauthenticated caller can reach:
// without a ceiling, a stream of one-shot attempts from unique addresses grows
// the map until the process runs out of memory. Sized well above the number of
// distinct clients any real deployment sees inside one window.
const maxKeys = 10_000

// limiter is a per-key sliding-window rate limiter intended for login and
// OIDC callback protection. Not intended for high-throughput endpoints.
type limiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
	limit   uint8
	window  time.Duration
}

// NewLimiter returns a Limiter allowing `limit` hits per key within `window`.
func NewLimiter(limit uint8, window time.Duration) Limiter {
	return &limiter{
		windows: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
	}
}

// pruneLocked drops timestamps older than the window, deleting the key
// outright once nothing remains — otherwise every client IP ever seen would
// leave a permanent map entry. Caller must hold mu.
func (l *limiter) pruneLocked(key string) []time.Time {
	cutoff := time.Now().Add(-l.window)
	w := l.windows[key]
	kept := make([]time.Time, 0, len(w))
	for _, t := range w {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.windows, key)
		return nil
	}
	l.windows[key] = kept
	return kept
}

// makeRoomLocked keeps the map at or below maxKeys before a new key is added.
// It first sweeps every key whose window has fully aged out, then — only if
// that many keys really are live at once — drops the least recently hit ones.
// Evicting the coldest keys forgives attempts that were about to age out
// anyway, so an attacker cannot use the overflow to reset a key that is
// actively being throttled. Caller must hold mu.
func (l *limiter) makeRoomLocked(key string) {
	if _, ok := l.windows[key]; ok || len(l.windows) < maxKeys {
		return
	}

	cutoff := time.Now().Add(-l.window)
	for k, w := range l.windows {
		if len(w) == 0 || !w[len(w)-1].After(cutoff) {
			delete(l.windows, k)
		}
	}
	if len(l.windows) < maxKeys {
		return
	}

	coldest := slices.SortedFunc(
		maps.Keys(l.windows),
		func(a, b string) int { return l.lastHitLocked(a).Compare(l.lastHitLocked(b)) },
	)
	for _, k := range coldest[:len(l.windows)-maxKeys+1] {
		delete(l.windows, k)
	}
}

// lastHitLocked returns the most recent hit recorded for key; timestamps are
// appended in order, so the newest is last. Caller must hold mu.
func (l *limiter) lastHitLocked(key string) time.Time {
	w := l.windows[key]
	if len(w) == 0 {
		return time.Time{}
	}
	return w[len(w)-1]
}

// Allow records a hit for key and reports whether it is within the limit.
func (l *limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	w := l.pruneLocked(key)
	if len(w) >= int(l.limit) {
		return false
	}
	l.makeRoomLocked(key)
	l.windows[key] = append(w, time.Now())
	return true
}

// RetryAfter returns how long until the oldest recorded hit for key ages out.
// Zero if key is currently under limit.
func (l *limiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	w := l.pruneLocked(key)
	if len(w) < int(l.limit) {
		return 0
	}
	oldest := w[0]
	return l.window - time.Since(oldest)
}
