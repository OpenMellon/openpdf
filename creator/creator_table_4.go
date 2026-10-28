package creator

import (
	"errors"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

// EnableRowWrap controls whether rows are wrapped across pages.
// It also selects how a row-spanning cell taller than its rows is laid out:
// with wrapping enabled the overflow goes to the last spanned row, which is
// then split across pages; without it the overflow is distributed evenly
// across the spanned rows.
// Rows are wrapped by splitting StyledParagraph, Division, List and Table
// content; subtables are split at row boundaries.
func (t *Table) EnableRowWrap(enable bool) { t.enableRowWrap = enable }

// SetStructureType sets the structure type for the table.
func (t *Table) SetStructureType(structureType model.StructureType) {
	if t.tagInfo == nil {
		t.tagInfo = model.NewStructureTagInfo()
	}
	t.tagInfo.StructureType = structureType
	if t.tagInfo.ComponentKObj != nil {
		if t.tagInfo.ParentKObj != nil {
			t.tagInfo.ParentKObj.RemoveKChild(t.tagInfo.ComponentKObj)
		}
		t.tagInfo.ComponentKObj = t.tagInfo.NewComponentKObj(structureType)
		t.tagInfo.ParentKObj.AddKChild(t.tagInfo.ComponentKObj)
	}
}

// SkipRows skips over a specified number of rows in the table.
func (t *Table) SkipRows(num int) {
	n := num*t.cols - 1
	if n < 0 {
		common.Log.Debug("Table: cannot skip back to previous cells")
		return
	}
	for i := 0; i < n; i++ {
		t.NewCell()
	}
}

func (t *Table) wrapContent(ctx2 DrawContext) error {
	t.sortCells()
	value := ctx2.contentHeight() - t.headerHeight()
	fn := func(head2 VectorDrawable, width, value2, height float64) (head, tail VectorDrawable) {
		switch head3 := head2.(type) {
		case *StyledParagraph:
			ctx := ctx2
			ctx.Width = width
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom - 0.5*head3.getTextHeight())
			head4, tail2, err := head3.split(ctx)
			if err != nil {
				common.Log.Error("Error wrap styled paragraph: %v", err.Error())
				return head2, nil
			}
			if head4 != nil {
				head = head4
			}
			if tail2 != nil {
				tail = tail2
			}
		case *Division:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom)
			head2, tail2 := head3.split(ctx, value-head3.margins.Vertical())
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		case *List:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom)
			head2, tail2 := head3.split(ctx, value-head3.margins.Vertical())
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		case *Table:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = height
			head2, tail2 := head3.split(ctx, value)
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		default:
			return head2, nil
		}
		return head, tail
	}
	fn2 := func(head2 VectorDrawable, width, value, height float64) []VectorDrawable {
		if para, ok := head2.(*StyledParagraph); ok {
			ctx := ctx2
			ctx.Width = width
			ctx.Height = math.Floor(height - para.margins.Top - para.margins.Bottom - 0.5*para.getTextHeight())
			toPieces, err := para.splitToPieces(ctx)
			if err != nil {
				common.Log.Error("Error splitting styled paragraph: %v", err.Error())
				return []VectorDrawable{head2}
			}
			drawables := make([]VectorDrawable, len(toPieces))
			for i, toPiece := range toPieces {
				drawables[i] = toPiece
			}
			return drawables
		}
		var drawables []VectorDrawable
		head := head2
		for {
			head2, tail := fn(head, width, value, height)
			if head2 == nil {
				drawables = append(drawables, head)
				break
			}
			drawables = append(drawables, head2)
			if tail == nil {
				break
			}
			head = tail
		}
		return drawables
	}
	contentHeight := ctx2.contentHeight()
	height2 := contentHeight
	if headerHeight := t.headerHeight(); headerHeight < height2 {
		height2 -= headerHeight
	}
	type cellSplit struct {
		cell  *TableCell
		parts []VectorDrawable
	}
	fn3 := func() {
		if t.enableRowWrap {
			return
		}
		ok := false
		for i := t.rows; i >= 1; i-- {
			height := height2
			if t.hasHeaderRows && i >= t.headerStartRow && i <= t.headerEndRow {
				height = contentHeight
			}
			if i-1 >= len(t.rowHeights) || t.rowHeights[i-1] <= height {
				continue
			}
			var cellSplits []cellSplit
			n := 1
			rowSpan := 1
			for _, cell := range t.cells {
				if cell.row != i {
					continue
				}
				width := cell.width(t.colWidths, ctx2.Width)
				if cell.height(width) <= height {
					continue
				}
				fn := fn2(cell.content, width, cell.indent, height)
				if len(fn) < 2 {
					continue
				}
				cellSplits = append(cellSplits, cellSplit{cell, fn})
				if len(fn) > n {
					n = len(fn)
				}
				if cell.rowSpan > rowSpan {
					rowSpan = cell.rowSpan
				}
			}
			if n < 2 {
				continue
			}
			t.insertRows(i, n-1)
			n2 := i + (n - 1) + (rowSpan - 1)
			for j := i; j <= n2 && j-1 < len(t.rowHeights); j++ {
				t.rowHeights[j-1] = t.defaultRowHeight
			}
			ok = true
			for _, cellSplit := range cellSplits {
				cellSplit.cell.content = cellSplit.parts[0]
				cellSplit.cell.rowSpan = 1
				for j := 1; j < len(cellSplit.parts); j++ {
					props := cellSplit.cell.cloneProps(cellSplit.parts[j])
					props.rowSpan = 1
					props.row = i + j
					props.col = cellSplit.cell.col
					t.cells = append(t.cells, props)
				}
			}
		}
		if ok {
			t.sortCells()
			t.updateRowHeights(ctx2.Width)
		}
	}
	fn3()
	t.splitSpanningCellsAtBreaks(ctx2, fn)
	fn3()
	t.sortCells()
	return nil
}

func (t *Table) wrapRow(i2 int, ctx DrawContext, totalWidth, height float64) (bool, error) {
	if !t.enableRowWrap {
		return false, nil
	}
	row := t.cells[i2].row
	for _, cell := range t.cells {
		if cell.rowSpan > 1 && cell.row <= row && row < cell.row+cell.rowSpan {
			return false, nil
		}
	}
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		if t.cells[i].wrapDisabled {
			return false, nil
		}
	}
	var (
		cell     = t.cells[i2]
		cellsLen = -1
		cells    []*TableCell
		value    float64
		row2     bool
		floats   = make([]float64, 0, len(t.colWidths))
	)
	fn := func(cell *TableCell, content VectorDrawable, ok bool) *TableCell {
		cell2 := *cell
		cell2.content = content
		if ok {
			cell2.row++
		}
		return &cell2
	}
	fn2 := func(n int, content VectorDrawable) {
		var height float64 = -1
		if content == nil {
			if float := floats[n-i2]; float > ctx.Height {
				content = t.cells[n].content
				t.cells[n].content = nil
				floats[n-i2] = 0
				height = float
			}
		}
		fn2 := fn(t.cells[n], content, true)
		cells = append(cells, fn2)
		if height < 0 {
			height = fn2.height(ctx.Width)
		}
		if height > value {
			value = height
		}
	}
	type savedCell struct {
		target *TableCell
		saved  TableCell
	}
	var savedCells []savedCell
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		savedCells = append(savedCells, savedCell{t.cells[i], *t.cells[i]})
	}
	subtables := t.splitSubtables(i2, ctx, totalWidth, height)
	for i := i2; i < len(t.cells); i++ {
		cell2 := t.cells[i]
		if cell.row != cell2.row {
			cellsLen = i
			break
		}
		ctx.Width = cell2.width(t.colWidths, totalWidth)
		height2 := cell2.height(ctx.Width)
		var content VectorDrawable
		switch content2 := cell2.content.(type) {
		case *StyledParagraph:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom - 0.5*content2.getTextHeight())
				head, tail, err := content2.split(ctx2)
				if err != nil {
					return false, err
				}
				if head != nil && tail != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
					content = tail
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		case *Division:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Width = ctx.Width - cell2.indent
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom)
				head, tail := content2.split(ctx2, height-content2.margins.Vertical())
				if head != nil && tail != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
					content = tail
					row2 = true
					if head.background != nil {
						head.background.BorderRadiusBottomLeft = 0
						head.background.BorderRadiusBottomRight = 0
					}
					if tail.background != nil {
						tail.background.BorderRadiusTopLeft = 0
						tail.background.BorderRadiusTopRight = 0
					}
					height2 = cell2.height(ctx.Width)
				}
			}
		case *List:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Width = ctx.Width - cell2.indent
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Vertical())
				head, tail := content2.split(ctx2, height-content2.margins.Vertical())
				if head != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
				}
				if tail != nil {
					content = tail
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		case *Table:
			if subtable, ok := subtables[i]; ok {
				content2, content3 := subtable[0], subtable[1]
				if content2 != nil {
					cell2 = fn(cell2, content2, false)
				} else {
					cell2 = fn(cell2, nil, false)
				}
				t.cells[i] = cell2
				if content3 != nil {
					content = content3
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		}
		floats = append(floats, height2)
		if row2 {
			if cells == nil {
				cells = make([]*TableCell, 0, len(t.colWidths))
				for j := i2; j < i; j++ {
					fn2(j, nil)
				}
			}
			fn2(i, content)
		}
	}
	var value2 float64
	for _, float := range floats {
		if float > value2 {
			value2 = float
		}
	}
	if row2 && value2 > ctx.Height {
		for i, savedCell := range savedCells {
			*savedCell.target = savedCell.saved
			t.cells[i2+i] = savedCell.target
		}
		return false, nil
	}
	if row2 {
		if cellsLen < 0 {
			cellsLen = len(t.cells)
		}
		row := t.cells[cellsLen-1].row
		for i := cellsLen; i < len(t.cells); i++ {
			t.cells[i].row++
		}
		t.cells = append(t.cells[:cellsLen], append(cells, t.cells[cellsLen:]...)...)
		t.rowHeights = append(t.rowHeights[:row], append([]float64{value}, t.rowHeights[row:]...)...)
		if len(t.rowHeights) > t.rows {
			t.rows = len(t.rowHeights)
		}
		t.rowHeights[cell.row-1] = value2
	}
	return row2, nil
}

// SetColumnWidths sets the fractional column widths.
// Each width should be in the range 0-1 and is a fraction of the table width.
// The number of width inputs must match number of columns, otherwise an error is returned.
func (t *Table) SetColumnWidths(widths ...float64) error {
	if len(widths) != t.cols {
		common.Log.Debug("Mismatching number of widths and columns")
		return errors.New("range check error")
	}
	t.colWidths = widths
	return nil
}

// GeneratePageBlocks generates the table page blocks. Multiple blocks are
// generated if the contents wrap over multiple pages.
// Implements the Drawable interface.
func (t *Table) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	table := t
	if t.enableRowWrap {
		table = t.clone()
	}
	return generateTablePageBlocks(table, ctx)
}

// GetRowHeight returns the height of the specified row.
func (t *Table) GetRowHeight(row int) (float64, error) {
	if row < 1 || row > len(t.rowHeights) {
		return 0, errors.New("range check error")
	}
	return t.rowHeights[row-1], nil
}

// SetRowHeight sets the height for a specified row.
func (t *Table) SetRowHeight(row int, h float64) error {
	if row < 1 || row > len(t.rowHeights) {
		return errors.New("range check error")
	}
	t.rowHeights[row-1] = h
	return nil
}

// EnablePageWrap controls whether the table is wrapped across pages.
// If disabled, the table is moved in its entirety on a new page, if it
// does not fit in the available height. By default, page wrapping is enabled.
// If the height of the table is larger than an entire page, wrapping is
// enabled automatically in order to avoid unwanted behavior.
func (t *Table) EnablePageWrap(enable bool) { t.enablePageWrap = enable }

// SetPos sets the Table's positioning to absolute mode and specifies the upper-left corner
// coordinates as (x,y).
// Note that this is only sensible to use when the table does not wrap over multiple pages.
// TODO: Should be able to set width too (not just based on context/relative positioning mode).
func (t *Table) SetPos(x, y float64) {
	t.positioning = PositionAbsolute
	t.xPos = x
	t.yPos = y
}

func (t *Table) splitAtRow(rows int) (head, tail *Table) {
	if rows <= 0 {
		return nil, t
	}
	if rows >= t.rows {
		return t, nil
	}
	t.sortCells()
	head = t.clone()
	cells := make([]*TableCell, 0, len(head.cells))
	for _, cell := range head.cells {
		if cell.row <= rows {
			cells = append(cells, cell)
		}
	}
	head.cells = cells
	head.rows = rows
	head.rowHeights = head.rowHeights[:rows]
	tail = t.clone()
	var cells2 []*TableCell
	var floats []float64
	headerEndRow := 0
	if t.hasHeaderRows && t.headerEndRow <= rows {
		headerEndRow = t.headerEndRow - t.headerStartRow + 1
		for _, cell := range tail.cells {
			if cell.row >= t.headerStartRow && cell.row <= t.headerEndRow {
				cell2 := *cell
				cell2.row = cell.row - t.headerStartRow + 1
				cells2 = append(cells2, &cell2)
			}
		}
		floats = append(floats, t.rowHeights[t.headerStartRow-1:t.headerEndRow]...)
		tail.headerStartRow, tail.headerEndRow = 1, headerEndRow
	} else if t.hasHeaderRows {
		tail.headerStartRow -= rows
		tail.headerEndRow -= rows
	}
	for _, cell := range tail.cells {
		if cell.row > rows {
			cell.row = cell.row - rows + headerEndRow
			cells2 = append(cells2, cell)
		}
	}
	tail.cells = cells2
	tail.rowHeights = append(floats, t.rowHeights[rows:]...)
	tail.rows = t.rows - rows + headerEndRow
	if t.tagInfo != nil {
		tagInfo, tagInfo2 := *t.tagInfo, *t.tagInfo
		head.tagInfo, tail.tagInfo = &tagInfo, &tagInfo2
	}
	return head, tail
}
