package metadata

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// backgroundGapFactor sets the least time between two background
	// MusicBrainz calls to that many intervals, so bulk hydration spends at
	// most half the budget even when nobody is interacting.
	backgroundGapFactor = 2

	backgroundPoll = 100 * time.Millisecond
)

type (
	backgroundKey struct{}
	prefetchKey   struct{}
)

// Background marks ctx as bulk work: the hydration worker, the credits sweep
// and the trackless re-hydration. MusicBrainz calls made under it wait behind
// any interactive request and keep backgroundGapFactor intervals between
// themselves.
func Background(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

// Prefetch marks ctx as work for something on screen that nobody is blocked
// on: the lookup art proxy settling a search hit's photo. MusicBrainz calls
// made under it wait behind any interactive request, go before background
// ones and keep no gap, so a page of tiles fills at the full rate while a
// person's own lookup never queues behind it.
func Prefetch(ctx context.Context) context.Context {
	return context.WithValue(ctx, prefetchKey{}, true)
}

func isBackground(ctx context.Context) bool {
	v, _ := ctx.Value(backgroundKey{}).(bool)
	return v
}

func isPrefetch(ctx context.Context) bool {
	v, _ := ctx.Value(prefetchKey{}).(bool)
	return v
}

// mbLimiter is the one limiter every MusicBrainz call shares, with
// three classes in front of it: an interactive caller takes the next token, a
// prefetch caller takes one only while no interactive caller is waiting, and a
// background caller only while neither is. A person on a screen therefore
// waits at most for the token already in flight.
type mbLimiter struct {
	lim *rate.Limiter
	gap time.Duration

	mu          sync.Mutex
	interactive int
	prefetch    int
	lastBG      time.Time
	heldUntil   time.Time
}

// hold refuses prefetch and background callers for d, after MusicBrainz
// refused a call. Calls sent into a refusal are refused too and keep the
// budget drained, so a page of art tiles turned one 503 into a run of them
// that a person's own lookup kept landing on. Interactive callers are not
// held: freeing the budget for them is the point.
func (l *mbLimiter) hold(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until := time.Now().Add(d); until.After(l.heldUntil) {
		l.heldUntil = until
	}
}

func newMBLimiter(interval, gap time.Duration) *mbLimiter {
	limit := rate.Inf
	if interval > 0 {
		limit = rate.Every(interval)
	}
	return &mbLimiter{lim: rate.NewLimiter(limit, 1), gap: gap}
}

func (l *mbLimiter) Wait(ctx context.Context) error {
	background := isBackground(ctx)
	if isPrefetch(ctx) && !background {
		l.mu.Lock()
		l.prefetch++
		l.mu.Unlock()
		defer func() {
			l.mu.Lock()
			l.prefetch--
			l.mu.Unlock()
		}()
		return l.poll(ctx, false)
	}
	if !background {
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
	return l.poll(ctx, true)
}

func (l *mbLimiter) poll(ctx context.Context, background bool) error {
	for {
		if held := l.heldFor(); held > 0 {
			return &RateLimitedError{RetryAfter: held}
		}
		if wait, ok := l.tryLow(background); ok {
			return nil
		} else if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

func (l *mbLimiter) heldFor() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Until(l.heldUntil)
}

// tryLow takes a token for a prefetch or background caller when no caller of
// a higher class waits and, for a background one, the gap since the last
// background call has passed; otherwise it says how long to sleep before
// asking again. Allow never queues a reservation, so a low caller cannot sit
// in front of a higher one that arrives after it.
func (l *mbLimiter) tryLow(background bool) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.interactive > 0 || background && l.prefetch > 0 {
		return backgroundPoll, false
	}
	if background {
		if since := time.Since(l.lastBG); since < l.gap {
			return min(l.gap-since, backgroundPoll), false
		}
	}
	if !l.lim.Allow() {
		return backgroundPoll, false
	}
	if background {
		l.lastBG = time.Now()
	}
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
