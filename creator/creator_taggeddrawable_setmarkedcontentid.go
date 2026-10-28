// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"math"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// SetMarkedContentID sets marked content ID.
func (td *taggedDrawable) SetMarkedContentID(mcid int64) {
	if td.tagInfo == nil {
		td.tagInfo = model.NewStructureTagInfo()
		td.tagInfo.StructureType = td.structType
	}
	td.tagInfo.Mcid = mcid
}

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

var textContentTags = map[string]struct{}{"paragraph": {}, "text-chunk": {}, "chapter-heading": {}, "list-marker": {}}

func renderBackground(background *Background, width, height float64) (*Block, error) {
	rectangle := newRectangle(0, 0, width, height)
	rectangle.SetFillColor(background.FillColor)
	rectangle.SetBorderColor(background.BorderColor)
	rectangle.SetBorderWidth(background.BorderSize)
	rectangle.SetBorderRadius(background.BorderRadiusTopLeft, background.BorderRadiusTopRight, background.BorderRadiusBottomLeft, background.BorderRadiusBottomRight)
	ctx := DrawContext{PageWidth: width, PageHeight: height, Width: width, Height: height}
	pageBlocks, _, err := rectangle.GeneratePageBlocks(ctx)
	if err != nil {
		return nil, err
	}
	return pageBlocks[0], nil
}

// NewPolygon creates a new polygon.
func (c *Creator) NewPolygon(points [][]contentdraw.Point) *Polygon { return newPolygon(points) }

// SetPos sets the position of the chart to the specified coordinates.
// This method sets the chart to use absolute positioning.
func (cha *Chart) SetPos(x, y float64) {
	cha.positioning = PositionAbsolute
	cha.xPos = x
	cha.yPos = y
}

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (fbs *FunctionBasedShading) SetAntiAlias(enable bool) { fbs.base.SetAntiAlias(enable) }

func parseTableCellTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseTableCell(node)
}

// NewList creates a new list.
func (c *Creator) NewList() *List { return newList(c.NewTextStyle()) }

// Positioning represents the positioning type for drawing creator components (relative/absolute).
type Positioning int

func rotatedBounds(x2, y2, theta float64) (x, y, width, height float64) {
	if theta == 0 {
		return 0, 0, x2, y2
	}
	boundingBox := contentdraw.Path{Points: []contentdraw.Point{contentdraw.NewPoint(0, 0).Rotate(theta), contentdraw.NewPoint(x2, 0).Rotate(theta), contentdraw.NewPoint(0, y2).Rotate(theta), contentdraw.NewPoint(x2, y2).Rotate(theta)}}.GetBoundingBox()
	return boundingBox.X, boundingBox.Y, boundingBox.Width, boundingBox.Height
}

// GetHorizontalAlignment returns the horizontal alignment of the image.
func (im *Image) GetHorizontalAlignment() HorizontalAlignment { return im.horizontalAlignment }

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (li *List) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var value float64
	var paras []*StyledParagraph
	ok := li.tagInfo != nil && li.tagInfo.ApplyTag
	for _, item := range li.items {
		styledParagraph := newStyledParagraph(li.textStyle)
		styledParagraph.SetEnableWrap(false)
		styledParagraph.SetTextAlignment(TextAlignmentRight)
		styledParagraph.Append(item.marker.Text).Style = item.marker.Style
		width := styledParagraph.getTextWidth() / 1000.0 / ctx.Width
		if value < width {
			value = width
		}
		paras = append(paras, styledParagraph)
	}
	table := newTable(2)
	table.SetColumnWidths(value, 1-value)
	table.SetMargins(li.margins.Left+li.indent, li.margins.Right, li.margins.Top, li.margins.Bottom)
	table.EnableRowWrap(true)
	if ok {
		table.SetStructPageNumber(li.tagInfo.StructPageNumber)
		table.AddTag(li.tagInfo.ParentKObj)
		table.SetStructureType(model.StructureTypeList)
		table.SetMarkedContentID(li.tagInfo.Mcid)
	}
	for i, item := range li.items {
		cell := table.NewCell()
		cell.SetIndent(0)
		cell.SetContent(paras[i])
		if ok {
			cell.SetStructPageNumber(li.tagInfo.StructPageNumber)
			cell.SetStructureType(model.StructureTypeLabel)
		}
		cell = table.NewCell()
		cell.SetIndent(0)
		cell.SetContent(item.content)
		if ok {
			cell.SetStructPageNumber(li.tagInfo.StructPageNumber)
			cell.SetStructureType(model.StructureTypeListBody)
		}
	}
	return table.GeneratePageBlocks(ctx)
}

// The Image type is used to draw an image onto PDF.
type Image struct {
	taggedDrawable
	xobj                  *model.XObjectImage
	img                   *model.Image
	altText               string
	angle                 float64
	width, height         float64
	origWidth, origHeight float64
	positioning           Positioning
	horizontalAlignment   HorizontalAlignment
	xPos                  float64
	yPos                  float64
	opacity               float64
	margins               Margins
	unusedA, unusedB      float64
	encoder               core.StreamEncoder
	fitMode               FitMode
	lazy                  bool
	mask                  *imageMask
	artifact              *model.Artifact
}

func parseFiniteFloat(s string) (float64, error) {
	float, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(float) || math.IsInf(float, 0) {
		return 0, fmt.Errorf("%q is not a finite number", s)
	}
	return float, nil
}

// NewLinearGradientColor creates a linear gradient color that could act as a color in other components.
func (c *Creator) NewLinearGradientColor(colorPoints []*ColorPoint) *LinearShading {
	return newLinearShading(colorPoints)
}

// DropCapsScope defines what portion of the paragraph text receives drop caps treatment.
type DropCapsScope int

func (pat *ParsedTemplate) validateWith(creator *Creator, renderer componentRenderer, data any) error {
	if creator == nil {
		common.Log.Error("Creator instance cannot be nil.")
		return errInvalidCreator
	}
	data2, err := pat.execute(data)
	if err != nil {
		return err
	}
	return newTemplateProcessor(creator, pat.source, data2, pat.options, renderer).validate()
}

// GetHeading returns the chapter heading paragraph. Used to give access to address style: font, sizing etc.
func (ch *Chapter) GetHeading() *StyledParagraph { return ch.heading }

func (sp *StyledParagraph) appendChunk(chunk *TextChunk) *TextChunk {
	sp.chunks = append(sp.chunks, chunk)
	sp.wrapText()
	return chunk
}

func newPolyBezierCurve(curves []contentdraw.CubicBezierCurve) *PolyBezierCurve {
	return &PolyBezierCurve{curve: &contentdraw.PolyBezierCurve{Curves: curves, BorderColor: model.NewPdfColorDeviceRGB(0, 0, 0), BorderWidth: 1.0}, fillOpacity: 1.0, borderOpacity: 1.0}
}

// AddSection adds a new content section at the end of the invoice.
func (i *Invoice) AddSection(title, content string) {
	i.sections = append(i.sections, [2]string{title, content})
}

// SetFitMode sets the fit mode of the rectangle.
// NOTE: The fit mode is only applied if relative positioning is used.
func (r *Rectangle) SetFitMode(fitMode FitMode) { r.fitMode = fitMode }

// Height returns the Block's height.
func (b *Block) Height() float64 { return b.height }

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) SetStructPageNumber(pageNumber *int64) {}

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

var (
	ColorBlack  = ColorRGBFromArithmetic(0, 0, 0)
	ColorWhite  = ColorRGBFromArithmetic(1, 1, 1)
	ColorRed    = ColorRGBFromArithmetic(1, 0, 0)
	ColorGreen  = ColorRGBFromArithmetic(0, 1, 0)
	ColorBlue   = ColorRGBFromArithmetic(0, 0, 1)
	ColorYellow = ColorRGBFromArithmetic(1, 1, 0)
)
