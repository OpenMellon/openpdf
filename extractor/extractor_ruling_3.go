package extractor

import (
	"fmt"
	"image/color"
	"math"
)

type ruling struct {
	kind rulingKind
	mark markKind
	color.Color
	xy    float64
	lo    float64
	hi    float64
	width float64
}

func (r *ruling) alignsSec(other *ruling) bool {
	const secondaryAlignSlack = alignTolerance + 1.0
	return r.lo-secondaryAlignSlack <= other.hi && other.lo-secondaryAlignSlack <= r.hi
}

func (r *ruling) gridIntersecting(other *ruling) bool {
	return approxEqual(r.lo, other.lo) && approxEqual(r.hi, other.hi)
}

func (r *ruling) intersects(other *ruling) bool {
	perp := (r.kind == rulingVert && other.kind == rulingHorz) || (other.kind == rulingVert && r.kind == rulingHorz)
	spans := func(a, b *ruling) bool {
		return a.lo-approxTolerance <= b.xy && b.xy <= a.hi+approxTolerance
	}
	ab := spans(r, other)
	ba := spans(other, r)
	if debugRulings {
		fmt.Printf("    intersects:  orthogonal=%t o1=%t o2=%t → %t\n"+"       v=%s\n"+"      w=%s\n", perp, ab, ba, perp && ab && ba, r, other)
	}
	return perp && ab && ba
}

func (r *ruling) alignsPrimary(other *ruling) bool {
	return r.kind == other.kind && math.Abs(r.xy-other.xy) < alignTolerance*0.5
}

func (r *ruling) encloses(lo, hi float64) bool {
	return r.lo-approxTolerance <= lo && hi <= r.hi+approxTolerance
}

func (r *ruling) equals(other *ruling) bool {
	return r.kind == other.kind && approxEqual(r.xy, other.xy) && approxEqual(r.lo, other.lo) && approxEqual(r.hi, other.hi)
}

// String returns a description of `v`.
func (r *ruling) String() string {
	if r.kind == rulingNone {
		return "NOT RULING"
	}
	primAxis, secAxis := "x", "y"
	if r.kind == rulingHorz {
		primAxis, secAxis = "y", "x"
	}
	widthStr := ""
	if r.width != 0.0 {
		widthStr = fmt.Sprintf(" width=%.2f", r.width)
	}
	return fmt.Sprintf("%10s %s=%6.2f %s=%6.2f - %6.2f (%6.2f) %s %v%s", r.kind, primAxis, r.xy, secAxis, r.lo, r.hi, r.hi-r.lo, r.mark, r.Color, widthStr)
}
