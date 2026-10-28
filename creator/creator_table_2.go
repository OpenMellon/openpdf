package creator

import (
	"errors"
	"slices"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// Table allows organizing content in an rows X columns matrix, which can spawn across multiple pages.
type Table struct {
	taggedDrawable
	rows             int
	cols             int
	curCell          int
	colWidths        []float64
	rowHeights       []float64
	defaultRowHeight float64
	cells            []*TableCell
	rowSpanRemaining []int
	positioning      Positioning
	xPos, yPos       float64
	margins          Margins
	hasHeaderRows    bool
	headerStartRow   int
	headerEndRow     int
	enableRowWrap    bool
	enablePageWrap   bool
	compactMode      bool
}

// Height returns the total height of all rows.
func (t *Table) Height() float64 {
	height := float64(0.0)
	for _, rowHeight := range t.rowHeights {
		height += rowHeight
	}
	return height
}

// NewCell makes a new cell and inserts it into the table at the current position.
func (t *Table) NewCell() *TableCell { return t.MultiCell(1, 1) }

func (t *Table) split(ctx DrawContext, value float64) (head, tail *Table) {
	headerCut := t.maxFittingRows(ctx.Width, ctx.Height)
	if !t.enablePageWrap && t.Height()+t.margins.Top+t.margins.Bottom <= value {
		return nil, t
	}
	if withPartialRow := t.withPartialRow(headerCut, ctx, value); withPartialRow != nil {
		return withPartialRow.splitAtRow(headerCut + 1)
	}
	return t.splitAtRow(t.headerCut(headerCut))
}

func (t *Table) topHeaderRows() int {
	if t.hasHeaderRows && t.headerStartRow == 1 {
		return t.headerEndRow
	}
	return 0
}

var tableAttrs = map[string]attrApplyFunc{"columns": nil, "column-widths": floatArrayAttrErr(func(table *Table, widths []float64) error { return table.SetColumnWidths(widths...) }), "margin": marginAttr(func(table *Table, margins Margins) {
	table.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "x": floatAttr(func(table *Table, x float64) { table.SetPos(x, table.yPos) }), "y": floatAttr(func(table *Table, y float64) { table.SetPos(table.xPos, y) }), "header-start-row": intAttr(func(table *Table, n int64) { table.headerStartRow = int(n) }), "header-end-row": intAttr(func(table *Table, n int64) { table.headerEndRow = int(n) }), "enable-row-wrap": boolAttr(func(table *Table, enable bool) { table.EnableRowWrap(enable) }), "enable-page-wrap": boolAttr(func(table *Table, enable bool) { table.EnablePageWrap(enable) })}

// GetCells returns the cells of the table. The returned slice references the
// table's internal cells; modifying its elements modifies the table.
func (t *Table) GetCells() []*TableCell { return t.cells }

// GetHeaderRows returns the inclusive start and end rows of the table header.
// The returned values are only meaningful when HasHeader returns true.
func (t *Table) GetHeaderRows() (startRow, endRow int) { return t.headerStartRow, t.headerEndRow }

func (t *Table) clone() *Table {
	table := *t
	table.rowHeights = make([]float64, len(t.rowHeights))
	copy(table.rowHeights, t.rowHeights)
	table.colWidths = make([]float64, len(t.colWidths))
	copy(table.colWidths, t.colWidths)
	table.cells = make([]*TableCell, 0, len(t.cells))
	for _, cell := range t.cells {
		cell2 := *cell
		cell2.table = &table
		table.cells = append(table.cells, &cell2)
	}
	return &table
}

// MultiCell makes a new cell with the specified row span and col span
// and inserts it into the table at the current position.
func (t *Table) MultiCell(rowspan, colspan int) *TableCell {
	t.curCell++
	col := (t.moveToNextAvailableCell()-1)%t.cols + 1
	row := (t.curCell-1)/t.cols + 1
	for row > t.rows {
		t.rows++
		t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
	}
	cell := &TableCell{}
	cell.row = row
	cell.col = col
	cell.structType = model.StructureTypeTableData
	cell.tagInfo = model.NewStructureTagInfo()
	cell.tagInfo.StructureType = model.StructureTypeTableData
	cell.indent = 5
	cell.borderStyleLeft = CellBorderStyleNone
	cell.borderLineStyle = contentdraw.LineStyleSolid
	cell.horizontalAlignment = CellHorizontalAlignmentLeft
	cell.verticalAlignment = CellVerticalAlignmentTop
	cell.borderWidthLeft = 0
	cell.borderWidthBottom = 0
	cell.borderWidthRight = 0
	cell.borderWidthTop = 0
	borderColorLeft := ColorBlack
	cell.borderColorLeft = borderColorLeft
	cell.borderColorBottom = borderColorLeft
	cell.borderColorRight = borderColorLeft
	cell.borderColorTop = borderColorLeft
	cell.opacity = 1.0
	if rowspan < 1 {
		common.Log.Debug("Table: cell rowspan less that 1 (%d). Setting cell rowspan to 1.", rowspan)
		rowspan = 1
	}
	rows := t.rows - (cell.row - 1)
	if rowspan > rows {
		common.Log.Debug("Table: cell rowspan (%d) exceeds remaining rows (%d). Adding rows.", rowspan, rows)
		t.rows += rowspan - rows
		for i := 0; i < rowspan-rows; i++ {
			t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
		}
	}
	for i := 0; i < colspan && col+i-1 < len(t.rowSpanRemaining); i++ {
		t.rowSpanRemaining[col+i-1] = rowspan - 1
	}
	cell.rowSpan = rowspan
	if colspan < 1 {
		common.Log.Debug("Table: cell colspan less than 1 (%d). Setting cell colspan to 1.", colspan)
		colspan = 1
	}
	cols := t.cols - (cell.col - 1)
	if colspan > cols {
		common.Log.Debug("Table: cell colspan (%d) exceeds remaining row cols (%d). Adjusting colspan.", colspan, cols)
		colspan = cols
	}
	cell.colSpan = colspan
	t.curCell += colspan - 1
	t.cells = append(t.cells, cell)
	cell.table = t
	return cell
}

func (t *Table) withPartialRow(n int, ctx2 DrawContext, value float64) *Table {
	n2 := n + 1
	if n2 > t.rows || n2-1 >= len(t.rowHeights) {
		return nil
	}
	if t.hasHeaderRows && n2 >= t.headerStartRow && n2 <= t.headerEndRow {
		return nil
	}
	if !t.enableRowWrap && t.rowHeights[n2-1] <= value-t.headerHeight()-t.margins.Top-t.margins.Bottom {
		return nil
	}
	ctx := ctx2
	ctx.Width = ctx2.Width - t.margins.Left - t.margins.Right
	ctx.Height = ctx2.Height - t.margins.Top - t.margins.Bottom
	for i := 0; i < n; i++ {
		ctx.Height -= t.rowHeights[i]
	}
	if ctx.Height <= 0 {
		return nil
	}
	withPartialRow := t.clone()
	withPartialRow.enableRowWrap = true
	withPartialRow.sortCells()
	i2 := -1
	for i, cell := range withPartialRow.cells {
		if cell.row == n2 {
			i2 = i
			break
		}
	}
	if i2 < 0 {
		return nil
	}
	row, err := withPartialRow.wrapRow(i2, ctx, ctx.Width, value-t.margins.Top-t.margins.Bottom-t.headerHeight())
	if err != nil || !row || withPartialRow.rows == t.rows {
		return nil
	}
	for _, cell := range withPartialRow.cells {
		if cell.row == n2 && cell.content != nil {
			withPartialRow.enableRowWrap = t.enableRowWrap
			return withPartialRow
		}
	}
	return nil
}

func (t *Table) addRowTag() *model.KDict {
	kDictionary := model.NewKDictionary()
	kDictionary.S = core.MakeName(string(model.StructureTypeTableRow))
	t.tagInfo.ComponentKObj.AddKChild(kDictionary)
	return kDictionary
}

// SetHeaderRows turns the selected table rows into headers that are repeated
// for every page the table spans. startRow and endRow are inclusive.
func (t *Table) SetHeaderRows(startRow, endRow int) error {
	if startRow <= 0 {
		return errors.New("header start row must be greater than 0")
	}
	if endRow <= 0 {
		return errors.New("header end row must be greater than 0")
	}
	if startRow > endRow {
		return errors.New("header start row  must be less than or equal to the end row")
	}
	t.hasHeaderRows = true
	t.headerStartRow = startRow
	t.headerEndRow = endRow
	return nil
}

func (t *Table) addListItemTag() *model.KDict {
	kDictionary := model.NewKDictionary()
	kDictionary.S = core.MakeName(string(model.StructureTypeListItem))
	t.tagInfo.ComponentKObj.AddKChild(kDictionary)
	return kDictionary
}

func (t *Table) getLastCellFromCol(n int) (int, *TableCell) {
	for i, v := range slices.Backward(t.cells) {
		if v.col <= n && n <= v.col+v.colSpan {
			return i, v
		}
	}
	return 0, nil
}

// CurCol returns the currently active cell's column number.
func (t *Table) CurCol() int { curCol := (t.curCell-1)%t.cols + 1; return curCol }

func (t *Table) insertRows(rowHeightsLen, row int) {
	if row <= 0 {
		return
	}
	for _, cell := range t.cells {
		switch {
		case cell.row > rowHeightsLen:
			cell.row += row
		case cell.row+cell.rowSpan-1 > rowHeightsLen:
			cell.rowSpan += row
		}
	}
	if t.hasHeaderRows {
		if rowHeightsLen < t.headerStartRow {
			t.headerStartRow += row
		}
		if rowHeightsLen < t.headerEndRow {
			t.headerEndRow += row
		}
	}
	if rowHeightsLen > len(t.rowHeights) {
		rowHeightsLen = len(t.rowHeights)
	}
	floats := make([]float64, 0, len(t.rowHeights)+row)
	floats = append(floats, t.rowHeights[:rowHeightsLen]...)
	floats = append(floats, make([]float64, row)...)
	floats = append(floats, t.rowHeights[rowHeightsLen:]...)
	t.rowHeights = floats
	t.rows += row
}

func (t *Table) moveToNextAvailableCell() int {
	moveToNextAvailableCell := (t.curCell-1)%t.cols + 1
	for {
		if moveToNextAvailableCell-1 >= len(t.rowSpanRemaining) {
			if t.rowSpanRemaining[0] == 0 {
				return moveToNextAvailableCell
			}
			moveToNextAvailableCell = 1
		} else if t.rowSpanRemaining[moveToNextAvailableCell-1] == 0 {
			return moveToNextAvailableCell
		}
		t.curCell++
		t.rowSpanRemaining[moveToNextAvailableCell-1]--
		moveToNextAvailableCell++
	}
}

// SetRowWrapDisabled excludes a row from row wrapping: when the table has row
// wrapping enabled (see EnableRowWrap), a disabled row moves to the next page
// whole instead of splitting at the page boundary. Splittable content
// (styled paragraphs, divisions, lists) taller than a full page is clipped;
// other content taller than a full page is not rendered. Applies to the cells
// currently in the row, so call it after the row's cells have been added.
func (t *Table) SetRowWrapDisabled(row int, disabled bool) error {
	if row < 1 || row > t.rows {
		return errors.New("range check error")
	}
	for _, cell := range t.cells {
		if cell.row == row {
			cell.wrapDisabled = disabled
		}
	}
	return nil
}

func (t *Table) splitSpanningCellsAtBreaks(ctx DrawContext, fn func(VectorDrawable, float64, float64, float64) (VectorDrawable, VectorDrawable)) {
	contentHeight := ctx.contentHeight()
	headerHeight := t.headerHeight()
	headerEndRow := 1
	if t.hasHeaderRows {
		headerEndRow = t.headerEndRow + 1
	}
	height := ctx.Height - headerHeight
	contentHeight2 := contentHeight - headerHeight
	if height <= 0 {
		height = contentHeight2
	}
	if contentHeight2 <= 0 {
		return
	}
	value := height
	value2 := 0.0
	m := make(map[int]bool)
	for i := headerEndRow; i <= t.rows && i-1 < len(t.rowHeights); i++ {
		rowHeight := t.rowHeights[i-1]
		if value2 > 0 && value2+rowHeight > value {
			m[i] = true
			value = contentHeight2
			value2 = 0
		}
		value2 += rowHeight
	}
	if len(m) == 0 {
		return
	}
	for i := 0; i < len(t.cells); i++ {
		cell := t.cells[i]
		if cell.rowSpan < 2 || cell.row < headerEndRow {
			continue
		}
		row := cell.row
		n := cell.row + cell.rowSpan - 1
		row2 := -1
		for i := row + 1; i <= n; i++ {
			if m[i] {
				row2 = i
				break
			}
		}
		if row2 < 0 {
			continue
		}
		rowHeight := 0.0
		for i := row; i < row2 && i-1 < len(t.rowHeights); i++ {
			rowHeight += t.rowHeights[i-1]
		}
		width := cell.width(t.colWidths, ctx.Width)
		var content VectorDrawable
		if content2, content3 := fn(cell.content, width, cell.indent, rowHeight); content2 == nil {
			content = cell.content
			cell.content = nil
		} else if content3 != nil {
			cell.content = content2
			content = content3
		}
		props := cell.cloneProps(content)
		props.row = row2
		props.col = cell.col
		props.rowSpan = n - row2 + 1
		cell.rowSpan = row2 - row
		t.cells = append(t.cells, props)
	}
}

// GetMargins returns the left, right, top, bottom Margins.
func (t *Table) GetMargins() (float64, float64, float64, float64) {
	return t.margins.Left, t.margins.Right, t.margins.Top, t.margins.Bottom
}

func (t *Table) resetColumnWidths() {
	t.colWidths = []float64{}
	colWidth := float64(1.0) / float64(t.cols)
	for i := 0; i < t.cols; i++ {
		t.colWidths = append(t.colWidths, colWidth)
	}
}

func (t *Table) clampCut(row int) int {
	for ok := true; ok && row > 0; {
		ok = false
		for _, cell := range t.cells {
			if cell.row <= row && row < cell.row+cell.rowSpan-1 {
				row = cell.row - 1
				ok = true
			}
		}
		if t.hasHeaderRows && row >= t.headerStartRow && row < t.headerEndRow {
			row = t.headerStartRow - 1
			ok = true
		}
	}
	return row
}

func newTable(cols int) *Table {
	table := &Table{cols: cols, defaultRowHeight: 10.0, colWidths: []float64{}, rowHeights: []float64{}, cells: []*TableCell{}, rowSpanRemaining: make([]int, cols), enablePageWrap: true}
	table.structType = model.StructureTypeTable
	table.resetColumnWidths()
	return table
}

// SetMargins sets the Table's left, right, top, bottom margins.
func (t *Table) SetMargins(left, right, top, bottom float64) {
	t.margins.Left = left
	t.margins.Right = right
	t.margins.Top = top
	t.margins.Bottom = bottom
}

// SkipCells skips over a specified number of cells in the table.
func (t *Table) SkipCells(num int) {
	if num < 0 {
		common.Log.Debug("Table: cannot skip back to previous cells")
		return
	}
	for i := 0; i < num; i++ {
		t.NewCell()
	}
}
