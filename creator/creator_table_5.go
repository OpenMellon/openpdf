package creator

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
)

// GridRow defines a row which can contain cells.
type GridRow struct {
	cells         []*GridCell
	height        float64
	contentHeight float64
	index         int
	grid          *Grid
	tagInfo       *model.StructureTagInfo
	section       GridRowSection
}

// IsHeader returns whether this row is a header row. Unlike Table, where the
// header is defined at the table level over a range of rows (see
// Table.HasHeader and Table.SetHeaderRows), a Grid defines headers per row via
// the row's section (see GetSection). IsHeader is a convenience shorthand for
// GetSection() == GridRowSectionHeader.
func (gr *GridRow) IsHeader() bool { return gr.section == GridRowSectionHeader }

// GetHeight get the current height for the row.
func (gr *GridRow) GetHeight() float64 { return gr.height }

// SetSection sets whether this row is a header row.
func (gr *GridRow) SetSection(role GridRowSection) { gr.section = role }

// GetCells returns the cells of the row. The returned slice references the
// row's internal cells; modifying its elements modifies the row.
func (gr *GridRow) GetCells() []*GridCell { return gr.cells }

// GetSection returns the section the row belongs to (e.g. header or body).
func (gr *GridRow) GetSection() GridRowSection { return gr.section }

// NewCell makes a new single cell and inserts it into the row at the current position.
func (gr *GridRow) NewCell() (*GridCell, error) { return gr.NewMultiCell(1, 1) }

func (gr *GridRow) updateRowHeight(totalWidth float64) {
	gr.contentHeight = gr.height
	for _, cell := range gr.cells {
		width := cell.width(gr.grid.colWidths, totalWidth)
		height := cell.height(width)
		if height > gr.contentHeight {
			gr.contentHeight = height
		}
	}
}

// AddTag adds the table row tag for Grid to the root K object.
func (gr *GridRow) AddTag(rootKObj *model.KDict) {
	if rootKObj == nil {
		common.Log.Debug("Table: AddTag called with nil rootKObj, skipping tagging.")
		return
	}
	if gr.tagInfo == nil {
		gr.tagInfo = model.NewStructureTagInfo()
		gr.tagInfo.StructureType = model.StructureTypeTableRow
	}
	gr.tagInfo.ParentKObj = rootKObj
	gr.tagInfo.ComponentKObj = gr.tagInfo.NewComponentKObj(gr.tagInfo.StructureType)
	gr.tagInfo.ParentKObj.AddKChild(gr.tagInfo.ComponentKObj)
}

// SetHeight sets the custom height for the row.
func (gr *GridRow) SetHeight(h float64) { gr.height = h }

// NewMultiCell makes a new cell with given colspan and rowspan and inserts it into the row at the current position.
func (gr *GridRow) NewMultiCell(colspan, rowspan int) (*GridCell, error) {
	cell := &GridCell{col: len(gr.cells), row: gr.index, colSpan: colspan, rowSpan: rowspan}
	cell.row = gr.index
	cell.col = 0
	for _, cell2 := range gr.cells {
		cell.col += cell2.colSpan
	}
	for i, row := range gr.grid.rows {
		if i == gr.index+1 {
			break
		}
		for _, cell2 := range row.cells {
			if cell2.row+cell2.rowSpan > cell.row {
				if cell2.col+cell2.colSpan > cell.col {
					cell.col = cell2.col + cell2.colSpan
				}
			}
		}
	}
	if cell.col >= gr.grid.cols {
		return nil, errors.New("can't add any more cells to this row")
	}
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
	gr.cells = append(gr.cells, cell)
	cell.structType = model.StructureTypeTableData
	return cell, nil
}

// CellBorderStyle defines the table cell's border style.
type CellBorderStyle int

const (
	CellBorderStyleNone CellBorderStyle = iota
	CellBorderStyleSingle
	CellBorderStyleDouble
)

// CellVerticalAlignment defines the table cell's vertical alignment.
type CellVerticalAlignment int

const (
	CellVerticalAlignmentTop CellVerticalAlignment = iota
	CellVerticalAlignmentMiddle
	CellVerticalAlignmentBottom
)

// CellHorizontalAlignment defines the table cell's horizontal alignment.
type (
	CellHorizontalAlignment int
	imageMask               struct {
		kind      imageMaskKind
		img       *model.Image
		threshold *uint8
		colorKey  []int
	}
)

const (
	CellHorizontalAlignmentLeft CellHorizontalAlignment = iota
	CellHorizontalAlignmentCenter
	CellHorizontalAlignmentRight
)

// CellBorderSide defines the table cell's border side.
type CellBorderSide int

const (
	CellBorderSideLeft CellBorderSide = iota
	CellBorderSideRight
	CellBorderSideTop
	CellBorderSideBottom
	CellBorderSideAll
)

type subtableSplit struct {
	index  int
	table  *Table
	ctx    DrawContext
	height float64
}
