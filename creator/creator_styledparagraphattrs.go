// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"fmt"
	"reflect"
	"strings"
	"text/template/parse"

	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
)

var styledParagraphAttrs = map[string]attrApplyFunc{"text-align": enumAttr(textAlignmentValues, func(para *StyledParagraph, align TextAlignment) { para.SetTextAlignment(align) }), "vertical-text-align": enumAttr(textVerticalAlignmentValues, func(para *StyledParagraph, align TextVerticalAlignment) { para.SetTextVerticalAlignment(align) }), "line-height": floatAttr(func(para *StyledParagraph, lineheight float64) { para.SetLineHeight(lineheight) }), "margin": marginAttr(func(para *StyledParagraph, margins Margins) {
	para.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "enable-wrap": boolAttr(func(para *StyledParagraph, enableWrap bool) { para.SetEnableWrap(enableWrap) }), "enable-word-wrap": boolAttr(func(para *StyledParagraph, val bool) { para.EnableWordWrap(val) }), "text-overflow": enumAttr(textOverflowValues, func(para *StyledParagraph, textOverflow TextOverflow) { para.SetTextOverflow(textOverflow) }), "x": floatAttr(func(para *StyledParagraph, x float64) { para.SetPos(x, para.yPos) }), "y": floatAttr(func(para *StyledParagraph, y float64) { para.SetPos(para.xPos, y) }), "angle": floatAttr(func(para *StyledParagraph, angle float64) { para.SetAngle(angle) })}

func (tp *templateProcessor) collectStyles(node *templateNode) error {
	tp.classStyles = map[string][]xml.Attr{}
	return tp.collectStylesNode(node)
}

// NewRadialGradientColor creates a radial gradient color that could act as a color in other componenents.
// Note: The innerRadius must be smaller than outerRadius for the circle to render properly.
func (c *Creator) NewRadialGradientColor(x float64, y float64, innerRadius float64, outerRadius float64, colorPoints []*ColorPoint) *RadialShading {
	return newRadialShading(x, y, innerRadius, outerRadius, colorPoints)
}

func pipeEndsWithRaw(pipeNode *parse.PipeNode) bool {
	if pipeNode == nil || len(pipeNode.Cmds) == 0 {
		return false
	}
	cmd := pipeNode.Cmds[len(pipeNode.Cmds)-1]
	if len(cmd.Args) == 0 {
		return false
	}
	switch arg := cmd.Args[0].(type) {
	case *parse.IdentifierNode:
		return arg.Ident == rawFuncName
	case *parse.PipeNode:
		return len(cmd.Args) == 1 && pipeEndsWithRaw(arg)
	}
	return false
}
func parseFloatArray(s string) ([]float64, error) { return parseFloatFields(s) }

// Height returns the height of the ellipse.
func (e *Ellipse) Height() float64 { return e.height }

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

// SetCoords sets the upper left corner coordinates of the rectangle.
func (r *Rectangle) SetCoords(x, y float64) { r.x = x; r.y = y }

// MoveDown moves the drawing context down by relative displacement dy (negative goes up).
func (c *Creator) MoveDown(dy float64) { c.context.Y += dy }

// DashPattern returns the dash pattern of the line.
func (l *Line) DashPattern() (dashArray []int64, dashPhase int64) { return l.dashArray, l.dashPhase }

// GetPos returns the absolute coordinates (x, y) of the table's upper-left
// corner. The coordinates are only meaningful when the table uses absolute
// positioning (see GetPositioning).
func (t *Table) GetPos() (float64, float64) { return t.xPos, t.yPos }

func mergeSubtableSplits(subtableSplits []subtableSplit) ([][2]*Table, bool) {
	nums := make([]int, len(subtableSplits))
	nums2 := make([]int, len(subtableSplits))
	nums3 := make([]int, len(subtableSplits))
	num := -1
	for i, subtableSplit := range subtableSplits {
		nums[i] = subtableSplit.table.topHeaderRows()
		nums2[i] = subtableSplit.table.rows - nums[i]
		nums3[i] = max(subtableSplit.table.maxFittingRows(subtableSplit.ctx.Width, subtableSplit.ctx.Height)-nums[i], 0)
		if nums3[i] < nums2[i] && (num < 0 || nums3[i] < num) {
			num = nums3[i]
		}
	}
	if num < 0 {
		return nil, false
	}
	for ok := true; ok; {
		ok = false
		for i, subtableSplit := range subtableSplits {
			num2 := min(num, nums2[i])
			if num3 := max(subtableSplit.table.clampCut(num2+nums[i])-nums[i], 0); num3 < num2 {
				num = num3
				ok = true
			}
		}
	}
	tables := make([]*Table, len(subtableSplits))
	nums4 := make([]int, len(subtableSplits))
	ok := true
	for i, subtableSplit := range subtableSplits {
		tables[i], nums4[i] = subtableSplit.table, num+1+nums[i]
		switch {
		case num >= nums2[i]:
			nums4[i] = subtableSplit.table.rows
		case nums3[i] > num:
			if subtableSplit.table.clampCut(nums4[i]) != nums4[i] {
				ok = false
			}
		default:
			if tables[i] = subtableSplit.table.withPartialRow(num+nums[i], subtableSplit.ctx, subtableSplit.height); tables[i] == nil {
				ok = false
			}
		}
		if !ok {
			break
		}
	}
	if !ok {
		for i, subtableSplit := range subtableSplits {
			tables[i], nums4[i] = subtableSplit.table, min(num+nums[i], subtableSplit.table.rows)
		}
	}
	rows := make([][2]*Table, len(subtableSplits))
	for i := range subtableSplits {
		head, tail := tables[i].splitAtRow(tables[i].headerCut(nums4[i]))
		rows[i] = [2]*Table{head, tail}
	}
	return rows, true
}

// ClearAnnotations clears any existing annotations.
func (tec *TextChunk) ClearAnnotations() { tec.annotations = []*model.PdfAnnotation{} }

// SetOpacity sets the cell's opacity in the range 0-1.
func (tc *TableCell) SetOpacity(opacity float64) { tc.opacity = opacity }

// EnableRowWrap controls whether rows are wrapped across pages.
// It also selects how a row-spanning cell taller than its rows is laid out:
// with wrapping enabled the overflow goes to the last spanned row, which is
// then split across pages; without it the overflow is distributed evenly
// across the spanned rows.
// Rows are wrapped by splitting StyledParagraph, Division, List and Table
// content; subtables are split at row boundaries.
func (t *Table) EnableRowWrap(enable bool) { t.enableRowWrap = enable }

type attrApplyFunc func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error

var chartAttrs = map[string]attrApplyFunc{"src": nil, "x": floatAttr(func(chart *Chart, x float64) { chart.SetPos(x, chart.yPos) }), "y": floatAttr(func(chart *Chart, y float64) { chart.SetPos(chart.xPos, y) }), "margin": marginAttr(func(chart *Chart, margins Margins) {
	chart.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "width": floatAttr(func(chart *Chart, value float64) { chart.chart.SetWidth(int(value)) }), "height": floatAttr(func(chart *Chart, value float64) { chart.chart.SetHeight(int(value)) })}

// DropCapsType represents the type of drop caps to apply to a paragraph.
// Note: Drop caps are currently not supported for RTL (right-to-left) text and will be
// automatically disabled with a debug log if applied to RTL content.
type DropCapsType int

// NewCell makes a new single cell and inserts it into the row at the current position.
func (gr *GridRow) NewCell() (*GridCell, error) { return gr.NewMultiCell(1, 1) }
func isBlank(s string) bool                     { return strings.Trim(s, whitespaceChars) == "" }

// FilledCurve represents a closed path of Bezier curves with a border and fill.
type FilledCurve struct {
	taggedDrawable
	curves        []contentdraw.CubicBezierCurve
	FillEnabled   bool
	fillColor     Color
	BorderEnabled bool
	BorderWidth   float64
	borderColor   Color
}

var (
	stringerType = reflect.TypeFor[fmt.Stringer]()
	errorType    = reflect.TypeFor[error]()
)

// Width returns the width of the ellipse.
func (e *Ellipse) Width() float64 { return e.width }

func (tp *templateProcessor) tableColumns(node *templateNode) (int64, error) {
	var tableColumns int64
	if effectiveAttrValue, ok := tp.effectiveAttrValue(node, "columns"); ok {
		int642, err := parseInt64(effectiveAttrValue)
		if err != nil {
			if err2 := tp.strictErr(node, "columns", effectiveAttrValue, err); err2 != nil {
				return 0, err2
			}
			int642 = 0
		}
		tableColumns = int642
	}
	if tableColumns <= 0 {
		if tp.options.Strict {
			return 0, tp.nodeError(node, "table columns must be a positive number, got %d", tableColumns)
		}
		tp.nodeLogDebug(node, "Invalid number of table columns: %d. Setting to 1. Output may be incorrect.", tableColumns)
		tableColumns = 1
	}
	return tableColumns, nil
}

// SetHeight sets the Image's document height to specified h.
func (im *Image) SetHeight(h float64) { im.height = h }

// SetOptimizer sets the optimizer to optimize PDF before writing.
func (c *Creator) SetOptimizer(optimizer model.Optimizer) { c.optimizer = optimizer }

// SetStyle sets paragraph style for all chunks.
func (sp *StyledParagraph) SetStyle(style TextStyle) {
	sp.defaultStyle = style
	for _, chunk := range sp.chunks {
		chunk.Style = style
	}
}

// SetLazy sets the lazy mode for the image.
func (im *Image) SetLazy(lazy bool) { im.lazy = lazy }

// SetHorizontalAlignment sets the cell's horizontal alignment of content.
// Can be one of:
// - CellHorizontalAlignmentLeft
// - CellHorizontalAlignmentCenter
// - CellHorizontalAlignmentRight
func (gc *GridCell) SetHorizontalAlignment(halign CellHorizontalAlignment) {
	gc.horizontalAlignment = halign
}

// SetMargins sets the margins for the Image (in relative mode): left, right, top, bottom.
func (im *Image) SetMargins(left, right, top, bottom float64) {
	im.margins.Left = left
	im.margins.Right = right
	im.margins.Top = top
	im.margins.Bottom = bottom
}

// DrawHeader sets a function to draw a header on created output pages.
func (c *Creator) DrawHeader(drawHeaderFunc func(block *Block, headerFunctionArgs HeaderFunctionArgs)) {
	c.drawHeaderFunc = drawHeaderFunc
}
