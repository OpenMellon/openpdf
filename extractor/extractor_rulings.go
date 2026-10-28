package extractor

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func (rl rulingList) secMinMax() (float64, float64) {
	lo, hi := rl[0].lo, rl[0].hi
	for _, r := range rl[1:] {
		if r.lo < lo {
			lo = r.lo
		}
		if r.hi > hi {
			hi = r.hi
		}
	}
	return lo, hi
}

func (r *ruling) alignsSec(other *ruling) bool {
	const secondaryAlignSlack = alignTolerance + 1.0
	return r.lo-secondaryAlignSlack <= other.hi && other.lo-secondaryAlignSlack <= r.hi
}

type rulingList []*ruling

func (rl rulingList) primaries() []float64 {
	set := make(map[float64]struct{}, len(rl))
	for _, r := range rl {
		set[r.xy] = struct{}{}
	}
	out := make([]float64, len(set))
	n := 0
	for v := range set {
		out[n] = v
		n++
	}
	sort.Float64s(out)
	return out
}

// String returns a description of `k`.
func (rk rulingKind) String() string {
	name, ok := rulingKindNames[rk]
	if !ok {
		return fmt.Sprintf("Not a ruling: %d", rk)
	}
	return name
}

func (rl rulingList) log(label string) {
	if !debugRulings {
		return
	}
	common.Log.Info("### %10s: vecs=%s", label, rl.String())
	for i, r := range rl {
		fmt.Printf("%4d: %s\n", i, r.String())
	}
}

func (r *ruling) gridIntersecting(other *ruling) bool {
	return approxEqual(r.lo, other.lo) && approxEqual(r.hi, other.hi)
}

type rulingKind int

func (rl rulingList) vertsHorzs() (rulingList, rulingList) {
	var verts, horzs rulingList
	for _, r := range rl {
		switch r.kind {
		case rulingVert:
			verts = append(verts, r)
		case rulingHorz:
			horzs = append(horzs, r)
		}
	}
	return verts, horzs
}

func (rl rulingList) removeDuplicates() rulingList {
	if len(rl) == 0 {
		return nil
	}
	rl.sort()
	out := rulingList{rl[0]}
	for _, r := range rl[1:] {
		if r.equals(out[len(out)-1]) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (rl rulingList) bbox() model.PdfRectangle {
	var rect model.PdfRectangle
	if len(rl) == 0 {
		common.Log.Error("rulingList.bbox: no rulings")
		return model.PdfRectangle{}
	}
	if rl[0].kind == rulingHorz {
		rect.Llx, rect.Urx = rl.secMinMax()
		rect.Lly, rect.Ury = rl.primMinMax()
	} else {
		rect.Llx, rect.Urx = rl.primMinMax()
		rect.Lly, rect.Ury = rl.secMinMax()
	}
	return rect
}

func rulingKindOfLine(p0, p1 transform.Point) rulingKind {
	dx := math.Abs(p0.X - p1.X)
	dy := math.Abs(p0.Y - p1.Y)
	return kindFromDeltas(dx, dy, minRulingLength)
}

func (rl rulingList) aligned() bool {
	if len(rl) < 2 {
		return false
	}
	counts := make(map[*ruling]int)
	counts[rl[0]] = 0
	for _, r := range rl[1:] {
		matched := false
		for rep := range counts {
			if r.gridIntersecting(rep) {
				counts[rep]++
				matched = true
				break
			}
		}
		if !matched {
			counts[r] = 0
		}
	}
	numUnmatched := 0
	for _, cnt := range counts {
		if cnt == 0 {
			numUnmatched++
		}
	}
	frac := float64(numUnmatched) / float64(len(rl))
	ok := frac <= 1.0-alignedFraction
	if debugRulings {
		common.Log.Info("aligned=%t unmatched=%.2f=%d/%d vecs=%s", ok, frac, numUnmatched, len(rl), rl.String())
	}
	return ok
}

func (rl rulingList) merge() *ruling {
	sumXY := rl[0].xy
	lo := rl[0].lo
	hi := rl[0].hi
	for _, r := range rl[1:] {
		sumXY += r.xy
		if r.lo < lo {
			lo = r.lo
		}
		if r.hi > hi {
			hi = r.hi
		}
	}
	merged := &ruling{kind: rl[0].kind, mark: rl[0].mark, Color: rl[0].Color, xy: sumXY / float64(len(rl)), lo: lo, hi: hi}
	if debugMergeRulings {
		common.Log.Info("merge: %2d vecs %s", len(rl), merged)
		for i, r := range rl {
			fmt.Printf("%4d: %s\n", i, r)
		}
	}
	return merged
}

type ruling struct {
	kind rulingKind
	mark markKind
	color.Color
	xy    float64
	lo    float64
	hi    float64
	width float64
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

func (rl rulingList) splitSec() []rulingList {
	sort.Slice(rl, func(i, j int) bool {
		a, b := rl[i], rl[j]
		if a.lo != b.lo {
			return a.lo < b.lo
		}
		return a.hi < b.hi
	})
	seen := make(map[*ruling]struct{}, len(rl))
	collect := func(seed *ruling) rulingList {
		group := rulingList{seed}
		seen[seed] = struct{}{}
		for _, r := range rl {
			if _, ok := seen[r]; ok {
				continue
			}
			if slices.ContainsFunc(group, r.alignsSec) {
				group = append(group, r)
				seen[r] = struct{}{}
			}
		}
		return group
	}
	groups := []rulingList{collect(rl[0])}
	for _, r := range rl[1:] {
		if _, ok := seen[r]; ok {
			continue
		}
		groups = append(groups, collect(r))
	}
	return groups
}

func (rl rulingList) mergePrimary() float64 {
	sum := rl[0].xy
	for _, r := range rl[1:] {
		sum += r.xy
	}
	return sum / float64(len(rl))
}

func (rl rulingList) blocks(a, b *ruling) bool {
	if a.lo > b.hi || b.lo > a.hi {
		return false
	}
	lo := math.Max(a.lo, b.lo)
	hi := math.Min(a.hi, b.hi)
	if a.xy > b.xy {
		a, b = b, a
	}
	for _, r := range rl {
		if a.xy <= r.xy+alignTolerance && r.xy <= b.xy+alignTolerance && r.lo <= hi && lo <= r.hi {
			return true
		}
	}
	return false
}

func (r *ruling) alignsPrimary(other *ruling) bool {
	return r.kind == other.kind && math.Abs(r.xy-other.xy) < alignTolerance*0.5
}

func (r *ruling) encloses(lo, hi float64) bool {
	return r.lo-approxTolerance <= lo && hi <= r.hi+approxTolerance
}

func rulingKindOfSegment(p0, p1 transform.Point) rulingKind {
	dx := math.Abs(p0.X - p1.X)
	dy := math.Abs(p0.Y - p1.Y)
	return kindFromDeltas(dx, dy, maxSlope)
}

func (r *ruling) equals(other *ruling) bool {
	return r.kind == other.kind && approxEqual(r.xy, other.xy) && approxEqual(r.lo, other.lo) && approxEqual(r.hi, other.hi)
}

func (rl rulingList) sortStrict() {
	sort.Slice(rl, func(i, j int) bool {
		a, b := rl[i], rl[j]
		ka, kb := a.kind, b.kind
		if ka != kb {
			return ka > kb
		}
		va, vb := a.xy, b.xy
		if !isZero(va - vb) {
			return va < vb
		}
		va, vb = a.lo, b.lo
		if va != vb {
			return va < vb
		}
		return a.hi < b.hi
	})
}

func (rl rulingList) primMinMax() (float64, float64) {
	lo, hi := rl[0].xy, rl[0].xy
	for _, r := range rl[1:] {
		if r.xy < lo {
			lo = r.xy
		} else if r.xy > hi {
			hi = r.xy
		}
	}
	return lo, hi
}

func (rl rulingList) comp(i, j int) bool {
	a, b := rl[i], rl[j]
	ka, kb := a.kind, b.kind
	if ka != kb {
		return ka > kb
	}
	if ka == rulingNone {
		return false
	}
	orient := func(flag bool) bool {
		if ka == rulingHorz {
			return flag
		}
		return !flag
	}
	va, vb := a.xy, b.xy
	if va != vb {
		return orient(va > vb)
	}
	va, vb = a.lo, b.lo
	if va != vb {
		return orient(va < vb)
	}
	return orient(a.hi < b.hi)
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

func (rl rulingList) tidied(label string) rulingList {
	uniq := rl.removeDuplicates()
	uniq.log("uniques")
	snapped := uniq.snapToGroups()
	if snapped == nil {
		return nil
	}
	snapped.sort()
	if debugRulings {
		common.Log.Info("tidied: %q vecs=%d uniques=%d coalesced=%d", label, len(rl), len(uniq), len(snapped))
	}
	snapped.log("coalesced")
	return snapped
}

func (rl rulingList) sort() { sort.Slice(rl, rl.comp) }

// String returns a human readable description of `vecs`.
func (rl rulingList) String() string {
	if len(rl) == 0 {
		return "{ EMPTY }"
	}
	vertsHorzs, rulings := rl.vertsHorzs()
	numVertsHorzs := len(vertsHorzs)
	numRulings := len(rulings)
	if numVertsHorzs == 0 || numRulings == 0 {
		return fmt.Sprintf("{%d x %d}", numVertsHorzs, numRulings)
	}
	rect := model.PdfRectangle{Llx: vertsHorzs[0].xy, Urx: vertsHorzs[numVertsHorzs-1].xy, Lly: rulings[numRulings-1].xy, Ury: rulings[0].xy}
	return fmt.Sprintf("{%d x %d: %6.2f}", numVertsHorzs, numRulings, rect)
}
