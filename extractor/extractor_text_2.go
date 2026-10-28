package extractor

import (
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

type textTable struct {
	model.PdfRectangle
	w, h           int
	exportable     bool
	cells          map[uint64]*textPara
	compositeCells map[uint64]compositeCell
}

func (tt *textTable) getComposite(col, row int) (paraList, model.PdfRectangle) {
	cell, ok := tt.compositeCells[cellKey(col, row)]
	if debugTables {
		fmt.Printf("        getComposite(%d,%d)->%s\n", col, row, cell.String())
	}
	if !ok {
		return nil, model.PdfRectangle{}
	}
	return cell.parasBBox()
}

func (tt *textTable) compositeRowCorridors() map[int][]float64 {
	corridors := make(map[int][]float64, tt.h)
	if debugTables {
		common.Log.Info("compositeRowCorridors: h=%d", tt.h)
	}
	for i := 1; i < tt.h; i++ {
		var cells []compositeCell
		for j := 0; j < tt.w; j++ {
			if cell, ok := tt.compositeCells[cellKey(j, i)]; ok {
				cells = append(cells, cell)
			}
		}
		if len(cells) == 0 {
			continue
		}
		rowCorr := rowCorridors(cells)
		corridors[i] = rowCorr
		if debugTables {
			fmt.Printf("   %2d: %6.2f\n", i, rowCorr)
		}
	}
	return corridors
}

func (tt *textTable) emptyCompositeColumn(col int) bool {
	for row := 0; row < tt.h; row++ {
		if cell, ok := tt.compositeCells[cellKey(col, row)]; ok {
			if len(cell.paraList) > 0 {
				return false
			}
		}
	}
	return true
}

func (tt *textTable) putComposite(col, row int, paras paraList, rect model.PdfRectangle) {
	if len(paras) == 0 {
		common.Log.Error("textTable) putComposite: empty paras")
		return
	}
	cell := compositeCell{PdfRectangle: rect, paraList: paras}
	if debugTables {
		fmt.Printf("        putComposite(%d,%d)<-%s\n", col, row, cell.String())
	}
	cell.updateBBox()
	tt.compositeCells[cellKey(col, row)] = cell
}

func (tt *textTable) emptyCompositeRow(row int) bool {
	for i := 0; i < tt.w; i++ {
		if cell, ok := tt.compositeCells[cellKey(i, row)]; ok {
			if len(cell.paraList) > 0 {
				return false
			}
		}
	}
	return true
}

func (tt *textTable) compositeColCorridors() map[int][]float64 {
	out := make(map[int][]float64, tt.w)
	if debugTables {
		common.Log.Info("compositeColCorridors: w=%d ", tt.w)
	}
	for i := 0; i < tt.w; i++ {
		out[i] = nil
	}
	return out
}

func (tt *textTable) logComposite(msg string) {
	if !debugTables {
		return
	}
	common.Log.Info("~~~Para %d x %d %s", tt.w, tt.h, msg)
	fmt.Printf("%5s |", "")
	for i := 0; i < tt.w; i++ {
		fmt.Printf("%3d |", i)
	}
	fmt.Println("")
	fmt.Printf("%5s +", "")
	for i := 0; i < tt.w; i++ {
		fmt.Printf("%3s +", "---")
	}
	fmt.Println("")
	for i := 0; i < tt.h; i++ {
		fmt.Printf("%5d |", i)
		for j := 0; j < tt.w; j++ {
			paras, _ := tt.compositeCells[cellKey(j, i)].parasBBox()
			fmt.Printf("%3d |", len(paras))
		}
		fmt.Println("")
	}
	common.Log.Info("~~~Text %d x %d %s", tt.w, tt.h, msg)
	fmt.Printf("%5s |", "")
	for i := 0; i < tt.w; i++ {
		fmt.Printf("%12d |", i)
	}
	fmt.Println("")
	fmt.Printf("%5s +", "")
	for i := 0; i < tt.w; i++ {
		fmt.Print("-------------+")
	}
	fmt.Println("")
	for i := 0; i < tt.h; i++ {
		fmt.Printf("%5d |", i)
		for j := 0; j < tt.w; j++ {
			paras, _ := tt.compositeCells[cellKey(j, i)].parasBBox()
			txt := ""
			para := paras.merge()
			if para != nil {
				txt = para.text()
			}
			txt = fmt.Sprintf("%q", truncate(txt, 12))
			txt = txt[1 : len(txt)-1]
			fmt.Printf("%12s |", txt)
		}
		fmt.Println("")
	}
}

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

func (tt *textTable) get(col, row int) *textPara {
	return tt.cells[cellKey(col, row)]
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

// TextTable represents a table.
// Cells are ordered top-to-bottom, left-to-right.
// Cells[y] is the (0-offset) y'th row in the table.
// Cells[y][x] is the (0-offset) x'th column in the table.
type TextTable struct {
	model.PdfRectangle
	W, H  int
	Cells [][]TableCell
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
