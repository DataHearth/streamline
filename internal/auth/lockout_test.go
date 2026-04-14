package auth

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LockoutPolicy", Label("unit"), func() {
	pol := LockoutPolicy{
		Threshold: 3,
		Window:    15 * time.Minute,
		Duration:  10 * time.Minute,
	}
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	It("starts unlocked with zero counter", func() {
		s := LockoutState{}
		locked, _ := IsLocked(s, t0)
		Expect(locked).To(BeFalse())
		Expect(s.FailedCount).To(Equal(uint8(0)))
	})

	It("increments counter on first failure", func() {
		s := OnFailedAttempt(LockoutState{}, t0, pol)
		Expect(s.FailedCount).To(Equal(uint8(1)))
		Expect(s.LastFailedAt).NotTo(BeNil())
		Expect(*s.LastFailedAt).To(Equal(t0))
		Expect(s.LockedUntil).To(BeNil())
	})

	It("resets the counter when the last failure is older than the window", func() {
		old := t0.Add(-20 * time.Minute)
		s := LockoutState{FailedCount: 2, LastFailedAt: &old}
		got := OnFailedAttempt(s, t0, pol)
		Expect(got.FailedCount).To(Equal(uint8(1)))
	})

	It("locks when counter reaches threshold", func() {
		recent := t0.Add(-1 * time.Minute)
		s := LockoutState{FailedCount: 2, LastFailedAt: &recent}
		got := OnFailedAttempt(s, t0, pol)
		Expect(got.FailedCount).To(Equal(uint8(3)))
		Expect(got.LockedUntil).NotTo(BeNil())
		Expect(*got.LockedUntil).To(Equal(t0.Add(pol.Duration)))
	})

	It("reports IsLocked while LockedUntil is in the future", func() {
		fut := t0.Add(5 * time.Minute)
		locked, until := IsLocked(LockoutState{LockedUntil: &fut}, t0)
		Expect(locked).To(BeTrue())
		Expect(until).To(Equal(fut))
	})

	It("reports unlocked once LockedUntil is in the past", func() {
		past := t0.Add(-1 * time.Minute)
		locked, _ := IsLocked(LockoutState{LockedUntil: &past, FailedCount: 5}, t0)
		Expect(locked).To(BeFalse())
	})
})
