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

	It("allows again once every recorded hit has aged out", func() {
		l := NewLimiter(1, time.Minute).(*limiter)
		l.windows["1.1.1.1"] = []time.Time{time.Now().Add(-2 * time.Minute)}
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.Allow("1.1.1.1")).To(BeFalse())
	})

	It("keeps hits that are still inside the window", func() {
		l := NewLimiter(2, time.Minute).(*limiter)
		l.windows["1.1.1.1"] = []time.Time{
			time.Now().Add(-2 * time.Minute),
			time.Now().Add(-time.Second),
		}
		Expect(l.Allow("1.1.1.1")).To(BeTrue())
		Expect(l.windows["1.1.1.1"]).To(HaveLen(2))
	})

	It("deletes the key once no timestamps remain in the window", func() {
		l := NewLimiter(1, time.Minute).(*limiter)
		l.windows["1.1.1.1"] = []time.Time{time.Now().Add(-2 * time.Minute)}

		Expect(l.RetryAfter("1.1.1.1")).To(Equal(time.Duration(0)))
		Expect(l.windows).NotTo(HaveKey("1.1.1.1"))
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
