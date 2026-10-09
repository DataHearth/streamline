package metadata

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	mbInterval = time.Second

	// musicBackgroundGap is the least time between two background MusicBrainz
	// calls, so bulk hydration spends at most half the budget even when nobody
	// is interacting.
	musicBackgroundGap = 2 * time.Second

	backgroundPoll = 100 * time.Millisecond
)

type backgroundKey struct{}

// Background marks ctx as bulk work: the hydration worker, the credits sweep
// and the trackless re-hydration. MusicBrainz calls made under it wait behind
// any interactive request and keep musicBackgroundGap between themselves.
func Background(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

func isBackground(ctx context.Context) bool {
	v, _ := ctx.Value(backgroundKey{}).(bool)
	return v
}

// mbLimiter is the one 1 req/s limiter every MusicBrainz call shares, with two
// classes in front of it: an interactive caller takes the next token, a
// background caller takes one only while no interactive caller is waiting. A
// person on a screen therefore waits at most for the token already in flight.
type mbLimiter struct {
	lim *rate.Limiter
	gap time.Duration

	mu          sync.Mutex
	interactive int
	lastBG      time.Time
}

func newMBLimiter(interval, gap time.Duration) *mbLimiter {
	limit := rate.Inf
	if interval > 0 {
		limit = rate.Every(interval)
	}
	return &mbLimiter{lim: rate.NewLimiter(limit, 1), gap: gap}
}

func (l *mbLimiter) Wait(ctx context.Context) error {
	if !isBackground(ctx) {
		l.mu.Lock()
		l.interactive++
		l.mu.Unlock()
		defer func() {
			l.mu.Lock()
			l.interactive--
			l.mu.Unlock()
		}()
		return l.lim.Wait(ctx)
	}
	for {
		if wait, ok := l.tryBackground(); ok {
			return nil
		} else if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

// tryBackground takes a token when no interactive caller waits and the gap
// since the last background call has passed; otherwise it says how long to
// sleep before asking again. Allow never queues a reservation, so a
// background caller cannot sit in front of one that arrives after it.
func (l *mbLimiter) tryBackground() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.interactive > 0 {
		return backgroundPoll, false
	}
	if since := time.Since(l.lastBG); since < l.gap {
		return min(l.gap-since, backgroundPoll), false
	}
	if !l.lim.Allow() {
		return backgroundPoll, false
	}
	l.lastBG = time.Now()
	return 0, true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
