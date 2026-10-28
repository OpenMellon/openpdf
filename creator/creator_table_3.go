package creator

import (
	"errors"
	"math"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func generateTablePageBlocks(table *Table, ctx DrawContext) ([]*Block, DrawContext, error) {
	var blocks []*Block
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if table.positioning.IsAbsolute() {
		table.updateRowHeights(ctx.Width)
	} else {
		table.updateRowHeights(ctx.Width - table.margins.Left - table.margins.Right)
	}
	top := table.margins.Top
	if table.positioning.IsRelative() && !table.enablePageWrap {
		height := table.Height()
		if height > ctx.Height-table.margins.Top && height <= ctx.PageHeight-ctx.Margins.Top-ctx.Margins.Bottom {
			blocks = []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y)}
			var err error
			if _, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			top = 0
		}
	}
	ctx2 := ctx
	if table.positioning.IsAbsolute() {
		ctx.X = table.xPos
		ctx.Y = table.yPos
	} else {
		ctx.X += table.margins.Left
		ctx.Y += top
		ctx.Width -= table.margins.Left + table.margins.Right
		ctx.Height -= top
	}
	width := ctx.Width
	x := ctx.X
	y := ctx.Y
	height := ctx.Height
	row := 0
	n, n2 := -1, -1
	if table.hasHeaderRows {
		for i, cell := range table.cells {
			if cell.row < table.headerStartRow {
				continue
			}
			if cell.row > table.headerEndRow {
				break
			}
			if n < 0 {
				n = i
			}
			n2 = i
		}
	}
	if err := table.wrapContent(ctx); err != nil {
		return nil, ctx, err
	}
	table.updateRowHeights(width)
	contentHeight := ctx.contentHeight()
	for _, cell := range table.cells {
		if cell.wrapDisabled && cell.row-1 < len(table.rowHeights) && table.rowHeights[cell.row-1] > contentHeight {
			table.rowHeights[cell.row-1] = contentHeight
		}
	}
	var (
		ok               bool
		n3               int
		row2             int
		ok2              bool
		row3             int
		err              error
		mcid             int64
		rowTag           *model.KDict
		structureTagInfo *model.StructureTagInfo
		ok3              bool
		ok4              = table.tagInfo != nil && table.tagInfo.ApplyTag
		n4               = -1
		n5               = -1
	)
	if ok4 {
		beginTaggedContent(block, table.tagInfo)
		mcid = table.tagInfo.Mcid + 1
	}
	for i := 0; i < len(table.cells); i++ {
		cell := table.cells[i]
		if lastCell, cell2 := table.getLastCellFromCol(cell.col); lastCell == i {
			if (cell2.row + cell2.rowSpan - 1) < table.rows {
				for i := cell.row; i < table.rows; i++ {
					cell2 := &TableCell{}
					cell2.row = i + 1
					cell2.rowSpan = 1
					cell2.col = cell.col
					table.cells = append(table.cells, cell2)
				}
			}
		}
		width2 := cell.width(table.colWidths, width)
		colWidth := float64(0.0)
		for i := 0; i < cell.col-1; i++ {
			colWidth += table.colWidths[i] * width
		}
		rowHeight := float64(0.0)
		for i := row; i < cell.row-1; i++ {
			rowHeight += table.rowHeights[i]
		}
		ctx.Height = height - rowHeight
		contentHeight := float64(0.0)
		for i := 0; i < cell.rowSpan; i++ {
			contentHeight += table.rowHeights[cell.row+i-1]
		}
		ok5 := ok2 && cell.row != row3
		row3 = cell.row
		if ok5 || contentHeight > ctx.Height {
			if table.enableRowWrap && !ok2 {
				ok2, err = table.wrapRow(i, ctx, width, ctx.contentHeight()-table.headerHeight())
				if err != nil {
					return nil, ctx, err
				}
				if ok2 {
					i--
					continue
				}
			}
			if ok4 && !ok3 {
				if structureTagInfo != nil {
					endMarkedContent(block)
					structureTagInfo = nil
				}
				endMarkedContent(block)
				ok3 = true
			}
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			x = ctx.Margins.Left + table.margins.Left
			y = ctx.Margins.Top
			ctx.Height = ctx.contentHeight()
			ctx.Page++
			height = ctx.Height
			row = cell.row - 1
			rowHeight = 0
			ok2 = false
			if table.hasHeaderRows && n >= 0 {
				if cell.rowSpan > (table.rows - row3 + 1) {
					common.Log.Debug("Table header rowspan exceeds available space.")
					n, n2 = -1, -1
				} else if n5 == i {
					common.Log.Debug("Table header rowspan exceeds available space.")
				} else {
					n5 = i
					n3 = i
					i = n - 1
					row2 = row
					row = table.headerStartRow - 1
					ok = true
					continue
				}
			}
			if ok5 {
				i--
				continue
			}
		}
		ok2 := ok4 && ok
		if ok4 && !ok2 && ok3 {
			beginTaggedContent(block, table.tagInfo)
			ok3 = false
		}
		if ok4 && !ok2 && structureTagInfo == nil {
			structureTagInfo = &model.StructureTagInfo{Mcid: mcid}
			switch table.tagInfo.StructureType {
			case model.StructureTypeTable:
				structureTagInfo.StructureType = model.StructureTypeTableRow
				rowTag = table.addRowTag()
			case model.StructureTypeList:
				structureTagInfo.StructureType = model.StructureTypeListItem
				rowTag = table.addListItemTag()
			}
			beginTaggedContent(block, structureTagInfo)
			mcid++
		}
		ctx.Width = width2
		ctx.X = x + colWidth
		ctx.Y = y + rowHeight
		if contentHeight > ctx.PageHeight-ctx.Margins.Top-ctx.Margins.Bottom {
			contentHeight = ctx.contentHeight()
		}
		border := newBorder(ctx.X, ctx.Y, width2, contentHeight)
		if cell.backgroundColor != nil {
			border.SetFillColor(cell.backgroundColor)
		}
		border.SetOpacity(cell.opacity)
		border.LineStyle = cell.borderLineStyle
		border.styleLeft = cell.borderStyleLeft
		border.styleRight = cell.borderStyleRight
		border.styleTop = cell.borderStyleTop
		border.styleBottom = cell.borderStyleBottom
		if cell.borderColorLeft != nil {
			border.SetColorLeft(cell.borderColorLeft)
		}
		if cell.borderColorBottom != nil {
			border.SetColorBottom(cell.borderColorBottom)
		}
		if cell.borderColorRight != nil {
			border.SetColorRight(cell.borderColorRight)
		}
		if cell.borderColorTop != nil {
			border.SetColorTop(cell.borderColorTop)
		}
		border.SetWidthBottom(cell.borderWidthBottom)
		border.SetWidthLeft(cell.borderWidthLeft)
		border.SetWidthRight(cell.borderWidthRight)
		border.SetWidthTop(cell.borderWidthTop)
		contentsLen, annotationsLen, mcid2 := len(*block.contents), len(block.annotations), mcid
		err := block.Draw(border)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		}
		var kChild *model.KDict
		if ok4 && !ok2 {
			cell.SetStructPageNumber(table.tagInfo.StructPageNumber)
			ok := table.hasHeaderRows && cell.row >= table.headerStartRow && cell.row <= table.headerEndRow
			structureTypeTableData := model.StructureTypeTableData
			switch structureType := cell.tagInfo.StructureType; {
			case structureType != "" && structureType != model.StructureTypeTableData:
				structureTypeTableData = structureType
			case ok:
				structureTypeTableData = model.StructureTypeTableHeaderCell
			}
			cell.SetStructureType(structureTypeTableData)
			kChild = model.NewKDictionary()
			kChild.S = core.MakeName(string(structureTypeTableData))
			if structureTypeTableData == model.StructureTypeTableHeaderCell {
				dict := core.MakeDict()
				dict.Set("O", core.MakeName("Table"))
				dict.Set("Scope", core.MakeName("Column"))
				kChild.A = core.MakeIndirectObject(dict)
			}
			rowTag.AddKChild(kChild)
		}
		if cell.content != nil {
			if kChild != nil {
				cell.SetMarkedContentID(mcid)
				beginTaggedContent(block, &model.StructureTagInfo{Mcid: mcid, StructureType: cell.tagInfo.StructureType})
				kValue := model.NewKValue()
				kValue.SetMCID(int(mcid))
				kChild.AddChild(kValue)
				mcid++
			}
			width := cell.content.Width()
			height := cell.content.Height()
			width, height, y, kChild2, ok, err := layoutCellContent(cell.content, width, height, cell.verticalAlignment, table.compactMode, kChild, mcid, width2)
			if err != nil {
				return nil, ctx, err
			}
			if kChild2 != nil {
				if !ok {
					kChild.AddKChild(kChild2)
				}
				mcid++
			}
			switch cell.horizontalAlignment {
			case CellHorizontalAlignmentLeft:
				ctx.X += cell.indent
				ctx.Width -= cell.indent
			case CellHorizontalAlignmentCenter:
				if width3 := width2 - width; width3 > 0 {
					ctx.X += width3 / 2
					ctx.Width -= width3 / 2
				}
			case CellHorizontalAlignmentRight:
				if width2 > width {
					ctx.X = ctx.X + width2 - width - cell.indent
					ctx.Width -= cell.indent
				}
			}
			y2 := ctx.Y
			height2 := ctx.Height
			ctx.Y += y
			switch cell.verticalAlignment {
			case CellVerticalAlignmentTop:
			case CellVerticalAlignmentMiddle:
				if contentHeight2 := contentHeight - height; contentHeight2 > 0 {
					ctx.Y += contentHeight2 / 2
					ctx.Height -= contentHeight2 / 2
				}
			case CellVerticalAlignmentBottom:
				if contentHeight > height {
					ctx.Y = ctx.Y + contentHeight - height
					ctx.Height = contentHeight
				}
			}
			fn := func(d VectorDrawable) error {
				if ok2 {
					for _, structureTagInfo := range collectStructureTags(d, nil) {
						structureTagInfo.ApplyTag = false
						defer func() { structureTagInfo.ApplyTag = true }()
					}
				}
				return block.DrawWithContext(d, ctx)
			}
			err = fn(cell.content)
			if errors.Is(err, ErrContentNotFit) {
				contentHeight := ctx.contentHeight()
				ok := cell.wrapDisabled && ctx.Height >= contentHeight-0.1
				if !ok5 && n4 != i && !ok {
					*block.contents = (*block.contents)[:contentsLen]
					block.annotations = block.annotations[:annotationsLen]
					if kChild != nil {
						rowTag.RemoveKChild(kChild)
					}
					mcid = mcid2
					n4 = i
					i--
					continue
				}
				if drawableToFit := splitDrawableToFit(cell.content, ctx); drawableToFit != nil {
					err = fn(drawableToFit)
				}
			}
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
			ctx.Y = y2
			ctx.Height = height2
		}
		if ok2 {
			wrapAsPaginationArtifact(block, contentsLen)
		}
		ctx.Y += contentHeight
		ctx.Height -= contentHeight
		if ok && i+1 > n2 {
			y += rowHeight + contentHeight
			height -= contentHeight + rowHeight
			row = row2
			i = n3 - 1
			ok = false
		}
		if ok4 && !ok2 {
			if cell.content != nil {
				endMarkedContent(block)
			}
			if structureTagInfo != nil && (i+1 == len(table.cells) || table.cells[i+1].row != cell.row) {
				endMarkedContent(block)
				structureTagInfo = nil
			}
		}
	}
	if ok4 && !ok3 {
		endMarkedContent(block)
	}
	blocks = append(blocks, block)
	if table.positioning.IsAbsolute() {
		return blocks, ctx2, nil
	}
	ctx.X = ctx2.X
	ctx.Width = ctx2.Width
	ctx.Y += table.margins.Bottom
	ctx.Height -= table.margins.Bottom
	return blocks, ctx, nil
}

// GetPositioning returns the positioning mode (relative or absolute) of the
// table.
func (t *Table) GetPositioning() Positioning { return t.positioning }

func parseTableTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseTable(node)
}

func (t *Table) headerHeight() float64 {
	var headerHeight float64
	if !t.hasHeaderRows {
		return headerHeight
	}
	for i := t.headerStartRow; i <= t.headerEndRow && i >= 1 && i-1 < len(t.rowHeights); i++ {
		headerHeight += t.rowHeights[i-1]
	}
	return headerHeight
}

// Rows returns the total number of rows the table has.
func (t *Table) Rows() int { return t.rows }

// AddTag adds the table tag to the root K object.
func (t *Table) AddTag(rootKObj *model.KDict) {
	if rootKObj == nil {
		common.Log.Debug("Table: AddTag called with nil rootKObj, skipping tagging.")
		return
	}
	if t.tagInfo == nil {
		t.tagInfo = model.NewStructureTagInfo()
		t.tagInfo.StructureType = t.structType
	}
	t.tagInfo.ApplyTag = true
	t.tagInfo.ParentKObj = rootKObj
	t.tagInfo.ComponentKObj = t.tagInfo.NewComponentKObj(t.tagInfo.StructureType)
	t.tagInfo.ParentKObj.AddKChild(t.tagInfo.ComponentKObj)
}

// MultiRowCell makes a new cell with the specified row span and inserts it
// into the table at the current position.
func (t *Table) MultiRowCell(rowspan int) *TableCell { return t.MultiCell(rowspan, 1) }

// Cols returns the total number of columns the table has.
func (t *Table) Cols() int { return t.cols }

func (t *Table) splitSubtables(i2 int, ctx2 DrawContext, totalWidth, height float64) map[int][2]*Table {
	row := t.cells[i2].row
	var subtableSplits []subtableSplit
	ok2 := false
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		cell := t.cells[i]
		table, ok := cell.content.(*Table)
		if !ok {
			continue
		}
		width := cell.width(t.colWidths, totalWidth)
		height2 := cell.height(width)
		if !table.enablePageWrap && height2 <= height {
			continue
		}
		if height2 > ctx2.Height {
			ok2 = true
		}
		ctx := ctx2
		ctx.Width = width - cell.indent
		subtableSplits = append(subtableSplits, subtableSplit{index: i, table: table, ctx: ctx, height: height})
	}
	if !ok2 {
		return nil
	}
	rows, ok := mergeSubtableSplits(subtableSplits)
	if !ok {
		return nil
	}
	if allFirstPartsEmpty(rows) {
		return nil
	}
	m := make(map[int][2]*Table, len(subtableSplits))
	for i, subtableSplit := range subtableSplits {
		m[subtableSplit.index] = rows[i]
	}
	return m
}

// CurRow returns the currently active cell's row number.
func (t *Table) CurRow() int { curRow := (t.curCell-1)/t.cols + 1; return curRow }

func (t *Table) headerCut(headerCut int) int {
	if t.hasHeaderRows && headerCut == t.headerEndRow && headerCut < t.rows {
		return t.headerStartRow - 1
	}
	return headerCut
}

// MultiColCell makes a new cell with the specified column span and inserts it
// into the table at the current position.
func (t *Table) MultiColCell(colspan int) *TableCell { return t.MultiCell(1, colspan) }

func (t *Table) maxFittingRows(value, top float64) int {
	t.sortCells()
	t.updateRowHeights(value - t.margins.Left - t.margins.Right)
	top -= t.margins.Top + t.margins.Bottom
	var rowHeight float64
	row := 0
	for i := 1; i <= t.rows && i-1 < len(t.rowHeights); i++ {
		rowHeight += t.rowHeights[i-1]
		if rowHeight > top {
			break
		}
		row = i
	}
	return t.clampCut(row)
}

// Width is not used. Not used as a Table element is designed to fill into
// available width depending on the context. Returns 0.
func (t *Table) Width() float64 { return 0 }

// SetCompactMode sets the compact mode flag for this table.
//
// By enabling compact mode, a table cell that contains a StyledParagraph
// will not add extra height when calculating its height.
//
// The default value is false.
func (t *Table) SetCompactMode(enable bool) { t.compactMode = enable }

// AddSubtable copies the cells of the subtable in the table, starting with the
// specified position. The table row and column indices are 1-based, which
// makes the position of the first cell of the first row of the table 1,1.
// The table is automatically extended if the subtable exceeds its columns.
// This can happen when the subtable has more columns than the table or when
// one or more columns of the subtable starting from the specified position
// exceed the last column of the table.
func (t *Table) AddSubtable(row, col int, subtable *Table) {
	for _, cell := range subtable.cells {
		cell2 := &TableCell{}
		*cell2 = *cell
		cell2.table = t
		cell2.col += col - 1
		if cols := t.cols - (cell2.col - 1); cols < cell2.colSpan {
			t.cols += cell2.colSpan - cols
			t.resetColumnWidths()
			common.Log.Debug("Table: subtable exceeds destination table. Expanding table to %d columns.", t.cols)
		}
		cell2.row += row - 1
		rowHeight := subtable.rowHeights[cell.row-1]
		if cell2.row > t.rows {
			for cell2.row > t.rows {
				t.rows++
				t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
			}
			t.rowHeights[cell2.row-1] = rowHeight
		} else {
			t.rowHeights[cell2.row-1] = math.Max(t.rowHeights[cell2.row-1], rowHeight)
		}
		t.cells = append(t.cells, cell2)
	}
	t.sortCells()
}

// HasHeader returns whether the table has header rows that are repeated for
// every page the table spans. See SetHeaderRows. Note that Table defines its
// header at the table level over a row range, whereas Grid defines headers
// per row via the row's section (see GridRow.IsHeader).
func (t *Table) HasHeader() bool { return t.hasHeaderRows }

func (t *Table) sortCells() {
	sort.Slice(t.cells, func(i, j int) bool {
		row := t.cells[i].row
		row2 := t.cells[j].row
		if row < row2 {
			return true
		}
		if row > row2 {
			return false
		}
		return t.cells[i].col < t.cells[j].col
	})
}

// GetColumnWidths returns the column width fractions of the table. The
// fractions add up to 1. The returned slice references the table's internal
// column widths; modifying its elements modifies the table.
func (t *Table) GetColumnWidths() []float64 { return t.colWidths }

// SkipOver skips over a specified number of rows and cols.
func (t *Table) SkipOver(rows, cols int) {
	n := rows*t.cols + cols - 1
	if n < 0 {
		common.Log.Debug("Table: cannot skip back to previous cells")
		return
	}
	for i := 0; i < n; i++ {
		t.NewCell()
	}
}

func (t *Table) updateRowHeights(width float64) {
	for _, cell := range t.cells {
		if cell.rowSpan > 1 {
			continue
		}
		height := cell.height(cell.width(t.colWidths, width))
		if rowHeight := t.rowHeights[cell.row-1]; height > rowHeight {
			t.rowHeights[cell.row-1] = height
		}
	}
	for _, cell := range t.cells {
		if cell.rowSpan <= 1 {
			continue
		}
		height := cell.height(cell.width(t.colWidths, width))
		spanHeight := 0.0
		for _, rowHeight := range t.rowHeights[cell.row-1 : (cell.row + cell.rowSpan - 1)] {
			spanHeight += rowHeight
		}
		if height <= spanHeight {
			continue
		}
		if t.enableRowWrap {
			t.rowHeights[cell.row+cell.rowSpan-2] += height - spanHeight
			continue
		}
		perRowHeight := height / float64(cell.rowSpan)
		for i := cell.row; i < cell.row+cell.rowSpan; i++ {
			if perRowHeight > t.rowHeights[i-1] {
				t.rowHeights[i-1] = perRowHeight
			}
		}
	}
}

// GetPos returns the absolute coordinates (x, y) of the table's upper-left
// corner. The coordinates are only meaningful when the table uses absolute
// positioning (see GetPositioning).
func (t *Table) GetPos() (float64, float64) { return t.xPos, t.yPos }
