// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// Margins returns the margins of the component.
func (d *Division) Margins() (left, right, top, bottom float64) {
	return d.margins.Left, d.margins.Right, d.margins.Top, d.margins.Bottom
}

// SetBuyerAddress sets the buyer address of the invoice.
func (i *Invoice) SetBuyerAddress(address *InvoiceAddress) { i.buyerAddress = address }

// SetDropCapsFirstWord is a convenience method to set traditional drop caps
// on the first word of the paragraph.
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("CHAPTER ONE: It was the best of times...")
//	p.SetDropCapsFirstWord(3, 8.0) // "CHAPTER" spans 3 lines with 8pt gap
func (sp *StyledParagraph) SetDropCapsFirstWord(numLines int, gap float64) {
	sp.SetDropCaps(DropCapsOptions{Type: DropCapsDrop, Scope: DropCapsFirstWord, NumLines: numLines, Gap: gap})
}

func (pt *pageTransformations) applyFlip(page *model.PdfPage) error {
	flipH, flipV := pt.flipH, pt.flipV
	if !flipH && !flipV {
		return nil
	}
	if page == nil {
		return errors.New("no page active")
	}
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return err
	}
	width, height := mediaBox.Width(), mediaBox.Height()
	rotate, err := page.GetRotate()
	if err != nil {
		common.Log.Debug("ERROR: %s - ignoring and assuming no rotation\n", err.Error())
	}
	if isRightAngle := rotate%360 != 0 && rotate%90 == 0; isRightAngle {
		if angle := (360 + rotate%360) % 360; angle == 90 || angle == 270 {
			flipH, flipV = flipV, flipH
		}
	}
	sx, tx := 1.0, 0.0
	if flipH {
		sx, tx = -1.0, -width
	}
	sy, ty := 1.0, 0.0
	if flipV {
		sy, ty = -1.0, -height
	}
	cc := contentstream.NewContentCreator().Scale(sx, sy).Translate(tx, ty)
	stream, err := core.MakeStream(cc.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	array := core.MakeArray(stream)
	array.Append(page.GetContentStreamObjs()...)
	page.Contents = array
	return nil
}

func (i *Invoice) drawAddress(invoiceAddress *InvoiceAddress) []*StyledParagraph {
	var paras []*StyledParagraph
	if invoiceAddress.Heading != "" {
		styledParagraph := newStyledParagraph(i.addressHeadingStyle)
		styledParagraph.SetMargins(0, 0, 0, 7)
		styledParagraph.Append(invoiceAddress.Heading)
		paras = append(paras, styledParagraph)
	}
	styledParagraph := newStyledParagraph(i.addressStyle)
	styledParagraph.SetLineHeight(1.2)
	separator := invoiceAddress.Separator
	if separator == "" {
		separator = i.addressSeparator
	}
	city := invoiceAddress.City
	if invoiceAddress.State != "" {
		if city != "" {
			city += separator
		}
		city += invoiceAddress.State
	}
	if invoiceAddress.Zip != "" {
		if city != "" {
			city += separator
		}
		city += invoiceAddress.Zip
	}
	if invoiceAddress.Name != "" {
		styledParagraph.Append(invoiceAddress.Name + "\n")
	}
	if invoiceAddress.Street != "" {
		styledParagraph.Append(invoiceAddress.Street + "\n")
	}
	if invoiceAddress.Street2 != "" {
		styledParagraph.Append(invoiceAddress.Street2 + "\n")
	}
	if city != "" {
		styledParagraph.Append(city + "\n")
	}
	if invoiceAddress.Country != "" {
		styledParagraph.Append(invoiceAddress.Country + "\n")
	}
	styledParagraph2 := newStyledParagraph(i.addressStyle)
	styledParagraph2.SetLineHeight(1.2)
	styledParagraph2.SetMargins(0, 0, 7, 0)
	if invoiceAddress.Phone != "" {
		styledParagraph2.Append(invoiceAddress.fmtLine(invoiceAddress.Phone, "Phone: ", invoiceAddress.HidePhoneLabel))
	}
	if invoiceAddress.Email != "" {
		styledParagraph2.Append(invoiceAddress.fmtLine(invoiceAddress.Email, "Email: ", invoiceAddress.HideEmailLabel))
	}
	paras = append(paras, styledParagraph, styledParagraph2)
	return paras
}

// SetBackgroundColor set background color of the shading area.
//
// By default the background color is set to white.
func (rs *RadialShading) SetBackgroundColor(backgroundColor Color) {
	rs.base.SetBackgroundColor(backgroundColor)
}

// ColorRGBFromHex converts color hex code to rgb color for using with creator.
// NOTE: If there is a problem interpreting the string, then will use black color and log a debug message.
// Example hex code: #ffffff -> (1,1,1) white.
func ColorRGBFromHex(hexStr string) Color {
	colorRGB := rgbColor{}
	if (len(hexStr) != 4 && len(hexStr) != 7) || hexStr[0] != '#' {
		common.Log.Debug("Invalid hex code: %s", hexStr)
		return colorRGB
	}
	var red, green, blue int
	if len(hexStr) == 4 {
		var redShort, greenShort, blueShort int
		n, err := fmt.Sscanf(hexStr, "#%1x%1x%1x", &redShort, &greenShort, &blueShort)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s, error: %v", hexStr, err)
			return colorRGB
		}
		if n != 3 {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorRGB
		}
		red = redShort*16 + redShort
		green = greenShort*16 + greenShort
		blue = blueShort*16 + blueShort
	} else {
		n, err := fmt.Sscanf(hexStr, "#%2x%2x%2x", &red, &green, &blue)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorRGB
		}
		if n != 3 {
			common.Log.Debug("Invalid hex code: %s, n != 3 (%d)", hexStr, n)
			return colorRGB
		}
	}
	r := float64(red) / 255.0
	g := float64(green) / 255.0
	b := float64(blue) / 255.0
	colorRGB.r = r
	colorRGB.g = g
	colorRGB.b = b
	return colorRGB
}

// SetLineSeparatorStyle sets the style for the separator part of all new
// lines of the table of contents.
func (to *TOC) SetLineSeparatorStyle(style TextStyle) { to.lineSeparatorStyle = style }

// NewPolyline creates a new polyline.
func (c *Creator) NewPolyline(points []contentdraw.Point) *Polyline { return newPolyline(points) }

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

// SetAngle would set the angle at which the gradient is rendered.
//
// The default angle would be 0 where the gradient would be rendered from left to right side.
func (ls *LinearShading) SetAngle(angle float64) { ls.angle = angle }

func (sp *StyledParagraph) createAccessibleLinkChunk(text string, annotation *model.PdfAnnotation, linkTagOptions LinkTagOptions) (*TextChunk, *model.KDict, error) {
	var kDict2 *model.KDict
	textChunk := NewTextChunk(text, sp.linkStyle)
	textChunk.AddAnnotation(annotation)
	if linkTagOptions.MCID > 0 {
		textChunk.SetMarkedContentID(linkTagOptions.MCID)
		textChunk.SetStructureType(model.StructureTypeLink)
		if linkTagOptions.AltText != "" && linkTagOptions.AltText != text {
			textChunk.SetAltText(linkTagOptions.AltText)
		}
		textChunk.AssociateAnnotationWithStructure(annotation)
		kDict, err := textChunk.GenerateKDict()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate KDict for link chunk: %v", err)
		}
		if linkTagOptions.AltText != "" && linkTagOptions.AltText != text {
			kDict.Alt = core.MakeString(linkTagOptions.AltText)
		}
		kDict2 = kDict
	}
	sp.appendChunk(textChunk)
	return textChunk, kDict2, nil
}

// Margins represents page margins or margins around an element.
type Margins struct {
	Left   float64
	Right  float64
	Top    float64
	Bottom float64
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

// SetIncludeInTOC sets a flag to indicate whether or not to include in tOC.
func (ch *Chapter) SetIncludeInTOC(includeInTOC bool) { ch.includeInTOC = includeInTOC }

// Highlight adds a highlight annotation to the text chunk with the specified color and alpha value.
func (tec *TextChunk) Highlight(color Color, alpha float64) *model.PdfAnnotation {
	r, g, b := color.ToRGB()
	pdfAnnotationHighlight := model.NewPdfAnnotationHighlight()
	pdfAnnotationHighlight.C = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfAnnotationHighlight.CA = core.MakeFloat(alpha)
	tec.AddAnnotation(pdfAnnotationHighlight.PdfAnnotation)
	return pdfAnnotationHighlight.PdfAnnotation
}

// SetTextAlignment sets the horizontal alignment of the text within the space provided.
func (sp *StyledParagraph) SetTextAlignment(align TextAlignment) { sp.alignment = align }

func (to *TOC) replaceFonts(font3, font, font4, font2 *model.PdfFont) {
	styles := []*TextStyle{&to.lineNumberStyle, &to.lineTitleStyle, &to.lineSeparatorStyle, &to.linePageStyle, &to.defaultStyle}
	for _, line := range to.lines {
		styles = append(styles, &line.Number.Style, &line.Title.Style, &line.Separator.Style, &line.Page.Style, &line.paragraph.defaultStyle, &line.paragraph.linkStyle)
	}
	styles2 := []*TextStyle{&to.heading.defaultStyle, &to.heading.linkStyle}
	for _, chunk := range to.heading.chunks {
		styles2 = append(styles2, &chunk.Style)
	}
	fn := func(styles []*TextStyle, font3, font, font4, font2 *model.PdfFont) {
		for _, style := range styles {
			switch style.Font {
			case font3:
				style.Font = font
			case font4:
				style.Font = font2
			}
		}
	}
	fn(styles, font3, font, font4, font2)
	fn(styles2, font4, font2, font3, font)
}

// Height returns the total height of all rows.
func (t *Table) Height() float64 {
	height := float64(0.0)
	for _, rowHeight := range t.rowHeights {
		height += rowHeight
	}
	return height
}

func newColorPoint(color Color, point float64) *ColorPoint {
	return &ColorPoint{color: color, point: point}
}

// SetBorderOpacity sets the border opacity of the rectangle.
func (r *Rectangle) SetBorderOpacity(opacity float64) { r.borderOpacity = opacity }

const (
	styleElementName = "style"
	classElementName = "class"
	classAttrName    = "class"
)

// Add adds a VectorDrawable to the Division container.
// Currently supported VectorDrawables:
// - *StyledParagraph
// - *Image
// - *Chart
// - *Rectangle
// - *Ellipse
// - *Line
// - *Table
// - *Division
// - *List
// - *PageBreak
func (di *Division) Add(d VectorDrawable) error {
	switch d2 := d.(type) {
	case *StyledParagraph, *Image, *Chart, *Rectangle, *Ellipse, *Line, *Table, *Division, *List, *PageBreak:
	case containerDrawable:
		containerComponent, err := d2.ContainerComponent(di)
		if err != nil {
			return err
		}
		drawable, ok := containerComponent.(VectorDrawable)
		if !ok {
			return fmt.Errorf("result of ContainerComponent - %T doesn't implement VectorDrawable interface", containerComponent)
		}
		d = drawable
	default:
		return errors.New("unsupported type in Division")
	}
	di.components = append(di.components, d)
	return nil
}
