package extractor

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

type rulingList []*ruling

func (rl rulingList) toTilings() (rulingList, []gridTiling) {
	rl.log("toTilings")
	if len(rl) == 0 {
		return nil, nil
	}
	rl = rl.tidied("all")
	rl.log("tidied")
	rulings := rl.toGrids()
	tilings := make([]gridTiling, len(rulings))
	for i, rulings2 := range rulings {
		tilings[i] = rulings2.asTiling()
	}
	return rl, tilings
}

func (rl rulingList) connections(adj map[int]intSet, start int) intSet {
	connected := make(intSet)
	visited := make(intSet)
	var visit func(int)
	visit = func(cur int) {
		if !visited.has(cur) {
			visited.add(cur)
			for j := range rl {
				if adj[j].has(cur) {
					connected.add(j)
				}
			}
			for j := range rl {
				if connected.has(j) {
					visit(j)
				}
			}
		}
	}
	visit(start)
	return connected
}

func (rl rulingList) toGrids() []rulingList {
	if debugRulings {
		common.Log.Info("toGrids: %s", rl)
	}
	inter := rl.intersections()
	if debugRulings {
		common.Log.Info("toGrids: vecs=%d intersects=%d ", len(rl), len(inter))
		for _, k := range sortedIntSetKeys(inter) {
			fmt.Printf("%4d: %+v\n", k, inter[k])
		}
	}
	conn := make(map[int]intSet, len(rl))
	for i := range rl {
		s := rl.connections(inter, i)
		if len(s) > 0 {
			conn[i] = s
		}
	}
	if debugRulings {
		common.Log.Info("toGrids: connects=%d", len(conn))
		for _, k := range sortedIntSetKeys(conn) {
			fmt.Printf("%4d: %+v\n", k, conn[k])
		}
	}
	order := sortedIndexes(len(rl), func(i, j int) bool {
		ci, cj := len(conn[i]), len(conn[j])
		if ci != cj {
			return ci > cj
		}
		return rl.comp(i, j)
	})
	if debugRulings {
		common.Log.Info("toGrids: ordering=%v", order)
	}
	groups := [][]int{{order[0]}}
nextIndex:
	for _, idx := range order[1:] {
		for gi, grp := range groups {
			for _, member := range grp {
				if conn[member].has(idx) {
					groups[gi] = append(grp, idx)
					continue nextIndex
				}
			}
		}
		groups = append(groups, []int{idx})
	}
	if debugRulings {
		common.Log.Info("toGrids: igrids=%v", groups)
	}
	sort.SliceStable(groups, func(i, j int) bool { return len(groups[i]) > len(groups[j]) })
	for _, grp := range groups {
		sort.Slice(grp, func(i, j int) bool { return rl.comp(grp[i], grp[j]) })
	}
	grids := make([]rulingList, len(groups))
	for gi, grp := range groups {
		grid := make(rulingList, len(grp))
		for mi, m := range grp {
			grid[mi] = rl[m]
		}
		grids[gi] = grid
	}
	if debugRulings {
		common.Log.Info("toGrids: grids=%+v", grids)
	}
	var actual []rulingList
	for _, g := range grids {
		if ag, ok := g.isActualGrid(); ok {
			g = ag
			g = g.snapToGroups()
			actual = append(actual, g)
		}
	}
	if debugRulings {
		logGrids("toGrids: actualGrids", actual)
		common.Log.Info("toGrids: grids=%d actualGrids=%d", len(grids), len(actual))
	}
	return actual
}

func (rl rulingList) intersections() map[int]intSet {
	var vert, horz []int
	for i, r := range rl {
		switch r.kind {
		case rulingVert:
			vert = append(vert, i)
		case rulingHorz:
			horz = append(horz, i)
		}
	}
	if len(vert) < minGridCols+1 || len(horz) < minGridRows+1 {
		return nil
	}
	if len(vert)+len(horz) > maxGridRulings {
		common.Log.Debug("intersections: TOO MANY rulings vecs=%d = %d x %d", len(rl), len(vert), len(horz))
		return nil
	}
	inter := make(map[int]intSet, len(vert)+len(horz))
	for _, vert2 := range vert {
		for _, horz2 := range horz {
			if rl[vert2].intersects(rl[horz2]) {
				if _, ok := inter[vert2]; !ok {
					inter[vert2] = make(intSet)
				}
				if _, ok := inter[horz2]; !ok {
					inter[horz2] = make(intSet)
				}
				inter[vert2].add(horz2)
				inter[horz2].add(vert2)
			}
		}
	}
	return inter
}

func (rl rulingList) isActualGrid() (rulingList, bool) {
	verts, horzs := rl.augmentGrid()
	if !(len(verts) >= minGridCols+1 && len(horzs) >= minGridRows+1) {
		if debugRulings {
			common.Log.Info("isActualGrid: Not aligned. %d x %d < %d x %d", len(verts), len(horzs), minGridCols+1, minGridRows+1)
		}
		return nil, false
	}
	if debugRulings {
		common.Log.Info("isActualGrid: %s : %t & %t → %t", rl, len(verts) >= 2, len(horzs) >= 2, len(verts) >= 2 && len(horzs) >= 2)
		for i, r := range rl {
			fmt.Printf("%4d: %v\n", i, r)
		}
	}
	if checkGridCorners {
		firstV, lastV := verts[0], verts[len(verts)-1]
		firstH, lastH := horzs[0], horzs[len(horzs)-1]
		if !(closeEnough(firstV.xy-firstH.lo) && closeEnough(lastV.xy-firstH.hi) && closeEnough(firstH.xy-firstV.hi) && closeEnough(lastH.xy-firstV.lo)) {
			if debugRulings {
				common.Log.Info("isActualGrid:  Not aligned.\n\tv0=%s\n\tv1=%s\n\th0=%s\n\th1=%s", firstV, lastV, firstH, lastH)
			}
			return nil, false
		}
	} else {
		if !verts.aligned() {
			if debugMergeRulings {
				common.Log.Info("isActualGrid: Not aligned verts. %d", len(verts))
			}
			return nil, false
		}
		if !horzs.aligned() {
			if debugRulings {
				common.Log.Info("isActualGrid: Not aligned horzs. %d", len(horzs))
			}
			return nil, false
		}
	}
	all := append(verts, horzs...)
	return all, true
}

func (rl rulingList) snapToGroupsDirection() rulingList {
	rl.sortStrict()
	groups := make(map[*ruling]rulingList, len(rl))
	cur := rl[0]
	startGroup := func(r *ruling) { cur = r; groups[cur] = rulingList{r} }
	startGroup(rl[0])
	for _, r := range rl[1:] {
		if r.xy < cur.xy-zeroTolerance {
			common.Log.Error("snapToGroupsDirection: Wrong primary order.\n\tv0=%s\n\t v=%s", cur, r)
		}
		if r.xy > cur.xy+alignTolerance {
			startGroup(r)
		} else {
			groups[cur] = append(groups[cur], r)
		}
	}
	pos := make(map[*ruling]float64, len(groups))
	leader := make(map[*ruling]*ruling, len(rl))
	for lead, members := range groups {
		pos[lead] = members.mergePrimary()
		for _, m := range members {
			leader[m] = lead
		}
	}
	for _, r := range rl {
		r.xy = pos[leader[r]]
	}
	out := make(rulingList, 0, len(rl))
	for _, members := range groups {
		parts := members.splitSec()
		for i, part := range parts {
			merged := part.merge()
			if len(out) > 0 {
				last := out[len(out)-1]
				if last.alignsPrimary(merged) && last.alignsSec(merged) {
					common.Log.Error("snapToGroupsDirection: Duplicate i=%d\n\tw=%s\n\tv=%s", i, last, merged)
					continue
				}
			}
			out = append(out, merged)
		}
	}
	out.sortStrict()
	return out
}

func (rl rulingList) findPrimSec(xy, sec float64) *ruling {
	for _, r := range rl {
		if isZero(r.xy-xy) && r.lo-approxTolerance <= sec && sec <= r.hi+approxTolerance {
			return r
		}
	}
	return nil
}

func (rl rulingList) augmentGrid() (rulingList, rulingList) {
	verts, horzs := rl.vertsHorzs()
	if len(verts) == 0 || len(horzs) == 0 {
		return verts, horzs
	}
	origVerts, origHorzs := verts, horzs
	vbox := verts.bbox()
	hbox := horzs.bbox()
	if debugRulings {
		common.Log.Info("augmentGrid: bboxV=%6.2f", vbox)
		common.Log.Info("augmentGrid: bboxH=%6.2f", hbox)
	}
	var left, right, bottom, top *ruling
	if hbox.Llx < vbox.Llx-approxTolerance {
		left = &ruling{mark: markKindAugment, kind: rulingVert, xy: hbox.Llx, lo: vbox.Lly, hi: vbox.Ury}
		verts = append(rulingList{left}, verts...)
	}
	if hbox.Urx > vbox.Urx+approxTolerance {
		right = &ruling{mark: markKindAugment, kind: rulingVert, xy: hbox.Urx, lo: vbox.Lly, hi: vbox.Ury}
		verts = append(verts, right)
	}
	if vbox.Lly < hbox.Lly-approxTolerance {
		bottom = &ruling{mark: markKindAugment, kind: rulingHorz, xy: vbox.Lly, lo: hbox.Llx, hi: hbox.Urx}
		horzs = append(rulingList{bottom}, horzs...)
	}
	if vbox.Ury > hbox.Ury+approxTolerance {
		top = &ruling{mark: markKindAugment, kind: rulingHorz, xy: vbox.Ury, lo: hbox.Llx, hi: hbox.Urx}
		horzs = append(horzs, top)
	}
	if len(verts)+len(horzs) == len(rl) {
		return origVerts, origHorzs
	}
	all := append(verts, horzs...)
	rl.log("unaugmented")
	all.log("augmented")
	return verts, horzs
}

func (rl rulingList) snapToGroups() rulingList {
	vertsHorzs, rulings := rl.vertsHorzs()
	if len(vertsHorzs) > 0 {
		vertsHorzs = vertsHorzs.snapToGroupsDirection()
	}
	if len(rulings) > 0 {
		rulings = rulings.snapToGroupsDirection()
	}
	vertsHorzs2 := append(vertsHorzs, rulings...)
	vertsHorzs2.log("snapToGroups")
	return vertsHorzs2
}

func (rl rulingList) asTiling() gridTiling {
	if debugGrids {
		common.Log.Info("rulingList.asTiling: vecs=%d ==================+++ ==================", len(rl))
	}
	for i, r := range rl[1:] {
		rl2 := rl[i]
		if rl2.alignsPrimary(r) && rl2.alignsSec(r) {
			common.Log.Error("asTiling: Duplicate rulings.\n\tv=%s\n\tw=%s", r, rl2)
		}
	}
	rl.sortStrict()
	rl.log("snapped")
	verts, horzs := rl.vertsHorzs()
	xs := verts.primaries()
	ys := horzs.primaries()
	numCols := len(xs) - 1
	numRows := len(ys) - 1
	if numCols == 0 || numRows == 0 {
		return gridTiling{}
	}
	rect := model.PdfRectangle{Llx: xs[0], Urx: xs[numCols], Lly: ys[0], Ury: ys[numRows]}
	if debugGrids {
		common.Log.Info("rulingList.asTiling: verts=%d", len(verts))
		for i, vert := range verts {
			fmt.Printf("%4d: %s\n", i, vert)
		}
		common.Log.Info("rulingList.asTiling: horzs=%d", len(horzs))
		for i, horz := range horzs {
			fmt.Printf("%4d: %s\n", i, horz)
		}
		common.Log.Info("rulingList.asTiling:  wxh=%dx%d\n\tllx=%.2f\n\tlly=%.2f", numCols, numRows, xs, ys)
	}
	tiles := make([]gridTile, numCols*numRows)
	for row := numRows - 1; row >= 0; row-- {
		y0 := ys[row]
		y1 := ys[row+1]
		for col := 0; col < numCols; col++ {
			x0 := xs[col]
			x1 := xs[col+1]
			left := verts.findPrimSec(x0, y0)
			right := verts.findPrimSec(x1, y0)
			bottom := horzs.findPrimSec(y0, x0)
			top := horzs.findPrimSec(y1, x0)
			cellRect := model.PdfRectangle{Llx: x0, Urx: x1, Lly: y0, Ury: y1}
			tile := newGridTile(cellRect, left, right, bottom, top)
			tiles[row*numCols+col] = tile
			if debugGrids {
				fmt.Printf("  x=%2d y=%2d: %s %6.2f x %6.2f\n", col, row, tile.String(), tile.Width(), tile.Height())
			}
		}
	}
	if debugGrids {
		common.Log.Info("rulingList.asTiling: coalesce horizontal. %6.2f", rect)
	}
	rowTiles := make([]map[float64]gridTile, numRows)
	for row := numRows - 1; row >= 0; row-- {
		if debugGrids {
			fmt.Printf("  y=%2d\n", row)
		}
		rowTiles[row] = make(map[float64]gridTile, numCols)
		for col := 0; col < numCols; col++ {
			t := tiles[row*numCols+col]
			if debugGrids {
				fmt.Printf("  %4d: %s\n", col, t)
			}
			if !t.llx {
				continue
			}
			last := col
			for k := col + 1; !t.urx && k < numCols; k++ {
				next := tiles[row*numCols+k]
				t.Urx = next.Urx
				t.ury = t.ury || next.ury
				t.lly = t.lly || next.lly
				t.urx = next.urx
				if debugGrids {
					fmt.Printf("   %4d: %s → %s\n", k, next, t)
				}
				last = k
			}
			if debugGrids {
				fmt.Printf("   %2d - %2d → %s\n", col, last, t)
			}
			col = last
			rowTiles[row][t.Llx] = t
		}
	}
	merged := make(map[float64]map[float64]gridTile, numRows)
	used := make(map[float64]map[float64]struct{}, numRows)
	for row := numRows - 1; row >= 0; row-- {
		lly := tiles[row*numCols].Lly
		merged[lly] = make(map[float64]gridTile, numCols)
		used[lly] = make(map[float64]struct{}, numCols)
	}
	if debugGrids {
		common.Log.Info("rulingList.asTiling: coalesce vertical. %6.2f", rect)
	}
	for row := numRows - 1; row >= 0; row-- {
		lly := tiles[row*numCols].Lly
		rowMap := rowTiles[row]
		if debugGrids {
			fmt.Printf("  y=%2d\n", row)
		}
		for _, llx := range sortedTileKeys(rowMap) {
			if _, ok := used[lly][llx]; ok {
				continue
			}
			t := rowMap[llx]
			if debugGrids {
				fmt.Printf("     v0=%s\n", t.String())
			}
			for j := row - 1; j >= 0; j-- {
				if t.lly {
					break
				}
				other := rowTiles[j]
				nt, ok := other[llx]
				if !ok {
					break
				}
				if nt.Urx != t.Urx {
					break
				}
				t.lly = nt.lly
				t.Lly = nt.Lly
				if debugGrids {
					fmt.Printf("        v=%s v0=%s\n", nt.String(), t.String())
				}
				used[nt.Lly][nt.Llx] = struct{}{}
			}
			if row == 0 {
				t.lly = true
			}
			if t.complete() {
				merged[lly][llx] = t
			}
		}
	}
	tiling := gridTiling{PdfRectangle: rect, llx: tileColumnKeys(merged), lly: tileRowKeys(merged), tiles: merged}
	tiling.log("Created")
	return tiling
}

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

func (rl rulingList) log(label string) {
	if !debugRulings {
		return
	}
	common.Log.Info("### %10s: vecs=%s", label, rl.String())
	for i, r := range rl {
		fmt.Printf("%4d: %s\n", i, r.String())
	}
}

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
