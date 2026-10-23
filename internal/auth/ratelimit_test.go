package auth

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// allowed drops the wait for the specs that only care about the verdict.
func allowed(l Limiter, key string) bool {
	GinkgoHelper()
	ok, _ := l.Allow(key)
	return ok
}

// rewind moves the limiter's origin back, which is how much time the limiter
// sees as having passed since the calls made before it.
func rewind(l *limiter, d time.Duration) {
	GinkgoHelper()
	l.start = l.start.Add(-d)
}

// tracked returns the attempts the limiter still holds under key, across both
// generations.
func tracked(l *limiter, key string) []time.Duration {
	GinkgoHelper()
	if w, ok := l.cur[key]; ok {
		return w
	}
	return l.prev[key]
}

// fill loads the table to the cap with keys that have each spent hits
// attempts, all of them recorded age ago.
func fill(l *limiter, hits int, age time.Duration) {
	GinkgoHelper()
	at := time.Since(l.start) - age
	for i := range maxKeys {
		w := make([]time.Duration, hits)
		for h := range w {
			w[h] = at
		}
		l.cur[strconv.Itoa(i)] = w
	}
}

// saturate spends key's budget until an attempt is refused and reports how
// many were granted first.
func saturate(l Limiter, key string) int {
	GinkgoHelper()
	for n := range 1_000 {
		if !allowed(l, key) {
			return n
		}
	}
	Fail("key never ran out of budget")
	return 0
}

// greedyGrants spends key as fast as the limiter will allow — one attempt every
// window/steps of virtual time, over spans windows — and reports the elapsed
// time of every grant.
func greedyGrants(l *limiter, key string, steps, spans int) []time.Duration {
	GinkgoHelper()
	tick := l.window / time.Duration(steps)
	grants := make([]time.Duration, 0, steps*spans)
	for range steps * spans {
		if allowed(l, key) {
			grants = append(grants, time.Since(l.start))
		}
		rewind(l, tick)
	}
	return grants
}

// maxPerWindow reports the most grants any window-long span contains. Grants
// arrive in order, and a span runs from one of them to strictly less than
// window later, which is the span the limiter itself measures.
func maxPerWindow(grants []time.Duration, window time.Duration) int {
	GinkgoHelper()
	most, first := 0, 0
	for last := range grants {
		for grants[last]-grants[first] >= window {
			first++
		}
		if n := last - first + 1; n > most {
			most = n
		}
	}
	return most
}

var _ = Describe("Limiter", Label("unit", "auth"), func() {
	It("allows up to N hits then denies", func() {
		l := NewLimiter(3, time.Minute)
		Expect(allowed(l, "1.1.1.1")).To(BeTrue())
		Expect(allowed(l, "1.1.1.1")).To(BeTrue())
		Expect(allowed(l, "1.1.1.1")).To(BeTrue())
		Expect(allowed(l, "1.1.1.1")).To(BeFalse())
	})

	It("segregates counts per key", func() {
		l := NewLimiter(1, time.Minute)
		Expect(allowed(l, "1.1.1.1")).To(BeTrue())
		Expect(allowed(l, "2.2.2.2")).To(BeTrue())
		Expect(allowed(l, "1.1.1.1")).To(BeFalse())
	})

	It("refuses every attempt at a zero limit", func() {
		l := NewLimiter(0, time.Minute)
		ok, wait := l.Allow("1.1.1.1")
		Expect(ok).To(BeFalse())
		Expect(wait).To(BeNumerically(">", 0))
	})

	Describe("Refund", func() {
		It("returns the refunded attempt to the budget", func() {
			l := NewLimiter(3, time.Minute)
			Expect(saturate(l, "1.1.1.1")).To(Equal(3))

			l.Refund("1.1.1.1")

			Expect(allowed(l, "1.1.1.1")).To(BeTrue())
			Expect(allowed(l, "1.1.1.1")).To(BeFalse())
		})

		// The case that motivated it: an address that only ever succeeds is
		// never throttled, however many times it comes back.
		It("never throttles a key whose every attempt is refunded", func() {
			l := NewLimiter(3, time.Minute)
			for range 20 {
				Expect(allowed(l, "1.1.1.1")).To(BeTrue())
				l.Refund("1.1.1.1")
			}
		})

		It("lends nothing to a key that has spent nothing", func() {
			l := NewLimiter(1, time.Minute)
			l.Refund("1.1.1.1")

			Expect(allowed(l, "1.1.1.1")).To(BeTrue())
			Expect(allowed(l, "1.1.1.1")).To(BeFalse())
		})

		It("refunds only the calling key", func() {
			l := NewLimiter(1, time.Minute)
			Expect(allowed(l, "1.1.1.1")).To(BeTrue())
			Expect(allowed(l, "2.2.2.2")).To(BeTrue())

			l.Refund("1.1.1.1")

			Expect(allowed(l, "1.1.1.1")).To(BeTrue())
			Expect(allowed(l, "2.2.2.2")).To(BeFalse())
		})

		It(
			"folds an IPv6 refund onto the same /64 the attempt was charged to",
			func() {
				l := NewLimiter(1, time.Minute)
				Expect(allowed(l, "2001:db8::1")).To(BeTrue())

				l.Refund("2001:db8::2")

				Expect(allowed(l, "2001:db8::3")).To(BeTrue())
			},
		)

		// Allow leaves the key in cur; a rotation between the two calls moves it
		// to prev, and the refund still has to find it there.
		It("finds the attempt after a rotation has retired it", func() {
			l := NewLimiter(2, 15*time.Minute).(*limiter)
			Expect(allowed(l, "1.1.1.1")).To(BeTrue())

			rewind(l, 15*time.Minute)
			Expect(allowed(l, "9.9.9.9")).To(BeTrue()) // forces the rotation
			l.Refund("1.1.1.1")

			Expect(l.cur["1.1.1.1"]).To(BeEmpty())
			Expect(l.prev["1.1.1.1"]).To(BeEmpty())
		})
	})

	It("allows again once every recorded attempt has aged out", func() {
		l := NewLimiter(5, 15*time.Minute).(*limiter)
		Expect(saturate(l, "1.1.1.1")).To(Equal(5))

		rewind(l, 15*time.Minute)

		for range 5 {
			Expect(allowed(l, "1.1.1.1")).To(BeTrue())
		}
		Expect(allowed(l, "1.1.1.1")).To(BeFalse())
	})

	It("keeps the attempts that are still inside the window", func() {
		l := NewLimiter(5, 15*time.Minute).(*limiter)
		Expect(saturate(l, "1.1.1.1")).To(Equal(5))

		rewind(l, 14*time.Minute)

		Expect(allowed(l, "1.1.1.1")).To(BeFalse())
	})

	It("returns one attempt to the budget a window after it was made", func() {
		l := NewLimiter(5, 15*time.Minute).(*limiter)
		Expect(allowed(l, "1.1.1.1")).To(BeTrue())
		rewind(l, 5*time.Minute)
		for range 4 {
			Expect(allowed(l, "1.1.1.1")).To(BeTrue())
		}
		Expect(allowed(l, "1.1.1.1")).To(BeFalse())

		// Ten more minutes retires the first attempt and nothing else.
		rewind(l, 10*time.Minute)

		Expect(allowed(l, "1.1.1.1")).To(BeTrue())
		Expect(allowed(l, "1.1.1.1")).To(BeFalse())
	})

	It("reports no wait while the key is under the limit", func() {
		l := NewLimiter(2, time.Minute)
		_, wait := l.Allow("1.1.1.1")
		Expect(wait).To(Equal(time.Duration(0)))
	})

	It("reports the wait to the oldest attempt ageing out when it refuses", func() {
		l := NewLimiter(5, 15*time.Minute).(*limiter)
		Expect(saturate(l, "1.1.1.1")).To(Equal(5))

		rewind(l, 10*time.Minute)

		ok, wait := l.Allow("1.1.1.1")
		Expect(ok).To(BeFalse())
		Expect(wait).To(BeNumerically("~", 5*time.Minute, time.Second))
	})

	It("never reports a zero wait alongside a refusal", func() {
		l := NewLimiter(5, 15*time.Minute).(*limiter)
		Expect(saturate(l, "1.1.1.1")).To(Equal(5))

		// All but half a second of the window has passed: the wait is under a
		// second, which a caller formatting whole seconds must not render as
		// "retry now".
		rewind(l, 15*time.Minute-500*time.Millisecond)

		ok, wait := l.Allow("1.1.1.1")
		Expect(ok).To(BeFalse())
		Expect(wait).To(BeNumerically(">", 0))
		Expect(wait).To(BeNumerically("<", time.Second))
	})

	Context("the per-window ceiling", func() {
		// The control the limiter exists to enforce is "N attempts per window",
		// measured the way an attacker would: greedily, against one key,
		// counting the most grants any window-long span can be made to hold.
		// A budget that refills continuously rather than by ageing out passes
		// every spec above and still lands here at N + window/interval - 1,
		// which for 5 per 15 minutes is 9.
		DescribeTable(
			"grants at most the limit inside any window-long span",
			func(limit uint8, window time.Duration) {
				l := NewLimiter(limit, window).(*limiter)

				grants := greedyGrants(l, "203.0.113.7", 100, 3)

				Expect(len(grants)).To(BeNumerically(">", int(limit)),
					"the schedule has to outlive one budget for the ceiling to mean anything")
				Expect(maxPerWindow(grants, window)).To(Equal(int(limit)))
			},
			Entry(
				"5 attempts per 15 minutes, as deployed",
				uint8(5),
				15*time.Minute,
			),
			Entry("3 attempts per minute", uint8(3), time.Minute),
			Entry("1 attempt per hour", uint8(1), time.Hour),
		)
	})

	Context("IPv6 aggregation", func() {
		It("counts one /64 against a single budget", func() {
			l := NewLimiter(2, time.Minute)
			Expect(allowed(l, "2001:db8:1:2::1")).To(BeTrue())
			Expect(allowed(l, "2001:db8:1:2::2")).To(BeTrue())
			Expect(allowed(l, "2001:db8:1:2:ffff:ffff:ffff:ffff")).To(BeFalse())
		})

		It("keeps two different /64s independent", func() {
			l := NewLimiter(1, time.Minute)
			Expect(allowed(l, "2001:db8:1:2::1")).To(BeTrue())
			Expect(allowed(l, "2001:db8:1:3::1")).To(BeTrue())
			Expect(allowed(l, "2001:db8:1:2::9")).To(BeFalse())
		})

		It("tracks one /64 as one key", func() {
			l := NewLimiter(5, time.Minute).(*limiter)
			for i := range 100 {
				l.Allow("2001:db8:1:2::" + strconv.Itoa(i))
			}
			Expect(l.cur).To(HaveLen(1))
			Expect(l.cur).To(HaveKey("2001:db8:1:2::/64"))
		})

		It("leaves IPv4 addresses on their own budget", func() {
			l := NewLimiter(1, time.Minute)
			Expect(allowed(l, "1.1.1.1")).To(BeTrue())
			Expect(allowed(l, "1.1.1.2")).To(BeTrue())
			Expect(allowed(l, "1.1.1.1")).To(BeFalse())
		})

		It("cannot be split by v4-mapped notation", func() {
			l := NewLimiter(1, time.Minute)
			Expect(allowed(l, "1.1.1.1")).To(BeTrue())
			Expect(allowed(l, "::ffff:1.1.1.1")).To(BeFalse())
		})

		It("passes a key that is not an address through untouched", func() {
			l := NewLimiter(1, time.Minute)
			Expect(allowed(l, "unknown")).To(BeTrue())
			Expect(allowed(l, "unknown")).To(BeFalse())
		})
	})

	Context("expiry", func() {
		It("leaves no entry behind a key that stops attempting", func() {
			l := NewLimiter(5, 15*time.Minute).(*limiter)
			Expect(saturate(l, "203.0.113.7")).To(Equal(5))

			// One window retires the entry into the older generation, the next
			// drops that generation, and neither needed a scan to find it.
			rewind(l, 15*time.Minute)
			l.Allow("198.51.100.1")
			rewind(l, 15*time.Minute)
			l.Allow("198.51.100.1")

			Expect(tracked(l, "203.0.113.7")).To(BeEmpty())
		})

		It(
			"keeps a throttle that outlives the generation it was recorded in",
			func() {
				l := NewLimiter(5, 15*time.Minute).(*limiter)
				Expect(saturate(l, "203.0.113.7")).To(Equal(5))

				rewind(l, 14*time.Minute)
				l.Allow("198.51.100.1")

				ok, wait := l.Allow("203.0.113.7")
				Expect(ok).To(BeFalse())
				Expect(wait).To(BeNumerically("~", time.Minute, time.Second))
			},
		)

		It("frees the whole table when the traffic stops", func() {
			l := NewLimiter(5, 15*time.Minute).(*limiter)
			for i := range 50_000 {
				l.Allow("flood-" + strconv.Itoa(i))
			}

			rewind(l, 30*time.Minute)
			l.Allow("198.51.100.1")

			Expect(len(l.cur) + len(l.prev)).To(Equal(1))
		})
	})

	Context("under key-space pressure", func() {
		It("never grows past the cap", func() {
			l := NewLimiter(5, 15*time.Minute).(*limiter)
			for i := range maxKeys + 50_000 {
				l.Allow("flood-" + strconv.Itoa(i))
			}

			Expect(len(l.cur) + len(l.prev)).To(Equal(maxKeys))
		})

		It("meters every address of a routed /48 flood", func() {
			// What the cap is sized for, asserted rather than asserted about:
			// 65,536 keys is a routed IPv6 /48, the cheapest block an attacker
			// can rent, and it fits four times over, so every address in it is
			// still tracked and still cut off at the limit. Shrink the cap past
			// a /48 and this is the spec that notices.
			const routed48 = 1 << 16
			keys := make([]string, routed48)
			for i := range keys {
				keys[i] = fmt.Sprintf("2001:db8:0:%x::1", i)
			}

			l := NewLimiter(5, 15*time.Minute)
			granted := 0
			for range 8 {
				for _, k := range keys {
					if ok, _ := l.Allow(k); ok {
						granted++
					}
				}
			}
			Expect(granted).To(Equal(5 * routed48))
		})

		It("admits a new key when every tracked key is mid-throttle", func() {
			// Restored from the evicting map this replaced. Its subject was the
			// eviction policy, but its content is the property that outlived it:
			// a caller that has made no attempts is never turned away, whatever
			// the table is holding. Here it holds nothing but keys that have
			// spent their budget, so there is no dead weight to reclaim and no
			// slot to give away — and the answer is to admit the newcomer
			// without a slot rather than to take one off somebody.
			l := NewLimiter(2, time.Minute).(*limiter)
			fill(l, 2, 0)

			Expect(allowed(l, "fresh")).To(BeTrue())
			Expect(tracked(l, "fresh")).To(BeEmpty())
			Expect(len(l.cur) + len(l.prev)).To(Equal(maxKeys))
		})

		It("admits every new caller through a flood that fills the table", func() {
			// Calibrated past the point where tracking lapses, not short of it:
			// the flood spends the budget of maxKeys distinct keys, so every
			// legitimate caller after it arrives at a table with no room. The
			// property is unconditional, so this holds at any flood size — what
			// this scale adds is that it holds on the far side of the cap too.
			l := NewLimiter(5, 15*time.Minute).(*limiter)
			for i := range maxKeys {
				for range 5 {
					l.Allow("flood-" + strconv.Itoa(i))
				}
			}
			Expect(len(l.cur) + len(l.prev)).To(Equal(maxKeys))

			for i := range 10_000 {
				Expect(allowed(l, "legit-"+strconv.Itoa(i))).To(BeTrue())
			}
		})

		It("refuses nobody on account of another key's attempts", func() {
			// There are no shared counters to collide in, so this is exact
			// rather than a rate: the table is full of keys that have spent
			// their budget, and not one of a million unrelated first-time
			// callers is refused.
			l := NewLimiter(5, 15*time.Minute).(*limiter)
			fill(l, 5, 0)

			refused := 0
			for i := range 1_000_000 {
				if !allowed(l, "innocent-"+strconv.Itoa(i)) {
					refused++
				}
			}
			Expect(refused).To(Equal(0))
		})

		It("keeps a throttled key throttled through a flood of unique keys", func() {
			const victim = "203.0.113.7"
			l := NewLimiter(5, 15*time.Minute)
			Expect(saturate(l, victim)).To(Equal(5))

			for i := range maxKeys + 50_000 {
				l.Allow("flood-" + strconv.Itoa(i))
			}

			ok, wait := l.Allow(victim)
			Expect(ok).To(BeFalse())
			Expect(wait).To(BeNumerically(">", 0))
		})

		It("keeps a throttled key throttled through one-hit keys", func() {
			// The shape that reset a throttle when the table evicted: fill it
			// with cheap one-hit keys, then keep inserting so every new key
			// arrives at a full table. Nothing is ever dropped to make room now,
			// so the sequence is only traffic.
			const victim = "203.0.113.7"
			l := NewLimiter(5, 15*time.Minute)
			Expect(saturate(l, victim)).To(Equal(5))

			for i := range maxKeys {
				l.Allow("filler-" + strconv.Itoa(i))
			}
			for i := range 50_000 {
				l.Allow("insert-" + strconv.Itoa(i))
			}

			ok, wait := l.Allow(victim)
			Expect(ok).To(BeFalse())
			Expect(wait).To(BeNumerically(">", 0))
		})

		It("never lets other traffic move a key's recorded attempts", func() {
			const victim = "198.51.100.9"
			l := NewLimiter(5, 15*time.Minute).(*limiter)
			Expect(saturate(l, victim)).To(Equal(5))
			before := append([]time.Duration(nil), tracked(l, victim)...)

			for i := range maxKeys + 50_000 {
				l.Allow("flood-" + strconv.Itoa(i))
			}

			Expect(tracked(l, victim)).To(Equal(before))
		})

		It("keeps a call constant-time against a full table", func() {
			l := NewLimiter(5, 15*time.Minute).(*limiter)
			fill(l, 5, 0)

			const calls = 10_000
			keys := make([]string, calls)
			for i := range keys {
				keys[i] = "timed-" + strconv.Itoa(i)
			}

			start := time.Now()
			for _, k := range keys {
				l.Allow(k)
			}
			per := time.Since(start) / calls

			// A hash lookup and a bounded copy, with no scan and no eviction
			// ranking: sub-microsecond in practice, and the bound is loose
			// enough to survive a busy machine while still catching a structure
			// that walks its state.
			Expect(per).To(BeNumerically("<", 20*time.Microsecond))
		})
	})
})
