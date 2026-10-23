package metadata

import (
	"errors"
	"fmt"
	"time"
)

var ErrRateLimited = errors.New("metadata: provider rate limited")

// RateLimitedError carries how long the caller should leave the provider
// alone. It wraps ErrRateLimited, so errors.Is is the usual test.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf(
		"%v: retry after %s", ErrRateLimited, e.RetryAfter.Round(time.Second),
	)
}

func (e *RateLimitedError) Unwrap() error { return ErrRateLimited }

// Budgeter exposes a provider's daily request budget so a caller can stop
// before an expensive loop instead of failing halfway through it.
type Budgeter interface {
	// Remaining is the number of requests the provider will still send today.
	Remaining() int
	// ScanReserve is the part of the day's budget a bulk scan must leave
	// unspent for interactive calls.
	ScanReserve() int
}
