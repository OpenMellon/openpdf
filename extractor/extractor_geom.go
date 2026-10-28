package extractor

import (
	"fmt"
	"math"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func topEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingHorz, xy: rect.Ury, lo: rect.Llx, hi: rect.Urx}
}

func rightEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingVert, xy: rect.Urx, lo: rect.Lly, hi: rect.Ury}
}
func approxEqual(a, b float64) bool { return math.Abs(a-b) <= approxTolerance }

func addInterval(vals []float64, lo, hi float64) []float64 {
	a, b := lo, hi
	if b < a {
		a, b = b, a
	}
	out := make([]float64, 0, len(vals)+2)
	out = append(out, lo)
	for _, v := range vals {
		if v <= a {
			continue
		} else if v >= b {
			break
		}
		out = append(out, v)
	}
	out = append(out, hi)
	return out
}

func intersectRects(a, b model.PdfRectangle) (model.PdfRectangle, bool) {
	if !rectsOverlap(a, b) {
		return model.PdfRectangle{}, false
	}
	return model.PdfRectangle{Llx: math.Max(a.Llx, b.Llx), Urx: math.Min(a.Urx, b.Urx), Lly: math.Max(a.Lly, b.Lly), Ury: math.Min(a.Ury, b.Ury)}, true
}
func isOdd(n int) bool              { return (n & 1) != 0 }
func roundToGrid(x float64) float64 { return coordGranularity * math.Round(x/coordGranularity) }

func (is intSet) add(v int) { is[v] = struct{}{} }
func roundToMultiple(v float64, multiple int) int {
	if multiple == 0 {
		multiple = 1
	}
	m := float64(multiple)
	return int(math.Round(v/m) * m)
}

func sortedIntSetKeys(m map[int]intSet) []int {
	keys := make([]int, 0, len(m))
	for i := range m {
		keys = append(keys, i)
	}
	sort.Ints(keys)
	return keys
}

func bottomEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingHorz, xy: rect.Lly, lo: rect.Llx, hi: rect.Urx}
}

func pointsEqual(a, b transform.Point) bool { return a.X == b.X && a.Y == b.Y }
func closeEnough(d float64) bool            { return math.Abs(d) < alignTolerance }
func isEven(n int) bool                     { return (n & 1) == 0 }

// String returns a human readable description of `s`.
func (is intSet) String() string {
	var idxs []int
	for i := range is {
		if is.has(i) {
			idxs = append(idxs, i)
		}
	}
	sort.Ints(idxs)
	return fmt.Sprintf("%+v", idxs)
}
func (is intSet) has(v int) bool { _, ok := is[v]; return ok }
func (is intSet) del(v int)      { delete(is, v) }

func rectContains(a, b model.PdfRectangle) bool {
	return a.Llx <= b.Llx && b.Urx <= a.Urx && a.Lly <= b.Lly && b.Ury <= a.Ury
}

func leftEdgeRuling(rect model.PdfRectangle) *ruling {
	return &ruling{kind: rulingVert, xy: rect.Llx, lo: rect.Lly, hi: rect.Ury}
}
func rectsOverlap(a, b model.PdfRectangle) bool { return xOverlap(a, b) && yOverlap(a, b) }
func translateBy(d transform.Point) transform.Matrix {
	return transform.TranslationMatrix(d.X, d.Y)
}

func splitRulingLists(rulings []rulingList) (rulingList, rulingList) {
	var all rulingList
	for _, l := range rulings {
		all = append(all, l...)
	}
	return all.vertsHorzs()
}

func granularize(sections []pathSection) {
	if coordGranularity < 0.0 {
		return
	}
	if debugRulings {
		common.Log.Info("granularize: %d subpath sections", len(sections))
	}
	for i, section := range sections {
		for j, sp := range section.subpaths {
			for k, point := range sp.points {
				sp.points[k] = transform.Point{X: roundToGrid(point.X), Y: roundToGrid(point.Y)}
				if debugRulings {
					point2 := sp.points[k]
					if !pointsEqual(point, point2) {
						pt := transform.Point{X: point2.X - point.X, Y: point2.Y - point.Y}
						fmt.Printf("%4d - %4d - %4d: %.2f → %.2f (%g)\n", i, j, k, point, point2, pt)
					}
				}
			}
		}
	}
}
func isZero(x float64) bool { return math.Abs(x) < zeroTolerance }

func isNearlyHorizontal(a, b transform.Point) bool {
	dx := math.Abs(a.X - b.X)
	dy := math.Abs(a.Y - b.Y)
	return isRatioSmall(dy, dx)
}

func unionRects(a, b model.PdfRectangle) model.PdfRectangle {
	return model.PdfRectangle{Llx: math.Min(a.Llx, b.Llx), Lly: math.Min(a.Lly, b.Lly), Urx: math.Max(a.Urx, b.Urx), Ury: math.Max(a.Ury, b.Ury)}
}

func sortedIntKeys(m map[int][]float64) []int {
	keys := make([]int, len(m))
	n := 0
	for i := range m {
		keys[n] = i
		n++
	}
	sort.Ints(keys)
	return keys
}

func isFlatSegment(p0, p1 transform.Point) bool {
	dx := math.Abs(p0.X - p1.X)
	dy := math.Abs(p0.Y - p1.Y)
	return isRatioSmall(dx, dy)
}

type intSet map[int]struct{}
