package extractor

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

func (tt *textTable) reduce() *textTable {
	rows := make([]int, 0, tt.h)
	cols := make([]int, 0, tt.w)
	for i := 0; i < tt.h; i++ {
		if !tt.emptyCompositeRow(i) {
			rows = append(rows, i)
		}
	}
	for i := 0; i < tt.w; i++ {
		if !tt.emptyCompositeColumn(i) {
			cols = append(cols, i)
		}
	}
	if len(rows) == tt.h && len(cols) == tt.w {
		return tt
	}
	reduced := textTable{exportable: tt.exportable, w: len(cols), h: len(rows), cells: make(map[uint64]*textPara, len(cols)*len(rows))}
	if debugTables {
		common.Log.Info("reduce: %dx%d -> %dx%d", tt.w, tt.h, len(cols), len(rows))
		common.Log.Info("reducedCols: %+v", cols)
		common.Log.Info("reducedRows: %+v", rows)
	}
	for newRow, row := range rows {
		for newCol, col := range cols {
			paras, rect := tt.getComposite(col, row)
			if paras == nil {
				continue
			}
			if debugTables {
				fmt.Printf("  %2d, %2d (%2d, %2d) %q\n", newCol, newRow, col, row, truncate(paras.merge().text(), 50))
			}
			reduced.putComposite(newCol, newRow, paras, rect)
		}
	}
	return &reduced
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

// TextTable represents a table.
// Cells are ordered top-to-bottom, left-to-right.
// Cells[y] is the (0-offset) y'th row in the table.
// Cells[y][x] is the (0-offset) x'th column in the table.
type TextTable struct {
	model.PdfRectangle
	W, H  int
	Cells [][]TableCell
}

func (tt *textTable) get(col, row int) *textPara {
	return tt.cells[cellKey(col, row)]
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

func corridorOffsets(n int, corridors map[int][]float64) ([]int, int) {
	offsets := make([]int, n)
	total := 0
	for i := 0; i < n; i++ {
		offsets[i] = total
		total += len(corridors[i]) + 1
	}
	return offsets, total
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

func (tt *textTable) put(col, row int, para *textPara) {
	tt.cells[cellKey(col, row)] = para
}

func (tt *textTable) isExportable() bool {
	if tt.exportable {
		return true
	}
	isSparse := func(row int) bool {
		para := tt.get(0, row)
		if para == nil {
			return false
		}
		text := para.text()
		numRunes := utf8.RuneCountInString(text)
		isNum := bareNumberRegexp.MatchString(text)
		return numRunes <= 1 || isNum
	}
	for i := 0; i < tt.h; i++ {
		if !isSparse(i) {
			return true
		}
	}
	return false
}

func (tt *textTable) subdivide() *textTable {
	tt.logComposite("subdivide")
	rowCorr := tt.compositeRowCorridors()
	colCorr := tt.compositeColCorridors()
	if debugTables {
		common.Log.Info("subdivide:\n\trowCorridors=%s\n\tcolCorridors=%s", corridorsString(rowCorr), corridorsString(colCorr))
	}
	if len(rowCorr) == 0 || len(colCorr) == 0 {
		return tt
	}
	fixCells(rowCorr)
	fixCells(colCorr)
	if debugTables {
		common.Log.Info("subdivide fixed:\n\trowCorridors=%s\n\tcolCorridors=%s", corridorsString(rowCorr), corridorsString(colCorr))
	}
	rowOff, totalRows := corridorOffsets(tt.h, rowCorr)
	colOff, totalCols := corridorOffsets(tt.w, colCorr)
	cells := make(map[uint64]*textPara, totalCols*totalRows)
	out := &textTable{PdfRectangle: tt.PdfRectangle, exportable: tt.exportable, h: totalRows, w: totalCols, cells: cells}
	if debugTables {
		common.Log.Info("subdivide: composite = %d x %d cells= %d x %d\n"+"\trowCorridors=%s\n"+"\tcolCorridors=%s\n"+"\tyOffsets=%+v\n"+"\txOffsets=%+v", tt.w, tt.h, totalCols, totalRows, corridorsString(rowCorr), corridorsString(colCorr), rowOff, colOff)
	}
	for row := 0; row < tt.h; row++ {
		r0 := rowOff[row]
		for col := 0; col < tt.w; col++ {
			c0 := colOff[col]
			if debugTables {
				fmt.Printf("%6d, %2d: x0=%d y0=%d\n", col, row, c0, r0)
			}
			cell, ok := tt.compositeCells[cellKey(col, row)]
			if !ok {
				continue
			}
			sub := cell.split(rowCorr[row], colCorr[col])
			for subRow := 0; subRow < sub.h; subRow++ {
				for subCol := 0; subCol < sub.w; subCol++ {
					para := sub.get(subCol, subRow)
					out.put(c0+subCol, r0+subRow, para)
					if debugTables {
						fmt.Printf("%8d, %2d: %s\n", c0+subCol, r0+subRow, para)
					}
				}
			}
		}
	}
	return out
}

func newTwoByTwoTable(tl, tr, bl, br *textPara) *textTable {
	table := &textTable{w: 2, h: 2, cells: make(map[uint64]*textPara, 4)}
	table.put(0, 0, tl)
	table.put(1, 0, tr)
	table.put(0, 1, bl)
	table.put(1, 1, br)
	return table
}

func (tt *textTable) log(msg string) {
	if !debugTables {
		return
	}
	common.Log.Info("~~~ %s: %d x %d grid=%t\n      %6.2f", msg, tt.w, tt.h, tt.exportable, tt.PdfRectangle)
	for i := 0; i < tt.h; i++ {
		for j := 0; j < tt.w; j++ {
			para := tt.get(j, i)
			if para == nil {
				continue
			}
			fmt.Printf("%4d %2d: %6.2f %q %d\n", j, i, para.PdfRectangle, truncate(para.text(), 50), utf8.RuneCountInString(para.text()))
		}
	}
}

func (tt *textTable) growTable() {
	addRow := func(row paraList) {
		tt.h++
		for i := 0; i < tt.w; i++ {
			row2 := row[i]
			tt.put(i, tt.h-1, row2)
		}
	}
	addCol := func(col paraList) {
		tt.w++
		for i := 0; i < tt.h; i++ {
			col2 := col[i]
			tt.put(tt.w-1, i, col2)
		}
	}
	if debugGrowTable {
		tt.log("growTable")
	}
	for i := 0; ; i++ {
		grew := false
		down := tt.getDown()
		right := tt.getRight()
		if debugGrowTable {
			fmt.Printf("%4d: %s\n", i, tt)
			fmt.Printf("        down=%s\n", down)
			fmt.Printf("       right=%s\n", right)
		}
		if down != nil && right != nil {
			corner := down[len(down)-1]
			if !corner.taken() && corner == right[len(right)-1] {
				addRow(down)
				if right = tt.getRight(); right != nil {
					addCol(right)
					tt.put(tt.w-1, tt.h-1, corner)
				}
				grew = true
			}
		}
		if !grew && down != nil {
			addRow(down)
			grew = true
		}
		if !grew && right != nil {
			addCol(right)
			grew = true
		}
		if !grew {
			break
		}
	}
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

func (tt *textTable) getDown() paraList {
	paras := make(paraList, tt.w)
	for i := 0; i < tt.w; i++ {
		para := tt.get(i, tt.h-1).below
		if para.taken() {
			return nil
		}
		paras[i] = para
	}
	for i := 0; i < tt.w-1; i++ {
		if paras[i].right != paras[i+1] {
			return nil
		}
	}
	return paras
}

func (tt *textTable) depth() float64 {
	minDepth := 1e10
	for i := 0; i < tt.w; i++ {
		para := tt.get(i, 0)
		if para == nil || para.isEmpty {
			continue
		}
		minDepth = math.Min(minDepth, para.depth())
	}
	return minDepth
}

// TableCell is a cell in a TextTable.
type TableCell struct {
	model.PdfRectangle

	// Text is the extracted text.
	Text string

	// Marks returns the TextMarks corresponding to the text in Text.
	Marks TextMarkArray
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

func (tt *textTable) getRight() paraList {
	paras := make(paraList, tt.h)
	for i := 0; i < tt.h; i++ {
		para := tt.get(tt.w-1, i).right
		if para.taken() {
			return nil
		}
		paras[i] = para
	}
	for i := 0; i < tt.h-1; i++ {
		if paras[i].below != paras[i+1] {
			return nil
		}
	}
	return paras
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
func (tt *textTable) bbox() model.PdfRectangle { return tt.PdfRectangle }

func (tt *textTable) computeBbox() model.PdfRectangle {
	var rect model.PdfRectangle
	set := false
	for i := 0; i < tt.h; i++ {
		for j := 0; j < tt.w; j++ {
			para := tt.get(j, i)
			if para == nil {
				continue
			}
			if !set {
				rect = para.PdfRectangle
				set = true
			} else {
				rect = unionRects(rect, para.PdfRectangle)
			}
		}
	}
	return rect
}

// String returns a description of `t`.
func (tt *textTable) String() string {
	return fmt.Sprintf("%d x %d %t", tt.w, tt.h, tt.exportable)
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

func (tt *textTable) markCells() {
	for row := 0; row < tt.h; row++ {
		for col := 0; col < tt.w; col++ {
			para := tt.get(col, row)
			if para != nil {
				para.isTaken = true
			}
		}
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

func (tt *textTable) reduceTiling(tiling gridTiling, tol float64) *textTable {
	rows := make([]int, 0, tt.h)
	cols := make([]int, 0, tt.w)
	llx := tiling.llx
	lly := tiling.lly
	for i := 0; i < tt.h; i++ {
		merge := i > 0 && math.Abs(lly[i-1]-lly[i]) < tol && tt.emptyCompositeRow(i)
		if !merge {
			rows = append(rows, i)
		}
	}
	for i := 0; i < tt.w; i++ {
		merge := i < tt.w-1 && math.Abs(llx[i+1]-llx[i]) < tol && tt.emptyCompositeColumn(i)
		if !merge {
			cols = append(cols, i)
		}
	}
	if len(rows) == tt.h && len(cols) == tt.w {
		return tt
	}
	reduced := textTable{exportable: tt.exportable, w: len(cols), h: len(rows), compositeCells: make(map[uint64]compositeCell, len(cols)*len(rows))}
	if debugTables {
		common.Log.Info("reduceTiling: %dx%d -> %dx%d", tt.w, tt.h, len(cols), len(rows))
		common.Log.Info("reducedCols: %+v", cols)
		common.Log.Info("reducedRows: %+v", rows)
	}
	for newRow, row := range rows {
		for newCol, col := range cols {
			composite, rect := tt.getComposite(col, row)
			if len(composite) == 0 {
				continue
			}
			if debugTables {
				fmt.Printf("  %2d, %2d (%2d, %2d) %q\n", newCol, newRow, col, row, truncate(composite.merge().text(), 50))
			}
			reduced.putComposite(newCol, newRow, composite, rect)
		}
	}
	return &reduced
}

type textTable struct {
	model.PdfRectangle
	w, h           int
	exportable     bool
	cells          map[uint64]*textPara
	compositeCells map[uint64]compositeCell
}

func (tt TextTable) getCellInfo(mark TextMark) [][]int {
	for i, cell := range tt.Cells {
		for j := range cell {
			cellMarks := &cell[j].Marks
			if cellMarks.exists(mark) {
				return [][]int{{i}, {j}}
			}
		}
	}
	return nil
}

func (tt *textTable) newTablePara() *textPara {
	rect := tt.computeBbox()
	para := &textPara{PdfRectangle: rect, eBBox: rect, table: tt}
	if debugTables {
		common.Log.Info("newTablePara: %s", para)
	}
	return para
}

func (tt *textTable) toTextTable(ctx *textContext) TextTable {
	if debugTables {
		common.Log.Info("toTextTable: %d x %d", tt.w, tt.h)
	}
	cells := make([][]TableCell, tt.h)
	for i := 0; i < tt.h; i++ {
		cells[i] = make([]TableCell, tt.w)
		for j := 0; j < tt.w; j++ {
			para := tt.get(j, i)
			if para == nil {
				continue
			}
			sortLinesByDepth(para.lines)
			if debugTables {
				fmt.Printf("%4d %2d: %s\n", j, i, para)
			}
			cells[i][j].Text = para.text()
			offset := 0
			cells[i][j].Marks.marks = para.toTextMarks(&offset, ctx)
		}
	}
	table := TextTable{
		W: tt.w, H: tt.h, Cells: cells,
		PdfRectangle: tt.bbox(),
	}
	return table
}
