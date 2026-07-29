package auth

import (
	"sync"
	"time"
)

// Limiter is the consumer-facing surface used by login and OIDC handlers
// to throttle credential attempts per IP.
type Limiter interface {
	Allow(key string) bool
	RetryAfter(key string) time.Duration
}

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

// Allow records a hit for key and reports whether it is within the limit.
func (l *limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	w := l.pruneLocked(key)
	if len(w) >= int(l.limit) {
		return false
	}
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
