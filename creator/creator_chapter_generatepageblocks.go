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
	"strings"

	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// GeneratePageBlocks generate the Page blocks.  Multiple blocks are generated if the contents wrap
// over multiple pages.
func (ch *Chapter) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx4 := ctx
	ok := ch.tagInfo != nil && ch.tagInfo.ApplyTag
	var mcid int64
	var (
		m  map[int]float64
		m2 map[int]bool
	)
	if ok {
		mcid = ch.tagInfo.Mcid
	}
	if ch.positioning.IsRelative() {
		ctx.X += ch.margins.Left
		ctx.Y += ch.margins.Top
		ctx.Width -= ch.margins.Left + ch.margins.Right
		ctx.Height -= ch.margins.Top
	}
	ctx3 := ctx
	if ok {
		ch.heading.SetMarkedContentID(mcid)
		ch.heading.SetStructureType(model.StructureTypeHeader)
	}
	pageBlocks, ctx2, err := ch.heading.GeneratePageBlocks(ctx)
	if err != nil {
		return pageBlocks, ctx, err
	}
	if ok {
		page := int64(ctx2.Page)
		ch.heading.SetStructPageNumber(&page)
		kDict, err := ch.heading.GenerateKDict()
		if err != nil {
			return nil, ctx, err
		}
		ch.tagInfo.ComponentKObj.AddKChild(kDict)
		mcid++
	}
	ctx = ctx2
	x := ctx.X
	y := ctx.Y - ch.heading.Height()
	page := int64(ctx.Page)
	headingNumber := ch.headingNumber()
	headingText := ch.headingText()
	if ch.recordFunc != nil {
		ch.recordFunc(ch, ctx.Page)
	}
	if ch.includeInTOC {
		tocLine := ch.toc.Add(headingNumber, ch.title, strconv.FormatInt(page, 10), ch.level)
		if ch.toc.showLinks {
			tocLine.SetLink(page, x, y)
		}
	}
	if ch.outlineItem == nil {
		ch.outlineItem = model.NewOutlineItem(headingText, model.NewOutlineDest(page-1, x, y))
		if ch.parent != nil {
			ch.parent.outlineItem.Add(ch.outlineItem)
		} else {
			ch.outline.Add(ch.outlineItem)
		}
	} else {
		dest := &ch.outlineItem.Dest
		dest.Page = page - 1
		dest.X = x
		dest.Y = y
	}
	for _, content := range ch.contents {
		if _, ok := content.(*PageBreak); ok {
			if m == nil {
				m = map[int]float64{}
				m2 = map[int]bool{}
			}
			n := len(pageBlocks) - 1
			m[n] = ctx.Y
			m2[n+1] = true
		}
		ok2 := false
		var structureTypeParagraph model.StructureType
		if ok {
			switch content2 := content.(type) {
			case *StyledParagraph:
				ok2 = true
				structureTypeParagraph = model.StructureTypeParagraph
				content.SetMarkedContentID(mcid)
				content.SetStructureType(structureTypeParagraph)
			case *Division:
				ok2 = true
				structureTypeParagraph = model.StructureTypeDivision
				content2.AddTag(ch.tagInfo.ComponentKObj)
			case *Table:
				ok2 = true
				structureTypeParagraph = model.StructureTypeTable
				content2.AddTag(ch.tagInfo.ComponentKObj)
			case *List:
				ok2 = true
				structureTypeParagraph = model.StructureTypeList
				content2.AddTag(ch.tagInfo.ComponentKObj)
			case *Chapter:
				ok2 = true
				structureTypeParagraph = model.StructureTypeSection
				content2.AddTag(ch.tagInfo.ComponentKObj)
			}
		}
		pageBlocks2, ctx2, err := content.GeneratePageBlocks(ctx)
		if err != nil {
			return pageBlocks, ctx, err
		}
		if ok && ok2 {
			page := int64(ctx2.Page)
			content.SetStructPageNumber(&page)
			if structureTypeParagraph == model.StructureTypeParagraph {
				kDict, err := content.GenerateKDict()
				if err != nil {
					return nil, ctx, err
				}
				ch.tagInfo.ComponentKObj.AddKChild(kDict)
			}
			mcid++
		}
		if len(pageBlocks2) < 1 {
			continue
		}
		pageBlocks[len(pageBlocks)-1].mergeBlocks(pageBlocks2[0])
		pageBlocks = append(pageBlocks, pageBlocks2[1:]...)
		ctx = ctx2
	}
	if ch.background != nil {
		var err error
		if pageBlocks, err = ch.drawBackground(pageBlocks, ctx3, ctx, m, m2); err != nil {
			return nil, ctx, err
		}
	}
	if ch.positioning.IsRelative() {
		ctx.X = ctx4.X
	}
	if ch.positioning.IsAbsolute() {
		return pageBlocks, ctx4, nil
	}
	return pageBlocks, ctx, nil
}

// SetLineSeparator sets the separator for all new lines of the table of contents.
func (to *TOC) SetLineSeparator(separator string) { to.lineSeparator = separator }

// SetFitMode sets the fit mode of the ellipse.
// NOTE: The fit mode is only applied if relative positioning is used.
func (e *Ellipse) SetFitMode(fitMode FitMode) { e.fitMode = fitMode }

// AddExternalLink adds a new external link to the paragraph.
// The text parameter represents the text that is displayed and the url
// parameter sets the destination of the link.
func (sp *StyledParagraph) AddExternalLink(text, url string) *TextChunk {
	s := url
	if strings.HasPrefix(url, "http") {
		s = text
	}
	textChunk := NewTextChunk(text, sp.linkStyle)
	textChunk.AddAnnotation(newURILinkAnnotation(url, s))
	return sp.appendChunk(textChunk)
}

func (im *Image) rotatedSize() (float64, float64) {
	width := im.width
	height := im.height
	angle := im.angle
	if angle == 0 {
		return width, height
	}
	boundingBox := contentdraw.Path{Points: []contentdraw.Point{contentdraw.NewPoint(0, 0).Rotate(angle), contentdraw.NewPoint(width, 0).Rotate(angle), contentdraw.NewPoint(0, height).Rotate(angle), contentdraw.NewPoint(width, height).Rotate(angle)}}.GetBoundingBox()
	return boundingBox.Width, boundingBox.Height
}

func (t *Table) clone() *Table {
	table := *t
	table.rowHeights = make([]float64, len(t.rowHeights))
	copy(table.rowHeights, t.rowHeights)
	table.colWidths = make([]float64, len(t.colWidths))
	copy(table.colWidths, t.colWidths)
	table.cells = make([]*TableCell, 0, len(t.cells))
	for _, cell := range t.cells {
		cell2 := *cell
		cell2.table = &table
		table.cells = append(table.cells, &cell2)
	}
	return &table
}

type border struct {
	x           float64
	y           float64
	width       float64
	height      float64
	fillColor   Color
	colorLeft   Color
	widthLeft   float64
	colorBottom Color
	widthBottom float64
	colorRight  Color
	widthRight  float64
	colorTop    Color
	widthTop    float64
	LineStyle   contentdraw.LineStyle
	styleLeft   CellBorderStyle
	styleRight  CellBorderStyle
	styleTop    CellBorderStyle
	styleBottom CellBorderStyle
	opacity     float64
}

func (g *Grid) addSectionTag(n GridRowSection) *model.KDict {
	kDictionary := model.NewKDictionary()
	kDictionary.S = core.MakeName(string(gridSectionStructureTypes[n]))
	g.tagInfo.ComponentKObj.AddKChild(kDictionary)
	return kDictionary
}

func (i *Invoice) drawSection(text, text2 string) []*StyledParagraph {
	var paras []*StyledParagraph
	if text != "" {
		styledParagraph := newStyledParagraph(i.noteHeadingStyle)
		styledParagraph.SetMargins(0, 0, 0, 5)
		styledParagraph.Append(text)
		paras = append(paras, styledParagraph)
	}
	if text2 != "" {
		styledParagraph := newStyledParagraph(i.noteStyle)
		styledParagraph.Append(text2)
		paras = append(paras, styledParagraph)
	}
	return paras
}

// SetFitMode sets the fit mode of the line.
// NOTE: The fit mode is only applied if relative positioning is used.
func (l *Line) SetFitMode(fitMode FitMode) { l.fitMode = fitMode }

// SetBorderOpacity sets the border opacity.
func (cp *CurvePolygon) SetBorderOpacity(opacity float64) { cp.borderOpacity = opacity }

// SetVerticalAlignment set the cell's vertical alignment of content.
// Can be one of:
// - CellHorizontalAlignmentTop
// - CellHorizontalAlignmentMiddle
// - CellHorizontalAlignmentBottom
func (gc *GridCell) SetVerticalAlignment(valign CellVerticalAlignment) { gc.verticalAlignment = valign }

// Height returns Image's document height.
func (im *Image) Height() float64 { return im.height }

// GetLines returns the text chunk lines produced after wrapping the paragraph
// to the available width. The returned slice references the paragraph's
// internal lines; modifying its elements modifies the paragraph.
func (sp *StyledParagraph) GetLines() [][]*TextChunk { return sp.lines }

func (b *Block) drawToPage(page *model.PdfPage) error {
	contentStreamOperations := &contentstream.ContentStreamOperations{}
	if page.Resources == nil {
		page.Resources = model.NewPdfPageResources()
	}
	err := mergeContentStreams(contentStreamOperations, page.Resources, b.contents, b.resources)
	if err != nil {
		return err
	}
	if err = mergeColorspaces(b.resources, page.Resources); err != nil {
		return err
	}
	if err = page.AppendContentBytes(contentStreamOperations.Bytes(), true); err != nil {
		return err
	}
	for _, annotation := range b.annotations {
		page.AddAnnotation(annotation)
	}
	return nil
}

func (i *Invoice) newColumn(value string, alignment CellHorizontalAlignment) *InvoiceCell {
	invoiceCell := &InvoiceCell{i.headingCellProps, value}
	invoiceCell.Alignment = alignment
	return invoiceCell
}

func (ch *Chapter) headingText() string {
	title := ch.title
	if headingNumber := ch.headingNumber(); headingNumber != "" {
		title = fmt.Sprintf("%s %s", headingNumber, title)
	}
	return title
}

// SetPositioning sets the positioning of the rectangle (absolute or relative).
func (r *Rectangle) SetPositioning(position Positioning) { r.positioning = position }

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

// GetMaxLines returns the maximum number of lines drawn for the paragraph.
// A value of 0 means no limit.
func (sp *StyledParagraph) GetMaxLines() int { return sp.maxLines }

type shading struct {
	backgroundColor Color
	antiAlias       bool
	extends         []bool
	colorPoints     []*ColorPoint
}

// Logo returns the logo of the invoice.
func (i *Invoice) Logo() *Image { return i.logo }

func (b *Block) addContents(contentStreamOperations *contentstream.ContentStreamOperations) {
	*b.contents = append(*b.contents, *contentStreamOperations...)
}

// SetScaling sets scaling value for graphic SVG and maintain the aspect ratio.
func (gse *GraphicSVGElement) SetScaling(xFactor, yFactor float64) {
	width := gse.Width / gse.ViewBox.W
	height := gse.Height / gse.ViewBox.H
	gse.setDefaultScaling(math.Max(width, height))
	for _, graphicSVGElement := range gse.Children {
		graphicSVGElement.SetScaling(xFactor, yFactor)
	}
}

func (tp *templateProcessor) checkStyleText(node *templateNode) error {
	for _, childValue := range node.childValues {
		if templateText2, ok := childValue.(*templateText); ok {
			if _, err := tp.checkNodeText(node, templateText2.text, templateText2); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetWidthRight sets border width for right.
func (bo *border) SetWidthRight(bw float64) { bo.widthRight = bw }

// Scale scales Image by a constant factor, both width and height.
func (im *Image) Scale(xFactor, yFactor float64) {
	im.width = xFactor * im.width
	im.height = yFactor * im.height
}

// SetAngle sets the rotation angle of the text.
func (sp *StyledParagraph) SetAngle(angle float64) { sp.angle = angle }
