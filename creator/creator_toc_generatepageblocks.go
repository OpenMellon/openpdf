// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/internal/graphic2d"
	"github.com/OpenMellon/openpdf/model"
)

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (to *TOC) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	pageBlocks, ctx, err := to.heading.GeneratePageBlocks(ctx)
	if err != nil {
		return pageBlocks, ctx, err
	}
	for _, line := range to.lines {
		linkPage := line.linkPage
		if !to.showLinks {
			line.linkPage = 0
		}
		pageBlocks2, ctx2, err := line.GeneratePageBlocks(ctx)
		line.linkPage = linkPage
		if err != nil {
			return pageBlocks, ctx, err
		}
		if len(pageBlocks2) < 1 {
			continue
		}
		pageBlocks[len(pageBlocks)-1].mergeBlocks(pageBlocks2[0])
		pageBlocks = append(pageBlocks, pageBlocks2[1:]...)
		ctx = ctx2
	}
	if to.positioning.IsRelative() {
		ctx.X = ctx2.X
	}
	if to.positioning.IsAbsolute() {
		return pageBlocks, ctx2, nil
	}
	return pageBlocks, ctx, nil
}

// SetTitle sets the title of the invoice.
func (i *Invoice) SetTitle(title string) { i.title = title }

func intArrayAttr(i any) attrApplyFunc {
	value2 := validateSetter("intArrayAttr", i, 0, int64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		int64List, err := parseInt64List(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(int64List))
		return nil
	}
}

// ValidateTemplate checks the parsed template against the specified `data`
// without drawing anything, reporting unknown tags, invalid nesting and, in
// strict mode, attribute errors. See ParsedTemplate for the checks it leaves to
// RenderTemplate. The `data` parameter can be nil.
func (c *Creator) ValidateTemplate(pt *ParsedTemplate, data any) error {
	return pt.validateWith(c, c, data)
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

// Number returns the invoice number description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Number() (*InvoiceCell, *InvoiceCell) {
	return i.number[0], i.number[1]
}

// GetBorderLineStyle returns the cell's border line style.
func (gc *GridCell) GetBorderLineStyle() contentdraw.LineStyle { return gc.borderLineStyle }

// SetBorderRadius sets the radius of the background corners.
func (ba *Background) SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight float64) {
	ba.BorderRadiusTopLeft = topLeft
	ba.BorderRadiusTopRight = topRight
	ba.BorderRadiusBottomLeft = bottomLeft
	ba.BorderRadiusBottomRight = bottomRight
}

func (b *Block) duplicate() *Block {
	block := &Block{}
	*block = *b
	contentStreamOperations := contentstream.ContentStreamOperations{}
	contentStreamOperations = append(contentStreamOperations, *b.contents...)
	block.contents = &contentStreamOperations
	return block
}

func (tc *TableCell) width(colWidths []float64, totalWidth float64) float64 {
	width := float64(0.0)
	for i := 0; i < tc.colSpan; i++ {
		width += colWidths[tc.col+i-1]
	}
	return width * totalWidth
}

func (l *Line) computeCoords(ctx DrawContext) (x1, y1, x2, y2 float64) {
	x1 = ctx.X
	x2 = x1 + l.x2 - l.x1
	lineWidth := l.lineWidth
	if l.x1 == l.x2 {
		lineWidth /= 2
	}
	if l.y1 < l.y2 {
		y1 = ctx.PageHeight - ctx.Y - lineWidth
		y2 = y1 - l.y2 + l.y1
	} else {
		y2 = ctx.PageHeight - ctx.Y - lineWidth
		y1 = y2 - l.y1 + l.y2
	}
	switch l.fitMode {
	case FitModeFillWidth:
		x2 = x1 + ctx.Width
	}
	return x1, y1, x2, y2
}

func (tp *templateProcessor) nodeOwnStyle(node *templateNode) map[string]string {
	var m map[string]string
	fn := func(s2, s string) {
		if _, ok := carrierAttrs[s2]; !ok {
			return
		}
		if s2 != "color" || !isGradientExpr(s) {
			if err := tp.typographyValueErr(s2, s); err != nil {
				tp.nodeLogDebug(node, "Ignoring invalid inherited %s value %q: %v.", s2, s, err)
				return
			}
		}
		if m == nil {
			m = map[string]string{}
		}
		m[s2] = s
	}
	for _, s := range node.classNames() {
		for _, attr := range tp.classStyles[s] {
			fn(attr.Name.Local, attr.Value)
		}
	}
	for _, attr := range node.start.Attr {
		if attr.Name.Space != "" {
			continue
		}
		fn(attr.Name.Local, attr.Value)
	}
	return m
}

// Subtotal returns the invoice subtotal description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Subtotal() (*InvoiceCell, *InvoiceCell) {
	return i.subtotal[0], i.subtotal[1]
}

var lineAttrs = map[string]attrApplyFunc{"x1": floatAttr(func(line *Line, x1 float64) { line.x1 = x1 }), "y1": floatAttr(func(line *Line, y1 float64) { line.y1 = y1 }), "x2": floatAttr(func(line *Line, x2 float64) { line.x2 = x2 }), "y2": floatAttr(func(line *Line, y float64) { line.y2 = y }), "thickness": floatAttr(func(line *Line, width float64) { line.SetLineWidth(width) }), "color": colorAttr(func(line *Line, color Color) { line.SetColor(color) }), "style": enumAttr(lineStyleValues, func(line *Line, style contentdraw.LineStyle) { line.SetStyle(style) }), "dash-array": intArrayAttr(func(line *Line, dashArray []int64) { line.SetDashPattern(dashArray, line.dashPhase) }), "dash-phase": intAttr(func(line *Line, dashPhase int64) { line.SetDashPattern(line.dashArray, dashPhase) }), "opacity": floatAttr(func(line *Line, opacity float64) { line.SetOpacity(opacity) }), "position": enumAttr(positioningValues, func(line *Line, positioning Positioning) { line.SetPositioning(positioning) }), "fit-mode": enumAttr(fitModeValues, func(line *Line, fitMode FitMode) { line.SetFitMode(fitMode) }), "margin": marginAttr(func(line *Line, margins Margins) {
	line.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

func newDivision() *Division {
	return &Division{enableBreak: true, taggedDrawable: taggedDrawable{structType: model.StructureTypeDivision}}
}

func (gse *GraphicSVGElement) drawRect(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	svgLength, err := parseSVGLength(gse.Attributes["x"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `x` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["y"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `y` value: %v", err.Error())
	}
	svgLength3, err := parseSVGLength(gse.Attributes["width"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing stroke width value: %v", err.Error())
	}
	svgLength4, err := parseSVGLength(gse.Attributes["height"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing stroke height value: %v", err.Error())
	}
	cc.Add_re(svgLength*gse.scale, svgLength2*gse.scale, svgLength3*gse.scale, svgLength4*gse.scale)
	gse.Style.fillStroke(cc)
	cc.Add_Q()
}

const (
	DropCapsFirstCharacter DropCapsScope = iota
	DropCapsFirstWord
)

// LinearShading holds data for rendering a linear shading gradient.
type LinearShading struct {
	base        *shading
	boundingBox *model.PdfRectangle
	angle       float64
}
type fontMetrics struct {
	capHeight float64
	xHeight   float64
	ascent    float64
	descent   float64
}

func (gse *GraphicSVGElement) parseColorPoints() []*ColorPoint {
	var colorPoints []*ColorPoint
	var err error
	for _, graphicSVGElement := range gse.Children {
		if graphicSVGElement.Name == "stop" {
			colorRGB := ColorBlack
			point := 0.0
			for attributeKey, attribute := range graphicSVGElement.Attributes {
				if attributeKey == "offset" {
					if before, ok := strings.CutSuffix(attribute, "%"); ok {
						float, err := strconv.ParseFloat(before, 64)
						if err != nil {
							continue
						}
						point = float / 100
					} else {
						point, err = strconv.ParseFloat(attribute, 64)
						if err != nil {
							continue
						}
					}
				} else if attributeKey == "stop-color" {
					if attribute[0] == '#' {
						colorRGB = ColorRGBFromHex(attribute)
					} else {
						rgba, ok := graphic2d.ColorMap[attribute]
						if ok {
							colorRGB = ColorRGBFrom8bit(rgba.R, rgba.G, rgba.B)
						}
					}
				}
			}
			colorPoints = append(colorPoints, NewColorPoint(colorRGB, point))
		}
	}
	return colorPoints
}

func templateNodeValue(node *templateNode) any {
	if node == nil {
		return nil
	}
	return node.value
}

var (
	PageSizeA3     = PageSize{297 * PPMM, 420 * PPMM}
	PageSizeA4     = PageSize{210 * PPMM, 297 * PPMM}
	PageSizeA5     = PageSize{148 * PPMM, 210 * PPMM}
	PageSizeLetter = PageSize{8.5 * PPI, 11 * PPI}
	PageSizeLegal  = PageSize{8.5 * PPI, 14 * PPI}
)
