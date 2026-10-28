// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"fmt"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// SetPos sets absolute positioning with specified coordinates.
func (sp *StyledParagraph) SetPos(x, y float64) {
	sp.positioning = PositionAbsolute
	sp.xPos = x
	sp.yPos = y
}

type Grid struct {
	taggedDrawable
	cols             int
	colWidths        []float64
	defaultRowHeight float64
	positioning      Positioning
	xPos, yPos       float64
	margins          Margins
	rows             []*GridRow
}

// ScaleToHeight scales the rectangle to the specified height. The width of
// the rectangle is scaled so that the aspect ratio is maintained.
func (r *Rectangle) ScaleToHeight(h float64) {
	width := r.width / r.height
	r.height = h
	r.width = h * width
}

// SetFillColor sets the fill color.
func (pbc *PolyBezierCurve) SetFillColor(color Color) {
	pbc.fillColor = color
	pbc.curve.FillColor = toPdfColor(color)
}

// GetBackgroundColor returns the cell's background color.
func (gc *GridCell) GetBackgroundColor() Color { return gc.backgroundColor }

func (gse *GraphicSVGElement) drawCircle(creator *contentstream.ContentCreator, resources *model.PdfPageResources) {
	creator.Add_q()
	gse.Style.toContentStream(creator, resources, gse)
	svgLength, err := parseSVGLength(gse.Attributes["cx"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cx` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["cy"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cy` value: %v", err.Error())
	}
	svgLength3, err := parseSVGLength(gse.Attributes["r"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `r` value: %v", err.Error())
	}
	x2 := svgLength3 * gse.scale
	y2 := svgLength3 * gse.scale
	x1 := x2 * bezierCircleKappa
	y := y2 * bezierCircleKappa
	cubicBezierPath := contentdraw.NewCubicBezierPath()
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(-x2, 0, -x2, y, -x1, y2, 0, y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, y2, x1, y2, x2, y, x2, 0))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(x2, 0, x2, -y, x1, -y2, 0, -y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, -y2, -x1, -y2, -x2, -y, -x2, 0))
	cubicBezierPath = cubicBezierPath.Offset(svgLength*gse.scale, svgLength2*gse.scale)
	if gse.Style.StrokeWidth > 0 {
		cubicBezierPath = cubicBezierPath.Offset(gse.Style.StrokeWidth/2, gse.Style.StrokeWidth/2)
	}
	contentdraw.DrawBezierPathWithCreator(cubicBezierPath, creator)
	gse.Style.fillStroke(creator)
	creator.Add_h()
	creator.Add_Q()
}

// GetSection returns the section the row belongs to (e.g. header or body).
func (gr *GridRow) GetSection() GridRowSection { return gr.section }

// SetBorderColor sets the cell's border color.
func (tc *TableCell) SetBorderColor(col Color) {
	tc.borderColorLeft = col
	tc.borderColorBottom = col
	tc.borderColorRight = col
	tc.borderColorTop = col
}

func (dc DrawContext) contentHeight() float64 {
	return dc.PageHeight - dc.Margins.Top - dc.Margins.Bottom
}

// IsWrapEnabled returns whether line wrapping is enabled for the paragraph.
func (sp *StyledParagraph) IsWrapEnabled() bool { return sp.enableWrap }

func (tp *templateProcessor) validateLink(node *templateNode) error {
	templateTag := templateTags[node.start.Name.Local]
	if templateTag == nil {
		return nil
	}
	if _, ok := templateTag.attrs["link"]; !ok {
		return nil
	}
	effectiveAttrValue, ok := tp.effectiveAttrValue(node, "link")
	if !ok {
		return nil
	}
	if tp.effectiveParentName(node) != "paragraph" {
		if tp.options.Strict {
			return tp.nodeError(node, "link attribute is only valid on a paragraph text chunk")
		}
		return nil
	}
	if _, err := tp.resolveLink(effectiveAttrValue); err != nil {
		if validateLink := tp.strictErr(node, "link", effectiveAttrValue, err); validateLink != nil {
			return validateLink
		}
	}
	return nil
}

// Length calculates and returns the length of the line.
func (l *Line) Length() float64 {
	return math.Sqrt(math.Pow(l.x2-l.x1, 2.0) + math.Pow(l.y2-l.y1, 2.0))
}

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

const (
	HorizontalAlignmentLeft HorizontalAlignment = iota
	HorizontalAlignmentCenter
	HorizontalAlignmentRight
)

func (tp *templateProcessor) nodeError(node *templateNode, format string, a ...any) error {
	return fmt.Errorf("%s", tp.getNodeErrorLocation(node, format, a...))
}

func (i *Invoice) generateInformationBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	styledParagraph := newStyledParagraph(i.defaultStyle)
	styledParagraph.SetMargins(0, 0, 0, 20)
	address := i.drawAddress(i.sellerAddress)
	address = append(address, styledParagraph)
	address = append(address, i.drawAddress(i.buyerAddress)...)
	division := newDivision()
	for _, d := range address {
		division.Add(d)
	}
	information := i.drawInformation()
	table := newTable(2)
	table.SetMargins(0, 0, 25, 0)
	cell := table.NewCell()
	cell.SetIndent(0)
	cell.SetContent(division)
	cell = table.NewCell()
	cell.SetContent(information)
	return table.GeneratePageBlocks(ctx)
}

// SetHorizontalAlignment sets the horizontal alignment of the image.
func (im *Image) SetHorizontalAlignment(alignment HorizontalAlignment) {
	im.horizontalAlignment = alignment
}

func (g *Grid) insertRowAfter(n int, row *GridRow) {
	g.rows = append(g.rows, &GridRow{})
	copy(g.rows[n+2:], g.rows[n+1:])
	g.rows[n+1] = row
	for i, row := range g.rows {
		if i > n+1 {
			row.index += 1
			for _, cell := range row.cells {
				cell.row += 1
			}
		}
	}
}

// SetPageLabels adds the specified page labels to the PDF file generated
// by the creator. See section 12.4.2 "Page Labels" (p. 382 PDF32000_2008).
// NOTE: for existing PDF files, the page label ranges object can be obtained
// using the model.PDFReader's GetPageLabels method.
func (c *Creator) SetPageLabels(pageLabels core.PdfObject) { c.pageLabels = pageLabels }

// SetStructureType sets the structure type for the grid.
func (g *Grid) SetStructureType(structureType model.StructureType) {
	if g.tagInfo == nil {
		g.tagInfo = &model.StructureTagInfo{}
	}
	g.tagInfo.StructureType = structureType
	if g.tagInfo.ComponentKObj == nil {
		if g.tagInfo.ParentKObj != nil {
			g.tagInfo.ParentKObj.RemoveKChild(g.tagInfo.ComponentKObj)
		}
		g.tagInfo.ComponentKObj = g.tagInfo.NewComponentKObj(structureType)
		g.tagInfo.ParentKObj.AddKChild(g.tagInfo.ComponentKObj)
	}
}

func (b *Block) addContentsByString(contentStr string) error {
	contentStreamParser := contentstream.NewContentStreamParser(contentStr)
	contentStreamOperations, err := contentStreamParser.Parse()
	if err != nil {
		return err
	}
	b.contents.WrapIfNeeded()
	contentStreamOperations.WrapIfNeeded()
	*b.contents = append(*b.contents, *contentStreamOperations...)
	return nil
}

// NewChapter creates a new chapter with the specified title as the heading.
func (c *Creator) NewChapter(title string) *Chapter {
	c.chapterCount++
	textStyle := c.NewTextStyle()
	textStyle.FontSize = 16
	fn := func(chapter *Chapter, page int) {
		for i := range c.chapters {
			if c.chapters[i].chapter == chapter {
				c.chapters[i].page = page
				return
			}
		}
		c.chapters = append(c.chapters, chapterInfoEntry{chapter: chapter, page: page})
	}
	return newChapter(nil, c.toc, c.outline, fn, title, c.chapterCount, textStyle)
}

// SetFillOpacity sets the fill opacity of the rectangle.
func (r *Rectangle) SetFillOpacity(opacity float64) { r.fillOpacity = opacity }

func (ts *TextStyle) effectiveLineHeight(effectiveLineHeight float64) float64 {
	if ts.LineHeight > 0 {
		return ts.LineHeight
	}
	return effectiveLineHeight
}

// Polyline represents a slice of points that are connected as straight lines.
// Implements the Drawable interface and can be rendered using the Creator.
type Polyline struct {
	taggedDrawable
	polyline    *contentdraw.Polyline
	lineOpacity float64
}

// SetBorderColor sets border color of the rectangle.
func (r *Rectangle) SetBorderColor(col Color) { r.borderColor = col }

// SetBorderWidth sets the border width.
func (pbc *PolyBezierCurve) SetBorderWidth(borderWidth float64) {
	pbc.curve.BorderWidth = borderWidth
}

// SetSoftMask sets a soft mask (/SMask) for the image: an 8-bit grayscale
// alpha channel where white marks fully opaque areas of the image, black
// fully transparent ones, and intermediate values partial transparency.
//
// The mask image may have different dimensions and encoding than the base
// image and, unlike Image.SetAlpha on the underlying model image, works with
// pass-through JPEG images without touching their pixel data. A mask with
// more than one color component is converted to grayscale luminance; a
// single-component mask is used as-is. The mask's own alpha channel, if any,
// is ignored.
//
// The mask replaces any SMask derived automatically from the base image's
// alpha channel. An image can carry a single mask: setting a mask of a
// different type than a previously set one returns an error.
func (im *Image) SetSoftMask(mask *model.Image) error {
	if mask == nil {
		return errors.New("mask image is nil")
	}
	return im.setMask(&imageMask{kind: imageMaskSoft, img: mask})
}

func (tp *templateProcessor) parseChart(node *templateNode) (any, error) {
	sourceName, _ := tp.effectiveAttrValue(node, "src")
	if sourceName == "" {
		return nil, tp.nodeError(node, "chart `src` attribute cannot be empty")
	}
	chart, ok := tp.options.ChartMap[sourceName]
	if !ok {
		return nil, tp.nodeError(node, "could not find chart resource: `%s`", sourceName)
	}
	chart2 := NewChart(chart)
	if err := tp.applyAttrs(node, chart2, chartAttrs); err != nil {
		return nil, err
	}
	return chart2, nil
}

// SetWidth sets line width.
func (cu *Curve) SetWidth(width float64) { cu.width = width }

// SetMargins sets the margins of the component. The margins are applied
// around the division.
func (d *Division) SetMargins(left, right, top, bottom float64) {
	d.margins.Left = left
	d.margins.Right = right
	d.margins.Top = top
	d.margins.Bottom = bottom
}

func wrapAsPaginationArtifact(block *Block, n int) {
	contents := (*block.contents)[n:]
	artifact := model.Artifact{Type: model.ArtifactTypePagination}
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Add_BDC(*core.MakeName("Artifact"), artifact.GenerateMap())
	operations := *contentCreator.Operations()
	for _, content := range contents {
		switch content.Operand {
		case "BDC", "BMC", "EMC":
			continue
		}
		operations = append(operations, content)
	}
	operations = append(operations, &contentstream.ContentStreamOperation{Operand: "EMC"})
	*block.contents = append((*block.contents)[:n], operations...)
}
