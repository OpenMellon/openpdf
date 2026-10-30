// Package intconv holds range-checked integer conversions for values parsed from untrusted input.
package intconv

import "math"

// ClampInt converts v to int, saturating at the int32 bounds so the result is the same on 32-bit and 64-bit platforms.
func ClampInt(v int64) int {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int(v)
}

// Uint32 converts v to uint32, reporting false when v is out of range.
func Uint32(v int64) (uint32, bool) {
	if v < 0 || v > math.MaxUint32 {
		return 0, false
	}
	return uint32(v), true
}
