package auth

import "time"

// LockoutPolicy is the tunable knobs read fresh from config on each login.
type LockoutPolicy struct {
	Threshold uint8
	Window    time.Duration
	Duration  time.Duration
}

// LockoutState mirrors the three lockout columns on the user row. Pointers
// distinguish "no failure yet" / "no active lockout" from zero values.
type LockoutState struct {
	FailedCount  uint8
	LastFailedAt *time.Time
	LockedUntil  *time.Time
}

// IsLocked reports whether the account is locked at now and, if so, when the
// lockout expires.
func IsLocked(s LockoutState, now time.Time) (bool, time.Time) {
	if s.LockedUntil == nil {
		return false, time.Time{}
	}
	if now.Before(*s.LockedUntil) {
		return true, *s.LockedUntil
	}
	return false, time.Time{}
}

// OnFailedAttempt advances state for one mismatched-password attempt. Resets
// the counter when the previous failure has fallen out of the sliding window.
// Stamps LockedUntil when the new counter meets the threshold.
func OnFailedAttempt(s LockoutState, now time.Time, p LockoutPolicy) LockoutState {
	out := s
	if out.LastFailedAt != nil && now.Sub(*out.LastFailedAt) > p.Window {
		out.FailedCount = 0
	}
	out.FailedCount++
	t := now
	out.LastFailedAt = &t
	if out.FailedCount >= p.Threshold {
		until := now.Add(p.Duration)
		out.LockedUntil = &until
	}
	return out
}
