package auth

import (
	"net/netip"
	"sync"
	"time"
)

// Limiter is the consumer-facing surface used by login and OIDC handlers
// to throttle credential attempts per client address.
type Limiter interface {
	// Allow records an attempt for key and reports whether it is within the
	// limit. The duration is how long the caller has to wait before the key
	// frees up: strictly positive whenever the attempt is refused, zero
	// otherwise. It is returned from the same locked decision so a caller
	// cannot observe a refusal alongside a stale "retry now".
	Allow(key string) (bool, time.Duration)

	// Refund returns the most recent attempt recorded under key to the budget.
	//
	// Credential endpoints have to charge before they know whether the
	// credentials were good — checking first would let an unauthenticated caller
	// spend the server's bcrypt time without limit — so a caller that turns out
	// to have been legitimate hands its attempt back here. Without it the
	// ceiling meters use rather than guessing: five successful logins from one
	// address lock out the sixth, and behind a reverse proxy whose address is
	// not in server.trusted_proxies every user shares that one budget.
	//
	// A no-op when the key holds nothing, so an unmatched call cannot lend
	// anyone an attempt they never spent.
	Refund(key string)
}

const (
	// maxKeys bounds how many keys are tracked at once. Login is pre-auth, so
	// the key space is whatever an unauthenticated caller can reach: without a
	// ceiling, a stream of attempts from unique addresses grows the table until
	// the process is killed.
	//
	// Past the cap a new key is allowed and not recorded. That is the only
	// answer to key-space pressure that keeps both halves of the contract:
	// nothing already tracked is dropped to make room, so no traffic can hand a
	// throttled key its attempts back early, and nobody is refused for want of
	// a slot, so a caller that has made no attempts is never turned away. The
	// alternatives each give up one of those. Refusing the new key is an outage
	// for precisely the callers with a clean record. Evicting a victim is a
	// lever, and not a theoretical one: a 10,000-key table that evicts the entry
	// nearest to draining grants all but 5 of 1,310,720 attempts made from
	// 65,536 addresses cycled round-robin, because every pass finds its own
	// entry already gone and starts fresh.
	//
	// What the cap costs is enforcement over keys first seen while the table is
	// already full, so its size is the question of how many distinct addresses
	// an attacker must command before that begins. Keys are addresses or IPv6
	// /64s (see ipv6Bucket): a routed /48, the cheapest block to rent, is 65,536
	// of them, so this sits four times above it and measures 41 MiB when full —
	// a ceiling only a flood approaches, since a real deployment tracks its
	// handful of clients and nothing else. The exact break-even is a /46
	// (2^(64-46) = 262,144); a /44 is four times more than needed.
	//
	// Past the cap the lapse is UNBOUNDED for any single address first seen
	// while the table is full: it is never metered at all. The often-quoted
	// 16.07-granted-per-address is an average over 1,000,000 addresses that
	// mixes the 262,144 metered ones with the unmetered remainder — it is not a
	// per-address bound. (5.00 at 262,144 addresses; 20.00 from the evicting map
	// this replaced, which was already a total bypass.) Filling it means holding 262,144
	// addresses that were each cut off at 5 first, and every account behind them
	// still locks after auth.lockout.threshold failures.
	maxKeys = 1 << 18

	// ipv6Bucket is the prefix length IPv6 keys are aggregated to. The smallest
	// block an ISP routes to one subscriber is a /64, so a client keyed by its
	// full address holds 2^64 separate budgets: it walks away from its own
	// throttle by picking the next address, and fills maxKeys on its own out of
	// a single allocation. Aggregating at /64 shares a budget only between
	// addresses that already shared a subscriber. IPv4 keys are left whole.
	ipv6Bucket = 64
)

// limiter meters login and OIDC-callback attempts per client address over a
// sliding window. A key holds the elapsed times of its attempts that are still
// younger than window and is refused once it holds limit of them, so no window
// of that length anywhere on the timeline ever contains more than limit grants.
// An attempt returns to the budget exactly window after it was made.
//
// Three properties hold, in decreasing order of how much they cost to keep:
//
//  1. No traffic from any number of addresses gives a throttled key its
//     attempts back early. Attempts are only ever dropped by ageing out, never
//     to reclaim space, so there is nothing to aim at: the timestamps under a
//     key move only with the clock.
//  2. A caller that has made no attempts is never refused. Nothing is shared
//     between keys — the /64 aggregation aside, which folds addresses that were
//     already one subscriber into one key — so no other caller's traffic can be
//     charged to a key, and pressure on the table is absorbed by not tracking
//     rather than by refusing.
//  3. Memory is bounded. maxKeys entries of limit timestamps, with expiry
//     costing nothing to run.
//
// Expiry is what usually forces per-key state to be scanned or evicted. Here
// entries live in two generations: attempts land in cur, cur becomes prev once
// window has passed, and the generation that was prev is dropped whole. A key
// touched while in prev moves back into cur, so an entry is only ever dropped
// after a full window with no attempt recorded under it — by which point every
// timestamp it holds has aged out anyway. Dropping a generation therefore
// discards nothing that could still throttle anyone, and costs one map
// assignment per window rather than a scan.
//
// The price of not scanning is that prev keeps holding entries that have gone
// quiet until its generation is dropped, and those entries count against
// maxKeys. A flood that fills the table therefore keeps it full for up to one
// window after it stops, which extends the interval over which new keys go
// untracked but takes nothing away from anyone.
type limiter struct {
	mu sync.Mutex
	// cur and prev hold, per key, the elapsed times since start of the attempts
	// still inside the window, oldest first. A key appears in at most one of
	// them. prev is nil until the first rotation.
	cur, prev map[string][]time.Duration
	// rotateAt is the elapsed time at which cur becomes prev.
	rotateAt time.Duration

	// start anchors every recorded time, so the window is measured on the
	// monotonic clock and a wall-clock jump cannot widen or collapse it.
	start time.Time

	limit  uint8
	window time.Duration
}

// NewLimiter returns a Limiter allowing `limit` attempts per key within any
// `window`-long span. A zero limit refuses every attempt.
func NewLimiter(limit uint8, window time.Duration) Limiter {
	return &limiter{
		cur:      make(map[string][]time.Duration),
		rotateAt: window,
		start:    time.Now(),
		limit:    limit,
		window:   window,
	}
}

// bucketKey maps a client address onto the key its attempts are counted under,
// folding every address in one IPv6 /64 together and every spelling of one
// address — v4-mapped notation, a zone suffix — onto a single form, so no
// notation buys a second budget. Keys that are not addresses are used as they
// arrive.
func bucketKey(key string) string {
	ip, err := netip.ParseAddr(key)
	if err != nil {
		return key
	}
	ip = ip.Unmap().WithZone("")
	if !ip.Is6() {
		return ip.String()
	}
	prefix, err := ip.Prefix(ipv6Bucket)
	if err != nil {
		return key
	}
	return prefix.String()
}

// insideWindow drops the attempts at or before cutoff, which are the oldest, in
// place: the entry keeps the one backing array it was allocated with for as
// long as it lives, so a key that attempts forever still allocates once.
func insideWindow(w []time.Duration, cutoff time.Duration) []time.Duration {
	aged := 0
	for aged < len(w) && w[aged] <= cutoff {
		aged++
	}
	if aged == 0 {
		return w
	}
	return w[:copy(w, w[aged:])]
}

// rotateLocked retires cur into prev once a window has passed, dropping what
// prev held. Caller must hold mu.
func (l *limiter) rotateLocked(now time.Duration) {
	if now < l.rotateAt {
		return
	}
	// Two windows without a call: cur is as stale as prev was, so both go.
	if now >= l.rotateAt+l.window {
		l.prev = nil
	} else {
		l.prev = l.cur
	}
	l.cur = make(map[string][]time.Duration)
	l.rotateAt = now + l.window
}

func (l *limiter) Allow(key string) (bool, time.Duration) {
	if l.limit == 0 {
		return false, l.window
	}
	key = bucketKey(key)
	now := time.Since(l.start)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.rotateLocked(now)

	w, tracked := l.cur[key]
	if !tracked {
		if w, tracked = l.prev[key]; tracked {
			delete(l.prev, key)
		}
	}
	w = insideWindow(w, now-l.window)

	if len(w) >= int(l.limit) {
		l.cur[key] = w
		// Every kept attempt is newer than now-window, so this is positive.
		return false, l.window - (now - w[0])
	}
	if !tracked && len(l.cur)+len(l.prev) >= maxKeys {
		return true, 0
	}
	if w == nil {
		w = make([]time.Duration, 0, l.limit)
	}
	l.cur[key] = append(w, now)
	return true, 0
}

func (l *limiter) Refund(key string) {
	key = bucketKey(key)

	l.mu.Lock()
	defer l.mu.Unlock()

	// Allow leaves the key in cur, but a rotation landing between the two calls
	// moves it to prev, so both generations are checked. The newest attempt is
	// last: Allow appends, and insideWindow only drops from the front.
	for _, gen := range []map[string][]time.Duration{l.cur, l.prev} {
		if w, ok := gen[key]; ok && len(w) > 0 {
			gen[key] = w[:len(w)-1]
			return
		}
	}
}
