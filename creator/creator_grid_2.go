package creator

import (
	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
)

// GridCell defines a cell which can contain a Drawable as content.
type GridCell struct {
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
	content             VectorDrawable
	row, col            int
	rowSpan             int
	colSpan             int
	horizontalAlignment CellHorizontalAlignment
	verticalAlignment   CellVerticalAlignment
	indent              float64
}

// GetHorizontalAlignment returns the cell's horizontal alignment of content.
func (gc *GridCell) GetHorizontalAlignment() CellHorizontalAlignment { return gc.horizontalAlignment }

// GetPosition returns the 1-based row and column the cell starts from.
// Consistent with TableCell.GetPosition.
func (gc *GridCell) GetPosition() (row, col int) {
	return gc.row + 1, gc.col + 1
}

// SetVerticalAlignment set the cell's vertical alignment of content.
// Can be one of:
// - CellHorizontalAlignmentTop
// - CellHorizontalAlignmentMiddle
// - CellHorizontalAlignmentBottom
func (gc *GridCell) SetVerticalAlignment(valign CellVerticalAlignment) { gc.verticalAlignment = valign }

// SetIndent sets the cell's left indent.
func (gc *GridCell) SetIndent(indent float64) { gc.indent = indent }

// GetRowspan returns the number of rows the cell spans.
func (gc *GridCell) GetRowspan() int { return gc.rowSpan }

// SetOpacity sets the cell's opacity in the range 0-1.
func (gc *GridCell) SetOpacity(opacity float64) { gc.opacity = opacity }

func (gc *GridCell) width(colWidths []float64, totalWidth float64) float64 {
	width := float64(0.0)
	for i := 0; i < gc.colSpan; i++ {
		width += colWidths[gc.col+i]
	}
	return width * totalWidth
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
func (gc *GridCell) SetContent(vd VectorDrawable) error {
	switch vd2 := vd.(type) {
	case *StyledParagraph:
		if vd2.defaultWrap {
			vd2.enableWrap = true
		}
		gc.content = vd
	case *Image, *Chart, *Table, *Division, *List, *Rectangle, *Ellipse, *Line:
		gc.content = vd
	default:
		common.Log.Debug("ERROR: unsupported cell content type %T", vd)
		return core.ErrTypeError
	}
	return nil
}

// GetSideBorderColor returns the cell's border color for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (gc *GridCell) GetSideBorderColor(side CellBorderSide) Color {
	switch side {
	case CellBorderSideBottom:
		return gc.borderColorBottom
	case CellBorderSideLeft:
		return gc.borderColorLeft
	case CellBorderSideRight:
		return gc.borderColorRight
	default:
		return gc.borderColorTop
	}
}

// GetSideBorderWidth returns the cell's border width for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (gc *GridCell) GetSideBorderWidth(side CellBorderSide) float64 {
	switch side {
	case CellBorderSideBottom:
		return gc.borderWidthBottom
	case CellBorderSideLeft:
		return gc.borderWidthLeft
	case CellBorderSideRight:
		return gc.borderWidthRight
	default:
		return gc.borderWidthTop
	}
}

// GetColspan returns the number of columns the cell spans.
func (gc *GridCell) GetColspan() int { return gc.colSpan }

// SetBorderColor sets the cell's border color.
func (gc *GridCell) SetBorderColor(col Color) {
	gc.borderColorLeft = col
	gc.borderColorBottom = col
	gc.borderColorRight = col
	gc.borderColorTop = col
}

// SetSideBorderColor sets the cell's side border color.
func (gc *GridCell) SetSideBorderColor(side CellBorderSide, col Color) {
	switch side {
	case CellBorderSideAll:
		gc.borderColorTop = col
		gc.borderColorBottom = col
		gc.borderColorLeft = col
		gc.borderColorRight = col
	case CellBorderSideTop:
		gc.borderColorTop = col
	case CellBorderSideBottom:
		gc.borderColorBottom = col
	case CellBorderSideLeft:
		gc.borderColorLeft = col
	case CellBorderSideRight:
		gc.borderColorRight = col
	}
}

// GetVerticalAlignment returns the cell's vertical alignment of content.
func (gc *GridCell) GetVerticalAlignment() CellVerticalAlignment { return gc.verticalAlignment }

// SetBackgroundColor sets the cell's background color.
func (gc *GridCell) SetBackgroundColor(col Color) { gc.backgroundColor = col }

// GetOpacity returns the cell's opacity in the range 0-1.
func (gc *GridCell) GetOpacity() float64 { return gc.opacity }

// GetBackgroundColor returns the cell's background color.
func (gc *GridCell) GetBackgroundColor() Color { return gc.backgroundColor }

// SetHorizontalAlignment sets the cell's horizontal alignment of content.
// Can be one of:
// - CellHorizontalAlignmentLeft
// - CellHorizontalAlignmentCenter
// - CellHorizontalAlignmentRight
func (gc *GridCell) SetHorizontalAlignment(halign CellHorizontalAlignment) {
	gc.horizontalAlignment = halign
}

func (gc *GridCell) height(value float64) float64 {
	var bottom float64
	switch content := gc.content.(type) {
	case *StyledParagraph:
		if content.enableWrap {
			content.SetWidth(value - gc.indent - content.margins.Left - content.margins.Right)
		}
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Image:
		content.applyFitMode(value - gc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Table:
		content.updateRowHeights(value - gc.indent - content.margins.Left - content.margins.Right)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *List:
		bottom = content.ctxHeight(value-gc.indent) + content.margins.Top + content.margins.Bottom
	case *Division:
		bottom = content.ctxHeight(value-gc.indent) + content.margins.Top + content.margins.Bottom + content.padding.Top + content.padding.Bottom
	case *Chart:
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Rectangle:
		content.applyFitMode(value - gc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom + content.borderWidth
	case *Ellipse:
		content.applyFitMode(value - gc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Line:
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	}
	return bottom
}

// GetContent returns the cell's content. Returns nil if no content has been
// set.
func (gc *GridCell) GetContent() VectorDrawable { return gc.content }

// GetIndent get the cell's left indent.
func (gc *GridCell) GetIndent() float64 { return gc.indent }

// SetBorder sets the cell's border style.
func (gc *GridCell) SetBorder(side CellBorderSide, style CellBorderStyle, width float64) {
	if style == CellBorderStyleSingle && side == CellBorderSideAll {
		gc.borderStyleLeft = CellBorderStyleSingle
		gc.borderWidthLeft = width
		gc.borderStyleBottom = CellBorderStyleSingle
		gc.borderWidthBottom = width
		gc.borderStyleRight = CellBorderStyleSingle
		gc.borderWidthRight = width
		gc.borderStyleTop = CellBorderStyleSingle
		gc.borderWidthTop = width
	} else if style == CellBorderStyleDouble && side == CellBorderSideAll {
		gc.borderStyleLeft = CellBorderStyleDouble
		gc.borderWidthLeft = width
		gc.borderStyleBottom = CellBorderStyleDouble
		gc.borderWidthBottom = width
		gc.borderStyleRight = CellBorderStyleDouble
		gc.borderWidthRight = width
		gc.borderStyleTop = CellBorderStyleDouble
		gc.borderWidthTop = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideLeft {
		gc.borderStyleLeft = style
		gc.borderWidthLeft = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideBottom {
		gc.borderStyleBottom = style
		gc.borderWidthBottom = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideRight {
		gc.borderStyleRight = style
		gc.borderWidthRight = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideTop {
		gc.borderStyleTop = style
		gc.borderWidthTop = width
	}
}

// GetSideBorderStyle returns the cell's border style for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (gc *GridCell) GetSideBorderStyle(side CellBorderSide) CellBorderStyle {
	switch side {
	case CellBorderSideBottom:
		return gc.borderStyleBottom
	case CellBorderSideLeft:
		return gc.borderStyleLeft
	case CellBorderSideRight:
		return gc.borderStyleRight
	default:
		return gc.borderStyleTop
	}
}

// GetBorderLineStyle returns the cell's border line style.
func (gc *GridCell) GetBorderLineStyle() contentdraw.LineStyle { return gc.borderLineStyle }

type GridRowSection int

const (
	GridRowSectionBody GridRowSection = iota
	GridRowSectionHeader
	GridRowSectionFooter
	GridRowSectionUnknown
)
