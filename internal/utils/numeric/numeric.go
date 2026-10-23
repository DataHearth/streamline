// Package numeric narrows integers at the boundaries where a Go int meets a
// narrower storage or protocol type. Values outside the target's range
// saturate at its ceiling instead of wrapping, so a pathological count
// degrades to a pinned maximum rather than a small lie.
//
// The guard-then-convert shape is also what lets gosec (G115) prove the
// conversion cannot overflow, which is what keeps the call sites free of
// per-line suppressions.
package numeric

import "math"

// Wider is any integer type wide enough to hold the values these helpers
// narrow from: a len(), a COUNT(*), or an operator-supplied flag.
type Wider interface {
	~int | ~int64 | ~uint | ~uint32 | ~uint64
}

// SaturateU16 narrows to uint16, clamping below at 0 and above at
// math.MaxUint16.
func SaturateU16[F Wider](v F) uint16 {
	if v <= 0 {
		return 0
	}
	if uint64(v) >= math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(v)
}

// SaturateU32 narrows to uint32, clamping below at 0 and above at
// math.MaxUint32.
func SaturateU32[F Wider](v F) uint32 {
	if v <= 0 {
		return 0
	}
	if uint64(v) >= math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}
