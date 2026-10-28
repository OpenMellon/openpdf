package extractor

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func isAdjacentRight(bag *wordBag, word *textWord, gap float64) bool {
	return bag.Urx <= word.Llx && word.Llx < bag.Urx+gap
}

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

func (tp *textPara) toTextMarks(offset *int, ctx *textContext) []TextMark {
	if tp.table == nil {
		return tp.toCellTextMarks(offset, ctx)
	}
	var marks []TextMark
	for row := 0; row < tp.table.h; row++ {
		for col := 0; col < tp.table.w; col++ {
			para := tp.table.get(col, row)
			if para == nil {
				marks = appendMetaMark(marks, offset, "\t")
			} else {
				cellMarks := para.toCellTextMarks(offset, ctx)
				marks = append(marks, cellMarks...)
			}
			marks = appendMetaMark(marks, offset, " ")
		}
		if row < tp.table.h-1 {
			marks = appendMetaMark(marks, offset, "\n")
		}
	}
	table := tp.table
	if table.isExportable() {
		tt := table.toTextTable(ctx)
		marks = attachTable(marks, &tt)
	}
	return marks
}

func newTextPara(rect model.PdfRectangle, lines []*textLine) *textPara {
	return &textPara{PdfRectangle: rect, lines: lines}
}
func (tp *textPara) bbox() model.PdfRectangle { return tp.PdfRectangle }

func compareDepthThenLlx(a, b bounded) float64 {
	d := compareDepth(a, b)
	if !isZero(d) {
		return d
	}
	return compareLlx(a, b)
}

func (pl paraList) lines() []*textLine {
	var lines []*textLine
	for _, para := range pl {
		lines = append(lines, para.lines...)
	}
	return lines
}

func (tp *textPara) writeCellText(writer io.Writer, ctx *textContext) {
	for i, line := range tp.lines {
		text := line.text()
		dehyphenate := !ctx.isDehyphenationDisabled() && line.endsInHyphen() && i != len(tp.lines)-1
		if dehyphenate {
			text = dropLastRune(text)
		}
		writer.Write([]byte(text))
		if !(dehyphenate || i == len(tp.lines)-1) {
			writer.Write([]byte(separatorFor(line.depth, tp.lines[i+1].depth)))
		}
	}
}

func (tp *textPara) writeText(writer io.Writer, ctx *textContext) {
	if tp.table == nil {
		tp.writeCellText(writer, ctx)
		return
	}
	for row := 0; row < tp.table.h; row++ {
		for col := 0; col < tp.table.w; col++ {
			para := tp.table.get(col, row)
			if para == nil {
				writer.Write([]byte("\t"))
			} else {
				sortLinesByDepth(para.lines)
				para.writeCellText(writer, ctx)
			}
			writer.Write([]byte(" "))
		}
		if row < tp.table.h-1 {
			writer.Write([]byte("\n"))
		}
	}
}

func compareLlxThenDepth(a, b bounded) float64 {
	d := compareLlx(a, b)
	if !isZero(d) {
		return d
	}
	return compareDepth(a, b)
}

func (tp *textPara) isAtom() *textTable {
	topLeft := tp
	right := tp.right
	below := tp.below
	if right.taken() || below.taken() {
		return nil
	}
	diag := right.below
	if diag.taken() || diag != below.right {
		return nil
	}
	return newTwoByTwoTable(topLeft, right, below, diag)
}
func isRatioSmall(num, den float64) bool { return num/math.Max(slopeEpsilon, den) < maxSlope }

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

func horizontalGap(a, b bounded) float64 { return a.bbox().Llx - b.bbox().Urx }

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
func depthFromTop(rect model.PdfRectangle, b bounded) float64 { return rect.Ury - b.bbox().Lly }

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
func (tp *textPara) taken() bool { return tp == nil || tp.isTaken }

func (pl paraList) reorder(idxs []int) {
	paras := make(paraList, len(pl))
	for i, idx := range idxs {
		paras[i] = pl[idx]
	}
	copy(pl, paras)
}

func overlapsHorizontally(bag *wordBag, word *textWord, gap float64) bool {
	return word.Llx < bag.Urx+gap && bag.Llx-gap < word.Urx
}

func minLlx(words []gridWord) float64 {
	minX := words[0].llx
	for _, word := range words[1:] {
		if word.llx < minX {
			minX = word.llx
		}
	}
	return minX
}

type textPara struct {
	model.PdfRectangle
	eBBox   model.PdfRectangle
	lines   []*textLine
	table   *textTable
	isTaken bool
	isEmpty bool
	left    *textPara
	right   *textPara
	above   *textPara
	below   *textPara
	lists   []list
}

func compareDepth(a, b bounded) float64 { return bboxDepth(a) - bboxDepth(b) }

func yOverlap(a, b model.PdfRectangle) bool {
	return a.Lly <= b.Ury && b.Lly <= a.Ury
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

type (
	paraList    []*textPara
	patternWalk struct {
		stream *core.PdfObjectStream
		ctm    transform.Matrix
	}
)

func compareLlx(a, b bounded) float64 { return a.bbox().Llx - b.bbox().Llx }

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

func spacesForGap(gap, charW float64) string {
	if gap <= 0 {
		return " "
	}
	n := min(max(int(gap/charW), 1), maxGapSpaces)
	return strings.Repeat(" ", n)
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
func paraXOverlaps(para, para2 *textPara) bool { return xOverlap(para.eBBox, para2.eBBox) }

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

func xOverlap(a, b model.PdfRectangle) bool {
	return b.Llx <= a.Urx && a.Llx <= b.Urx
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
func (tp *textPara) fontsize() float64 { return tp.lines[0].fontSize }
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

// String returns a description of `p`.
func (tp *textPara) String() string {
	if tp.isEmpty {
		return fmt.Sprintf("%6.2f [EMPTY]", tp.PdfRectangle)
	}
	dims := ""
	if tp.table != nil {
		dims = fmt.Sprintf("[%dx%d] ", tp.table.w, tp.table.h)
	}
	return fmt.Sprintf("%6.2f %s%d lines %q", tp.PdfRectangle, dims, len(tp.lines), truncate(tp.text(), 50))
}

func buildChildLists(lines []*textLine, m map[float64][]*textLine, depths []float64, level int, parentDepth, nextDepth float64) []*list {
	items := []*list{}
	lvl := level
	level = level + 1
	d := depths[lvl]
	levelLines := m[d]
	cands := linesBetweenDepths(levelLines, nextDepth, parentDepth)
	for i, line := range cands {
		var limit float64
		subItems := []*list{}
		depth := line.depth
		next := nextDepth
		if i < len(cands)-1 {
			next = cands[i+1].depth
		}
		if level < len(depths) {
			subItems = buildChildLists(lines, m, depths, level, depth, next)
		}
		limit = next
		if len(subItems) > 0 {
			subItem := subItems[0]
			if len(subItem.lines) > 0 {
				limit = subItem.lines[0].depth
			}
		}
		itemLines := []*textLine{line}
		cont := bulletContinuationLines(line, lines, depth, limit)
		itemLines = append(itemLines, cont...)
		item := newList(itemLines, "bullet", subItems)
		item.text = joinLinesText(itemLines, "")
		items = append(items, item)
	}
	return items
}

func nextIndexNot(strs []string, from int, val string) int {
	i := from
	for ; i < len(strs); i++ {
		if strs[i] != val {
			return i
		}
	}
	return i
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

func paraDepthEqual(a, b *textPara) bool {
	if a.isEmpty || b.isEmpty {
		return true
	}
	return isZero(a.depth() - b.depth())
}
func lineStartX(line *textLine) float64 { return line.words[0].Llx }

func withGap(test func(*wordBag, *textWord, float64) bool, gap float64) func(*wordBag, *textWord) bool {
	return func(bag *wordBag, w *textWord) bool { return test(bag, w, gap) }
}

func (tp *textPara) getListLines() []*textLine {
	var lines []*textLine
	bulletLines := linesWithBullets(tp.lines)
	for _, line := range tp.lines {
		b := line.words[0].text[0]
		if startsWithBulletByte(b) {
			lines = append(lines, line)
		}
	}
	lines = append(lines, bulletLines...)
	return lines
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
func bboxDepth(b bounded) float64 { return -b.bbox().Lly }

func (tp *textPara) text() string {
	buf := new(bytes.Buffer)
	tp.writeText(buf, &textContext{})
	return buf.String()
}

type bounded interface{ bbox() model.PdfRectangle }

func spacesForOffset(offset, unit float64) string {
	if offset <= unit {
		return ""
	}
	n := min(int(offset/unit+0.5), maxOffsetSpaces)
	return strings.Repeat(" ", n)
}

type event struct {
	pos     float64
	isStart bool
	idx     int
}

func (tp *textPara) depth() float64 {
	if tp.isEmpty {
		return -1.0
	}
	if len(tp.lines) > 0 {
		return tp.lines[0].depth
	}
	return tp.table.depth()
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

func (tp *textPara) toCellTextMarks(offset *int, ctx *textContext) []TextMark {
	var marks []TextMark
	for i, line := range tp.lines {
		lineMarks := line.toTextMarks(offset)
		dehyphenate := !ctx.isDehyphenationDisabled() && line.endsInHyphen() && i != len(tp.lines)-1
		if dehyphenate {
			lineMarks = trimLastMarkRune(lineMarks, offset)
		}
		marks = append(marks, lineMarks...)
		if !(dehyphenate || i == len(tp.lines)-1) {
			marks = appendMetaMark(marks, offset, separatorFor(line.depth, tp.lines[i+1].depth))
		}
	}
	return marks
}
