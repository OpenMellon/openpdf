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
	"github.com/OpenMellon/typesetting/di"
	"github.com/OpenMellon/typesetting/shaping"
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

func drawParagraphChunks(block *Block, para *StyledParagraph, rows4 [][]*TextChunk, ctx DrawContext) (DrawContext, [][]*TextChunk, error) {
	n := 1
	keyName := core.PdfObjectName(fmt.Sprintf("Font%d", n))
	for block.resources.HasFontByName(keyName) {
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("Font%d", n))
	}
	err := block.resources.SetFontByName(keyName, para.defaultStyle.Font.ToPdfObject())
	if err != nil {
		return ctx, nil, err
	}
	n++
	fontName := keyName
	fontSize := para.defaultStyle.FontSize
	isRelative := para.positioning.IsRelative()
	var rows [][]core.PdfObjectName
	var rows5 [][]*TextChunk
	var height float64
	rows2 := [][]shaping.Line{}
	floats := make([]float64, 0, len(rows4))
	for i, chunks := range rows4 {
		var pdfObjectNames []core.PdfObjectName
		var (
			fontSize  float64
			fontSize2 float64
		)
		if len(chunks) > 0 {
			fontSize = chunks[0].Style.FontSize
			fontSize2 = chunks[0].Style.FontSize * chunks[0].Style.effectiveLineHeight(para.lineHeight)
			if i == 0 && len(para.chunks) > 0 && para.dropCaps != nil {
				dropCaps := para.dropCaps
				if dropCaps.Type != DropCapsNone && para.dropCapsEnabled {
					if len(chunks) > 1 {
						fontSize = chunks[1].Style.FontSize
						fontSize2 = chunks[1].Style.FontSize * chunks[1].Style.effectiveLineHeight(para.lineHeight)
					}
				}
			}
		}
		rows3 := []shaping.Line{}
		for j, chunk := range chunks {
			if i == 0 && j == 0 && len(para.chunks) > 0 && para.dropCaps != nil {
				dropCaps := para.dropCaps
				if dropCaps.Type != DropCapsNone && para.dropCapsEnabled {
					continue
				}
			}
			style := chunk.Style
			if chunk.Text != "" {
				if style.FontSize > fontSize {
					fontSize = style.FontSize
				}
				if fontSize := style.FontSize * style.effectiveLineHeight(para.lineHeight); fontSize > fontSize2 {
					fontSize2 = fontSize
				}
			}
			if fontSize > ctx.PageHeight {
				return ctx, nil, errors.New("paragraph height can't be larger than page height")
			}
			keyName = core.PdfObjectName(fmt.Sprintf("Font%d", n))
			pdfObjectNames = append(pdfObjectNames, keyName)
			if para.enableShaping {
				line := style.Font.ShapeText(chunk.Text, style.FontSize)
				rows3 = append(rows3, line)
				for _, shapingOutput := range line {
					chunk.Style.Font.RegisterLigatureMapping(shapingOutput, chunk.Text)
				}
			}
			err := block.resources.SetFontByName(keyName, style.Font.ToPdfObject())
			if err != nil {
				return ctx, nil, err
			}
			n++
		}
		rows2 = append(rows2, rows3)
		if isRelative && height+fontSize2 > ctx.Height {
			rows5 = rows4[i:]
			rows4 = rows4[:i]
			break
		}
		height += fontSize2
		floats = append(floats, fontSize2)
		rows = append(rows, pdfObjectNames)
	}
	value, value2, value3 := 0.0, 0.0, 0.0
	if len(rows4) > 0 {
		value, value2, value3 = chunksFontMetrics(rows4[0])
	}
	var float float64
	if len(floats) > 0 {
		float = floats[0]
	}
	lineHeight, fontSize2 := value*para.lineHeight, float
	var chunk *TextChunk
	if len(para.chunks) > 0 && para.dropCaps != nil {
		dropCaps := para.dropCaps
		if dropCaps.Type != DropCapsNone && para.dropCapsEnabled {
			chunk = para.chunks[0]
			chunk2 := para.lines[0][0]
			if len(para.lines[0]) > 1 && chunk2 == chunk {
				chunk2 = para.lines[0][1]
			}
			fontSize2 = chunk2.Style.FontSize * chunk2.Style.effectiveLineHeight(para.lineHeight)
		}
	}
	if len(rows4) == 0 {
		return ctx, rows5, nil
	}
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Add_q()
	value4 := fontSize2
	if para.verticalAlignment == TextVerticalAlignmentCenter {
		value4 = value2 + (value+value3-value2)/2 + (fontSize2-value2)/2
	}
	y := ctx.PageHeight - ctx.Y - value4
	contentCreator.Translate(ctx.X, y)
	value5 := y
	if para.angle != 0 {
		contentCreator.RotateDeg(para.angle)
	}
	m := map[Color]model.PdfColor{}
	m2 := map[Color]*[2]float64{}
	fn := func() Rectangle {
		return Rectangle{x: ctx.X, y: y, width: para.getMaxLineWidth() / 1000.0, height: height}
	}
	for _, chunks := range rows4 {
		for _, chunk := range chunks {
			switch chunk.Style.Color.(type) {
			case *LinearShading, *RadialShading, *FunctionBasedShading:
				color := chunk.Style.Color
				if _, ok := m[color]; ok {
					continue
				}
				pdfColor := toPdfColor(color)
				if err := applyShapeColor(block, pdfColor, color, fn); err != nil {
					return ctx, nil, err
				}
				m[color] = pdfColor
				m2[color] = nil
			}
		}
	}
	fn2 := func(color Color, value, value2 float64) {
		if _, ok := m[color]; !ok {
			return
		}
		floats := m2[color]
		if floats == nil {
			m2[color] = &[2]float64{value, value + value2}
			return
		}
		if value < floats[0] {
			floats[0] = value
		}
		if value+value2 > floats[1] {
			floats[1] = value + value2
		}
	}
	fn3 := func(color Color) model.PdfColor {
		if color2, ok := m[color]; ok {
			return color2
		}
		return toPdfColor(color)
	}
	if para.textOverflow == TextOverflowHidden {
		contentCreator.Add_re(0, -height+lineHeight+1, para.width, height).Add_W().Add_n()
	}
	contentCreator.Add_BT()
	propertyList := map[string]core.PdfObject{}
	if para.artifact == nil {
		if para.tagInfo != nil {
			propertyList["MCID"] = core.MakeInteger(para.tagInfo.Mcid)
		}
		if para.languageID != "" {
			propertyList["Lang"] = core.MakeString(para.languageID)
		}
		if len(propertyList) > 0 {
			contentCreator.Add_BDC(*core.MakeName(string(para.tagInfo.StructureType)), propertyList)
		}
	} else {
		propertyList = para.artifact.GenerateMap()
		if len(propertyList) > 0 {
			contentCreator.Add_BDC(*core.MakeName(string("Artifact")), propertyList)
		} else {
			contentCreator.Add_BMC(*core.MakeName(string("Artifact")))
		}
	}
	var gap float64
	if chunk != nil {
		style := &chunk.Style
		dropCaps := para.dropCaps
		fontName := core.PdfObjectName(fmt.Sprintf("Font%d", n))
		err := block.resources.SetFontByName(fontName, style.Font.ToPdfObject())
		if err != nil {
			return ctx, nil, err
		}
		n++
		data := style.Font.Encoder().Encode(chunk.Text)
		var charSpacing float64
		text := []rune(chunk.Text)
		for i, r := range text {
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if ok {
				charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale() / 1000.0
				if i < len(text)-1 {
					charSpacing += style.CharSpacing
				}
			}
		}
		switch dropCaps.Type {
		case DropCapsDrop:
			chunk2 := para.lines[0][0]
			if len(para.lines[0]) > 1 && chunk2 == chunk {
				chunk2 = para.lines[0][1]
			}
			fontMetrics := newFontMetrics(chunk2.Style.Font, chunk2.Style.FontSize)
			fontMetrics2 := newFontMetrics(style.Font, style.FontSize)
			ascent := fontMetrics.ascent - fontMetrics2.ascent
			contentCreator.SetNonStrokingColor(fn3(style.Color)).Add_Tf(fontName, style.FontSize).Add_Td(0, ascent).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(data)}...)
			contentCreator.Add_Td(charSpacing+dropCaps.Gap, -ascent)
			gap = charSpacing + dropCaps.Gap
			fn2(style.Color, ctx.X, charSpacing)
		case DropCapsInline:
			chunk2 := para.lines[0][0]
			if len(para.lines[0]) > 1 && chunk2 == chunk {
				chunk2 = para.lines[0][1]
			}
			fontMetrics := newFontMetrics(chunk2.Style.Font, chunk2.Style.FontSize)
			fontMetrics2 := newFontMetrics(style.Font, style.FontSize)
			capHeight := fontMetrics.capHeight - fontMetrics2.capHeight
			contentCreator.SetNonStrokingColor(fn3(style.Color)).Add_Tf(fontName, style.FontSize).Add_Td(0, capHeight).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(data)}...)
			contentCreator.Add_Td(charSpacing+dropCaps.Gap, -capHeight)
			gap = charSpacing + dropCaps.Gap
			fn2(style.Color, ctx.X, charSpacing)
		}
	}
	var basicLines []*contentdraw.BasicLine
	for i, chunks := range rows4 {
		x := ctx.X
		value, _, value3 = para.getLineMetrics(i)
		fontSize2 = (value + value3)
		fontSize3 := fontSize2 * para.lineHeight
		float := floats[i]
		if fontSize3 > float {
			float = fontSize3
		}
		if i != 0 {
			contentCreator.Add_TD(0, -float)
			value5 -= float
		}
		var value float64
		x2 := 0.0
		if chunk != nil {
			dropCaps := para.dropCaps
			if i > 0 {
				if dropCaps.Type == DropCapsDrop && i-1 < dropCaps.NumLines {
					value = gap
				} else if dropCaps.Type == DropCapsInline {
					value = gap
				}
			}
			switch dropCaps.Type {
			case DropCapsDrop:
				if i < dropCaps.NumLines {
					x2 = gap
				}
			case DropCapsInline:
				x2 = gap
			}
		}
		if i > 0 {
			x := x2 - value
			if x != 0 {
				contentCreator.Add_Td(x, 0)
			}
		}
		x += x2
		ok := i == len(rows4)-1
		var (
			value3       float64
			fontSize4    float64
			fontMetrics2 *fontMetrics
			wx           float64
			n2           uint
		)
		var floats2 []float64
		for j, chunk2 := range chunks {
			if i == 0 && j == 0 && chunk != nil {
				continue
			}
			style := &chunk2.Style
			if style.FontSize > fontSize4 {
				fontSize4 = style.FontSize
				fontMetrics2 = newFontMetrics(chunk2.Style.Font, style.FontSize)
			}
			if fontSize2 > fontSize4 {
				fontSize4 = fontSize2
			}
			runeMetrics, ok := style.Font.GetRuneMetrics(' ')
			if runeMetrics.Wx == 0 && style.MultiFont != nil {
				runeMetrics, ok = style.MultiFont.GetRuneMetrics(' ')
				style.MultiFont.Reset()
			}
			if !ok {
				return ctx, nil, errors.New("the font does not have a space glyph")
			}
			var n uint
			var charSpacing float64
			textLen := len(chunk2.Text)
			for i, r := range chunk2.Text {
				if r == ' ' {
					n++
					continue
				}
				if r == '\u000A' {
					continue
				}
				runeMetrics, ok := style.Font.GetRuneMetrics(r)
				if runeMetrics.Wx == 0 && style.MultiFont != nil {
					runeMetrics, ok = style.MultiFont.GetRuneMetrics(' ')
					style.MultiFont.Reset()
				}
				if !ok {
					common.Log.Debug("Unsupported rune %v in font\n", r)
					return ctx, nil, errors.New("unsupported text glyph")
				}
				charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale()
				if i != textLen-1 {
					charSpacing += style.CharSpacing * 1000.0
				}
			}
			floats2 = append(floats2, charSpacing)
			value3 += charSpacing
			wx += float64(n) * runeMetrics.Wx * style.FontSize * style.horizontalScale()
			n2 += n
		}
		fontSize4 = floats[i]
		if fontSize3 > fontSize4 {
			fontSize4 = fontSize3
		}
		var objs []core.PdfObject
		width := para.width * 1000.0
		switch para.alignment {
		case TextAlignmentJustify:
			if n2 > 0 && !ok {
				wx = (width - value3) / float64(n2) / fontSize
			}
		case TextAlignmentCenter:
			value := (width - value3 - wx) / 2
			fontSize2 := value / fontSize
			objs = append(objs, core.MakeFloat(-fontSize2))
			x += value / 1000.0
		case TextAlignmentRight:
			wx2 := (width - value3 - wx)
			wx := wx2 / fontSize
			objs = append(objs, core.MakeFloat(-wx))
			x += wx2 / 1000.0
		}
		if len(objs) > 0 {
			contentCreator.Add_Tf(fontName, fontSize).Add_TL(fontSize * para.lineHeight).Add_TJ(objs...)
		}
		descent := 0.0
		n3 := 0
		for j, chunk2 := range chunks {
			if i == 0 && j == 0 && chunk != nil {
				continue
			}
			style := &chunk2.Style
			keyName := fontName
			fontSize2 := fontSize
			ok2 := style.OutlineColor != nil
			ok3 := style.HorizontalScaling != DefaultHorizontalScaling
			ok4 := style.OutlineSize != 1
			if ok4 {
				contentCreator.Add_w(style.OutlineSize)
			}
			ok5 := style.RenderingMode != TextRenderingModeFill
			if ok5 {
				contentCreator.Add_Tr(int64(style.RenderingMode))
			}
			ok6 := style.CharSpacing != 0
			if ok6 {
				contentCreator.Add_Tc(style.CharSpacing)
			}
			ok7 := style.TextRise != 0
			if ok7 {
				contentCreator.Add_Ts(style.TextRise)
			}
			if chunk2.VerticalAlignment != TextVerticalAlignmentBaseline {
				fontMetrics := newFontMetrics(chunk2.Style.Font, style.FontSize)
				switch chunk2.VerticalAlignment {
				case TextVerticalAlignmentCenter:
					descent = fontMetrics2.xHeight/2 - fontMetrics.xHeight/2
				case TextVerticalAlignmentBottom:
					descent = fontMetrics2.descent - fontMetrics.descent
				case TextVerticalAlignmentTop:
					descent = value2 - style.FontSize
				}
				if descent != 0.0 {
					contentCreator.Translate(0, descent)
				}
			}
			if para.alignment != TextAlignmentJustify || ok {
				runeMetrics, ok := style.Font.GetRuneMetrics(' ')
				if !ok {
					return ctx, nil, errors.New("the font does not have a space glyph")
				}
				keyName = rows[i][n3]
				fontSize2 = style.FontSize
				wx = runeMetrics.Wx * style.horizontalScale()
			}
			encoder := style.Font.Encoder()
			var data []byte
			var ok8 bool
			font := style.Font
			m := map[string]core.PdfObject{}
			if chunk2.textExpansion != nil {
				m["E"] = core.MakeString(*chunk2.textExpansion)
			}
			if chunk2.actualText != nil {
				m["ActualText"] = core.MakeString(*chunk2.actualText)
			}
			if chunk2.altText != nil {
				m["Alt"] = core.MakeString(*chunk2.altText)
			}
			if chunk2.tagInfo != nil {
				m["MCID"] = core.MakeInteger(chunk2.tagInfo.Mcid)
			}
			structureTypeSpan := model.StructureTypeSpan
			if chunk2.tagInfo != nil && chunk2.tagInfo.StructureType != model.StructureTypeUnknown {
				structureTypeSpan = chunk2.tagInfo.StructureType
			}
			if len(m) > 0 {
				contentCreator.Add_BDC(*core.MakeName(string(structureTypeSpan)), m)
			}
			if para.enableShaping && rows2[i][j] != nil {
				keyName := rows[i][n3]
				if ok8 {
					keyName = core.PdfObjectName(fmt.Sprintf("Font%d", n))
					err := block.resources.SetFontByName(keyName, font.ToPdfObject())
					if err != nil {
						return ctx, nil, err
					}
					n++
					ok8 = false
				}
				contentCreator = contentCreator.SetNonStrokingColor(fn3(style.Color)).Add_Tf(keyName, style.FontSize)
				text := []rune(chunk2.Text)
				fn := func(glyph shaping.Glyph) bool {
					return glyph.RunesCount() == 1 && glyph.TextIndex() < len(text) && text[glyph.TextIndex()] == ' '
				}
				var objs []core.PdfObject
				fn2 := func(glyph shaping.Glyph) {
					if glyph.GlyphID > 0xffff {
						if common.Log.IsLogLevel(common.LogLevelDebug) {
							common.Log.Debug("Shaped glyph ID %d exceeds uint16 - skipping", glyph.GlyphID)
						}
						return
					}
					objs = append(objs, core.MakeHexStringFromHexNumber(uint32(glyph.GlyphID), 2))
				}
				for _, output := range rows2[i][j] {
					if output.Direction.Progression() == di.TowardTopLeft && len(output.Glyphs) > 0 {
						n := 0
						for _, glyph := range output.Glyphs {
							if fn(glyph) {
								n++
							} else {
								break
							}
						}
						n2 := 0
						for i := len(output.Glyphs) - 1; i >= n; i-- {
							if fn(output.Glyphs[i]) {
								n2++
							} else {
								break
							}
						}
						if n > 0 || n2 > 0 {
							for _, glyph := range output.Glyphs[len(output.Glyphs)-n2:] {
								fn2(glyph)
							}
							for _, glyph := range output.Glyphs[n : len(output.Glyphs)-n2] {
								fn2(glyph)
							}
							for _, glyph := range output.Glyphs[:n] {
								fn2(glyph)
							}
						} else {
							for _, glyph := range output.Glyphs {
								fn2(glyph)
							}
						}
					} else {
						for _, glyph := range output.Glyphs {
							fn2(glyph)
						}
					}
				}
				if objs != nil {
					contentCreator.Add_TJ(objs...)
				}
			} else {
				fn := func(r rune) error {
					if r == ' ' {
						if len(data) > 0 {
							if ok2 {
								contentCreator.SetStrokingColor(toPdfColor(style.OutlineColor))
							}
							if ok3 {
								contentCreator.Add_Tz(style.HorizontalScaling)
							}
							keyName := rows[i][n3]
							if ok8 {
								keyName = core.PdfObjectName(fmt.Sprintf("Font%d", n))
								if err := block.resources.SetFontByName(keyName, font.ToPdfObject()); err != nil {
									return err
								}
								n++
								ok8 = false
								encoder = style.Font.Encoder()
							}
							contentCreator.SetNonStrokingColor(fn3(style.Color)).Add_Tf(keyName, style.FontSize).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(data)}...)
							data = nil
						}
						if ok3 {
							contentCreator.Add_Tz(DefaultHorizontalScaling)
						}
						contentCreator.Add_Tf(keyName, fontSize2).Add_TJ([]core.PdfObject{core.MakeFloat(-wx)}...)
						floats2[n3] += wx * fontSize2
					} else {
						if _, ok := encoder.RuneToCharcode(r); !ok {
							if style.MultiFont != nil {
								encoder2, ok := style.MultiFont.Encoder(r)
								if ok {
									if len(data) != 0 {
										fontName := core.PdfObjectName(fmt.Sprintf("Font%d", n))
										if err := block.resources.SetFontByName(keyName, font.ToPdfObject()); err != nil {
											return err
										}
										contentCreator.SetNonStrokingColor(fn3(style.Color)).Add_Tf(fontName, style.FontSize).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(data)}...)
										n++
										data = nil
									}
									encoder = encoder2
									ok8 = true
									font = style.MultiFont.CurrentFont
								}
							} else {
								unsupportedRuneError := UnsupportedRuneError{Message: fmt.Sprintf("unsupported rune in text encoding: %#x (%c)", r, r), Rune: r}
								ctx.errs = append(ctx.errs, unsupportedRuneError)
								common.Log.Debug(unsupportedRuneError.Error())
								if ctx.unsupportedRune <= 0 {
									return nil
								}
								r = ctx.unsupportedRune
							}
						}
						data2 := encoder.Encode(string(r))
						data = append(data, data2...)
					}
					if style.MultiFont != nil {
						style.MultiFont.Reset()
					}
					return nil
				}
				if hasEmojiSequenceMarker(chunk2.Text) {
					graphemeClusters := splitGraphemeClusters(chunk2.Text)
					for _, graphemeCluster := range graphemeClusters {
						if len(graphemeCluster.runes) == 1 && graphemeCluster.runes[0] == '\u000A' {
							continue
						}
						colorGlyph, err := drawColorGlyph(ctx, contentCreator, block, graphemeCluster.text, x, style, m, structureTypeSpan)
						if err != nil {
							return ctx, nil, err
						}
						if colorGlyph {
							continue
						}
						for _, r := range graphemeCluster.runes {
							if err := fn(r); err != nil {
								return ctx, nil, err
							}
						}
					}
				} else {
					for _, r := range chunk2.Text {
						if r == '\u000A' {
							continue
						}
						if isEmoji(r) {
							colorGlyph, err := drawColorGlyph(ctx, contentCreator, block, string(r), x, style, m, structureTypeSpan)
							if err != nil {
								return ctx, nil, err
							}
							if colorGlyph {
								continue
							}
						}
						if err := fn(r); err != nil {
							return ctx, nil, err
						}
					}
				}
			}
			if len(data) > 0 {
				if ok2 {
					contentCreator.SetStrokingColor(toPdfColor(style.OutlineColor))
				}
				if ok3 {
					contentCreator.Add_Tz(style.HorizontalScaling)
				}
				keyName := rows[i][n3]
				if ok8 {
					keyName = core.PdfObjectName(fmt.Sprintf("Font%d", n))
					err := block.resources.SetFontByName(keyName, font.ToPdfObject())
					if err != nil {
						return ctx, nil, err
					}
					n++
					ok8 = false
				}
				contentCreator.SetNonStrokingColor(fn3(style.Color)).Add_Tf(keyName, style.FontSize).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(data)}...)
			}
			if len(m) > 0 {
				contentCreator.Add_EMC()
			}
			float := floats2[n3] / 1000.0
			if style.Underline {
				color := style.UnderlineStyle.Color
				if color == nil {
					color = chunk2.Style.Color
				}
				r, g, b := color.ToRGB()
				x1 := x - ctx.X
				offset := value5 - y + style.TextRise - style.UnderlineStyle.Offset
				basicLines = append(basicLines, &contentdraw.BasicLine{X1: x1, Y1: offset, X2: x1 + float, Y2: offset, LineWidth: chunk2.Style.UnderlineStyle.Thickness, LineColor: model.NewPdfColorDeviceRGB(r, g, b)})
			}
			for i, annotation := range chunk2.annotations {
				var array *core.PdfObjectArray
				if len(chunk2.lineBreaks) == i {
					switch context := annotation.GetContext().(type) {
					case *model.PdfAnnotationLink:
						array = core.MakeArray()
						context.Rect = array
						pdfObjectArray, ok := context.Dest.(*core.PdfObjectArray)
						if ok && pdfObjectArray.Len() == 5 {
							pdfObjectName, ok := pdfObjectArray.Get(1).(*core.PdfObjectName)
							if ok && pdfObjectName.String() == "XYZ" {
								numberAsFloat, err := core.GetNumberAsFloat(pdfObjectArray.Get(3))
								if err == nil {
									pdfObjectArray.Set(3, core.MakeFloat(ctx.PageHeight-numberAsFloat))
								}
							}
						}
					case *model.PdfAnnotationHighlight:
						array = core.MakeArray()
						context.Rect = array
						llx := x
						textRise := value5 + style.TextRise
						points := rotatedCorners(&model.PdfRectangle{Llx: llx, Lly: textRise, Urx: llx + float, Ury: textRise + fontSize4}, para.angle)
						context.QuadPoints = core.MakeArrayFromFloats([]float64{points[0].X, points[0].Y, points[1].X, points[1].Y, points[3].X, points[3].Y, points[2].X, points[2].Y})
					}
					chunk2.lineBreaks = append(chunk2.lineBreaks, true)
				}
				if array != nil {
					point := contentdraw.NewPoint(x-ctx.X, value5+style.TextRise-y).Rotate(para.angle)
					point.X += ctx.X
					point.Y += y
					x, y, width, height := rotatedBounds(float, fontSize4, para.angle)
					point.X += x
					point.Y += y
					array.Clear()
					array.Append(core.MakeFloat(point.X))
					array.Append(core.MakeFloat(point.Y))
					array.Append(core.MakeFloat(point.X + width))
					array.Append(core.MakeFloat(point.Y + height))
				}
				block.AddAnnotation(annotation)
			}
			fn2(style.Color, x, float)
			x += float
			if ok4 {
				contentCreator.Add_w(1.0)
			}
			if ok2 {
				contentCreator.Add_RG(0.0, 0.0, 0.0)
			}
			if ok5 {
				contentCreator.Add_Tr(int64(TextRenderingModeFill))
			}
			if ok6 {
				contentCreator.Add_Tc(0)
			}
			if ok7 {
				contentCreator.Add_Ts(0)
			}
			if ok3 {
				contentCreator.Add_Tz(DefaultHorizontalScaling)
			}
			if descent != 0.0 {
				contentCreator.Translate(0, -descent)
				descent = 0.0
			}
			n3++
		}
	}
	for key, floats := range m2 {
		if floats == nil {
			continue
		}
		switch key2 := key.(type) {
		case *LinearShading:
			key2.SetBoundingBox(floats[0], y, floats[1]-floats[0], height)
			patternName := m[key].(*model.PdfColorPatternType2).PatternName
			if err := block.resources.SetPatternByName(patternName, key2.ToPdfShadingPattern().ToPdfObject()); err != nil {
				return ctx, nil, err
			}
		case *RadialShading:
			key2.SetBoundingBox(floats[0], y, floats[1]-floats[0], height)
			patternName := m[key].(*model.PdfColorPatternType3).PatternName
			if err := block.resources.SetPatternByName(patternName, key2.ToPdfShadingPattern().ToPdfObject()); err != nil {
				return ctx, nil, err
			}
		case *FunctionBasedShading:
			key2.SetBoundingBox(floats[0], y, floats[1]-floats[0], height)
			patternName := m[key].(*model.PdfColorPatternType1).PatternName
			pdfShadingPattern, err := key2.ToPdfShadingPattern()
			if err != nil {
				return ctx, nil, err
			}
			if err := block.resources.SetPatternByName(patternName, pdfShadingPattern.ToPdfObject()); err != nil {
				return ctx, nil, err
			}
		}
	}
	if len(propertyList) > 0 {
		contentCreator.Add_EMC()
	}
	contentCreator.Add_ET()
	for _, basicLine := range basicLines {
		contentCreator.SetStrokingColor(basicLine.LineColor).Add_w(basicLine.LineWidth).Add_m(basicLine.X1, basicLine.Y1).Add_l(basicLine.X2, basicLine.Y2).Add_s()
	}
	contentCreator.Add_Q()
	operations := contentCreator.Operations()
	operations.WrapIfNeeded()
	block.addWrappedContents(operations)
	if isRelative {
		y := height
		ctx.Y += y
		ctx.Height -= y
		if ctx.Inline {
			ctx.X += para.Width() + para.margins.Right
		}
	}
	return ctx, rows5, nil
}
