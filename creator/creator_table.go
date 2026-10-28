package creator

import (
	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
)

// TableCell defines a table cell which can contain a Drawable as content.
type TableCell struct {
	taggedDrawable
	backgroundColor     Color
	opacity             float64
	borderLineStyle     contentdraw.LineStyle
	borderStyleLeft     CellBorderStyle
	borderColorLeft     Color
	borderWidthLeft     float64
	borderStyleBottom   CellBorderStyle
	borderColorBottom   Color
	borderWidthBottom   float64
	borderStyleRight    CellBorderStyle
	borderColorRight    Color
	borderWidthRight    float64
	borderStyleTop      CellBorderStyle
	borderColorTop      Color
	borderWidthTop      float64
	row, col            int
	rowSpan             int
	colSpan             int
	wrapDisabled        bool
	content             VectorDrawable
	horizontalAlignment CellHorizontalAlignment
	verticalAlignment   CellVerticalAlignment
	indent              float64
	table               *Table
}

// SetContent sets the cell's content.  The content is a VectorDrawable, i.e.
// a Drawable with a known height and width.
// Currently supported VectorDrawables:
// - *StyledParagraph
// - *Image
// - *Chart
// - *Table
// - *Division
// - *List
// - *Rectangle
// - *Ellipse
// - *Line
func (tc *TableCell) SetContent(vd VectorDrawable) error {
	switch vd2 := vd.(type) {
	case *StyledParagraph:
		if vd2.defaultWrap {
			vd2.enableWrap = true
		}
		tc.content = vd
	case *Image, *Chart, *Table, *Division, *List, *Rectangle, *Ellipse, *Line:
		tc.content = vd
	default:
		common.Log.Debug("ERROR: unsupported cell content type %T", vd)
		return core.ErrTypeError
	}
	return nil
}

// Width returns the cell's width based on the input draw context.
func (tc *TableCell) Width(ctx DrawContext) float64 {
	colWidth := float64(0.0)
	for i := 0; i < tc.colSpan; i++ {
		colWidth += tc.table.colWidths[tc.col+i-1]
	}
	width := ctx.Width * colWidth
	return width
}

func (tc *TableCell) cloneProps(content VectorDrawable) *TableCell {
	props := *tc
	props.content = content
	return &props
}

// GetIndent get the cell's left indent.
func (tc *TableCell) GetIndent() float64 { return tc.indent }

// SetColPosition sets cell column position.
func (tc *TableCell) SetColPosition(col int) { tc.col = col }

// GetSideBorderWidth returns the cell's border width for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (tc *TableCell) GetSideBorderWidth(side CellBorderSide) float64 {
	switch side {
	case CellBorderSideBottom:
		return tc.borderWidthBottom
	case CellBorderSideLeft:
		return tc.borderWidthLeft
	case CellBorderSideRight:
		return tc.borderWidthRight
	default:
		return tc.borderWidthTop
	}
}

// GetRowspan returns the number of rows the cell spans.
func (tc *TableCell) GetRowspan() int { return tc.rowSpan }

// SetBorder sets the cell's border style.
func (tc *TableCell) SetBorder(side CellBorderSide, style CellBorderStyle, width float64) {
	if style == CellBorderStyleSingle && side == CellBorderSideAll {
		tc.borderStyleLeft = CellBorderStyleSingle
		tc.borderWidthLeft = width
		tc.borderStyleBottom = CellBorderStyleSingle
		tc.borderWidthBottom = width
		tc.borderStyleRight = CellBorderStyleSingle
		tc.borderWidthRight = width
		tc.borderStyleTop = CellBorderStyleSingle
		tc.borderWidthTop = width
	} else if style == CellBorderStyleDouble && side == CellBorderSideAll {
		tc.borderStyleLeft = CellBorderStyleDouble
		tc.borderWidthLeft = width
		tc.borderStyleBottom = CellBorderStyleDouble
		tc.borderWidthBottom = width
		tc.borderStyleRight = CellBorderStyleDouble
		tc.borderWidthRight = width
		tc.borderStyleTop = CellBorderStyleDouble
		tc.borderWidthTop = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideLeft {
		tc.borderStyleLeft = style
		tc.borderWidthLeft = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideBottom {
		tc.borderStyleBottom = style
		tc.borderWidthBottom = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideRight {
		tc.borderStyleRight = style
		tc.borderWidthRight = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideTop {
		tc.borderStyleTop = style
		tc.borderWidthTop = width
	}
}

// GetSideBorderColor returns the cell's border color for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (tc *TableCell) GetSideBorderColor(side CellBorderSide) Color {
	switch side {
	case CellBorderSideBottom:
		return tc.borderColorBottom
	case CellBorderSideLeft:
		return tc.borderColorLeft
	case CellBorderSideRight:
		return tc.borderColorRight
	default:
		return tc.borderColorTop
	}
}

// SetSideBorderColor sets the cell's side border color.
func (tc *TableCell) SetSideBorderColor(side CellBorderSide, col Color) {
	switch side {
	case CellBorderSideAll:
		tc.borderColorTop = col
		tc.borderColorBottom = col
		tc.borderColorLeft = col
		tc.borderColorRight = col
	case CellBorderSideTop:
		tc.borderColorTop = col
	case CellBorderSideBottom:
		tc.borderColorBottom = col
	case CellBorderSideLeft:
		tc.borderColorLeft = col
	case CellBorderSideRight:
		tc.borderColorRight = col
	}
}

// GetPosition returns the 1-based row and column the cell starts from.
// Consistent with GridCell.GetPosition.
func (tc *TableCell) GetPosition() (row, col int) { return tc.row, tc.col }

// GetTable returns the table the cell belongs to.
func (tc *TableCell) GetTable() *Table { return tc.table }

// GetContent returns the cell's content. Returns nil if no content has been
// set.
func (tc *TableCell) GetContent() VectorDrawable { return tc.content }

// GetBorderLineStyle returns the cell's border line style.
func (tc *TableCell) GetBorderLineStyle() contentdraw.LineStyle { return tc.borderLineStyle }

// SetRowPosition sets cell row position.
func (tc *TableCell) SetRowPosition(row int) { tc.row = row }

// SetIndent sets the cell's left indent.
func (tc *TableCell) SetIndent(indent float64) { tc.indent = indent }

var tableCellAttrs = map[string]attrApplyFunc{"colspan": nil, "rowspan": nil, "indent": floatAttr(func(cell *TableCell, indent float64) { cell.SetIndent(indent) }), "align": enumAttr(cellHorizontalAlignmentValues, func(cell *TableCell, halign CellHorizontalAlignment) { cell.SetHorizontalAlignment(halign) }), "vertical-align": enumAttr(cellVerticalAlignmentValues, func(cell *TableCell, valign CellVerticalAlignment) { cell.SetVerticalAlignment(valign) }), "border-style": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) { cell.SetSideBorderStyle(CellBorderSideAll, style) }), "border-style-top": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideTop, style)
}), "border-style-bottom": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideBottom, style)
}), "border-style-left": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideLeft, style)
}), "border-style-right": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideRight, style)
}), "border-width": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideAll, width) }), "border-width-top": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideTop, width) }), "border-width-bottom": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideBottom, width) }), "border-width-left": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideLeft, width) }), "border-width-right": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideRight, width) }), "border-color": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideAll, col) }), "border-color-top": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideTop, col) }), "border-color-bottom": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideBottom, col) }), "border-color-left": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideLeft, col) }), "border-color-right": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideRight, col) }), "border-line-style": enumAttr(lineStyleValues, func(cell *TableCell, style contentdraw.LineStyle) { cell.SetBorderLineStyle(style) }), "background-color": colorAttr(func(cell *TableCell, col Color) { cell.SetBackgroundColor(col) })}

// SetBackgroundColor sets the cell's background color.
func (tc *TableCell) SetBackgroundColor(col Color) { tc.backgroundColor = col }

// SetBorderLineStyle sets border style (currently dashed or plain).
func (tc *TableCell) SetBorderLineStyle(style contentdraw.LineStyle) { tc.borderLineStyle = style }

// GetBackgroundColor returns the cell's background color.
func (tc *TableCell) GetBackgroundColor() Color { return tc.backgroundColor }

// GetHorizontalAlignment returns the cell's horizontal alignment of content.
func (tc *TableCell) GetHorizontalAlignment() CellHorizontalAlignment { return tc.horizontalAlignment }

// SetBorderColor sets the cell's border color.
func (tc *TableCell) SetBorderColor(col Color) {
	tc.borderColorLeft = col
	tc.borderColorBottom = col
	tc.borderColorRight = col
	tc.borderColorTop = col
}

// SetOpacity sets the cell's opacity in the range 0-1.
func (tc *TableCell) SetOpacity(opacity float64) { tc.opacity = opacity }

// GetColspan returns the number of columns the cell spans.
func (tc *TableCell) GetColspan() int { return tc.colSpan }

// GetOpacity returns the cell's opacity in the range 0-1.
func (tc *TableCell) GetOpacity() float64 { return tc.opacity }

// GetVerticalAlignment returns the cell's vertical alignment of content.
func (tc *TableCell) GetVerticalAlignment() CellVerticalAlignment { return tc.verticalAlignment }

func (tc *TableCell) height(width float64) float64 {
	var bottom float64
	switch content := tc.content.(type) {
	case *StyledParagraph:
		if content.enableWrap {
			content.SetWidth(width - tc.indent - content.margins.Left - content.margins.Right)
		}
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
		if !tc.table.compactMode {
			bottom += (0.5 * content.getTextHeight())
		}
	case *Image:
		content.applyFitMode(width - tc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Table:
		content.updateRowHeights(width - tc.indent - content.margins.Left - content.margins.Right)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *List:
		bottom = content.ctxHeight(width-tc.indent) + content.margins.Top + content.margins.Bottom
	case *Division:
		bottom = content.ctxHeight(width-tc.indent) + content.margins.Top + content.margins.Bottom + content.padding.Top + content.padding.Bottom
	case *Chart:
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Rectangle:
		content.applyFitMode(width - tc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom + content.borderWidth
	case *Ellipse:
		content.applyFitMode(width - tc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Line:
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	}
	return bottom
}

// SetSideBorderStyle sets the cell's side border style.
func (tc *TableCell) SetSideBorderStyle(side CellBorderSide, style CellBorderStyle) {
	switch side {
	case CellBorderSideAll:
		tc.borderStyleTop = style
		tc.borderStyleBottom = style
		tc.borderStyleLeft = style
		tc.borderStyleRight = style
	case CellBorderSideTop:
		tc.borderStyleTop = style
	case CellBorderSideBottom:
		tc.borderStyleBottom = style
	case CellBorderSideLeft:
		tc.borderStyleLeft = style
	case CellBorderSideRight:
		tc.borderStyleRight = style
	}
}

func parseTableCellTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseTableCell(node)
}

// SetHorizontalAlignment sets the cell's horizontal alignment of content.
// Can be one of:
// - CellHorizontalAlignmentLeft
// - CellHorizontalAlignmentCenter
// - CellHorizontalAlignmentRight
func (tc *TableCell) SetHorizontalAlignment(halign CellHorizontalAlignment) {
	tc.horizontalAlignment = halign
}

// SetVerticalAlignment set the cell's vertical alignment of content.
// Can be one of:
// - CellHorizontalAlignmentTop
// - CellHorizontalAlignmentMiddle
// - CellHorizontalAlignmentBottom
func (tc *TableCell) SetVerticalAlignment(valign CellVerticalAlignment) {
	tc.verticalAlignment = valign
}

func (tc *TableCell) width(colWidths []float64, totalWidth float64) float64 {
	width := float64(0.0)
	for i := 0; i < tc.colSpan; i++ {
		width += colWidths[tc.col+i-1]
	}
	return width * totalWidth
}

// SetSideBorderWidth sets the cell's side border width.
func (tc *TableCell) SetSideBorderWidth(side CellBorderSide, width float64) {
	switch side {
	case CellBorderSideAll:
		tc.borderWidthTop = width
		tc.borderWidthBottom = width
		tc.borderWidthLeft = width
		tc.borderWidthRight = width
	case CellBorderSideTop:
		tc.borderWidthTop = width
	case CellBorderSideBottom:
		tc.borderWidthBottom = width
	case CellBorderSideLeft:
		tc.borderWidthLeft = width
	case CellBorderSideRight:
		tc.borderWidthRight = width
	}
}

// GetSideBorderStyle returns the cell's border style for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (tc *TableCell) GetSideBorderStyle(side CellBorderSide) CellBorderStyle {
	switch side {
	case CellBorderSideBottom:
		return tc.borderStyleBottom
	case CellBorderSideLeft:
		return tc.borderStyleLeft
	case CellBorderSideRight:
		return tc.borderStyleRight
	default:
		return tc.borderStyleTop
	}
}
