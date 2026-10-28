package extractor

import (
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
)

type (
	paraList    []*textPara
	patternWalk struct {
		stream *core.PdfObjectStream
		ctm    transform.Matrix
	}
)

func (pl paraList) inTile(tile gridTile) paraList {
	var paras paraList
	for _, para := range pl {
		if tile.contains(para.PdfRectangle) {
			paras = append(paras, para)
		}
	}
	if debugTables {
		fmt.Printf("   inTile:  %s inside=%d\n", tile, len(paras))
		for i, para := range paras {
			fmt.Printf("%4d: %s\n", i, para)
		}
		fmt.Println("")
	}
	return paras
}

func (pl paraList) lines() []*textLine {
	var lines []*textLine
	for _, para := range pl {
		lines = append(lines, para.lines...)
	}
	return lines
}

func (pl paraList) merge() *textPara {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("merge: paras=%d ===========x=============", len(pl))
	}
	if len(pl) == 0 {
		return nil
	}
	pl.sortReadingOrder()
	rect := pl[0].PdfRectangle
	lines := pl[0].lines
	for _, para := range pl[1:] {
		rect = unionRects(rect, para.PdfRectangle)
		lines = append(lines, para.lines...)
	}
	return newTextPara(rect, lines)
}

func (pl paraList) list() []*list {
	var listLines []*textLine
	var allLines []*textLine
	for _, para := range pl {
		ll := para.getListLines()
		listLines = append(listLines, ll...)
		allLines = append(allLines, para.lines...)
	}
	byStartX := groupLinesByDepth(listLines)
	items := buildListsFromLines(allLines, byStartX)
	return items
}

func (pl paraList) yNeighbours(tol float64) map[*textPara][]int {
	evs := make([]event, 2*len(pl))
	if tol == 0 {
		for i, para := range pl {
			evs[2*i] = event{para.Lly, true, i}
			evs[2*i+1] = event{para.Ury, false, i}
		}
	} else {
		for i, para := range pl {
			evs[2*i] = event{para.Lly - tol*para.fontsize(), true, i}
			evs[2*i+1] = event{para.Ury + tol*para.fontsize(), false, i}
		}
	}
	return pl.eventNeighbours(evs)
}

func (pl paraList) computeEBBoxes() {
	if debugEBBoxes {
		common.Log.Info("computeEBBoxes:")
	}
	for _, para := range pl {
		para.eBBox = para.PdfRectangle
	}
	m := pl.yNeighbours(0)
	for i, p := range pl {
		box := p.eBBox
		leftLimit, rightLimit := -1.0e9, +1.0e9
		for _, nbIdx := range m[p] {
			nb := pl[nbIdx].eBBox
			if nb.Urx < box.Llx {
				leftLimit = math.Max(leftLimit, nb.Urx)
			} else if box.Urx < nb.Llx {
				rightLimit = math.Min(rightLimit, nb.Llx)
			}
		}
		for j, q := range pl {
			qBox := q.eBBox
			if i == j || qBox.Ury > box.Lly {
				continue
			}
			if leftLimit <= qBox.Llx && qBox.Llx < box.Llx {
				box.Llx = qBox.Llx
			} else if qBox.Urx <= rightLimit && box.Urx < qBox.Urx {
				box.Urx = qBox.Urx
			}
		}
		if debugEBBoxes {
			fmt.Printf("%4d: %6.2f→%6.2f %q\n", i, p.eBBox, box, truncate(p.text(), 50))
		}
		p.eBBox = box
	}
	if applyEnlargedBBoxes {
		for _, p := range pl {
			p.PdfRectangle = p.eBBox
		}
	}
}

func (pl paraList) reorder(idxs []int) {
	paras := make(paraList, len(pl))
	for i, idx := range idxs {
		paras[i] = pl[idx]
	}
	copy(pl, paras)
}

func (pl paraList) xNeighbours(tol float64) map[*textPara][]int {
	evs := make([]event, 2*len(pl))
	if tol == 0 {
		for i, para := range pl {
			evs[2*i] = event{para.Llx, true, i}
			evs[2*i+1] = event{para.Urx, false, i}
		}
	} else {
		for i, para := range pl {
			evs[2*i] = event{para.Llx - tol*para.fontsize(), true, i}
			evs[2*i+1] = event{para.Urx + tol*para.fontsize(), false, i}
		}
	}
	return pl.eventNeighbours(evs)
}

func (pl paraList) log(s string) {
	if !debugTopoOrder {
		return
	}
	common.Log.Info("%8s: %d paras =======-------=======", s, len(pl))
	for i, para := range pl {
		if para == nil {
			continue
		}
		text := para.text()
		s2 := "  "
		if para.table != nil {
			s2 = fmt.Sprintf("[%dx%d]", para.table.w, para.table.h)
		}
		fmt.Printf("%4d: %6.2f %s %q\n", i, para.PdfRectangle, s2, truncate(text, 50))
	}
}

func (pl paraList) sortReadingOrder() {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("sortReadingOrder: paras=%d ===========x=============", len(pl))
	}
	if len(pl) <= 1 {
		return
	}
	pl.computeEBBoxes()
	sort.Slice(pl, func(i, j int) bool { return compareDepthThenLlx(pl[i], pl[j]) <= 0 })
}

func (pl paraList) eventNeighbours(evs []event) map[*textPara][]int {
	sort.Slice(evs, func(i, j int) bool {
		a, b := evs[i], evs[j]
		pa, pb := a.pos, b.pos
		if pa != pb {
			return pa < pb
		}
		if a.isStart != b.isStart {
			return a.isStart
		}
		return i < j
	})
	overlaps := make(map[int]intSet)
	active := make(intSet)
	for _, ev := range evs {
		if ev.isStart {
			overlaps[ev.idx] = make(intSet)
			for k := range active {
				if k != ev.idx {
					overlaps[ev.idx].add(k)
					overlaps[k].add(ev.idx)
				}
			}
			active.add(ev.idx)
		} else {
			active.del(ev.idx)
		}
	}
	out := map[*textPara][]int{}
	for idx, set := range overlaps {
		para := pl[idx]
		if len(set) == 0 {
			out[para] = nil
			continue
		}
		idxs := make([]int, len(set))
		n := 0
		for k := range set {
			idxs[n] = k
			n++
		}
		out[para] = idxs
	}
	return out
}

func (pl paraList) readBefore(llyIdx []int, i, j int) bool {
	a, b := pl[i], pl[j]
	if paraXOverlaps(a, b) && a.Lly > b.Lly {
		return true
	}
	if !(a.eBBox.Urx < b.eBBox.Llx) {
		return false
	}
	lo, hi := a.Lly, b.Lly
	if lo > hi {
		hi, lo = lo, hi
	}
	x0 := math.Max(a.eBBox.Llx, b.eBBox.Llx)
	x1 := math.Min(a.eBBox.Urx, b.eBBox.Urx)
	between := pl.llyRange(llyIdx, lo, hi)
	for _, k := range between {
		if k == i || k == j {
			continue
		}
		c := pl[k]
		if c.eBBox.Llx <= x1 && x0 <= c.eBBox.Urx {
			return false
		}
	}
	return true
}

func (pl paraList) topoOrder() []int {
	if debugTopoOrder {
		common.Log.Info("topoOrder:")
	}
	n := len(pl)
	visited := make([]bool, n)
	order := make([]int, 0, n)
	llyOrder := pl.llyOrdering()
	var visit func(i int)
	visit = func(i int) {
		visited[i] = true
		for j := 0; j < n; j++ {
			if !visited[j] {
				if pl.readBefore(llyOrder, i, j) {
					visit(j)
				}
			}
		}
		order = append(order, i)
	}
	for i := 0; i < n; i++ {
		if !visited[i] {
			visit(i)
		}
	}
	return reverseInts(order)
}

func (pl paraList) toTextMarks(ctx *textContext) []TextMark {
	offset := 0
	var marks []TextMark
	for i, para := range pl {
		if para.isEmpty {
			continue
		}
		paraMarks := para.toTextMarks(&offset, ctx)
		marks = append(marks, paraMarks...)
		if i != len(pl)-1 {
			if paraDepthEqual(para, pl[i+1]) {
				marks = appendMetaMark(marks, &offset, " ")
			} else {
				marks = appendMetaMark(marks, &offset, "\n")
				marks = appendMetaMark(marks, &offset, "\n")
			}
		}
	}
	marks = appendMetaMark(marks, &offset, "\n")
	marks = appendMetaMark(marks, &offset, "\n")
	return marks
}

func (pl paraList) llyOrdering() []int {
	order := make([]int, len(pl))
	for i := range pl {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		return pl[a].Lly < pl[b].Lly
	})
	return order
}

func (pl paraList) sortTopoOrder() { order := pl.topoOrder(); pl.reorder(order) }

func (pl paraList) writeText(writer io.Writer, ctx *textContext) {
	for i, para := range pl {
		if para.isEmpty {
			continue
		}
		para.writeText(writer, ctx)
		if i != len(pl)-1 {
			if paraDepthEqual(para, pl[i+1]) {
				writer.Write([]byte(" "))
			} else {
				writer.Write([]byte("\n"))
				writer.Write([]byte("\n"))
			}
		}
	}
	writer.Write([]byte("\n"))
	writer.Write([]byte("\n"))
}

func (pl paraList) addNeighbours() {
	splitLR := func(idxs []int, para *textPara) ([]*textPara, []*textPara) {
		left := make([]*textPara, 0, len(idxs)-1)
		right := make([]*textPara, 0, len(idxs)-1)
		for _, idx := range idxs {
			pl2 := pl[idx]
			if pl2.Urx <= para.Llx {
				left = append(left, pl2)
			} else if pl2.Llx >= para.Urx {
				right = append(right, pl2)
			}
		}
		return left, right
	}
	splitUD := func(idxs []int, para *textPara) ([]*textPara, []*textPara) {
		above := make([]*textPara, 0, len(idxs)-1)
		below := make([]*textPara, 0, len(idxs)-1)
		for _, idx := range idxs {
			pl2 := pl[idx]
			if pl2.Ury <= para.Lly {
				below = append(below, pl2)
			} else if pl2.Lly >= para.Ury {
				above = append(above, pl2)
			}
		}
		return above, below
	}
	nbrs := pl.yNeighbours(yNeighbourTolerance)
	for _, para := range pl {
		idxs := nbrs[para]
		if len(idxs) == 0 {
			continue
		}
		left, right := splitLR(idxs, para)
		if len(left) == 0 && len(right) == 0 {
			continue
		}
		if len(left) > 0 {
			nearestLeft := left[0]
			for _, cand := range left[1:] {
				if cand.Urx >= nearestLeft.Urx {
					nearestLeft = cand
				}
			}
			for _, cand := range left {
				if cand != nearestLeft && cand.Urx > nearestLeft.Llx {
					nearestLeft = nil
					break
				}
			}
			if nearestLeft != nil && yOverlap(para.PdfRectangle, nearestLeft.PdfRectangle) {
				para.left = nearestLeft
			}
		}
		if len(right) > 0 {
			nearestRight := right[0]
			for _, cand := range right[1:] {
				if cand.Llx <= nearestRight.Llx {
					nearestRight = cand
				}
			}
			for _, cand := range right {
				if cand != nearestRight && cand.Llx < nearestRight.Urx {
					nearestRight = nil
					break
				}
			}
			if nearestRight != nil && yOverlap(para.PdfRectangle, nearestRight.PdfRectangle) {
				para.right = nearestRight
			}
		}
	}
	nbrs = pl.xNeighbours(xNeighbourTolerance)
	for _, para := range pl {
		idxs := nbrs[para]
		if len(idxs) == 0 {
			continue
		}
		above, below := splitUD(idxs, para)
		if len(above) == 0 && len(below) == 0 {
			continue
		}
		if len(below) > 0 {
			nearestBelow := below[0]
			for _, cand := range below[1:] {
				if cand.Ury >= nearestBelow.Ury {
					nearestBelow = cand
				}
			}
			for _, cand := range below {
				if cand != nearestBelow && cand.Ury > nearestBelow.Lly {
					nearestBelow = nil
					break
				}
			}
			if nearestBelow != nil && xOverlap(para.PdfRectangle, nearestBelow.PdfRectangle) {
				para.below = nearestBelow
			}
		}
		if len(above) > 0 {
			nearestAbove := above[0]
			for _, cand := range above[1:] {
				if cand.Lly <= nearestAbove.Lly {
					nearestAbove = cand
				}
			}
			for _, cand := range above {
				if cand != nearestAbove && cand.Lly < nearestAbove.Ury {
					nearestAbove = nil
					break
				}
			}
			if nearestAbove != nil && xOverlap(para.PdfRectangle, nearestAbove.PdfRectangle) {
				para.above = nearestAbove
			}
		}
	}
	for _, para := range pl {
		if para.left != nil && para.left.right != para {
			para.left = nil
		}
		if para.above != nil && para.above.below != para {
			para.above = nil
		}
		if para.right != nil && para.right.left != para {
			para.right = nil
		}
		if para.below != nil && para.below.above != para {
			para.below = nil
		}
	}
}

func (pl paraList) llyRange(idxs []int, lo, hi float64) []int {
	n := len(pl)
	if hi < pl[idxs[0]].Lly || lo > pl[idxs[n-1]].Lly {
		return nil
	}
	start := sort.Search(n, func(i int) bool { return pl[idxs[i]].Lly >= lo })
	end := sort.Search(n, func(i int) bool { return pl[idxs[i]].Lly > hi })
	return idxs[start:end]
}

func (pl paraList) findTextTables() []*textTable {
	var tables []*textTable
	for _, para := range pl {
		if para.taken() || para.Width() == 0 {
			continue
		}
		table := para.isAtom()
		if table == nil {
			continue
		}
		table.growTable()
		if table.w*table.h < minTableParas {
			continue
		}
		table.markCells()
		table.log("grown")
		tables = append(tables, table)
	}
	return tables
}

func (pl paraList) extractTables(tilings []gridTiling) paraList {
	if debugTables {
		common.Log.Debug("extractTables=%d ===========x=============", len(pl))
	}
	if len(pl) < minTableParas {
		return pl
	}
	tables := pl.findTables(tilings)
	if debugTables {
		common.Log.Info("combined tables %d ================", len(tables))
		for i, table := range tables {
			table.log(fmt.Sprintf("combined %d", i))
		}
	}
	return pl.applyTables(tables)
}

func (pl paraList) tables(ctx *textContext) []TextTable {
	var tables []TextTable
	if debugTables {
		common.Log.Info("paras.tables:")
	}
	for _, para := range pl {
		table := para.table
		if table != nil && table.isExportable() {
			tables = append(tables, table.toTextTable(ctx))
		}
	}
	return tables
}

func (pl paraList) findTables(tilings []gridTiling) []*textTable {
	pl.addNeighbours()
	sort.Slice(pl, func(n, n2 int) bool { return compareLlxThenDepth(pl[n], pl[n2]) < 0 })
	var tables []*textTable
	if useGridTables {
		gridTables := pl.findGridTables(tilings)
		tables = append(tables, gridTables...)
	}
	if useTextTables {
		textTables := pl.findTextTables()
		tables = append(tables, textTables...)
	}
	return tables
}

func (pl paraList) findTableGrid(tiling gridTiling) (*textTable, map[*textPara]struct{}) {
	numLlx := len(tiling.llx)
	numLly := len(tiling.lly)
	table := textTable{
		exportable: true, w: numLlx, h: numLly, cells: make(map[uint64]*textPara, numLlx*numLly), compositeCells: make(map[uint64]compositeCell, numLlx*numLly),
		PdfRectangle: tiling.PdfRectangle,
	}
	used := make(map[*textPara]struct{})
	maxEmpty := int((1.0 - maxEmptyFraction) * float64(numLlx*numLly))
	numEmpty := 0
	if debugGrids {
		common.Log.Info("findTableGrid: %d x %d", numLlx, numLly)
	}
	for row, lly := range tiling.lly {
		rowTiles, ok := tiling.tiles[lly]
		if !ok {
			continue
		}
		for col, llx := range tiling.llx {
			tile, ok2 := rowTiles[llx]
			if !ok2 {
				continue
			}
			paras := pl.inTile(tile)
			if len(paras) == 0 {
				numEmpty++
				if numEmpty > maxEmpty {
					if debugGrids {
						common.Log.Info("!numEmpty=%d", numEmpty)
					}
					return nil, nil
				}
			} else {
				table.putComposite(col, row, paras, tile.PdfRectangle)
				for _, para := range paras {
					used[para] = struct{}{}
				}
			}
		}
	}
	numHeader := 0
	for i := 0; i < numLlx; i++ {
		para := table.get(i, 0)
		if para == nil || !para.isEmpty {
			numHeader++
		}
	}
	if numHeader == 0 {
		if debugGrids {
			common.Log.Info("!numHeader=0")
		}
		return nil, nil
	}
	reduced := table.reduceTiling(tiling, reduceTilingTolerance)
	reduced = reduced.subdivide()
	return reduced, used
}

func (pl paraList) findGridTables(tilings []gridTiling) []*textTable {
	if debugTables {
		common.Log.Info("findGridTables: %d paras", len(pl))
		for i, para := range pl {
			fmt.Printf("%4d: %s\n", i, para)
		}
	}
	var tables []*textTable
	for i, tiling := range tilings {
		tableGrid, used := pl.findTableGrid(tiling)
		if tableGrid != nil {
			tableGrid.log(fmt.Sprintf("findTableWithGrids: %d", i))
			tables = append(tables, tableGrid)
			tableGrid.markCells()
		}
		for para := range used {
			para.isTaken = true
		}
	}
	if debugTables {
		common.Log.Info("findGridTables: %d tables", len(tables))
	}
	return tables
}

func (pl paraList) applyTables(tables []*textTable) paraList {
	var paras paraList
	for _, table := range tables {
		paras = append(paras, table.newTablePara())
	}
	for _, para := range pl {
		if para.isTaken {
			continue
		}
		paras = append(paras, para)
	}
	return paras
}

func paraXOverlaps(para, para2 *textPara) bool { return xOverlap(para.eBBox, para2.eBBox) }

func paraDepthEqual(a, b *textPara) bool {
	if a.isEmpty || b.isEmpty {
		return true
	}
	return isZero(a.depth() - b.depth())
}
