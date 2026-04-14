package auth

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Limiter", Label("unit", "auth"), func() {
	It("allows up to N hits then denies", func() {
		l := NewLimiter(3, time.Minute)
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.Allow("1.1.1.1")).To(BeFalse())
	})

	It("segregates counts per key", func() {
		l := NewLimiter(1, time.Minute)
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.Allow("2.2.2.2")).To(BeTrue())
		Expect(l.Allow("1.1.1.1")).To(BeFalse())
	})

	It("resets after window via clock override", func() {
		now := time.Unix(1000, 0)
		l := NewLimiterWithClock(
			1,
			time.Minute,
			func() time.Time { return now },
		)
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.Allow("1.1.1.1")).To(BeFalse())
		now = now.Add(time.Minute + time.Second)
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
	})

	It("RetryAfter is zero when under limit", func() {
		l := NewLimiter(2, time.Minute)
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.RetryAfter("1.1.1.1")).To(Equal(time.Duration(0)))
	})

	It("RetryAfter > 0 when over limit", func() {
		l := NewLimiter(1, time.Minute)
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.Allow("1.1.1.1")).To(BeFalse())
		Expect(l.RetryAfter("1.1.1.1")).To(BeNumerically(">", 0))
	})
})
