// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// GeneratePageBlocks draws the ellipse on a new block representing the page.
func (e *Ellipse) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks []*Block
		block  = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx2   = ctx
	)
	isRelative := e.positioning.IsRelative()
	if isRelative {
		e.applyFitMode(ctx.Width)
		ctx.X += e.margins.Left
		ctx.Y += e.margins.Top
		ctx.Width -= e.margins.Left + e.margins.Right
		ctx.Height -= e.margins.Top + e.margins.Bottom
		if e.height > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + e.margins.Top
			ctx2.X = ctx.Margins.Left + e.margins.Left
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - e.margins.Top - e.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - e.margins.Left - e.margins.Right
			ctx = ctx2
		}
	} else {
		ctx.X = e.xc - e.width/2
		ctx.Y = e.yc - e.height/2
	}
	circle := contentdraw.Circle{X: ctx.X, Y: ctx.PageHeight - ctx.Y - e.height, Width: e.width, Height: e.height, BorderWidth: e.borderWidth, Opacity: 1.0}
	if e.fillColor != nil {
		circle.FillEnabled = true
		pdfColor := toPdfColor(e.fillColor)
		err := applyShapeColor(block, pdfColor, e.fillColor, func() Rectangle {
			return Rectangle{x: circle.X, y: circle.Y, width: circle.Width, height: circle.Height}
		})
		if err != nil {
			return nil, ctx, err
		}
		circle.FillColor = pdfColor
	}
	if e.borderColor != nil {
		circle.BorderEnabled = false
		if e.borderWidth > 0 {
			circle.BorderEnabled = true
		}
		circle.BorderColor = toPdfColor(e.borderColor)
		circle.BorderWidth = e.borderWidth
	}
	gsName, err := block.setOpacity(e.fillOpacity, e.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	markedDraw, _, err := circle.MarkedDraw(gsName, e.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	err = block.addContentsByString(string(markedDraw))
	if err != nil {
		return nil, ctx, err
	}
	if isRelative {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		ctx.Y += e.height + e.margins.Bottom
		ctx.Height -= e.height
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

func newTable(cols int) *Table {
	table := &Table{cols: cols, defaultRowHeight: 10.0, colWidths: []float64{}, rowHeights: []float64{}, cells: []*TableCell{}, rowSpanRemaining: make([]int, cols), enablePageWrap: true}
	table.structType = model.StructureTypeTable
	table.resetColumnWidths()
	return table
}

func newPolygon(points [][]contentdraw.Point) *Polygon {
	return &Polygon{polygon: &contentdraw.Polygon{Points: points}, fillOpacity: 1.0, borderOpacity: 1.0}
}

func drawColorGlyph(ctx DrawContext, add_q *contentstream.ContentCreator, block *Block, text string, x float64, style *TextStyle, propertyList map[string]core.PdfObject, s model.StructureType) (bool, error) {
	if text == "" {
		return false, nil
	}
	r, _ := utf8.DecodeRuneInString(text)
	if !isEmoji(r) && !isKeycapSequence(text) {
		return false, nil
	}
	var colorGlyphInfo *model.ColorGlyphInfo
	if style.MultiFont != nil {
		style.MultiFont.Reset()
		colorGlyphInfo = style.MultiFont.GetColorGlyphInfoByText(text)
	} else if otfType := style.Font.GetOTFType(); otfType != nil {
		colorGlyphInfo = model.GetColorGlyphInfoByText(otfType, text)
	}
	if colorGlyphInfo != nil {
		if colorGlyphInfo.GlyphType == model.ColorGlyphTypeSVG && len(colorGlyphInfo.SVGData) > 0 {
			svgData := string(colorGlyphInfo.SVGData)
			graphicSVG, err := newGraphicSVGFromString(svgData)
			if err != nil {
				return false, err
			}
			graphicSVG.ScaleToHeight(style.FontSize)
			graphicSVG.SetPos(x, ctx.Y)
			if len(propertyList) > 0 {
				add_q.Add_EMC()
			}
			add_q = add_q.Add_ET().Add_q()
			pageBlocks, ctx2, err := graphicSVG.GeneratePageBlocks(ctx)
			if err != nil {
				return false, err
			}
			if len(pageBlocks) != 1 {
				return false, fmt.Errorf("expected 1 block from emoji image, got %d", len(pageBlocks))
			}
			block.mergeBlocks(pageBlocks[0])
			width := graphicSVG.Width()
			add_q.Add_Q().Add_BT().Translate(width, 0)
			ctx2.X += width
			if len(propertyList) > 0 {
				add_q.Add_BDC(*core.MakeName(string(s)), propertyList)
			}
			return true, nil
		} else if colorGlyphInfo.GlyphType == model.ColorGlyphTypeBitmap && len(colorGlyphInfo.BitmapData) > 0 {
			image, err := newImageFromBytes(colorGlyphInfo.BitmapData)
			if err != nil {
				return false, err
			}
			image.SetEncoder(core.NewDCTEncoder())
			if colorGlyphInfo.GetColorGlyphFormat() == "PNG" {
				image.SetEncoder(core.NewFlateEncoder())
			}
			fontSize := style.FontSize / float64(colorGlyphInfo.BitmapHeight)
			w := float64(colorGlyphInfo.BitmapWidth) * fontSize
			h := float64(colorGlyphInfo.BitmapHeight) * fontSize
			image.SetWidth(w)
			image.SetHeight(h)
			image.SetPos(x, ctx.Y)
			if len(propertyList) > 0 {
				add_q.Add_EMC()
			}
			add_q = add_q.Add_ET().Add_q()
			pageBlocks, ctx2, err := image.GeneratePageBlocks(ctx)
			if err != nil {
				return false, err
			}
			if len(pageBlocks) != 1 {
				return false, fmt.Errorf("expected 1 block from emoji image, got %d", len(pageBlocks))
			}
			block.mergeBlocks(pageBlocks[0])
			add_q.Add_Q().Add_BT().Translate(w, 0)
			ctx2.X += w
			if len(propertyList) > 0 {
				add_q.Add_BDC(*core.MakeName(string(s)), propertyList)
			}
			return true, nil
		}
	}
	return false, nil
}

func (tp *templateProcessor) strictErr(node *templateNode, s, s2 string, err error) error {
	if tp.options.Strict {
		return tp.nodeError(node, "invalid value %q for attribute %q: %v", s2, s, err)
	}
	return nil
}

// ColorRGBFromArithmetic creates a Color from arithmetic color values (0-1).
// Example:
//
//	green := ColorRGBFromArithmetic(0.0, 1.0, 0.0)
func ColorRGBFromArithmetic(r, g, b float64) Color {
	return rgbColor{r: math.Max(math.Min(r, 1.0), 0.0), g: math.Max(math.Min(g, 1.0), 0.0), b: math.Max(math.Min(b, 1.0), 0.0)}
}

var imageAttrs = map[string]attrApplyFunc{"src": nil, "align": enumAttr(horizontalAlignmentValues, func(img *Image, alignment HorizontalAlignment) { img.SetHorizontalAlignment(alignment) }), "opacity": floatAttr(func(img *Image, opacity float64) { img.SetOpacity(opacity) }), "margin": marginAttr(func(img *Image, margins Margins) {
	img.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "fit-mode": enumAttr(fitModeValues, func(img *Image, fitMode FitMode) { img.SetFitMode(fitMode) }), "x": floatAttr(func(img *Image, x float64) { img.SetPos(x, img.yPos) }), "y": floatAttr(func(img *Image, y float64) { img.SetPos(img.xPos, y) }), "width": floatAttr(func(img *Image, w float64) { img.SetWidth(w) }), "height": floatAttr(func(img *Image, h float64) { img.SetHeight(h) }), "angle": floatAttr(func(img *Image, angle float64) { img.SetAngle(angle) }), "encoder": encoderAttr(func(img *Image, encoder core.StreamEncoder) { img.SetEncoder(encoder) })}

// GetMargins returns the Chapter's margin: left, right, top, bottom.
func (ch *Chapter) GetMargins() (float64, float64, float64, float64) {
	return ch.margins.Left, ch.margins.Right, ch.margins.Top, ch.margins.Bottom
}

func (sp *StyledParagraph) addLine(chunks []*TextChunk) bool {
	if sp.maxLines > 0 && len(sp.lines)+1 >= sp.maxLines {
		if len(chunks) > 0 {
			sp.lines = append(sp.lines, chunks)
		}
		return false
	}
	sp.lines = append(sp.lines, chunks)
	return true
}

// ColorGrayFrom8bit creates a Color from 8-bit (0-255) gray values.
// Example:
//
//	gray := ColorGrayFrom8bit(255, 0, 0)
func ColorGrayFrom8bit(g byte) Color { return grayColor{float64(g) / 255.0} }

// GetColspan returns the number of columns the cell spans.
func (gc *GridCell) GetColspan() int { return gc.colSpan }

// Width returns the width of the chart. In relative positioning mode,
// all the available context width is used at render time.
func (cha *Chart) Width() float64 { return float64(cha.chart.Width()) }
func isTagCharacter(r rune) bool  { return r >= 0xE0020 && r <= 0xE007F }

// AppendColumn appends a column to the line items table.
func (i *Invoice) AppendColumn(description string) *InvoiceCell {
	column := i.NewColumn(description)
	i.columns = append(i.columns, column)
	return column
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

func (tp *templateProcessor) parseColor(hexStr string) Color {
	if hexStr == "" {
		return nil
	}
	color, ok := tp.options.ColorMap[hexStr]
	if ok {
		return color
	}
	if hexStr[0] == '#' {
		if isHexColor(hexStr) {
			return ColorRGBFromHex(hexStr)
		}
		return nil
	}
	return nil
}

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) SetMarkedContentID(id int64) {}

// Width is not used as the division component is designed to fill all the
// available space, depending on the context. Returns 0.
func (d *Division) Width() float64 { return 0 }

// Indent returns the left offset of the list when nested into another list.
func (li *List) Indent() float64 { return li.indent }

// AddTotalLine adds a new line in the invoice totals table.
func (i *Invoice) AddTotalLine(desc, value string) (*InvoiceCell, *InvoiceCell) {
	invoiceCell := &InvoiceCell{i.totalCellProps, desc}
	invoiceCell2 := &InvoiceCell{i.totalCellProps, value}
	i.totalLines = append(i.totalLines, [2]*InvoiceCell{invoiceCell, invoiceCell2})
	return invoiceCell, invoiceCell2
}

// FunctionBasedShading holds data for rendering a function-based shading
// gradient (shading type 1). The color at a point comes from a PostScript
// calculator program rather than from color stops along an axis, so it can
// vary in two dimensions.
type FunctionBasedShading struct {
	base        *shading
	psCode      string
	boundingBox *model.PdfRectangle
}

// SetMargins sets the Grid's left, right, top, bottom margins.
func (g *Grid) SetMargins(left, right, top, bottom float64) {
	g.margins.Left = left
	g.margins.Right = right
	g.margins.Top = top
	g.margins.Bottom = bottom
}
