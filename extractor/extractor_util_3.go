package extractor

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func makeTextPage(marks []*textMark, pageSize model.PdfRectangle, rulings rulingList, tilings []gridTiling, noBreaks bool) paraList {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("makeTextPage: %d elements pageSize=%.2f", len(marks), pageSize)
	}
	if len(marks) == 0 {
		return nil
	}
	words := makeTextWords(marks, pageSize, false)
	if len(words) == 0 {
		return nil
	}
	rulings.log("makeTextPage")
	vertRulings, horzRulings := rulings.vertsHorzs()
	bag := makeWordBag(words, pageSize.Ury, vertRulings, horzRulings)
	bags := splitWordBag(bag, pageSize.Ury, vertRulings, horzRulings, noBreaks)
	bags = mergeWordBags(bags)
	paras := make(paraList, 0, len(bags))
	for _, bag2 := range bags {
		para := bag2.arrangeText(noBreaks)
		if para != nil {
			paras = append(paras, para)
		}
	}
	if len(paras) >= minTableParas {
		paras = paras.extractTables(tilings)
	}
	paras.sortReadingOrder()
	paras.sortTopoOrder()
	paras.log("sorted in reading order")
	return paras
}

func makeSimpleText(marks []*textMark, pageSize model.PdfRectangle) string {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("makeSimpleText: %d elements pageSize=%.2f", len(marks), pageSize)
	}
	out := ""
	if len(marks) == 0 {
		return out
	}
	words := makeTextWords(marks, pageSize, true)
	if len(words) == 0 {
		return out
	}
	prevDepth := 0.0
	first := true
	line := ""
	for _, word := range words {
		fontSize := word.fontSize
		if fontSize > minFontSize {
			fontSize = minFontSize
		}
		if (word.depth-prevDepth > lineMergeFactor*fontSize && prevDepth != 0.0) || (prevDepth-word.depth > fontSize*10) {
			bidi := reorderBidi([]rune(line))
			line = bidi.text
			line += "\n"
			out += line
			line = ""
		} else {
			if !first {
				line += " "
			}
		}
		line += word.text
		first = false
		prevDepth = word.depth
	}
	if line != "" {
		bidi := reorderBidi([]rune(line))
		line = bidi.text
		line += "\n"
		out += line
	}
	return out
}

func makeStrokeRulings(sections []pathSection) rulingList {
	granularize(sections)
	if debugRulings {
		common.Log.Info("makeStrokeRulings: %d strokes", len(sections))
	}
	var rulings rulingList
	for _, section := range sections {
		for _, sp := range section.subpaths {
			if len(sp.points) < 2 {
				continue
			}
			p := sp.points[0]
			for _, next := range sp.points[1:] {
				if r, ok := lineToRuling(p, next, section.Color); ok {
					rulings = append(rulings, r)
				}
				p = next
			}
		}
	}
	if debugRulings {
		common.Log.Info("makeStrokeRulings: %s", rulings)
	}
	return rulings
}

func makeFillRulings(sections []pathSection) rulingList {
	granularize(sections)
	if debugRulings {
		common.Log.Info("makeFillRulings: %d fills", len(sections))
	}
	var rulings rulingList
	for _, section := range sections {
		for _, path := range section.subpaths {
			if !path.isQuadrilateral() {
				if debugRulings {
					common.Log.Error("!isQuadrilateral: %s", path)
				}
				continue
			}
			if r, ok := path.makeRectRuling(section.Color); ok {
				rulings = append(rulings, r)
			} else {
				if debugRectRulings {
					common.Log.Error("!makeRectRuling: %s", path)
				}
			}
		}
	}
	if debugRulings {
		common.Log.Info("makeFillRulings: %s", rulings.String())
	}
	return rulings
}

func compareDepthThenLlx(a, b bounded) float64 {
	d := compareDepth(a, b)
	if !isZero(d) {
		return d
	}
	return compareLlx(a, b)
}

func compareLlxThenDepth(a, b bounded) float64 {
	d := compareLlx(a, b)
	if !isZero(d) {
		return d
	}
	return compareDepth(a, b)
}

func compareDepth(a, b bounded) float64 { return bboxDepth(a) - bboxDepth(b) }

func compareLlx(a, b bounded) float64 { return a.bbox().Llx - b.bbox().Llx }

func horizontalGap(a, b bounded) float64 { return a.bbox().Llx - b.bbox().Urx }

func depthFromTop(rect model.PdfRectangle, b bounded) float64 { return rect.Ury - b.bbox().Lly }

func depthBinIndex(depth float64) int {
	var idx int
	if depth >= 0 {
		idx = int(depth / depthBinWidth)
	} else {
		idx = int(depth/depthBinWidth) - 1
	}
	return idx
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func yOverlap(a, b model.PdfRectangle) bool {
	return a.Lly <= b.Ury && b.Lly <= a.Ury
}

func spacesForGap(gap, charW float64) string {
	if gap <= 0 {
		return " "
	}
	n := min(max(int(gap/charW), 1), maxGapSpaces)
	return strings.Repeat(" ", n)
}

func spacesForOffset(offset, unit float64) string {
	if offset <= unit {
		return ""
	}
	n := min(int(offset/unit+0.5), maxOffsetSpaces)
	return strings.Repeat(" ", n)
}

func xOverlap(a, b model.PdfRectangle) bool {
	return b.Llx <= a.Urx && a.Llx <= b.Urx
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

func nextLineDepth(line *textLine, lines []*textLine) float64 {
	var next float64 = -1
	for _, l := range lines {
		if l.depth > line.depth {
			if math.Round(l.Llx) >= math.Round(line.Llx) {
				next = l.depth
			} else {
				break
			}
		}
	}
	return next
}

func withGap(test func(*wordBag, *textWord, float64) bool, gap float64) func(*wordBag, *textWord) bool {
	return func(bag *wordBag, w *textWord) bool { return test(bag, w, gap) }
}

func bboxDepth(b bounded) float64 { return -b.bbox().Lly }

func kindFromDeltas(dx, dy, minLen float64) rulingKind {
	if dx >= minLen && isRatioSmall(dy, dx) {
		return rulingHorz
	}
	if dy >= minLen && isRatioSmall(dx, dy) {
		return rulingVert
	}
	return rulingNone
}

func cellKey(col, row int) uint64 { return uint64(col)*0x1000000 + uint64(row) }

func dedupeGridWords(words []gridWord) []gridWord {
	if len(words) < 2 {
		return words
	}
	byY := make(map[int][]int)
	for i, w := range words {
		key := int(w.ury * 10)
		byY[key] = append(byY[key], i)
	}
	dup := make([]bool, len(words))
	for i, w := range words {
		if dup[i] {
			continue
		}
		runes := utf8.RuneCountInString(w.text)
		if runes == 0 || runes > maxDedupeRunes {
			continue
		}
		yKey := int(w.ury * 10)
		span := int(dupPositionTolerance*10) + 1
		for d := -span; d <= span; d++ {
			cands := byY[yKey+d]
			for _, j := range cands {
				if i == j || dup[j] {
					continue
				}
				other := words[j]
				if utf8.RuneCountInString(other.text) <= runes {
					continue
				}
				if strings.Contains(other.text, w.text) && math.Abs(w.ury-other.ury) <= dupPositionTolerance && w.llx >= other.llx-dupPositionTolerance && w.urx <= other.urx+dupPositionTolerance {
					dup[i] = true
					break
				}
			}
			if dup[i] {
				break
			}
		}
	}
	anyDup := false
	for _, f := range dup {
		if f {
			anyDup = true
			break
		}
	}
	if !anyDup {
		return words
	}
	out := make([]gridWord, 0, len(words))
	for i, w := range words {
		if !dup[i] {
			out = append(out, w)
		}
	}
	return out
}

func logGrids(msg string, grids []rulingList) {
	common.Log.Info("$$ %d grids - %s", len(grids), msg)
	for i, grid := range grids {
		fmt.Printf("%4d: %s\n", i, grid.String())
	}
}

func tileRowKeys(m map[float64]map[float64]gridTile) []float64 {
	keys := make([]float64, 0, len(m))
	for v := range m {
		keys = append(keys, v)
	}
	sort.Float64s(keys)
	n := len(keys)
	for i := 0; i < n/2; i++ {
		keys[i], keys[n-1-i] = keys[n-1-i], keys[i]
	}
	return keys
}

func tileColumnKeys(m map[float64]map[float64]gridTile) []float64 {
	keys := make([]float64, 0, len(m))
	seen := make(map[float64]struct{}, len(m))
	for _, row := range m {
		for v := range row {
			if _, ok := seen[v]; ok {
				continue
			}
			keys = append(keys, v)
			seen[v] = struct{}{}
		}
	}
	sort.Float64s(keys)
	return keys
}

func refineGridRows(rows []gridRow, charWidth float64) []gridRow {
	var out []gridRow
	for _, row := range rows {
		if len(row.words) < 4 {
			out = append(out, row)
			continue
		}
		sort.Slice(row.words, func(n, n2 int) bool { return row.words[n].llx < row.words[n2].llx })
		maxGap := 0.0
		splitIdx := -1
		for i := 1; i < len(row.words); i++ {
			gap := row.words[i].llx - row.words[i-1].urx
			if gap > maxGap {
				maxGap = gap
				splitIdx = i
			}
		}
		if maxGap < charWidth*splitGapFactor || splitIdx <= 0 {
			out = append(out, row)
			continue
		}
		yLeft, yRight := 0.0, 0.0
		for _, word := range row.words[:splitIdx] {
			yLeft += word.ury
		}
		yLeft /= float64(splitIdx)
		for _, word := range row.words[splitIdx:] {
			yRight += word.ury
		}
		yRight /= float64(len(row.words) - splitIdx)
		dy := math.Abs(yLeft - yRight)
		if dy < charWidth*mergeGapFactor {
			out = append(out, row)
			continue
		}
		left := make([]gridWord, splitIdx)
		copy(left, row.words[:splitIdx])
		right := make([]gridWord, len(row.words)-splitIdx)
		copy(right, row.words[splitIdx:])
		out = append(out, gridRow{words: left, y: yLeft})
		out = append(out, gridRow{words: right, y: yRight})
	}
	return out
}

func rowCorridors(cells []compositeCell) []float64 {
	var lines []*textLine
	numParas := 0
	for _, cell := range cells {
		numParas += len(cell.paraList)
		lines = append(lines, cell.lines()...)
	}
	sort.Slice(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		da, db := a.depth, b.depth
		if !isZero(da - db) {
			return da < db
		}
		return a.Llx < b.Llx
	})
	if debugTables {
		fmt.Printf("    rowBorders: %d paras %d lines\n", numParas, len(lines))
		for i, l := range lines {
			fmt.Printf("%8d: %s\n", i, l)
		}
	}
	var corridors []float64
	lowest := lines[0]
	var groups [][]*textLine
	group := []*textLine{lowest}
	for i, line := range lines[1:] {
		if line.Ury < lowest.Lly {
			y := 0.5 * (line.Ury + lowest.Lly)
			if debugTables {
				fmt.Printf("%4d: %6.2f < %6.2f border=%6.2f\n"+"\t q=%s\n\t p=%s\n", i, line.Ury, lowest.Lly, y, lowest, line)
			}
			corridors = append(corridors, y)
			groups = append(groups, group)
			group = nil
		}
		group = append(group, line)
		if line.Lly < lowest.Lly {
			lowest = line
		}
	}
	if len(group) > 0 {
		groups = append(groups, group)
	}
	if debugTables {
		fmt.Printf("        rowCorridors=%6.2f\n", corridors)
	}
	if debugTables {
		common.Log.Info("row=%d", len(cells))
		for i, c := range cells {
			fmt.Printf("%4d: %s\n", i, c)
		}
		common.Log.Info("groups=%d", len(groups))
		for i, g := range groups {
			fmt.Printf("%4d: %d\n", i, len(g))
			for j, l := range g {
				fmt.Printf("%8d: %s\n", j, l)
			}
		}
	}
	allSpan := true
	for i, g := range groups {
		spans := true
		for j, c := range cells {
			if debugTables {
				fmt.Printf("   ~~~group %d of %d cell %d of %d %s\n", i, len(groups), j, len(cells), c)
			}
			if !c.hasLines(g) {
				if debugTables {
					fmt.Printf("   !!!group %d of %d cell %d of %d OUT\n", i, len(groups), j, len(cells))
				}
				spans = false
				break
			}
		}
		if !spans {
			allSpan = false
			break
		}
	}
	if !allSpan {
		if debugTables {
			common.Log.Info("row corridors don't span all cells in row. ignoring")
		}
		corridors = nil
	}
	if debugTables && corridors != nil {
		fmt.Printf("        ***rowCorridors=%6.2f\n", corridors)
	}
	return corridors
}

func rowSpacing(rows []gridRow, charWidth float64) float64 {
	if len(rows) < 3 {
		return charWidth * defaultRowSpacingFactor
	}
	gaps := make([]float64, 0, len(rows))
	for i := 0; i+1 < len(rows); i++ {
		if gap := rows[i].y - rows[i+1].y; gap > 0 {
			gaps = append(gaps, gap)
		}
	}
	if len(gaps) < 3 {
		return charWidth * defaultRowSpacingFactor
	}
	sort.Float64s(gaps)
	return gaps[len(gaps)/2] * medianSpacingFactor
}

func corridorOffsets(n int, corridors map[int][]float64) ([]int, int) {
	offsets := make([]int, n)
	total := 0
	for i := 0; i < n; i++ {
		offsets[i] = total
		total += len(corridors[i]) + 1
	}
	return offsets, total
}

func attachTable(marks []TextMark, table *TextTable) []TextMark {
	var out []TextMark
	for _, mark := range marks {
		mark.hasTable = true
		mark.table = table
		out = append(out, mark)
	}
	return out
}

func fixCells(corridors map[int][]float64) {
	if len(corridors) <= 1 {
		return
	}
	keys := sortedIntKeys(corridors)
	if debugTables {
		common.Log.Info("fixCells: keys=%+v", keys)
	}
	var start, prevKey int
	for start, prevKey = range keys {
		if corridors[prevKey] != nil {
			break
		}
	}
	for i, key := range keys[start:] {
		cur := corridors[key]
		if cur == nil {
			continue
		}
		if debugTables {
			fmt.Printf("%4d: k0=%d k1=%d\n", start+i, prevKey, key)
		}
		prev := corridors[key]
		if prev[len(prev)-1] > cur[0] {
			prev[len(prev)-1] = cur[0]
			corridors[prevKey] = prev
		}
		prevKey = key
	}
}

func corridorsString(corridors map[int][]float64) string {
	idxs := sortedIntKeys(corridors)
	parts := make([]string, len(corridors))
	for i, idx := range idxs {
		parts[i] = fmt.Sprintf("%d: %.2f", idx, corridors[idx])
	}
	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

func containsFont(fonts []Font, name string) bool {
	for _, font := range fonts {
		if font.FontName == name {
			return true
		}
	}
	return false
}

func setStringFromText(str *core.PdfObjectString, text string, font *model.PdfFont) {
	codes, numMissing := font.StringToCharcodeBytes(text)
	if numMissing != 0 {
		common.Log.Debug("WARN: some runes could not be encoded.\n\t%s -> %v", text, codes)
	}
	pdfStr := core.MakeStringFromBytes(codes)
	*str = *pdfStr
}

func appendMarkWithOffset(marks []TextMark, offset *int, mark TextMark) []TextMark {
	mark.Offset = *offset
	marks = append(marks, mark)
	*offset += len(mark.Text)
	return marks
}

func appendMetaMark(marks []TextMark, offset *int, text string) []TextMark {
	mark := metaMark
	mark.Text = text
	return appendMarkWithOffset(marks, offset, mark)
}

func singleFloatOperand(op *contentstream.ContentStreamOperation) (float64, error) {
	if len(op.Params) != 1 {
		err := errors.New("incorrect parameter count")
		common.Log.Debug("ERROR: %#q should have %d input params, got %d %+v", op.Operand, 1, len(op.Params), op.Params)
		return 0.0, err
	}
	return core.GetNumberAsFloat(op.Params[0])
}

func fillStrings(strs []string, from int, to int, s string) {
	for i := from; i < to; i++ {
		strs[i] = s
	}
}

var metaMark = TextMark{Text: "[X]", Original: " ", Meta: true, FillColor: color.White, StrokeColor: color.White}

func twoFloatOperands(objs []core.PdfObject) (x, x2 float64, err error) {
	if len(objs) != 2 {
		return 0, 0, fmt.Errorf("invalid number of params: %d", len(objs))
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objs)
	if err != nil {
		return 0, 0, err
	}
	return numbersAsFloat[0], numbersAsFloat[1], nil
}

func translationPoint(m transform.Matrix) transform.Point {
	tx, ty := m.Translation()
	return transform.Point{X: tx, Y: ty}
}

func mergeWordBags(bags []*wordBag) []*wordBag {
	if len(bags) <= 1 {
		return bags
	}
	if debugWordBags {
		common.Log.Info("mergeWordBags:")
	}
	sort.Slice(bags, func(i, j int) bool {
		a, b := bags[i], bags[j]
		areaA := a.Width() * a.Height()
		areaB := b.Width() * b.Height()
		if areaA != areaB {
			return areaA > areaB
		}
		if a.Height() != b.Height() {
			return a.Height() > b.Height()
		}
		return i < j
	})
	var merged []*wordBag
	absorbed := make(intSet)
	for i := 0; i < len(bags); i++ {
		if absorbed.has(i) {
			continue
		}
		big := bags[i]
		for j := i + 1; j < len(bags); j++ {
			if absorbed.has(i) {
				continue
			}
			small := bags[j]
			expanded := big.PdfRectangle
			expanded.Llx -= big.maxFontSize
			if rectContains(expanded, small.PdfRectangle) {
				big.absorb(small)
				absorbed.add(j)
			}
		}
		merged = append(merged, big)
	}
	if len(bags) != len(merged)+len(absorbed) {
		common.Log.Error("mergeWordBags: %d→%d absorbed=%d", len(bags), len(merged), len(absorbed))
	}
	return merged
}

func medianCharWidth(words []*textWord) float64 {
	widths := make([]float64, 0, len(words))
	for _, word := range words {
		numRunes := utf8.RuneCountInString(word.text)
		width := word.Urx - word.Llx
		if numRunes > 0 && width > 0 {
			widths = append(widths, width/float64(numRunes))
		}
	}
	if len(widths) == 0 {
		return 0
	}
	sort.Float64s(widths)
	return widths[len(widths)/2]
}

func sortLinesByDepth(lines []*textLine) {
	sort.Slice(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		return a.depth < b.depth
	})
}

func separatorFor(d1, d2 float64) string {
	newLine := !isZero(d1 - d2)
	if newLine {
		return "\n"
	}
	return " "
}
