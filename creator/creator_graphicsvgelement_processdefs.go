// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"math"
	"reflect"

	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
)

func (gse *GraphicSVGElement) processDefs() {
	gse.linearShadings = make(map[string]*LinearShading)
	gse.radialShadings = make(map[string]*RadialShading)
	for _, graphicSVGElement := range gse.Children {
		if graphicSVGElement.Name == "defs" {
			for _, graphicSVGElement2 := range graphicSVGElement.Children {
				switch graphicSVGElement2.Name {
				case "linearGradient":
					attribute := graphicSVGElement2.Attributes["id"]
					colorPoints := graphicSVGElement2.parseColorPoints()
					linearShading := newLinearShading(colorPoints)
					gradientAngle := graphicSVGElement2.getGradientAngle()
					linearShading.SetAngle(-gradientAngle)
					linearShading.SetExtends(true, true)
					linearShading.SetBoundingBox(0, 0, gse.Width, gse.Height)
					gse.linearShadings[attribute] = linearShading
				case "radialGradient":
					attribute := graphicSVGElement2.Attributes["id"]
					colorPoints := graphicSVGElement2.parseColorPoints()
					radialShading := newRadialShading(gse.xPos, gse.ViewBox.H, 0, math.Min(gse.Width, gse.Height)/2, colorPoints)
					radialShading.SetExtends(true, true)
					radialShading.SetBoundingBox(0, 0, gse.Width, gse.Height)
					gse.radialShadings[attribute] = radialShading
				}
			}
		}
	}
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

var carrierAttrs = map[string]struct{}{"font": {}, "font-size": {}, "color": {}, "line-height": {}, "text-align": {}}

func (su *Subpath) compare(subpath *Subpath) bool {
	if len(su.Commands) != len(subpath.Commands) {
		return false
	}
	for i, command := range su.Commands {
		if !command.compare(subpath.Commands[i]) {
			return false
		}
	}
	return true
}

func (tn *templateNode) activeParent() *templateNode {
	for parent := tn.parent; parent != nil; parent = parent.parent {
		if parent.hasHandler {
			return parent
		}
	}
	return nil
}

// Height returns the total height of all rows.
func (g *Grid) Height() float64 {
	height := float64(0.0)
	for _, row := range g.rows {
		height += row.height
	}
	return height
}

// Command is a representation of an SVG path command and its parameters.
type Command struct {
	Symbol string
	Params []float64
}

// GetColumnWidths returns the fractional column widths of the grid. The
// fractions add up to 1. The returned slice references the grid's internal
// column widths; modifying its elements modifies the grid.
func (g *Grid) GetColumnWidths() []float64 { return g.colWidths }

// GetMargins returns the Paragraph's margins: left, right, top, bottom.
func (sp *StyledParagraph) GetMargins() (float64, float64, float64, float64) {
	return sp.margins.Left, sp.margins.Right, sp.margins.Top, sp.margins.Bottom
}

// SetPos sets the Block's positioning to absolute mode with the specified coordinates.
func (b *Block) SetPos(x, y float64) { b.positioning = PositionAbsolute; b.xPos = x; b.yPos = y }

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) SetStructPageNumber(pageNumber *int64) {}

func newLinearShading(colorPoints []*ColorPoint) *LinearShading {
	return &LinearShading{base: &shading{backgroundColor: ColorWhite, antiAlias: false, extends: []bool{false, false}, colorPoints: colorPoints}, boundingBox: &model.PdfRectangle{}}
}

// Curve represents a cubic Bezier curve with a control point.
type Curve struct {
	taggedDrawable
	x1    float64
	y1    float64
	cx    float64
	cy    float64
	x2    float64
	y2    float64
	color Color
	width float64
}

// SetLogo sets the logo of the invoice.
func (i *Invoice) SetLogo(logo *Image) { i.logo = logo }

func floatAttr(i any) attrApplyFunc {
	value2 := validateSetter("floatAttr", i, 0, float64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		f, err := parseFloat64(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
			f = 0
		}
		callSetter(value2, target, reflect.ValueOf(f))
		return nil
	}
}

// ValidateTemplate checks the parsed template against the specified `data`
// without drawing anything, reporting unknown tags, invalid nesting and, in
// strict mode, attribute errors. The `data` parameter can be nil.
func (b *Block) ValidateTemplate(c *Creator, pt *ParsedTemplate, data any) error {
	return pt.validateWith(c, b, data)
}

// Sections returns the custom content sections of the invoice as
// title-content pairs.
func (i *Invoice) Sections() [][2]string { return i.sections }

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

// TitleStyle returns the style properties used to render the invoice title.
func (i *Invoice) TitleStyle() TextStyle { return i.titleStyle }

func newPolyline(points []contentdraw.Point) *Polyline {
	return &Polyline{polyline: &contentdraw.Polyline{Points: points, LineColor: model.NewPdfColorDeviceRGB(0, 0, 0), LineWidth: 1.0}, lineOpacity: 1.0}
}

// AddTag adds a tag to the chapter.
func (ch *Chapter) AddTag(rootKObj *model.KDict) {
	if ch.tagInfo == nil {
		ch.tagInfo = model.NewStructureTagInfo()
		ch.tagInfo.StructureType = model.StructureTypeSection
	}
	ch.tagInfo.ApplyTag = true
	ch.tagInfo.ParentKObj = rootKObj
	ch.tagInfo.ComponentKObj = ch.tagInfo.NewComponentKObj(ch.tagInfo.StructureType)
	ch.tagInfo.ParentKObj.AddKChild(ch.tagInfo.ComponentKObj)
}

// GetChunks returns the text chunks that compose the paragraph. The returned
// slice references the paragraph's internal chunks; modifying its elements
// modifies the paragraph.
func (sp *StyledParagraph) GetChunks() []*TextChunk { return sp.chunks }

// SetLineTitleStyle sets the style for the title part of all new lines
// of the table of contents.
func (to *TOC) SetLineTitleStyle(style TextStyle) { to.lineTitleStyle = style }

// GetCells returns the cells of the row. The returned slice references the
// row's internal cells; modifying its elements modifies the row.
func (gr *GridRow) GetCells() []*GridCell { return gr.cells }

// Marker returns the marker used for the list items.
// The marker instance can be used the change the text and the style
// of newly added list items.
func (li *List) Marker() *TextChunk { return &li.marker }

// Scale scales the rectangle dimensions by the specified factors.
func (r *Rectangle) Scale(xFactor, yFactor float64) {
	r.width = xFactor * r.width
	r.height = yFactor * r.height
}

// SetLineLevelOffset sets the amount of space an indentation level occupies
// for all new lines of the table of contents.
func (to *TOC) SetLineLevelOffset(levelOffset float64) { to.lineLevelOffset = levelOffset }

// InvoiceCell represents any cell belonging to a table from the invoice
// template. The main tables are the invoice information table, the line
// items table and totals table. Contains the text value of the cell and
// the style properties of the cell.
type InvoiceCell struct {
	InvoiceCellProps
	Value string
}
