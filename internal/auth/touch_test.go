package auth

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("touchDebounce", Label("unit", "auth"), func() {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	It("allows a key it has never seen", func() {
		d := newTouchDebounce()
		Expect(d.allow("session:a", now)).To(BeTrue())
	})

	It("suppresses a repeat inside the interval", func() {
		d := newTouchDebounce()
		Expect(d.allow("session:a", now)).To(BeTrue())
		Expect(d.allow("session:a", now.Add(touchInterval-time.Second))).
			To(BeFalse())
	})

	It("allows again once the interval has passed", func() {
		d := newTouchDebounce()
		Expect(d.allow("session:a", now)).To(BeTrue())
		Expect(d.allow("session:a", now.Add(touchInterval))).To(BeTrue())
	})

	It("tracks keys independently", func() {
		d := newTouchDebounce()
		Expect(d.allow("session:a", now)).To(BeTrue())
		Expect(d.allow("session:b", now)).To(BeTrue())
		Expect(d.allow("session:a", now)).To(BeFalse())
	})

	It("does not grow without bound as keys age out", func() {
		d := newTouchDebounce()
		for i := range touchSweepAt {
			Expect(d.allow(fmt.Sprintf("session:%d", i), now)).To(BeTrue())
		}
		Expect(d.last).To(HaveLen(touchSweepAt))

		// One write past the sweep threshold, far enough ahead that every
		// existing entry is expired.
		Expect(d.allow("session:late", now.Add(2*touchInterval))).To(BeTrue())
		Expect(d.last).To(HaveLen(1))
	})

	It("keeps live entries when it sweeps", func() {
		d := newTouchDebounce()
		for i := range touchSweepAt - 1 {
			Expect(d.allow(fmt.Sprintf("session:%d", i), now)).To(BeTrue())
		}
		later := now.Add(touchInterval - time.Second)
		Expect(d.allow("session:fresh", later)).To(BeTrue())

		// Sweeping at a point where the bulk has expired but "fresh" has not.
		Expect(d.allow("session:trigger", now.Add(touchInterval+time.Second))).
			To(BeTrue())
		Expect(d.last).To(HaveKey("session:fresh"))
	})
})
