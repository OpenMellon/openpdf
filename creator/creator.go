// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/graphic2d"
	chartintegration "github.com/OpenMellon/openpdf/internal/integrations/unichart"
	"github.com/OpenMellon/openpdf/internal/license"
	"github.com/OpenMellon/openpdf/internal/linebreak"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/ps"
	"github.com/OpenMellon/typesetting/di"
	"github.com/OpenMellon/typesetting/shaping"
	chartrender "github.com/OpenMellon/unichart/render"
	"golang.org/x/net/html/charset"
	"image"
	imagedraw "image/draw"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"text/template/parse"
	"unicode"
	"unicode/utf8"
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

// AssociateAnnotationWithStructure associates the given annotation with the text chunk's structure element.
// This is necessary for proper tag structure when using annotations in tagged PDFs.
func (tec *TextChunk) AssociateAnnotationWithStructure(annotation *model.PdfAnnotation) {
	if tec.tagInfo == nil {
		tec.tagInfo = model.NewStructureTagInfo()
		tec.tagInfo.StructureType = model.StructureTypeSpan
	}
	containingPdfObject := annotation.GetContainingPdfObject()
	tec.tagInfo.SetObjAttrib(containingPdfObject)
	if dict, ok := core.GetDict(containingPdfObject); ok {
		dict.Set("StructParent", core.MakeInteger(tec.tagInfo.Mcid))
	}
}

// UnsupportedRuneError is an error that occurs when there is unsupported glyph being used.
type UnsupportedRuneError struct {
	Message string
	Rune    rune
}

func newBorder(x, y, width, height float64) *border {
	border2 := &border{}
	border2.x = x
	border2.y = y
	border2.width = width
	border2.height = height
	border2.colorTop = ColorBlack
	border2.colorBottom = ColorBlack
	border2.colorLeft = ColorBlack
	border2.colorRight = ColorBlack
	border2.widthTop = 0
	border2.widthBottom = 0
	border2.widthLeft = 0
	border2.widthRight = 0
	border2.opacity = 1.0
	border2.LineStyle = contentdraw.LineStyleSolid
	return border2
}
func (sp *StyledParagraph) getMaxLineWidth() float64 {
	if sp.lines == nil || (sp.lines != nil && len(sp.lines) == 0) {
		sp.wrapText()
	}
	var maxLineWidth float64
	for _, line := range sp.lines {
		textLineWidth := sp.getTextLineWidth(line)
		if textLineWidth > maxLineWidth {
			maxLineWidth = textLineWidth
		}
	}
	return maxLineWidth
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

// Add appends a new item to the list.
// The supported components are: *StyledParagraph, *Division, *Image, *Table, and *List.
// Returns the marker used for the newly added item. The returned marker
// object can be used to change the text and style of the marker for the
// current item.
func (li *List) Add(item VectorDrawable) (*TextChunk, error) {
	listItem2 := &listItem{content: item, marker: li.marker}
	switch item2 := item.(type) {
	case *StyledParagraph:
	case *List:
		if item2.defaultIndent {
			item2.indent = 15
		}
	case *Division:
	case *Image:
	case *Table:
	default:
		return nil, errors.New("this type of drawable is not supported in list")
	}
	li.items = append(li.items, listItem2)
	return &listItem2.marker, nil
}

// SetDropCapsInline sets inline drop caps for the paragraph, where all lines
// flow beside the enlarged character(s).
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This paragraph has inline drop caps...")
//	p.SetDropCapsInline(creator.DropCapsFirstCharacter, 2, 5.0)
func (sp *StyledParagraph) SetDropCapsInline(scope DropCapsScope, numLines int, gap float64) {
	sp.SetDropCaps(DropCapsOptions{Type: DropCapsInline, Scope: scope, NumLines: numLines, Gap: gap})
}

// ParseTemplate reads and compiles the template from the specified reader using
// the specified `options`, returning a handle that Creator.RenderTemplate or
// Creator.ValidateTemplate (or the Block equivalents) render or validate with
// data. Reading and compiling are done once, so a template rendered many times,
// including across documents, does not pay that cost each render. The `options`
// parameter can be nil.
func ParseTemplate(r io.Reader, options *TemplateOptions) (*ParsedTemplate, error) {
	if r == nil || isNilValue(reflect.ValueOf(r)) {
		common.Log.Error("Template reader cannot be nil.")
		return nil, errInvalidReader
	}
	if options == nil {
		options = &TemplateOptions{}
	}
	options.init()
	fileName := ""
	if options.FileName != "" {
		fileName = options.FileName
	} else if file, ok := r.(*os.File); ok {
		fileName = file.Name()
	}
	buffer := bytes.NewBuffer(nil)
	if _, err := io.Copy(buffer, r); err != nil {
		return nil, err
	}
	ok := !options.DisableAutoEscape
	funcMap := template.FuncMap{"dict": templateDict, "add": templateAdd, "array": templateArray, "extendDict": templateExtendDict, "makeSeq": templateMakeSeq}
	if ok {
		funcMap[rawFuncName] = templateValueToString
		funcMap[xmlEscapeFuncName] = templateXMLEscape
	}
	if options.HelperFuncMap != nil {
		for key, v := range options.HelperFuncMap {
			if _, ok := funcMap[key]; ok {
				common.Log.Error("Helper function `%s` is a reserved template helper and cannot be overridden; using the built-in.", key)
				continue
			}
			funcMap[key] = v
		}
	}
	funcs := template.New("").Funcs(funcMap)
	if options.Strict {
		funcs.Option("missingkey=error")
	}
	funcs, err := funcs.Parse(buffer.String())
	if err != nil {
		return nil, err
	}
	if options.SubtemplateMap != nil {
		for key, src := range options.SubtemplateMap {
			if key == "" {
				common.Log.Debug("Subtemplate name cannot be empty. Skipping. Output may be incorrect.")
				continue
			}
			if src == nil {
				common.Log.Debug("Subtemplate content cannot be nil. Skipping. Output may be incorrect.")
				continue
			}
			buffer := bytes.NewBuffer(nil)
			if _, err := io.Copy(buffer, src); err != nil {
				return nil, err
			}
			if _, err := funcs.New(key).Parse(buffer.String()); err != nil {
				return nil, err
			}
		}
	}
	if ok {
		for _, template := range funcs.Templates() {
			if template.Tree != nil {
				addEscapeToTemplate(template.Root)
			}
		}
	}
	return &ParsedTemplate{options: options, tmpl: funcs, source: fileName}, nil
}

// SetAddressStyle sets the style properties used to render the content of
// the invoice address sections.
func (i *Invoice) SetAddressStyle(style TextStyle) { i.addressStyle = style }
func (tp *templateProcessor) render() error        { return tp.process(false) }

// SetPos sets the Table's positioning to absolute mode and specifies the upper-left corner
// coordinates as (x,y).
// Note that this is only sensible to use when the table does not wrap over multiple pages.
// TODO: Should be able to set width too (not just based on context/relative positioning mode).
func (t *Table) SetPos(x, y float64) {
	t.positioning = PositionAbsolute
	t.xPos = x
	t.yPos = y
}
func drawTemplate(creator *Creator, r io.Reader, data interface{}, options *TemplateOptions, renderer componentRenderer) error {
	if creator == nil {
		common.Log.Error("Creator instance cannot be nil.")
		return errInvalidCreator
	}
	template, err := ParseTemplate(r, options)
	if err != nil {
		return err
	}
	return template.renderInto(creator, renderer, data)
}

// AddColorStop add color stop info for rendering gradient color.
func (rs *RadialShading) AddColorStop(color Color, point float64) {
	rs.base.AddColorStop(color, point)
}
func isZeroWidthJoiner(r rune) bool { return r == 0x200D }

// SetNumber sets the number of the invoice.
func (i *Invoice) SetNumber(number string) (*InvoiceCell, *InvoiceCell) {
	i.number[1].Value = number
	return i.number[0], i.number[1]
}

type grayColor struct{ gray float64 }

func parseChapterHeadingTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseChapterHeading(node)
}
func newLine(x1, y1, x2, y2 float64) *Line {
	return &Line{x1: x1, y1: y1, x2: x2, y2: y2, color: ColorBlack, opacity: 1.0, lineWidth: 1.0, dashArray: []int64{1, 1}, positioning: PositionAbsolute}
}

// SetFillOpacity sets the fill opacity of the ellipse.
func (e *Ellipse) SetFillOpacity(opacity float64) { e.fillOpacity = opacity }

// ColorCMYKFromArithmetic creates a Color from arithmetic color values (0-1).
// Example:
//
//	green := ColorCMYKFromArithmetic(1.0, 0.0, 1.0, 0.0)
func ColorCMYKFromArithmetic(c, m, y, k float64) Color {
	return cmykColor{c: math.Max(math.Min(c, 1.0), 0.0), m: math.Max(math.Min(m, 1.0), 0.0), y: math.Max(math.Min(y, 1.0), 0.0), k: math.Max(math.Min(k, 1.0), 0.0)}
}

// Scale sets the scale ratio with `X` factor and `Y` factor for the graphic svg.
func (gs *GraphicSVG) Scale(xFactor, yFactor float64) {
	gs.root.Width = xFactor * gs.root.Width
	gs.root.Height = yFactor * gs.root.Height
	gs.root.SetScaling(xFactor, yFactor)
}

// SetWidthLeft sets border width for left.
func (bo *border) SetWidthLeft(bw float64) { bo.widthLeft = bw }

const (
	TextOverflowVisible TextOverflow = iota
	TextOverflowHidden
)

func newTOCLineFromChunks(number, title, page TextChunk, level uint, style TextStyle) *TOCLine {
	styledParagraph := newStyledParagraph(style)
	styledParagraph.SetEnableWrap(true)
	styledParagraph.SetTextAlignment(TextAlignmentLeft)
	styledParagraph.SetMargins(0, 0, 2, 2)
	tocLine := &TOCLine{paragraph: styledParagraph, Number: number, Title: title, Page: page, Separator: TextChunk{Text: ".", Style: style}, left: 0, level: level, levelOffset: 10, positioning: PositionRelative}
	styledParagraph.margins.Left = tocLine.left + float64(tocLine.level-1)*tocLine.levelOffset
	styledParagraph.beforeRender = tocLine.prepareParagraph
	return tocLine
}

// GetMargins returns the Grid's margins: left, right, top, bottom.
func (g *Grid) GetMargins() (float64, float64, float64, float64) {
	return g.margins.Left, g.margins.Right, g.margins.Top, g.margins.Bottom
}
func (gse *GraphicSVGElement) drawPolyline(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	svgPoints, err := parseSVGPoints(gse.Attributes["points"])
	if err != nil {
		common.Log.Debug("ERROR unable to parse points attribute: %v", err)
		return
	}
	if len(svgPoints)%2 > 0 {
		common.Log.Debug("ERROR invalid points attribute length")
		return
	}
	for i := 0; i < len(svgPoints); {
		if i == 0 {
			cc.Add_m(svgPoints[i]*gse.scale, svgPoints[i+1]*gse.scale)
		} else {
			cc.Add_l(svgPoints[i]*gse.scale, svgPoints[i+1]*gse.scale)
		}
		i += 2
	}
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

// SetBorderWidth sets the border width.
func (p *Polygon) SetBorderWidth(borderWidth float64) { p.polygon.BorderWidth = borderWidth }
func (c *Creator) initContext() {
	c.context.X = precision.RoundDefault(c.pageMargins.Left)
	c.context.Y = precision.RoundDefault(c.pageMargins.Top)
	c.context.Width = precision.RoundDefault(c.pageWidth - c.pageMargins.Right - c.pageMargins.Left)
	c.context.Height = precision.RoundDefault(c.pageHeight - c.pageMargins.Bottom - c.pageMargins.Top)
	c.context.PageHeight = precision.RoundDefault(c.pageHeight)
	c.context.PageWidth = precision.RoundDefault(c.pageWidth)
	c.context.Margins = c.pageMargins
	c.context.unsupportedRune = c.UnsupportedCharacterReplacement
}

// SetStyleTop sets border style for top side.
func (bo *border) SetStyleTop(style CellBorderStyle) { bo.styleTop = style }
func tokenizePathData(s2 string) []token {
	var (
		tokens []token
		s      string
	)
	for _, r := range s2 {
		text := string(r)
		switch {
		case svgCommands.isCommand(text):
			tokens, s = flushToken(tokens, s)
			tokens = append(tokens, token{text, true})
		case text == ".":
			if s == "" {
				s = "0"
			}
			if strings.Contains(s, text) {
				tokens = append(tokens, token{s, false})
				s = "0"
			}
			fallthrough
		case text >= "0" && text <= "9" || text == "e":
			s += text
		case text == "-":
			if strings.HasSuffix(s, "e") {
				s += text
			} else {
				tokens, _ = flushToken(tokens, s)
				s = text
			}
		default:
			tokens, s = flushToken(tokens, s)
		}
	}
	tokens, _ = flushToken(tokens, s)
	return tokens
}

// SetForms adds an Acroform to a PDF file.  Sets the specified form for writing.
func (c *Creator) SetForms(form *model.PdfAcroForm) error { c.acroForm = form; return nil }

// TOC returns the table of contents component of the creator.
func (c *Creator) TOC() *TOC { return c.toc }

// Insert adds a new text chunk at the specified position in the paragraph.
func (sp *StyledParagraph) Insert(index uint, text string) *TextChunk {
	chunksLen := uint(len(sp.chunks))
	if index > chunksLen {
		index = chunksLen
	}
	textChunk := NewTextChunk(text, sp.defaultStyle)
	sp.chunks = append(sp.chunks[:index], append([]*TextChunk{textChunk}, sp.chunks[index:]...)...)
	sp.wrapText()
	return textChunk
}

// GetHorizontalAlignment returns the cell's horizontal alignment of content.
func (gc *GridCell) GetHorizontalAlignment() CellHorizontalAlignment { return gc.horizontalAlignment }

type chapterRecordFunc func(chapter *Chapter, page int)

func (ch *Chapter) drawBackground(background []*Block, ctx2, ctx3 DrawContext, m map[int]float64, m2 map[int]bool) ([]*Block, error) {
	backgroundLen := len(background)
	if backgroundLen == 0 || ch.background == nil {
		return background, nil
	}
	n, n2 := -1, -1
	for i, block := range background {
		if !block.isEmpty() {
			if n == -1 {
				n = i
			}
			n2 = i
		}
	}
	blocks := make([]*Block, 0, backgroundLen)
	ok2 := false
	for i, block := range background {
		if block.isEmpty() {
			if m2[i] {
				ok2 = true
			}
			blocks = append(blocks, block)
			continue
		}
		var (
			borderRadiusTopLeft     = ch.background.BorderRadiusTopLeft
			borderRadiusTopRight    = ch.background.BorderRadiusTopRight
			borderRadiusBottomLeft  = ch.background.BorderRadiusBottomLeft
			borderRadiusBottomRight = ch.background.BorderRadiusBottomRight
		)
		ctx := ctx2
		ctx.Page += i
		value, ok := m[i]
		ok3 := m2[i] || ok2 || i == n
		ok2 = false
		y := ctx2.Y
		if i != 0 {
			ctx.X = ctx.Margins.Left
			ctx.Y = ctx.Margins.Top
			ctx.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right
			y = ctx.Margins.Top
		}
		switch {
		case ok:
			ctx.Height = value - y
		case i == n2:
			ctx.Height = ctx3.Y - y
		default:
			ctx.Height = ctx.PageHeight - ctx.Margins.Bottom - y
		}
		if ctx.Height <= 0 {
			blocks = append(blocks, block)
			continue
		}
		if !ok3 {
			borderRadiusTopLeft = 0
			borderRadiusTopRight = 0
		}
		if !(i == n2 || ok) {
			borderRadiusBottomLeft = 0
			borderRadiusBottomRight = 0
		}
		rectangle := newRectangle(ctx.X, ctx.Y, ctx.Width, ctx.Height)
		rectangle.SetFillColor(ch.background.FillColor)
		rectangle.SetBorderColor(ch.background.BorderColor)
		rectangle.SetBorderWidth(ch.background.BorderSize)
		rectangle.SetBorderRadius(borderRadiusTopLeft, borderRadiusTopRight, borderRadiusBottomLeft, borderRadiusBottomRight)
		pageBlocks, _, err := rectangle.GeneratePageBlocks(ctx)
		if err != nil {
			return nil, err
		}
		if len(pageBlocks) == 0 {
			blocks = append(blocks, block)
			continue
		}
		pageBlock := pageBlocks[0]
		contentStreamOperations := &contentstream.ContentStreamOperations{}
		if err = mergeContentStreams(contentStreamOperations, block.resources, pageBlock.contents, pageBlock.resources); err != nil {
			return nil, err
		}
		*block.contents = append(*contentStreamOperations, *block.contents...)
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// SetBorderColor sets the border color of the ellipse.
func (e *Ellipse) SetBorderColor(col Color) { e.borderColor = col }
func (t *Table) topHeaderRows() int {
	if t.hasHeaderRows && t.headerStartRow == 1 {
		return t.headerEndRow
	}
	return 0
}

// TOCLine represents a line in a table of contents.
// The component can be used both in the context of a
// table of contents component and as a standalone component.
// The representation of a table of contents line is as follows:
/*
         [number] [title]      [separator] [page]
   e.g.: Chapter1 Introduction ........... 1
*/
type TOCLine struct {
	paragraph *StyledParagraph

	// Holds the text and style of the number part of the TOC line.
	Number TextChunk

	// Holds the text and style of the title part of the TOC line.
	Title TextChunk

	// Holds the text and style of the separator part of the TOC line.
	Separator TextChunk

	// Holds the text and style of the page part of the TOC line.
	Page        TextChunk
	left        float64
	level       uint
	levelOffset float64
	positioning Positioning
	linkX       float64
	linkY       float64
	linkPage    int64
}

// GeneratePageBlocks draws the line on a new block representing the page.
// Implements the Drawable interface.
func (l *Line) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks         []*Block
		block          = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx2           = ctx
		x1, pageHeight = l.x1, ctx.PageHeight - l.y1
		x2, y2         = l.x2, ctx.PageHeight - l.y2
	)
	isRelative := l.positioning.IsRelative()
	if isRelative {
		ctx.X += l.margins.Left
		ctx.Y += l.margins.Top
		ctx.Width -= l.margins.Left + l.margins.Right
		ctx.Height -= l.margins.Top + l.margins.Bottom
		x1, pageHeight, x2, y2 = l.computeCoords(ctx)
		if l.Height() > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + l.margins.Top
			ctx2.X = ctx.Margins.Left + l.margins.Left
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - l.margins.Top - l.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - l.margins.Left - l.margins.Right
			ctx = ctx2
			x1, pageHeight, x2, y2 = l.computeCoords(ctx)
		}
	}
	basicLine := contentdraw.BasicLine{X1: x1, Y1: pageHeight, X2: x2, Y2: y2, LineColor: toPdfColor(l.color), Opacity: l.opacity, LineWidth: l.lineWidth, LineStyle: l.style, DashArray: l.dashArray, DashPhase: l.dashPhase}
	gsName, err := block.setOpacity(1.0, l.opacity)
	if err != nil {
		return nil, ctx, err
	}
	markedDraw, _, err := basicLine.MarkedDraw(gsName, l.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		height := l.Height()
		ctx.Y += height + l.margins.Bottom
		ctx.Height -= height
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}
func (tp *templateProcessor) parseRectangle(node *templateNode) (interface{}, error) {
	rectangle := tp.creator.NewRectangle(0, 0, 0, 0)
	if err := tp.applyAttrs(node, rectangle, rectangleAttrs); err != nil {
		return nil, err
	}
	return rectangle, nil
}
func parseRGBColor(s string) (float64, float64, float64) {
	s2 := strings.TrimPrefix(s, "rgb(")
	s2 = strings.TrimSuffix(s2, ")")
	strs := strings.Split(s2, ",")
	if len(strs) != 3 {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	var value, value2, value3 float64
	value, err := parseColorComponent(strs[0])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value2, err = parseColorComponent(strs[1])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value3, err = parseColorComponent(strs[2])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value4 := value / 255.0
	value5 := value2 / 255.0
	value6 := value3 / 255.0
	return value4, value5, value6
}

// NewChart creates a new creator drawable based on the provided
// unichart chart component.
func NewChart(chart chartrender.ChartRenderable) *Chart { return newChart(chart) }

// CreateTableOfContents sets a function to generate table of contents.
//
// Chapters drawn inside the callback (via NewChapter + Draw) are not tracked
// by Creator.ChapterForPage and will not appear in header/footer callback
// args. The callback is expected to customize the TOC layout, not add
// chapter bodies.
func (c *Creator) CreateTableOfContents(genTOCFunc func(toc *TOC) error) {
	c.genTOCFunc = genTOCFunc
}

// SetFillColor sets the fill color of the rectangle.
func (r *Rectangle) SetFillColor(col Color) { r.fillColor = col }

// GetPosition returns the 1-based row and column the cell starts from.
// Consistent with TableCell.GetPosition.
func (gc *GridCell) GetPosition() (row, col int) {
	return gc.row + 1, gc.col + 1
}

// AddInfo is used to append a piece of invoice information in the template
// information table.
func (i *Invoice) AddInfo(description, value string) (*InvoiceCell, *InvoiceCell) {
	invoiceCells := [2]*InvoiceCell{i.newCell(description, i.defaultCellProps), i.newCell(value, i.defaultCellProps)}
	i.infoLines = append(i.infoLines, invoiceCells)
	return invoiceCells[0], invoiceCells[1]
}

// SetAltText sets the alternative text for the text chunk.
func (tec *TextChunk) SetAltText(text string) { tec.altText = &text }

var backgroundAttrs = map[string]attrApplyFunc{"fill-color": colorAttr(func(background *Background, fillColor Color) { background.FillColor = fillColor }), "border-color": colorAttr(func(background *Background, borderColor Color) { background.BorderColor = borderColor }), "border-size": floatAttr(func(background *Background, borderSize float64) { background.BorderSize = borderSize }), "border-radius": borderRadiusAttr(func(background *Background, topLeft, topRight, bottomRight, bottomLeft float64) {
	background.SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight)
}), "border-top-left-radius": floatAttr(func(background *Background, borderRadiusTopLeft float64) {
	background.BorderRadiusTopLeft = borderRadiusTopLeft
}), "border-top-right-radius": floatAttr(func(background *Background, borderRadiusTopRight float64) {
	background.BorderRadiusTopRight = borderRadiusTopRight
}), "border-bottom-left-radius": floatAttr(func(background *Background, borderRadiusBottomLeft float64) {
	background.BorderRadiusBottomLeft = borderRadiusBottomLeft
}), "border-bottom-right-radius": floatAttr(func(background *Background, borderRadiusBottomRight float64) {
	background.BorderRadiusBottomRight = borderRadiusBottomRight
})}

func addEscapeToTemplate(node parse.Node) {
	switch listNode := node.(type) {
	case *parse.ListNode:
		if listNode == nil {
			return
		}
		for _, node := range listNode.Nodes {
			addEscapeToTemplate(node)
		}
	case *parse.ActionNode:
		addEscapeToPipe(listNode.Pipe)
	case *parse.IfNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	case *parse.RangeNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	case *parse.WithNode:
		addEscapeToTemplate(listNode.List)
		addEscapeToTemplate(listNode.ElseList)
	}
}

// RotatedSize returns the width and height of the rotated block.
func (b *Block) RotatedSize() (float64, float64) {
	_, _, width, height := rotatedBounds(b.width, b.height, b.angle)
	return width, height
}

// GraphicSVG represents a drawable graphic SVG.
// It is used to render the graphic SVG components using a creator instance.
type GraphicSVG struct {
	taggedDrawable
	root        *GraphicSVGElement
	positioning Positioning
	xPos        float64
	yPos        float64
	margins     Margins
}

func newImageFromFile(name string) (*Image, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, err := model.ImageHandling.Read(file)
	if err != nil {
		common.Log.Error("Error loading image: %s", err)
		return nil, err
	}
	return newImage(img)
}

// SetIndent sets the left offset of the list when nested into another list.
func (li *List) SetIndent(indent float64) { li.indent = indent; li.defaultIndent = false }

// NewRectangle creates a new rectangle with the left corner at (`x`, `y`),
// having the specified width and height.
// NOTE: In relative positioning mode, `x` and `y` are calculated using the
// current context. Furthermore, when the fit mode is set to fill the available
// space, the rectangle is scaled so that it occupies the entire context width
// while maintaining the original aspect ratio.
func (c *Creator) NewRectangle(x, y, width, height float64) *Rectangle {
	return newRectangle(x, y, width, height)
}
func generateTablePageBlocks(table *Table, ctx DrawContext) ([]*Block, DrawContext, error) {
	var blocks []*Block
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if table.positioning.IsAbsolute() {
		table.updateRowHeights(ctx.Width)
	} else {
		table.updateRowHeights(ctx.Width - table.margins.Left - table.margins.Right)
	}
	top := table.margins.Top
	if table.positioning.IsRelative() && !table.enablePageWrap {
		height := table.Height()
		if height > ctx.Height-table.margins.Top && height <= ctx.PageHeight-ctx.Margins.Top-ctx.Margins.Bottom {
			blocks = []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y)}
			var err error
			if _, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			top = 0
		}
	}
	ctx2 := ctx
	if table.positioning.IsAbsolute() {
		ctx.X = table.xPos
		ctx.Y = table.yPos
	} else {
		ctx.X += table.margins.Left
		ctx.Y += top
		ctx.Width -= table.margins.Left + table.margins.Right
		ctx.Height -= top
	}
	width := ctx.Width
	x := ctx.X
	y := ctx.Y
	height := ctx.Height
	row := 0
	n, n2 := -1, -1
	if table.hasHeaderRows {
		for i, cell := range table.cells {
			if cell.row < table.headerStartRow {
				continue
			}
			if cell.row > table.headerEndRow {
				break
			}
			if n < 0 {
				n = i
			}
			n2 = i
		}
	}
	if err := table.wrapContent(ctx); err != nil {
		return nil, ctx, err
	}
	table.updateRowHeights(width)
	contentHeight := ctx.contentHeight()
	for _, cell := range table.cells {
		if cell.wrapDisabled && cell.row-1 < len(table.rowHeights) && table.rowHeights[cell.row-1] > contentHeight {
			table.rowHeights[cell.row-1] = contentHeight
		}
	}
	var (
		ok               bool
		n3               int
		row2             int
		ok2              bool
		row3             int
		err              error
		mcid             int64
		rowTag           *model.KDict
		structureTagInfo *model.StructureTagInfo
		ok3              bool
		ok4              = table.tagInfo != nil && table.tagInfo.ApplyTag
		n4               = -1
		n5               = -1
	)
	if ok4 {
		beginTaggedContent(block, table.tagInfo)
		mcid = table.tagInfo.Mcid + 1
	}
	for i := 0; i < len(table.cells); i++ {
		cell := table.cells[i]
		if lastCell, cell2 := table.getLastCellFromCol(cell.col); lastCell == i {
			if (cell2.row + cell2.rowSpan - 1) < table.rows {
				for i := cell.row; i < table.rows; i++ {
					cell2 := &TableCell{}
					cell2.row = i + 1
					cell2.rowSpan = 1
					cell2.col = cell.col
					table.cells = append(table.cells, cell2)
				}
			}
		}
		width2 := cell.width(table.colWidths, width)
		colWidth := float64(0.0)
		for i := 0; i < cell.col-1; i++ {
			colWidth += table.colWidths[i] * width
		}
		rowHeight := float64(0.0)
		for i := row; i < cell.row-1; i++ {
			rowHeight += table.rowHeights[i]
		}
		ctx.Height = height - rowHeight
		contentHeight := float64(0.0)
		for i := 0; i < cell.rowSpan; i++ {
			contentHeight += table.rowHeights[cell.row+i-1]
		}
		ok5 := ok2 && cell.row != row3
		row3 = cell.row
		if ok5 || contentHeight > ctx.Height {
			if table.enableRowWrap && !ok2 {
				ok2, err = table.wrapRow(i, ctx, width, ctx.contentHeight()-table.headerHeight())
				if err != nil {
					return nil, ctx, err
				}
				if ok2 {
					i--
					continue
				}
			}
			if ok4 && !ok3 {
				if structureTagInfo != nil {
					endMarkedContent(block)
					structureTagInfo = nil
				}
				endMarkedContent(block)
				ok3 = true
			}
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			x = ctx.Margins.Left + table.margins.Left
			y = ctx.Margins.Top
			ctx.Height = ctx.contentHeight()
			ctx.Page++
			height = ctx.Height
			row = cell.row - 1
			rowHeight = 0
			ok2 = false
			if table.hasHeaderRows && n >= 0 {
				if cell.rowSpan > (table.rows - row3 + 1) {
					common.Log.Debug("Table header rowspan exceeds available space.")
					n, n2 = -1, -1
				} else if n5 == i {
					common.Log.Debug("Table header rowspan exceeds available space.")
				} else {
					n5 = i
					n3 = i
					i = n - 1
					row2 = row
					row = table.headerStartRow - 1
					ok = true
					continue
				}
			}
			if ok5 {
				i--
				continue
			}
		}
		ok2 := ok4 && ok
		if ok4 && !ok2 && ok3 {
			beginTaggedContent(block, table.tagInfo)
			ok3 = false
		}
		if ok4 && !ok2 && structureTagInfo == nil {
			structureTagInfo = &model.StructureTagInfo{Mcid: mcid}
			switch table.tagInfo.StructureType {
			case model.StructureTypeTable:
				structureTagInfo.StructureType = model.StructureTypeTableRow
				rowTag = table.addRowTag()
			case model.StructureTypeList:
				structureTagInfo.StructureType = model.StructureTypeListItem
				rowTag = table.addListItemTag()
			}
			beginTaggedContent(block, structureTagInfo)
			mcid++
		}
		ctx.Width = width2
		ctx.X = x + colWidth
		ctx.Y = y + rowHeight
		if contentHeight > ctx.PageHeight-ctx.Margins.Top-ctx.Margins.Bottom {
			contentHeight = ctx.contentHeight()
		}
		border := newBorder(ctx.X, ctx.Y, width2, contentHeight)
		if cell.backgroundColor != nil {
			border.SetFillColor(cell.backgroundColor)
		}
		border.SetOpacity(cell.opacity)
		border.LineStyle = cell.borderLineStyle
		border.styleLeft = cell.borderStyleLeft
		border.styleRight = cell.borderStyleRight
		border.styleTop = cell.borderStyleTop
		border.styleBottom = cell.borderStyleBottom
		if cell.borderColorLeft != nil {
			border.SetColorLeft(cell.borderColorLeft)
		}
		if cell.borderColorBottom != nil {
			border.SetColorBottom(cell.borderColorBottom)
		}
		if cell.borderColorRight != nil {
			border.SetColorRight(cell.borderColorRight)
		}
		if cell.borderColorTop != nil {
			border.SetColorTop(cell.borderColorTop)
		}
		border.SetWidthBottom(cell.borderWidthBottom)
		border.SetWidthLeft(cell.borderWidthLeft)
		border.SetWidthRight(cell.borderWidthRight)
		border.SetWidthTop(cell.borderWidthTop)
		contentsLen, annotationsLen, mcid2 := len(*block.contents), len(block.annotations), mcid
		err := block.Draw(border)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
		}
		var kChild *model.KDict
		if ok4 && !ok2 {
			cell.SetStructPageNumber(table.tagInfo.StructPageNumber)
			ok := table.hasHeaderRows && cell.row >= table.headerStartRow && cell.row <= table.headerEndRow
			structureTypeTableData := model.StructureTypeTableData
			switch structureType := cell.tagInfo.StructureType; {
			case structureType != "" && structureType != model.StructureTypeTableData:
				structureTypeTableData = structureType
			case ok:
				structureTypeTableData = model.StructureTypeTableHeaderCell
			}
			cell.SetStructureType(structureTypeTableData)
			kChild = model.NewKDictionary()
			kChild.S = core.MakeName(string(structureTypeTableData))
			if structureTypeTableData == model.StructureTypeTableHeaderCell {
				dict := core.MakeDict()
				dict.Set("O", core.MakeName("Table"))
				dict.Set("Scope", core.MakeName("Column"))
				kChild.A = core.MakeIndirectObject(dict)
			}
			rowTag.AddKChild(kChild)
		}
		if cell.content != nil {
			if kChild != nil {
				cell.SetMarkedContentID(mcid)
				beginTaggedContent(block, &model.StructureTagInfo{Mcid: mcid, StructureType: cell.tagInfo.StructureType})
				kValue := model.NewKValue()
				kValue.SetMCID(int(mcid))
				kChild.AddChild(kValue)
				mcid++
			}
			width := cell.content.Width()
			height := cell.content.Height()
			width, height, y, kChild2, ok, err := layoutCellContent(cell.content, width, height, cell.verticalAlignment, table.compactMode, kChild, mcid, width2)
			if err != nil {
				return nil, ctx, err
			}
			if kChild2 != nil {
				if !ok {
					kChild.AddKChild(kChild2)
				}
				mcid++
			}
			switch cell.horizontalAlignment {
			case CellHorizontalAlignmentLeft:
				ctx.X += cell.indent
				ctx.Width -= cell.indent
			case CellHorizontalAlignmentCenter:
				if width3 := width2 - width; width3 > 0 {
					ctx.X += width3 / 2
					ctx.Width -= width3 / 2
				}
			case CellHorizontalAlignmentRight:
				if width2 > width {
					ctx.X = ctx.X + width2 - width - cell.indent
					ctx.Width -= cell.indent
				}
			}
			y2 := ctx.Y
			height2 := ctx.Height
			ctx.Y += y
			switch cell.verticalAlignment {
			case CellVerticalAlignmentTop:
			case CellVerticalAlignmentMiddle:
				if contentHeight2 := contentHeight - height; contentHeight2 > 0 {
					ctx.Y += contentHeight2 / 2
					ctx.Height -= contentHeight2 / 2
				}
			case CellVerticalAlignmentBottom:
				if contentHeight > height {
					ctx.Y = ctx.Y + contentHeight - height
					ctx.Height = contentHeight
				}
			}
			fn := func(d VectorDrawable) error {
				if ok2 {
					for _, structureTagInfo := range collectStructureTags(d, nil) {
						structureTagInfo.ApplyTag = false
						defer func() { structureTagInfo.ApplyTag = true }()
					}
				}
				return block.DrawWithContext(d, ctx)
			}
			err = fn(cell.content)
			if errors.Is(err, ErrContentNotFit) {
				contentHeight := ctx.contentHeight()
				ok := cell.wrapDisabled && ctx.Height >= contentHeight-0.1
				if !ok5 && n4 != i && !ok {
					*block.contents = (*block.contents)[:contentsLen]
					block.annotations = block.annotations[:annotationsLen]
					if kChild != nil {
						rowTag.RemoveKChild(kChild)
					}
					mcid = mcid2
					n4 = i
					i--
					continue
				}
				if drawableToFit := splitDrawableToFit(cell.content, ctx); drawableToFit != nil {
					err = fn(drawableToFit)
				}
			}
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
			ctx.Y = y2
			ctx.Height = height2
		}
		if ok2 {
			wrapAsPaginationArtifact(block, contentsLen)
		}
		ctx.Y += contentHeight
		ctx.Height -= contentHeight
		if ok && i+1 > n2 {
			y += rowHeight + contentHeight
			height -= contentHeight + rowHeight
			row = row2
			i = n3 - 1
			ok = false
		}
		if ok4 && !ok2 {
			if cell.content != nil {
				endMarkedContent(block)
			}
			if structureTagInfo != nil && (i+1 == len(table.cells) || table.cells[i+1].row != cell.row) {
				endMarkedContent(block)
				structureTagInfo = nil
			}
		}
	}
	if ok4 && !ok3 {
		endMarkedContent(block)
	}
	blocks = append(blocks, block)
	if table.positioning.IsAbsolute() {
		return blocks, ctx2, nil
	}
	ctx.X = ctx2.X
	ctx.Width = ctx2.Width
	ctx.Y += table.margins.Bottom
	ctx.Height -= table.margins.Bottom
	return blocks, ctx, nil
}

// SetColorLeft sets border color for left.
func (bo *border) SetColorLeft(col Color) { bo.colorLeft = col }

// Invoice represents a configurable invoice template.
type Invoice struct {
	title               string
	logo                *Image
	buyerAddress        *InvoiceAddress
	sellerAddress       *InvoiceAddress
	addressSeparator    string
	number              [2]*InvoiceCell
	date                [2]*InvoiceCell
	dueDate             [2]*InvoiceCell
	infoLines           [][2]*InvoiceCell
	columns             []*InvoiceCell
	lines               [][]*InvoiceCell
	subtotal            [2]*InvoiceCell
	total               [2]*InvoiceCell
	totalLines          [][2]*InvoiceCell
	notes               [2]string
	terms               [2]string
	sections            [][2]string
	defaultStyle        TextStyle
	headingStyle        TextStyle
	titleStyle          TextStyle
	addressStyle        TextStyle
	addressHeadingStyle TextStyle
	noteStyle           TextStyle
	noteHeadingStyle    TextStyle
	defaultCellProps    InvoiceCellProps
	headingCellProps    InvoiceCellProps
	lineProps           InvoiceCellProps
	totalCellProps      InvoiceCellProps
	positioning         Positioning
}

// SetStructPageNumber sets the page object where the structure element for this drawable is located.
func (td *taggedDrawable) SetStructPageNumber(pageNumber *int64) {
	if td.tagInfo == nil {
		td.tagInfo = model.NewStructureTagInfo()
		td.tagInfo.StructureType = td.structType
	}
	td.tagInfo.StructPageNumber = pageNumber
}
func (t *Table) splitSubtables(i2 int, ctx2 DrawContext, totalWidth, height float64) map[int][2]*Table {
	row := t.cells[i2].row
	var subtableSplits []subtableSplit
	ok2 := false
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		cell := t.cells[i]
		table, ok := cell.content.(*Table)
		if !ok {
			continue
		}
		width := cell.width(t.colWidths, totalWidth)
		height2 := cell.height(width)
		if !table.enablePageWrap && height2 <= height {
			continue
		}
		if height2 > ctx2.Height {
			ok2 = true
		}
		ctx := ctx2
		ctx.Width = width - cell.indent
		subtableSplits = append(subtableSplits, subtableSplit{index: i, table: table, ctx: ctx, height: height})
	}
	if !ok2 {
		return nil
	}
	rows, ok := mergeSubtableSplits(subtableSplits)
	if !ok {
		return nil
	}
	if allFirstPartsEmpty(rows) {
		return nil
	}
	m := make(map[int][2]*Table, len(subtableSplits))
	for i, subtableSplit := range subtableSplits {
		m[subtableSplit.index] = rows[i]
	}
	return m
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (fbs *FunctionBasedShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }

// AddTextItem appends a new item with the specified text to the list.
// The method creates a styled paragraph with the specified text and returns
// it so that the item style can be customized.
// The method also returns the marker used for the newly added item.
// The marker object can be used to change the text and style of the marker
// for the current item.
func (li *List) AddTextItem(text string) (*StyledParagraph, *TextChunk, error) {
	styledParagraph := newStyledParagraph(li.textStyle)
	styledParagraph.Append(text)
	chunk, err := li.Add(styledParagraph)
	return styledParagraph, chunk, err
}
func (tp *templateProcessor) walk(activeParent2 *templateNode, attrs map[string]string, ok2 bool) error {
	if activeParent2.start.Name.Local == styleElementName {
		return nil
	}
	activeParent2.attrs = attrs
	templateTag, ok := templateTags[activeParent2.start.Name.Local]
	if !ok {
		if tp.options.Strict {
			return tp.nodeError(activeParent2, "unknown tag <%s>", activeParent2.start.Name.Local)
		}
		tp.nodeLogDebug(activeParent2, "Unsupported template tag <%s>. Skipping. Output may be incorrect.", activeParent2.start.Name.Local)
	} else {
		effectiveParentName := tp.effectiveParentName(activeParent2)
		s, ok3 := effectiveParentName, false
		if _, ok := templateTag.allowedParents[effectiveParentName]; ok {
			ok3 = true
			if activeParent2.start.Name.Local == "page-break" {
				if pageBreakInvalidContext := tp.pageBreakInvalidContext(activeParent2); pageBreakInvalidContext != "" {
					ok3, s = false, pageBreakInvalidContext
				}
			}
		}
		if !ok3 {
			if tp.options.Strict {
				return tp.nodeError(activeParent2, "tag <%s> is not valid inside <%s>", activeParent2.start.Name.Local, s)
			}
			tp.nodeLogDebug(activeParent2, "Tag <%s> is not valid inside <%s>. Skipping.", activeParent2.start.Name.Local, s)
		} else {
			if templateTag2 := templateTags[effectiveParentName]; templateTag2 != nil && templateTag2.singleChild && !templateTag.allowMultipleChildren {
				if activeParent := activeParent2.activeParent(); !activeParent.hasChild {
					activeParent.hasChild = true
				} else {
					if tp.options.Strict {
						return tp.nodeError(activeParent2, "tag <%s> takes a single child; wrap multiple components in <division>", effectiveParentName)
					}
					tp.nodeLogError(activeParent2, "tag <%s> takes a single child; wrap multiple components in <division>. Skipping <%s>.", effectiveParentName, activeParent2.start.Name.Local)
					ok2 = true
				}
			}
			if ok2 {
				activeParent2.hasHandler = templateTag.parse != nil
				if err := tp.applyAttrs(activeParent2, nil, templateTag.attrs); err != nil {
					return err
				}
				if err := tp.validateLink(activeParent2); err != nil {
					return err
				}
				if activeParent2.start.Name.Local == "table" {
					if err := tp.validateTable(activeParent2); err != nil {
						return err
					}
				}
			} else if parse := templateTag.parse; parse != nil {
				value, err := parse(tp, activeParent2)
				if err != nil {
					return err
				}
				activeParent2.value = value
				activeParent2.hasHandler = value != nil
			}
		}
	}
	childStyle := tp.childStyle(activeParent2)
	activeParent2.textAttrs = childStyle
	n, n2 := -1, -1
	if activeParent2.hasHandler && activeParent2.start.Name.Local == "paragraph" {
		n, n2 = firstLastTextChild(activeParent2.childValues)
	}
	for i, childValue := range activeParent2.childValues {
		switch childValue2 := childValue.(type) {
		case *templateNode:
			if err := tp.walk(childValue2, childStyle, ok2); err != nil {
				return err
			}
		case *templateText:
			activeParent := activeParent2
			if !activeParent.hasHandler {
				activeParent = activeParent.activeParent()
			}
			if activeParent == nil {
				if !isBlank(childValue2.text) {
					tp.nodeLogDebug(childValue2.pos(), "Text content inside <%s> has no enclosing tag that takes it. Skipping.", activeParent2.start.Name.Local)
				}
				continue
			}
			text := childValue2.text
			if activeParent.start.Name.Local == "paragraph" {
				ok := activeParent == activeParent2
				text = collapseWhitespace(text, ok && (n < 0 || i <= n), ok && (n2 < 0 || i >= n2))
			}
			ok, err := tp.checkNodeText(activeParent, text, childValue2)
			if err != nil {
				return err
			}
			if ok && !ok2 {
				if err := tp.addNodeText(activeParent, text, childValue2); err != nil {
					return err
				}
			}
		}
	}
	if !ok2 && activeParent2.value != nil {
		if table, ok := activeParent2.value.(*Table); ok {
			tp.clampTableHeaderRows(activeParent2, table)
		}
		if err := tp.renderNode(activeParent2); err != nil {
			return err
		}
	}
	return nil
}

// SetSellerAddress sets the seller address of the invoice.
func (i *Invoice) SetSellerAddress(address *InvoiceAddress) { i.sellerAddress = address }
func (grc grayColor) ToRGB() (float64, float64, float64) {
	return grc.gray, grc.gray, grc.gray
}

// Add adds a new Drawable to the chapter.
// Currently supported Drawables:
// - *StyledParagraph
// - *Image
// - *Chart
// - *Table
// - *Division
// - *List
// - *Rectangle
// - *Ellipse
// - *Line
// - *Block,
// - *PageBreak
// - *Chapter
func (ch *Chapter) Add(d Drawable) error {
	if Drawable(ch) == d {
		common.Log.Debug("ERROR: Cannot add itself")
		return errors.New("range check error")
	}
	switch d2 := d.(type) {
	case *StyledParagraph, *Image, *Chart, *Table, *Division, *List, *Rectangle, *Ellipse, *Line, *Block, *PageBreak, *Chapter:
		ch.contents = append(ch.contents, d)
	case containerDrawable:
		containerComponent, err := d2.ContainerComponent(ch)
		if err != nil {
			return err
		}
		ch.contents = append(ch.contents, containerComponent)
	default:
		common.Log.Debug("Unsupported: %T", d)
		return errors.New("type check error")
	}
	return nil
}
func (tp *templateProcessor) clampTableHeaderRows(node *templateNode, table *Table) {
	if !table.hasHeaderRows || table.headerEndRow <= table.rows {
		return
	}
	tp.nodeLogDebug(node, "Table header rows %d-%d exceed the table's %d rows.", table.headerStartRow, table.headerEndRow, table.rows)
	if table.headerStartRow > table.rows {
		table.hasHeaderRows = false
		table.headerStartRow, table.headerEndRow = 0, 0
	} else {
		table.headerEndRow = table.rows
	}
}
func (tp *templateProcessor) effectiveAttrs(node *templateNode, m map[string]attrApplyFunc) []xml.Attr {
	var attrs []xml.Attr
	if _, ok := styledTextTags[node.start.Name.Local]; ok {
		for attrKey, attr := range node.attrs {
			if _, ok := m[attrKey]; ok {
				attrs = append(attrs, xml.Attr{Name: xml.Name{Local: attrKey}, Value: attr})
			}
		}
	}
	for _, s := range node.classNames() {
		attrs = append(attrs, tp.classStyles[s]...)
	}
	for _, attr := range node.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == classAttrName {
			continue
		}
		attrs = append(attrs, attr)
	}
	return attrs
}

// ToContentCreator convert SVG and add elements contentstream then returns `contentstream.ContentCreator`.
func (gse *GraphicSVGElement) ToContentCreator(cc *contentstream.ContentCreator, res *model.PdfPageResources, scaleX, scaleY, translateX, translateY float64) *contentstream.ContentCreator {
	if gse.Name == "svg" {
		gse.SetScaling(scaleX, scaleY)
		cc.Add_cm(1, 0, 0, 1, translateX, translateY)
		gse.setDefaultScaling(gse.scale)
		cc.Add_W()
		cc.Add_n()
		gse.processDefs()
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.ViewBox = gse.ViewBox
			graphicSVGElement.linearShadings = gse.linearShadings
			graphicSVGElement.radialShadings = gse.radialShadings
			graphicSVGElement.toContentStream(cc, res)
		}
		return cc
	}
	return nil
}

// SetColorKeyMask sets a color key mask (/Mask range array) for the image:
// pixels whose component values all fall inside the given ranges are not
// painted (PDF 32000-1:2008 8.9.6.4). The ranges are given as [min1 max1 ...
// minN maxN] pairs in the image's color component order, e.g. 6 values for an
// RGB image, and are validated against the image's final colorspace and bit
// depth when the image is drawn.
//
// Color key masking compares raw integer sample values, before any Decode
// array mapping (PDF 32000-1:2008 8.9.6.4), so the ranges are given in raw
// sample units [0, 2^BitsPerComponent-1]. Exact matching makes it unreliable
// for lossily (DCT/JPEG) encoded pixels, where compression noise shifts
// sample values; when no encoder is set with SetEncoder, a color-key-masked
// image is therefore flate (losslessly) encoded, except for pass-through
// JPEG images, whose original samples are embedded unchanged.
//
// The mask replaces any SMask derived automatically from the base image's
// alpha channel. An image can carry a single mask: setting a mask of a
// different type than a previously set one returns an error.
func (im *Image) SetColorKeyMask(ranges []int) error {
	if len(ranges) == 0 || len(ranges)%2 != 0 {
		return fmt.Errorf("color key mask ranges must be non-empty [min max] pairs, got %d values", len(ranges))
	}
	for i := 0; i < len(ranges); i += 2 {
		if ranges[i] < 0 || ranges[i+1] < 0 {
			return fmt.Errorf("color key mask values must not be negative, got [%d %d]", ranges[i], ranges[i+1])
		}
		if ranges[i] > ranges[i+1] {
			return fmt.Errorf("color key mask range min %d exceeds max %d", ranges[i], ranges[i+1])
		}
	}
	nums := make([]int, len(ranges))
	copy(nums, ranges)
	return im.setMask(&imageMask{kind: imageMaskColorKey, colorKey: nums})
}

// MultiRowCell makes a new cell with the specified row span and inserts it
// into the table at the current position.
func (t *Table) MultiRowCell(rowspan int) *TableCell { return t.MultiCell(rowspan, 1) }

// SetBorderOpacity sets the border opacity.
func (p *Polygon) SetBorderOpacity(opacity float64) { p.borderOpacity = opacity }
func parseBorderRadius(s string) (topLeft, topRight, bottomRight, bottomLeft float64, err error) {
	floatFields, err := parseFloatFields(s)
	switch len(floatFields) {
	case 0:
	case 1:
		topLeft, topRight, bottomRight, bottomLeft = floatFields[0], floatFields[0], floatFields[0], floatFields[0]
	case 2:
		topLeft, bottomRight = floatFields[0], floatFields[0]
		topRight, bottomLeft = floatFields[1], floatFields[1]
	case 3:
		topLeft = floatFields[0]
		topRight, bottomLeft = floatFields[1], floatFields[1]
		bottomRight = floatFields[2]
	case 4:
		topLeft, topRight, bottomRight, bottomLeft = floatFields[0], floatFields[1], floatFields[2], floatFields[3]
	default:
		if err == nil {
			err = fmt.Errorf("border-radius expects 1-4 values, got %d", len(floatFields))
		}
	}
	return topLeft, topRight, bottomRight, bottomLeft, err
}

// SetHeading sets the text and the style of the heading of the TOC component.
func (to *TOC) SetHeading(text string, style TextStyle) {
	heading := to.Heading()
	heading.Reset()
	chunk := heading.Append(text)
	chunk.Style = style
}
func hasEmojiSequenceMarker(s string) bool {
	for _, r := range s {
		switch {
		case r == 0x200D:
			return true
		case r >= 0xFE00 && r <= 0xFE0F:
			return true
		case r >= 0xE0100 && r <= 0xE01EF:
			return true
		case r >= 0x1F3FB && r <= 0x1F3FF:
			return true
		case r >= 0x1F1E6 && r <= 0x1F1FF:
			return true
		case r == 0x20E3:
			return true
		case r >= 0xE0020 && r <= 0xE007F:
			return true
		}
	}
	return false
}

// SetDashPattern sets the dash pattern of the line.
// NOTE: the dash pattern is taken into account only if the style of the
// line is set to dashed.
func (l *Line) SetDashPattern(dashArray []int64, dashPhase int64) {
	l.dashArray = dashArray
	l.dashPhase = dashPhase
}
func (r *Rectangle) applyFitMode(borderWidth float64) {
	borderWidth -= r.margins.Left + r.margins.Right + r.borderWidth
	switch r.fitMode {
	case FitModeFillWidth:
		r.ScaleToWidth(borderWidth)
	}
}

// TOC represents a table of contents component.
// It consists of a paragraph heading and a collection of
// table of contents lines.
// The representation of a table of contents line is as follows:
//
//	[number] [title]      [separator] [page]
//
// e.g.: Chapter1 Introduction ........... 1
type TOC struct {
	taggedDrawable
	heading            *StyledParagraph
	lines              []*TOCLine
	lineNumberStyle    TextStyle
	lineTitleStyle     TextStyle
	lineSeparatorStyle TextStyle
	linePageStyle      TextStyle
	lineSeparator      string
	lineLevelOffset    float64
	lineMargins        Margins
	positioning        Positioning
	defaultStyle       TextStyle
	showLinks          bool
}
type templateTag struct {
	allowedParents        map[string]struct{}
	parse                 func(*templateProcessor, *templateNode) (interface{}, error)
	attrs                 map[string]attrApplyFunc
	singleChild           bool
	allowMultipleChildren bool
}

// FitMode returns the fit mode of the line.
func (l *Line) FitMode() FitMode { return l.fitMode }

// SetStyleRight sets border style for right side.
func (bo *border) SetStyleRight(style CellBorderStyle) { bo.styleRight = style }

// GraphicSVGStyle represents style attributes for `GraphicSVG`.
type GraphicSVGStyle struct {
	FillColor   string
	StrokeColor string
	StrokeWidth float64
	FillOpacity float64
}

// NewRow makes a new row and inserts it into the table at the current position.
func (g *Grid) NewRow() *GridRow {
	row := &GridRow{height: g.defaultRowHeight, index: len(g.rows), grid: g}
	g.rows = append(g.rows, row)
	return row
}
func (gse *GraphicSVGElement) setDefaultScaling(scale float64) {
	gse.scale = scale
	if gse.Style != nil && gse.Style.StrokeWidth > 0 {
		gse.Style.StrokeWidth = gse.Style.StrokeWidth * gse.scale
	}
	for _, graphicSVGElement := range gse.Children {
		graphicSVGElement.setDefaultScaling(scale)
	}
}
func makeGrayMaskXObject(img3 *model.Image) (*model.XObjectImage, error) {
	img2 := *img3
	img2.SetAlpha(nil)
	img := &img2
	if img2.ColorComponents != 1 {
		goImage, err := img2.ToGoImage()
		if err != nil {
			return nil, err
		}
		img, err = model.ImageHandling.NewGrayImageFromGoImage(goImage)
		if err != nil {
			return nil, err
		}
	}
	xObjectImage, err := model.NewXObjectImageFromImage(img, model.NewPdfColorspaceDeviceGray(), core.NewFlateEncoder())
	if err != nil {
		return nil, err
	}
	if vals := img.Decode(); len(vals) == 2 {
		xObjectImage.Decode = core.MakeArrayFromFloats(vals)
	}
	return xObjectImage, nil
}

// Cols returns the total number of columns the table has.
func (t *Table) Cols() int { return t.cols }
func parseListMarkerTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseListMarker(node)
}

// TextStyle is a collection of properties that can be assigned to a text chunk.
type TextStyle struct {

	// Color represents the color of the text.
	Color Color

	// OutlineColor represents the color of the text outline.
	OutlineColor Color

	// MultiFont represents an encoder that accepts multiple fonts and selects the correct font for encoding.
	MultiFont *model.MultipleFontEncoder

	// Font represents the font the text will use.
	Font *model.PdfFont

	// FontSize represents the size of the font.
	FontSize float64

	// LineHeight is the line height multiplier applied to FontSize when this
	// chunk contributes to a line's vertical advance. A value of 0 (default)
	// means the chunk inherits the line-height setting of its containing
	// paragraph (default 1.0).
	LineHeight float64

	// OutlineSize represents the thickness of the text outline.
	OutlineSize float64

	// CharSpacing represents the character spacing.
	CharSpacing float64

	// HorizontalScaling represents the percentage to horizontally scale
	// characters by (default: 100). Values less than 100 will result in
	// narrower text while values greater than 100 will result in wider text.
	HorizontalScaling float64

	// RenderingMode represents the rendering mode.
	RenderingMode TextRenderingMode

	// Underline specifies if the text chunk is underlined.
	Underline bool

	// UnderlineStyle represents the style of the line used to underline text.
	UnderlineStyle TextDecorationLineStyle

	// TextRise specifies a vertical adjustment for text. It is useful for
	// drawing subscripts/superscripts. A positive text rise value will
	// produce superscript text, while a negative one will result in
	// subscript text.
	TextRise float64
}

// InfoLines returns all the rows in the invoice information table as
// description-value cell pairs.
func (i *Invoice) InfoLines() [][2]*InvoiceCell {
	rows := [][2]*InvoiceCell{i.number, i.date, i.dueDate}
	return append(rows, i.infoLines...)
}

// SetExtends specifies whether ot extend the shading beyond the starting and ending points.
//
// Text extends is set to `[]bool{false, false}` by default.
func (rs *RadialShading) SetExtends(start bool, end bool) { rs.base.SetExtends(start, end) }

// Notes returns the notes section of the invoice as a title-content pair.
func (i *Invoice) Notes() (string, string) { return i.notes[0], i.notes[1] }
func (tp *templateProcessor) resolveColor(s string) (Color, error) {
	hexStr := strings.TrimSpace(s)
	if hexStr == "" {
		return ColorBlack, nil
	}
	if isGradientExpr(hexStr) {
		if strings.HasPrefix(hexStr, "linear-gradient(") {
			return tp.parseLinearGradientAttr(tp.creator, hexStr)
		}
		return tp.parseRadialGradientAttr(tp.creator, hexStr)
	}
	if color, ok := tp.options.ColorMap[hexStr]; ok {
		return color, nil
	}
	if strings.HasPrefix(hexStr, "#") && isHexColor(hexStr) {
		return ColorRGBFromHex(hexStr), nil
	}
	return ColorBlack, fmt.Errorf("unresolved color %q", s)
}

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) SetMarkedContentID(id int64) {}

// SetMargins sets the Paragraph's margins.
func (sp *StyledParagraph) SetMargins(left, right, top, bottom float64) {
	sp.margins.Left = left
	sp.margins.Right = right
	sp.margins.Top = top
	sp.margins.Bottom = bottom
}

// List represents a list of items.
// The representation of a list item is as follows:
//
//	[marker] [content]
//
// e.g.:        • This is the content of the item.
// The supported components to add content to list items are:
// - StyledParagraph
// - List
type List struct {
	taggedDrawable
	items         []*listItem
	margins       Margins
	marker        TextChunk
	indent        float64
	defaultIndent bool
	positioning   Positioning
	textStyle     TextStyle
}

func cropImage(img *model.Image, rect2 image.Rectangle) (*model.Image, error) {
	rect := image.Rect(0, 0, int(img.Width), int(img.Height))
	rect3 := rect2.Intersect(rect)
	switch {
	case rect3.Empty():
		return model.ImageHandling.NewImageFromGoImage(&image.RGBA{})
	case img.BitsPerComponent == 8 && len(img.Decode()) == 0 && (img.ColorComponents == 1 || img.ColorComponents == 3) && len(img.Data) >= int(img.Width)*int(img.Height)*img.ColorComponents:
		return cropImageFast(img, rect2, rect3), nil
	default:
		return cropImageGeneric(img, rect2, rect3)
	}
}

// SetRowWrapDisabled excludes a row from row wrapping: when the table has row
// wrapping enabled (see EnableRowWrap), a disabled row moves to the next page
// whole instead of splitting at the page boundary. Splittable content
// (styled paragraphs, divisions, lists) taller than a full page is clipped;
// other content taller than a full page is not rendered. Applies to the cells
// currently in the row, so call it after the row's cells have been added.
func (t *Table) SetRowWrapDisabled(row int, disabled bool) error {
	if row < 1 || row > t.rows {
		return errors.New("range check error")
	}
	for _, cell := range t.cells {
		if cell.row == row {
			cell.wrapDisabled = disabled
		}
	}
	return nil
}

// BorderWidth returns the border width of the ellipse.
func (e *Ellipse) BorderWidth() float64 { return e.borderWidth }

// SetPos set position of the element on PDF page
func (gse *GraphicSVGElement) SetPos(x, y float64) { gse.xPos = x; gse.yPos = y }

// DrawWithContext draws the Block using the specified drawing context.
func (b *Block) DrawWithContext(d Drawable, ctx DrawContext) error {
	pageBlocks, _, err := d.GeneratePageBlocks(ctx)
	if err != nil {
		return err
	}
	if len(pageBlocks) != 1 {
		return ErrContentNotFit
	}
	for _, pageBlock := range pageBlocks {
		if err := b.mergeBlocks(pageBlock); err != nil {
			return err
		}
	}
	return nil
}
func (t *Table) splitSpanningCellsAtBreaks(ctx DrawContext, fn func(VectorDrawable, float64, float64, float64) (VectorDrawable, VectorDrawable)) {
	contentHeight := ctx.contentHeight()
	headerHeight := t.headerHeight()
	headerEndRow := 1
	if t.hasHeaderRows {
		headerEndRow = t.headerEndRow + 1
	}
	height := ctx.Height - headerHeight
	contentHeight2 := contentHeight - headerHeight
	if height <= 0 {
		height = contentHeight2
	}
	if contentHeight2 <= 0 {
		return
	}
	value := height
	value2 := 0.0
	m := make(map[int]bool)
	for i := headerEndRow; i <= t.rows && i-1 < len(t.rowHeights); i++ {
		rowHeight := t.rowHeights[i-1]
		if value2 > 0 && value2+rowHeight > value {
			m[i] = true
			value = contentHeight2
			value2 = 0
		}
		value2 += rowHeight
	}
	if len(m) == 0 {
		return
	}
	for i := 0; i < len(t.cells); i++ {
		cell := t.cells[i]
		if cell.rowSpan < 2 || cell.row < headerEndRow {
			continue
		}
		row := cell.row
		n := cell.row + cell.rowSpan - 1
		row2 := -1
		for i := row + 1; i <= n; i++ {
			if m[i] {
				row2 = i
				break
			}
		}
		if row2 < 0 {
			continue
		}
		rowHeight := 0.0
		for i := row; i < row2 && i-1 < len(t.rowHeights); i++ {
			rowHeight += t.rowHeights[i-1]
		}
		width := cell.width(t.colWidths, ctx.Width)
		var content VectorDrawable
		if content2, content3 := fn(cell.content, width, cell.indent, rowHeight); content2 == nil {
			content = cell.content
			cell.content = nil
		} else if content3 != nil {
			cell.content = content2
			content = content3
		}
		props := cell.cloneProps(content)
		props.row = row2
		props.col = cell.col
		props.rowSpan = n - row2 + 1
		cell.rowSpan = row2 - row
		t.cells = append(t.cells, props)
	}
}

const (
	CellHorizontalAlignmentLeft CellHorizontalAlignment = iota
	CellHorizontalAlignmentCenter
	CellHorizontalAlignmentRight
)

func beginTaggedContent(_bedefcd *Block, structureTagInfo *model.StructureTagInfo) {
	contentCreator := contentstream.NewContentCreator()
	m := map[string]core.PdfObject{}
	if structureTagInfo != nil {
		m["MCID"] = core.MakeInteger(structureTagInfo.Mcid)
	}
	contentCreator.Add_BDC(*core.MakeName(string(structureTagInfo.StructureType)), m)
	_bedefcd.addContents(contentCreator.Operations())
}
func parseListItemTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseListItem(node)
}
func splitDrawableToFit(drawable VectorDrawable, ctx DrawContext) VectorDrawable {
	switch para := drawable.(type) {
	case *StyledParagraph:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Top - para.margins.Bottom - 0.5*para.getTextHeight())
		if ctx2.Height <= 0 {
			return nil
		}
		head, _, err := para.split(ctx2)
		if err != nil || head == nil {
			return nil
		}
		return head
	case *Division:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Top - para.margins.Bottom)
		if ctx2.Height <= 0 {
			return nil
		}
		head, _ := para.split(ctx2, 0)
		if head == nil {
			return nil
		}
		return head
	case *List:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Vertical())
		if ctx2.Height <= 0 {
			return nil
		}
		head, _ := para.split(ctx2, 0)
		if head == nil {
			return nil
		}
		return head
	case *Table:
		head, _ := para.split(ctx, 0)
		if head == nil {
			return nil
		}
		return head
	}
	return nil
}

// Height returns the height of the graphic svg.
func (gs *GraphicSVG) Height() float64 { return gs.root.Height }

// SetBackground sets the background properties of the component.
func (d *Division) SetBackground(background *Background) { d.background = background }

// Vertical returns total vertical (top + bottom) margin.
func (m *Margins) Vertical() float64 { return m.Bottom + m.Top }

// SetOpacity sets opacity for Image.
func (im *Image) SetOpacity(opacity float64) { im.opacity = opacity }
func templateExtendDict(templateExtendDict map[string]interface{}, vals ...interface{}) (map[string]interface{}, error) {
	n := len(vals)
	if n%2 != 0 {
		return nil, core.ErrRangeError
	}
	for i := 0; i < n; i += 2 {
		s, ok := vals[i].(string)
		if !ok {
			return nil, core.ErrTypeError
		}
		templateExtendDict[s] = vals[i+1]
	}
	return templateExtendDict, nil
}

const (
	PositionRelative Positioning = iota
	PositionAbsolute
)

func (gse *GraphicSVGElement) drawLine(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	svgLength, err := parseSVGLength(gse.Attributes["x1"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cx` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["y1"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cy` value: %v", err.Error())
	}
	svgLength3, err := parseSVGLength(gse.Attributes["x2"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `rx` value: %v", err.Error())
	}
	svgLength4, err := parseSVGLength(gse.Attributes["y2"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `ry` value: %v", err.Error())
	}
	cc.Add_m(svgLength*gse.scale, svgLength2*gse.scale)
	cc.Add_l(svgLength3*gse.scale, svgLength4*gse.scale)
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

// GeneratePageBlocks draw graphic svg into block.
func (gs *GraphicSVG) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	isRelative := gs.positioning.IsRelative()
	var blocks []*Block
	if isRelative {
		value := 1.0
		top := gs.margins.Top
		if gs.root.Height > ctx.Height-gs.margins.Top {
			blocks = []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y)}
			var err error
			if _, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			top = 0
		}
		ctx.X += gs.margins.Left + value
		ctx.Y += top
		ctx.Width -= gs.margins.Left + gs.margins.Right + 2*value
		ctx.Height -= top
	} else {
		ctx.X = gs.xPos
		ctx.Y = gs.yPos
	}
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Translate(0, ctx.PageHeight)
	contentCreator.Scale(1, -1)
	contentCreator.Translate(ctx.X, ctx.Y)
	width := gs.root.Width / gs.root.ViewBox.W
	height := gs.root.Height / gs.root.ViewBox.H
	xPos := 0.0
	yPos := 0.0
	if isRelative {
		xPos = gs.xPos - (gs.root.ViewBox.X * math.Max(width, height))
		yPos = gs.yPos - (gs.root.ViewBox.Y * math.Max(width, height))
	}
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if gs.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(gs.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(gs.tagInfo.Mcid)})
	}
	gs.root.SetPos(ctx.X, ctx.Y)
	gs.root.ToContentCreator(contentCreator, block.resources, width, height, xPos, yPos)
	if gs.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	operations := contentCreator.Operations()
	operations.WrapIfNeeded()
	block.addWrappedContents(operations)
	if isRelative {
		bottom := gs.Height() + gs.margins.Bottom
		ctx.Y += bottom
		ctx.Height -= bottom
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// AddressStyle returns the style properties used to render the content of
// the invoice address sections.
func (i *Invoice) AddressStyle() TextStyle { return i.addressStyle }
func copyHighlightAnnotation(annotationHighlight *model.PdfAnnotationHighlight) *model.PdfAnnotationHighlight {
	if annotationHighlight == nil {
		return nil
	}
	pdfAnnotationHighlight := model.NewPdfAnnotationHighlight()
	pdfAnnotationHighlight.CopyFrom(annotationHighlight.PdfAnnotation)
	pdfAnnotationHighlight.CA = annotationHighlight.CA
	pdfAnnotationHighlight.QuadPoints = annotationHighlight.QuadPoints
	return pdfAnnotationHighlight
}
func (gc *GridCell) width(colWidths []float64, totalWidth float64) float64 {
	width := float64(0.0)
	for i := 0; i < gc.colSpan; i++ {
		width += colWidths[gc.col+i]
	}
	return width * totalWidth
}

// CellBorderSide defines the table cell's border side.
type CellBorderSide int

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (i *Invoice) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	fns := []func(ctx DrawContext) ([]*Block, DrawContext, error){i.generateHeaderBlocks, i.generateInformationBlocks, i.generateLineBlocks, i.generateTotalBlocks, i.generateNoteBlocks}
	var blocks []*Block
	for _, fn := range fns {
		fn2, ctx2, err := fn(ctx)
		if err != nil {
			return blocks, ctx, err
		}
		if len(blocks) == 0 {
			blocks = fn2
		} else if len(fn2) > 0 {
			blocks[len(blocks)-1].mergeBlocks(fn2[0])
			blocks = append(blocks, fn2[1:]...)
		}
		ctx = ctx2
	}
	if i.positioning.IsRelative() {
		ctx.X = ctx2.X
	}
	if i.positioning.IsAbsolute() {
		return blocks, ctx2, nil
	}
	return blocks, ctx, nil
}

// GetContent returns the cell's content. Returns nil if no content has been
// set.
func (tc *TableCell) GetContent() VectorDrawable { return tc.content }
func (c *Creator) newPage() *model.PdfPage {
	pdfPage := model.NewPdfPage()
	urx := c.pageSize[0]
	ury := c.pageSize[1]
	rect := model.PdfRectangle{Llx: 0, Lly: 0, Urx: urx, Ury: ury}
	pdfPage.MediaBox = &rect
	c.pageWidth = urx
	c.pageHeight = ury
	c.initContext()
	return pdfPage
}

// SetSideBorderColor sets the cell's side border color.
func (tc *TableCell) SetSideBorderColor(side CellBorderSide, col Color) {
	switch side {
	case CellBorderSideAll:
		tc.borderColorTop = col
		tc.borderColorBottom = col
		tc.borderColorLeft = col
		tc.borderColorRight = col
	case CellBorderSideTop:
		tc.borderColorTop = col
	case CellBorderSideBottom:
		tc.borderColorBottom = col
	case CellBorderSideLeft:
		tc.borderColorLeft = col
	case CellBorderSideRight:
		tc.borderColorRight = col
	}
}

// SetOpacity sets the cell's opacity in the range 0-1.
func (gc *GridCell) SetOpacity(opacity float64) { gc.opacity = opacity }

// SetViewerPreferences sets the viewer preferences for the PDF document.
func (c *Creator) SetViewerPreferences(viewerPreferences *model.ViewerPreferences) {
	c.viewerPreferences = viewerPreferences
}
func addShadingPatternResource(block *Block, pattern core.PdfObject) (core.PdfObjectName, error) {
	i := 1
	keyName := core.PdfObjectName("P" + strconv.Itoa(i))
	for block.resources.HasPatternByName(keyName) {
		i++
		keyName = core.PdfObjectName("P" + strconv.Itoa(i))
	}
	if err := block.resources.SetPatternByName(keyName, pattern); err != nil {
		return "", err
	}
	return keyName, nil
}
func (tp *templateProcessor) parseStyledParagraph(node *templateNode) (interface{}, error) {
	styledParagraph := tp.creator.NewStyledParagraph()
	if err := tp.applyAttrs(node, styledParagraph, styledParagraphAttrs); err != nil {
		return nil, err
	}
	return styledParagraph, nil
}

// GetPosition returns the 1-based row and column the cell starts from.
// Consistent with GridCell.GetPosition.
func (tc *TableCell) GetPosition() (row, col int) { return tc.row, tc.col }

// MoveTo moves the drawing context to absolute coordinates (x, y).
func (c *Creator) MoveTo(x, y float64) { c.context.X = x; c.context.Y = y }
func newGraphicSVG(root *GraphicSVGElement) (*GraphicSVG, error) {
	return &GraphicSVG{root: root, positioning: PositionRelative, margins: Margins{Top: 10, Bottom: 10}, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}, nil
}
func (rc rgbColor) ToRGB() (float64, float64, float64) {
	return rc.r, rc.g, rc.b
}

// Table allows organizing content in an rows X columns matrix, which can spawn across multiple pages.
type Table struct {
	taggedDrawable
	rows             int
	cols             int
	curCell          int
	colWidths        []float64
	rowHeights       []float64
	defaultRowHeight float64
	cells            []*TableCell
	rowSpanRemaining []int
	positioning      Positioning
	xPos, yPos       float64
	margins          Margins
	hasHeaderRows    bool
	headerStartRow   int
	headerEndRow     int
	enableRowWrap    bool
	enablePageWrap   bool
	compactMode      bool
}

var (
	lengthUnits  = []string{"cm", "mm", "px", "pt"}
	unitToPoints = map[string]float64{"cm": cmToPt, "mm": mmToPt, "px": pxToPt, "pt": 1}
)

// ScaleToWidth scales the ellipse to the specified width. The height of
// the ellipse is scaled so that the aspect ratio is maintained.
func (e *Ellipse) ScaleToWidth(w float64) {
	height := e.height / e.width
	e.width = w
	e.height = w * height
}

// SetWidth sets the the Paragraph width. This is essentially the wrapping width,
// i.e. the width the text can extend to prior to wrapping over to next line.
func (sp *StyledParagraph) SetWidth(width float64) { sp.width = width; sp.wrapText() }

// NoteHeadingStyle returns the style properties used to render the heading of
// the invoice note sections.
func (i *Invoice) NoteHeadingStyle() TextStyle { return i.noteHeadingStyle }
func newChapter(parent *Chapter, toc *TOC, outline *model.Outline, recordFunc chapterRecordFunc, title string, number int, style TextStyle) *Chapter {
	var level uint = 1
	if parent != nil {
		level = parent.level + 1
	}
	chapter := &Chapter{number: number, title: title, showNumbering: true, includeInTOC: true, parent: parent, toc: toc, outline: outline, contents: []Drawable{}, level: level, recordFunc: recordFunc}
	styledParagraph := newStyledParagraph(style)
	text := styledParagraph.SetText(chapter.headingText())
	text.Style = style
	chapter.heading = styledParagraph
	return chapter
}

// Reset removes all the text chunks the paragraph contains.
func (sp *StyledParagraph) Reset() { sp.chunks = []*TextChunk{} }

// ScaleToWidth sets the graphic svg scaling factor with the given width.
func (gs *GraphicSVG) ScaleToWidth(w float64) {
	height := gs.root.Height / gs.root.Width
	gs.root.Width = w
	gs.root.Height = w * height
	gs.root.SetScaling(height, height)
}
func cropImageGeneric(img *model.Image, rect, rect2 image.Rectangle) (*model.Image, error) {
	goImage, err := img.ToGoImage()
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	imagedraw.Draw(rgba, image.Rect(0, 0, rect2.Dx(), rect2.Dy()), goImage, rect2.Min, imagedraw.Src)
	image, err := model.ImageHandling.NewImageFromGoImage(rgba)
	if err != nil {
		return nil, err
	}
	if alpha := cropAlphaData(img, rect, rect2); alpha != nil {
		image.SetAlpha(alpha)
	}
	return image, nil
}

var svgCommands commands

// AddTag adds the table tag for Grid to the root K object.
func (g *Grid) AddTag(rootKObj *model.KDict) {
	if rootKObj == nil {
		common.Log.Debug("Table: AddTag called with nil rootKObj, skipping tagging.")
		return
	}
	if g.tagInfo == nil {
		g.tagInfo = model.NewStructureTagInfo()
		g.tagInfo.StructureType = model.StructureTypeTable
	}
	g.tagInfo.ApplyTag = true
	g.tagInfo.ParentKObj = rootKObj
	g.tagInfo.ComponentKObj = g.tagInfo.NewComponentKObj(g.tagInfo.StructureType)
	g.tagInfo.ParentKObj.AddKChild(g.tagInfo.ComponentKObj)
}

// GetTable returns the table the cell belongs to.
func (tc *TableCell) GetTable() *Table { return tc.table }

// Width returns the Block's width.
func (b *Block) Width() float64 { return b.width }
func (c *Creator) wrapPageIfNeeded(pageIfNeeded *model.PdfPage) (*model.PdfPage, error) {
	allContentStreams, err := pageIfNeeded.GetAllContentStreams()
	if err != nil {
		return nil, err
	}
	contentStreamParser := contentstream.NewContentStreamParser(allContentStreams)
	contentStreamOperations, err := contentStreamParser.Parse()
	if err != nil {
		return nil, err
	}
	if !contentStreamOperations.HasUnclosedQ() {
		return nil, nil
	}
	contentStreamOperations.WrapIfNeeded()
	stream, err := core.MakeStream(contentStreamOperations.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return nil, err
	}
	pageIfNeeded.Contents = core.MakeArray(stream)
	return pageIfNeeded, nil
}

// PolyBezierCurve represents a composite curve that is the result of joining
// multiple cubic Bezier curves.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type PolyBezierCurve struct {
	taggedDrawable
	curve         *contentdraw.PolyBezierCurve
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

func transformRectangle(rect *model.PdfRectangle, matrix transform.Matrix) *model.PdfRectangle {
	var transformRectangle model.PdfRectangle
	transformRectangle.Llx, transformRectangle.Lly = matrix.Transform(rect.Llx, rect.Lly)
	transformRectangle.Urx, transformRectangle.Ury = matrix.Transform(rect.Urx, rect.Ury)
	transformRectangle.Normalize()
	return &transformRectangle
}

// MarkAsArtifact marks the paragraph as an artifact of the specified type.
//
// This would override regular structure tagging and accessibility
// properties, and the paragraph will be treated as an artifact in the PDF.
func (sp *StyledParagraph) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	sp.artifact = &model.Artifact{Type: artifactType}
	return sp.artifact
}
func (t *Table) insertRows(rowHeightsLen, row int) {
	if row <= 0 {
		return
	}
	for _, cell := range t.cells {
		switch {
		case cell.row > rowHeightsLen:
			cell.row += row
		case cell.row+cell.rowSpan-1 > rowHeightsLen:
			cell.rowSpan += row
		}
	}
	if t.hasHeaderRows {
		if rowHeightsLen < t.headerStartRow {
			t.headerStartRow += row
		}
		if rowHeightsLen < t.headerEndRow {
			t.headerEndRow += row
		}
	}
	if rowHeightsLen > len(t.rowHeights) {
		rowHeightsLen = len(t.rowHeights)
	}
	floats := make([]float64, 0, len(t.rowHeights)+row)
	floats = append(floats, t.rowHeights[:rowHeightsLen]...)
	floats = append(floats, make([]float64, row)...)
	floats = append(floats, t.rowHeights[rowHeightsLen:]...)
	t.rowHeights = floats
	t.rows += row
}

// SetColumns overwrites any columns in the line items table. This should be
// called before AddLine.
func (i *Invoice) SetColumns(cols []*InvoiceCell) { i.columns = cols }

// MoveX moves the drawing context to absolute position x.
func (c *Creator) MoveX(x float64) { c.context.X = x }
func reverseFloats(reverseFloats []float64) []float64 {
	for i, j := 0, len(reverseFloats)-1; i < j; i, j = i+1, j-1 {
		reverseFloats[i], reverseFloats[j] = reverseFloats[j], reverseFloats[i]
	}
	return reverseFloats
}

// SetFillColor sets the fill color.
func (p *Polygon) SetFillColor(color Color) {
	p.fillColor = color
	p.polygon.FillColor = toPdfColor(color)
}
func newURILinkAnnotation(s, s2 string) *model.PdfAnnotation {
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	borderStyle := model.NewBorderStyle()
	borderStyle.SetBorderWidth(0)
	pdfAnnotationLink.BS = borderStyle.ToPdfObject()
	pdfActionURI := model.NewPdfActionURI()
	pdfActionURI.URI = core.MakeString(s)
	pdfAnnotationLink.SetAction(pdfActionURI.PdfAction)
	if s2 != "" {
		pdfAnnotationLink.Contents = core.MakeString(s2)
	}
	return pdfAnnotationLink.PdfAnnotation
}
func (t *Table) moveToNextAvailableCell() int {
	moveToNextAvailableCell := (t.curCell-1)%(t.cols) + 1
	for {
		if moveToNextAvailableCell-1 >= len(t.rowSpanRemaining) {
			if t.rowSpanRemaining[0] == 0 {
				return moveToNextAvailableCell
			}
			moveToNextAvailableCell = 1
		} else if t.rowSpanRemaining[moveToNextAvailableCell-1] == 0 {
			return moveToNextAvailableCell
		}
		t.curCell++
		t.rowSpanRemaining[moveToNextAvailableCell-1]--
		moveToNextAvailableCell++
	}
}

// Ellipse defines an ellipse with a center at (xc,yc) and a specified width and height.  The ellipse can have a colored
// fill and/or border with a specified width.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Ellipse struct {
	taggedDrawable
	xc            float64
	yc            float64
	width         float64
	height        float64
	positioning   Positioning
	fillColor     Color
	fillOpacity   float64
	borderColor   Color
	borderWidth   float64
	borderOpacity float64
	margins       Margins
	fitMode       FitMode
}

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) GenerateKDict() (*model.KDict, error) { return nil, nil }

// ScaleToWidth scales the Block to a specified width, maintaining the same aspect ratio.
func (b *Block) ScaleToWidth(w float64) { sx := w / b.width; b.Scale(sx, sx) }
func newPathCommands() commands {
	var m = map[string]int{"m": 2, "z": 0, "l": 2, "h": 1, "v": 1, "c": 6, "s": 4, "q": 4, "t": 2, "a": 7}
	var strs []string
	for key := range m {
		strs = append(strs, key)
	}
	return commands{strs, m, "m", "z"}
}
func splitGraphemeClusters(s string) []graphemeCluster {
	const maxTagSequence = 16
	rs2 := []rune(s)
	if len(rs2) == 0 {
		return nil
	}
	graphemeClusters := make([]graphemeCluster, 0, len(rs2))
	n := 0
	for n < len(rs2) {
		n2 := n
		rs := []rune{rs2[n]}
		n++
		if isRegionalIndicator(rs2[n2]) {
			if n < len(rs2) && isRegionalIndicator(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
			continue
		}
		if isKeycapBase(rs2[n2]) && n < len(rs2) && (isVariationSelector(rs2[n]) || isKeycapEnclosing(rs2[n])) {
			if isVariationSelector(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			if n < len(rs2) && isKeycapEnclosing(rs2[n]) {
				rs = append(rs, rs2[n])
				n++
			}
			graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
			continue
		}
		if isEmoji(rs2[n2]) {
			for n < len(rs2) {
				if isVariationSelector(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					continue
				}
				if isSkinToneModifier(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					continue
				}
				if isTagCharacter(rs2[n]) {
					n2 := 0
					for n < len(rs2) && isTagCharacter(rs2[n]) && n2 < maxTagSequence {
						rs = append(rs, rs2[n])
						n2++
						n++
						if rs[len(rs)-1] == 0xE007F {
							break
						}
					}
					continue
				}
				if isZeroWidthJoiner(rs2[n]) {
					rs = append(rs, rs2[n])
					n++
					if n < len(rs2) && isEmoji(rs2[n]) {
						rs = append(rs, rs2[n])
						n++
						if n < len(rs2) && isVariationSelector(rs2[n]) {
							rs = append(rs, rs2[n])
							n++
						}
					}
					continue
				}
				break
			}
		}
		graphemeClusters = append(graphemeClusters, graphemeCluster{runes: rs, text: string(rs)})
	}
	return graphemeClusters
}
func valueToString(i interface{}) (string, error) {
	if i == nil {
		return "", nil
	}
	valueOf := reflect.ValueOf(i)
	for (valueOf.Kind() == reflect.Pointer || valueOf.Kind() == reflect.Interface) && !valueOf.Type().Implements(stringerType) && !valueOf.Type().Implements(errorType) && !valueOf.IsNil() {
		valueOf = valueOf.Elem()
	}
	if !valueOf.IsValid() {
		return "", nil
	}
	if !valueOf.Type().Implements(stringerType) && !valueOf.Type().Implements(errorType) {
		switch valueOf.Kind() {
		case reflect.Chan, reflect.Func:
			return "", fmt.Errorf("can't print %s of type %s", valueOf.Kind(), valueOf.Type())
		}
	}
	return fmt.Sprint(valueOf.Interface()), nil
}
func parseListTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseList(node)
}
func (i *Invoice) drawInformation() *Table {
	table := newTable(2)
	rows := append([][2]*InvoiceCell{i.number, i.date, i.dueDate}, i.infoLines...)
	for _, row := range rows {
		invoiceCell, invoiceCell2 := row[0], row[1]
		if invoiceCell2.Value == "" {
			continue
		}
		cell := table.NewCell()
		cell.SetBackgroundColor(invoiceCell.BackgroundColor)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph := newStyledParagraph(invoiceCell.TextStyle)
		styledParagraph.Append(invoiceCell.Value)
		styledParagraph.SetMargins(0, 0, 2, 1)
		cell.SetContent(styledParagraph)
		cell = table.NewCell()
		cell.SetBackgroundColor(invoiceCell2.BackgroundColor)
		i.setCellBorder(cell, invoiceCell2)
		styledParagraph = newStyledParagraph(invoiceCell2.TextStyle)
		styledParagraph.Append(invoiceCell2.Value)
		styledParagraph.SetMargins(0, 0, 2, 1)
		cell.SetContent(styledParagraph)
	}
	return table
}

// AddInternalLinkWithTag adds a new internal link to the paragraph with proper tagging for accessibility.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y coordinates.
// Position 0, 0 is at the top left of the page.
// The zoom of the destination page is controlled with the zoom parameter (0 keeps current zoom).
// The options parameter contains accessibility properties like tooltip, altText, and mcid.
func (sp *StyledParagraph) AddInternalLinkWithTag(text string, page int64, x, y, zoom float64, options LinkTagOptions) (*TextChunk, *model.KDict) {
	internalLinkAnnotation := newInternalLinkAnnotation(page-1, x, y, zoom, options.Tooltip)
	accessibleLinkChunk, kDict, err := sp.createAccessibleLinkChunk(text, internalLinkAnnotation, options)
	if err != nil {
		common.Log.Error("Failed to add internal link with tag: %v", err)
		return nil, nil
	}
	return accessibleLinkChunk, kDict
}
func (com *commands) isCommand(s string) bool {
	for _, name := range com.names {
		if strings.ToLower(s) == name {
			return true
		}
	}
	return false
}

// ParseFromSVGFile creates a GraphicSVG instance from file.
func ParseFromSVGFile(path string) (*GraphicSVGElement, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	return ParseFromSVGStream(source)
}

// GeneratePageBlocks draws the rectangle on a new block representing the page. Implements the Drawable interface.
func (r *Rectangle) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks      []*Block
		block       = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx2        = ctx
		borderWidth = r.borderWidth / 2
	)
	isRelative := r.positioning.IsRelative()
	if isRelative {
		r.applyFitMode(ctx.Width)
		ctx.X += r.margins.Left + borderWidth
		ctx.Y += r.margins.Top + borderWidth
		ctx.Width -= r.margins.Left + r.margins.Right
		ctx.Height -= r.margins.Top + r.margins.Bottom
		if r.height > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + r.margins.Top + borderWidth
			ctx2.X = ctx.Margins.Left + r.margins.Left + borderWidth
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - r.margins.Top - r.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - r.margins.Left - r.margins.Right
			ctx = ctx2
		}
	} else {
		ctx.X = r.x
		ctx.Y = r.y
	}
	rect := contentdraw.Rectangle{X: ctx.X, Y: ctx.PageHeight - ctx.Y - r.height, Width: r.width, Height: r.height, BorderRadiusTopLeft: r.borderRadiusTopLeft, BorderRadiusTopRight: r.borderRadiusTopRight, BorderRadiusBottomLeft: r.borderRadiusBottomLeft, BorderRadiusBottomRight: r.borderRadiusBottomRight, Opacity: 1.0}
	if r.fillColor != nil {
		rect.FillEnabled = true
		pdfColor := toPdfColor(r.fillColor)
		err := applyShapeColor(block, pdfColor, r.fillColor, func() Rectangle {
			return Rectangle{x: rect.X, y: rect.Y, width: rect.Width, height: rect.Height}
		})
		if err != nil {
			return nil, ctx, err
		}
		rect.FillColor = pdfColor
	}
	if r.borderColor != nil && r.borderWidth > 0 {
		rect.BorderEnabled = true
		rect.BorderColor = toPdfColor(r.borderColor)
		rect.BorderWidth = r.borderWidth
	}
	gsName, err := block.setOpacity(r.fillOpacity, r.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	markedDraw, _, err := rect.MarkedDraw(gsName, r.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		height := r.height + borderWidth
		ctx.Y += height + r.margins.Bottom
		ctx.Height -= height
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// SetLineColor sets the line color.
func (po *Polyline) SetLineColor(color Color) { po.polyline.LineColor = toPdfColor(color) }

// GetMargins returns the margins of the TOC line: left, right, top, bottom.
func (tl *TOCLine) GetMargins() (float64, float64, float64, float64) {
	margins := &tl.paragraph.margins
	return tl.left, margins.Right, margins.Top, margins.Bottom
}

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) SetMarkedContentID(id int64) {}

// GetPos returns the absolute coordinates (x, y) of the grid's upper-left
// corner. The coordinates are only meaningful when the grid uses absolute
// positioning (see GetPositioning).
func (g *Grid) GetPos() (float64, float64) { return g.xPos, g.yPos }

// ColorRGBFrom8bit creates a Color from 8-bit (0-255) r,g,b values.
// Example:
//
//	red := ColorRGBFrom8Bit(255, 0, 0)
func ColorRGBFrom8bit(r, g, b byte) Color {
	return rgbColor{r: float64(r) / 255.0, g: float64(g) / 255.0, b: float64(b) / 255.0}
}
func (tp *templateProcessor) parseList(node *templateNode) (interface{}, error) {
	list := tp.creator.NewList()
	if err := tp.applyAttrs(node, list, listAttrs); err != nil {
		return nil, err
	}
	return list, nil
}

var gridSectionStructureTypes = map[GridRowSection]model.StructureType{GridRowSectionBody: model.StructureTypeTableBody, GridRowSectionHeader: model.StructureTypeTableHead, GridRowSectionFooter: model.StructureTypeTableFooter, GridRowSectionUnknown: model.StructureTypeUnknown}

// Width returns 0. A page break has no width; the method exists so PageBreak
// satisfies the VectorDrawable interface and can be added to a Division.
func (pb *PageBreak) Width() float64 { return 0 }
func (fc *FilledCurve) draw(block *Block, s string) ([]byte, *model.PdfRectangle, error) {
	cubicBezierPath := contentdraw.NewCubicBezierPath()
	for _, curve := range fc.curves {
		cubicBezierPath = cubicBezierPath.AppendCurve(curve)
	}
	creator := contentstream.NewContentCreator()
	if fc.tagInfo != nil {
		creator.Add_BDC(*core.MakeName(string(fc.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(fc.tagInfo.Mcid)})
	}
	creator.Add_q()
	if fc.FillEnabled && fc.fillColor != nil {
		pdfColor := toPdfColor(fc.fillColor)
		err := applyShapeColor(block, pdfColor, fc.fillColor, func() Rectangle {
			cubicBezierPath := contentdraw.NewCubicBezierPath()
			for _, curve := range fc.curves {
				cubicBezierPath = cubicBezierPath.AppendCurve(curve)
			}
			boundingBox := cubicBezierPath.GetBoundingBox()
			if fc.BorderEnabled {
				boundingBox.Height += fc.BorderWidth
				boundingBox.Width += fc.BorderWidth
				boundingBox.X -= fc.BorderWidth / 2
				boundingBox.Y -= fc.BorderWidth / 2
			}
			return Rectangle{x: boundingBox.X, y: boundingBox.Y, width: boundingBox.Width, height: boundingBox.Height}
		})
		if err != nil {
			return nil, nil, err
		}
		creator.SetNonStrokingColor(pdfColor)
	}
	if fc.BorderEnabled {
		if fc.borderColor != nil {
			creator.SetStrokingColor(toPdfColor(fc.borderColor))
		}
		creator.Add_w(fc.BorderWidth)
	}
	if len(s) > 1 {
		creator.Add_gs(core.PdfObjectName(s))
	}
	contentdraw.DrawBezierPathWithCreator(cubicBezierPath, creator)
	creator.Add_h()
	if fc.FillEnabled && fc.BorderEnabled {
		creator.Add_B()
	} else if fc.FillEnabled {
		creator.Add_f()
	} else if fc.BorderEnabled {
		creator.Add_S()
	}
	creator.Add_Q()
	if fc.tagInfo != nil {
		creator.Add_EMC()
	}
	boundingBox := cubicBezierPath.GetBoundingBox()
	if fc.BorderEnabled {
		boundingBox.Height += fc.BorderWidth
		boundingBox.Width += fc.BorderWidth
		boundingBox.X -= fc.BorderWidth / 2
		boundingBox.Y -= fc.BorderWidth / 2
	}
	rect := &model.PdfRectangle{}
	rect.Llx = boundingBox.X
	rect.Lly = boundingBox.Y
	rect.Urx = boundingBox.X + boundingBox.Width
	rect.Ury = boundingBox.Y + boundingBox.Height
	return creator.Bytes(), rect, nil
}
func parseRectangleTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseRectangle(node)
}

// SetPositioning sets the positioning of the ellipse (absolute or relative).
func (e *Ellipse) SetPositioning(position Positioning) { e.positioning = position }
func (tp *templateProcessor) parseEllipse(node *templateNode) (interface{}, error) {
	ellipse := tp.creator.NewEllipse(0, 0, 0, 0)
	if err := tp.applyAttrs(node, ellipse, ellipseAttrs); err != nil {
		return nil, err
	}
	return ellipse, nil
}

// SetPos sets the position of the graphic svg to the specified coordinates.
// This method sets the graphic svg to use absolute positioning.
func (gs *GraphicSVG) SetPos(x, y float64) {
	gs.positioning = PositionAbsolute
	gs.xPos = x
	gs.yPos = y
}

// BorderColor returns the border color of the rectangle.
func (r *Rectangle) BorderColor() Color { return r.borderColor }

// SetBorderColor sets the cell's border color.
func (gc *GridCell) SetBorderColor(col Color) {
	gc.borderColorLeft = col
	gc.borderColorBottom = col
	gc.borderColorRight = col
	gc.borderColorTop = col
}

// Width returns the width of the line.
// NOTE: Depending on the fit mode the line is set to use, its width may be
// calculated at runtime (e.g. when using FitModeFillWidth).
func (l *Line) Width() float64 { return math.Abs(l.x2 - l.x1) }

// SetOutlineTree adds the specified outline tree to the PDF file generated
// by the creator. Adding an external outline tree disables the automatic
// generation of outlines done by the creator for the relevant components.
func (c *Creator) SetOutlineTree(outlineTree *model.PdfOutlineTreeNode) {
	c.outlineTree = outlineTree
}

// Wrap wraps the text of the chunk into lines based on its style and the
// specified width.
func (tec *TextChunk) Wrap(width float64) ([]string, error) {
	if int(width) <= 0 {
		return []string{tec.Text}, nil
	}
	var strs []string
	var rs []rune
	var value float64
	var floats []float64
	style := tec.Style
	isTextWriteDirectionLTR := core.IsTextWriteDirectionLTR(tec.Text)
	for _, r := range tec.Text {
		if r == '\u000A' {
			formatWriteDirectionLTR := core.FormatWriteDirectionLTR(string(rs), isTextWriteDirectionLTR)
			strs = append(strs, strings.TrimRightFunc(formatWriteDirectionLTR, unicode.IsSpace)+string(r))
			rs = nil
			value = 0
			floats = nil
			continue
		}
		ok2 := r == ' '
		runeMetrics, ok := style.Font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("ERROR: Rune char metrics not found! rune=0x%04x=%c font=%s %#q", r, r, style.Font.BaseFont(), style.Font.Subtype())
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Font: %#v", style.Font)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Encoder: %#v", style.Font.Encoder())
			}
			return nil, errors.New("glyph char metrics missing")
		}
		fontSize := style.FontSize * runeMetrics.Wx
		fontSize2 := fontSize
		if !ok2 {
			fontSize2 = fontSize + style.CharSpacing*1000.0
		}
		if value+fontSize > width*1000.0 {
			n := -1
			if !ok2 {
				for i := len(rs) - 1; i >= 0; i-- {
					if rs[i] == ' ' {
						n = i
						break
					}
				}
			}
			formatWriteDirectionLTR := string(rs)
			if n > 0 {
				formatWriteDirectionLTR = string(rs[0 : n+1])
				rs = append(rs[n+1:], r)
				floats = append(floats[n+1:], fontSize2)
				value = 0
				for _, float := range floats {
					value += float
				}
			} else {
				if ok2 {
					rs = []rune{}
					floats = []float64{}
					value = 0
				} else {
					rs = []rune{r}
					floats = []float64{fontSize2}
					value = fontSize2
				}
			}
			formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
			strs = append(strs, strings.TrimRightFunc(formatWriteDirectionLTR, unicode.IsSpace))
		} else {
			rs = append(rs, r)
			value += fontSize2
			floats = append(floats, fontSize2)
		}
	}
	if len(rs) > 0 {
		formatWriteDirectionLTR := string(rs)
		formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
		strs = append(strs, formatWriteDirectionLTR)
	}
	return strs, nil
}

// SetBorderWidth sets the border width of the ellipse.
func (e *Ellipse) SetBorderWidth(bw float64) { e.borderWidth = bw }

// FitMode returns the fit mode of the image.
func (im *Image) FitMode() FitMode    { return im.fitMode }
func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (ls *LinearShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingPatternResource(block, ls.ToPdfShadingPattern().ToPdfObject())
}

// GeneratePageBlocks draws the polyline on a new block representing the page.
// Implements the Drawable interface.
func (po *Polyline) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(po.lineOpacity, po.lineOpacity)
	if err != nil {
		return nil, ctx, err
	}
	points := po.polyline.Points
	for i := range points {
		point := &points[i]
		point.Y = ctx.PageHeight - point.Y
	}
	markedDraw, _, err := po.polyline.MarkedDraw(gsName, po.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}
func (ch *Chapter) chapterInfo() ChapterInfo {
	return ChapterInfo{Number: strings.TrimSuffix(ch.headingNumber(), "."), Title: ch.title, Level: ch.level}
}

// NewLine creates a new line between (x1, y1) to (x2, y2),
// using default attributes.
// NOTE: In relative positioning mode, `x1` and `y1` are calculated using the
// current context and `x2`, `y2` are used only to calculate the position of
// the second point in relation to the first one (used just as a measurement
// of size). Furthermore, when the fit mode is set to fill the context width,
// `x2` is set to the right edge coordinate of the context.
func (c *Creator) NewLine(x1, y1, x2, y2 float64) *Line { return newLine(x1, y1, x2, y2) }
func firstLastTextChild(vals []interface{}) (int, int) {
	n, n2 := -1, -1
	for i, v := range vals {
		if hasTextChunk(v) {
			if n < 0 {
				n = i
			}
			n2 = i
		}
	}
	return n, n2
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (ls *LinearShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }
func (pt *pageTransformations) transformBlock(block *Block) {
	if pt.matrix != nil {
		block.transform(*pt.matrix)
	}
}

// AppendCurve appends a Bezier curve to the filled curve.
func (fc *FilledCurve) AppendCurve(curve contentdraw.CubicBezierCurve) *FilledCurve {
	fc.curves = append(fc.curves, curve)
	return fc
}
func (tp *templateProcessor) parseLinearGradientAttr(creator *Creator, s2 string) (Color, error) {
	linearGradientAttr := ColorBlack
	if s2 == "" {
		return linearGradientAttr, nil
	}
	linearGradientColor := creator.NewLinearGradientColor([]*ColorPoint{})
	linearGradientColor.SetExtends(true, true)
	var (
		strs = strings.Split(s2[16:len(s2)-1], ",")
		s    = strings.TrimSpace(strs[0])
		err  error
	)
	if strings.HasSuffix(s, "deg") {
		finiteFloat, err2 := parseFiniteFloat(s[:len(s)-3])
		if err2 != nil {
			common.Log.Debug("Failed parsing gradient angle: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid gradient angle %q", s))
		} else {
			linearGradientColor.SetAngle(finiteFloat)
		}
		strs = strs[1:]
	}
	colors, positions, err2 := tp.processGradientColorPair(strs)
	setFirstError(&err, err2)
	if colors == nil || positions == nil {
		return linearGradientAttr, err
	}
	if len(colors) == 1 {
		return colors[0], err
	}
	for i := 0; i < len(colors); i++ {
		linearGradientColor.AddColorStop(colors[i], positions[i])
	}
	return linearGradientColor, err
}

// SetText replaces all the text of the paragraph with the specified one.
func (sp *StyledParagraph) SetText(text string) *TextChunk {
	sp.Reset()
	return sp.Append(text)
}

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) GenerateKDict() (*model.KDict, error) { return nil, nil }
func newPageBreak() *PageBreak                             { return &PageBreak{} }

// SetBorderOpacity sets the border opacity of the ellipse.
func (e *Ellipse) SetBorderOpacity(opacity float64) { e.borderOpacity = opacity }
func parseSVGElementTree(decoder *xml.Decoder) (*GraphicSVGElement, error) {
	for {
		token, err := decoder.Token()
		if token == nil && err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token2 := token.(type) {
		case xml.StartElement:
			return newGraphicSVGElement(token2), nil
		}
	}
	return &GraphicSVGElement{}, nil
}

// NewCell returns a new invoice table cell.
func (i *Invoice) NewCell(value string) *InvoiceCell {
	return i.newCell(value, i.NewCellProps())
}

// GetOpacity returns the cell's opacity in the range 0-1.
func (gc *GridCell) GetOpacity() float64 { return gc.opacity }
func fontAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("fontAttr", i, 0, pdfFontPtrType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		if target == nil {
			if err := proc.validateFontValue(value); err != nil {
				if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
					return err2
				}
			}
			return nil
		}
		font, err := proc.resolveFont(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(font))
		return nil
	}
}

// GetMargins returns the margins of the ellipse: left, right, top, bottom.
func (e *Ellipse) GetMargins() (float64, float64, float64, float64) {
	return e.margins.Left, e.margins.Right, e.margins.Top, e.margins.Bottom
}

// SetCompactMode sets the compact mode flag for this table.
//
// By enabling compact mode, a table cell that contains a StyledParagraph
// will not add extra height when calculating its height.
//
// The default value is false.
func (t *Table) SetCompactMode(enable bool) { t.compactMode = enable }
func borderRadiusAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("borderRadiusAttr", i, 0, float64Type, float64Type, float64Type, float64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		topLeft, topRight, bottomRight, bottomLeft, err := parseBorderRadius(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(topLeft), reflect.ValueOf(topRight), reflect.ValueOf(bottomRight), reflect.ValueOf(bottomLeft))
		return nil
	}
}
func parseColorComponent(s string) (float64, error) {
	s = strings.TrimSpace(s)
	var colorComponent float64
	if strings.HasSuffix(s, "%") {
		float, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil {
			return 0, err
		}
		colorComponent = (float * 255.0) / 100.0
	} else {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		colorComponent = float64(n)
	}
	return colorComponent, nil
}

// SetPositioning sets the positioning of the line (absolute or relative).
func (l *Line) SetPositioning(positioning Positioning) { l.positioning = positioning }

// SetStyle sets the style of the line (solid or dashed).
func (l *Line) SetStyle(style contentdraw.LineStyle) { l.style = style }

// GetMargins returns the margins of the line: left, right, top, bottom.
func (l *Line) GetMargins() (float64, float64, float64, float64) {
	return l.margins.Left, l.margins.Right, l.margins.Top, l.margins.Bottom
}

// ParsedTemplate is a template read and compiled once that can be rendered many
// times, into different creators, with different data. Compiling (reading the
// source, compiling the text/template and its subtemplates, and installing the
// escaping helpers) is done up front by ParseTemplate; a creator's RenderTemplate
// or ValidateTemplate (or the Block equivalents) executes the compiled template
// with the caller's data and builds the resulting XML tree, so the read and
// compile cost is not paid per render. A parsed template is not bound to a
// creator and may be reused across documents.
//
// Fonts referenced by a path are loaded per render (once per RenderTemplate
// call), so the same font is embedded independently in each document.
type ParsedTemplate struct {
	options *TemplateOptions
	tmpl    *template.Template
	source  string
}

// PageFinalizeFunctionArgs holds the input arguments provided to the page
// finalize callback function which can be set using Creator.PageFinalize.
type PageFinalizeFunctionArgs struct {
	PageNum    int
	PageWidth  float64
	PageHeight float64
	TOCPages   int
	TotalPages int

	// Chapter describes the chapter the current page belongs to, resolved via
	// Creator.ChapterForPage. It is nil on pages that precede the first chapter.
	Chapter *ChapterInfo
}

// FillOpacity returns the fill opacity of the ellipse (0-1).
func (e *Ellipse) FillOpacity() float64 { return e.fillOpacity }

// AddSubtable copies the cells of the subtable in the table, starting with the
// specified position. The table row and column indices are 1-based, which
// makes the position of the first cell of the first row of the table 1,1.
// The table is automatically extended if the subtable exceeds its columns.
// This can happen when the subtable has more columns than the table or when
// one or more columns of the subtable starting from the specified position
// exceed the last column of the table.
func (t *Table) AddSubtable(row, col int, subtable *Table) {
	for _, cell := range subtable.cells {
		cell2 := &TableCell{}
		*cell2 = *cell
		cell2.table = t
		cell2.col += col - 1
		if cols := t.cols - (cell2.col - 1); cols < cell2.colSpan {
			t.cols += cell2.colSpan - cols
			t.resetColumnWidths()
			common.Log.Debug("Table: subtable exceeds destination table. Expanding table to %d columns.", t.cols)
		}
		cell2.row += row - 1
		rowHeight := subtable.rowHeights[cell.row-1]
		if cell2.row > t.rows {
			for cell2.row > t.rows {
				t.rows++
				t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
			}
			t.rowHeights[cell2.row-1] = rowHeight
		} else {
			t.rowHeights[cell2.row-1] = math.Max(t.rowHeights[cell2.row-1], rowHeight)
		}
		t.cells = append(t.cells, cell2)
	}
	t.sortCells()
}

// Width returns the width of the specified text chunk.
func (tec *TextChunk) Width() float64 {
	var (
		value float64
		style = tec.Style
	)
	for _, r := range tec.Text {
		runeMetrics, ok := style.Font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("ERROR: Rune char metrics not found! rune=0x%04x=%c font=%s %#q", r, r, style.Font.BaseFont(), style.Font.Subtype())
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Font: %#v", style.Font)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Encoder: %#v", style.Font.Encoder())
			}
		}
		fontSize := style.FontSize * runeMetrics.Wx
		fontSize2 := fontSize
		if r != ' ' {
			fontSize2 = fontSize + style.CharSpacing*1000.0
		}
		value += fontSize2
	}
	return value / 1000.0
}
func boolAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("boolAttr", i, 0, boolType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		flag, err := parseBool(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
			flag = false
		}
		callSetter(value2, target, reflect.ValueOf(flag))
		return nil
	}
}
func (tp *templateProcessor) parseTable(node *templateNode) (interface{}, error) {
	tableColumns, err := tp.tableColumns(node)
	if err != nil {
		return nil, err
	}
	table := tp.creator.NewTable(int(tableColumns))
	if err := tp.applyAttrs(node, table, tableAttrs); err != nil {
		return nil, err
	}
	if err := tp.setTableHeaderRows(node, table, table.headerStartRow, table.headerEndRow); err != nil {
		return nil, err
	}
	return table, nil
}

// Padding returns the padding of the component.
func (d *Division) Padding() (left, right, top, bottom float64) {
	return d.padding.Left, d.padding.Right, d.padding.Top, d.padding.Bottom
}

// SetMargins sets the Block's left, right, top, bottom, margins.
func (b *Block) SetMargins(left, right, top, bottom float64) {
	b.margins.Left = left
	b.margins.Right = right
	b.margins.Top = top
	b.margins.Bottom = bottom
}

// HasHeader returns whether the table has header rows that are repeated for
// every page the table spans. See SetHeaderRows. Note that Table defines its
// header at the table level over a row range, whereas Grid defines headers
// per row via the row's section (see GridRow.IsHeader).
func (t *Table) HasHeader() bool { return t.hasHeaderRows }

// SetTextVerticalAlignment sets the vertical alignment of the text within the
// bounds of the styled paragraph.
//
// Note: Currently Styled Paragraph doesn't support TextVerticalAlignmentBottom
// as that option only used for aligning text chunks.
//
// In order to change the vertical alignment of individual text chunks, use TextChunk.VerticalAlignment.
func (sp *StyledParagraph) SetTextVerticalAlignment(align TextVerticalAlignment) {
	sp.verticalAlignment = align
}
func newTOC(text string, lineNumberStyle, style2 TextStyle) *TOC {
	style := style2
	style.FontSize = 14
	styledParagraph := newStyledParagraph(style)
	styledParagraph.SetEnableWrap(true)
	styledParagraph.SetTextAlignment(TextAlignmentLeft)
	styledParagraph.SetMargins(0, 0, 0, 5)
	chunk := styledParagraph.Append(text)
	chunk.Style = style
	return &TOC{heading: styledParagraph, lines: []*TOCLine{}, lineNumberStyle: lineNumberStyle, lineTitleStyle: lineNumberStyle, lineSeparatorStyle: lineNumberStyle, linePageStyle: lineNumberStyle, lineSeparator: ".", lineLevelOffset: 10, lineMargins: Margins{0, 0, 2, 2}, positioning: PositionRelative, defaultStyle: lineNumberStyle, showLinks: true, taggedDrawable: taggedDrawable{structType: model.StructureTypeTOC}}
}

// SetDefaultFont sets the fonts used by default by creator components which
// have no font specified, replacing the standard Helvetica and Helvetica-Bold
// fonts. This covers text styles created via NewTextStyle, components built
// on them (e.g. NewStyledParagraph, NewTOC, NewInvoice) and template
// components with no `font` attribute. Setting a composite font enables
// scripts the standard fonts do not cover, such as Cyrillic or CJK. The
// method should be called before any components are created or drawn. A nil
// argument leaves the corresponding default font unchanged.
func (c *Creator) SetDefaultFont(fontRegular, fontBold *model.PdfFont) {
	fontRegular2 := c.fontRegular
	fontBold2 := c.fontBold
	if fontRegular != nil {
		c.fontRegular = fontRegular
	}
	if fontBold != nil {
		c.fontBold = fontBold
	}
	c.toc.replaceFonts(fontRegular2, c.fontRegular, fontBold2, c.fontBold)
}

type componentRenderer interface{ Draw(drawable Drawable) error }

const (
	TextAlignmentLeft TextAlignment = iota
	TextAlignmentRight
	TextAlignmentCenter
	TextAlignmentJustify
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
func intArrayAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("intArrayAttr", i, 0, int64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
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
func (c *Creator) ValidateTemplate(pt *ParsedTemplate, data interface{}) error {
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
					if strings.HasSuffix(attribute, "%") {
						float, err := strconv.ParseFloat(strings.TrimSuffix(attribute, "%"), 64)
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
func templateNodeValue(node *templateNode) interface{} {
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

type attrApplyFunc func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error

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
	stringerType = reflect.TypeOf((*fmt.Stringer)(nil)).Elem()
	errorType    = reflect.TypeOf((*error)(nil)).Elem()
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

// DropCapsOptions contains configuration options for drop caps rendering.
type DropCapsOptions struct {

	// Type specifies the drop caps type (Drop or Inline).
	Type DropCapsType

	// Scope defines what portion of the paragraph text gets drop caps treatment.
	// Default: DropCapsFirstCharacter
	Scope DropCapsScope

	// NumLines specifies how many lines the drop cap should span.
	// For DropCapsDrop: how many lines flow beside before continuing below.
	// For DropCapsInline: affects the size of the drop cap character.
	// Default: 3 lines.
	NumLines int

	// Gap specifies the horizontal distance between the drop cap and the text.
	// Measured in points. Default: 5.0 points.
	Gap float64

	// Style specifies the text style to apply to the drop cap character(s).
	// This allows customization of font, color, outline, and other text properties.
	// If nil, the drop cap will inherit the style from the first chunk of text.
	// Note: The FontSize in this style will be overridden by the calculated size
	// based on NumLines, but other properties like Color, Font, OutlineColor,
	// CharSpacing, etc. will be applied.
	Style *TextStyle
}

func (tp *templateProcessor) parseChapterHeading(node *templateNode) (interface{}, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "Chapter heading parent cannot be nil.")
		return nil, errInvalidParent
	}
	chapter, ok := componentParent.value.(*Chapter)
	if !ok {
		tp.nodeLogError(node, "Chapter heading parent (%T) is not a chapter.", componentParent.value)
		return nil, errInvalidParent
	}
	heading := chapter.GetHeading()
	if _, err := tp.parseParagraph(node, heading); err != nil {
		return nil, err
	}
	return heading, nil
}
func (sp *StyledParagraph) splitToPieces(ctx DrawContext) ([]*StyledParagraph, error) {
	if err := sp.wrapChunks(false); err != nil {
		return nil, err
	}
	if !sp.positioning.IsRelative() || len(sp.lines) <= 1 {
		sp.lines = nil
		return []*StyledParagraph{sp}, nil
	}
	fn := func(chunks []*TextChunk, chunks2 []*TextChunk) []*TextChunk {
		if len(chunks2) == 0 {
			return chunks
		}
		chunksLen := len(chunks)
		if chunksLen == 0 {
			return append(chunks, chunks2...)
		}
		if chunks[chunksLen-1].Style == chunks2[0].Style {
			chunks[chunksLen-1].Text += chunks2[0].Text
		} else {
			chunks = append(chunks, chunks2[0])
		}
		return append(chunks, chunks2[1:]...)
	}
	fn2 := func(chunks []*TextChunk) *StyledParagraph {
		para := *sp
		para.chunks = chunks
		para.lines = nil
		return &para
	}
	var (
		paras  []*StyledParagraph
		chunks []*TextChunk
		value  float64
	)
	for _, line := range sp.lines {
		var value2 float64
		chunks2 := make([]*TextChunk, 0, len(line))
		for _, chunk := range line {
			if fontSize := chunk.Style.FontSize * chunk.Style.effectiveLineHeight(sp.lineHeight); fontSize > value2 {
				value2 = fontSize
			}
			chunks2 = append(chunks2, chunk.clone())
		}
		if value+value2 >= ctx.Height && len(chunks) > 0 {
			paras = append(paras, fn2(chunks))
			chunks = nil
			value = 0
		}
		chunks = fn(chunks, chunks2)
		value += value2
	}
	if len(chunks) > 0 {
		paras = append(paras, fn2(chunks))
	}
	sp.lines = nil
	return paras, nil
}

// GetHeight get the current height for the row.
func (gr *GridRow) GetHeight() float64 { return gr.height }

// SetColor sets the line color.
func (cu *Curve) SetColor(col Color) { cu.color = col }

// Link returns link information for this line.
func (tl *TOCLine) Link() (page int64, x, y float64) {
	return tl.linkPage, tl.linkX, tl.linkY
}

// SetTitleStyle sets the style properties of the invoice title.
func (i *Invoice) SetTitleStyle(style TextStyle) { i.titleStyle = style }
func (sp *StyledParagraph) wrapWordChunks() {
	if !sp.enableWordWrap {
		return
	}
	var (
		chunks []*TextChunk
		font   *model.PdfFont
	)
	for _, chunk := range sp.chunks {
		text := []rune(chunk.Text)
		if font == nil {
			font = chunk.Style.Font
		}
		annotations := chunk.annotations
		verticalAlignment := chunk.VerticalAlignment
		if len(chunks) > 0 {
			if len(text) == 1 && unicode.IsPunct(text[0]) && chunk.Style.Font == font {
				text2 := []rune(chunks[len(chunks)-1].Text)
				chunks[len(chunks)-1].Text = string(append(text2, text[0]))
				continue
			} else {
				_, err := strconv.Atoi(chunk.Text)
				if err == nil {
					text2 := []rune(chunks[len(chunks)-1].Text)
					textLen := len(text2)
					if textLen >= 2 {
						_, err := strconv.Atoi(string(text2[textLen-2]))
						if err == nil && unicode.IsPunct(text2[textLen-1]) {
							chunks[len(chunks)-1].Text = string(append(text2, text...))
							continue
						}
					}
				}
			}
		}
		intoWords, err := splitIntoWords(chunk.Text)
		if err != nil {
			common.Log.Debug("ERROR: Unable to break string to words: %v", err)
			intoWords = []string{chunk.Text}
		}
		for _, intoWord := range intoWords {
			textChunk := NewTextChunk(intoWord, chunk.Style)
			textChunk.annotations = copyAnnotations(annotations)
			textChunk.VerticalAlignment = verticalAlignment
			chunks = append(chunks, textChunk)
		}
		font = chunk.Style.Font
	}
	if len(chunks) > 0 {
		sp.chunks = chunks
	}
}

// Angle returns the block rotation angle in degrees.
func (b *Block) Angle() float64 { return b.angle }

const (
	CellBorderSideLeft CellBorderSide = iota
	CellBorderSideRight
	CellBorderSideTop
	CellBorderSideBottom
	CellBorderSideAll
)

func (g *Grid) cloneRow(n int) *GridRow {
	row := &GridRow{height: g.defaultRowHeight, index: n + 1, grid: g, section: g.rows[n].section}
	for _, cell := range g.rows[n].cells {
		cell2 := &GridCell{backgroundColor: cell.backgroundColor, opacity: cell.opacity, borderLineStyle: cell.borderLineStyle, borderStyleLeft: cell.borderStyleLeft, borderColorLeft: cell.borderColorLeft, borderWidthLeft: cell.borderWidthLeft, borderStyleBottom: cell.borderStyleBottom, borderColorBottom: cell.borderColorBottom, borderWidthBottom: cell.borderWidthBottom, borderStyleRight: cell.borderStyleRight, borderColorRight: cell.borderColorRight, borderWidthRight: cell.borderWidthRight, borderStyleTop: cell.borderStyleTop, borderColorTop: cell.borderColorTop, borderWidthTop: cell.borderWidthTop, row: n + 1, col: cell.col, rowSpan: 1, colSpan: cell.colSpan, horizontalAlignment: cell.horizontalAlignment, verticalAlignment: cell.verticalAlignment, indent: cell.indent}
		cell2.structType = model.StructureTypeTableData
		row.cells = append(row.cells, cell2)
	}
	return row
}

// Level returns the indentation level of the TOC line.
func (_acgfaed *TOCLine) Level() uint { return _acgfaed.level }

const (
	GridRowSectionBody GridRowSection = iota
	GridRowSectionHeader
	GridRowSectionFooter
	GridRowSectionUnknown
)

func (tp *templateProcessor) parseTree() (*templateNode, error) {
	b := bytes.TrimPrefix(tp.data, []byte("\ufeff"))
	decoder := xml.NewDecoder(bytes.NewReader(b))
	node := &templateNode{}
	parent := node
	for {
		line, col := decoderInputPos(decoder)
		inputOffset := decoder.InputOffset()
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if token == nil {
			break
		}
		switch token2 := token.(type) {
		case xml.StartElement:
			common.Log.Debug("Parsing template start tag: `%s`.", token2.Name.Local)
			node := &templateNode{start: token2.Copy(), parent: parent, line: line, col: col, offset: inputOffset}
			parent.children = append(parent.children, node)
			parent.childValues = append(parent.childValues, node)
			parent = node
		case xml.EndElement:
			common.Log.Debug("Parsing template end tag: `%s`.", token2.Name.Local)
			if parent != node {
				parent = parent.parent
			}
		case xml.CharData:
			templateText2 := newTemplateText(b, string(token2), line, col, inputOffset)
			if childValuesLen := len(parent.childValues); childValuesLen > 0 {
				if templateText3, ok := parent.childValues[childValuesLen-1].(*templateText); ok {
					if isBlank(templateText3.text) && !isBlank(templateText2.text) {
						templateText3.line, templateText3.col, templateText3.offset = templateText2.line, templateText2.col, templateText2.offset
					}
					templateText3.text += templateText2.text
					break
				}
			}
			parent.childValues = append(parent.childValues, templateText2)
		case xml.Comment:
			common.Log.Debug("Parsing template comment: `%s`.", string(token2))
		}
	}
	return node, nil
}

// Positioning returns the type of positioning the ellipse is set to use.
func (e *Ellipse) Positioning() Positioning { return e.positioning }

// BorderColor returns the border color of the ellipse.
func (e *Ellipse) BorderColor() Color { return e.borderColor }

const (
	DropCapsNone DropCapsType = iota
	DropCapsDrop
	DropCapsInline
)

func cropImageFast(img2 *model.Image, rect, rect2 image.Rectangle) *model.Image {
	colorComponents := img2.ColorComponents
	dx, dy := rect.Dx(), rect.Dy()
	colorComponents2 := int(img2.Width) * colorComponents
	dx2 := dx * colorComponents
	colorComponents3 := rect2.Dx() * colorComponents
	data := make([]byte, dx2*dy)
	for i := rect2.Min.Y; i < rect2.Max.Y; i++ {
		n := i*colorComponents2 + rect2.Min.X*colorComponents
		dx := (i - rect2.Min.Y) * dx2
		copy(data[dx:dx+colorComponents3], img2.Data[n:n+colorComponents3])
	}
	img := &model.Image{Width: int64(dx), Height: int64(dy), BitsPerComponent: 8, ColorComponents: colorComponents, Data: data}
	img.SetAlpha(cropAlphaData(img2, rect, rect2))
	img.SetICCProfile(img2.ICCProfile())
	return img
}
func parseInt64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
func (tp *templateProcessor) parseDivision(node *templateNode) (interface{}, error) {
	division := tp.creator.NewDivision()
	if err := tp.applyAttrs(node, division, divisionAttrs); err != nil {
		return nil, err
	}
	return division, nil
}

// SetNoteStyle sets the style properties used to render the content of the
// invoice note sections.
func (i *Invoice) SetNoteStyle(style TextStyle) { i.noteStyle = style }

// FrontpageFunctionArgs holds the input arguments to a front page drawing function.
// It is designed as a struct, so additional parameters can be added in the future with backwards
// compatibility.
type FrontpageFunctionArgs struct {
	PageNum    int
	TotalPages int
}

// Lazy gets the lazy mode for the image.
func (im *Image) Lazy() bool { return im.lazy }
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

var carrierAttrs = map[string]struct{}{"font": struct{}{}, "font-size": struct{}{}, "color": struct{}{}, "line-height": struct{}{}, "text-align": struct{}{}}

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
func floatAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("floatAttr", i, 0, float64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
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
func (b *Block) ValidateTemplate(c *Creator, pt *ParsedTemplate, data interface{}) error {
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

// GeneratePageBlocks implements drawable interface.
func (bo *border) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	x := bo.x
	pageHeight := ctx.PageHeight - bo.y
	if bo.fillColor != nil {
		rect := contentdraw.Rectangle{Opacity: 1.0, X: bo.x, Y: ctx.PageHeight - bo.y - bo.height, Height: bo.height, Width: bo.width}
		rect.FillEnabled = true
		pdfColor := toPdfColor(bo.fillColor)
		err := applyShapeColor(block, pdfColor, bo.fillColor, func() Rectangle {
			return Rectangle{x: rect.X, y: rect.Y, width: rect.Width, height: rect.Height}
		})
		if err != nil {
			return nil, ctx, err
		}
		rect.FillColor = pdfColor
		rect.BorderEnabled = false
		gsName := ""
		if bo.opacity < 1 {
			dict := core.MakeDict()
			dict.Set("ca", core.MakeFloat(bo.opacity))
			dict.Set("CA", core.MakeFloat(bo.opacity))
			err := block.resources.AddExtGState("gs1", dict)
			if err == nil {
				gsName = "gs1"
			}
		}
		data, _, err := rect.Draw(gsName)
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	widthTop := bo.widthTop
	widthBottom := bo.widthBottom
	widthLeft := bo.widthLeft
	widthRight := bo.widthRight
	widthTop2 := bo.widthTop
	if bo.styleTop == CellBorderStyleDouble {
		widthTop2 += 2 * widthTop
	}
	widthBottom2 := bo.widthBottom
	if bo.styleBottom == CellBorderStyleDouble {
		widthBottom2 += 2 * widthBottom
	}
	widthLeft2 := bo.widthLeft
	if bo.styleLeft == CellBorderStyleDouble {
		widthLeft2 += 2 * widthLeft
	}
	widthRight2 := bo.widthRight
	if bo.styleRight == CellBorderStyleDouble {
		widthRight2 += 2 * widthRight
	}
	value := (widthTop2 - widthLeft2) / 2
	value2 := (widthTop2 - widthRight2) / 2
	value3 := (widthBottom2 - widthLeft2) / 2
	value4 := (widthBottom2 - widthRight2) / 2
	if bo.widthTop != 0 {
		value3 := x
		y1 := pageHeight
		if bo.styleTop == CellBorderStyleDouble {
			y1 -= widthTop
			basicLine := contentdraw.BasicLine{LineColor: toPdfColor(bo.colorTop), Opacity: 1.0, LineWidth: bo.widthTop, LineStyle: bo.LineStyle, X1: value3 - widthTop2/2 + value, Y1: y1 + 2*widthTop, X2: value3 + widthTop2/2 - value2 + bo.width, Y2: y1 + 2*widthTop}
			data, _, err := basicLine.Draw("")
			if err != nil {
				return nil, ctx, err
			}
			err = block.addContentsByString(string(data))
			if err != nil {
				return nil, ctx, err
			}
		}
		basicLine := contentdraw.BasicLine{LineWidth: bo.widthTop, Opacity: 1.0, LineColor: toPdfColor(bo.colorTop), LineStyle: bo.LineStyle, X1: value3 - widthTop2/2 + value + (widthLeft2 - bo.widthLeft), Y1: y1, X2: value3 + widthTop2/2 - value2 + bo.width - (widthRight2 - bo.widthRight), Y2: y1}
		data, _, err := basicLine.Draw("")
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	if bo.widthBottom != 0 {
		value := x
		y1 := pageHeight - bo.height
		if bo.styleBottom == CellBorderStyleDouble {
			y1 += widthBottom
			basicLine := contentdraw.BasicLine{LineWidth: bo.widthBottom, Opacity: 1.0, LineColor: toPdfColor(bo.colorBottom), LineStyle: bo.LineStyle, X1: value - widthBottom2/2 + value3, Y1: y1 - 2*widthBottom, X2: value + widthBottom2/2 - value4 + bo.width, Y2: y1 - 2*widthBottom}
			data, _, err := basicLine.Draw("")
			if err != nil {
				return nil, ctx, err
			}
			err = block.addContentsByString(string(data))
			if err != nil {
				return nil, ctx, err
			}
		}
		basicLine := contentdraw.BasicLine{LineWidth: bo.widthBottom, Opacity: 1.0, LineColor: toPdfColor(bo.colorBottom), LineStyle: bo.LineStyle, X1: value - widthBottom2/2 + value3 + (widthLeft2 - bo.widthLeft), Y1: y1, X2: value + widthBottom2/2 - value4 + bo.width - (widthRight2 - bo.widthRight), Y2: y1}
		data, _, err := basicLine.Draw("")
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	if bo.widthLeft != 0 {
		x1 := x
		value2 := pageHeight
		if bo.styleLeft == CellBorderStyleDouble {
			x1 += widthLeft
			basicLine := contentdraw.BasicLine{LineWidth: bo.widthLeft, Opacity: 1.0, LineColor: toPdfColor(bo.colorLeft), LineStyle: bo.LineStyle, X1: x1 - 2*widthLeft, Y1: value2 + widthLeft2/2 + value, X2: x1 - 2*widthLeft, Y2: value2 - widthLeft2/2 - value3 - bo.height}
			data, _, err := basicLine.Draw("")
			if err != nil {
				return nil, ctx, err
			}
			err = block.addContentsByString(string(data))
			if err != nil {
				return nil, ctx, err
			}
		}
		basicLine := contentdraw.BasicLine{LineWidth: bo.widthLeft, Opacity: 1.0, LineColor: toPdfColor(bo.colorLeft), LineStyle: bo.LineStyle, X1: x1, Y1: value2 + widthLeft2/2 + value - (widthTop2 - bo.widthTop), X2: x1, Y2: value2 - widthLeft2/2 - value3 - bo.height + (widthBottom2 - bo.widthBottom)}
		data, _, err := basicLine.Draw("")
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	if bo.widthRight != 0 {
		x1 := x + bo.width
		value := pageHeight
		if bo.styleRight == CellBorderStyleDouble {
			x1 -= widthRight
			basicLine := contentdraw.BasicLine{LineWidth: bo.widthRight, Opacity: 1.0, LineColor: toPdfColor(bo.colorRight), LineStyle: bo.LineStyle, X1: x1 + 2*widthRight, Y1: value + widthRight2/2 + value2, X2: x1 + 2*widthRight, Y2: value - widthRight2/2 - value4 - bo.height}
			data, _, err := basicLine.Draw("")
			if err != nil {
				return nil, ctx, err
			}
			err = block.addContentsByString(string(data))
			if err != nil {
				return nil, ctx, err
			}
		}
		basicLine := contentdraw.BasicLine{LineWidth: bo.widthRight, Opacity: 1.0, LineColor: toPdfColor(bo.colorRight), LineStyle: bo.LineStyle, X1: x1, Y1: value + widthRight2/2 + value2 - (widthTop2 - bo.widthTop), X2: x1, Y2: value - widthRight2/2 - value4 - bo.height + (widthBottom2 - bo.widthBottom)}
		data, _, err := basicLine.Draw("")
		if err != nil {
			return nil, ctx, err
		}
		err = block.addContentsByString(string(data))
		if err != nil {
			return nil, ctx, err
		}
	}
	return []*Block{block}, ctx, nil
}

// SetFillColor sets the fill color for the path.
func (fc *FilledCurve) SetFillColor(color Color) { fc.fillColor = color }
func (co *Command) isAbsolute() bool             { return co.Symbol == strings.ToUpper(co.Symbol) }

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (tl *TOCLine) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	pageBlocks, ctx, err := tl.paragraph.GeneratePageBlocks(ctx)
	if err != nil {
		return pageBlocks, ctx, err
	}
	if tl.positioning.IsRelative() {
		ctx.X = ctx2.X
	}
	if tl.positioning.IsAbsolute() {
		return pageBlocks, ctx2, nil
	}
	return pageBlocks, ctx, nil
}
func floatArrayAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("floatArrayAttr", i, 0, float64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		floatArray, err := parseFloatArray(value)
		if err != nil {
			if _aaagcce := proc.strictErr(node, attrName, value, err); _aaagcce != nil {
				return _aaagcce
			}
		}
		callSetter(value2, target, reflect.ValueOf(floatArray))
		return nil
	}
}

// GetArtifact returns the artifact associated with the image, if any.
func (im *Image) GetArtifact() *model.Artifact { return im.artifact }

// NewGraphicSVGFromString creates a graphic SVG from a SVG string.
func NewGraphicSVGFromString(svgStr string) (*GraphicSVG, error) {
	return newGraphicSVGFromString(svgStr)
}

// SetFillColor sets the fill color of the ellipse.
func (e *Ellipse) SetFillColor(col Color) { e.fillColor = col }

// EnableFontSubsetting enables font subsetting for `font` when the creator output is written to file.
// Embeds only the subset of the runes/glyphs that are actually used to display the file.
// Subsetting can reduce the size of fonts significantly.
func (c *Creator) EnableFontSubsetting(font *model.PdfFont) {
	c.subsetFonts = append(c.subsetFonts, font)
}
func (pat *ParsedTemplate) renderInto(creator *Creator, renderer componentRenderer, data interface{}) error {
	if creator == nil {
		common.Log.Error("Creator instance cannot be nil.")
		return errInvalidCreator
	}
	data2, err := pat.execute(data)
	if err != nil {
		return err
	}
	return newTemplateProcessor(creator, pat.source, data2, pat.options, renderer).render()
}

// TextDecorationLineStyle represents the style of lines used to decorate
// a text chunk (e.g. underline).
type TextDecorationLineStyle struct {

	// Color represents the color of the line (default: the color of the text).
	Color Color

	// Offset represents the vertical offset of the line (default: 1).
	Offset float64

	// Thickness represents the thickness of the line (default: 1).
	Thickness float64
}

// CreateFrontPage sets a function to generate a front Page.
//
// Chapters drawn inside the callback (via NewChapter + Draw) are not tracked
// by Creator.ChapterForPage and will not appear in header/footer callback
// args. The front page is expected to hold cover content, not chapter bodies.
func (c *Creator) CreateFrontPage(genFrontPageFunc func(frontpageFunctionArgs FrontpageFunctionArgs)) {
	c.genFrontPageFunc = genFrontPageFunc
}

// SetBorderColor sets the border color.
func (cp *CurvePolygon) SetBorderColor(color Color) { cp.polygon.BorderColor = toPdfColor(color) }
func newImageFromGoImage(img image.Image) (*Image, error) {
	image, err := model.ImageHandling.NewImageFromGoImage(img)
	if err != nil {
		return nil, err
	}
	return newImage(image)
}
func isSkinToneModifier(r rune) bool { return r >= 0x1F3FB && r <= 0x1F3FF }

// NewBlockFromPage creates a Block from a PDF Page.  Useful for loading template pages as blocks
// from a PDF document and additional content with the creator.
func NewBlockFromPage(page *model.PdfPage) (*Block, error) {
	block := &Block{}
	allContentStreams, err := page.GetAllContentStreams()
	if err != nil {
		return nil, err
	}
	contentStreamParser := contentstream.NewContentStreamParser(allContentStreams)
	contents, err := contentStreamParser.Parse()
	if err != nil {
		return nil, err
	}
	contents.WrapIfNeeded()
	block.contents = contents
	if page.Resources != nil {
		block.resources = page.Resources
	} else {
		block.resources = model.NewPdfPageResources()
	}
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return nil, err
	}
	if mediaBox.Llx != 0 || mediaBox.Lly != 0 {
		block.translate(-mediaBox.Llx, mediaBox.Lly)
	}
	block.width = mediaBox.Urx - mediaBox.Llx
	block.height = mediaBox.Ury - mediaBox.Lly
	if page.Rotate != nil {
		block.angle = -float64(*page.Rotate)
	}
	return block, nil
}

type token struct {
	text      string
	isCommand bool
}

// SetBorderOpacity sets the border opacity.
func (pbc *PolyBezierCurve) SetBorderOpacity(opacity float64) { pbc.borderOpacity = opacity }

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

func (tp *templateProcessor) nodeError(node *templateNode, format string, a ...interface{}) error {
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
func (tp *templateProcessor) parseChart(node *templateNode) (interface{}, error) {
	_abcbdab, _ := tp.effectiveAttrValue(node, "src")
	if _abcbdab == "" {
		return nil, tp.nodeError(node, "chart `src` attribute cannot be empty")
	}
	chart, ok := tp.options.ChartMap[_abcbdab]
	if !ok {
		return nil, tp.nodeError(node, "could not find chart resource: `%s`", _abcbdab)
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
func applySVGTransform(graphicSVGElement *GraphicSVGElement, cc *contentstream.ContentCreator) {
	attribute, ok := graphicSVGElement.Attributes["transform"]
	if ok {
		fields := strings.Fields(attribute)
		for _, field := range fields {
			fieldsFunc := strings.FieldsFunc(field, isTransformSeparator)
			if len(fieldsFunc) < 3 {
				common.Log.Debug("can't parse transform attribute %v", attribute)
				return
			}
			svgLength, err := parseSVGLength(fieldsFunc[1], 64)
			if err != nil {
				common.Log.Debug("can't parse transform attribute %v", attribute)
				return
			}
			ty, err := parseSVGLength(fieldsFunc[2], 64)
			if err != nil {
				common.Log.Debug("can't parse transform attribute %v", attribute)
				return
			}
			switch fieldsFunc[0] {
			case "translate":
				cc.Translate(svgLength, ty)
			case "scale":
				cc.Scale(svgLength, ty)
			default:
				common.Log.Debug("can't parse transform attribute %v", attribute)
				return
			}
		}
	}
}

// ToPdfShadingPattern generates a new model.PdfShadingPatternType1 object. It
// returns an error when the PostScript program cannot be parsed or does not
// produce a usable number of color components, rather than a pattern with no
// shading in it.
func (fbs *FunctionBasedShading) ToPdfShadingPattern() (*model.PdfShadingPatternType1, error) {
	shadingModel, err := fbs.shadingModel()
	if err != nil {
		return nil, err
	}
	pdfShadingPatternType1 := model.NewPdfShadingPatternType1()
	pdfShadingPatternType1.Shading = shadingModel
	return pdfShadingPatternType1, nil
}

// FillColor returns the fill color of the rectangle.
func (r *Rectangle) FillColor() Color { return r.fillColor }
func addShadingResource(block *Block, shadingObj core.PdfObject) (core.PdfObjectName, error) {
	i := 1
	keyName := core.PdfObjectName("Sh" + strconv.Itoa(i))
	for block.resources.HasShadingByName(keyName) {
		i++
		keyName = core.PdfObjectName("Sh" + strconv.Itoa(i))
	}
	if err := block.resources.SetShadingByName(keyName, shadingObj); err != nil {
		return "", err
	}
	return keyName, nil
}

// RemoveArtifact removes the artifact associated with the paragraph, if any.
func (sp *StyledParagraph) RemoveArtifact() { sp.artifact = nil }

// SetColor sets the line color. Use ColorRGBFromHex, ColorRGBFrom8bit or
// ColorRGBFromArithmetic to create the color object.
func (l *Line) SetColor(color Color) { l.color = color }

// WrappedHeight returns the total height the chunk would occupy when wrapped
// to the given width: number of wrapped lines × Height().
func (tec *TextChunk) WrappedHeight(width float64) (float64, error) {
	strs, err := tec.Wrap(width)
	if err != nil {
		return 0, err
	}
	return float64(len(strs)) * tec.Height(), nil
}

// GetDefaultLinkStyle returns the text style applied to link annotations
// appended to the paragraph.
func (sp *StyledParagraph) GetDefaultLinkStyle() TextStyle { return sp.linkStyle }

// IsHeader returns whether this row is a header row. Unlike Table, where the
// header is defined at the table level over a range of rows (see
// Table.HasHeader and Table.SetHeaderRows), a Grid defines headers per row via
// the row's section (see GetSection). IsHeader is a convenience shorthand for
// GetSection() == GridRowSectionHeader.
func (gr *GridRow) IsHeader() bool { return gr.section == GridRowSectionHeader }

// GetStructureType returns the structure type of this drawable.
func (td *taggedDrawable) GetStructureType() model.StructureType { return td.tagInfo.StructureType }

const (
	imageMaskSoft imageMaskKind = iota + 1
	imageMaskStencil
	imageMaskColorKey
	imageMaskOptimizedAlpha
)

// GeneratePageBlocks draws the filled curve on page blocks.
func (fc *FilledCurve) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	data, _, err := fc.draw(block, "")
	if err != nil {
		return nil, ctx, err
	}
	err = block.addContentsByString(string(data))
	if err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// GetMargins returns the margins of the chart (left, right, top, bottom).
func (cha *Chart) GetMargins() (float64, float64, float64, float64) {
	return cha.margins.Left, cha.margins.Right, cha.margins.Top, cha.margins.Bottom
}

// AddShadingResource adds shading dictionary inside the resources dictionary.
func (rs *RadialShading) AddShadingResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingResource(block, rs.shadingModel().ToPdfObject())
}

// SetDueDate sets the due date of the invoice.
func (i *Invoice) SetDueDate(dueDate string) (*InvoiceCell, *InvoiceCell) {
	i.dueDate[1].Value = dueDate
	return i.dueDate[0], i.dueDate[1]
}

// Title returns the title of the invoice.
func (i *Invoice) Title() string { return i.title }

// SetColorTop sets border color for top.
func (bo *border) SetColorTop(col Color) { bo.colorTop = col }

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (ls *LinearShading) SetAntiAlias(enable bool) { ls.base.SetAntiAlias(enable) }

var ellipseAttrs = map[string]attrApplyFunc{"cx": floatAttr(func(ellipse *Ellipse, xc float64) { ellipse.xc = xc }), "cy": floatAttr(func(ellipse *Ellipse, yc float64) { ellipse.yc = yc }), "width": floatAttr(func(ellipse *Ellipse, width float64) { ellipse.SetWidth(width) }), "height": floatAttr(func(ellipse *Ellipse, height float64) { ellipse.SetHeight(height) }), "fill-color": colorAttr(func(ellipse *Ellipse, col Color) { ellipse.SetFillColor(col) }), "fill-opacity": floatAttr(func(ellipse *Ellipse, opacity float64) { ellipse.SetFillOpacity(opacity) }), "border-color": colorAttr(func(ellipse *Ellipse, col Color) { ellipse.SetBorderColor(col) }), "border-opacity": floatAttr(func(ellipse *Ellipse, opacity float64) { ellipse.SetBorderOpacity(opacity) }), "border-width": floatAttr(func(ellipse *Ellipse, bw float64) { ellipse.SetBorderWidth(bw) }), "position": enumAttr(positioningValues, func(ellipse *Ellipse, position Positioning) { ellipse.SetPositioning(position) }), "fit-mode": enumAttr(fitModeValues, func(ellipse *Ellipse, fitMode FitMode) { ellipse.SetFitMode(fitMode) }), "margin": marginAttr(func(ellipse *Ellipse, margins Margins) {
	ellipse.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

type templateNode struct {
	value       interface{}
	start       xml.StartElement
	parent      *templateNode
	children    []*templateNode
	childValues []interface{}
	attrs       map[string]string
	textAttrs   map[string]string
	hasHandler  bool
	hasChild    bool
	line        int
	col         int
	offset      int64
	hasText     bool
}

// Height returns the height of the line.
func (l *Line) Height() float64 {
	lineWidth := l.lineWidth
	if l.x1 == l.x2 {
		lineWidth /= 2
	}
	return math.Abs(l.y2-l.y1) + lineWidth
}

// SetStructTreeRoot sets the structure tree root to be appended in the document that will be created.
func (c *Creator) SetStructTreeRoot(structTreeRoot *model.StructTreeRoot) {
	c.structTreeRoot = structTreeRoot
}

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) SetStructureType(structureType model.StructureType) {}
func isTransformSeparator(r rune) bool                              { return r == '(' || r == ',' || r == ')' }

// IsAbsolute checks if the positioning is absolute.
func (pos Positioning) IsAbsolute() bool { return pos == PositionAbsolute }

// DrawTemplate renders the template provided through the specified reader,
// using the specified `data` and `options`.
// Creator templates are first executed as text/template *Template instances,
// so the specified `data` is inserted within the template.
// The second phase of processing is actually parsing the template, translating
// it into creator components and rendering them using the provided options.
// Both the `data` and `options` parameters can be nil.
func (b *Block) DrawTemplate(c *Creator, r io.Reader, data interface{}, options *TemplateOptions) error {
	return drawTemplate(c, r, data, options, b)
}
func parseBool(str string) (bool, error) { return strconv.ParseBool(str) }
func svgStandardFont(s string) (*model.PdfFont, error) {
	basefont, ok := map[string]model.StdFontName{"courier": model.CourierName, "courier-bold": model.CourierBoldName, "courier-oblique": model.CourierObliqueName, "courier-bold-oblique": model.CourierBoldObliqueName, "helvetica": model.HelveticaName, "helvetica-bold": model.HelveticaBoldName, "helvetica-oblique": model.HelveticaObliqueName, "helvetica-bold-oblique": model.HelveticaBoldObliqueName, "symbol": model.SymbolName, "zapf-dingbats": model.ZapfDingbatsName, "times": model.TimesRomanName, "times-bold": model.TimesBoldName, "times-italic": model.TimesItalicName, "times-bold-italic": model.TimesBoldItalicName}[s]
	if !ok {
		return nil, fmt.Errorf("font-family %s not found in the standard fonts list", s)
	}
	standard14Font, err := model.NewStandard14Font(basefont)
	if err != nil {
		return nil, err
	}
	return standard14Font, nil
}

// SetEncoder sets the encoding/compression mechanism for the image.
//
// Note: for an image loaded from a JPEG file/data, setting a DCTEncoder (the
// default) embeds the original JPEG bytes unchanged, so encoder settings such
// as quality are not applied. Set a non-DCT encoder (e.g. a FlateEncoder) to
// force a re-encode.
func (im *Image) SetEncoder(encoder core.StreamEncoder) { im.encoder = encoder }
func (tp *templateProcessor) parseImage(node *templateNode) (interface{}, error) {
	effectiveAttrValue, _ := tp.effectiveAttrValue(node, "src")
	image, s, err := tp.loadImageFromSrc(effectiveAttrValue)
	if err != nil {
		return nil, tp.nodeError(node, "could not load image `%s`: %v", effectiveAttrValue, err)
	}
	var dctEncoder core.StreamEncoder
	if s == ".jpg" || s == ".jpeg" {
		dctEncoder = core.NewDCTEncoder()
	} else {
		dctEncoder = core.NewFlateEncoder()
	}
	image.SetEncoder(dctEncoder)
	if err := tp.applyAttrs(node, image, imageAttrs); err != nil {
		return nil, err
	}
	return image, nil
}

// SetFillColor sets background color for border.
func (bo *border) SetFillColor(col Color) { bo.fillColor = col }
func newRectangle(x, y, width, height float64) *Rectangle {
	return &Rectangle{x: x, y: y, width: width, height: height, positioning: PositionAbsolute, fillOpacity: 1.0, borderColor: ColorBlack, borderWidth: 1.0, borderOpacity: 1.0}
}

// SetWidth sets the width of the ellipse.
func (e *Ellipse) SetWidth(width float64) { e.width = width }

// Height returns the current page height.
func (c *Creator) Height() float64 { return c.pageHeight }

// SetLevel sets the indentation level of the TOC line.
func (tl *TOCLine) SetLevel(level uint) {
	tl.level = level
	tl.paragraph.margins.Left = tl.left + float64(tl.level-1)*tl.levelOffset
}

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (rs *RadialShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingPatternResource(block, rs.ToPdfShadingPattern().ToPdfObject())
}
func (rs *RadialShading) shadingModel() *model.PdfShadingType3 {
	r, g, b := rs.base.backgroundColor.ToRGB()
	var point contentdraw.Point
	switch rs.anchor {
	case AnchorBottomLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly}
	case AnchorBottomRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Ury - rs.boundingBox.Height()}
	case AnchorTopLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()}
	case AnchorTopRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Ury}
	case AnchorLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()/2}
	case AnchorTop:
		point = contentdraw.Point{X: rs.boundingBox.Llx + rs.boundingBox.Width()/2, Y: rs.boundingBox.Ury}
	case AnchorRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()/2}
	case AnchorBottom:
		point = contentdraw.Point{X: rs.boundingBox.Urx + rs.boundingBox.Width()/2, Y: rs.boundingBox.Lly}
	default:
		point = contentdraw.NewPoint(rs.boundingBox.Llx+rs.boundingBox.Width()/2, rs.boundingBox.Lly+rs.boundingBox.Height()/2)
	}
	r0 := rs.r0
	r1 := rs.r1
	x := point.X + rs.x0
	y := point.Y + rs.y0
	if r0 == -1.0 {
		r0 = 0.0
	}
	if r1 == -1.0 {
		var floats []float64
		x2 := x - rs.boundingBox.Llx
		urx := rs.boundingBox.Urx - x
		y2 := y - rs.boundingBox.Lly
		x := x2*x2 + y2*y2
		floats = append(floats, math.Abs(x))
		y3 := rs.boundingBox.Lly + rs.boundingBox.Height() - y
		x3 := x2*x2 + y3*y3
		floats = append(floats, math.Abs(x3))
		y4 := y - rs.boundingBox.Lly
		x4 := urx*urx + y4*y4
		floats = append(floats, math.Abs(x4))
		ury := rs.boundingBox.Ury - y
		x5 := urx*urx + ury*ury
		floats = append(floats, math.Abs(x5))
		sort.Slice(floats, func(i, j int) bool { return floats[i] > floats[j] })
		r1 = math.Sqrt(floats[0])
	}
	pdfShadingType3 := model.NewPdfShadingType3()
	pdfShadingType3.ShadingType = core.MakeInteger(3)
	pdfShadingType3.ColorSpace = model.NewPdfColorspaceDeviceRGB()
	pdfShadingType3.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingType3.AntiAlias = core.MakeBool(rs.base.antiAlias)
	pdfShadingType3.Coords = core.MakeArrayFromFloats([]float64{x, y, r0, x, y, r1})
	pdfShadingType3.Domain = core.MakeArrayFromFloats([]float64{0.0, 1.0})
	pdfShadingType3.Extend = core.MakeArray(core.MakeBool(rs.base.extends[0]), core.MakeBool(rs.base.extends[1]))
	pdfShadingType3.Function = rs.base.generatePdfFunctions()
	return pdfShadingType3
}
func readPagesFromFile(r *os.File) ([]*model.PdfPage, error) {
	pdfReader, err := model.NewPdfReader(r)
	if err != nil {
		return nil, err
	}
	numPages, err := pdfReader.GetNumPages()
	if err != nil {
		return nil, err
	}
	var pages []*model.PdfPage
	for i := 0; i < numPages; i++ {
		page, err := pdfReader.GetPage(i + 1)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}

const (
	FitModeNone FitMode = iota
	FitModeFillWidth
)

func newImageFromBytes(b []byte) (*Image, error) {
	reader := bytes.NewReader(b)
	img, err := model.ImageHandling.Read(reader)
	if err != nil {
		common.Log.Error("Error loading image: %s", err)
		return nil, err
	}
	return newImage(img)
}

// SetMargins sets the margins of the chart component.
func (cha *Chart) SetMargins(left, right, top, bottom float64) {
	cha.margins.Left = left
	cha.margins.Right = right
	cha.margins.Top = top
	cha.margins.Bottom = bottom
}

// SetLineStyle sets the style for all the line components: number, title,
// separator, page. The style is applied only for new lines added to the
// TOC component.
func (to *TOC) SetLineStyle(style TextStyle) {
	to.SetLineNumberStyle(style)
	to.SetLineTitleStyle(style)
	to.SetLineSeparatorStyle(style)
	to.SetLinePageStyle(style)
}
func (tl *TOCLine) prepareParagraph(para *StyledParagraph, ctx DrawContext) {
	text := tl.Title.Text
	if tl.Number.Text != "" {
		text = " " + text
	}
	text += " "
	text2 := tl.Page.Text
	if text2 != "" {
		text2 = " " + text2
	}
	annotations := []*model.PdfAnnotation{}
	lineLink := tl.getLineLink()
	if lineLink != nil {
		annotations = append(annotations, lineLink)
	}
	para.chunks = []*TextChunk{{Text: tl.Number.Text, Style: tl.Number.Style, annotations: annotations}, {Text: text, Style: tl.Title.Style, annotations: annotations}, {Text: text2, Style: tl.Page.Style, annotations: annotations}}
	para.wrapText()
	linesLen := len(para.lines)
	if linesLen == 0 {
		return
	}
	value := ctx.Width*1000 - para.getTextLineWidth(para.lines[linesLen-1])
	textLineWidth := para.getTextLineWidth([]*TextChunk{&tl.Separator})
	count := int(value / textLineWidth)
	text3 := strings.Repeat(tl.Separator.Text, count)
	style := tl.Separator.Style
	chunk := para.Insert(2, text3)
	chunk.Style = style
	chunk.SetAnnotation(tl.getLineLink())
	value = value - float64(count)*textLineWidth
	if value > 500 {
		runeMetrics, ok := style.Font.GetRuneMetrics(' ')
		if ok && value > runeMetrics.Wx {
			wx := int(value / runeMetrics.Wx)
			if wx > 0 {
				style2 := style
				style2.FontSize = 1
				chunk = para.Insert(2, strings.Repeat(" ", wx))
				chunk.Style = style2
				chunk.SetAnnotation(tl.getLineLink())
			}
		}
	}
}

// Background contains properties related to the background of a component.
type Background struct {
	FillColor               Color
	BorderColor             Color
	BorderSize              float64
	BorderRadiusTopLeft     float64
	BorderRadiusTopRight    float64
	BorderRadiusBottomLeft  float64
	BorderRadiusBottomRight float64
}

func (im *Image) setMask(mask *imageMask) error {
	if im.mask != nil && im.mask.kind != mask.kind {
		return errors.New("image already has a mask of a different type")
	}
	im.mask = mask
	im.xobj = nil
	return nil
}

// Add adds a new line with the default style to the table of contents.
func (to *TOC) Add(number, title, page string, level uint) *TOCLine {
	line := to.AddLine(newTOCLineFromChunks(TextChunk{Text: number, Style: to.lineNumberStyle}, TextChunk{Text: title, Style: to.lineTitleStyle}, TextChunk{Text: page, Style: to.linePageStyle}, level, to.defaultStyle))
	if line == nil {
		return nil
	}
	lineMargins := &to.lineMargins
	line.SetMargins(lineMargins.Left, lineMargins.Right, lineMargins.Top, lineMargins.Bottom)
	line.SetLevelOffset(to.lineLevelOffset)
	line.Separator.Text = to.lineSeparator
	line.Separator.Style = to.lineSeparatorStyle
	return line
}
func newChart(chart chartrender.ChartRenderable) *Chart {
	return &Chart{chart: chart, positioning: PositionRelative, margins: Margins{Top: 10, Bottom: 10}, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}
}
func validateSetter(s string, i interface{}, n int, types ...reflect.Type) reflect.Value {
	valueOf := reflect.ValueOf(i)
	if !valueOf.IsValid() || valueOf.Kind() != reflect.Func {
		panic(fmt.Sprintf("creator: %s requires a setter function, got %T", s, i))
	}
	typ := valueOf.Type()
	if typ.IsVariadic() || typ.NumIn() != len(types)+1 {
		panic(fmt.Sprintf("creator: %s setter must take a component and %d value(s), got %s", s, len(types), typ))
	}
	if typ.NumOut() != n {
		panic(fmt.Sprintf("creator: %s setter must return %d value(s), got %s", s, n, typ))
	}
	for i, typ2 := range types {
		if in := typ.In(i + 1); in != typ2 {
			panic(fmt.Sprintf("creator: %s setter value %d must be %s, got %s in %s", s, i+1, typ2, in, typ))
		}
	}
	if n == 1 && typ.Out(0) != errorIfaceType {
		panic(fmt.Sprintf("creator: %s setter must return error, got %s", s, typ))
	}
	return valueOf
}

// Width returns the width of the rectangle.
// NOTE: the returned value does not include the border width of the rectangle.
func (r *Rectangle) Width() float64 { return r.width }
func (tp *templateProcessor) parseTextChunk(node *templateNode, textChunk *TextChunk) (interface{}, error) {
	if node.parent == nil {
		tp.nodeLogError(node, "Text chunk parent cannot be nil.")
		return nil, errInvalidParent
	}
	var (
		textStyle = tp.creator.NewTextStyle()
		ok2       bool
		ok3       bool
	)
	effectiveAttrValue, ok := tp.effectiveAttrValue(node, "link")
	if ok {
		componentParent := node.componentParent()
		para, ok := templateNodeValue(componentParent).(*StyledParagraph)
		if !ok {
			if tp.options.Strict {
				return nil, tp.nodeError(node, "link attribute is only valid on a paragraph text chunk")
			}
			tp.nodeLogError(node, "Link attribute only applicable to paragraph's text chunk.")
			ok2 = true
		} else {
			textStyle = para.linkStyle
			ok3 = true
		}
	}
	if textChunk == nil {
		textChunk = NewTextChunk("", textStyle)
	}
	if err := tp.applyAttrs(node, textChunk, textChunkAttrs); err != nil {
		return nil, err
	}
	if ok3 {
		if _, ok := tp.nodeOwnStyle(node)["color"]; !ok {
			textChunk.Style.Color = textStyle.Color
		}
	}
	if ok && !ok2 {
		link, err := tp.resolveLink(effectiveAttrValue)
		if err != nil {
			if err2 := tp.strictErr(node, "link", effectiveAttrValue, err); err2 != nil {
				return nil, err2
			}
		}
		if link != nil {
			textChunk.AddAnnotation(link)
		}
	}
	return textChunk, nil
}

// SetAddressHeadingStyle sets the style properties used to render the
// heading of the invoice address sections.
func (i *Invoice) SetAddressHeadingStyle(style TextStyle) { i.addressHeadingStyle = style }

// PageFinalize sets a function to be called for each page before finalization
// (i.e. the last stage of page processing before they get written out).
// The callback function allows final touch-ups for each page, and it
// provides information that might not be known at other stages of designing
// the document (e.g. the total number of pages). Unlike the header/footer
// functions, which are limited to the top/bottom margins of the page, the
// finalize function can be used draw components anywhere on the current page.
func (c *Creator) PageFinalize(pageFinalizeFunc func(pageFinalizeFunctionArgs PageFinalizeFunctionArgs) error) {
	c.pageFinalizeFunc = pageFinalizeFunc
}

// GetColumnWidths returns the column width fractions of the table. The
// fractions add up to 1. The returned slice references the table's internal
// column widths; modifying its elements modifies the table.
func (t *Table) GetColumnWidths() []float64 { return t.colWidths }
func (tp *templateProcessor) resolveFont(s string) (*model.PdfFont, error) {
	if font, ok := tp.fontCache[s]; ok {
		return font, nil
	}
	fontUncached, err := tp.resolveFontUncached(s)
	if err == nil {
		if tp.fontCache == nil {
			tp.fontCache = map[string]*model.PdfFont{}
		}
		tp.fontCache[s] = fontUncached
	}
	return fontUncached, err
}

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

var textContentTags = map[string]struct{}{"paragraph": struct{}{}, "text-chunk": struct{}{}, "chapter-heading": struct{}{}, "list-marker": struct{}{}}

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
func parseTableCellTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
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

func (pat *ParsedTemplate) validateWith(creator *Creator, renderer componentRenderer, data interface{}) error {
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

// GeneratePageBlocks draws the composite curve polygon on a new block
// representing the page. Implements the Drawable interface.
func (cp *CurvePolygon) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(cp.fillOpacity, cp.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	polygon := cp.polygon
	polygon.FillEnabled = polygon.FillColor != nil
	polygon.BorderEnabled = polygon.BorderColor != nil && polygon.BorderWidth > 0
	var (
		pageHeight = ctx.PageHeight
		rings      = polygon.Rings
		rows       = make([][]contentdraw.CubicBezierCurve, 0, len(polygon.Rings))
	)
	rect := model.PdfRectangle{}
	if len(rings) > 0 && len(rings[0]) > 0 {
		cubicBezierCurve := rings[0][0]
		cubicBezierCurve.P0.Y = pageHeight - cubicBezierCurve.P0.Y
		cubicBezierCurve.P1.Y = pageHeight - cubicBezierCurve.P1.Y
		cubicBezierCurve.P2.Y = pageHeight - cubicBezierCurve.P2.Y
		cubicBezierCurve.P3.Y = pageHeight - cubicBezierCurve.P3.Y
		rect = cubicBezierCurve.GetBounds()
	}
	for _, ring := range rings {
		cubicBezierCurves := make([]contentdraw.CubicBezierCurve, 0, len(ring))
		for _, cubicBezierCurve := range ring {
			cubicBezierCurve2 := cubicBezierCurve
			cubicBezierCurve2.P0.Y = pageHeight - cubicBezierCurve2.P0.Y
			cubicBezierCurve2.P1.Y = pageHeight - cubicBezierCurve2.P1.Y
			cubicBezierCurve2.P2.Y = pageHeight - cubicBezierCurve2.P2.Y
			cubicBezierCurve2.P3.Y = pageHeight - cubicBezierCurve2.P3.Y
			cubicBezierCurves = append(cubicBezierCurves, cubicBezierCurve2)
			bounds := cubicBezierCurve2.GetBounds()
			rect.Llx = math.Min(rect.Llx, bounds.Llx)
			rect.Lly = math.Min(rect.Lly, bounds.Lly)
			rect.Urx = math.Max(rect.Urx, bounds.Urx)
			rect.Ury = math.Max(rect.Ury, bounds.Ury)
		}
		rows = append(rows, cubicBezierCurves)
	}
	polygon.Rings = rows
	defer func() { polygon.Rings = rings }()
	if polygon.FillEnabled {
		err := applyShapeColor(block, cp.polygon.FillColor, cp.fillColor, func() Rectangle {
			return Rectangle{x: rect.Llx, y: rect.Lly, width: rect.Width(), height: rect.Height()}
		})
		if err != nil {
			return nil, ctx, err
		}
	}
	markedDraw, _, err := polygon.MarkedDraw(gsName, cp.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// Height returns 0. A page break has no height; the method exists so PageBreak
// satisfies the VectorDrawable interface and can be added to a Division.
func (pb *PageBreak) Height() float64 { return 0 }

// Path is a collection of all the subpaths in 'd' attribute.
type Path struct{ Subpaths []*Subpath }

// SetStencilMask sets a 1-bit stencil mask (/ImageMask true XObject on /Mask)
// for the image: white areas of the mask image mark areas of the base image
// that are painted, black areas are masked out (PDF 32000-1:2008 8.9.6).
// Stencil masks are binary — for partial transparency use SetSoftMask, but a
// stencil stores 1 bit per pixel instead of 8 and is preferred when the mask
// is binary anyway.
//
// The optional threshold selects the grayscale luminance (0-255) at or above
// which a mask pixel counts as white; when omitted, a histogram-based auto
// threshold is used. A mask that is already 1 bit per component is used
// as-is, with set bits marking painted areas.
//
// The mask replaces any SMask derived automatically from the base image's
// alpha channel. An image can carry a single mask: setting a mask of a
// different type than a previously set one returns an error.
func (im *Image) SetStencilMask(mask *model.Image, threshold ...uint8) error {
	if mask == nil {
		return errors.New("mask image is nil")
	}
	if len(threshold) > 1 {
		return fmt.Errorf("expected at most one threshold value, got %d", len(threshold))
	}
	imageMask2 := &imageMask{kind: imageMaskStencil, img: mask}
	if len(threshold) == 1 {
		threshold2 := threshold[0]
		imageMask2.threshold = &threshold2
	}
	return im.setMask(imageMask2)
}

// SetFont sets the font for the paragraph.
func (sp *StyledParagraph) SetFont(font *model.PdfFont) {
	sp.defaultStyle.Font = font
	for _, chunk := range sp.chunks {
		chunk.Style.Font = font
	}
}

// GetMargins returns the left, right, top, bottom Margins.
func (t *Table) GetMargins() (float64, float64, float64, float64) {
	return t.margins.Left, t.margins.Right, t.margins.Top, t.margins.Bottom
}

// ScaleToWidth scales the rectangle to the specified width. The height of
// the rectangle is scaled so that the aspect ratio is maintained.
func (r *Rectangle) ScaleToWidth(w float64) {
	height := r.height / r.width
	r.width = w
	r.height = w * height
}
func (tp *templateProcessor) nodeLogDebug(node *templateNode, format string, a ...interface{}) {
	common.Log.Debug(tp.getNodeErrorLocation(node, format, a...))
}
func (tp *templateProcessor) nodeLogError(node *templateNode, format string, a ...interface{}) {
	common.Log.Error(tp.getNodeErrorLocation(node, format, a...))
}

// GetBorderLineStyle returns the cell's border line style.
func (tc *TableCell) GetBorderLineStyle() contentdraw.LineStyle { return tc.borderLineStyle }

// Height returns the height of the division, assuming all components are
// stacked on top of each other.
func (d *Division) Height() float64 {
	var height float64
	for _, component := range d.components {
		switch component2 := component.(type) {
		case marginDrawable:
			_, _, top, bottom := component2.GetMargins()
			height += component2.Height() + top + bottom
		default:
			height += component2.Height()
		}
	}
	return height
}

// NewStyledParagraph creates a new styled paragraph.
// Default attributes:
// Font: the creator default font (Helvetica, unless changed via SetDefaultFont)
// Font size: 10
// Encoding: the font's encoding (WinAnsiEncoding for the standard fonts)
// Wrap: enabled
// Text color: black
func (c *Creator) NewStyledParagraph() *StyledParagraph { return newStyledParagraph(c.NewTextStyle()) }

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

// SetRowPosition sets cell row position.
func (tc *TableCell) SetRowPosition(row int) { tc.row = row }

// SetNoteHeadingStyle sets the style properties used to render the heading
// of the invoice note sections.
func (i *Invoice) SetNoteHeadingStyle(style TextStyle) { i.noteHeadingStyle = style }

// Positioning returns the type of positioning the rectangle is set to use.
func (r *Rectangle) Positioning() Positioning { return r.positioning }

// This method is not supported by Block component and exists solely to satisfy the Drawable interface.
func (b *Block) GenerateKDict() (*model.KDict, error) { return nil, nil }
func (li *List) split(ctx2 DrawContext, value float64) (head, tail *List) {
	var (
		value2           float64
		listItems, items []*listItem
	)
	markerWidth := li.markerWidth()
	itemWidth := li.itemWidth(ctx2.Width, markerWidth)
	for i, item := range li.items {
		ctxHeight := item.ctxHeight(itemWidth)
		value2 += ctxHeight
		if value2 <= ctx2.Height {
			listItems = append(listItems, item)
			continue
		}
		height := ctx2.Height - (value2 - ctxHeight)
		var content, content2 VectorDrawable
		style := item.marker.Style
		switch content3 := item.content.(type) {
		case *List:
			ctx := ctx2
			ctx.Width = itemWidth
			ctx.Height = math.Floor(height)
			head, tail := content3.split(ctx, value-content3.margins.Vertical())
			if head != nil {
				content = head
			}
			if tail != nil {
				content2 = tail
			}
			style = content3.marker.Style
		case *StyledParagraph:
			ctx := ctx2
			ctx.Width = itemWidth
			ctx.Height = math.Floor(height - content3.margins.Vertical() - 0.5*content3.getTextHeight())
			if ctx.Height > 0 {
				head, tail, err := content3.split(ctx)
				if err == nil && head != nil && tail != nil {
					content, content2 = head, tail
				}
			}
		case *Division:
			ctx := ctx2
			ctx.Width = itemWidth
			ctx.Height = math.Floor(height - content3.margins.Vertical())
			if ctx.Height > 0 {
				head, tail := content3.split(ctx, value-content3.margins.Vertical())
				if head != nil {
					content = head
				}
				if tail != nil {
					content2 = tail
				}
			}
		case *Table:
			ctx := ctx2
			ctx.Width = itemWidth
			ctx.Height = height
			head, tail := content3.split(ctx, value)
			if head != nil {
				content = head
			}
			if tail != nil {
				content2 = tail
			}
		}
		if content == nil {
			items = li.items[i:]
			break
		}
		listItem := newListItem()
		listItem.marker = item.marker
		listItem.content = content
		listItems = append(listItems, listItem)
		if content2 != nil {
			fontSize := style.FontSize
			runeMetrics, ok := style.Font.GetRuneMetrics(' ')
			if ok {
				fontSize = style.FontSize * runeMetrics.Wx * style.horizontalScale() / 1000.0
			}
			var text string
			if fontSize > 0 {
				text = strings.Repeat(" ", int(markerWidth/fontSize))
			}
			listItem := newListItem()
			listItem.marker = *NewTextChunk(text, style)
			listItem.content = content2
			items = append(items, listItem)
		}
		items = append(items, li.items[i+1:]...)
		break
	}
	if len(listItems) > 0 {
		head = newList(li.textStyle)
		*head = *li
		head.items = listItems
	}
	if len(items) > 0 {
		tail = newList(li.textStyle)
		*tail = *li
		tail.items = items
	}
	return head, tail
}

// Subpath is a collection of Commands, beginning with moveto command and
// usually ending with closepath command.
type Subpath struct{ Commands []*Command }

// AddTag adds a tag to the list.
func (li *List) AddTag(rootKObj *model.KDict) {
	if li.tagInfo == nil {
		li.tagInfo = model.NewStructureTagInfo()
		li.tagInfo.StructureType = model.StructureTypeList
	}
	li.tagInfo.ApplyTag = true
	li.tagInfo.ParentKObj = rootKObj
}

// SetTextExpansion sets the text expansion for the text chunk.
func (tec *TextChunk) SetTextExpansion(text string) { tec.textExpansion = &text }

// BorderWidth returns the border width of the rectangle.
func (r *Rectangle) BorderWidth() float64 { return r.borderWidth }

// SetCoords sets the center coordinates of the ellipse.
func (e *Ellipse) SetCoords(xc, yc float64) { e.xc = xc; e.yc = yc }

// MoveY moves the drawing context to absolute position y.
func (c *Creator) MoveY(y float64) { c.context.Y = y }

// GetOpacity returns the cell's opacity in the range 0-1.
func (tc *TableCell) GetOpacity() float64 { return tc.opacity }
func colorAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("colorAttr", i, 0, colorType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		color, err := proc.resolveColor(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(color))
		return nil
	}
}

// NewFilledCurve returns a instance of filled curve.
func (c *Creator) NewFilledCurve() *FilledCurve { return newFilledCurve() }
func newFontMetrics(font *model.PdfFont, value float64) *fontMetrics {
	fontMetrics2 := &fontMetrics{}
	if font == nil {
		common.Log.Debug("WARN: font is nil")
		return fontMetrics2
	}
	fontDescriptor, err := font.GetFontDescriptor()
	if err != nil {
		common.Log.Debug("ERROR: Unable to get font descriptor: %v", err)
		return fontMetrics2
	}
	if fontMetrics2.capHeight, err = fontDescriptor.GetCapHeight(); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Unable to get font cap height: %v", err)
		}
	}
	if int(fontMetrics2.capHeight) <= 0 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Cap Height not available - setting to 1000")
		}
		fontMetrics2.capHeight = 1000
	}
	fontMetrics2.capHeight *= value / 1000.0
	if fontMetrics2.xHeight, err = fontDescriptor.GetXHeight(); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Unable to get font x-height: %v", err)
		}
	}
	fontMetrics2.xHeight *= value / 1000.0
	if fontMetrics2.ascent, err = fontDescriptor.GetAscent(); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Unable to get font ascent: %v", err)
		}
	}
	fontMetrics2.ascent *= value / 1000.0
	if fontMetrics2.descent, err = fontDescriptor.GetDescent(); err != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("WARN: Unable to get font descent: %v", err)
		}
	}
	fontMetrics2.descent *= value / 1000.0
	return fontMetrics2
}

// SetDropCapsWithStyle is a convenience method to set drop caps with a custom style.
// This allows you to customize the appearance of the drop cap (color, font, etc.)
// while specifying the basic drop caps parameters.
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This is a paragraph with a styled drop cap...")
//	style := creator.TextStyle{
//	    Font:  boldFont,
//	    Color: creator.ColorRGBFrom8bit(200, 0, 0), // Red
//	}
//	p.SetDropCapsWithStyle(creator.DropCapsDrop, creator.DropCapsFirstCharacter, 3, 5.0, &style)
func (sp *StyledParagraph) SetDropCapsWithStyle(dropType DropCapsType, scope DropCapsScope, numLines int, gap float64, style *TextStyle) {
	sp.SetDropCaps(DropCapsOptions{Type: dropType, Scope: scope, NumLines: numLines, Gap: gap, Style: style})
}
func floatArrayAttrErr(i interface{}) attrApplyFunc {
	value2 := validateSetter("floatArrayAttrErr", i, 1, float64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		floatArray, err := parseFloatArray(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		values := callSetter(value2, target, reflect.ValueOf(floatArray))
		if values == nil {
			return nil
		}
		if err, _ := values[0].Interface().(error); err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		return nil
	}
}

// GeneratePageBlocks generates the page blocks for the Division component.
// Multiple blocks are generated if the contents wrap over multiple pages.
func (d *Division) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks     []*Block
		ok2        bool
		err        error
		isRelative = d.positioning.IsRelative()
		top        = d.margins.Top
		ok3        = d.tagInfo != nil && d.tagInfo.ApplyTag
		mcid       int64
		m          map[int]float64
		m2         map[int]bool
	)
	if ok3 {
		mcid = d.tagInfo.Mcid
	}
	if isRelative && !d.enableBreak && !d.inline {
		ctxHeight := d.ctxHeight(ctx.Width)
		if ctxHeight > ctx.Height-d.margins.Top && ctxHeight <= ctx.PageHeight-ctx.Margins.Top-ctx.Margins.Bottom {
			if blocks, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			ok2 = true
			top = 0
		}
	}
	ctx3 := ctx
	ctx2 := ctx
	if isRelative {
		ctx.X += d.margins.Left
		ctx.Y += top
		ctx.Width -= d.margins.Left + d.margins.Right
		ctx.Height -= top
		ctx2 = ctx
		ctx.X += d.padding.Left
		ctx.Y += d.padding.Top
		ctx.Width -= d.padding.Left + d.padding.Right
		ctx.Height -= d.padding.Top
		ctx.Margins.Top += d.padding.Top
		ctx.Margins.Bottom += d.padding.Bottom
		ctx.Margins.Left += d.margins.Left + d.padding.Left
		ctx.Margins.Right += d.margins.Right + d.padding.Right
	}
	ctx.Inline = d.inline
	ctx4 := ctx
	ctx5 := ctx
	var height float64
	for _, component := range d.components {
		if _, ok := component.(*PageBreak); ok {
			if ctx.Inline {
				continue
			}
			if len(blocks) > 0 {
				if m == nil {
					m = map[int]float64{}
					m2 = map[int]bool{}
				}
				n := len(blocks) - 1
				m[n] = ctx.Y
				m2[n+1] = true
			}
		}
		if ctx.Inline {
			if (ctx.X-ctx4.X)+component.Width() <= ctx.Width {
				ctx.Y = ctx5.Y
				ctx.Height = ctx5.Height
			} else {
				ctx.X = ctx4.X
				ctx.Width = ctx4.Width
				ctx5.Y += height
				ctx5.Height -= height
				height = 0
			}
		}
		_, ok := component.(*StyledParagraph)
		structureTypeParagraph := model.StructureTypeParagraph
		if ok3 && ok {
			component.SetMarkedContentID(mcid)
			component.SetStructureType(structureTypeParagraph)
		}
		pageBlocks, ctx2, err := component.GeneratePageBlocks(ctx)
		if err != nil {
			common.Log.Debug("Error generating page blocks: %v", err)
			return nil, ctx, err
		}
		if ok3 && ok {
			page := int64(ctx2.Page)
			component.SetStructPageNumber(&page)
			kDict, err := component.GenerateKDict()
			if err != nil {
				return nil, ctx, err
			}
			d.tagInfo.ComponentKObj.AddKChild(kDict)
			mcid++
		}
		if len(pageBlocks) < 1 {
			continue
		}
		if len(blocks) > 0 {
			blocks[len(blocks)-1].mergeBlocks(pageBlocks[0])
			blocks = append(blocks, pageBlocks[1:]...)
		} else {
			if contents := pageBlocks[0].contents; contents == nil || len(*contents) == 0 {
				ok2 = true
			}
			blocks = append(blocks, pageBlocks[0:]...)
		}
		y := 0.0
		switch component2 := component.(type) {
		case *StyledParagraph:
			y = (0.5 * component2.getTextHeight())
		}
		ctx2.Y += y
		ctx2.Height -= y
		if ctx.Inline {
			if ctx.Page != ctx2.Page {
				ctx4.Y = ctx.Margins.Top
				ctx4.Height = ctx.PageHeight - ctx.Margins.Top
				ctx5.Y = ctx4.Y
				ctx5.Height = ctx4.Height
				height = ctx2.Height - ctx4.Height
			} else {
				if height2 := ctx.Height - ctx2.Height; height2 > height {
					height = height2
				}
			}
		} else {
			ctx2.X = ctx.X
		}
		ctx = ctx2
	}
	if len(d.components) == 0 {
		block := NewBlock(ctx.Width, 0)
		blocks = append(blocks, block)
	}
	ctx.Inline = ctx3.Inline
	ctx.Margins = ctx3.Margins
	if isRelative {
		ctx.X = ctx3.X
		ctx.Width = ctx3.Width
		ctx.Y += d.padding.Bottom
		ctx.Height -= d.padding.Bottom
	}
	if d.background != nil {
		blocks, err = d.drawBackground(blocks, ctx2, ctx, ok2, m, m2)
		if err != nil {
			return nil, ctx, err
		}
	}
	if d.positioning.IsAbsolute() {
		return blocks, ctx3, nil
	}
	ctx.Y += d.margins.Bottom
	ctx.Height -= d.margins.Bottom
	return blocks, ctx, nil
}

// GetVerticalAlignment returns the cell's vertical alignment of content.
func (tc *TableCell) GetVerticalAlignment() CellVerticalAlignment { return tc.verticalAlignment }
func chunksFontMetrics(chunks []*TextChunk) (maxCapHeight, maxFontSize, minDescent float64) {
	if len(chunks) == 0 {
		return 0, 0, 0
	}
	for _, chunk := range chunks {
		fontMetrics := newFontMetrics(chunk.Style.Font, chunk.Style.FontSize)
		if fontMetrics.capHeight > maxCapHeight {
			maxCapHeight = fontMetrics.capHeight
		}
		if fontMetrics.descent < minDescent {
			minDescent = fontMetrics.descent
		}
		if fontSize := chunk.Style.FontSize; fontSize > maxFontSize {
			maxFontSize = fontSize
		}
	}
	return maxCapHeight, maxFontSize, minDescent
}

// SetColumnWidths sets the fractional column widths.
// Each width should be in the range 0-1 and is a fraction of the table width.
// The number of width inputs must match number of columns, otherwise an error is returned.
func (t *Table) SetColumnWidths(widths ...float64) error {
	if len(widths) != t.cols {
		common.Log.Debug("Mismatching number of widths and columns")
		return errors.New("range check error")
	}
	t.colWidths = widths
	return nil
}

// GeneratePageBlocks generates the table page blocks. Multiple blocks are
// generated if the contents wrap over multiple pages.
// Implements the Drawable interface.
func (t *Table) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	table := t
	if t.enableRowWrap {
		table = t.clone()
	}
	return generateTablePageBlocks(table, ctx)
}
func parseStyledParagraphTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseStyledParagraph(node)
}

// GetRowHeight returns the height of the specified row.
func (t *Table) GetRowHeight(row int) (float64, error) {
	if row < 1 || row > len(t.rowHeights) {
		return 0, errors.New("range check error")
	}
	return t.rowHeights[row-1], nil
}

// SetBoundingBox set gradient color bounding box where the gradient would be rendered.
func (fbs *FunctionBasedShading) SetBoundingBox(x, y, width, height float64) {
	fbs.boundingBox = &model.PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

// SetDate sets the date of the invoice.
func (i *Invoice) SetDate(date string) (*InvoiceCell, *InvoiceCell) {
	i.date[1].Value = date
	return i.date[0], i.date[1]
}

// GetIndent get the cell's left indent.
func (gc *GridCell) GetIndent() float64 { return gc.indent }

const (
	CellBorderStyleNone CellBorderStyle = iota
	CellBorderStyleSingle
	CellBorderStyleDouble
)

// ScaleToHeight scales the Block to a specified height, maintaining the same aspect ratio.
func (b *Block) ScaleToHeight(h float64) { sx := h / b.height; b.Scale(sx, sx) }
func (tc *TableCell) height(width float64) float64 {
	var bottom float64
	switch content := tc.content.(type) {
	case *StyledParagraph:
		if content.enableWrap {
			content.SetWidth(width - tc.indent - content.margins.Left - content.margins.Right)
		}
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
		if !tc.table.compactMode {
			bottom += (0.5 * content.getTextHeight())
		}
	case *Image:
		content.applyFitMode(width - tc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Table:
		content.updateRowHeights(width - tc.indent - content.margins.Left - content.margins.Right)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *List:
		bottom = content.ctxHeight(width-tc.indent) + content.margins.Top + content.margins.Bottom
	case *Division:
		bottom = content.ctxHeight(width-tc.indent) + content.margins.Top + content.margins.Bottom + content.padding.Top + content.padding.Bottom
	case *Chart:
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Rectangle:
		content.applyFitMode(width - tc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom + content.borderWidth
	case *Ellipse:
		content.applyFitMode(width - tc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Line:
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	}
	return bottom
}
func alphaToBitmask(img *model.Image) (*model.Image, bool) {
	var n int
	switch img.BitsPerComponent {
	case 8:
		n = 1
	case 16:
		n = 2
	default:
		return nil, false
	}
	alphaData := img.AlphaData()
	width, height := int(img.Width), int(img.Height)
	if len(alphaData) == 0 || len(alphaData) < width*height*n {
		return nil, false
	}
	n2 := (width + 7) / 8
	data := make([]byte, n2*height)
	n3 := 0
	for i := 0; i < height; i++ {
		i2 := i * n2
		for i := 0; i < width; i++ {
			b := alphaData[n3]
			if b != 0x00 && b != 0xff {
				return nil, false
			}
			if n == 2 && alphaData[n3+1] != b {
				return nil, false
			}
			if b == 0xff {
				data[i2+i/8] |= 0x80 >> uint(i%8)
			}
			n3 += n
		}
	}
	return &model.Image{Width: img.Width, Height: img.Height, BitsPerComponent: 1, ColorComponents: 1, Data: data}, true
}

// SetRowHeight sets the height for a specified row.
func (t *Table) SetRowHeight(row int, h float64) error {
	if row < 1 || row > len(t.rowHeights) {
		return errors.New("range check error")
	}
	t.rowHeights[row-1] = h
	return nil
}
func (gr *GridRow) updateRowHeight(totalWidth float64) {
	gr.contentHeight = gr.height
	for _, cell := range gr.cells {
		width := cell.width(gr.grid.colWidths, totalWidth)
		height := cell.height(width)
		if height > gr.contentHeight {
			gr.contentHeight = height
		}
	}
}

// NewTOCLine creates a new table of contents line with the default style.
func (c *Creator) NewTOCLine(number, title, page string, level uint) *TOCLine {
	return newTOCLineFromStrings(number, title, page, level, c.NewTextStyle())
}

// SetOpacity sets the opacity of the line (0-1).
func (l *Line) SetOpacity(opacity float64) { l.opacity = opacity }

// PageSize represents the page size as a 2 element array representing the width and height in PDF document units (points).
type PageSize [2]float64

func enumAttr(i interface{}, i2 interface{}) attrApplyFunc {
	valueOf := reflect.ValueOf(i)
	if !valueOf.IsValid() || valueOf.Kind() != reflect.Map || valueOf.Type().Key() != stringType {
		panic(fmt.Sprintf("creator: enumAttr requires a map[string]E value map, got %T", i))
	}
	elem := valueOf.Type().Elem()
	value2 := validateSetter("enumAttr", i2, 0, elem)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		mapIndex := valueOf.MapIndex(reflect.ValueOf(value))
		if !mapIndex.IsValid() {
			if proc.options.Strict {
				return proc.nodeError(node, "invalid value %q for attribute %q", value, attrName)
			}
			mapIndex = reflect.Zero(elem)
		}
		callSetter(value2, target, mapIndex)
		return nil
	}
}

// Decode decodes the child elements of element.
func (gse *GraphicSVGElement) Decode(decoder *xml.Decoder) error {
	for {
		token, err := decoder.Token()
		if token == nil && err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch token2 := token.(type) {
		case xml.StartElement:
			graphicSVGElement := newGraphicSVGElement(token2)
			err := graphicSVGElement.Decode(decoder)
			if err != nil {
				return err
			}
			gse.Children = append(gse.Children, graphicSVGElement)
		case xml.CharData:
			s := strings.TrimSpace(string(token2))
			if s != "" {
				gse.Content = string(token2)
			}
		case xml.EndElement:
			if token2.Name.Local == gse.Name {
				return nil
			}
		}
	}
	return nil
}

// AddTag adds the table row tag for Grid to the root K object.
func (gr *GridRow) AddTag(rootKObj *model.KDict) {
	if rootKObj == nil {
		common.Log.Debug("Table: AddTag called with nil rootKObj, skipping tagging.")
		return
	}
	if gr.tagInfo == nil {
		gr.tagInfo = model.NewStructureTagInfo()
		gr.tagInfo.StructureType = model.StructureTypeTableRow
	}
	gr.tagInfo.ParentKObj = rootKObj
	gr.tagInfo.ComponentKObj = gr.tagInfo.NewComponentKObj(gr.tagInfo.StructureType)
	gr.tagInfo.ParentKObj.AddKChild(gr.tagInfo.ComponentKObj)
}

// EnablePageWrap controls whether the table is wrapped across pages.
// If disabled, the table is moved in its entirety on a new page, if it
// does not fit in the available height. By default, page wrapping is enabled.
// If the height of the table is larger than an entire page, wrapping is
// enabled automatically in order to avoid unwanted behavior.
func (t *Table) EnablePageWrap(enable bool) { t.enablePageWrap = enable }

// SetSubtotal sets the subtotal of the invoice.
func (i *Invoice) SetSubtotal(value string) { i.subtotal[1].Value = value }

// SetLineHeight sets the default line height for chunks in this paragraph
// (1.0 default). Individual chunks can override it via TextStyle.LineHeight.
func (sp *StyledParagraph) SetLineHeight(lineheight float64) { sp.lineHeight = lineheight }

// NewSubchapter creates a new child chapter with the specified title.
func (ch *Chapter) NewSubchapter(title string) *Chapter {
	style := defaultTextStyle(ch.heading.chunks[0].Style.Font)
	style.FontSize = 14
	ch.subchapterCount++
	chapter := newChapter(ch, ch.toc, ch.outline, ch.recordFunc, title, ch.subchapterCount, style)
	ch.Add(chapter)
	return chapter
}

// NewImageFromFile creates an Image from a file.
func (c *Creator) NewImageFromFile(path string) (*Image, error) { return newImageFromFile(path) }

// GetCoords returns the upper left corner coordinates of the rectangle (`x`, `y`).
func (r *Rectangle) GetCoords() (float64, float64) { return r.x, r.y }

// SetStyleLeft sets border style for left side.
func (bo *border) SetStyleLeft(style CellBorderStyle) { bo.styleLeft = style }
func templateMakeSeq(i2, i3, n int) []int {
	nums := []int{}
	for i := i2; i <= n; i += i3 {
		nums = append(nums, i)
	}
	return nums
}
func isKeycapEnclosing(r rune) bool { return r == 0x20E3 }

// TagComponents instructs Creator whether or not to tag any component added after calling this function.
func (c *Creator) TagComponents(enableTagging bool) {
	c.taggingEnabled = enableTagging
	if enableTagging {
		c.docKDict = model.NewKDictionary()
		c.docKDict.S = core.MakeName(string(model.StructureTypeDocument))
		c.docKDict.ID = core.MakeString(c.docKDict.GenerateRandomID())
		if c.structTreeRoot == nil {
			c.structTreeRoot = model.NewStructTreeRoot()
		}
		c.structTreeRoot.AddKDict(c.docKDict)
	}
}

// SetBorderColor sets the border color for the path.
func (fc *FilledCurve) SetBorderColor(color Color) { fc.borderColor = color }
func newTOCLineFromStrings(text, text2, text3 string, level uint, style TextStyle) *TOCLine {
	return newTOCLineFromChunks(TextChunk{Text: text, Style: style}, TextChunk{Text: text2, Style: style}, TextChunk{Text: text3, Style: style}, level, style)
}

// FillColor returns the fill color of the ellipse.
func (e *Ellipse) FillColor() Color { return e.fillColor }
func (i *Invoice) generateHeaderBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	styledParagraph := newStyledParagraph(i.titleStyle)
	styledParagraph.SetEnableWrap(true)
	styledParagraph.Append(i.title)
	table := newTable(2)
	if i.logo != nil {
		cell := table.NewCell()
		cell.SetHorizontalAlignment(CellHorizontalAlignmentLeft)
		cell.SetVerticalAlignment(CellVerticalAlignmentMiddle)
		cell.SetIndent(0)
		cell.SetContent(i.logo)
		i.logo.ScaleToHeight(styledParagraph.Height() + 20)
	} else {
		table.SkipCells(1)
	}
	cell := table.NewCell()
	cell.SetHorizontalAlignment(CellHorizontalAlignmentRight)
	cell.SetVerticalAlignment(CellVerticalAlignmentMiddle)
	cell.SetContent(styledParagraph)
	return table.GeneratePageBlocks(ctx)
}
func (ts *TextStyle) horizontalScale() float64 { return ts.HorizontalScaling / 100 }

// NewColumn returns a new column for the line items invoice table.
func (i *Invoice) NewColumn(description string) *InvoiceCell {
	return i.newColumn(description, CellHorizontalAlignmentLeft)
}

// SetSideBorderStyle sets the cell's side border style.
func (tc *TableCell) SetSideBorderStyle(side CellBorderSide, style CellBorderStyle) {
	switch side {
	case CellBorderSideAll:
		tc.borderStyleTop = style
		tc.borderStyleBottom = style
		tc.borderStyleLeft = style
		tc.borderStyleRight = style
	case CellBorderSideTop:
		tc.borderStyleTop = style
	case CellBorderSideBottom:
		tc.borderStyleBottom = style
	case CellBorderSideLeft:
		tc.borderStyleLeft = style
	case CellBorderSideRight:
		tc.borderStyleRight = style
	}
}

// AddShadingResource adds shading dictionary inside the resources dictionary.
func (fbs *FunctionBasedShading) AddShadingResource(block *Block) (name core.PdfObjectName, err error) {
	shadingModel, err := fbs.shadingModel()
	if err != nil {
		return "", err
	}
	return addShadingResource(block, shadingModel.ToPdfObject())
}
func (gse *GraphicSVGElement) drawText(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_BT()
	svgLength, err := parseSVGLength(gse.Attributes["x"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `x` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["y"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `y` value: %v", err.Error())
	}
	attribute := gse.Attributes["fill"]
	var r, g, b float64
	if rgba, ok := graphic2d.ColorMap[attribute]; ok {
		r2, g2, b2, _ := rgba.RGBA()
		r, g, b = float64(r2), float64(g2), float64(b2)
	} else if strings.HasPrefix(attribute, "rgb(") {
		r, g, b = parseRGBColor(attribute)
	} else {
		r, g, b = ColorRGBFromHex(attribute).ToRGB()
	}
	cc.Add_rg(r, g, b)
	value := defaultSVGFontSize
	if attribute, ok := gse.Attributes["font-size"]; ok {
		value, err = strconv.ParseFloat(attribute, 64)
		if err != nil {
			common.Log.Debug("Error while parsing `font-size` value: %v", err.Error())
			value = defaultSVGFontSize
		}
	}
	fontSize := gse.scale * value * PPI / svgDPI
	keyName := core.PdfObjectName("SFont")
	defaultFont := model.DefaultFont()
	attribute2, ok := gse.Attributes["font-family"]
	if ok {
		if svgStandardFont2, err := svgStandardFont(attribute2); err == nil {
			defaultFont = svgStandardFont2
			i := 1
			for resources.HasFontByName(keyName) {
				keyName = core.PdfObjectName("SFont" + strconv.Itoa(i))
				i++
			}
		}
	}
	value2 := 0.0
	attribute3, ok := gse.Attributes["text-anchor"]
	if ok && attribute3 != "start" {
		var wx float64
		for _, r := range gse.Content {
			runeMetrics, ok := defaultFont.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("ERROR: unsupported rune %v in font", r)
			}
			wx += runeMetrics.Wx
		}
		wx = wx * fontSize / 1000.0
		switch attribute3 {
		case "middle":
			value2 = -wx / 2
		case "end":
			value2 = -wx
		}
	}
	cc.Add_Tm(1, 0, 0, -1, svgLength*gse.scale+value2, svgLength2*gse.scale)
	resources.SetFontByName(keyName, defaultFont.ToPdfObject())
	cc.Add_Tf(keyName, fontSize)
	content := gse.Content
	str := core.MakeString(content)
	cc.Add_Tj(*str)
	cc.Add_ET()
}
func newTemplateProcessor(creator *Creator, filename string, data []byte, templateOptions *TemplateOptions, renderer componentRenderer) *templateProcessor {
	if templateOptions == nil {
		templateOptions = &TemplateOptions{}
	}
	templateOptions.init()
	if renderer == nil {
		renderer = creator
	}
	return &templateProcessor{creator: creator, data: data, options: templateOptions, renderer: renderer, filename: filename}
}

// GetPositioning returns the positioning mode (relative or absolute) of the
// table.
func (t *Table) GetPositioning() Positioning { return t.positioning }

// SetSection sets whether this row is a header row.
func (gr *GridRow) SetSection(role GridRowSection) { gr.section = role }
func (tp *templateProcessor) parseBackground(node *templateNode) (interface{}, error) {
	background := &Background{}
	if err := tp.applyAttrs(node, background, backgroundAttrs); err != nil {
		return nil, err
	}
	return background, nil
}
func parseDivisionTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseDivision(node)
}

// GeneratePageBlocks draws the curve onto page blocks.
func (cu *Curve) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	contentCreator := contentstream.NewContentCreator()
	if cu.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(cu.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(cu.tagInfo.Mcid)})
	}
	contentCreator.Add_q().Add_w(cu.width).SetStrokingColor(toPdfColor(cu.color)).Add_m(cu.x1, ctx.PageHeight-cu.y1).Add_v(cu.cx, ctx.PageHeight-cu.cy, cu.x2, ctx.PageHeight-cu.y2).Add_S().Add_Q()
	if cu.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	err := block.addContentsByString(contentCreator.String())
	if err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// RemoveArtifact removes the artifact associated with the block, if any.
func (b *Block) RemoveArtifact() { b.artifact = nil }
func (sp *StyledParagraph) getTextLineWidth(chunks []*TextChunk) float64 {
	var charSpacing float64
	n := len(chunks)
	for i, chunk := range chunks {
		style := &chunk.Style
		textLen := len(chunk.Text)
		for j, r := range chunk.Text {
			if r == '\u000A' {
				continue
			}
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("Rune char metrics not found! %v\n", r)
				return -1
			}
			charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale()
			if r != ' ' && (i != n-1 || j != textLen-1) {
				charSpacing += style.CharSpacing * 1000.0
			}
		}
	}
	return charSpacing
}

var PPMM = float64(72 * 1.0 / 25.4)

func (b *Block) mergeBlocks(block *Block) error {
	err := mergeContentStreams(b.contents, b.resources, block.contents, block.resources)
	if err != nil {
		return err
	}
	for _, annotation := range block.annotations {
		b.AddAnnotation(annotation)
	}
	return nil
}
func parseImageTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseImage(node)
}
func allFirstPartsEmpty(rows [][2]*Table) bool {
	for _, tables := range rows {
		if tables[0] != nil {
			return false
		}
	}
	return true
}
func parseSVGPath(s string) (*Path, error) {
	svgCommands = newPathCommands()
	commands, err := parseCommands(tokenizePathData(s))
	if err != nil {
		return nil, err
	}
	return buildPath(commands), nil
}

// Height returns the height of the chart.
func (cha *Chart) Height() float64 { return float64(cha.chart.Height()) }

// SetLineWidth sets the line width.
func (po *Polyline) SetLineWidth(lineWidth float64) { po.polyline.LineWidth = lineWidth }
func (tp *templateProcessor) loadImageFromSrc(s string) (*Image, string, error) {
	if s == "" {
		common.Log.Error("Image `src` attribute cannot be empty.")
		return nil, "", errInvalidResource
	}
	strs := strings.Split(s, ",")
	for _, s := range strs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		img, ok := tp.options.ImageMap[s]
		if ok {
			image, err := newImage(img)
			return image, "", err
		}
		if attrPropList := tp.parseAttrPropList(s); len(attrPropList) > 0 {
			if name, ok := attrPropList["path"]; ok {
				if image, err := newImageFromFile(name); err != nil {
					common.Log.Debug("Could not load image `%s`: %v.", name, err)
				} else {
					return image, strings.ToLower(filepath.Ext(name)), nil
				}
			}
		}
	}
	common.Log.Error("Could not find image resource: `%s`.", s)
	return nil, "", errInvalidResource
}

// SetMaxLines sets the maximum number of lines to be drawn.
func (sp *StyledParagraph) SetMaxLines(maxLines int) { sp.maxLines = maxLines }

// GetCoords returns the center coordinates of ellipse (`xc`, `yc`).
func (e *Ellipse) GetCoords() (float64, float64) { return e.xc, e.yc }
func parseTableTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseTable(node)
}

// SetHeight sets the height of the rectangle.
func (r *Rectangle) SetHeight(height float64) { r.height = height }
func (b *Block) setOpacity(val float64, val2 float64) (string, error) {
	if (val < 0 || val >= 1.0) && (val2 < 0 || val2 >= 1.0) {
		return "", nil
	}
	n := 0
	setOpacity := fmt.Sprintf("GS%d", n)
	for b.resources.HasExtGState(core.PdfObjectName(setOpacity)) {
		n++
		setOpacity = fmt.Sprintf("GS%d", n)
	}
	dict := core.MakeDict()
	if val >= 0 && val < 1.0 {
		dict.Set("ca", core.MakeFloat(val))
	}
	if val2 >= 0 && val2 < 1.0 {
		dict.Set("CA", core.MakeFloat(val2))
	}
	err := b.resources.AddExtGState(core.PdfObjectName(setOpacity), dict)
	if err != nil {
		return "", err
	}
	return setOpacity, nil
}

// GetTextVerticalAlignment returns the vertical alignment of the text within
// the bounds of the paragraph.
func (sp *StyledParagraph) GetTextVerticalAlignment() TextVerticalAlignment {
	return sp.verticalAlignment
}
func layoutCellContent(drawable VectorDrawable, left float64, top float64, n CellVerticalAlignment, ok bool, rootKObj *model.KDict, mcid int64, left2 float64) (float64, float64, float64, *model.KDict, bool, error) {
	var (
		ok2           bool
		kDict         *model.KDict
		maxCapHeight3 float64
		err           error
	)
	switch para := drawable.(type) {
	case *StyledParagraph:
		if para.enableWrap {
			left = para.getMaxLineWidth() / 1000.0
		}
		maxCapHeight, maxFontSize, minDescent := para.getLineMetrics(0)
		maxCapHeight2, maxFontSize2 := maxCapHeight*para.lineHeight, maxFontSize*para.lineHeight
		if para.verticalAlignment == TextVerticalAlignmentCenter {
			maxCapHeight3 = maxFontSize2 - (maxFontSize + (maxCapHeight+minDescent-maxFontSize)/2 + (maxFontSize2-maxFontSize)/2)
		}
		if len(para.lines) == 1 {
			top = maxCapHeight2
		} else {
			top = top - maxFontSize2 + maxCapHeight2
		}
		maxCapHeight3 += maxCapHeight2 - maxFontSize2
		switch n {
		case CellVerticalAlignmentTop:
			maxCapHeight3 += maxCapHeight2 * 0.5
		case CellVerticalAlignmentBottom:
			maxCapHeight3 -= maxCapHeight2 * 0.5
		}
		left += para.margins.Left + para.margins.Right
		top += para.margins.Top + para.margins.Bottom
		if rootKObj != nil {
			para.SetMarkedContentID(mcid)
			para.SetStructureType(model.StructureTypeParagraph)
			kDict, err = para.GenerateKDict()
			if err != nil {
				return 0, 0, 0, nil, false, fmt.Errorf("failed to generate K dictionary for styled paragraph: %w", err)
			}
		}
	case *Table:
		left = left2
		if rootKObj != nil {
			para.SetMarkedContentID(mcid)
			para.AddTag(rootKObj)
			ok2 = true
			kDict, err = para.GenerateKDict()
			if err != nil {
				return 0, 0, 0, nil, false, fmt.Errorf("failed to generate K dictionary for table: %w", err)
			}
		}
	case *List:
		left = left2
		if rootKObj != nil {
			para.SetMarkedContentID(mcid)
			para.AddTag(rootKObj)
			ok2 = true
			kDict, err = para.GenerateKDict()
			if err != nil {
				return 0, 0, 0, nil, false, fmt.Errorf("failed to generate K dictionary for list: %w", err)
			}
		}
	case *Division:
		left = left2
		if rootKObj != nil {
			para.SetMarkedContentID(mcid)
			para.AddTag(rootKObj)
			ok2 = true
			kDict, err = para.GenerateKDict()
			if err != nil {
				return 0, 0, 0, nil, false, fmt.Errorf("failed to generate K dictionary for division: %w", err)
			}
		}
	case *Chart:
		left = left2
	case *Line:
		top += para.margins.Top + para.margins.Bottom
		maxCapHeight3 -= para.Height() / 2
	case *Image:
		left += para.margins.Left + para.margins.Right
		top += para.margins.Top + para.margins.Bottom
	}
	return left, top, maxCapHeight3, kDict, ok2, nil
}
func (i *Invoice) GenerateKDict() (*model.KDict, error) { return nil, nil }

// ToPdfShadingPattern generates a new model.PdfShadingPatternType3 object.
func (rs *RadialShading) ToPdfShadingPattern() *model.PdfShadingPatternType3 {
	r, g, b := rs.base.backgroundColor.ToRGB()
	shadingModel := rs.shadingModel()
	shadingModel.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingPatternType3 := model.NewPdfShadingPatternType3()
	pdfShadingPatternType3.Shading = shadingModel
	return pdfShadingPatternType3
}
func (tp *templateProcessor) collectClasses(node *templateNode) error {
	if tp.options.Strict {
		for _, attr := range node.start.Attr {
			if attr.Name.Space == "" {
				return tp.nodeError(node, "<style> does not take attributes")
			}
		}
	}
	if err := tp.checkStyleText(node); err != nil {
		return err
	}
	for _, node2 := range node.children {
		if node2.start.Name.Local != classElementName {
			if tp.options.Strict {
				return tp.nodeError(node2, "only <class> is allowed inside <style>, found <%s>", node2.start.Name.Local)
			}
			tp.nodeLogDebug(node2, "Only <class> is allowed inside <style>. Skipping <%s>.", node2.start.Name.Local)
			continue
		}
		if tp.options.Strict && len(node2.children) > 0 {
			return tp.nodeError(node2, "<class> does not take child elements")
		}
		if err := tp.checkStyleText(node2); err != nil {
			return err
		}
		var (
			value string
			attrs []xml.Attr
		)
		for _, attr := range node2.start.Attr {
			if attr.Name.Space != "" {
				continue
			}
			if attr.Name.Local == "name" {
				value = attr.Value
				continue
			}
			attrs = append(attrs, attr)
		}
		if value == "" {
			if tp.options.Strict {
				return tp.nodeError(node2, "<class> requires a name")
			}
			tp.nodeLogDebug(node2, "Template <class> requires a name. Skipping.")
			continue
		}
		tp.classStyles[value] = attrs
	}
	return nil
}

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (rs *RadialShading) SetAntiAlias(enable bool) { rs.base.SetAntiAlias(enable) }

// Color returns the color of the line.
func (l *Line) Color() Color { return l.color }

// GraphicSVGElement represents SVG instances.
type GraphicSVGElement struct {

	// ViewBox represents viewBox value of element.
	ViewBox struct{ X, Y, W, H float64 }

	// Name of element.
	Name string

	// Attributes of element.
	Attributes map[string]string

	// Children elements.
	Children []*GraphicSVGElement

	// Content contains text value of element.
	Content string

	// Style element style attribute.
	Style *GraphicSVGStyle

	// Width of element.
	Width float64

	// Height of element.
	Height         float64
	scale          float64
	linearShadings map[string]*LinearShading
	radialShadings map[string]*RadialShading
	xPos           float64
	yPos           float64
}

// GetLineHeight returns the default line height multiplier of the paragraph.
func (sp *StyledParagraph) GetLineHeight() float64 { return sp.lineHeight }

// GetBackgroundColor returns the cell's background color.
func (tc *TableCell) GetBackgroundColor() Color { return tc.backgroundColor }
func (tp *templateProcessor) parseRadialGradientAttr(creator *Creator, s2 string) (Color, error) {
	radialGradientAttr := ColorBlack
	if s2 == "" {
		return radialGradientAttr, nil
	}
	var (
		err         error
		x           = 0.0
		y           = 0.0
		outerRadius = -1.0
		strs        = strings.Split(s2[16:len(s2)-1], ",")
	)
	fn := func(strs []string) bool {
		return len(strs) > 0 && (strs[0][0] == '#' || tp.parseColor(strs[0]) != nil)
	}
	fields := strings.Fields(strs[0])
	if len(fields) == 2 && !fn(fields) {
		if finiteFloat, err2 := parseFiniteFloat(fields[0]); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient X position: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid radial gradient X position %q", fields[0]))
		} else {
			x = finiteFloat
		}
		if finiteFloat, err2 := parseFiniteFloat(fields[1]); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient Y position: %v", err2)
			setFirstError(&err, fmt.Errorf("invalid radial gradient Y position %q", fields[1]))
		} else {
			y = finiteFloat
		}
		strs = strs[1:]
	}
	if len(strs) == 0 {
		setFirstError(&err, errors.New("gradient has no color stops"))
		return radialGradientAttr, err
	}
	s := strings.TrimSpace(strs[0])
	if len(s) > 0 && !fn(strings.Fields(s)) {
		if finiteFloat, err2 := parseFiniteFloat(s); err2 != nil {
			common.Log.Debug("Failed parsing radial gradient size: %v", err2)
			setFirstError(&err, fmt.Errorf("radial gradient component %q is neither a size nor a color", s))
		} else {
			outerRadius = finiteFloat
		}
		strs = strs[1:]
	}
	colors, positions, err2 := tp.processGradientColorPair(strs)
	setFirstError(&err, err2)
	if colors == nil || positions == nil {
		return radialGradientAttr, err
	}
	if len(colors) == 1 {
		return colors[0], err
	}
	radialGradientColor := creator.NewRadialGradientColor(x, y, 0, outerRadius, []*ColorPoint{})
	for i := 0; i < len(colors); i++ {
		radialGradientColor.AddColorStop(colors[i], positions[i])
	}
	return radialGradientColor, err
}
func (s *shading) generatePdfFunctions() []model.PdfFunction {
	if len(s.colorPoints) == 0 {
		return nil
	} else if len(s.colorPoints) <= 2 {
		r, g, b := s.colorPoints[0].color.ToRGB()
		r2, g2, b2 := s.colorPoints[len(s.colorPoints)-1].color.ToRGB()
		return []model.PdfFunction{&model.PdfFunctionType2{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, N: 1, C0: []float64{r, g, b}, C1: []float64{r2, g2, b2}}}
	} else {
		functions := []model.PdfFunction{}
		floats := []float64{}
		for i := 0; i < len(s.colorPoints)-1; i++ {
			r, g, b := s.colorPoints[i].color.ToRGB()
			r2, g2, b2 := s.colorPoints[i+1].color.ToRGB()
			functionType2 := &model.PdfFunctionType2{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, N: 1, C0: []float64{r, g, b}, C1: []float64{r2, g2, b2}}
			functions = append(functions, functionType2)
			if i > 0 {
				floats = append(floats, s.colorPoints[i].point)
			}
		}
		encode := []float64{}
		for range functions {
			encode = append(encode, []float64{0.0, 1.0}...)
		}
		return []model.PdfFunction{&model.PdfFunctionType3{Domain: []float64{0.0, 1.0}, Range: []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}, Functions: functions, Bounds: floats, Encode: encode}}
	}
}

// Columns returns all the columns in the invoice line items table.
func (i *Invoice) Columns() []*InvoiceCell { return i.columns }

// SetTotal sets the total of the invoice.
func (i *Invoice) SetTotal(value string) { i.total[1].Value = value }
func (gss *GraphicSVGStyle) toContentStream(cc *contentstream.ContentCreator, resources *model.PdfPageResources, graphicSVGElement *GraphicSVGElement) {
	if gss == nil {
		return
	}
	if gss.FillColor != "" {
		var r, g, b float64
		if rgba, ok := graphic2d.ColorMap[gss.FillColor]; ok {
			r2, g2, b2, _ := rgba.RGBA()
			r, g, b = float64(r2), float64(g2), float64(b2)
			cc.Add_rg(r, g, b)
		} else if strings.HasPrefix(gss.FillColor, "rgb(") {
			r, g, b = parseRGBColor(gss.FillColor)
			cc.Add_rg(r, g, b)
		} else if strings.HasPrefix(gss.FillColor, "url(") {
			s := strings.TrimPrefix(gss.FillColor, "url('#")
			s = strings.TrimPrefix(s, "url(#")
			s = strings.TrimSuffix(s, "')")
			s = strings.TrimSuffix(s, ")")
			if graphicSVGElement.linearShadings[s] != nil {
				resources.SetPatternByName(*core.MakeName(s), graphicSVGElement.linearShadings[s].ToPdfShadingPattern().ToPdfObject())
				cc.Add_cs(*core.MakeName("Pattern"))
				cc.Add_scn_pattern(*core.MakeName(s))
			} else if graphicSVGElement.radialShadings[s] != nil {
				resources.SetPatternByName(*core.MakeName(s), graphicSVGElement.radialShadings[s].ToPdfShadingPattern().ToPdfObject())
				cc.Add_cs(*core.MakeName("Pattern"))
				cc.Add_scn_pattern(*core.MakeName(s))
			}
		} else {
			r, g, b = ColorRGBFromHex(gss.FillColor).ToRGB()
			cc.Add_rg(r, g, b)
		}
	}
	if gss.FillOpacity < 1.0 {
		n := 0
		keyName := core.PdfObjectName(fmt.Sprintf("GS%d", n))
		for {
			_, ok := resources.GetExtGState(keyName)
			if !ok {
				break
			}
			n++
			keyName = core.PdfObjectName(fmt.Sprintf("GS%d", n))
		}
		dict := core.MakeDict()
		dict.Set("ca", core.MakeFloat(gss.FillOpacity))
		err := resources.AddExtGState(keyName, core.MakeIndirectObject(dict))
		if err != nil {
			common.Log.Debug(err.Error())
			return
		}
		cc.Add_gs(keyName)
	}
	if gss.StrokeColor != "" {
		var r, g, b float64
		if rgba, ok := graphic2d.ColorMap[gss.StrokeColor]; ok {
			r2, g2, b2, _ := rgba.RGBA()
			r, g, b = float64(r2)/255.0, float64(g2)/255.0, float64(b2)/255.0
		} else if strings.HasPrefix(gss.FillColor, "rgb(") {
			r, g, b = parseRGBColor(gss.FillColor)
		} else {
			r, g, b = ColorRGBFromHex(gss.StrokeColor).ToRGB()
		}
		cc.Add_RG(r, g, b)
	}
	if gss.StrokeWidth > 0 {
		cc.Add_w(gss.StrokeWidth)
	}
}
func (b *Block) translate(tx, ty float64) {
	operations := contentstream.NewContentCreator().Translate(tx, -ty).Operations()
	*b.contents = append(*operations, *b.contents...)
	b.contents.WrapIfNeeded()
}

const (
	AnchorBottomLeft AnchorPoint = iota
	AnchorBottomRight
	AnchorTopLeft
	AnchorTopRight
	AnchorCenter
	AnchorLeft
	AnchorRight
	AnchorTop
	AnchorBottom
)

func templateValueToString(i interface{}) (string, error) { return valueToString(i) }

// SetAnchor set gradient position anchor.
// Default to center.
func (rs *RadialShading) SetAnchor(anchor AnchorPoint) { rs.anchor = anchor }

// SetMarkedContentID sets marked content ID.
func (i *Invoice) SetMarkedContentID(id int64) {}

// SetFontSize sets the font size for the paragraph.
func (sp *StyledParagraph) SetFontSize(fontSize float64) {
	sp.defaultStyle.FontSize = fontSize
	for _, chunk := range sp.chunks {
		chunk.Style.FontSize = fontSize
	}
}
func (i *Invoice) generateNoteBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	division := newDivision()
	rows := append([][2]string{i.notes, i.terms}, i.sections...)
	for _, row := range rows {
		if row[1] != "" {
			section := i.drawSection(row[0], row[1])
			for _, d := range section {
				division.Add(d)
			}
			styledParagraph := newStyledParagraph(i.defaultStyle)
			styledParagraph.SetMargins(0, 0, 10, 0)
			division.Add(styledParagraph)
		}
	}
	return division.GeneratePageBlocks(ctx)
}

// GetHorizontalAlignment returns the cell's horizontal alignment of content.
func (tc *TableCell) GetHorizontalAlignment() CellHorizontalAlignment { return tc.horizontalAlignment }

var textChunkAttrs = map[string]attrApplyFunc{"color": colorAttr(func(chunk *TextChunk, color Color) { chunk.Style.Color = color }), "outline-color": colorAttr(func(chunk *TextChunk, outlineColor Color) { chunk.Style.OutlineColor = outlineColor }), "font": fontAttr(func(chunk *TextChunk, font *model.PdfFont) { chunk.Style.Font = font }), "font-size": floatAttr(func(chunk *TextChunk, fontSize float64) { chunk.Style.FontSize = fontSize }), "line-height": floatAttr(func(chunk *TextChunk, lineHeight float64) { chunk.Style.LineHeight = lineHeight }), "outline-size": floatAttr(func(chunk *TextChunk, outlineSize float64) { chunk.Style.OutlineSize = outlineSize }), "character-spacing": floatAttr(func(chunk *TextChunk, charSpacing float64) { chunk.Style.CharSpacing = charSpacing }), "horizontal-scaling": floatAttr(func(chunk *TextChunk, horizontalScaling float64) { chunk.Style.HorizontalScaling = horizontalScaling }), "rendering-mode": enumAttr(textRenderingModeValues, func(chunk *TextChunk, renderingMode TextRenderingMode) { chunk.Style.RenderingMode = renderingMode }), "underline": boolAttr(func(chunk *TextChunk, underline bool) { chunk.Style.Underline = underline }), "underline-color": colorAttr(func(chunk *TextChunk, color Color) { chunk.Style.UnderlineStyle.Color = color }), "underline-offset": floatAttr(func(chunk *TextChunk, offset float64) { chunk.Style.UnderlineStyle.Offset = offset }), "underline-thickness": floatAttr(func(chunk *TextChunk, thickness float64) { chunk.Style.UnderlineStyle.Thickness = thickness }), "text-rise": floatAttr(func(chunk *TextChunk, textRise float64) { chunk.Style.TextRise = textRise }), "link": nil}

func marginAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("marginAttr", i, 0, marginsType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		margins, err := parseMargins(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(margins))
		return nil
	}
}

// CellVerticalAlignment defines the table cell's vertical alignment.
type CellVerticalAlignment int

// Scale block by specified factors in the x and y directions.
func (b *Block) Scale(sx, sy float64) {
	operations := contentstream.NewContentCreator().Scale(sx, sy).Operations()
	*b.contents = append(*operations, *b.contents...)
	b.contents.WrapIfNeeded()
	b.width *= sx
	b.height *= sy
}

type marginDrawable interface {
	VectorDrawable
	GetMargins() (float64, float64, float64, float64)
}

func (tp *templateProcessor) renderNode(node *templateNode) error {
	value := node.value
	if value == nil {
		return nil
	}
	local := node.start.Name.Local
	if _, ok := templateTags[local]; !ok {
		tp.nodeLogDebug(node, "Invalid tag <%s>. Skipping.", local)
		return nil
	}
	var renderer interface{}
	if componentParent := node.componentParent(); componentParent != nil {
		renderer = componentParent.value
	} else {
		renderer = tp.renderer
	}
	switch renderer2 := renderer.(type) {
	case componentRenderer:
		if background, ok := value.(*Background); ok {
			if block, ok := renderer2.(*Block); ok {
				block.SetBackground(background)
			}
			return nil
		}
		drawable, ok := value.(Drawable)
		if !ok {
			tp.nodeLogError(node, "Tag <%s> (%T) is not a drawable.", local, value)
			return errInvalidChild
		}
		err := renderer2.Draw(drawable)
		if err != nil {
			return tp.nodeError(node, "Cannot draws tag <%s> (%T): %s.", local, value, err)
		}
	case *Division:
		switch value2 := value.(type) {
		case *Background:
			renderer2.SetBackground(value2)
		case VectorDrawable:
			err := renderer2.Add(value2)
			if err != nil {
				return tp.nodeError(node, "Cannot add tag <%s> (%T) into a Division: %s.", local, value, err)
			}
		}
	case *TableCell:
		drawable, ok := value.(VectorDrawable)
		if !ok {
			tp.nodeLogError(node, "Tag <%s> (%T) is not a vector drawable.", local, value)
			return errInvalidChild
		}
		err := renderer2.SetContent(drawable)
		if err != nil {
			return tp.nodeError(node, "Cannot add tag <%s> (%T) into a table cell: %s.", local, value, err)
		}
	case *StyledParagraph:
		chunk, ok := value.(*TextChunk)
		if !ok {
			tp.nodeLogError(node, "Tag <%s> (%T) is not a text chunk.", local, value)
			return errInvalidChild
		}
		renderer2.appendChunk(chunk)
	case *Chapter:
		switch value2 := value.(type) {
		case *Chapter:
			return nil
		case *Background:
			renderer2.SetBackground(value2)
		case *StyledParagraph:
			if node.start.Name.Local == "chapter-heading" {
				return nil
			}
			err := renderer2.Add(value2)
			if err != nil {
				return tp.nodeError(node, "Cannot add tag <%s> (%T) into a Chapter: %s.", local, value, err)
			}
		case Drawable:
			err := renderer2.Add(value2)
			if err != nil {
				return tp.nodeError(node, "Cannot add tag <%s> (%T) into a Chapter: %s.", local, value, err)
			}
		}
	case *List:
		switch value2 := value.(type) {
		case *TextChunk:
		case *listItem:
			renderer2.items = append(renderer2.items, value2)
		default:
			tp.nodeLogError(node, "Tag <%s> (%T) is not a list item. Skipping.", local, value)
		}
	case *listItem:
		switch value2 := value.(type) {
		case *TextChunk:
		case *StyledParagraph:
			renderer2.content = value2
		case *List:
			if value2.defaultIndent {
				value2.indent = 15
			}
			renderer2.content = value2
		case *Image:
			renderer2.content = value2
		case *Division:
			renderer2.content = value2
		case *Table:
			renderer2.content = value2
		default:
			tp.nodeLogError(node, "Tag <%s> (%T) is not supported in a list.", local, value)
			return errInvalidChild
		}
	}
	return nil
}

// SetVerticalAlignment set the cell's vertical alignment of content.
// Can be one of:
// - CellHorizontalAlignmentTop
// - CellHorizontalAlignmentMiddle
// - CellHorizontalAlignmentBottom
func (tc *TableCell) SetVerticalAlignment(valign CellVerticalAlignment) {
	tc.verticalAlignment = valign
}

// SetBackground sets the background properties of the chapter. The background
// is drawn behind the chapter content on every page the chapter occupies.
func (ch *Chapter) SetBackground(background *Background) { ch.background = background }

// SetBackgroundColor set background color of the shading area.
//
// By default the background color is set to white.
func (s *shading) SetBackgroundColor(backgroundColor Color) { s.backgroundColor = backgroundColor }

// InsertColumn inserts a column in the line items table at the specified index.
func (i *Invoice) InsertColumn(index uint, description string) *InvoiceCell {
	columnsLen := uint(len(i.columns))
	if index > columnsLen {
		index = columnsLen
	}
	column := i.NewColumn(description)
	i.columns = append(i.columns[:index], append([]*InvoiceCell{column}, i.columns[index:]...)...)
	return column
}
func (t *Table) wrapRow(i2 int, ctx DrawContext, totalWidth, height float64) (bool, error) {
	if !t.enableRowWrap {
		return false, nil
	}
	row := t.cells[i2].row
	for _, cell := range t.cells {
		if cell.rowSpan > 1 && cell.row <= row && row < cell.row+cell.rowSpan {
			return false, nil
		}
	}
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		if t.cells[i].wrapDisabled {
			return false, nil
		}
	}
	var (
		cell     = t.cells[i2]
		cellsLen = -1
		cells    []*TableCell
		value    float64
		row2     bool
		floats   = make([]float64, 0, len(t.colWidths))
	)
	fn := func(cell *TableCell, content VectorDrawable, ok bool) *TableCell {
		cell2 := *cell
		cell2.content = content
		if ok {
			cell2.row++
		}
		return &cell2
	}
	fn2 := func(n int, content VectorDrawable) {
		var height float64 = -1
		if content == nil {
			if float := floats[n-i2]; float > ctx.Height {
				content = t.cells[n].content
				t.cells[n].content = nil
				floats[n-i2] = 0
				height = float
			}
		}
		fn2 := fn(t.cells[n], content, true)
		cells = append(cells, fn2)
		if height < 0 {
			height = fn2.height(ctx.Width)
		}
		if height > value {
			value = height
		}
	}
	type savedCell struct {
		target *TableCell
		saved  TableCell
	}
	var savedCells []savedCell
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		savedCells = append(savedCells, savedCell{t.cells[i], *t.cells[i]})
	}
	subtables := t.splitSubtables(i2, ctx, totalWidth, height)
	for i := i2; i < len(t.cells); i++ {
		cell2 := t.cells[i]
		if cell.row != cell2.row {
			cellsLen = i
			break
		}
		ctx.Width = cell2.width(t.colWidths, totalWidth)
		height2 := cell2.height(ctx.Width)
		var content VectorDrawable
		switch content2 := cell2.content.(type) {
		case *StyledParagraph:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom - 0.5*content2.getTextHeight())
				head, tail, err := content2.split(ctx2)
				if err != nil {
					return false, err
				}
				if head != nil && tail != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
					content = tail
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		case *Division:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Width = ctx.Width - cell2.indent
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom)
				head, tail := content2.split(ctx2, height-content2.margins.Vertical())
				if head != nil && tail != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
					content = tail
					row2 = true
					if head.background != nil {
						head.background.BorderRadiusBottomLeft = 0
						head.background.BorderRadiusBottomRight = 0
					}
					if tail.background != nil {
						tail.background.BorderRadiusTopLeft = 0
						tail.background.BorderRadiusTopRight = 0
					}
					height2 = cell2.height(ctx.Width)
				}
			}
		case *List:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Width = ctx.Width - cell2.indent
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Vertical())
				head, tail := content2.split(ctx2, height-content2.margins.Vertical())
				if head != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
				}
				if tail != nil {
					content = tail
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		case *Table:
			if subtable, ok := subtables[i]; ok {
				content2, content3 := subtable[0], subtable[1]
				if content2 != nil {
					cell2 = fn(cell2, content2, false)
				} else {
					cell2 = fn(cell2, nil, false)
				}
				t.cells[i] = cell2
				if content3 != nil {
					content = content3
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		}
		floats = append(floats, height2)
		if row2 {
			if cells == nil {
				cells = make([]*TableCell, 0, len(t.colWidths))
				for j := i2; j < i; j++ {
					fn2(j, nil)
				}
			}
			fn2(i, content)
		}
	}
	var value2 float64
	for _, float := range floats {
		if float > value2 {
			value2 = float
		}
	}
	if row2 && value2 > ctx.Height {
		for i, savedCell := range savedCells {
			*savedCell.target = savedCell.saved
			t.cells[i2+i] = savedCell.target
		}
		return false, nil
	}
	if row2 {
		if cellsLen < 0 {
			cellsLen = len(t.cells)
		}
		row := t.cells[cellsLen-1].row
		for i := cellsLen; i < len(t.cells); i++ {
			t.cells[i].row++
		}
		t.cells = append(t.cells[:cellsLen], append(cells, t.cells[cellsLen:]...)...)
		t.rowHeights = append(t.rowHeights[:row], append([]float64{value}, t.rowHeights[row:]...)...)
		if len(t.rowHeights) > t.rows {
			t.rows = len(t.rowHeights)
		}
		t.rowHeights[cell.row-1] = value2
	}
	return row2, nil
}

// AddAnnotation adds an annotation to the current block.
// The annotation will be added to the page the block will be rendered on.
func (b *Block) AddAnnotation(annotation *model.PdfAnnotation) {
	for _, annotation2 := range b.annotations {
		if annotation2 == annotation {
			return
		}
	}
	b.annotations = append(b.annotations, annotation)
}

// FillOpacity returns the fill opacity of the rectangle (0-1).
func (r *Rectangle) FillOpacity() float64 { return r.fillOpacity }
func templateDict(vals ...interface{}) (map[string]interface{}, error) {
	n := len(vals)
	if n%2 != 0 {
		common.Log.Error("Invalid number of parameters for creating map: %d.", n)
		return nil, core.ErrRangeError
	}
	m := map[string]interface{}{}
	for i := 0; i < n; i += 2 {
		s, ok := vals[i].(string)
		if !ok {
			common.Log.Error("Invalid map key type (%T). Expected string.", vals[i])
			return nil, core.ErrTypeError
		}
		m[s] = vals[i+1]
	}
	return m, nil
}

// InvoiceCellProps holds all style properties for an invoice cell.
type InvoiceCellProps struct {
	TextStyle       TextStyle
	Alignment       CellHorizontalAlignment
	BackgroundColor Color
	BorderColor     Color
	BorderWidth     float64
	BorderSides     []CellBorderSide
}

// ScaleToWidth scale Image to a specified width w, maintaining the aspect ratio.
func (im *Image) ScaleToWidth(w float64) {
	height := im.height / im.width
	im.width = w
	im.height = w * height
}

// Total returns the invoice total description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Total() (*InvoiceCell, *InvoiceCell) { return i.total[0], i.total[1] }
func (tn *templateNode) classNames() []string {
	for _, attr := range tn.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == classAttrName {
			return strings.Fields(attr.Value)
		}
	}
	return nil
}
func applyShapeColor(block *Block, color model.PdfColor, color2 Color, fn func() Rectangle) error {
	switch colorPatternType1 := color.(type) {
	case *model.PdfColorPatternType1:
		functionBasedShading, ok := color2.(*FunctionBasedShading)
		if !ok {
			return fmt.Errorf("Color is not FunctionBasedShading")
		}
		rect := fn()
		functionBasedShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := functionBasedShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	case *model.PdfColorPatternType2:
		linearShading, ok := color2.(*LinearShading)
		if !ok {
			return fmt.Errorf("Color is not LinearShading")
		}
		rect := fn()
		linearShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := linearShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	case *model.PdfColorPatternType3:
		radialShading, ok := color2.(*RadialShading)
		if !ok {
			return fmt.Errorf("Color is not RadialShading")
		}
		rect := fn()
		radialShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := radialShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	}
	return nil
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
func copyAnnotations(annotations2 []*model.PdfAnnotation) []*model.PdfAnnotation {
	var annotations []*model.PdfAnnotation
	for _, annotation := range annotations2 {
		annotations = append(annotations, copyAnnotation(annotation))
	}
	return annotations
}
func maxMCID(kDict *model.KDict) int64 {
	maxMCID2 := int64(-1)
	if kDict == nil {
		return maxMCID2
	}
	if val, ok := core.GetIntVal(kDict.K); ok {
		maxMCID2 = int64(val)
	} else if arr, ok := core.GetArray(kDict.K); ok {
		for _, obj := range arr.Elements() {
			if val, ok := core.GetIntVal(obj); ok && int64(val) > maxMCID2 {
				maxMCID2 = int64(val)
			}
		}
	}
	for _, kValue := range kDict.GetChildren() {
		if mcid := kValue.GetMCID(); mcid != nil {
			if maxMCID := int64(*mcid); maxMCID > maxMCID2 {
				maxMCID2 = maxMCID
			}
		}
		if kDict := kValue.GetKDict(); kDict != nil {
			if maxMCID3 := maxMCID(kDict); maxMCID3 > maxMCID2 {
				maxMCID2 = maxMCID3
			}
		}
	}
	return maxMCID2
}

type commands struct {
	names       []string
	paramCounts map[string]int
	moveSymbol  string
	closeSymbol string
}

func (t *Table) wrapContent(ctx2 DrawContext) error {
	t.sortCells()
	value := ctx2.contentHeight() - t.headerHeight()
	fn := func(head2 VectorDrawable, width, value2, height float64) (head, tail VectorDrawable) {
		switch head3 := head2.(type) {
		case *StyledParagraph:
			ctx := ctx2
			ctx.Width = width
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom - 0.5*head3.getTextHeight())
			head4, tail2, err := head3.split(ctx)
			if err != nil {
				common.Log.Error("Error wrap styled paragraph: %v", err.Error())
				return head2, nil
			}
			if head4 != nil {
				head = head4
			}
			if tail2 != nil {
				tail = tail2
			}
		case *Division:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom)
			head2, tail2 := head3.split(ctx, value-head3.margins.Vertical())
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		case *List:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom)
			head2, tail2 := head3.split(ctx, value-head3.margins.Vertical())
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		case *Table:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = height
			head2, tail2 := head3.split(ctx, value)
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		default:
			return head2, nil
		}
		return head, tail
	}
	fn2 := func(head2 VectorDrawable, width, value, height float64) []VectorDrawable {
		if para, ok := head2.(*StyledParagraph); ok {
			ctx := ctx2
			ctx.Width = width
			ctx.Height = math.Floor(height - para.margins.Top - para.margins.Bottom - 0.5*para.getTextHeight())
			toPieces, err := para.splitToPieces(ctx)
			if err != nil {
				common.Log.Error("Error splitting styled paragraph: %v", err.Error())
				return []VectorDrawable{head2}
			}
			drawables := make([]VectorDrawable, len(toPieces))
			for i, toPiece := range toPieces {
				drawables[i] = toPiece
			}
			return drawables
		}
		var drawables []VectorDrawable
		head := head2
		for {
			head2, tail := fn(head, width, value, height)
			if head2 == nil {
				drawables = append(drawables, head)
				break
			}
			drawables = append(drawables, head2)
			if tail == nil {
				break
			}
			head = tail
		}
		return drawables
	}
	contentHeight := ctx2.contentHeight()
	height2 := contentHeight
	if headerHeight := t.headerHeight(); headerHeight < height2 {
		height2 -= headerHeight
	}
	type cellSplit struct {
		cell  *TableCell
		parts []VectorDrawable
	}
	fn3 := func() {
		if t.enableRowWrap {
			return
		}
		ok := false
		for i := t.rows; i >= 1; i-- {
			height := height2
			if t.hasHeaderRows && i >= t.headerStartRow && i <= t.headerEndRow {
				height = contentHeight
			}
			if i-1 >= len(t.rowHeights) || t.rowHeights[i-1] <= height {
				continue
			}
			var cellSplits []cellSplit
			n := 1
			rowSpan := 1
			for _, cell := range t.cells {
				if cell.row != i {
					continue
				}
				width := cell.width(t.colWidths, ctx2.Width)
				if cell.height(width) <= height {
					continue
				}
				fn := fn2(cell.content, width, cell.indent, height)
				if len(fn) < 2 {
					continue
				}
				cellSplits = append(cellSplits, cellSplit{cell, fn})
				if len(fn) > n {
					n = len(fn)
				}
				if cell.rowSpan > rowSpan {
					rowSpan = cell.rowSpan
				}
			}
			if n < 2 {
				continue
			}
			t.insertRows(i, n-1)
			n2 := i + (n - 1) + (rowSpan - 1)
			for j := i; j <= n2 && j-1 < len(t.rowHeights); j++ {
				t.rowHeights[j-1] = t.defaultRowHeight
			}
			ok = true
			for _, cellSplit := range cellSplits {
				cellSplit.cell.content = cellSplit.parts[0]
				cellSplit.cell.rowSpan = 1
				for j := 1; j < len(cellSplit.parts); j++ {
					props := cellSplit.cell.cloneProps(cellSplit.parts[j])
					props.rowSpan = 1
					props.row = i + j
					props.col = cellSplit.cell.col
					t.cells = append(t.cells, props)
				}
			}
		}
		if ok {
			t.sortCells()
			t.updateRowHeights(ctx2.Width)
		}
	}
	fn3()
	t.splitSpanningCellsAtBreaks(ctx2, fn)
	fn3()
	t.sortCells()
	return nil
}

// GetColspan returns the number of columns the cell spans.
func (tc *TableCell) GetColspan() int { return tc.colSpan }

// GetTextAlignment returns the horizontal alignment of the text within the
// paragraph.
func (sp *StyledParagraph) GetTextAlignment() TextAlignment { return sp.alignment }
func flushToken(tokens []token, text string) ([]token, string) {
	if text != "" {
		tokens = append(tokens, token{text, false})
		text = ""
	}
	return tokens, text
}

// StyledParagraph represents text drawn with a specified font and can wrap across lines and pages.
// By default occupies the available width in the drawing context.
type StyledParagraph struct {
	taggedDrawable
	chunks            []*TextChunk
	defaultStyle      TextStyle
	linkStyle         TextStyle
	alignment         TextAlignment
	verticalAlignment TextVerticalAlignment
	lineHeight        float64
	enableWrap        bool
	width             float64
	enableWordWrap    bool
	maxLines          int
	defaultWrap       bool
	textOverflow      TextOverflow
	angle             float64
	margins           Margins
	positioning       Positioning
	xPos              float64
	yPos              float64
	unusedA           float64
	unusedB           float64
	lines             [][]*TextChunk
	beforeRender      func(para *StyledParagraph, ctx DrawContext)
	languageID        string
	artifact          *model.Artifact
	enableShaping     bool
	dropCaps          *DropCapsOptions
	dropCapsEnabled   bool
}

// SetStructureType sets the structure type for the drawable.
func (td *taggedDrawable) SetStructureType(structureType model.StructureType) {
	if td.tagInfo == nil {
		td.tagInfo = model.NewStructureTagInfo()
	}
	td.tagInfo.StructureType = structureType
}

var (
	positioningValues             = map[string]Positioning{"relative": PositionRelative, "absolute": PositionAbsolute}
	textRenderingModeValues       = map[string]TextRenderingMode{"fill": TextRenderingModeFill, "stroke": TextRenderingModeStroke, "fill-stroke": TextRenderingModeFillStroke, "invisible": TextRenderingModeInvisible, "fill-clip": TextRenderingModeFillClip, "stroke-clip": TextRenderingModeStrokeClip, "fill-stroke-clip": TextRenderingModeFillStrokeClip, "clip": TextRenderingModeClip}
	textAlignmentValues           = map[string]TextAlignment{"left": TextAlignmentLeft, "right": TextAlignmentRight, "center": TextAlignmentCenter, "justify": TextAlignmentJustify}
	textVerticalAlignmentValues   = map[string]TextVerticalAlignment{"baseline": TextVerticalAlignmentBaseline, "center": TextVerticalAlignmentCenter}
	textOverflowValues            = map[string]TextOverflow{"visible": TextOverflowVisible, "hidden": TextOverflowHidden}
	fitModeValues                 = map[string]FitMode{"none": FitModeNone, "fill-width": FitModeFillWidth}
	lineStyleValues               = map[string]contentdraw.LineStyle{"solid": contentdraw.LineStyleSolid, "dashed": contentdraw.LineStyleDashed}
	cellHorizontalAlignmentValues = map[string]CellHorizontalAlignment{"left": CellHorizontalAlignmentLeft, "center": CellHorizontalAlignmentCenter, "right": CellHorizontalAlignmentRight}
	cellVerticalAlignmentValues   = map[string]CellVerticalAlignment{"top": CellVerticalAlignmentTop, "middle": CellVerticalAlignmentMiddle, "bottom": CellVerticalAlignmentBottom}
	cellBorderStyleValues         = map[string]CellBorderStyle{"none": CellBorderStyleNone, "single": CellBorderStyleSingle, "double": CellBorderStyleDouble}
	horizontalAlignmentValues     = map[string]HorizontalAlignment{"left": HorizontalAlignmentLeft, "center": HorizontalAlignmentCenter, "right": HorizontalAlignmentRight}
	encoderFactories              = map[string]func() core.StreamEncoder{"flate": func() core.StreamEncoder { return core.NewFlateEncoder() }, "dct": func() core.StreamEncoder { return core.NewDCTEncoder() }}
)
var templateTags = map[string]*templateTag{"paragraph": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}, "list-item": struct{}{}}, parse: parseStyledParagraphTag, attrs: styledParagraphAttrs}, "text-chunk": &templateTag{allowedParents: map[string]struct{}{"paragraph": struct{}{}}, parse: parseTextChunkTag, attrs: textChunkAttrs}, "division": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}, "list-item": struct{}{}}, parse: parseDivisionTag, attrs: divisionAttrs}, "table": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}, "list-item": struct{}{}}, parse: parseTableTag, attrs: tableAttrs}, "table-cell": &templateTag{allowedParents: map[string]struct{}{"table": struct{}{}}, parse: parseTableCellTag, attrs: tableCellAttrs, singleChild: true}, "line": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}}, parse: parseLineTag, attrs: lineAttrs}, "rectangle": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}}, parse: parseRectangleTag, attrs: rectangleAttrs}, "ellipse": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}}, parse: parseEllipseTag, attrs: ellipseAttrs}, "image": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}, "list-item": struct{}{}}, parse: parseImageTag, attrs: imageAttrs}, "chapter": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "chapter": struct{}{}}, parse: parseChapterTag, attrs: chapterAttrs}, "chapter-heading": &templateTag{allowedParents: map[string]struct{}{"chapter": struct{}{}}, parse: parseChapterHeadingTag, attrs: chapterHeadingAttrs}, "chart": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}}, parse: parseChartTag, attrs: chartAttrs}, "page-break": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "chapter": struct{}{}, "division": struct{}{}}, parse: parsePageBreakTag, attrs: emptyAttrs}, "background": &templateTag{allowedParents: map[string]struct{}{"block": struct{}{}, "division": struct{}{}, "chapter": struct{}{}}, parse: parseBackgroundTag, attrs: backgroundAttrs}, "list": &templateTag{allowedParents: map[string]struct{}{"creator": struct{}{}, "block": struct{}{}, "division": struct{}{}, "table-cell": struct{}{}, "chapter": struct{}{}, "list-item": struct{}{}}, parse: parseListTag, attrs: listAttrs}, "list-item": &templateTag{allowedParents: map[string]struct{}{"list": struct{}{}}, parse: parseListItemTag, attrs: emptyAttrs, singleChild: true}, "list-marker": &templateTag{allowedParents: map[string]struct{}{"list": struct{}{}, "list-item": struct{}{}}, parse: parseListMarkerTag, attrs: textChunkAttrs, allowMultipleChildren: true}}

func (d *Division) split(ctx2 DrawContext, value float64) (head, tail *Division) {
	var (
		drawableHeight2       float64
		drawables, components []VectorDrawable
	)
	right := ctx2.Width - d.margins.Left - d.margins.Right - d.padding.Left - d.padding.Right
	bottom := ctx2.Height - d.padding.Top - d.padding.Bottom
	bottom2 := value - d.padding.Top - d.padding.Bottom
	for i, component := range d.components {
		drawableHeight3 := drawableHeight(component, right)
		if drawableHeight2+drawableHeight3 <= bottom {
			drawableHeight2 += drawableHeight3
			drawables = append(drawables, component)
			continue
		}
		switch component2 := component.(type) {
		case *StyledParagraph:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2 - component2.margins.Vertical() - 0.5*component2.getTextHeight()
			if ctx.Height > 0 {
				head, tail, err := component2.split(ctx)
				if err == nil && head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *Division:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2-component2.margins.Vertical())
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *List:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2-component2.margins.Vertical())
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		case *Table:
			ctx := ctx2
			ctx.Width = right
			ctx.Height = bottom - drawableHeight2
			if ctx.Height > 0 {
				head, tail := component2.split(ctx, bottom2)
				if head != nil {
					drawables = append(drawables, head)
					drawableHeight2 += drawableHeight(head, ctx.Width)
				}
				if tail != nil {
					components = append(components, tail)
				}
			} else {
				components = append(components, component)
			}
		default:
			components = append(components, component)
		}
		components = append(components, d.components[i+1:]...)
		break
	}
	if len(drawables) > 0 {
		head = newDivision()
		*head = *d
		head.components = drawables
		if d.background != nil {
			head.background = &Background{}
			*head.background = *d.background
		}
	}
	if len(components) > 0 {
		tail = newDivision()
		*tail = *d
		tail.components = components
		if d.background != nil {
			tail.background = &Background{}
			*tail.background = *d.background
		}
	}
	return head, tail
}

// Chart represents a chart drawable.
// It is used to render unichart chart components using a creator instance.
type Chart struct {
	taggedDrawable
	chart       chartrender.ChartRenderable
	positioning Positioning
	xPos        float64
	yPos        float64
	margins     Margins
}

// EnableWordWrap sets the paragraph word wrap flag.
func (sp *StyledParagraph) EnableWordWrap(val bool) { sp.enableWordWrap = val }
func templateXMLEscape(i interface{}) (string, error) {
	valueToString2, err := valueToString(i)
	if err != nil {
		return "", err
	}
	var w bytes.Buffer
	xml.EscapeText(&w, []byte(valueToString2))
	return w.String(), nil
}
func (tp *templateProcessor) parseListItem(node *templateNode) (interface{}, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "List item parent cannot be nil.")
		return nil, errInvalidParent
	}
	list, ok := componentParent.value.(*List)
	if !ok {
		tp.nodeLogError(node, "List item parent is not List.")
		return nil, errInvalidParent
	}
	listItem := newListItem()
	listItem.marker = list.marker
	if err := tp.applyAttrs(node, listItem, emptyAttrs); err != nil {
		return nil, err
	}
	return listItem, nil
}

// DrawContext defines the drawing context. The DrawContext is continuously used and updated when
// drawing the page contents in relative mode.  Keeps track of current X, Y position, available
// height as well as other page parameters such as margins and dimensions.
type DrawContext struct {

	// Current page number.
	Page int

	// Current position.  In a relative positioning mode, a drawable will be placed at these coordinates.
	X, Y float64

	// Context dimensions.  Available width and height (on current page).
	Width, Height float64

	// Page Margins.
	Margins Margins

	// Absolute Page size, widths and height.
	PageWidth  float64
	PageHeight float64

	// Controls whether the components are stacked horizontally
	Inline          bool
	unsupportedRune rune
	errs            []error
}

func (t *Table) resetColumnWidths() {
	t.colWidths = []float64{}
	colWidth := float64(1.0) / float64(t.cols)
	for i := 0; i < t.cols; i++ {
		t.colWidths = append(t.colWidths, colWidth)
	}
}

// Color interface represents colors in the PDF creator.
type Color interface {
	ToRGB() (float64, float64, float64)
}

func parseInt64List(s string) ([]int64, error) {
	fields := strings.Fields(s)
	nums := make([]int64, len(fields))
	var err error
	for i, field := range fields {
		i2, err2 := strconv.ParseInt(field, 10, 64)
		if err2 != nil {
			setFirstError(&err, err2)
			i2 = 0
		}
		nums[i] = i2
	}
	return nums, err
}
func colorspaceForComponents(n int) (model.PdfColorspace, error) {
	switch n {
	case 1:
		return model.NewPdfColorspaceDeviceGray(), nil
	case 3:
		return model.NewPdfColorspaceDeviceRGB(), nil
	case 4:
		return model.NewPdfColorspaceDeviceCMYK(), nil
	}
	return nil, fmt.Errorf("program returns %d values, expected 1, 3 or 4 color components", n)
}
func (t *Table) clampCut(row int) int {
	for ok := true; ok && row > 0; {
		ok = false
		for _, cell := range t.cells {
			if cell.row <= row && row < cell.row+cell.rowSpan-1 {
				row = cell.row - 1
				ok = true
			}
		}
		if t.hasHeaderRows && row >= t.headerStartRow && row < t.headerEndRow {
			row = t.headerStartRow - 1
			ok = true
		}
	}
	return row
}

// GeneratePageBlocks generates a page break block.
func (pb *PageBreak) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	blocks := []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y), NewBlock(ctx.PageWidth, ctx.PageHeight)}
	ctx.Page++
	ctx2 := ctx
	ctx2.Y = ctx.Margins.Top
	ctx2.X = ctx.Margins.Left
	ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
	ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right
	ctx = ctx2
	return blocks, ctx, nil
}

// NewImage create a new image from an OpenPDF image (model.Image).
func (c *Creator) NewImage(img *model.Image) (*Image, error) { return newImage(img) }

// NewTOC creates a new table of contents.
func (c *Creator) NewTOC(title string) *TOC {
	textStyle := c.NewTextStyle()
	textStyle.Font = c.fontBold
	return newTOC(title, c.NewTextStyle(), textStyle)
}
func (g *Grid) resetColumnWidths() {
	g.colWidths = []float64{}
	colWidth := float64(1.0) / float64(g.cols)
	for i := 0; i < g.cols; i++ {
		g.colWidths = append(g.colWidths, colWidth)
	}
}
func (im *Image) applyFitMode(left float64) {
	left -= im.margins.Left + im.margins.Right
	switch im.fitMode {
	case FitModeFillWidth:
		im.ScaleToWidth(left)
	}
}

// ToPdfShadingPattern generates a new model.PdfShadingPatternType2 object.
func (ls *LinearShading) ToPdfShadingPattern() *model.PdfShadingPatternType2 {
	r, g, b := ls.base.backgroundColor.ToRGB()
	shadingModel := ls.shadingModel()
	shadingModel.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingPatternType2 := model.NewPdfShadingPatternType2()
	pdfShadingPatternType2.Shading = shadingModel
	return pdfShadingPatternType2
}
func (sp *StyledParagraph) getTextHeight() float64 {
	var textHeight float64
	for _, chunk := range sp.chunks {
		fontSize := chunk.Style.FontSize * chunk.Style.effectiveLineHeight(sp.lineHeight)
		if fontSize > textHeight {
			textHeight = fontSize
		}
	}
	return textHeight
}

// GetDefaultStyle returns the default text style of the paragraph. This is the
// style applied to chunks appended without an explicit style.
func (sp *StyledParagraph) GetDefaultStyle() TextStyle { return sp.defaultStyle }

// SetBorderWidth sets the border width.
func (cp *CurvePolygon) SetBorderWidth(borderWidth float64) { cp.polygon.BorderWidth = borderWidth }

// DrawTemplate renders the template provided through the specified reader,
// using the specified `data` and `options`.
// Creator templates are first executed as text/template *Template instances,
// so the specified `data` is inserted within the template.
// The second phase of processing is actually parsing the template, translating
// it into creator components and rendering them using the provided options.
// Both the `data` and `options` parameters can be nil.
func (c *Creator) DrawTemplate(r io.Reader, data interface{}, options *TemplateOptions) error {
	return drawTemplate(c, r, data, options, c)
}

// SetAnnotation sets an annotation on a TextChunk,
// this will replace any existing annotation that has been set.
// Supplying a nil value as parameter would clear all the annotations.
func (tec *TextChunk) SetAnnotation(annotation *model.PdfAnnotation) {
	if annotation == nil {
		tec.ClearAnnotations()
	} else {
		tec.annotations = []*model.PdfAnnotation{annotation}
	}
}
func isHexColor(s string) bool {
	if (len(s) != 4 && len(s) != 7) || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
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

// SetFitMode sets the fit mode of the image.
// NOTE: The fit mode is only applied if relative positioning is used.
func (im *Image) SetFitMode(fitMode FitMode) { im.fitMode = fitMode }

// NewFunctionBasedGradient creates a function-based (Type 1) gradient color from a
// PostScript calculator program, which maps a point in the shading's domain to
// the colour components at that point. The program's output count selects the
// colourspace: 1 for DeviceGray, 3 for DeviceRGB and 4 for DeviceCMYK.
func (c *Creator) NewFunctionBasedGradient(content string) *FunctionBasedShading {
	return newFunctionBasedShading(content)
}

// SetBoundingBox set gradient color bounding box where the gradient would be rendered.
func (ls *LinearShading) SetBoundingBox(x, y, width, height float64) {
	ls.boundingBox = &model.PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}
func (tp *templateProcessor) checkNodeText(node *templateNode, s string, templateText2 *templateText) (bool, error) {
	local := node.start.Name.Local
	if _, ok := textContentTags[local]; ok {
		return true, nil
	}
	if isBlank(s) {
		return false, nil
	}
	if tp.options.Strict {
		return false, tp.nodeError(templateText2.pos(), "tag <%s> does not take text content", local)
	}
	tp.nodeLogDebug(templateText2.pos(), "Tag <%s> does not take text content. Skipping.", local)
	return false, nil
}

// ScaleToHeight sets the graphic svg scaling factor with the given height.
func (gs *GraphicSVG) ScaleToHeight(h float64) {
	width := gs.root.Width / gs.root.Height
	gs.root.Height = h
	gs.root.Width = h * width
	gs.root.SetScaling(width, width)
}

type cmykColor struct{ c, m, y, k float64 }

func isKeycapSequence(s string) bool {
	rs := []rune(s)
	if len(rs) < 2 {
		return false
	}
	if !isKeycapBase(rs[0]) {
		return false
	}
	if len(rs) == 3 && rs[1] == 0xFE0F && isKeycapEnclosing(rs[2]) {
		return true
	}
	if len(rs) == 2 && isKeycapEnclosing(rs[1]) {
		return true
	}
	return false
}

// RenderTemplate draws the parsed template into the creator using the specified
// `data`. The handle comes from ParseTemplate and can be rendered repeatedly,
// with different data, and into different creators. The `data` parameter can be
// nil.
func (c *Creator) RenderTemplate(pt *ParsedTemplate, data interface{}) error {
	return pt.renderInto(c, c, data)
}

// SetBackground sets the background properties of the block. The background is
// drawn behind the block content, covering the block bounds.
func (b *Block) SetBackground(background *Background) { b.background = background }

var chapterHeadingAttrs = map[string]attrApplyFunc{"font": fontAttr(func(para *StyledParagraph, font *model.PdfFont) { para.SetFont(font) }), "font-size": floatAttr(func(para *StyledParagraph, fontSize float64) { para.SetFontSize(fontSize) }), "text-align": enumAttr(textAlignmentValues, func(para *StyledParagraph, align TextAlignment) { para.SetTextAlignment(align) }), "line-height": floatAttr(func(para *StyledParagraph, lineheight float64) { para.SetLineHeight(lineheight) }), "enable-wrap": boolAttr(func(para *StyledParagraph, enableWrap bool) { para.SetEnableWrap(enableWrap) }), "color": colorAttr(func(para *StyledParagraph, color Color) { para.SetFontColor(color) }), "x": floatAttr(func(para *StyledParagraph, x float64) { para.SetPos(x, para.yPos) }), "y": floatAttr(func(para *StyledParagraph, y float64) { para.SetPos(para.xPos, y) }), "angle": floatAttr(func(para *StyledParagraph, angle float64) { para.SetAngle(angle) }), "margin": marginAttr(func(para *StyledParagraph, margins Margins) {
	para.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "max-lines": intAttr(func(para *StyledParagraph, n int64) { para.SetMaxLines(int(n)) })}

// Style returns the style of the line.
func (l *Line) Style() contentdraw.LineStyle { return l.style }
func newStyledParagraph(defaultStyle TextStyle) *StyledParagraph {
	return &StyledParagraph{chunks: []*TextChunk{}, defaultStyle: defaultStyle, linkStyle: linkTextStyle(defaultStyle.Font), lineHeight: 1.0, alignment: TextAlignmentLeft, enableWrap: true, defaultWrap: true, enableWordWrap: false, angle: 0, unusedA: 1, unusedB: 1, positioning: PositionRelative, languageID: "", artifact: nil, taggedDrawable: taggedDrawable{structType: model.StructureTypeParagraph}}
}
func (tp *templateProcessor) effectiveAttrValue(node *templateNode, s string) (string, bool) {
	var (
		value string
		ok    bool
	)
	for _, s2 := range node.classNames() {
		for _, attr := range tp.classStyles[s2] {
			if attr.Name.Local == s {
				value, ok = attr.Value, true
			}
		}
	}
	for _, attr := range node.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == s {
			value, ok = attr.Value, true
		}
	}
	return value, ok
}

var PPI float64 = 72

// Width returns the width of the Paragraph.
func (sp *StyledParagraph) Width() float64 {
	if sp.enableWrap && int(sp.width) > 0 {
		return sp.width
	}
	return sp.getTextWidth() / 1000.0
}
func copyLinkAnnotation(annotationLink *model.PdfAnnotationLink) *model.PdfAnnotationLink {
	if annotationLink == nil {
		return nil
	}
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	pdfAnnotationLink.CopyFrom(annotationLink.PdfAnnotation)
	pdfAnnotationLink.BS = annotationLink.BS
	pdfAnnotationLink.A = annotationLink.A
	if action, err := annotationLink.GetAction(); err == nil && action != nil {
		pdfAnnotationLink.SetAction(action)
	}
	if pdfObjectArray, ok := annotationLink.Dest.(*core.PdfObjectArray); ok {
		pdfAnnotationLink.Dest = core.MakeArray(pdfObjectArray.Elements()...)
	}
	return pdfAnnotationLink
}

// SetPageMargins sets the page margins: left, right, top, bottom.
// The default page margins are 10% of document width.
func (c *Creator) SetPageMargins(left, right, top, bottom float64) {
	c.pageMargins.Left = left
	c.pageMargins.Right = right
	c.pageMargins.Top = top
	c.pageMargins.Bottom = bottom
}

type listItem struct {
	content VectorDrawable
	marker  TextChunk
}

// GetSideBorderColor returns the cell's border color for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (tc *TableCell) GetSideBorderColor(side CellBorderSide) Color {
	switch side {
	case CellBorderSideBottom:
		return tc.borderColorBottom
	case CellBorderSideLeft:
		return tc.borderColorLeft
	case CellBorderSideRight:
		return tc.borderColorRight
	default:
		return tc.borderColorTop
	}
}
func (tp *templateProcessor) parseParagraph(node *templateNode, styledParagraph *StyledParagraph) (interface{}, error) {
	if styledParagraph == nil {
		styledParagraph = tp.creator.NewStyledParagraph()
	}
	if err := tp.applyAttrs(node, styledParagraph, chapterHeadingAttrs); err != nil {
		return nil, err
	}
	return styledParagraph, nil
}

// GetRowspan returns the number of rows the cell spans.
func (gc *GridCell) GetRowspan() int { return gc.rowSpan }
func (tp *templateProcessor) setTableHeaderRows(node *templateNode, table *Table, startRow, endRow int) error {
	if startRow != 0 && endRow == 0 {
		endRow = startRow
	} else if startRow == 0 && endRow != 0 {
		startRow = 1
	}
	table.headerStartRow, table.headerEndRow = 0, 0
	if startRow == 0 {
		return nil
	}
	if err := table.SetHeaderRows(startRow, endRow); err != nil {
		if tp.options.Strict {
			return tp.nodeError(node, "invalid table header rows: %v", err)
		}
		tp.nodeLogDebug(node, "Could not set table header rows: %v.", err)
	}
	return nil
}
func (fbs *FunctionBasedShading) shadingModel() (*model.PdfShadingType1, error) {
	pdfFunction, colorSpace, err := fbs.pdfFunction()
	if err != nil {
		return nil, err
	}
	pdfShadingType1 := model.NewPdfShadingType1()
	pdfShadingType1.ShadingType = core.MakeInteger(1)
	pdfShadingType1.ColorSpace = colorSpace
	pdfShadingType1.AntiAlias = core.MakeBool(fbs.base.antiAlias)
	pdfShadingType1.Domain = core.MakeArrayFromFloats(functionDomain)
	pdfShadingType1.Matrix = core.MakeArrayFromFloats([]float64{fbs.boundingBox.Width(), 0, 0, fbs.boundingBox.Height(), fbs.boundingBox.Llx, fbs.boundingBox.Lly})
	pdfShadingType1.Function = []model.PdfFunction{pdfFunction}
	return pdfShadingType1, nil
}

// GetPositioning returns the positioning mode (relative or absolute) of the
// grid.
func (g *Grid) GetPositioning() Positioning { return g.positioning }
func intAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("intAttr", i, 0, int64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		i, err := parseInt64(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
			i = 0
		}
		callSetter(value2, target, reflect.ValueOf(i))
		return nil
	}
}

// GeneratePageBlocks generates the page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages. Implements the Drawable interface.
func (sp *StyledParagraph) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	var blocks []*Block
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if sp.positioning.IsRelative() {
		ctx.X += sp.margins.Left
		ctx.Y += sp.margins.Top
		ctx.Width -= sp.margins.Left + sp.margins.Right
		ctx.Height -= sp.margins.Top
		sp.SetWidth(ctx.Width)
	} else {
		if int(sp.width) <= 0 {
			sp.SetWidth(sp.getTextWidth() / 1000.0)
		}
		ctx.X = sp.xPos
		ctx.Y = sp.yPos
	}
	if sp.beforeRender != nil {
		sp.beforeRender(sp, ctx)
	}
	if err := sp.wrapText(); err != nil {
		return nil, ctx, err
	}
	lines := sp.lines
	n := 0
	for {
		paragraphChunks, lines2, err := drawParagraphChunks(block, sp, lines, ctx)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, ctx, err
		}
		ctx = paragraphChunks
		blocks = append(blocks, block)
		if lines = lines2; len(lines2) == 0 {
			break
		}
		if len(lines2) == n {
			return nil, ctx, errors.New("not enough space for paragraph")
		}
		sp.dropCaps = nil
		block = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx.Page++
		paragraphChunks = ctx
		paragraphChunks.Y = ctx.Margins.Top
		paragraphChunks.X = ctx.Margins.Left + sp.margins.Left
		paragraphChunks.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
		paragraphChunks.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - sp.margins.Left - sp.margins.Right
		ctx = paragraphChunks
		n = len(lines2)
	}
	if sp.positioning.IsRelative() {
		ctx.Y += sp.margins.Bottom
		ctx.Height -= sp.margins.Bottom
		if !ctx.Inline {
			ctx.X = ctx2.X
			ctx.Width = ctx2.Width
		}
		return blocks, ctx, nil
	}
	return blocks, ctx2, nil
}
func (tp *templateProcessor) parseAttrPropList(s string) map[string]string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	m := map[string]string{}
	for _, field := range fields {
		stringSubmatch := attrFuncRe.FindStringSubmatch(field)
		if len(stringSubmatch) < 3 {
			continue
		}
		s, s2 := strings.TrimSpace(stringSubmatch[1]), stringSubmatch[2]
		if s == "" {
			continue
		}
		m[s] = s2
	}
	return m
}

// GetOptimizer returns current PDF optimizer.
func (c *Creator) GetOptimizer() model.Optimizer      { return c.optimizer }
func roundTo(value float64, multiple float64) float64 { return math.Round(value/multiple) * multiple }

// GeneratePageBlocks generate the Page blocks. Draws the Image on a block, implementing the Drawable interface.
func (im *Image) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	if im.xobj == nil {
		if err := im.makeXObject(); err != nil {
			return nil, ctx, err
		}
	}
	var blocks []*Block
	ctx2 := ctx
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if im.positioning.IsRelative() {
		im.applyFitMode(ctx.Width)
		ctx.X += im.margins.Left
		ctx.Y += im.margins.Top
		ctx.Width -= im.margins.Left + im.margins.Right
		ctx.Height -= im.margins.Top + im.margins.Bottom
		if im.height > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + im.margins.Top
			ctx2.X = ctx.Margins.Left + im.margins.Left
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - im.margins.Top - im.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - im.margins.Left - im.margins.Right
			ctx = ctx2
		}
	} else {
		ctx.X = im.xPos
		ctx.Y = im.yPos
	}
	ctx, err := drawImageBlock(block, im, ctx)
	if err != nil {
		return nil, ctx, err
	}
	blocks = append(blocks, block)
	if im.positioning.IsAbsolute() {
		ctx = ctx2
	} else {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		ctx.Y += im.margins.Bottom
	}
	return blocks, ctx, nil
}

// CurCol returns the currently active cell's column number.
func (t *Table) CurCol() int { curCol := (t.curCell-1)%(t.cols) + 1; return curCol }
func (tp *templateProcessor) checkTopLevelText(templateText2 *templateText) error {
	if isBlank(templateText2.text) {
		return nil
	}
	if tp.options.Strict {
		return tp.nodeError(templateText2.pos(), "text content is not valid outside a tag")
	}
	tp.nodeLogDebug(templateText2.pos(), "Text content outside a tag. Skipping.")
	return nil
}

// Write output of creator to io.Writer interface.
func (c *Creator) Write(ws io.Writer) error {
	if err := c.Finalize(); err != nil {
		return err
	}
	name := ""
	if file, ok := ws.(*os.File); ok {
		name = file.Name()
	}
	pdfWriter := model.NewPdfWriter()
	pdfWriter.SetOptimizer(c.optimizer)
	pdfWriter.SetFileName(name)
	if c.acroForm != nil {
		err := pdfWriter.SetForms(c.acroForm)
		if err != nil {
			common.Log.Debug("Failure: %v", err)
			return err
		}
	}
	if c.outlineTree != nil {
		pdfWriter.AddOutlineTree(c.outlineTree)
	} else if c.outline != nil && c.AddOutlines {
		pdfWriter.AddOutlineTree(&c.outline.ToPdfOutline().PdfOutlineTreeNode)
	}
	if c.pageLabels != nil {
		if err := pdfWriter.SetPageLabels(c.pageLabels); err != nil {
			common.Log.Debug("ERROR: Could not set page labels: %v", err)
			return err
		}
	}
	if c.subsetFonts != nil {
		for _, subsetFont := range c.subsetFonts {
			err := subsetFont.SubsetRegistered()
			if err != nil {
				common.Log.Debug("ERROR: Could not subset font: %v", err)
				return err
			}
		}
	}
	if c.taggingEnabled && c.structTreeRoot != nil {
		pdfWriter.SetCatalogMarkInfo(core.MakeDictMap(map[string]core.PdfObject{"Marked": core.MakeBool(true)}))
	}
	if c.pdfWriterAccessFunc != nil {
		err := c.pdfWriterAccessFunc(&pdfWriter)
		if err != nil {
			common.Log.Debug("Failure: %v", err)
			return err
		}
	}
	m := make(map[int64]*core.PdfIndirectObject)
	for i, page := range c.pages {
		err := pdfWriter.AddPage(page)
		if err != nil {
			common.Log.Error("Failed to add Page: %v", err)
			return err
		}
		if c.structTreeRoot != nil {
			if c.taggingEnabled {
				page.SetStructParentsKey(i)
			}
			pageIndirectObject, err := pdfWriter.GetPageIndirectObject(i)
			if err != nil {
				common.Log.Debug("ERROR: Could not get page indirect object %v", err)
			} else {
				m[int64(i+1)] = pageIndirectObject
			}
		}
	}
	if c.structTreeRoot != nil {
		if c.structNamespace != nil && !slices.Contains(c.structTreeRoot.Namespaces, c.structNamespace) {
			c.structTreeRoot.AddNamespace(c.structNamespace)
		}
		var fn func(kDict *model.KDict, n int64)
		fn = func(kDict *model.KDict, n int64) {
			if kDict == nil {
				return
			}
			n2 := n
			if pageNumber := kDict.GetPageNumber(); pageNumber > 0 {
				n2 = pageNumber
			}
			if n2 > 0 {
				if page, ok := m[n2]; ok {
					kDict.SetPage(page)
				}
			}
			if c.structNamespace != nil {
				kDict.SetNamespace(c.structNamespace)
			}
			for _, kValue := range kDict.GetChildren() {
				if kDict := kValue.GetKDict(); kDict != nil {
					fn(kDict, n2)
				}
			}
		}
		for _, kDict := range c.structTreeRoot.K {
			fn(kDict, 0)
		}
		if err := pdfWriter.SetCatalogStructTreeRoot(c.structTreeRoot.ToPdfObject()); err != nil {
			common.Log.Debug("ERROR: Could not set StructTreeRoot: %v", err)
			return err
		}
		if c.structNamespace != nil {
			pdfWriter.SetVersion(2, 0)
		}
	}
	if c.viewerPreferences != nil {
		if err := pdfWriter.SetCatalogViewerPreferences(c.viewerPreferences.ToPdfObject()); err != nil {
			common.Log.Debug("ERROR: Could not set ViewerPreferences: %v", err)
			return err
		}
	}
	if c.language != "" {
		if err := pdfWriter.SetCatalogLanguage(core.MakeString(c.language)); err != nil {
			common.Log.Debug("ERROR: Could not set catalog language: %v", err)
			return err
		}
	}
	err := pdfWriter.Write(ws)
	if err != nil {
		return err
	}
	return nil
}

// ConvertToBinary converts current image data into binary (Bi-level image) format.
// If provided image is RGB or GrayScale the function converts it into binary image
// using histogram auto threshold method.
func (im *Image) ConvertToBinary() error { return im.img.ConvertToBinary() }

// GetAngle returns the rotation angle of the text, in degrees.
func (sp *StyledParagraph) GetAngle() float64 { return sp.angle }
func linkTextStyle(font *model.PdfFont) TextStyle {
	return TextStyle{Color: ColorRGBFrom8bit(0, 0, 238), Font: font, FontSize: 10, OutlineSize: 1, HorizontalScaling: DefaultHorizontalScaling, UnderlineStyle: TextDecorationLineStyle{Offset: 1, Thickness: 1}}
}

const (
	CellVerticalAlignmentTop CellVerticalAlignment = iota
	CellVerticalAlignmentMiddle
	CellVerticalAlignmentBottom
)

// SetNotes sets the notes section of the invoice.
func (i *Invoice) SetNotes(title, content string) { i.notes = [2]string{title, content} }

const whitespaceChars = " \t\r\n"

func newImage(img *model.Image) (*Image, error) {
	width := float64(img.Width)
	height := float64(img.Height)
	return &Image{img: img, origWidth: width, origHeight: height, width: width, height: height, angle: 0, opacity: 1.0, positioning: PositionRelative, taggedDrawable: taggedDrawable{structType: model.StructureTypeFigure}}, nil
}

// NewTextStyle creates a new text style object which can be used to style
// chunks of text.
// Default attributes:
// Font: the creator default font (Helvetica, unless changed via SetDefaultFont)
// Font size: 10
// Encoding: the font's encoding (WinAnsiEncoding for the standard fonts)
// Text color: black
func (c *Creator) NewTextStyle() TextStyle { return defaultTextStyle(c.fontRegular) }
func newListItem() *listItem               { return &listItem{} }
func (sp *StyledParagraph) getTextWidth() float64 {
	var charSpacing float64
	chunksLen := len(sp.chunks)
	for i, chunk := range sp.chunks {
		style := &chunk.Style
		textLen := len(chunk.Text)
		for j, r := range chunk.Text {
			if r == '\u000A' {
				continue
			}
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("Rune char metrics not found! %v\n", r)
				return -1
			}
			charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale()
			if r != ' ' && (i != chunksLen-1 || j != textLen-1) {
				charSpacing += style.CharSpacing * 1000.0
			}
		}
	}
	return charSpacing
}

// LineWidth returns the width of the line.
func (l *Line) LineWidth() float64 { return l.lineWidth }

// NewGrid creates a new Grid with a specified number of columns.
func (c *Creator) NewGrid(cols int) *Grid { return newGrid(cols) }

// FooterFunctionArgs holds the input arguments to a footer drawing function.
// It is designed as a struct, so additional parameters can be added in the future with backwards
// compatibility.
type FooterFunctionArgs struct {
	PageNum    int
	TotalPages int

	// Chapter describes the chapter the current page belongs to, resolved via
	// Creator.ChapterForPage. It is nil on pages that precede the first chapter.
	Chapter *ChapterInfo
}

func parseSVGPoints(s string) ([]float64, error) {
	n := -1
	var svgPoints []float64
	r2 := ' '
	for i, r := range s {
		if !unicode.IsNumber(r) && r != '.' && !(r == '-' && r2 == 'e') && r != 'e' {
			if n != -1 {
				numbers, err := parseNumberList(s[n:i])
				if err != nil {
					return svgPoints, err
				}
				svgPoints = append(svgPoints, numbers...)
			}
			if r == '-' {
				n = i
			} else {
				n = -1
			}
		} else if n == -1 {
			n = i
		}
		r2 = r
	}
	if n != -1 && n != len(s) {
		numbers, err := parseNumberList(s[n:])
		if err != nil {
			return svgPoints, err
		}
		svgPoints = append(svgPoints, numbers...)
	}
	return svgPoints, nil
}
func normalizeTemplateNumber(v interface{}) (interface{}, error) {
	switch n := v.(type) {
	case uint8:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	}
	return nil, fmt.Errorf("invalid value, %v is not a number", v)
}

// Append adds a new text chunk to the paragraph.
func (sp *StyledParagraph) Append(text string) *TextChunk {
	textChunk := NewTextChunk(text, sp.defaultStyle)
	return sp.appendChunk(textChunk)
}

// AddPage adds the specified page to the creator.
// NOTE: If the page has a Rotate flag, the creator will take care of
// transforming the contents to maintain the correct orientation.
func (c *Creator) AddPage(page *model.PdfPage) error {
	if c.AutofixPageContentStream {
		pageIfNeeded, err := c.wrapPageIfNeeded(page)
		if err != nil {
			return err
		}
		if pageIfNeeded != nil {
			page = pageIfNeeded
		}
	}
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		common.Log.Debug("Failed to get page mediabox: %v", err)
		return err
	}
	mediaBox.Normalize()
	llx, lly := mediaBox.Llx, mediaBox.Lly
	rect := mediaBox
	if cropBox := page.CropBox; cropBox != nil && *cropBox != *mediaBox {
		cropBox.Normalize()
		llx, lly = cropBox.Llx, cropBox.Lly
		rect = cropBox
	}
	identityMatrix := transform.IdentityMatrix()
	rotate, err := page.GetRotate()
	if err != nil {
		common.Log.Debug("ERROR: %s - ignoring and assuming no rotation\n", err.Error())
	}
	isRightAngle := rotate%360 != 0 && rotate%90 == 0
	if isRightAngle {
		angle := float64((360 + rotate%360) % 360)
		switch angle {
		case 90:
			identityMatrix = identityMatrix.Translate(rect.Width(), 0)
		case 180:
			identityMatrix = identityMatrix.Translate(rect.Width(), rect.Height())
		case 270:
			identityMatrix = identityMatrix.Translate(0, rect.Height())
		}
		identityMatrix = identityMatrix.Mult(transform.RotationMatrix(angle * math.Pi / 180))
		identityMatrix = identityMatrix.Round(0.000001)
		rect2 := transformRectangle(rect, identityMatrix)
		rect = rect2
		rect.Normalize()
	}
	if llx != 0 || lly != 0 {
		identityMatrix = transform.TranslationMatrix(llx, lly).Mult(identityMatrix)
	}
	if !identityMatrix.Identity() {
		identityMatrix = identityMatrix.Round(0.000001)
		c.pageTransforms[page] = &pageTransformations{matrix: &identityMatrix}
	}
	c.pageWidth = rect.Width()
	c.pageHeight = rect.Height()
	c.initContext()
	c.pages = append(c.pages, page)
	c.context.Page++
	return nil
}
func newCurve(x1, y1, cx, cy, x, y float64) *Curve {
	curve := &Curve{}
	curve.x1 = x1
	curve.y1 = y1
	curve.cx = cx
	curve.cy = cy
	curve.x2 = x
	curve.y2 = y
	curve.color = ColorBlack
	curve.width = 1.0
	return curve
}
func templateAdd(v, v2 interface{}) (interface{}, error) {
	v3, err := normalizeTemplateNumber(v)
	if err != nil {
		return nil, err
	}
	switch n := v3.(type) {
	case int64:
		v, err := normalizeTemplateNumber(v2)
		if err != nil {
			return nil, err
		}
		switch n2 := v.(type) {
		case int64:
			return n + n2, nil
		case float64:
			return float64(n) + n2, nil
		}
	case float64:
		v, err := normalizeTemplateNumber(v2)
		if err != nil {
			return nil, err
		}
		switch n2 := v.(type) {
		case int64:
			return n + float64(n2), nil
		case float64:
			return n + n2, nil
		}
	}
	return nil, fmt.Errorf("failed to add %v and %v", v, v2)
}

// SetDropCapsFirstCharacter is a convenience method to set traditional drop caps
// on the first character of the paragraph.
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This paragraph starts with a large T...")
//	p.SetDropCapsFirstCharacter(3, 5.0) // T spans 3 lines with 5pt gap
func (sp *StyledParagraph) SetDropCapsFirstCharacter(numLines int, gap float64) {
	sp.SetDropCaps(DropCapsOptions{Type: DropCapsDrop, Scope: DropCapsFirstCharacter, NumLines: numLines, Gap: gap})
}
func (fbs *FunctionBasedShading) pdfFunction() (model.PdfFunction, model.PdfColorspace, error) {
	if fbs.psCode == "" {
		return nil, nil, errors.New("no PostScript program specified")
	}
	program, err := ps.NewPSParser([]byte(fbs.psCode)).Parse()
	if err != nil {
		return nil, nil, fmt.Errorf("failed parsing PostScript program: %v", err)
	}
	functionType4 := &model.PdfFunctionType4{Domain: functionDomain, Program: program}
	value := (functionDomain[0] + functionDomain[1]) / 2
	value2 := (functionDomain[2] + functionDomain[3]) / 2
	floats2, err := functionType4.Evaluate([]float64{value, value2})
	if err != nil {
		return nil, nil, fmt.Errorf("failed evaluating PostScript program: %v", err)
	}
	colorspace, err := colorspaceForComponents(len(floats2))
	if err != nil {
		return nil, nil, err
	}
	floats := make([]float64, 0, 2*len(floats2))
	for range floats2 {
		floats = append(floats, 0, 1)
	}
	return &model.PdfFunctionType4{Domain: functionDomain, Range: floats, Program: program}, colorspace, nil
}
func (b *Block) addWrappedContents(contentStreamOperations *contentstream.ContentStreamOperations) {
	b.contents.WrapIfNeeded()
	contentStreamOperations.WrapIfNeeded()
	*b.contents = append(*b.contents, *contentStreamOperations...)
}
func isGradientExpr(s string) bool {
	s2 := strings.TrimSpace(s)
	return (strings.HasPrefix(s2, "linear-gradient(") || strings.HasPrefix(s2, "radial-gradient(")) && strings.HasSuffix(s2, ")") && len(s2) > 17
}
func (tp *templateProcessor) validateTable(node *templateNode) error {
	if _, err := tp.tableColumns(node); err != nil {
		return err
	}
	fn := func(s string) int {
		effectiveAttrValue, _ := tp.effectiveAttrValue(node, s)
		int642, _ := parseInt64(effectiveAttrValue)
		return int(int642)
	}
	return tp.setTableHeaderRows(node, &Table{}, fn("header-start-row"), fn("header-end-row"))
}
func newGraphicSVGFromString(svgStr string) (*GraphicSVG, error) {
	fromSVGString, err := ParseFromSVGString(svgStr)
	if err != nil {
		return nil, err
	}
	return newGraphicSVG(fromSVGString)
}

// ParseFromSVGStream creates a GraphicSVG instance from SVG stream input.
func ParseFromSVGStream(source io.Reader) (*GraphicSVGElement, error) {
	decoder := xml.NewDecoder(source)
	decoder.CharsetReader = charset.NewReaderLabel
	svgElementTree, err := parseSVGElementTree(decoder)
	if err != nil {
		return nil, err
	}
	if err := svgElementTree.Decode(decoder); err != nil && err != io.EOF {
		return nil, err
	}
	return svgElementTree, nil
}

type rgbColor struct{ r, g, b float64 }

func newGraphicSVGFromFile(path string) (*GraphicSVG, error) {
	fromSVGFile, err := ParseFromSVGFile(path)
	if err != nil {
		return nil, err
	}
	return newGraphicSVG(fromSVGFile)
}

// SetDropCaps sets drop caps options for the paragraph.
// The drop caps will be applied during text wrapping and rendering.
//
// Note: Drop caps are currently not supported for RTL (right-to-left) text.
// If applied to RTL text, the feature will be automatically disabled during rendering
// with a debug log notification.
//
// Example:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This is a paragraph with a beautiful drop cap...")
//	p.SetDropCaps(creator.DropCapsOptions{
//	    Type:     creator.DropCapsDrop,
//	    Scope:    creator.DropCapsFirstCharacter,
//	    NumLines: 3,
//	    Gap:      5.0,
//	})
//
// Example with custom style:
//
//	p := c.NewStyledParagraph()
//	p.SetText("This is a paragraph with a styled drop cap...")
//	dropCapStyle := creator.TextStyle{
//	    Font:  boldFont,
//	    Color: creator.ColorRGBFrom8bit(200, 0, 0), // Red
//	}
//	p.SetDropCaps(creator.DropCapsOptions{
//	    Type:     creator.DropCapsDrop,
//	    Scope:    creator.DropCapsFirstCharacter,
//	    NumLines: 3,
//	    Gap:      5.0,
//	    Style:    &dropCapStyle,
//	})
func (sp *StyledParagraph) SetDropCaps(options DropCapsOptions) {
	if options.Type == DropCapsNone {
		sp.dropCaps = nil
		return
	}
	if options.NumLines <= 0 {
		options.NumLines = 3
	}
	if options.Gap <= 0 {
		options.Gap = 5.0
	}
	sp.dropCaps = &options
}

// NewPageBreak create a new page break.
func (c *Creator) NewPageBreak() *PageBreak { return newPageBreak() }

// Heading returns the heading component of the table of contents.
func (to *TOC) Heading() *StyledParagraph { return to.heading }

// AddressHeadingStyle returns the style properties used to render the
// heading of the invoice address sections.
func (i *Invoice) AddressHeadingStyle() TextStyle { return i.headingStyle }
func collectStructureTags(drawable Drawable, structureTags []*model.StructureTagInfo) []*model.StructureTagInfo {
	var tagInfo *model.StructureTagInfo
	var drawables []VectorDrawable
	switch table := drawable.(type) {
	case *Table:
		tagInfo = table.tagInfo
		for _, cell := range table.cells {
			drawables = append(drawables, cell.content)
		}
	case *List:
		tagInfo = table.tagInfo
		for _, item := range table.items {
			drawables = append(drawables, item.content)
		}
	case *Division:
		tagInfo = table.tagInfo
		drawables = table.components
	case *Grid:
		tagInfo = table.tagInfo
		for _, row := range table.rows {
			for _, cell := range row.cells {
				drawables = append(drawables, cell.content)
			}
		}
	}
	if tagInfo != nil && tagInfo.ApplyTag {
		structureTags = append(structureTags, tagInfo)
	}
	for _, drawable := range drawables {
		if drawable != nil {
			structureTags = collectStructureTags(drawable, structureTags)
		}
	}
	return structureTags
}

// GetRowspan returns the number of rows the cell spans.
func (tc *TableCell) GetRowspan() int { return tc.rowSpan }

// TextChunk represents a chunk of text along with a particular style.
type TextChunk struct {
	taggedDrawable

	// The text that is being rendered in the PDF.
	Text string

	// The style of the text being rendered.
	Style       TextStyle
	annotations []*model.PdfAnnotation
	lineBreaks  []bool

	// The vertical alignment of the text chunk.
	VerticalAlignment TextVerticalAlignment
	textExpansion     *string
	actualText        *string
	altText           *string
}

// SetFillOpacity sets the fill opacity.
func (p *Polygon) SetFillOpacity(opacity float64) { p.fillOpacity = opacity }

// SetLineNumberStyle sets the style for the numbers part of all new lines
// of the table of contents.
func (to *TOC) SetLineNumberStyle(style TextStyle) { to.lineNumberStyle = style }

// SetLineWidth sets the line width.
func (l *Line) SetLineWidth(width float64) { l.lineWidth = width }

// Inline returns whether the inline mode of the division is active.
func (d *Division) Inline() bool { return d.inline }

// RadialShading holds information that will be used to render a radial shading.
type RadialShading struct {
	base        *shading
	boundingBox *model.PdfRectangle
	anchor      AnchorPoint
	x0          float64
	y0          float64
	r0          float64
	r1          float64
}

// Date returns the invoice date description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Date() (*InvoiceCell, *InvoiceCell) { return i.date[0], i.date[1] }

// GenerateKDict generates a K dictionary for the drawable component.
func (td *taggedDrawable) GenerateKDict() (*model.KDict, error) {
	if td.tagInfo == nil {
		return nil, errors.New("structure tag info is not set")
	}
	return td.tagInfo.GenerateKDict(), nil
}

// GetArtifact returns the artifact associated with the block, if any.
func (b *Block) GetArtifact() *model.Artifact { return b.artifact }
func newGrid(cols int) *Grid {
	grid := &Grid{cols: cols, defaultRowHeight: 10.0, colWidths: []float64{}}
	grid.structType = model.StructureTypeTable
	grid.resetColumnWidths()
	return grid
}

// GeneratePageBlocks draws the block contents on a template Page block.
// Implements the Drawable interface.
func (b *Block) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	identityMatrix := transform.IdentityMatrix()
	width, height := b.Width(), b.Height()
	if b.positioning.IsRelative() {
		identityMatrix = identityMatrix.Translate(ctx.X, ctx.PageHeight-ctx.Y-height)
	} else {
		identityMatrix = identityMatrix.Translate(b.xPos, ctx.PageHeight-b.yPos-height)
	}
	y := height
	if b.angle != 0 {
		identityMatrix = identityMatrix.Translate(width/2, height/2).Rotate(b.angle*math.Pi/180.0).Translate(-width/2, -height/2)
		_, y = b.RotatedSize()
	}
	if b.positioning.IsRelative() {
		ctx.Y += y
	}
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Add_cm(identityMatrix[0], identityMatrix[1], identityMatrix[3], identityMatrix[4], identityMatrix[6], identityMatrix[7])
	duplicate := b.duplicate()
	if b.background != nil {
		resources := *b.resources
		if pdfObjectDictionary, ok := b.resources.Pattern.(*core.PdfObjectDictionary); ok {
			resources.Pattern = core.MakeDict().Merge(pdfObjectDictionary)
		}
		if pdfObjectDictionary, ok := b.resources.Shading.(*core.PdfObjectDictionary); ok {
			resources.Shading = core.MakeDict().Merge(pdfObjectDictionary)
		}
		if pdfObjectDictionary, ok := b.resources.ExtGState.(*core.PdfObjectDictionary); ok {
			resources.ExtGState = core.MakeDict().Merge(pdfObjectDictionary)
		}
		duplicate.resources = &resources
		background, err := renderBackground(b.background, width, height)
		if err != nil {
			return nil, ctx, err
		}
		contentStreamOperations := &contentstream.ContentStreamOperations{}
		if err := mergeContentStreams(contentStreamOperations, duplicate.resources, background.contents, background.resources); err != nil {
			return nil, ctx, err
		}
		*duplicate.contents = append(*contentStreamOperations, *duplicate.contents...)
	}
	contentStreamOperations := append(*contentCreator.Operations(), *duplicate.contents...)
	contentStreamOperations.WrapIfNeeded()
	duplicate.contents = &contentStreamOperations
	if b.artifact != nil {
		contentCreator := contentstream.NewContentCreator()
		if m := b.artifact.GenerateMap(); len(m) > 0 {
			contentCreator.Add_BDC(*core.MakeName("Artifact"), m)
		} else {
			contentCreator.Add_BMC(*core.MakeName("Artifact"))
		}
		contentStreamOperations := append(*contentCreator.Operations(), *duplicate.contents...)
		contentCreator2 := contentstream.NewContentCreator()
		contentCreator2.Add_EMC()
		contentStreamOperations = append(contentStreamOperations, *contentCreator2.Operations()...)
		contentStreamOperations.WrapIfNeeded()
		duplicate.contents = &contentStreamOperations
	}
	for _, annotation := range b.annotations {
		arr, ok := core.GetArray(annotation.Rect)
		if !ok || arr.Len() != 4 {
			common.Log.Debug("WARN: invalid annotation Rect field: %v. Output may be incorrect.", annotation.Rect)
			continue
		}
		pdfRectangle, err := model.NewPdfRectangle(*arr)
		if err != nil {
			common.Log.Debug("WARN: could not parse annotation Rect field: %v. Output may be incorrect.", err)
			continue
		}
		pdfRectangle.Transform(identityMatrix)
		annotation.Rect = pdfRectangle.ToPdfObject()
	}
	return []*Block{duplicate}, ctx, nil
}

// NewCurve returns new instance of Curve between points (x1,y1) and (x2, y2) with control point (cx,cy).
func (c *Creator) NewCurve(x1, y1, cx, cy, x2, y2 float64) *Curve {
	return newCurve(x1, y1, cx, cy, x2, y2)
}

// NewTextChunk returns a new text chunk instance.
func NewTextChunk(text string, style TextStyle) *TextChunk {
	return &TextChunk{Text: text, Style: style, VerticalAlignment: TextVerticalAlignmentBaseline, annotations: []*model.PdfAnnotation{}, lineBreaks: []bool{}, taggedDrawable: taggedDrawable{structType: model.StructureTypeSpan}}
}

// CurvePolygon represents a curve polygon shape.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type CurvePolygon struct {
	taggedDrawable
	polygon       *contentdraw.CurvePolygon
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

const (
	TextVerticalAlignmentBaseline TextVerticalAlignment = iota
	TextVerticalAlignmentCenter
	TextVerticalAlignmentBottom
	TextVerticalAlignmentTop
)

// SetMargins sets the margins of the line.
// NOTE: line margins are only applied if relative positioning is used.
func (l *Line) SetMargins(left, right, top, bottom float64) {
	l.margins.Left = left
	l.margins.Right = right
	l.margins.Top = top
	l.margins.Bottom = bottom
}

// ScaleToHeight scales the ellipse to the specified height. The width of
// the ellipse is scaled so that the aspect ratio is maintained.
func (e *Ellipse) ScaleToHeight(h float64) {
	width := e.width / e.height
	e.height = h
	e.width = h * width
}
func parsePageBreakTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parsePageBreak(node)
}

var newlineWhitespaceRe = regexp.MustCompile("[ \\t\\r\\n]*\\n[ \\t\\r\\n]*")

func hasTextChunk(v interface{}) bool {
	switch templateText2 := v.(type) {
	case *templateText:
		return !isBlank(templateText2.text)
	case *templateNode:
		switch templateText2.start.Name.Local {
		case styleElementName:
			return false
		case "text-chunk":
			return true
		}
		for _, childValue := range templateText2.childValues {
			if hasTextChunk(childValue) {
				return true
			}
		}
	}
	return false
}
func newList(style TextStyle) *List {
	return &List{marker: TextChunk{Text: "• ", Style: style}, indent: 0, defaultIndent: true, positioning: PositionRelative, textStyle: style, taggedDrawable: taggedDrawable{structType: model.StructureTypeList}}
}
func (tc *TableCell) cloneProps(content VectorDrawable) *TableCell {
	props := *tc
	props.content = content
	return &props
}

// Width returns the current page width.
func (c *Creator) Width() float64 { return c.pageWidth }

// GetIndent get the cell's left indent.
func (tc *TableCell) GetIndent() float64 { return tc.indent }
func drawImageBlock(block *Block, img *Image, imageBlock DrawContext) (DrawContext, error) {
	imageBlock2 := imageBlock
	n := 1
	keyName := core.PdfObjectName(fmt.Sprintf("Img%d", n))
	for block.resources.HasXObjectByName(keyName) {
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("Img%d", n))
	}
	err := block.resources.SetXObjectImageByNameLazy(keyName, img.xobj, img.lazy)
	if err != nil {
		return imageBlock, err
	}
	n2 := 0
	gsName := core.PdfObjectName(fmt.Sprintf("GS%d", n2))
	for block.resources.HasExtGState(gsName) {
		n2++
		gsName = core.PdfObjectName(fmt.Sprintf("GS%d", n2))
	}
	dict := core.MakeDict()
	dict.Set("BM", core.MakeName("Normal"))
	if img.opacity < 1.0 {
		dict.Set("CA", core.MakeFloat(img.opacity))
		dict.Set("ca", core.MakeFloat(img.opacity))
	}
	err = block.resources.AddExtGState(gsName, core.MakeIndirectObject(dict))
	if err != nil {
		return imageBlock, err
	}
	width := img.Width()
	height := img.Height()
	_, y := img.rotatedSize()
	x := imageBlock.X
	ty := imageBlock.PageHeight - imageBlock.Y - height
	if img.positioning.IsRelative() {
		ty -= (y - height) / 2
		switch img.horizontalAlignment {
		case HorizontalAlignmentCenter:
			x += (imageBlock.Width - width) / 2
		case HorizontalAlignmentRight:
			x = imageBlock.PageWidth - imageBlock.Margins.Right - img.margins.Right - width
		}
	}
	angle := img.angle
	if img.artifact == nil && img.tagInfo != nil && img.tagInfo.ComponentKObj != nil {
		width2, height2 := img.rotatedSize()
		x2 := x + width/2
		ty2 := ty + height/2
		img.tagInfo.ComponentKObj.SetBoundingBox(x2-width2/2, ty2-height2/2, width2, height2)
	}
	contentCreator := contentstream.NewContentCreator()
	if img.artifact == nil {
		if img.tagInfo != nil {
			contentCreator.Add_BDC(*core.MakeName(string(img.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(img.tagInfo.Mcid)})
		}
	} else {
		m := img.artifact.GenerateMap()
		if len(m) > 0 {
			contentCreator.Add_BDC(*core.MakeName(string("Artifact")), m)
		} else {
			contentCreator.Add_BMC(*core.MakeName(string("Artifact")))
		}
	}
	contentCreator.Add_gs(gsName)
	contentCreator.Translate(x, ty)
	if angle != 0 {
		contentCreator.Translate(width/2, height/2)
		contentCreator.RotateDeg(angle)
		contentCreator.Translate(-width/2, -height/2)
	}
	contentCreator.Scale(width, height).Add_Do(keyName)
	if img.tagInfo != nil || img.artifact != nil {
		contentCreator.Add_EMC()
	}
	operations := contentCreator.Operations()
	operations.WrapIfNeeded()
	block.addWrappedContents(operations)
	if img.positioning.IsRelative() {
		imageBlock.Y += y
		imageBlock.Height -= y
		return imageBlock, nil
	}
	return imageBlock2, nil
}
func isVariationSelector(r rune) bool {
	return (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}

// NewCell makes a new cell and inserts it into the table at the current position.
func (t *Table) NewCell() *TableCell { return t.MultiCell(1, 1) }
func (t *Table) split(ctx DrawContext, value float64) (head, tail *Table) {
	headerCut := t.maxFittingRows(ctx.Width, ctx.Height)
	if !t.enablePageWrap && t.Height()+t.margins.Top+t.margins.Bottom <= value {
		return nil, t
	}
	if withPartialRow := t.withPartialRow(headerCut, ctx, value); withPartialRow != nil {
		return withPartialRow.splitAtRow(headerCut + 1)
	}
	return t.splitAtRow(t.headerCut(headerCut))
}

// SetExtends specifies whether to extend the shading beyond the starting and ending points.
//
// Text extends is set to `[]bool{false, false}` by default.
func (s *shading) SetExtends(start bool, end bool) { s.extends = []bool{start, end} }
func parseFloat64(s string) (float64, error)       { return strconv.ParseFloat(s, 64) }

// SetAngle sets Image rotation angle in degrees.
func (im *Image) SetAngle(angle float64) { im.angle = angle }
func newFunctionBasedShading(psCode string) *FunctionBasedShading {
	return &FunctionBasedShading{base: &shading{antiAlias: false}, psCode: psCode, boundingBox: &model.PdfRectangle{}}
}
func decoderInputPos(decoder *xml.Decoder) (int, int) { return decoder.InputPos() }

// Margins returns the margins of the list: left, right, top, bottom.
func (li *List) Margins() (float64, float64, float64, float64) {
	return li.margins.Left, li.margins.Right, li.margins.Top, li.margins.Bottom
}
func (im *Image) makeXObject() error {
	mask := im.mask
	var img2 *model.Image
	if mask != nil && mask.kind == imageMaskOptimizedAlpha {
		var ok bool
		if img2, ok = alphaToBitmask(im.img); !ok {
			common.Log.Debug("Image alpha channel is no longer binary; keeping it as an SMask")
			mask = nil
		}
	}
	if mask == nil {
		xObjectImage, err := model.NewXObjectImageFromImageLazy(im.img, nil, im.encoder, im.lazy)
		if err != nil {
			common.Log.Error("Failed to create xobject image: %s", err)
			return err
		}
		im.xobj = xObjectImage
		return nil
	}
	img := im.img
	encoder := im.encoder
	if len(im.img.AlphaData()) > 0 {
		img2 := *im.img
		img2.SetAlpha(nil)
		img = &img2
		if encoder == nil {
			encoder = core.NewFlateEncoder()
		}
	}
	if mask.kind == imageMaskColorKey && encoder == nil && !im.img.CanPassThroughJPEG() {
		encoder = core.NewFlateEncoder()
	}
	xObjectImage, err := model.NewXObjectImageFromImageLazy(img, nil, encoder, false)
	if err != nil {
		common.Log.Error("Failed to create xobject image: %s", err)
		return err
	}
	if err := im.applyMask(xObjectImage, img2); err != nil {
		return err
	}
	if im.lazy {
		if err := xObjectImage.MakeLazy(); err != nil {
			return err
		}
	}
	im.xobj = xObjectImage
	return nil
}

var emptyAttrs = map[string]attrApplyFunc{}

func (tn *templateNode) componentParent() *templateNode {
	for parent := tn.parent; parent != nil; parent = parent.parent {
		if parent.value != nil {
			return parent
		}
	}
	return nil
}
func (tp *templateProcessor) effectiveParentName(node *templateNode) string {
	if activeParent := node.activeParent(); activeParent != nil {
		return activeParent.start.Name.Local
	}
	if _, ok := tp.renderer.(*Block); ok {
		return "block"
	}
	return "creator"
}

// Height returns the height of a single line of this chunk: FontSize × effective
// line height. When Style.LineHeight is 0 (the default), the effective line
// height is 1.0. To account for the line height set on a containing paragraph,
// callers should either propagate it into Style.LineHeight or multiply the
// result accordingly.
func (tec *TextChunk) Height() float64 {
	return tec.Style.FontSize * tec.Style.effectiveLineHeight(1.0)
}

// ChapterForPage returns the chapter the given 1-based page number belongs to.
// The chapter returned is the one whose heading was most recently laid out at
// or before pageNum - when a subchapter heading appears on the same page as
// (or after) its parent's heading, the subchapter is returned, giving the
// innermost section the reader is currently in. Returns nil if no chapter
// heading has appeared by pageNum (e.g. front matter).
//
// ChapterForPage is populated as chapters are drawn via Creator.Draw.
//
// Page numbering depends on when ChapterForPage is called. Before Finalize
// (e.g. from a custom-TOC callback), pageNum refers to pages as drawn by
// Creator.Draw - any front-page or TOC pages have not yet been prepended.
// After Finalize runs (including from within a header, footer, or
// page-finalize callback), pageNum refers to the final assembled document,
// with front-page and TOC pages counted. Chapters drawn inside a
// CreateFrontPage or CreateTableOfContents callback are not tracked.
func (c *Creator) ChapterForPage(pageNum int) *ChapterInfo {
	for i := len(c.chapters) - 1; i >= 0; i-- {
		if c.chapters[i].page <= pageNum {
			chapterInfo := c.chapters[i].chapter.chapterInfo()
			return &chapterInfo
		}
	}
	return nil
}
func setFirstError(err2 *error, err error) {
	if *err2 == nil {
		*err2 = err
	}
}
func (ls *LinearShading) shadingModel() *model.PdfShadingType2 {
	point := contentdraw.NewPoint(ls.boundingBox.Llx+ls.boundingBox.Width()/2, ls.boundingBox.Lly+ls.boundingBox.Height()/2)
	point2 := contentdraw.NewPoint(ls.boundingBox.Llx, ls.boundingBox.Lly+ls.boundingBox.Height()/2).Add(-point.X, -point.Y).Rotate(ls.angle).Add(point.X, point.Y)
	point2 = contentdraw.NewPoint(math.Max(math.Min(point2.X, ls.boundingBox.Urx), ls.boundingBox.Llx), math.Max(math.Min(point2.Y, ls.boundingBox.Ury), ls.boundingBox.Lly))
	point3 := contentdraw.NewPoint(ls.boundingBox.Urx, ls.boundingBox.Lly+ls.boundingBox.Height()/2).Add(-point.X, -point.Y).Rotate(ls.angle).Add(point.X, point.Y)
	point3 = contentdraw.NewPoint(math.Min(math.Max(point3.X, ls.boundingBox.Llx), ls.boundingBox.Urx), math.Min(math.Max(point3.Y, ls.boundingBox.Lly), ls.boundingBox.Ury))
	pdfShadingType2 := model.NewPdfShadingType2()
	pdfShadingType2.ShadingType = core.MakeInteger(2)
	pdfShadingType2.ColorSpace = model.NewPdfColorspaceDeviceRGB()
	pdfShadingType2.AntiAlias = core.MakeBool(ls.base.antiAlias)
	pdfShadingType2.Coords = core.MakeArrayFromFloats([]float64{point2.X, point2.Y, point3.X, point3.Y})
	pdfShadingType2.Extend = core.MakeArray(core.MakeBool(ls.base.extends[0]), core.MakeBool(ls.base.extends[1]))
	pdfShadingType2.Function = ls.base.generatePdfFunctions()
	return pdfShadingType2
}

// Height returns the height of the rectangle.
// NOTE: the returned value does not include the border width of the rectangle.
func (r *Rectangle) Height() float64 { return r.height }

// SetStyle sets the style for all the line components: number, title,
// separator, page.
func (tl *TOCLine) SetStyle(style TextStyle) {
	tl.Number.Style = style
	tl.Title.Style = style
	tl.Separator.Style = style
	tl.Page.Style = style
}
func (c *Creator) getActivePage() *model.PdfPage {
	if c.activePage == nil {
		if len(c.pages) == 0 {
			return nil
		}
		return c.pages[len(c.pages)-1]
	}
	return c.activePage
}

// AddInternalLink adds a new internal link to the paragraph.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y
// coordinates. Position 0, 0 is at the top left of the page.
// The zoom of the destination page is controlled with the zoom
// parameter. Pass in 0 to keep the current zoom value.
func (sp *StyledParagraph) AddInternalLink(text string, page int64, x, y, zoom float64) *TextChunk {
	textChunk := NewTextChunk(text, sp.linkStyle)
	textChunk.AddAnnotation(newInternalLinkAnnotation(page-1, x, y, zoom, ""))
	return sp.appendChunk(textChunk)
}

// GenerateKDict generates a K dictionary for the image, wiring the alternate
// text (see SetAlternateText) into the Figure structure element's /Alt entry.
func (im *Image) GenerateKDict() (*model.KDict, error) {
	kDict, err := im.taggedDrawable.GenerateKDict()
	if err != nil {
		return nil, err
	}
	if kDict != nil && im.altText != "" {
		kDict.Alt = core.MakeEncodedString(im.altText, true)
	}
	if kDict != nil && im.tagInfo != nil {
		im.tagInfo.ComponentKObj = kDict
	}
	return kDict, nil
}

// AddTag adds the table tag to the root K object.
func (t *Table) AddTag(rootKObj *model.KDict) {
	if rootKObj == nil {
		common.Log.Debug("Table: AddTag called with nil rootKObj, skipping tagging.")
		return
	}
	if t.tagInfo == nil {
		t.tagInfo = model.NewStructureTagInfo()
		t.tagInfo.StructureType = t.structType
	}
	t.tagInfo.ApplyTag = true
	t.tagInfo.ParentKObj = rootKObj
	t.tagInfo.ComponentKObj = t.tagInfo.NewComponentKObj(t.tagInfo.StructureType)
	t.tagInfo.ParentKObj.AddKChild(t.tagInfo.ComponentKObj)
}

// ChapterInfo describes the chapter a page belongs to. It is surfaced to the
// header, footer, and page-finalize callbacks so they can render chapter-aware
// content such as a header like "1.2 Overview".
type ChapterInfo struct {

	// Number is the dotted chapter number (e.g. "1" for a top-level chapter,
	// "1.2.3" for a nested subchapter). Unlike the internal heading text, it
	// has no trailing dot.
	Number string

	// Title is the chapter title as supplied to NewChapter/NewSubchapter,
	// without any number prefix.
	Title string

	// Level is the depth in the chapter hierarchy: 1 for a top-level chapter,
	// 2 or more for nested subchapters.
	Level uint
}

// TotalLines returns all the rows in the invoice totals table as
// description-value cell pairs.
func (i *Invoice) TotalLines() [][2]*InvoiceCell {
	rows := [][2]*InvoiceCell{i.subtotal}
	rows = append(rows, i.totalLines...)
	return append(rows, i.total)
}

// SetLink makes the line an internal link.
// The text parameter represents the text that is displayed.
// The user is taken to the specified page, at the specified x and y
// coordinates. Position 0, 0 is at the top left of the page.
func (tl *TOCLine) SetLink(page int64, x, y float64) {
	tl.linkX = x
	tl.linkY = y
	tl.linkPage = page
	color := tl.paragraph.linkStyle.Color
	tl.Number.Style.Color = color
	tl.Title.Style.Color = color
	tl.Separator.Style.Color = color
	tl.Page.Style.Color = color
}

// NewTable create a new Table with a specified number of columns.
func (c *Creator) NewTable(cols int) *Table { return newTable(cols) }

// AddAnnotation adds an annotation on a TextChunk.
func (tec *TextChunk) AddAnnotation(annotation *model.PdfAnnotation) {
	if annotation == nil {
		return
	}
	tec.annotations = append(tec.annotations, annotation)
}

// Creator is a wrapper around functionality for creating PDF reports and/or adding new
// content onto imported PDF pages, etc.
type Creator struct {

	// Errors keeps error messages that should not interrupt pdf processing and to be checked later.
	Errors []error

	// UnsupportedCharacterReplacement is character that will be used to replace unsupported glyph.
	// The value will be passed to drawing context.
	UnsupportedCharacterReplacement rune
	pages                           []*model.PdfPage
	blocks                          map[*model.PdfPage]*Block
	pageTransforms                  map[*model.PdfPage]*pageTransformations
	activePage                      *model.PdfPage
	pageSize                        PageSize
	context                         DrawContext
	pageMargins                     Margins
	pageWidth, pageHeight           float64
	chapterCount                    int
	chapters                        []chapterInfoEntry
	genFrontPageFunc                func(frontpageFunctionArgs FrontpageFunctionArgs)
	genTOCFunc                      func(toc *TOC) error
	drawHeaderFunc                  func(block *Block, headerFunctionArgs HeaderFunctionArgs)
	drawFooterFunc                  func(block *Block, footerFunctionArgs FooterFunctionArgs)
	pageFinalizeFunc                func(pageFinalizeFunctionArgs PageFinalizeFunctionArgs) error
	pdfWriterAccessFunc             func(writer *model.PdfWriter) error
	finalized                       bool

	// Controls whether a table of contents will be generated.
	AddTOC bool

	// CustomTOC specifies if the TOC is rendered by the user.
	// When the `CustomTOC` field is set to `true`, the default TOC component is not rendered.
	// Instead the TOC is drawn by the user, in the callback provided to
	// the `Creator.CreateTableOfContents` method.
	// If `CustomTOC` is set to `false`, the callback provided to
	// `Creator.CreateTableOfContents` customizes the style of the automatically generated TOC component.
	CustomTOC bool
	toc       *TOC

	// Controls whether outlines will be generated.
	AddOutlines       bool
	outline           *model.Outline
	outlineTree       *model.PdfOutlineTreeNode
	acroForm          *model.PdfAcroForm
	pageLabels        core.PdfObject
	optimizer         model.Optimizer
	subsetFonts       []*model.PdfFont
	fontRegular       *model.PdfFont
	fontBold          *model.PdfFont
	taggingEnabled    bool
	docKDict          *model.KDict
	pageMCIDs         map[int64]int64
	structTreeRoot    *model.StructTreeRoot
	structNamespace   *model.PdfStructureNamespace
	viewerPreferences *model.ViewerPreferences
	language          string

	// AutofixPageContentStream indicates whether the creator should attempt to fix
	// page content streams that have unclosed `q` and `Q` commands.
	// The default value is `true`.
	// When set to `false`, the creator will skip the content stream checking and wrapping.
	// This will speed up and optimize memory usage the creation of PDF, the drawback is
	// need to ensure that the source of PDF content streams are well-formed.
	AutofixPageContentStream bool
}

func (ppe pathParserError) Error() string { return ppe.msg }

// LevelOffset returns the amount of space an indentation level occupies.
func (tl *TOCLine) LevelOffset() float64 { return tl.levelOffset }

// GetDropCapsOptions returns the drop caps options set on the paragraph, if any.
func (sp *StyledParagraph) GetDropCapsOptions() *DropCapsOptions { return sp.dropCaps }

// SetActualText sets the actual text for the text chunk.
func (tec *TextChunk) SetActualText(text string) { tec.actualText = &text }

// New creates a new instance of the PDF Creator.
func New() *Creator {
	const trackName = "creator.New"
	c := &Creator{}
	c.pages = []*model.PdfPage{}
	c.blocks = map[*model.PdfPage]*Block{}
	c.pageTransforms = map[*model.PdfPage]*pageTransformations{}
	c.pageMCIDs = map[int64]int64{}
	c.SetPageSize(PageSizeLetter)
	pageWidth := 0.1 * c.pageWidth
	c.pageMargins.Left = pageWidth
	c.pageMargins.Right = pageWidth
	c.pageMargins.Top = pageWidth
	c.pageMargins.Bottom = pageWidth
	var err error
	c.fontRegular, err = model.NewStandard14Font(model.HelveticaName)
	if err != nil {
		c.fontRegular = model.DefaultFont()
	}
	c.fontBold, err = model.NewStandard14Font(model.HelveticaBoldName)
	if err != nil {
		c.fontBold = model.DefaultFont()
	}
	c.toc = c.NewTOC("Table of Contents")
	c.AddOutlines = true
	c.outline = model.NewOutline()
	c.AutofixPageContentStream = true
	license.TrackUse(trackName)
	return c
}
func (li *List) ctxHeight(width float64) float64 {
	itemWidth := li.itemWidth(width, li.markerWidth())
	var ctxHeight float64
	for _, item := range li.items {
		ctxHeight += item.ctxHeight(itemWidth)
	}
	return ctxHeight
}
func (tp *templateProcessor) validateCarrierValue(node *templateNode, s2, s string) error {
	if !tp.options.Strict {
		return nil
	}
	if err := tp.typographyValueErr(s2, s); err != nil {
		return tp.nodeError(node, "invalid value %q for attribute %q: %v", s, s2, err)
	}
	return nil
}

// MultiCell makes a new cell with the specified row span and col span
// and inserts it into the table at the current position.
func (t *Table) MultiCell(rowspan, colspan int) *TableCell {
	t.curCell++
	col := (t.moveToNextAvailableCell()-1)%(t.cols) + 1
	row := (t.curCell-1)/t.cols + 1
	for row > t.rows {
		t.rows++
		t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
	}
	cell := &TableCell{}
	cell.row = row
	cell.col = col
	cell.structType = model.StructureTypeTableData
	cell.tagInfo = model.NewStructureTagInfo()
	cell.tagInfo.StructureType = model.StructureTypeTableData
	cell.indent = 5
	cell.borderStyleLeft = CellBorderStyleNone
	cell.borderLineStyle = contentdraw.LineStyleSolid
	cell.horizontalAlignment = CellHorizontalAlignmentLeft
	cell.verticalAlignment = CellVerticalAlignmentTop
	cell.borderWidthLeft = 0
	cell.borderWidthBottom = 0
	cell.borderWidthRight = 0
	cell.borderWidthTop = 0
	borderColorLeft := ColorBlack
	cell.borderColorLeft = borderColorLeft
	cell.borderColorBottom = borderColorLeft
	cell.borderColorRight = borderColorLeft
	cell.borderColorTop = borderColorLeft
	cell.opacity = 1.0
	if rowspan < 1 {
		common.Log.Debug("Table: cell rowspan less that 1 (%d). Setting cell rowspan to 1.", rowspan)
		rowspan = 1
	}
	rows := t.rows - (cell.row - 1)
	if rowspan > rows {
		common.Log.Debug("Table: cell rowspan (%d) exceeds remaining rows (%d). Adding rows.", rowspan, rows)
		t.rows += rowspan - rows
		for i := 0; i < rowspan-rows; i++ {
			t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
		}
	}
	for i := 0; i < colspan && col+i-1 < len(t.rowSpanRemaining); i++ {
		t.rowSpanRemaining[col+i-1] = rowspan - 1
	}
	cell.rowSpan = rowspan
	if colspan < 1 {
		common.Log.Debug("Table: cell colspan less than 1 (%d). Setting cell colspan to 1.", colspan)
		colspan = 1
	}
	cols := t.cols - (cell.col - 1)
	if colspan > cols {
		common.Log.Debug("Table: cell colspan (%d) exceeds remaining row cols (%d). Adjusting colspan.", colspan, cols)
		colspan = cols
	}
	cell.colSpan = colspan
	t.curCell += colspan - 1
	t.cells = append(t.cells, cell)
	cell.table = t
	return cell
}
func (t *Table) withPartialRow(n int, ctx2 DrawContext, value float64) *Table {
	n2 := n + 1
	if n2 > t.rows || n2-1 >= len(t.rowHeights) {
		return nil
	}
	if t.hasHeaderRows && n2 >= t.headerStartRow && n2 <= t.headerEndRow {
		return nil
	}
	if !t.enableRowWrap && t.rowHeights[n2-1] <= value-t.headerHeight()-t.margins.Top-t.margins.Bottom {
		return nil
	}
	ctx := ctx2
	ctx.Width = ctx2.Width - t.margins.Left - t.margins.Right
	ctx.Height = ctx2.Height - t.margins.Top - t.margins.Bottom
	for i := 0; i < n; i++ {
		ctx.Height -= t.rowHeights[i]
	}
	if ctx.Height <= 0 {
		return nil
	}
	withPartialRow := t.clone()
	withPartialRow.enableRowWrap = true
	withPartialRow.sortCells()
	i2 := -1
	for i, cell := range withPartialRow.cells {
		if cell.row == n2 {
			i2 = i
			break
		}
	}
	if i2 < 0 {
		return nil
	}
	row, err := withPartialRow.wrapRow(i2, ctx, ctx.Width, value-t.margins.Top-t.margins.Bottom-t.headerHeight())
	if err != nil || !row || withPartialRow.rows == t.rows {
		return nil
	}
	for _, cell := range withPartialRow.cells {
		if cell.row == n2 && cell.content != nil {
			withPartialRow.enableRowWrap = t.enableRowWrap
			return withPartialRow
		}
	}
	return nil
}

type taggedDrawable struct {
	structType model.StructureType
	tagInfo    *model.StructureTagInfo
}

func copyAnnotation(annotation *model.PdfAnnotation) *model.PdfAnnotation {
	if annotation == nil {
		return nil
	}
	var pdfAnnotation *model.PdfAnnotation
	switch context := annotation.GetContext().(type) {
	case *model.PdfAnnotationLink:
		if linkAnnotation := copyLinkAnnotation(context); linkAnnotation != nil {
			pdfAnnotation = linkAnnotation.PdfAnnotation
		}
	case *model.PdfAnnotationHighlight:
		if highlightAnnotation := copyHighlightAnnotation(context); highlightAnnotation != nil {
			pdfAnnotation = highlightAnnotation.PdfAnnotation
		}
	}
	return pdfAnnotation
}

// SetEnableWrap sets the line wrapping enabled flag.
func (sp *StyledParagraph) SetEnableWrap(enableWrap bool) {
	sp.enableWrap = enableWrap
	sp.defaultWrap = false
}

type templateProcessor struct {
	creator     *Creator
	data        []byte
	options     *TemplateOptions
	renderer    componentRenderer
	filename    string
	classStyles map[string][]xml.Attr
	fontCache   map[string]*model.PdfFont
}

const (
	xmlEscapeFuncName = "__xmlescape"
	rawFuncName       = "raw"
)

func parseChartTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseChart(node)
}

var styledTextTags = map[string]struct{}{"paragraph": struct{}{}, "text-chunk": struct{}{}, "chapter-heading": struct{}{}, "list-marker": struct{}{}}

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
func (e *Ellipse) applyFitMode(left float64) {
	left -= e.margins.Left + e.margins.Right
	switch e.fitMode {
	case FitModeFillWidth:
		e.ScaleToWidth(left)
	}
}

// SetWidth set the Image's document width to specified w. This does not change the raw image data, i.e.
// no actual scaling of data is performed. That is handled by the PDF viewer.
func (im *Image) SetWidth(w float64) { im.width = w }

// GeneratePageBlocks draws the chart onto a block.
func (cha *Chart) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	isRelative := cha.positioning.IsRelative()
	var blocks []*Block
	if isRelative {
		value := 1.0
		top := cha.margins.Top
		if float64(cha.chart.Height()) > ctx.Height-cha.margins.Top {
			blocks = []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y)}
			var err error
			if _, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			top = 0
		}
		ctx.X += cha.margins.Left + value
		ctx.Y += top
		ctx.Width -= cha.margins.Left + cha.margins.Right + 2*value
		ctx.Height -= top
		cha.chart.SetWidth(int(ctx.Width))
	} else {
		ctx.X = cha.xPos
		ctx.Y = cha.yPos
	}
	contentCreator := contentstream.NewContentCreator()
	if cha.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(cha.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(cha.tagInfo.Mcid)})
	}
	contentCreator.Translate(0, ctx.PageHeight)
	contentCreator.Scale(1, -1)
	contentCreator.Translate(ctx.X, ctx.Y)
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	cha.chart.Render(chartintegration.NewRenderer(contentCreator, block.resources), nil)
	if cha.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	if err := block.addContentsByString(contentCreator.String()); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		bottom := cha.Height() + cha.margins.Bottom
		ctx.Y += bottom
		ctx.Height -= bottom
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

var (
	ErrContentNotFit = errors.New("cannot fit content into an existing space")
)

// NewBlock creates a new Block with specified width and height.
func NewBlock(width float64, height float64) *Block {
	block := &Block{}
	block.contents = &contentstream.ContentStreamOperations{}
	block.resources = model.NewPdfPageResources()
	block.width = width
	block.height = height
	return block
}

// SetTextOverflow controls the behavior of paragraph text which
// does not fit in the available space.
func (sp *StyledParagraph) SetTextOverflow(textOverflow TextOverflow) {
	sp.textOverflow = textOverflow
}

type pageTransformations struct {
	matrix *transform.Matrix
	flipH  bool
	flipV  bool
}

// GeneratePageBlocks draws the composite Bezier curve on a new block
// representing the page. Implements the Drawable interface.
func (pbc *PolyBezierCurve) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(pbc.fillOpacity, pbc.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	curve := pbc.curve
	curve.FillEnabled = curve.FillColor != nil
	var (
		pageHeight        = ctx.PageHeight
		curves            = curve.Curves
		cubicBezierCurves = make([]contentdraw.CubicBezierCurve, 0, len(curve.Curves))
	)
	rect := model.PdfRectangle{}
	for i := range curve.Curves {
		curve := curves[i]
		curve.P0.Y = pageHeight - curve.P0.Y
		curve.P1.Y = pageHeight - curve.P1.Y
		curve.P2.Y = pageHeight - curve.P2.Y
		curve.P3.Y = pageHeight - curve.P3.Y
		cubicBezierCurves = append(cubicBezierCurves, curve)
		bounds := curve.GetBounds()
		if i == 0 {
			rect = bounds
		} else {
			rect.Llx = math.Min(rect.Llx, bounds.Llx)
			rect.Lly = math.Min(rect.Lly, bounds.Lly)
			rect.Urx = math.Max(rect.Urx, bounds.Urx)
			rect.Ury = math.Max(rect.Ury, bounds.Ury)
		}
	}
	curve.Curves = cubicBezierCurves
	defer func() { curve.Curves = curves }()
	if curve.FillEnabled {
		err := applyShapeColor(block, pbc.curve.FillColor, pbc.fillColor, func() Rectangle {
			return Rectangle{x: rect.Llx, y: rect.Lly, width: rect.Width(), height: rect.Height()}
		})
		if err != nil {
			return nil, ctx, err
		}
	}
	markedDraw, _, err := curve.MarkedDraw(gsName, pbc.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
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
func (sp *StyledParagraph) split(ctx DrawContext) (head, tail *StyledParagraph, err error) {
	if err = sp.wrapChunks(false); err != nil {
		return nil, nil, err
	}
	if len(sp.lines) == 1 && sp.lineHeight > ctx.Height {
		return sp, nil, nil
	}
	fn := func(chunks []*TextChunk, chunks2 []*TextChunk) []*TextChunk {
		if len(chunks2) == 0 {
			return chunks
		}
		chunksLen := len(chunks)
		if chunksLen == 0 {
			return append(chunks, chunks2...)
		}
		if chunks[chunksLen-1].Style == chunks2[0].Style {
			chunks[chunksLen-1].Text += chunks2[0].Text
		} else {
			chunks = append(chunks, chunks2[0])
		}
		return append(chunks, chunks2[1:]...)
	}
	fn2 := func(para *StyledParagraph, chunks []*TextChunk) *StyledParagraph {
		if len(chunks) == 0 {
			return nil
		}
		para2 := *para
		para2.chunks = chunks
		return &para2
	}
	var (
		value   float64
		chunks  []*TextChunk
		chunks2 []*TextChunk
	)
	for _, line := range sp.lines {
		var value2 float64
		chunks3 := make([]*TextChunk, 0, len(line))
		for _, chunk := range line {
			if fontSize := chunk.Style.FontSize * chunk.Style.effectiveLineHeight(sp.lineHeight); fontSize > value2 {
				value2 = fontSize
			}
			chunks3 = append(chunks3, chunk.clone())
		}
		if sp.positioning.IsRelative() {
			if value+value2 >= ctx.Height {
				chunks2 = fn(chunks2, chunks3)
			} else {
				chunks = fn(chunks, chunks3)
			}
		}
		value += value2
	}
	sp.lines = nil
	if len(chunks2) == 0 {
		return sp, nil, nil
	}
	return fn2(sp, chunks), fn2(sp, chunks2), nil
}

// SetIndent sets the cell's left indent.
func (tc *TableCell) SetIndent(indent float64) { tc.indent = indent }
func (gse *GraphicSVGElement) drawEllipse(creator *contentstream.ContentCreator, resources *model.PdfPageResources) {
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
	svgLength3, err := parseSVGLength(gse.Attributes["rx"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `rx` value: %v", err.Error())
	}
	svgLength4, err := parseSVGLength(gse.Attributes["ry"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `ry` value: %v", err.Error())
	}
	x2 := svgLength3 * gse.scale
	y2 := svgLength4 * gse.scale
	offX := svgLength * gse.scale
	offY := svgLength2 * gse.scale
	x := x2 * bezierCircleKappa
	y := y2 * bezierCircleKappa
	cubicBezierPath := contentdraw.NewCubicBezierPath()
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(-x2, 0, -x2, y, -x, y2, 0, y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, y2, x, y2, x2, y, x2, 0))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(x2, 0, x2, -y, x, -y2, 0, -y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, -y2, -x, -y2, -x2, -y, -x2, 0))
	cubicBezierPath = cubicBezierPath.Offset(offX, offY)
	if gse.Style.StrokeWidth > 0 {
		cubicBezierPath = cubicBezierPath.Offset(gse.Style.StrokeWidth/2, gse.Style.StrokeWidth/2)
	}
	contentdraw.DrawBezierPathWithCreator(cubicBezierPath, creator)
	gse.Style.fillStroke(creator)
	creator.Add_h()
	creator.Add_Q()
}
func collapseWhitespace(s string, ok, ok2 bool) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	if ok {
		if s2 := strings.TrimLeft(s, whitespaceChars); strings.Contains(s[:len(s)-len(s2)], "\n") {
			s = s2
		}
	}
	if ok2 {
		if s2 := strings.TrimRight(s, whitespaceChars); strings.Contains(s[len(s2):], "\n") {
			s = s2
		}
	}
	return newlineWhitespaceRe.ReplaceAllString(s, " ")
}

// SetPdfWriterAccessFunc sets a PdfWriter access function/hook.
// Exposes the PdfWriter just prior to writing the PDF.  Can be used to encrypt the output PDF, etc.
//
// Example of encrypting with a user/owner password "password"
// Prior to calling c.WriteFile():
//
//	c.SetPdfWriterAccessFunc(func(w *model.PdfWriter) error {
//		userPass := []byte("password")
//		ownerPass := []byte("password")
//		err := w.Encrypt(userPass, ownerPass, nil)
//		return err
//	})
func (c *Creator) SetPdfWriterAccessFunc(pdfWriterAccessFunc func(writer *model.PdfWriter) error) {
	c.pdfWriterAccessFunc = pdfWriterAccessFunc
}

// SetExtends specifies whether ot extend the shading beyond the starting and ending points.
//
// Text extends is set to `[]bool{false, false}` by default.
func (ls *LinearShading) SetExtends(start bool, end bool) { ls.base.SetExtends(start, end) }

// ColorCMYKFrom8bit creates a Color from c,m,y,k values (0-100).
// Example:
//
//	red := ColorCMYKFrom8Bit(0, 100, 100, 0)
func ColorCMYKFrom8bit(c, m, y, k byte) Color {
	return cmykColor{c: math.Min(float64(c), 100) / 100.0, m: math.Min(float64(m), 100) / 100.0, y: math.Min(float64(y), 100) / 100.0, k: math.Min(float64(k), 100) / 100.0}
}

// SetWidth sets the width of the rectangle.
func (r *Rectangle) SetWidth(width float64) { r.width = width }

// RemoveArtifact removes the artifact associated with the image, if any.
func (im *Image) RemoveArtifact() { im.artifact = nil }
func endMarkedContent(block *Block) {
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Add_EMC()
	block.addContents(contentCreator.Operations())
}

// EnableTextShaping enables or disables advanced text shaping for the paragraph.
//
// This only has effect if the fonts used in the paragraph support text shaping (OTF fonts).
// Advanced text shaping is useful for rendering complex scripts, ligatures,
// and proper character positioning.
//
// By default, text shaping is disabled.
func (sp *StyledParagraph) EnableTextShaping(enable bool) { sp.enableShaping = enable }

// SetStructureType sets the structure type for the table.
func (t *Table) SetStructureType(structureType model.StructureType) {
	if t.tagInfo == nil {
		t.tagInfo = model.NewStructureTagInfo()
	}
	t.tagInfo.StructureType = structureType
	if t.tagInfo.ComponentKObj != nil {
		if t.tagInfo.ParentKObj != nil {
			t.tagInfo.ParentKObj.RemoveKChild(t.tagInfo.ComponentKObj)
		}
		t.tagInfo.ComponentKObj = t.tagInfo.NewComponentKObj(structureType)
		t.tagInfo.ParentKObj.AddKChild(t.tagInfo.ComponentKObj)
	}
}
func parseBackgroundTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseBackground(node)
}

// GetMargins returns the Image's margins: left, right, top, bottom.
func (im *Image) GetMargins() (float64, float64, float64, float64) {
	return im.margins.Left, im.margins.Right, im.margins.Top, im.margins.Bottom
}

// SellerAddress returns the seller address used in the invoice template.
func (i *Invoice) SellerAddress() *InvoiceAddress { return i.sellerAddress }

// SetBorderColor sets the border color.
func (p *Polygon) SetBorderColor(color Color) { p.polygon.BorderColor = toPdfColor(color) }

// NewPage adds a new Page to the Creator and sets as the active Page.
func (c *Creator) NewPage() *model.PdfPage {
	page := c.newPage()
	c.pages = append(c.pages, page)
	c.context.Page++
	return page
}

// NewDivision returns a new Division container component.
func (c *Creator) NewDivision() *Division { return newDivision() }

// SetStructureNamespace configures the PDF 2.0 structure namespace applied to the
// generated structure tree. It takes effect only when a structure tree is produced
// (via TagComponents or SetStructTreeRoot): every generated structure element then
// carries a shared /NS referencing this namespace, the namespace is declared on the
// StructTreeRoot /Namespaces array, and the document is written as PDF 2.0.
// Passing an empty URI clears the namespace; repeat calls with the same URI are
// idempotent (they keep one shared instance). Pass model.StructureNamespacePDF2 to
// author PDF/UA-2 documents.
func (c *Creator) SetStructureNamespace(uri string) {
	if uri == "" {
		c.structNamespace = nil
		return
	}
	if c.structNamespace != nil && c.structNamespace.NS() == uri {
		return
	}
	c.structNamespace = model.NewPdfStructureNamespace(uri)
}
func addEscapeToPipe(pipeNode *parse.PipeNode) {
	if pipeNode == nil || len(pipeNode.Decl) > 0 || len(pipeNode.Cmds) == 0 {
		return
	}
	if pipeEndsWithRaw(pipeNode) {
		return
	}
	commandNode := &parse.CommandNode{NodeType: parse.NodeCommand}
	commandNode.Args = append(commandNode.Args, parse.NewIdentifier(xmlEscapeFuncName))
	pipeNode.Cmds = append(pipeNode.Cmds, commandNode)
}

// TextOverflow determines the behavior of paragraph text which does
// not fit in the available space.
type TextOverflow int

// AddLine adds a new line with the provided style to the table of contents.
func (to *TOC) AddLine(line *TOCLine) *TOCLine {
	if line == nil {
		return nil
	}
	to.lines = append(to.lines, line)
	return line
}

// SkipRows skips over a specified number of rows in the table.
func (t *Table) SkipRows(num int) {
	n := num*t.cols - 1
	if n < 0 {
		common.Log.Debug("Table: cannot skip back to previous cells")
		return
	}
	for i := 0; i < n; i++ {
		t.NewCell()
	}
}

// SetPageSize sets the Creator's page size.  Pages that are added after this will be created with
// this Page size.
// Does not affect pages already created.
//
// Common page sizes are defined as constants.
// Examples:
// 1. c.SetPageSize(creator.PageSizeA4)
// 2. c.SetPageSize(creator.PageSizeA3)
// 3. c.SetPageSize(creator.PageSizeLegal)
// 4. c.SetPageSize(creator.PageSizeLetter)
//
// For custom sizes: Use the PPMM (points per mm) and PPI (points per inch) when defining those based on
// physical page sizes:
//
// Examples:
// 1. 10x15 sq. mm: SetPageSize(PageSize{10*creator.PPMM, 15*creator.PPMM}) where PPMM is points per mm.
// 2. 3x2 sq. inches: SetPageSize(PageSize{3*creator.PPI, 2*creator.PPI}) where PPI is points per inch.
func (c *Creator) SetPageSize(size PageSize) {
	c.pageSize = size
	c.pageWidth = size[0]
	c.pageHeight = size[1]
	pageWidth := 0.1 * c.pageWidth
	c.pageMargins.Left = pageWidth
	c.pageMargins.Right = pageWidth
	c.pageMargins.Top = pageWidth
	c.pageMargins.Bottom = pageWidth
}
func isNilValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}

// TextVerticalAlignment controls the vertical position of the text
// in a styled paragraph.
type TextVerticalAlignment int

// MarkAsArtifact marks the image as an artifact of the specified type.
//
// This would override regular structure tagging and accessibility
// properties, and the image will be treated as an artifact in the PDF.
func (im *Image) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	im.artifact = &model.Artifact{Type: artifactType}
	return im.artifact
}
func cropAlphaData(img *model.Image, rect, rect2 image.Rectangle) []byte {
	alphaData := img.AlphaData()
	ok := rect != rect2
	if len(alphaData) == 0 && !ok {
		return nil
	}
	width, height := int(img.Width), int(img.Height)
	n := 0
	if len(alphaData) != 0 {
		switch img.BitsPerComponent {
		case 8:
			n = 1
		case 16:
			n = 2
		}
		if n == 0 {
			return nil
		}
		if len(alphaData) != n*width*height {
			common.Log.Debug("Alpha channel has %d bytes, expected %d; missing samples are treated as opaque", len(alphaData), n*width*height)
		}
	}
	dx := rect.Dx()
	data := make([]byte, dx*rect.Dy())
	for i := rect2.Min.Y; i < rect2.Max.Y; i++ {
		data2 := data[(i-rect2.Min.Y)*dx:][:rect2.Dx()]
		for j := range data2 {
			n2 := n * (i*width + rect2.Min.X + j)
			if n > 0 && n2 < len(alphaData) {
				data2[j] = alphaData[n2]
			} else {
				data2[j] = 0xff
			}
		}
	}
	for _, b := range data {
		if b != 0xff {
			return data
		}
	}
	return nil
}

// SetMargins sets the margins of the ellipse.
// NOTE: ellipse margins are only applied if relative positioning is used.
func (e *Ellipse) SetMargins(left, right, top, bottom float64) {
	e.margins.Left = left
	e.margins.Right = right
	e.margins.Top = top
	e.margins.Bottom = bottom
}

// NoteStyle returns the style properties used to render the content of the
// invoice note sections.
func (i *Invoice) NoteStyle() TextStyle { return i.noteStyle }

// String implements error interface.
func (ure UnsupportedRuneError) Error() string { return ure.Message }

// ColorGrayFromHex converts color hex code to gray color for using with creator.
// NOTE: If there is a problem interpreting the string, then will use black color and log a debug message.
// Example hex code: #ff -> white.
func ColorGrayFromHex(hexStr string) Color {
	colorGray := grayColor{}
	if (len(hexStr) != 2 && len(hexStr) != 3) || hexStr[0] != '#' {
		common.Log.Debug("Invalid hex code: %s", hexStr)
		return colorGray
	}
	var gray int
	if len(hexStr) == 2 {
		var shortGray int
		n, err := fmt.Sscanf(hexStr, "#%1x", &shortGray)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s, error: %v", hexStr, err)
			return colorGray
		}
		if n != 1 {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorGray
		}
		gray = shortGray*16 + shortGray
	} else {
		n, err := fmt.Sscanf(hexStr, "#%2x", &gray)
		if err != nil {
			common.Log.Debug("Invalid hex code: %s", hexStr)
			return colorGray
		}
		if n != 1 {
			common.Log.Debug("Invalid hex code: %s, n != 3 (%d)", hexStr, n)
			return colorGray
		}
	}
	colorGray.gray = float64(gray) / 255.0
	return colorGray
}

// Crop crops the Image to the specified bounds. Parts of the rectangle
// outside the image are padded with transparent pixels. The alpha channel,
// if any, is cropped along with the color samples, as is the matching region
// of a soft or stencil mask set on the image, and the image is rebuilt from
// the cropped data on its next draw.
func (im *Image) Crop(x0, y0, x1, y1 int) {
	rect := image.Rect(x0, y0, x1, y1)
	img, err := cropImage(im.img, rect)
	if err != nil {
		log.Fatalf("Error cropping image: %v", err)
	}
	if mask := im.mask; mask != nil && mask.img != nil && im.img.Width > 0 && im.img.Height > 0 {
		width, height := float64(mask.img.Width), float64(mask.img.Height)
		width2, height2 := float64(im.img.Width), float64(im.img.Height)
		rect2 := image.Rect(int(math.Floor(float64(rect.Min.X)*width/width2)), int(math.Floor(float64(rect.Min.Y)*height/height2)), int(math.Ceil(float64(rect.Max.X)*width/width2)), int(math.Ceil(float64(rect.Max.Y)*height/height2)))
		if rect2.Dx() < 1 {
			rect2.Max.X = rect2.Min.X + 1
		}
		if rect2.Dy() < 1 {
			rect2.Max.Y = rect2.Min.Y + 1
		}
		img, err := cropImage(mask.img, rect2)
		if err != nil {
			log.Fatalf("Error cropping image mask: %v", err)
		}
		img.SetAlpha(nil)
		mask.img = img
	}
	width := float64(img.Width)
	height := float64(img.Height)
	im.img = img
	im.origWidth = width
	im.origHeight = height
	im.width = width
	im.height = height
	im.xobj = nil
}

// DrawFooter sets a function to draw a footer on created output pages.
func (c *Creator) DrawFooter(drawFooterFunc func(block *Block, footerFunctionArgs FooterFunctionArgs)) {
	c.drawFooterFunc = drawFooterFunc
}
func splitUnitSuffix(value2 string) (value, unit string) {
	if value2 == "" || (value2[len(value2)-1] >= '0' && value2[len(value2)-1] <= '9') {
		return value2, ""
	}
	value = value2
	for _, lengthUnit := range lengthUnits {
		if strings.Contains(value, lengthUnit) {
			unit = lengthUnit
		}
		value = strings.TrimSuffix(value, lengthUnit)
	}
	return
}
func newCurvePolygon(rings [][]contentdraw.CubicBezierCurve) *CurvePolygon {
	return &CurvePolygon{polygon: &contentdraw.CurvePolygon{Rings: rings}, fillOpacity: 1.0, borderOpacity: 1.0}
}

// SetPos sets the grid positioning to absolute mode and specifies the upper-left corner
// coordinates as (x,y).
// Note that this is only sensible to use when the grid does not wrap over multiple pages.
// TODO: Should be able to set width too (not just based on context/relative positioning mode).
func (g *Grid) SetPos(x, y float64) {
	g.positioning = PositionAbsolute
	g.xPos = x
	g.yPos = y
}
func (b *Block) transform(matrix transform.Matrix) {
	operations := contentstream.NewContentCreator().Add_cm(matrix[0], matrix[1], matrix[3], matrix[4], matrix[6], matrix[7]).Operations()
	*b.contents = append(*operations, *b.contents...)
	b.contents.WrapIfNeeded()
}

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (fbs *FunctionBasedShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	pdfShadingPattern, err := fbs.ToPdfShadingPattern()
	if err != nil {
		return "", err
	}
	return addShadingPatternResource(block, pdfShadingPattern.ToPdfObject())
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

// Positioning returns the type of positioning the line is set to use.
func (l *Line) Positioning() Positioning { return l.positioning }

var tableCellAttrs = map[string]attrApplyFunc{"colspan": nil, "rowspan": nil, "indent": floatAttr(func(cell *TableCell, indent float64) { cell.SetIndent(indent) }), "align": enumAttr(cellHorizontalAlignmentValues, func(cell *TableCell, halign CellHorizontalAlignment) { cell.SetHorizontalAlignment(halign) }), "vertical-align": enumAttr(cellVerticalAlignmentValues, func(cell *TableCell, valign CellVerticalAlignment) { cell.SetVerticalAlignment(valign) }), "border-style": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) { cell.SetSideBorderStyle(CellBorderSideAll, style) }), "border-style-top": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideTop, style)
}), "border-style-bottom": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideBottom, style)
}), "border-style-left": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideLeft, style)
}), "border-style-right": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideRight, style)
}), "border-width": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideAll, width) }), "border-width-top": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideTop, width) }), "border-width-bottom": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideBottom, width) }), "border-width-left": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideLeft, width) }), "border-width-right": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideRight, width) }), "border-color": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideAll, col) }), "border-color-top": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideTop, col) }), "border-color-bottom": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideBottom, col) }), "border-color-left": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideLeft, col) }), "border-color-right": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideRight, col) }), "border-line-style": enumAttr(lineStyleValues, func(cell *TableCell, style contentdraw.LineStyle) { cell.SetBorderLineStyle(style) }), "background-color": colorAttr(func(cell *TableCell, col Color) { cell.SetBackgroundColor(col) })}

func parsePercentOrFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	var percentOrFloat float64
	if strings.HasSuffix(s, "%") {
		float, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil {
			return 0, err
		}
		percentOrFloat = float / 100.0
	} else {
		float, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, err
		}
		percentOrFloat = float
	}
	return percentOrFloat, nil
}

var containerTags = map[string]struct{}{}

// MultiColCell makes a new cell with the specified column span and inserts it
// into the table at the current position.
func (t *Table) MultiColCell(colspan int) *TableCell { return t.MultiCell(1, colspan) }

var divisionAttrs = map[string]attrApplyFunc{"enable-page-wrap": boolAttr(func(division *Division, enable bool) { division.EnablePageWrap(enable) }), "margin": marginAttr(func(division *Division, margins Margins) {
	division.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "padding": marginAttr(func(division *Division, margins Margins) {
	division.SetPadding(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

func isKeycapBase(r rune) bool { return (r >= '0' && r <= '9') || r == '#' || r == '*' }

// NewStyledTOCLine creates a new table of contents line with the provided style.
func (c *Creator) NewStyledTOCLine(number, title, page TextChunk, level uint, style TextStyle) *TOCLine {
	return newTOCLineFromChunks(number, title, page, level, style)
}

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) SetStructPageNumber(pageNumber *int64)              {}
func (i *Invoice) SetStructureType(structureType model.StructureType) {}
func parseFloatFields(s string) ([]float64, error) {
	fields := strings.Fields(s)
	floats := make([]float64, len(fields))
	var err error
	for i, field := range fields {
		float, err2 := strconv.ParseFloat(field, 64)
		if err2 != nil {
			setFirstError(&err, err2)
			float = 0
		}
		floats[i] = float
	}
	return floats, err
}

// GetMargins returns the margins of the graphic svg (left, right, top, bottom).
func (gs *GraphicSVG) GetMargins() (float64, float64, float64, float64) {
	return gs.margins.Left, gs.margins.Right, gs.margins.Top, gs.margins.Bottom
}
func drawableHeight(drawable VectorDrawable, left float64) float64 {
	switch para := drawable.(type) {
	case *StyledParagraph:
		if para.enableWrap {
			para.SetWidth(left - para.margins.Left - para.margins.Right)
		}
		return para.Height() + para.margins.Top + para.margins.Bottom + (0.5 * para.getTextHeight())
	case *Image:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *Rectangle:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom + para.borderWidth
	case *Ellipse:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *Division:
		return para.ctxHeight(left) + para.margins.Top + para.margins.Bottom + para.padding.Top + para.padding.Bottom
	case *Table:
		para.updateRowHeights(left - para.margins.Left - para.margins.Right)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *List:
		return para.ctxHeight(left) + para.margins.Top + para.margins.Bottom
	case marginDrawable:
		_, _, top, bottom := para.GetMargins()
		return para.Height() + top + bottom
	default:
		return para.Height()
	}
}

// SetShowNumbering sets a flag to indicate whether or not to show chapter numbers as part of title.
func (ch *Chapter) SetShowNumbering(show bool) {
	ch.showNumbering = show
	ch.heading.SetText(ch.headingText())
}
func (tp *templateProcessor) resolveFontUncached(s string) (*model.PdfFont, error) {
	fontRegular := tp.creator.fontRegular
	if strings.TrimSpace(s) == "" {
		return fontRegular, nil
	}
	ok := false
	for _, s2 := range strings.Split(s, ",") {
		s2 = strings.TrimSpace(s2)
		if s2 == "" {
			continue
		}
		ok = true
		if fontUncached, ok := tp.options.FontMap[s2]; ok {
			return fontUncached, nil
		}
		stdFontName, ok := stdFontNames[s2]
		if ok {
			if standard14Font, err := model.NewStandard14Font(stdFontName); err == nil {
				return standard14Font, nil
			}
		}
		if attrPropList := tp.parseAttrPropList(s2); len(attrPropList) > 0 {
			if filePath, ok := attrPropList["path"]; ok {
				fn := model.NewPdfFontFromTTFFile
				if s, ok := attrPropList["type"]; ok && s == "composite" {
					fn = model.NewCompositePdfFontFile
				}
				if fontUncached, err := fn(filePath); err != nil {
					common.Log.Debug("Could not load font `%s`: %v.", filePath, err)
				} else {
					return fontUncached, nil
				}
			}
		}
	}
	if ok {
		return fontRegular, fmt.Errorf("unresolved font %q", s)
	}
	return fontRegular, nil
}

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) SetStructureType(structureType model.StructureType) {}

// Height returns the height of the Paragraph. The height is calculated based on the input text and how it is wrapped
// within the container. Does not include Margins.
func (sp *StyledParagraph) Height() float64 {
	sp.wrapText()
	var height float64
	for _, line := range sp.lines {
		var height2 float64
		for _, chunk := range line {
			fontSize := chunk.Style.effectiveLineHeight(sp.lineHeight) * chunk.Style.FontSize
			if fontSize > height2 {
				height2 = fontSize
			}
		}
		height += height2
	}
	return height
}

// HorizontalAlignment represents the horizontal alignment of components
// within a page.
type HorizontalAlignment int

func (teo *TemplateOptions) init() {
	if teo.SubtemplateMap == nil {
		teo.SubtemplateMap = map[string]io.Reader{}
	}
	if teo.FontMap == nil {
		teo.FontMap = map[string]*model.PdfFont{}
	}
	if teo.ImageMap == nil {
		teo.ImageMap = map[string]*model.Image{}
	}
	if teo.ColorMap == nil {
		teo.ColorMap = map[string]Color{}
	}
	if teo.ChartMap == nil {
		teo.ChartMap = map[string]chartrender.ChartRenderable{}
	}
}

// AddHighlightedText adds a new highlighted text to the paragraph.
func (sp *StyledParagraph) AddHighlightedText(text string, color Color, alpha float64) *TextChunk {
	textChunk := NewTextChunk(text, sp.defaultStyle)
	textChunk.Highlight(color, alpha)
	return sp.appendChunk(textChunk)
}

// PageBreak represents a page break for a chapter.
type PageBreak struct{}

func mergeContentStreams(contentStreamOperations *contentstream.ContentStreamOperations, resources *model.PdfPageResources, contentStreamOperations2 *contentstream.ContentStreamOperations, resources2 *model.PdfPageResources) error {
	m := map[core.PdfObjectName]core.PdfObjectName{}
	m2 := map[core.PdfObjectName]core.PdfObjectName{}
	m3 := map[core.PdfObjectName]core.PdfObjectName{}
	m4 := map[core.PdfObjectName]core.PdfObjectName{}
	m5 := map[core.PdfObjectName]core.PdfObjectName{}
	m6 := map[core.PdfObjectName]core.PdfObjectName{}
	for _, op := range *contentStreamOperations2 {
		switch op.Operand {
		case "Do":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						xObject, _ := resources2.GetXObjectByName(*pdfObjectName)
						if xObject != nil {
							keyName = *pdfObjectName
							for {
								xObject2, _ := resources.GetXObjectByName(keyName)
								if xObject2 == nil || xObject2 == xObject {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
						}
						resources.SetXObjectByName(keyName, xObject)
						m[*pdfObjectName] = keyName
					}
					s := m[*pdfObjectName]
					op.Params[0] = &s
				}
			}
		case "Tf":
			if len(op.Params) == 2 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m2[*pdfObjectName]; !ok {
						font, ok := resources2.GetFontByName(*pdfObjectName)
						keyName := *pdfObjectName
						if ok && font != nil {
							keyName = uniqueFontName(pdfObjectName.String(), font, resources)
						}
						resources.SetFontByName(keyName, font)
						m2[*pdfObjectName] = keyName
					}
					s := m2[*pdfObjectName]
					op.Params[0] = &s
				}
			}
		case "CS", "cs":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m3[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						colorspace, ok := resources2.GetColorspaceByName(*pdfObjectName)
						if ok {
							keyName = *pdfObjectName
							for {
								colorspace2, ok := resources.GetColorspaceByName(keyName)
								if !ok || colorspace == colorspace2 {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
							resources.SetColorspaceByName(keyName, colorspace)
							m3[*pdfObjectName] = keyName
						} else {
							common.Log.Debug("Colorspace not found")
						}
					}
					if s, ok := m3[*pdfObjectName]; ok {
						op.Params[0] = &s
					} else {
						common.Log.Debug("Error: Colorspace %s not found", *pdfObjectName)
					}
				}
			}
		case "SCN", "scn":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m4[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						pattern, ok := resources2.GetPatternByName(*pdfObjectName)
						if ok {
							keyName = *pdfObjectName
							for {
								pattern2, ok := resources.GetPatternByName(keyName)
								if !ok || pattern2 == pattern {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
							err := resources.SetPatternByName(keyName, pattern.ToPdfObject())
							if err != nil {
								return err
							}
							m4[*pdfObjectName] = keyName
						}
					}
					if s, ok := m4[*pdfObjectName]; ok {
						op.Params[0] = &s
					}
				}
			}
		case "sh":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m5[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						shading, ok := resources2.GetShadingByName(*pdfObjectName)
						if ok {
							keyName = *pdfObjectName
							for {
								shading2, ok := resources.GetShadingByName(keyName)
								if !ok || shading == shading2 {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
							err := resources.SetShadingByName(keyName, shading.ToPdfObject())
							if err != nil {
								common.Log.Debug("ERROR Set shading: %v", err)
								return err
							}
							m5[*pdfObjectName] = keyName
						} else {
							common.Log.Debug("Shading not found")
						}
					}
					if s, ok := m5[*pdfObjectName]; ok {
						op.Params[0] = &s
					} else {
						common.Log.Debug("Error: Shading %s not found", *pdfObjectName)
					}
				}
			}
		case "gs":
			if len(op.Params) == 1 {
				if pdfObjectName, ok := op.Params[0].(*core.PdfObjectName); ok {
					if _, ok := m6[*pdfObjectName]; !ok {
						var keyName core.PdfObjectName
						extGState, ok := resources2.GetExtGState(*pdfObjectName)
						if ok {
							keyName = *pdfObjectName
							for {
								extGState2, ok := resources.GetExtGState(keyName)
								if !ok || extGState == extGState2 {
									break
								}
								keyName = *core.MakeName(nextResourceName(keyName.String()))
							}
						}
						resources.AddExtGState(keyName, extGState)
						m6[*pdfObjectName] = keyName
					}
					s := m6[*pdfObjectName]
					op.Params[0] = &s
				}
			}
		}
		*contentStreamOperations = append(*contentStreamOperations, op)
	}
	return nil
}

// Draw processes the specified Drawable widget and generates blocks that can
// be rendered to the output document. The generated blocks can span over one
// or more pages. Additional pages are added if the contents go over the current
// page. Each generated block is assigned to the creator page it will be
// rendered to. In order to render the generated blocks to the creator pages,
// call Finalize, Write or WriteToFile.
func (c *Creator) Draw(d Drawable) error {
	if c.getActivePage() == nil {
		c.NewPage()
	}
	var (
		pagesLen      int64
		mcid          int64
		componentKObj *model.KDict
		n             int
	)
	if c.taggingEnabled {
		pagesLen = int64(len(c.pages))
		d.SetStructPageNumber(&pagesLen)
		mcid = c.pageMCIDs[pagesLen]
		n = len(c.docKDict.GetChildren())
		switch d2 := d.(type) {
		case *Table:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		case *Grid:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		case *List:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
		case *Division:
			d2.AddTag(c.docKDict)
			d2.SetMarkedContentID(mcid)
			componentKObj = d2.tagInfo.ComponentKObj
		default:
			d2.SetMarkedContentID(mcid)
			kDict, err := d2.GenerateKDict()
			if err != nil {
				return err
			}
			if kDict != nil {
				c.docKDict.AddKChild(kDict)
			}
			componentKObj = kDict
		}
	}
	pageBlocks, ctx, err := d.GeneratePageBlocks(c.context)
	if err != nil {
		return err
	}
	if len(ctx.errs) > 0 {
		c.Errors = append(c.Errors, ctx.errs...)
	}
	if c.taggingEnabled {
		if componentKObj == nil {
			if children := c.docKDict.GetChildren(); len(children) > n {
				componentKObj = children[len(children)-1].GetKDict()
			}
		}
		if componentKObj != nil {
			mcid2 := mcid + 1
			if n := maxMCID(componentKObj); n >= mcid2 {
				mcid2 = n + 1
			}
			c.pageMCIDs[pagesLen] = mcid2
		}
	}
	for i, pageBlock := range pageBlocks {
		if i > 0 {
			c.NewPage()
		}
		activePage := c.getActivePage()
		if block, ok := c.blocks[activePage]; ok {
			if err := block.mergeBlocks(pageBlock); err != nil {
				return err
			}
			if err := mergeColorspaces(pageBlock.resources, block.resources); err != nil {
				return err
			}
		} else {
			c.blocks[activePage] = pageBlock
		}
	}
	c.context.X = ctx.X
	c.context.Y = ctx.Y
	c.context.Height = precision.RoundDefault(ctx.PageHeight - ctx.Y - ctx.Margins.Bottom)
	return nil
}
func (tp *templateProcessor) collectStylesNode(node *templateNode) error {
	for _, node2 := range node.children {
		if node2.start.Name.Local == styleElementName {
			if err := tp.collectClasses(node2); err != nil {
				return err
			}
			continue
		}
		if err := tp.collectStylesNode(node2); err != nil {
			return err
		}
	}
	return nil
}
func (t *Table) maxFittingRows(value, top float64) int {
	t.sortCells()
	t.updateRowHeights(value - t.margins.Left - t.margins.Right)
	top -= t.margins.Top + t.margins.Bottom
	var rowHeight float64
	row := 0
	for i := 1; i <= t.rows && i-1 < len(t.rowHeights); i++ {
		rowHeight += t.rowHeights[i-1]
		if rowHeight > top {
			break
		}
		row = i
	}
	return t.clampCut(row)
}

// GetCoords returns coordinates of border.
func (bo *border) GetCoords() (float64, float64) { return bo.x, bo.y }

// Width is not used. Not used as a Table element is designed to fill into
// available width depending on the context. Returns 0.
func (t *Table) Width() float64 { return 0 }
func (gse *GraphicSVGElement) getGradientAngle() float64 {
	attribute, ok := gse.Attributes["gradientTransform"]
	if ok {
		fields := strings.Fields(attribute)
		for _, field := range fields {
			fieldsFunc := strings.FieldsFunc(field, isTransformSeparator)
			if len(fieldsFunc) < 2 {
				continue
			}
			if fieldsFunc[0] == "rotate" {
				colorComponent, err := parseColorComponent(fieldsFunc[1])
				if err != nil {
					common.Log.Debug("can't parse transform attribute %v", attribute)
					return 0.0
				}
				return colorComponent
			}
		}
	}
	return 0.0
}
func templateArray(templateArray ...interface{}) []interface{} { return templateArray }

// FitMode returns the fit mode of the ellipse.
func (e *Ellipse) FitMode() FitMode { return e.fitMode }

// WriteToFile writes the Creator output to file specified by path.
func (c *Creator) WriteToFile(outputPath string) error {
	ws, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer ws.Close()
	return c.Write(ws)
}

// Terms returns the terms and conditions section of the invoice as a
// title-content pair.
func (i *Invoice) Terms() (string, string)    { return i.terms[0], i.terms[1] }
func (tp *templateProcessor) validate() error { return tp.process(true) }

// Lines returns all the lines the table of contents has.
func (to *TOC) Lines() []*TOCLine { return to.lines }
func defaultTextStyle(font *model.PdfFont) TextStyle {
	return TextStyle{Color: ColorRGBFrom8bit(0, 0, 0), Font: font, FontSize: 10, OutlineSize: 1, HorizontalScaling: DefaultHorizontalScaling, UnderlineStyle: TextDecorationLineStyle{Offset: 1, Thickness: 1}}
}

// SetFillOpacity sets the fill opacity.
func (cp *CurvePolygon) SetFillOpacity(opacity float64) { cp.fillOpacity = opacity }

// BorderOpacity returns the border opacity of the ellipse (0-1).
func (e *Ellipse) BorderOpacity() float64 { return e.borderOpacity }

// Flip flips the active page on the specified axes.
// If `flipH` is true, the page is flipped horizontally. Similarly, if `flipV`
// is true, the page is flipped vertically. If both are true, the page is
// flipped both horizontally and vertically.
// NOTE: the flip transformations are applied when the creator is finalized,
// which is at write time in most cases.
func (c *Creator) Flip(flipH, flipV bool) error {
	activePage := c.getActivePage()
	if activePage == nil {
		return errors.New("no page active")
	}
	pageTransform, ok := c.pageTransforms[activePage]
	if !ok {
		pageTransform = &pageTransformations{}
		c.pageTransforms[activePage] = pageTransform
	}
	pageTransform.flipH = flipH
	pageTransform.flipV = flipV
	return nil
}

// GetCoords returns the (x1, y1), (x2, y2) points defining the Line.
func (l *Line) GetCoords() (float64, float64, float64, float64) {
	return l.x1, l.y1, l.x2, l.y2
}

// SetBorder sets the cell's border style.
func (tc *TableCell) SetBorder(side CellBorderSide, style CellBorderStyle, width float64) {
	if style == CellBorderStyleSingle && side == CellBorderSideAll {
		tc.borderStyleLeft = CellBorderStyleSingle
		tc.borderWidthLeft = width
		tc.borderStyleBottom = CellBorderStyleSingle
		tc.borderWidthBottom = width
		tc.borderStyleRight = CellBorderStyleSingle
		tc.borderWidthRight = width
		tc.borderStyleTop = CellBorderStyleSingle
		tc.borderWidthTop = width
	} else if style == CellBorderStyleDouble && side == CellBorderSideAll {
		tc.borderStyleLeft = CellBorderStyleDouble
		tc.borderWidthLeft = width
		tc.borderStyleBottom = CellBorderStyleDouble
		tc.borderWidthBottom = width
		tc.borderStyleRight = CellBorderStyleDouble
		tc.borderWidthRight = width
		tc.borderStyleTop = CellBorderStyleDouble
		tc.borderWidthTop = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideLeft {
		tc.borderStyleLeft = style
		tc.borderWidthLeft = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideBottom {
		tc.borderStyleBottom = style
		tc.borderWidthBottom = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideRight {
		tc.borderStyleRight = style
		tc.borderWidthRight = width
	} else if (style == CellBorderStyleSingle || style == CellBorderStyleDouble) && side == CellBorderSideTop {
		tc.borderStyleTop = style
		tc.borderWidthTop = width
	}
}

// IsRelative checks if the positioning is relative.
func (pos Positioning) IsRelative() bool { return pos == PositionRelative }
func (co *Command) compare(command *Command) bool {
	if co.Symbol != command.Symbol {
		return false
	}
	for i, param := range co.Params {
		if param != command.Params[i] {
			return false
		}
	}
	return true
}

// RotateDeg rotates the current active page by angle degrees.  An error is returned on failure,
// which can be if there is no currently active page, or the angleDeg is not a multiple of 90 degrees.
func (c *Creator) RotateDeg(angleDeg int64) error {
	activePage := c.getActivePage()
	if activePage == nil {
		common.Log.Debug("Fail to rotate: no page currently active")
		return errors.New("no page active")
	}
	if angleDeg%90 != 0 {
		common.Log.Debug("ERROR: Page rotation angle not a multiple of 90")
		return errors.New("range check error")
	}
	var rotate int64
	if activePage.Rotate != nil {
		rotate = *(activePage.Rotate)
	}
	rotate += angleDeg
	activePage.Rotate = &rotate
	return nil
}

// SetWidthTop sets border width for top.
func (bo *border) SetWidthTop(bw float64) { bo.widthTop = bw }
func (tp *templateProcessor) parseChapter(node *templateNode) (interface{}, error) {
	fn := tp.creator.NewChapter
	if componentParent := node.componentParent(); componentParent != nil {
		if chapter, ok := componentParent.value.(*Chapter); ok {
			fn = chapter.NewSubchapter
		}
	}
	chapter := fn("")
	if err := tp.applyAttrs(node, chapter, chapterAttrs); err != nil {
		return nil, err
	}
	return chapter, nil
}

// SetIndent sets the cell's left indent.
func (gc *GridCell) SetIndent(indent float64) { gc.indent = indent }

// SetAlternateText sets the alternate text for the image.
func (im *Image) SetAlternateText(text string) { im.altText = text }

// NewPolyBezierCurve creates a new composite Bezier (polybezier) curve.
func (c *Creator) NewPolyBezierCurve(curves []contentdraw.CubicBezierCurve) *PolyBezierCurve {
	return newPolyBezierCurve(curves)
}
func init() {
	for _, templateTag := range templateTags {
		for allowedParentKey := range templateTag.allowedParents {
			containerTags[allowedParentKey] = struct{}{}
		}
	}
}
func (gss *GraphicSVGStyle) fillStroke(cc *contentstream.ContentCreator) {
	if gss.FillColor != "" && gss.StrokeColor != "" {
		cc.Add_B()
	} else if gss.FillColor != "" {
		cc.Add_f()
	} else if gss.StrokeColor != "" {
		cc.Add_S()
	}
}

// NewCurvePolygon creates a new curve polygon.
func (c *Creator) NewCurvePolygon(rings [][]contentdraw.CubicBezierCurve) *CurvePolygon {
	return newCurvePolygon(rings)
}

// DueDate returns the invoice due date description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) DueDate() (*InvoiceCell, *InvoiceCell) {
	return i.dueDate[0], i.dueDate[1]
}

// Height returns the height of the list.
func (li *List) Height() float64 {
	var ctxHeight float64
	for _, item := range li.items {
		ctxHeight += item.ctxHeight(li.Width())
	}
	return ctxHeight
}
func (tp *templateProcessor) childStyle(node *templateNode) map[string]string {
	nodeOwnStyle := tp.nodeOwnStyle(node)
	if len(nodeOwnStyle) == 0 {
		return node.attrs
	}
	m := make(map[string]string, len(node.attrs)+len(nodeOwnStyle))
	for attrKey, attr := range node.attrs {
		m[attrKey] = attr
	}
	for key, s := range nodeOwnStyle {
		m[key] = s
	}
	return m
}

// LinkTagOptions represents optional parameters for tagged link annotations,
// providing flexibility for adding accessibility features.
type LinkTagOptions struct {

	// MCID is the marked content identifier for structure tree association
	MCID int64

	// AltText is alternative text for screen readers (only used if different from visible text)
	AltText string

	// Tooltip sets the annotation's Contents field (tooltip on hover)
	Tooltip string
}

func (i *Invoice) SetStructPageNumber(pageNumber *int64) {}
func (t *Table) addListItemTag() *model.KDict {
	kDictionary := model.NewKDictionary()
	kDictionary.S = core.MakeName(string(model.StructureTypeListItem))
	t.tagInfo.ComponentKObj.AddKChild(kDictionary)
	return kDictionary
}

type containerDrawable interface {
	Drawable

	// ContainerComponent checks if the component is allowed to be added into provided 'container' and returns
	// preprocessed copy of itself. If the component is not changed it is allowed to return itself in a callback way.
	// If the component is not compatible with provided container this method should return an error.
	ContainerComponent(drawable Drawable) (Drawable, error)
}

func (t *Table) getLastCellFromCol(n int) (int, *TableCell) {
	for i := len(t.cells) - 1; i >= 0; i-- {
		if t.cells[i].col <= n && n <= t.cells[i].col+t.cells[i].colSpan {
			return i, t.cells[i]
		}
	}
	return 0, nil
}

// This method is not supported by PageBreak component and exists solely to satisfy the Drawable interface.
func (pb *PageBreak) SetStructureType(structureType model.StructureType) {}

// MarkAsArtifact marks the block content as an artifact of the specified type.
//
// All of the block's content is wrapped in an /Artifact marked content sequence
// and the block is excluded from the structure tree. This is used for content
// such as running headers and footers that must not be part of the logical
// structure of a tagged PDF.
func (b *Block) MarkAsArtifact(artifactType model.ArtifactType) *model.Artifact {
	b.artifact = &model.Artifact{Type: artifactType}
	return b.artifact
}
func newInvoice(defaultStyle, textStyle TextStyle) *Invoice {
	invoice := &Invoice{title: "INVOICE", addressSeparator: ", ", defaultStyle: defaultStyle, headingStyle: textStyle}
	invoice.sellerAddress = &InvoiceAddress{Separator: invoice.addressSeparator}
	invoice.buyerAddress = &InvoiceAddress{Heading: "Bill to", Separator: invoice.addressSeparator}
	colorRGBFrom8bit := ColorRGBFrom8bit(245, 245, 245)
	color := ColorRGBFrom8bit(155, 155, 155)
	invoice.titleStyle = textStyle
	invoice.titleStyle.Color = color
	invoice.titleStyle.FontSize = 20
	invoice.addressStyle = defaultStyle
	invoice.addressHeadingStyle = textStyle
	invoice.noteStyle = defaultStyle
	invoice.noteHeadingStyle = textStyle
	invoice.defaultCellProps = invoice.NewCellProps()
	invoice.defaultCellProps.BackgroundColor = colorRGBFrom8bit
	invoice.defaultCellProps.TextStyle = textStyle
	invoice.headingCellProps = invoice.NewCellProps()
	invoice.headingCellProps.TextStyle = textStyle
	invoice.headingCellProps.BackgroundColor = colorRGBFrom8bit
	invoice.headingCellProps.BorderColor = colorRGBFrom8bit
	invoice.lineProps = invoice.NewCellProps()
	invoice.lineProps.BorderColor = colorRGBFrom8bit
	invoice.lineProps.BorderSides = []CellBorderSide{CellBorderSideBottom}
	invoice.lineProps.Alignment = CellHorizontalAlignmentRight
	invoice.totalCellProps = invoice.NewCellProps()
	invoice.totalCellProps.Alignment = CellHorizontalAlignmentRight
	invoice.number = [2]*InvoiceCell{invoice.newCell("Invoice number", invoice.defaultCellProps), invoice.newCell("", invoice.defaultCellProps)}
	invoice.date = [2]*InvoiceCell{invoice.newCell("Date", invoice.defaultCellProps), invoice.newCell("", invoice.defaultCellProps)}
	invoice.dueDate = [2]*InvoiceCell{invoice.newCell("Due Date", invoice.defaultCellProps), invoice.newCell("", invoice.defaultCellProps)}
	invoice.subtotal = [2]*InvoiceCell{invoice.newCell("Subtotal", invoice.totalCellProps), invoice.newCell("", invoice.totalCellProps)}
	totalCellProps := invoice.totalCellProps
	totalCellProps.TextStyle = textStyle
	totalCellProps.BackgroundColor = colorRGBFrom8bit
	totalCellProps.BorderColor = colorRGBFrom8bit
	invoice.total = [2]*InvoiceCell{invoice.newCell("Total", totalCellProps), invoice.newCell("", totalCellProps)}
	invoice.notes = [2]string{"Notes", ""}
	invoice.terms = [2]string{"Terms and conditions", ""}
	invoice.columns = []*InvoiceCell{invoice.newColumn("Description", CellHorizontalAlignmentLeft), invoice.newColumn("Quantity", CellHorizontalAlignmentRight), invoice.newColumn("Unit price", CellHorizontalAlignmentRight), invoice.newColumn("Amount", CellHorizontalAlignmentRight)}
	return invoice
}

// Division is a container component which can wrap across multiple pages.
// Currently supported drawable components:
// - *StyledParagraph
// - *Image
// - *Chart
//
// The component stacking behavior is vertical, where the drawables are drawn
// on top of each other.
type Division struct {
	taggedDrawable
	components  []VectorDrawable
	positioning Positioning
	margins     Margins
	padding     Margins
	inline      bool
	enableBreak bool
	background  *Background
}

// Opacity returns the opacity of the line.
func (l *Line) Opacity() float64 { return l.opacity }

// Chapter is used to arrange multiple drawables (paragraphs, images, etc) into a single section.
// The concept is the same as a book or a report chapter.
type Chapter struct {
	taggedDrawable
	number          int
	title           string
	heading         *StyledParagraph
	contents        []Drawable
	subchapterCount int
	showNumbering   bool
	includeInTOC    bool
	positioning     Positioning
	margins         Margins
	background      *Background
	parent          *Chapter
	toc             *TOC
	outline         *model.Outline
	outlineItem     *model.OutlineItem
	level           uint
	recordFunc      chapterRecordFunc
}

func nextResourceName(s string) string {
	allString := trailingDigitsRe.FindAllString(s, -1)
	if len(allString) == 0 {
		s = s + "0"
	} else {
		n, err := strconv.Atoi(allString[len(allString)-1])
		if err != nil {
			common.Log.Debug("Error converting digit component in resources name, fallback to basic method: %v", err)
			s = s + "0"
		} else {
			n++
			lastIndex := strings.LastIndex(s, allString[len(allString)-1])
			if lastIndex == -1 {
				s = fmt.Sprintf("%s%d", s[:len(s)-1], n)
			} else {
				s = s[:lastIndex] + strconv.Itoa(n)
			}
		}
	}
	return s
}

// SetLineOpacity sets the line opacity.
func (po *Polyline) SetLineOpacity(opacity float64) { po.lineOpacity = opacity }

// CurRow returns the currently active cell's row number.
func (t *Table) CurRow() int { curRow := (t.curCell-1)/t.cols + 1; return curRow }
func (t *Table) headerCut(headerCut int) int {
	if t.hasHeaderRows && headerCut == t.headerEndRow && headerCut < t.rows {
		return t.headerStartRow - 1
	}
	return headerCut
}

// GetRows returns the rows of the grid. The returned slice references the
// grid's internal rows; modifying its elements modifies the grid. Use
// len(GetRows()) to obtain the row count.
func (g *Grid) GetRows() []*GridRow { return g.rows }
func (tp *templateProcessor) pageBreakInvalidContext(node *templateNode) string {
	for activeParent := node.activeParent(); activeParent != nil; activeParent = activeParent.activeParent() {
		switch activeParent.start.Name.Local {
		case "division":
			continue
		case "chapter":
			return ""
		default:
			return activeParent.start.Name.Local
		}
	}
	return ""
}

// BorderOpacity returns the border opacity of the rectangle (0-1).
func (r *Rectangle) BorderOpacity() float64 { return r.borderOpacity }

// EnablePageWrap controls whether the division is wrapped across pages.
// If disabled, the division is moved in its entirety on a new page, if it
// does not fit in the available height. By default, page wrapping is enabled.
// If the height of the division is larger than an entire page, wrapping is
// enabled automatically in order to avoid unwanted behavior.
// Currently, page wrapping can only be disabled for vertical divisions.
func (d *Division) EnablePageWrap(enable bool) { d.enableBreak = enable }
func parseMargins(s string) (Margins, error) {
	floatFields, err := parseFloatFields(s)
	var margins Margins
	switch len(floatFields) {
	case 0:
	case 1:
		margins.Top, margins.Bottom, margins.Left, margins.Right = floatFields[0], floatFields[0], floatFields[0], floatFields[0]
	case 2:
		margins.Top, margins.Bottom = floatFields[0], floatFields[0]
		margins.Left, margins.Right = floatFields[1], floatFields[1]
	case 3:
		margins.Top = floatFields[0]
		margins.Left, margins.Right = floatFields[1], floatFields[1]
		margins.Bottom = floatFields[2]
	case 4:
		margins.Top, margins.Right, margins.Bottom, margins.Left = floatFields[0], floatFields[1], floatFields[2], floatFields[3]
	default:
		if err == nil {
			err = fmt.Errorf("margin expects 1-4 values, got %d", len(floatFields))
		}
	}
	return margins, err
}

// SetStyleBottom sets border style for bottom side.
func (bo *border) SetStyleBottom(style CellBorderStyle) { bo.styleBottom = style }

// FitMode returns the fit mode of the rectangle.
func (r *Rectangle) FitMode() FitMode { return r.fitMode }

// SetMargins sets the margins TOC line.
func (tl *TOCLine) SetMargins(left, right, top, bottom float64) {
	tl.left = left
	margins := &tl.paragraph.margins
	margins.Left = tl.left + float64(tl.level-1)*tl.levelOffset
	margins.Right = right
	margins.Top = top
	margins.Bottom = bottom
}

type chapterInfoEntry struct {
	chapter *Chapter
	page    int
}

func (ch *Chapter) headingNumber() string {
	var headingNumber string
	if ch.showNumbering {
		if ch.number != 0 {
			headingNumber = strconv.Itoa(ch.number) + "."
		}
		if ch.parent != nil {
			headingNumber2 := ch.parent.headingNumber()
			if headingNumber2 != "" {
				headingNumber = headingNumber2 + headingNumber
			}
		}
	}
	return headingNumber
}
func (b *Block) isEmpty() bool { return b.contents == nil || len(*b.contents) == 0 }

var listAttrs = map[string]attrApplyFunc{"indent": floatAttr(func(list *List, indent float64) { list.SetIndent(indent) }), "margin": marginAttr(func(list *List, margins Margins) {
	list.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

func (c *Creator) setActivePage(activePage *model.PdfPage) { c.activePage = activePage }

// GetTextOverflow returns the behavior applied to paragraph text which does
// not fit in the available space.
func (sp *StyledParagraph) GetTextOverflow() TextOverflow { return sp.textOverflow }
func (tp *templateProcessor) addNodeText(parent *templateNode, text string, templateText2 *templateText) error {
	value := parent.value
	if value == nil {
		return nil
	}
	switch value2 := value.(type) {
	case *TextChunk:
		if parent.hasText {
			value2.Text += text
		} else {
			value2.Text = text
			parent.hasText = true
		}
	case *StyledParagraph:
		switch parent.start.Name.Local {
		case "paragraph":
			if isBlank(text) {
				return nil
			}
			node := &templateNode{start: xml.StartElement{Name: xml.Name{Local: "text-chunk"}}, parent: parent, attrs: parent.textAttrs, line: templateText2.line, col: templateText2.col, offset: templateText2.offset}
			textChunk, err := tp.parseTextChunk(node, nil)
			if err != nil {
				return err
			}
			chunk := textChunk.(*TextChunk)
			chunk.Text = text
			value2.appendChunk(chunk)
		case "chapter-heading":
			if componentParent := parent.componentParent(); componentParent != nil {
				if chapter, ok := componentParent.value.(*Chapter); ok {
					if parent.hasText {
						chapter.title += text
					} else {
						chapter.title = text
						parent.hasText = true
					}
					value2.SetText(chapter.headingText())
				}
			}
		}
	}
	return nil
}

// NewColorPoint creates a new color and point object for use in the gradient rendering process.
func NewColorPoint(color Color, point float64) *ColorPoint { return newColorPoint(color, point) }

// SetBoundingBox set gradient color bounding box where the gradient would be rendered.
func (rs *RadialShading) SetBoundingBox(x, y, width, height float64) {
	rs.boundingBox = &model.PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

// GetArtifact returns the artifact associated with the paragraph, if any.
func (sp *StyledParagraph) GetArtifact() *model.Artifact { return sp.artifact }

var stdFontNames = map[string]model.StdFontName{"courier": model.CourierName, "courier-bold": model.CourierBoldName, "courier-oblique": model.CourierObliqueName, "courier-bold-oblique": model.CourierBoldObliqueName, "helvetica": model.HelveticaName, "helvetica-bold": model.HelveticaBoldName, "helvetica-oblique": model.HelveticaObliqueName, "helvetica-bold-oblique": model.HelveticaBoldObliqueName, "symbol": model.SymbolName, "zapf-dingbats": model.ZapfDingbatsName, "times": model.TimesRomanName, "times-bold": model.TimesBoldName, "times-italic": model.TimesItalicName, "times-bold-italic": model.TimesBoldItalicName}

// SetFillColor sets the fill color.
func (cp *CurvePolygon) SetFillColor(color Color) {
	cp.fillColor = color
	cp.polygon.FillColor = toPdfColor(color)
}
func (tp *templateProcessor) applyAttrs(node *templateNode, target interface{}, m map[string]attrApplyFunc) error {
	if err := tp.checkClasses(node); err != nil {
		return err
	}
	for _, attr := range tp.effectiveAttrs(node, m) {
		if attr.Name.Space != "" {
			continue
		}
		local := attr.Name.Local
		attrApplyFunc, ok := m[local]
		if !ok {
			_, ok := carrierAttrs[local]
			_, ok2 := containerTags[node.start.Name.Local]
			if ok && ok2 {
				if err := tp.validateCarrierValue(node, local, attr.Value); err != nil {
					return err
				}
				continue
			}
			if tp.options.Strict {
				return tp.nodeError(node, "unknown attribute %q on <%s>", local, node.start.Name.Local)
			}
			tp.nodeLogDebug(node, "Unsupported %s attribute: `%s`. Skipping.", node.start.Name.Local, local)
			continue
		}
		if attrApplyFunc == nil {
			continue
		}
		if err := attrApplyFunc(tp, node, local, target, attr.Value); err != nil {
			return err
		}
	}
	return nil
}
func newGraphicSVGStyle() *GraphicSVGStyle {
	return &GraphicSVGStyle{FillColor: "#000000", StrokeColor: "", StrokeWidth: 0, FillOpacity: 1.0}
}

// TableCell defines a table cell which can contain a Drawable as content.
type TableCell struct {
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
	row, col            int
	rowSpan             int
	colSpan             int
	wrapDisabled        bool
	content             VectorDrawable
	horizontalAlignment CellHorizontalAlignment
	verticalAlignment   CellVerticalAlignment
	indent              float64
	table               *Table
}

// GeneratePageBlocks generates the grid page blocks. Multiple blocks are
// generated if the contents wrap over multiple pages.
// Implements the Drawable interface.
func (g *Grid) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var blocks []*Block
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	g.updateRowHeights(ctx.Width - g.margins.Left - g.margins.Right)
	top := g.margins.Top
	ctx2 := ctx
	if g.positioning.IsAbsolute() {
		ctx.X = g.xPos
		ctx.Y = g.yPos
	} else {
		ctx.X += g.margins.Left
		ctx.Y += top
		ctx.Width -= g.margins.Left + g.margins.Right
		ctx.Height -= top
	}
	width := ctx.Width
	x := ctx.X
	y := ctx.Y
	height := ctx.Height
	var (
		i2               = 0
		ok               = false
		mcid             int64
		componentKObj    *model.KDict
		section          = GridRowSectionUnknown
		structureTagInfo *model.StructureTagInfo
		ok2              = g.tagInfo != nil && g.tagInfo.ApplyTag
		row3             *GridRow
	)
	if ok2 {
		beginTaggedContent(block, g.tagInfo)
		mcid = g.tagInfo.Mcid + 1
		componentKObj = g.tagInfo.ComponentKObj
	}
	for i := 0; i < len(g.rows); i++ {
		row := g.rows[i]
		contentHeight := float64(0.0)
		for i := i2; i < row.index; i++ {
			contentHeight += g.rows[i].contentHeight
		}
		ctx.Height = height - contentHeight
		if ok {
			if ok2 {
				if structureTagInfo != nil {
					endMarkedContent(block)
				}
				endMarkedContent(block)
			}
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			x = ctx.Margins.Left + g.margins.Left
			y = ctx.Margins.Top
			ctx.Height = ctx.contentHeight()
			ctx.Page++
			height = ctx.Height
			i2 = i
			contentHeight = 0
			ok = false
			if ok2 {
				beginTaggedContent(block, g.tagInfo)
				if structureTagInfo != nil {
					if row.section == section {
						beginTaggedContent(block, structureTagInfo)
					} else {
						structureTagInfo = nil
						section = GridRowSectionUnknown
						componentKObj = g.tagInfo.ComponentKObj
					}
				}
			}
		}
		if row.contentHeight > ctx.Height {
			row2 := g.cloneRow(row.index)
			type cellSplit struct{ fit, rest VectorDrawable }
			cellSplits := make([]cellSplit, len(row.cells))
			ok2 := false
			for i, cell := range row.cells {
				width2 := cell.width(g.colWidths, width)
				if cell.content == nil || cell.height(width2) <= ctx.Height {
					continue
				}
				ctx2 := ctx
				ctx2.Width = width2 - cell.indent
				var fit, content VectorDrawable
				switch content2 := cell.content.(type) {
				case *StyledParagraph:
					ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom - 0.5*content2.getTextHeight())
					head, tail, err := content2.split(ctx2)
					if err != nil {
						common.Log.Debug("ERROR: %v", err)
					}
					if head != nil && tail != nil {
						fit, content = head, tail
					}
				case *Division:
					ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom)
					if head, tail := content2.split(ctx2, ctx.contentHeight()-content2.margins.Vertical()); tail != nil {
						content = tail
						if head != nil {
							fit = head
						}
					}
				case *List:
					ctx2.Height = math.Floor(ctx.Height - content2.margins.Vertical())
					if head, tail := content2.split(ctx2, ctx.contentHeight()-content2.margins.Vertical()); tail != nil {
						content = tail
						if head != nil {
							fit = head
						}
					}
				case *Table:
					if head, tail := content2.split(ctx2, ctx.contentHeight()); tail != nil {
						content = tail
						if head != nil {
							fit = head
						}
					}
				}
				if content == nil && cell.rowSpan == 1 {
					content = cell.content
				}
				if content != nil {
					cellSplits[i] = cellSplit{fit, content}
					if fit != nil {
						ok2 = true
					}
				}
			}
			ok = true
			if ok2 {
				for i, cellSplit := range cellSplits {
					if cellSplit.rest == nil {
						continue
					}
					if cellSplit.fit != nil {
						row.cells[i].SetContent(cellSplit.fit)
					} else {
						row.cells[i].content = nil
					}
					row2.cells[i].SetContent(cellSplit.rest)
				}
				g.insertRowAfter(row.index, row2)
				row.updateRowHeight(width)
				row2.updateRowHeight(width)
			} else if row3 != row && !(contentHeight == 0 && ctx.Height >= ctx.contentHeight()-0.1) {
				row3 = row
				i -= 1
				continue
			}
		}
		if ok2 {
			if section != row.section {
				if structureTagInfo != nil {
					endMarkedContent(block)
				}
				section = row.section
				componentKObj = g.addSectionTag(section)
				structureTagInfo = &model.StructureTagInfo{Mcid: mcid, StructureType: gridSectionStructureTypes[section]}
				beginTaggedContent(block, structureTagInfo)
				mcid++
			}
			structureTypeTableRow := model.StructureTypeTableRow
			beginTaggedContent(block, &model.StructureTagInfo{Mcid: mcid, StructureType: structureTypeTableRow})
			row.AddTag(componentKObj)
			mcid++
		}
		for _, cell := range row.cells {
			var kDictionary *model.KDict
			if ok2 {
				cell.SetMarkedContentID(mcid)
				structureTypeTableData := model.StructureTypeTableData
				if row.IsHeader() {
					structureTypeTableData = model.StructureTypeTableHeaderCell
				}
				cell.SetStructureType(structureTypeTableData)
				beginTaggedContent(block, &model.StructureTagInfo{Mcid: mcid, StructureType: structureTypeTableData})
				kDictionary = model.NewKDictionary()
				kDictionary.S = core.MakeName(string(structureTypeTableData))
				kValue := model.NewKValue()
				kValue.SetMCID(int(mcid))
				kDictionary.AddChild(kValue)
				row.tagInfo.ComponentKObj.AddKChild(kDictionary)
				mcid++
			}
			left := cell.width(g.colWidths, width)
			colWidth := float64(0.0)
			for i := 0; i < cell.col; i++ {
				colWidth += g.colWidths[i] * width
			}
			height := float64(0.0)
			for i := 0; i < cell.rowSpan; i++ {
				height += g.rows[cell.row+i].contentHeight
			}
			if cell.rowSpan == 1 && height > ctx.Height {
				height = ctx.Height
			}
			ctx.Width = left
			ctx.X = x + colWidth
			ctx.Y = y + contentHeight
			border := newBorder(ctx.X, ctx.Y, left, height)
			if cell.backgroundColor != nil {
				border.SetFillColor(cell.backgroundColor)
			}
			border.SetOpacity(cell.opacity)
			border.LineStyle = cell.borderLineStyle
			border.styleLeft = cell.borderStyleLeft
			border.styleRight = cell.borderStyleRight
			border.styleTop = cell.borderStyleTop
			border.styleBottom = cell.borderStyleBottom
			if cell.borderColorLeft != nil {
				border.SetColorLeft(cell.borderColorLeft)
			}
			if cell.borderColorBottom != nil {
				border.SetColorBottom(cell.borderColorBottom)
			}
			if cell.borderColorRight != nil {
				border.SetColorRight(cell.borderColorRight)
			}
			if cell.borderColorTop != nil {
				border.SetColorTop(cell.borderColorTop)
			}
			border.SetWidthBottom(cell.borderWidthBottom)
			border.SetWidthLeft(cell.borderWidthLeft)
			border.SetWidthRight(cell.borderWidthRight)
			border.SetWidthTop(cell.borderWidthTop)
			err := block.Draw(border)
			if err != nil {
				common.Log.Debug("ERROR: %v", err)
			}
			if cell.content != nil {
				width := cell.content.Width()
				top := cell.content.Height()
				width, top, y, kChild, ok, err := layoutCellContent(cell.content, width, top, cell.verticalAlignment, false, kDictionary, mcid, left)
				if err != nil {
					return nil, ctx, err
				}
				if kChild != nil {
					if !ok {
						kDictionary.AddKChild(kChild)
					}
					mcid++
				}
				switch cell.horizontalAlignment {
				case CellHorizontalAlignmentLeft:
					ctx.X += cell.indent
					ctx.Width -= cell.indent
				case CellHorizontalAlignmentCenter:
					if left2 := left - width; left2 > 0 {
						ctx.X += left2 / 2
						ctx.Width -= left2 / 2
					}
				case CellHorizontalAlignmentRight:
					if left > width {
						ctx.X = ctx.X + left - width - cell.indent
						ctx.Width -= cell.indent
					}
				}
				y2 := ctx.Y
				height2 := ctx.Height
				ctx.Y += y
				switch cell.verticalAlignment {
				case CellVerticalAlignmentTop:
				case CellVerticalAlignmentMiddle:
					if height2 := height - top; height2 > 0 {
						ctx.Y += height2 / 2
						ctx.Height -= height2 / 2
					}
				case CellVerticalAlignmentBottom:
					if height > top {
						ctx.Y = ctx.Y + height - top
						ctx.Height = height
					}
				}
				err = block.DrawWithContext(cell.content, ctx)
				if errors.Is(err, ErrContentNotFit) {
					if drawableToFit := splitDrawableToFit(cell.content, ctx); drawableToFit != nil {
						err = block.DrawWithContext(drawableToFit, ctx)
					}
				}
				if err != nil {
					common.Log.Debug("ERROR: %v", err)
				}
				ctx.Y = y2
				ctx.Height = height2
			}
			if ok2 {
				endMarkedContent(block)
			}
		}
		ctx.Y += math.Min(row.contentHeight, ctx.Height)
		if ok2 {
			endMarkedContent(block)
		}
	}
	if ok2 {
		if structureTagInfo != nil {
			endMarkedContent(block)
		}
		endMarkedContent(block)
	}
	blocks = append(blocks, block)
	if g.positioning.IsAbsolute() {
		return blocks, ctx2, nil
	}
	ctx.X = ctx2.X
	ctx.Width = ctx2.Width
	ctx.Y += g.margins.Bottom
	ctx.Height -= g.margins.Bottom
	return blocks, ctx, nil
}

// Rows returns the total number of rows the table has.
func (t *Table) Rows() int { return t.rows }

var rectangleAttrs = map[string]attrApplyFunc{"x": floatAttr(func(rect *Rectangle, x float64) { rect.x = x }), "y": floatAttr(func(rect *Rectangle, y float64) { rect.y = y }), "width": floatAttr(func(rect *Rectangle, width float64) { rect.SetWidth(width) }), "height": floatAttr(func(rect *Rectangle, height float64) { rect.SetHeight(height) }), "fill-color": colorAttr(func(rect *Rectangle, col Color) { rect.SetFillColor(col) }), "fill-opacity": floatAttr(func(rect *Rectangle, opacity float64) { rect.SetFillOpacity(opacity) }), "border-color": colorAttr(func(rect *Rectangle, col Color) { rect.SetBorderColor(col) }), "border-opacity": floatAttr(func(rect *Rectangle, opacity float64) { rect.SetBorderOpacity(opacity) }), "border-width": floatAttr(func(rect *Rectangle, bw float64) { rect.SetBorderWidth(bw) }), "border-radius": borderRadiusAttr(func(rect *Rectangle, topLeft, topRight, bottomRight, bottomLeft float64) {
	rect.SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight)
}), "border-top-left-radius": floatAttr(func(rect *Rectangle, borderRadiusTopLeft float64) { rect.borderRadiusTopLeft = borderRadiusTopLeft }), "border-top-right-radius": floatAttr(func(rect *Rectangle, borderRadiusTopRight float64) { rect.borderRadiusTopRight = borderRadiusTopRight }), "border-bottom-left-radius": floatAttr(func(rect *Rectangle, borderRadiusBottomLeft float64) {
	rect.borderRadiusBottomLeft = borderRadiusBottomLeft
}), "border-bottom-right-radius": floatAttr(func(rect *Rectangle, borderRadiusBottomRight float64) {
	rect.borderRadiusBottomRight = borderRadiusBottomRight
}), "position": enumAttr(positioningValues, func(rect *Rectangle, position Positioning) { rect.SetPositioning(position) }), "fit-mode": enumAttr(fitModeValues, func(rect *Rectangle, fitMode FitMode) { rect.SetFitMode(fitMode) }), "margin": marginAttr(func(rect *Rectangle, margins Margins) {
	rect.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// SetMargins sets the Chapter margins: left, right, top, bottom.
// Typically not needed as the creator's page margins are used.
func (ch *Chapter) SetMargins(left, right, top, bottom float64) {
	ch.margins.Left = left
	ch.margins.Right = right
	ch.margins.Top = top
	ch.margins.Bottom = bottom
}
func (tec *TextChunk) clone() *TextChunk {
	clone := *tec
	clone.annotations = copyAnnotations(tec.annotations)
	return &clone
}
func (i *Invoice) generateTotalBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	table := newTable(4)
	table.SetMargins(0, 0, 10, 10)
	rows := [][2]*InvoiceCell{i.subtotal}
	rows = append(rows, i.totalLines...)
	rows = append(rows, i.total)
	for _, row := range rows {
		invoiceCell, invoiceCell2 := row[0], row[1]
		if invoiceCell2.Value == "" {
			continue
		}
		table.SkipCells(2)
		cell := table.NewCell()
		cell.SetBackgroundColor(invoiceCell.BackgroundColor)
		cell.SetHorizontalAlignment(invoiceCell2.Alignment)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph := newStyledParagraph(invoiceCell.TextStyle)
		styledParagraph.SetMargins(0, 0, 2, 1)
		styledParagraph.Append(invoiceCell.Value)
		cell.SetContent(styledParagraph)
		cell = table.NewCell()
		cell.SetBackgroundColor(invoiceCell2.BackgroundColor)
		cell.SetHorizontalAlignment(invoiceCell2.Alignment)
		i.setCellBorder(cell, invoiceCell)
		styledParagraph = newStyledParagraph(invoiceCell2.TextStyle)
		styledParagraph.SetMargins(0, 0, 2, 1)
		styledParagraph.Append(invoiceCell2.Value)
		cell.SetContent(styledParagraph)
	}
	return table.GeneratePageBlocks(ctx)
}

// SetAngle sets the rotation angle in degrees.
func (b *Block) SetAngle(angleDeg float64) { b.angle = angleDeg }
func (tp *templateProcessor) getNodeErrorLocation(node *templateNode, format string, a ...interface{}) string {
	s := fmt.Sprintf(format, a...)
	s2 := fmt.Sprintf("%d", node.offset)
	if node.line != 0 {
		s2 = fmt.Sprintf("%d:%d", node.line, node.col)
	}
	if tp.filename != "" {
		return fmt.Sprintf("%s [%s:%s]", s, tp.filename, s2)
	}
	return fmt.Sprintf("%s [%s]", s, s2)
}

var functionDomain = []float64{0, 1, 0, 1}

func parseTextChunkTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseTextChunk(node, nil)
}
func (tp *templateProcessor) resolveLink(s string) (*model.PdfAnnotation, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "url('") && strings.HasSuffix(s, "')") && len(s) > 7 {
		return newURILinkAnnotation(s[5:len(s)-2], ""), nil
	}
	if strings.HasPrefix(s, "page(") && strings.HasSuffix(s, ")") && len(s) > 6 {
		var (
			err  error
			n    int64
			val  float64
			val2 float64
			val3 = 1.0
			strs = strings.Split(s[5:len(s)-1], ",")
		)
		n, err = strconv.ParseInt(strings.TrimSpace(strs[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("link page parameter: %v", err)
		}
		if len(strs) >= 2 {
			val, err = strconv.ParseFloat(strings.TrimSpace(strs[1]), 64)
			if err != nil {
				return nil, fmt.Errorf("link X parameter: %v", err)
			}
		}
		if len(strs) >= 3 {
			val2, err = strconv.ParseFloat(strings.TrimSpace(strs[2]), 64)
			if err != nil {
				return nil, fmt.Errorf("link Y parameter: %v", err)
			}
		}
		if len(strs) >= 4 {
			val3, err = strconv.ParseFloat(strings.TrimSpace(strs[3]), 64)
			if err != nil {
				return nil, fmt.Errorf("link zoom parameter: %v", err)
			}
		}
		return newInternalLinkAnnotation(n-1, val, val2, val3, ""), nil
	}
	return nil, fmt.Errorf("unrecognized link %q", s)
}

// AnchorPoint defines anchor point where the center position of the radial gradient would be calculated.
type AnchorPoint int

func (tp *templateProcessor) process(ok bool) error {
	tree, err := tp.parseTree()
	if err != nil {
		return err
	}
	if err := tp.collectStyles(tree); err != nil {
		return err
	}
	for _, childValue := range tree.childValues {
		switch childValue2 := childValue.(type) {
		case *templateNode:
			if err := tp.walk(childValue2, nil, ok); err != nil {
				return err
			}
		case *templateText:
			if err := tp.checkTopLevelText(childValue2); err != nil {
				return err
			}
		}
	}
	return nil
}

var trailingDigitsRe = regexp.MustCompile("\\d+")

func newInternalLinkAnnotation(val int64, val2, val3, val4 float64, s string) *model.PdfAnnotation {
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	borderStyle := model.NewBorderStyle()
	borderStyle.SetBorderWidth(0)
	pdfAnnotationLink.BS = borderStyle.ToPdfObject()
	if val < 0 {
		val = 0
	}
	pdfAnnotationLink.Dest = core.MakeArray(core.MakeInteger(val), core.MakeName("XYZ"), core.MakeFloat(val2), core.MakeFloat(val3), core.MakeFloat(val4))
	if s != "" {
		pdfAnnotationLink.Contents = core.MakeString(s)
	}
	return pdfAnnotationLink.PdfAnnotation
}
func parseLineTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseLine(node)
}

// SetBorderWidth sets the border width of the rectangle.
func (r *Rectangle) SetBorderWidth(bw float64) { r.borderWidth = bw }

// Rectangle defines a rectangle with upper left corner at (x,y) and a specified width and height.  The rectangle
// can have a colored fill and/or border with a specified width.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Rectangle struct {
	taggedDrawable
	x                       float64
	y                       float64
	width                   float64
	height                  float64
	positioning             Positioning
	fillColor               Color
	fillOpacity             float64
	borderColor             Color
	borderWidth             float64
	borderOpacity           float64
	borderRadiusTopLeft     float64
	borderRadiusTopRight    float64
	borderRadiusBottomLeft  float64
	borderRadiusBottomRight float64
	margins                 Margins
	fitMode                 FitMode
}

func (gse *GraphicSVGElement) drawPolygon(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	svgPoints, err := parseSVGPoints(gse.Attributes["points"])
	if err != nil {
		common.Log.Debug("ERROR unable to parse points attribute: %v", err)
		return
	}
	if len(svgPoints)%2 > 0 {
		common.Log.Debug("ERROR invalid points attribute length")
		return
	}
	for i := 0; i < len(svgPoints); {
		if i == 0 {
			cc.Add_m(svgPoints[i]*gse.scale, svgPoints[i+1]*gse.scale)
		} else {
			cc.Add_l(svgPoints[i]*gse.scale, svgPoints[i+1]*gse.scale)
		}
		i += 2
	}
	cc.Add_l(svgPoints[0]*gse.scale, svgPoints[1]*gse.scale)
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}
func buildPath(commands2 []*Command) *Path {
	path := &Path{}
	var commands []*Command
	for i, command := range commands2 {
		switch strings.ToLower(command.Symbol) {
		case svgCommands.moveSymbol:
			if len(commands) > 0 {
				path.Subpaths = append(path.Subpaths, &Subpath{commands})
			}
			commands = []*Command{command}
		case svgCommands.closeSymbol:
			commands = append(commands, command)
			path.Subpaths = append(path.Subpaths, &Subpath{commands})
			commands = []*Command{}
		default:
			commands = append(commands, command)
			if len(commands2) == i+1 {
				path.Subpaths = append(path.Subpaths, &Subpath{commands})
			}
		}
	}
	return path
}

type imageMaskKind int

func (tp *templateProcessor) checkClasses(node *templateNode) error {
	for _, s := range node.classNames() {
		if _, ok := tp.classStyles[s]; ok {
			continue
		}
		if tp.options.Strict {
			return tp.nodeError(node, "undefined class %q", s)
		}
		tp.nodeLogDebug(node, "Undefined class %q. Skipping.", s)
	}
	return nil
}
func encoderAttr(i interface{}) attrApplyFunc {
	value2 := validateSetter("encoderAttr", i, 0, streamEncoderType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target interface{}, value string) error {
		encoderFactory, ok := encoderFactories[value]
		if !ok {
			if proc.options.Strict {
				return proc.nodeError(node, "invalid value %q for attribute %q", value, attrName)
			}
			return nil
		}
		callSetter(value2, target, reflect.ValueOf(encoderFactory()))
		return nil
	}
}

// AddExternalLinkWithTag adds a new external link to the paragraph with proper tagging for accessibility.
// The text parameter represents the text that is displayed and the url parameter sets the destination.
// The options parameter contains accessibility properties like tooltip, altText, and mcid.
func (sp *StyledParagraph) AddExternalLinkWithTag(text, url string, options LinkTagOptions) (*TextChunk, *model.KDict) {
	uriLinkAnnotation := newURILinkAnnotation(url, options.Tooltip)
	accessibleLinkChunk, kDict, err := sp.createAccessibleLinkChunk(text, uriLinkAnnotation, options)
	if err != nil {
		common.Log.Error("Failed to add external link with tag: %v", err)
		return nil, nil
	}
	return accessibleLinkChunk, kDict
}

// NewInvoice returns an instance of an empty invoice.
func (c *Creator) NewInvoice() *Invoice {
	textStyle := c.NewTextStyle()
	textStyle.Font = c.fontBold
	return newInvoice(c.NewTextStyle(), textStyle)
}

// SetWidthBottom sets border width for bottom.
func (bo *border) SetWidthBottom(bw float64) { bo.widthBottom = bw }

// AddLine appends a new line to the invoice line items table.
func (i *Invoice) AddLine(values ...string) []*InvoiceCell {
	columnsLen := len(i.columns)
	var invoiceCells []*InvoiceCell
	for j, value := range values {
		cell := i.newCell(value, i.lineProps)
		if j < columnsLen {
			cell.Alignment = i.columns[j].Alignment
		}
		invoiceCells = append(invoiceCells, cell)
	}
	i.lines = append(i.lines, invoiceCells)
	return invoiceCells
}

// SetLanguage sets the language identifier that will be stored inside document catalog.
func (c *Creator) SetLanguage(language string) { c.language = language }

// GetMargins returns the margins of the rectangle: left, right, top, bottom.
func (r *Rectangle) GetMargins() (float64, float64, float64, float64) {
	return r.margins.Left, r.margins.Right, r.margins.Top, r.margins.Bottom
}

var tableAttrs = map[string]attrApplyFunc{"columns": nil, "column-widths": floatArrayAttrErr(func(table *Table, widths []float64) error { return table.SetColumnWidths(widths...) }), "margin": marginAttr(func(table *Table, margins Margins) {
	table.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "x": floatAttr(func(table *Table, x float64) { table.SetPos(x, table.yPos) }), "y": floatAttr(func(table *Table, y float64) { table.SetPos(table.xPos, y) }), "header-start-row": intAttr(func(table *Table, n int64) { table.headerStartRow = int(n) }), "header-end-row": intAttr(func(table *Table, n int64) { table.headerEndRow = int(n) }), "enable-row-wrap": boolAttr(func(table *Table, enable bool) { table.EnableRowWrap(enable) }), "enable-page-wrap": boolAttr(func(table *Table, enable bool) { table.EnablePageWrap(enable) })}

func (gse *GraphicSVGElement) toContentStream(creator *contentstream.ContentCreator, resources *model.PdfPageResources) {
	svgStyle, err := parseSVGStyle(gse.Attributes, gse.scale)
	if err != nil {
		common.Log.Debug("Unable to parse style attribute: %v", err)
	}
	gse.Style = svgStyle
	switch gse.Name {
	case "path":
		gse.drawPath(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "rect":
		gse.drawRect(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "circle":
		gse.drawCircle(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "ellipse":
		gse.drawEllipse(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "polyline":
		gse.drawPolyline(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "polygon":
		gse.drawPolygon(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "line":
		gse.drawLine(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "text":
		gse.drawText(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "g":
		attribute, ok := gse.Attributes["fill"]
		attribute2, ok2 := gse.Attributes["stroke"]
		attribute3, ok3 := gse.Attributes["stroke-width"]
		attribute4, ok4 := gse.Attributes["transform"]
		for _, graphicSVGElement := range gse.Children {
			if _, ok2 := graphicSVGElement.Attributes["fill"]; !ok2 && ok {
				graphicSVGElement.Attributes["fill"] = attribute
			}
			if _, ok := graphicSVGElement.Attributes["stroke"]; !ok && ok2 {
				graphicSVGElement.Attributes["stroke"] = attribute2
			}
			if _, ok := graphicSVGElement.Attributes["stroke-width"]; !ok && ok3 {
				graphicSVGElement.Attributes["stroke-width"] = attribute3
			}
			if _, ok := graphicSVGElement.Attributes["transform"]; !ok && ok4 {
				graphicSVGElement.Attributes["transform"] = attribute4
			}
			graphicSVGElement.toContentStream(creator, resources)
		}
	}
}

// TextRenderingMode determines whether showing text shall cause glyph
// outlines to be stroked, filled, used as a clipping boundary, or some
// combination of the three.
// See section 9.3 "Text State Parameters and Operators" and
// Table 106 (pp. 254-255 PDF32000_2008).
type TextRenderingMode int

func (tl *TOCLine) getLineLink() *model.PdfAnnotation {
	if tl.linkPage <= 0 {
		return nil
	}
	return newInternalLinkAnnotation(tl.linkPage-1, tl.linkX, tl.linkY, 0, "")
}

// SetColPosition sets cell column position.
func (tc *TableCell) SetColPosition(col int) { tc.col = col }
func (tp *templateProcessor) parsePageBreak(node *templateNode) (interface{}, error) {
	pageBreak := newPageBreak()
	if err := tp.applyAttrs(node, pageBreak, emptyAttrs); err != nil {
		return nil, err
	}
	return pageBreak, nil
}
func (sp *StyledParagraph) getLineMetrics(n int) (maxCapHeight, maxFontSize, minDescent float64) {
	if sp.lines == nil || (sp.lines != nil && len(sp.lines) == 0) {
		sp.wrapText()
	}
	if n < 0 || n > len(sp.lines)-1 {
		common.Log.Debug("ERROR: invalid paragraph line index %d. Returning 0, 0", n)
		return 0, 0, 0
	}
	return chunksFontMetrics(sp.lines[n])
}
func (li *List) itemWidth(width, markerWidth float64) float64 {
	indent := width - li.margins.Horizontal() - li.indent
	return indent * (1 - markerWidth/width)
}
func (tp *templateProcessor) validateFontValue(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	ok := false
	for _, s2 := range strings.Split(s, ",") {
		s2 = strings.TrimSpace(s2)
		if s2 == "" {
			continue
		}
		ok = true
		if _, ok := tp.options.FontMap[s2]; ok {
			return nil
		}
		if _, ok := stdFontNames[s2]; ok {
			return nil
		}
		if attrPropList := tp.parseAttrPropList(s2); len(attrPropList) > 0 {
			if _, ok := attrPropList["path"]; ok {
				return nil
			}
		}
	}
	if ok {
		return fmt.Errorf("unresolved font %q", s)
	}
	return nil
}

// SetMargins sets the margins of the graphic svg component.
func (gs *GraphicSVG) SetMargins(left, right, top, bottom float64) {
	gs.margins.Left = left
	gs.margins.Right = right
	gs.margins.Top = top
	gs.margins.Bottom = bottom
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (rs *RadialShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }

// GetCells returns the cells of the table. The returned slice references the
// table's internal cells; modifying its elements modifies the table.
func (t *Table) GetCells() []*TableCell { return t.cells }

// GetHeaderRows returns the inclusive start and end rows of the table header.
// The returned values are only meaningful when HasHeader returns true.
func (t *Table) GetHeaderRows() (startRow, endRow int) { return t.headerStartRow, t.headerEndRow }
func (sp *StyledParagraph) wrapChunks(ok2 bool) error {
	if !sp.enableWrap || int(sp.width) <= 0 {
		sp.lines = [][]*TextChunk{sp.chunks}
		return nil
	}
	for _, chunk := range sp.chunks {
		if chunk.Style.HorizontalScaling == 0 {
			chunk.Style.HorizontalScaling = DefaultHorizontalScaling
		}
	}
	if sp.enableWordWrap {
		sp.wrapWordChunks()
	}
	if sp.dropCaps != nil && len(sp.chunks) > 0 && len(sp.chunks[0].Text) > 0 {
		chunk := sp.chunks[0]
		dropCaps := sp.dropCaps
		if !core.IsTextWriteDirectionLTR(chunk.Text) {
			common.Log.Debug("Drop caps not supported for RTL text")
			sp.dropCaps = nil
		} else if dropCaps.Type != DropCapsNone && !sp.dropCapsEnabled {
			var chunk2 *TextChunk
			switch dropCaps.Scope {
			case DropCapsFirstCharacter:
				text := []rune(chunk.Text)
				if len(text) > 0 {
					chunk2 = chunk.clone()
					chunk2.Text = string(text[0])
					if dropCaps.Style != nil {
						chunk2.Style = *dropCaps.Style
					}
					if chunk2.Style.HorizontalScaling == 0 {
						chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
					}
					if len(text) > 1 {
						chunk2 := chunk.clone()
						chunk2.Text = string(text[1:])
						if chunk2.Style.HorizontalScaling == 0 {
							chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
						}
						sp.chunks[0] = chunk2
					}
				}
			case DropCapsFirstWord:
				text := chunk.Text
				indexAny := strings.IndexAny(text, " \t\n")
				chunk2 = chunk.clone()
				chunk3 := chunk.clone()
				if indexAny > 0 {
					chunk2.Text = text[:indexAny]
					chunk3.Text = strings.TrimSpace(text[indexAny:])
				} else {
					chunk2.Text = text
					chunk3.Text = ""
				}
				if dropCaps.Style != nil {
					chunk2.Style = *dropCaps.Style
				}
				if chunk2.Style.HorizontalScaling == 0 {
					chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
				}
				if chunk3.Style.HorizontalScaling == 0 {
					chunk3.Style.HorizontalScaling = DefaultHorizontalScaling
				}
				sp.chunks[0] = chunk3
			default:
				text := []rune(chunk.Text)
				if len(text) > 0 {
					chunk2 = chunk.clone()
					chunk2.Text = string(text[0])
					if dropCaps.Style != nil {
						chunk2.Style = *dropCaps.Style
					}
					if chunk2.Style.HorizontalScaling == 0 {
						chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
					}
					if len(text) > 1 {
						chunk2 := chunk.clone()
						chunk2.Text = string(text[1:])
						if chunk2.Style.HorizontalScaling == 0 {
							chunk2.Style.HorizontalScaling = DefaultHorizontalScaling
						}
						sp.chunks[0] = chunk2
					}
				}
			}
			if chunk2 != nil {
				numLines := dropCaps.NumLines
				if numLines <= 0 {
					numLines = 3
				}
				var font *model.PdfFont
				var fontSize float64
				if len(sp.chunks) > 0 {
					font = sp.chunks[0].Style.Font
					fontSize = sp.chunks[0].Style.FontSize
				} else if sp.defaultStyle.Font != nil {
					font = sp.defaultStyle.Font
					fontSize = sp.defaultStyle.FontSize
				} else {
					font = chunk2.Style.Font
					fontSize = chunk2.Style.FontSize
				}
				fontMetrics := newFontMetrics(font, fontSize)
				capHeight := fontMetrics.capHeight
				value := fontSize
				capHeight2 := capHeight + value
				if capHeight2 > value {
					value = capHeight2
				}
				capHeight3 := (value * float64(numLines)) - capHeight
				if capHeight3 <= 0 {
					capHeight3 = fontSize * float64(numLines)
				}
				chunk2.Style.FontSize = capHeight3
				sp.dropCapsEnabled = true
				chunks := sp.chunks
				sp.chunks = []*TextChunk{chunk2}
				sp.chunks = append(sp.chunks, chunks...)
			}
		}
	}
	sp.lines = [][]*TextChunk{}
	var chunks []*TextChunk
	var value float64
	f := unicode.IsSpace
	if !ok2 {
		f = func(rune) bool { return false }
	}
	value2 := roundTo(sp.width*1000.0, 0.000001)
	n := 0
	i2 := 0
	var chunk *TextChunk
	if len(sp.chunks) > 0 && sp.dropCaps != nil {
		dropCaps := sp.dropCaps
		if dropCaps.Type != DropCapsNone && sp.dropCapsEnabled {
			chunk = sp.chunks[0]
			i2 = 1
		}
	}
	for i := i2; i < len(sp.chunks); i++ {
		chunk2 := sp.chunks[i]
		style := chunk2.Style
		annotations := chunk2.annotations
		verticalAlignment := chunk2.VerticalAlignment
		var (
			rs     []rune
			floats []float64
		)
		isTextWriteDirectionLTR := core.IsTextWriteDirectionLTR(chunk2.Text)
		for _, r := range chunk2.Text {
			if r == '\u000A' {
				if !ok2 {
					rs = append(rs, r)
				}
				chunks = append(chunks, &TextChunk{taggedDrawable: chunk2.taggedDrawable, Text: strings.TrimRightFunc(string(rs), f), Style: style, annotations: copyAnnotations(annotations), VerticalAlignment: verticalAlignment, textExpansion: chunk2.textExpansion, actualText: chunk2.actualText, altText: chunk2.altText})
				if line := sp.addLine(chunks); !line {
					return nil
				}
				n++
				chunks = nil
				value = 0
				rs = nil
				floats = nil
				continue
			}
			ok3 := r == ' '
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if runeMetrics.Wx == 0 && style.MultiFont != nil || style.MultiFont != nil && !ok {
				runeMetrics, ok = style.MultiFont.GetRuneMetrics(r)
			}
			if !ok {
				common.Log.Debug("Rune char metrics not found! %v\n", r)
				return errors.New("glyph char metrics missing")
			}
			value3 := style.FontSize * runeMetrics.Wx * style.horizontalScale()
			value4 := value3
			if !ok3 {
				value4 = value3 + style.CharSpacing*1000.0
			}
			value5 := value2
			if chunk != nil {
				dropCaps := sp.dropCaps
				var charSpacing float64
				text := []rune(chunk.Text)
				for i, r := range text {
					runeMetrics, ok := chunk.Style.Font.GetRuneMetrics(r)
					if ok {
						charSpacing += chunk.Style.FontSize * runeMetrics.Wx * chunk.Style.horizontalScale() / 1000.0
						if i < len(text)-1 {
							charSpacing += chunk.Style.CharSpacing
						}
					}
				}
				gap := dropCaps.Gap
				if gap <= 0 {
					gap = 5.0
				}
				switch dropCaps.Type {
				case DropCapsDrop:
					if n < dropCaps.NumLines {
						value5 = value2 - (charSpacing * 1000.0) - (gap * 1000.0)
					}
				case DropCapsInline:
					value5 = value2 - (charSpacing * 1000.0) - (gap * 1000.0)
				}
			}
			if value+value3 > value5 {
				n2 := -1
				if !ok3 {
					for i := len(rs) - 1; i >= 0; i-- {
						if rs[i] == ' ' {
							n2 = i
							break
						}
					}
				}
				if sp.enableWordWrap {
					chunksLen := len(chunks)
					if chunksLen > 0 {
						chunks[chunksLen-1].Text = strings.TrimRightFunc(chunks[chunksLen-1].Text, f)
						sp.lines = append(sp.lines, chunks)
						chunks = []*TextChunk{}
					}
					rs = append(rs, r)
					floats = append(floats, value4)
					if n2 >= 0 {
						rs = rs[n2+1:]
						floats = floats[n2+1:]
					}
					value = 0
					for _, float := range floats {
						value += float
					}
					if value > value2 {
						formatWriteDirectionLTR := string(rs[:len(rs)-1])
						if !sp.enableShaping {
							formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
						}
						if !ok2 && ok3 {
							formatWriteDirectionLTR += " "
						}
						chunks = append(chunks, &TextChunk{taggedDrawable: chunk2.taggedDrawable, Text: strings.TrimRightFunc(formatWriteDirectionLTR, f), Style: style, annotations: copyAnnotations(annotations), VerticalAlignment: verticalAlignment, textExpansion: chunk2.textExpansion, actualText: chunk2.actualText, altText: chunk2.altText})
						if line := sp.addLine(chunks); !line {
							return nil
						}
						n++
						chunks = []*TextChunk{}
						rs = []rune{r}
						floats = []float64{value4}
						value = value4
					}
					continue
				}
				formatWriteDirectionLTR := string(rs)
				if n2 >= 0 {
					formatWriteDirectionLTR = string(rs[0 : n2+1])
					rs = rs[n2+1:]
					rs = append(rs, r)
					floats = floats[n2+1:]
					floats = append(floats, value4)
					value = 0
					for _, float := range floats {
						value += float
					}
				} else {
					if ok3 {
						value = 0
						rs = []rune{}
						floats = []float64{}
					} else {
						value = value4
						rs = []rune{r}
						floats = []float64{value4}
					}
				}
				if !sp.enableShaping {
					formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
				}
				if !ok2 && ok3 {
					formatWriteDirectionLTR += " "
				}
				chunks = append(chunks, &TextChunk{taggedDrawable: chunk2.taggedDrawable, Text: strings.TrimRightFunc(formatWriteDirectionLTR, f), Style: style, annotations: copyAnnotations(annotations), VerticalAlignment: verticalAlignment, textExpansion: chunk2.textExpansion, actualText: chunk2.actualText, altText: chunk2.altText})
				if line := sp.addLine(chunks); !line {
					return nil
				}
				n++
				chunks = []*TextChunk{}
			} else {
				value += value4
				rs = append(rs, r)
				floats = append(floats, value4)
			}
		}
		if len(rs) > 0 {
			formatWriteDirectionLTR := string(rs)
			if !sp.enableShaping {
				formatWriteDirectionLTR = core.FormatWriteDirectionLTR(formatWriteDirectionLTR, isTextWriteDirectionLTR)
			}
			chunks = append(chunks, &TextChunk{taggedDrawable: chunk2.taggedDrawable, Text: formatWriteDirectionLTR, Style: style, annotations: copyAnnotations(annotations), VerticalAlignment: verticalAlignment, textExpansion: chunk2.textExpansion, actualText: chunk2.actualText, altText: chunk2.altText})
		}
	}
	if len(chunks) > 0 {
		if line := sp.addLine(chunks); !line {
			return nil
		}
		n++
	}
	if chunk != nil {
		if len(sp.lines) > 0 {
			sp.lines[0] = append([]*TextChunk{chunk}, sp.lines[0]...)
		} else {
			sp.lines = append(sp.lines, []*TextChunk{chunk})
		}
	}
	return nil
}

// SetBorderColor sets the border color.
func (pbc *PolyBezierCurve) SetBorderColor(color Color) { pbc.curve.BorderColor = toPdfColor(color) }

// SetMargins sets the margins of the paragraph.
func (li *List) SetMargins(left, right, top, bottom float64) {
	li.margins.Left = left
	li.margins.Right = right
	li.margins.Top = top
	li.margins.Bottom = bottom
}
func parseCommands(tokens []token) ([]*Command, error) {
	var (
		commands []*Command
		floats   []float64
	)
	for i := len(tokens) - 1; i >= 0; i-- {
		token := tokens[i]
		if token.isCommand {
			paramCount := svgCommands.paramCounts[strings.ToLower(token.text)]
			floatsLen := len(floats)
			if paramCount == 0 && floatsLen == 0 {
				command := &Command{Symbol: token.text}
				commands = append([]*Command{command}, commands...)
			} else if paramCount != 0 && floatsLen%paramCount == 0 {
				floatsLen2 := floatsLen / paramCount
				for i := 0; i < floatsLen2; i++ {
					text := token.text
					if text == "m" && i < floatsLen2-1 {
						text = "l"
					}
					if text == "M" && i < floatsLen2-1 {
						text = "L"
					}
					command := &Command{text, reverseFloats(floats[:paramCount])}
					commands = append([]*Command{command}, commands...)
					floats = floats[paramCount:]
				}
			} else {
				pathParserError2 := pathParserError{"Incorrect number of parameters for " + token.text}
				return nil, pathParserError2
			}
		} else {
			svgLength, err := parseSVGLength(token.text, 64)
			if err != nil {
				return nil, err
			}
			floats = append(floats, svgLength)
		}
	}
	return commands, nil
}

// GeneratePageBlocks draws the polygon on a new block representing the page.
// Implements the Drawable interface.
func (p *Polygon) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(p.fillOpacity, p.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	polygon := p.polygon
	polygon.FillEnabled = polygon.FillColor != nil
	polygon.BorderEnabled = polygon.BorderColor != nil && polygon.BorderWidth > 0
	points := polygon.Points
	rect := model.PdfRectangle{}
	ok := false
	for i := range points {
		for j := range points[i] {
			point := &points[i][j]
			point.Y = ctx.PageHeight - point.Y
			if !ok {
				rect.Llx = point.X
				rect.Lly = point.Y
				rect.Urx = point.X
				rect.Ury = point.Y
				ok = true
			} else {
				rect.Llx = math.Min(rect.Llx, point.X)
				rect.Lly = math.Min(rect.Lly, point.Y)
				rect.Urx = math.Max(rect.Urx, point.X)
				rect.Ury = math.Max(rect.Ury, point.Y)
			}
		}
	}
	if polygon.FillEnabled {
		err := applyShapeColor(block, p.polygon.FillColor, p.fillColor, func() Rectangle {
			return Rectangle{x: rect.Llx, y: rect.Lly, width: rect.Width(), height: rect.Height()}
		})
		if err != nil {
			return nil, ctx, err
		}
	}
	markedDraw, _, err := polygon.MarkedDraw(gsName, p.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// Width returns Image's document width.
func (im *Image) Width() float64 { return im.width }
func (ia *InvoiceAddress) fmtLine(s, s2 string, ok bool) string {
	if ok {
		s2 = ""
	}
	return fmt.Sprintf("%s%s\n", s2, s)
}

// SetBackgroundColor sets the cell's background color.
func (gc *GridCell) SetBackgroundColor(col Color) { gc.backgroundColor = col }

// AddTag adds a tag to the division.
func (d *Division) AddTag(rootKObj *model.KDict) {
	if d.tagInfo == nil {
		d.tagInfo = model.NewStructureTagInfo()
		d.tagInfo.StructureType = model.StructureTypeDivision
	}
	d.tagInfo.ApplyTag = true
	d.tagInfo.ParentKObj = rootKObj
	d.tagInfo.ComponentKObj = d.tagInfo.NewComponentKObj(d.tagInfo.StructureType)
	d.tagInfo.ParentKObj.AddKChild(d.tagInfo.ComponentKObj)
}

// Cols returns the number of columns the grid has.
func (g *Grid) Cols() int { return g.cols }

// SetInline sets the inline mode of the division.
func (d *Division) SetInline(inline bool) { d.inline = inline }
func (d *Division) drawBackground(background []*Block, ctx2, ctx3 DrawContext, ok2 bool, m map[int]float64, m2 map[int]bool) ([]*Block, error) {
	backgroundLen := len(background)
	if backgroundLen == 0 || d.background == nil {
		return background, nil
	}
	n, n2 := -1, -1
	for i, block := range background {
		if !block.isEmpty() {
			if n == -1 {
				n = i
			}
			n2 = i
		}
	}
	ok3 := len(d.components) == 0
	if n == -1 && ok3 {
		n, n2 = 0, len(background)-1
	}
	blocks := make([]*Block, 0, len(background))
	ok4 := false
	for i, block := range background {
		var (
			borderRadiusTopLeft     = d.background.BorderRadiusTopLeft
			borderRadiusTopRight    = d.background.BorderRadiusTopRight
			borderRadiusBottomLeft  = d.background.BorderRadiusBottomLeft
			borderRadiusBottomRight = d.background.BorderRadiusBottomRight
		)
		ctx := ctx2
		ctx.Page += i
		value, ok := m[i]
		ok5 := m2[i] || ok4 || i == n
		if i == 0 {
			if ok2 {
				blocks = append(blocks, block)
				continue
			}
			if ok {
				ctx.Height = value - ctx2.Y
			} else if backgroundLen == 1 {
				ctx.Height = ctx3.Y - ctx2.Y
			}
		} else {
			ctx.X = ctx.Margins.Left + d.margins.Left
			ctx.Y = ctx.Margins.Top
			ctx.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - d.margins.Left - d.margins.Right
			if ok {
				ctx.Height = value - ctx.Margins.Top
			} else if i == backgroundLen-1 {
				ctx.Height = ctx3.Y - ctx.Margins.Top - d.margins.Top
			} else {
				ctx.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
			}
			if !ok5 {
				borderRadiusTopLeft = 0
				borderRadiusTopRight = 0
			}
		}
		if (block.isEmpty() && !ok3) || ctx.Height <= 0 {
			if m2[i] {
				ok4 = true
			}
			blocks = append(blocks, block)
			continue
		}
		ok4 = false
		if i != n2 && !ok {
			borderRadiusBottomLeft = 0
			borderRadiusBottomRight = 0
		}
		rectangle := newRectangle(ctx.X, ctx.Y, ctx.Width, ctx.Height)
		rectangle.SetFillColor(d.background.FillColor)
		rectangle.SetBorderColor(d.background.BorderColor)
		rectangle.SetBorderWidth(d.background.BorderSize)
		rectangle.SetBorderRadius(borderRadiusTopLeft, borderRadiusTopRight, borderRadiusBottomLeft, borderRadiusBottomRight)
		pageBlocks, _, err := rectangle.GeneratePageBlocks(ctx)
		if err != nil {
			return nil, err
		}
		if len(pageBlocks) == 0 {
			blocks = append(blocks, block)
			continue
		}
		pageBlock := pageBlocks[0]
		if err = pageBlock.mergeBlocks(block); err != nil {
			return nil, err
		}
		blocks = append(blocks, pageBlock)
	}
	return blocks, nil
}

// Polygon represents a polygon shape.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type Polygon struct {
	taggedDrawable
	polygon       *contentdraw.Polygon
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

func (pat *ParsedTemplate) execute(data interface{}) ([]byte, error) {
	buffer := bytes.NewBuffer(nil)
	if err := pat.tmpl.Execute(buffer, data); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func (tp *templateProcessor) parseTableCell(node *templateNode) (interface{}, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "Table cell parent cannot be nil.")
		return nil, errInvalidParent
	}
	table, ok := componentParent.value.(*Table)
	if !ok {
		tp.nodeLogError(node, "Table cell parent (%T) is not a table.", componentParent.value)
		return nil, errInvalidParent
	}
	var n, n2 int64
	for _, s := range []string{"colspan", "rowspan"} {
		effectiveAttrValue, ok := tp.effectiveAttrValue(node, s)
		if !ok {
			continue
		}
		int642, err := parseInt64(effectiveAttrValue)
		if err != nil {
			if err2 := tp.strictErr(node, s, effectiveAttrValue, err); err2 != nil {
				return nil, err2
			}
			int642 = 0
		}
		if s == "colspan" {
			n = int642
		} else {
			n2 = int642
		}
	}
	if n <= 0 {
		n = 1
	}
	if n2 <= 0 {
		n2 = 1
	}
	multiCell := table.MultiCell(int(n2), int(n))
	if err := tp.applyAttrs(node, multiCell, tableCellAttrs); err != nil {
		return nil, err
	}
	return multiCell, nil
}

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (s *shading) SetAntiAlias(enable bool) { s.antiAlias = enable }

// MoveRight moves the drawing context right by relative displacement dx (negative goes left).
func (c *Creator) MoveRight(dx float64) { c.context.X += dx }
func parseChapterTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseChapter(node)
}
func newRadialShading(x0 float64, y0 float64, r0 float64, r1 float64, colorPoints []*ColorPoint) *RadialShading {
	return &RadialShading{base: &shading{backgroundColor: ColorWhite, antiAlias: false, extends: []bool{false, false}, colorPoints: colorPoints}, x0: x0, y0: y0, r0: r0, r1: r1, anchor: AnchorCenter}
}

// GetPositioning returns the positioning mode (relative or absolute) of the
// paragraph.
func (sp *StyledParagraph) GetPositioning() Positioning { return sp.positioning }

// SetBorderLineStyle sets border style (currently dashed or plain).
func (tc *TableCell) SetBorderLineStyle(style contentdraw.LineStyle) { tc.borderLineStyle = style }

// SetColumnWidths sets the fractional column widths.
// Each width should be in the range 0-1 and is a fraction of the table width.
// The number of width inputs must match number of columns, otherwise an error is returned.
func (g *Grid) SetColumnWidths(widths ...float64) error {
	if len(widths) != g.cols {
		common.Log.Debug("Mismatching number of widths and columns")
		return errors.New("range check error")
	}
	g.colWidths = widths
	return nil
}

// NewGraphicSVGFromFile creates a graphic SVG from a file.
func NewGraphicSVGFromFile(path string) (*GraphicSVG, error) { return newGraphicSVGFromFile(path) }

// Finalize renders all blocks to the creator pages. In addition, it takes care
// of adding headers and footers, as well as generating the front page,
// table of contents and outlines.
// Finalize is automatically called before writing the document out. Calling the
// method manually can be useful when adding external pages to the creator,
// using the AddPage method, as it renders all creator blocks to the added
// pages, without having to write the document out.
// NOTE: TOC and outlines are generated only if the AddTOC and AddOutlines
// fields of the creator are set to true (enabled by default). Furthermore, TOCs
// and outlines without content are skipped. TOC and outline content is
// added automatically when using the chapter component. TOCs and outlines can
// also be set externally, using the SetTOC and SetOutlineTree methods.
// Finalize should only be called once, after all draw calls have taken place,
// as it will return immediately if the creator instance has been finalized.
func (c *Creator) Finalize() error {
	if c.finalized {
		return nil
	}
	pagesLen := len(c.pages)
	n := 0
	if c.genFrontPageFunc != nil {
		c2 := *c
		c.pages = nil
		c.activePage = nil
		c.initContext()
		frontpageFunctionArgs := FrontpageFunctionArgs{PageNum: 1, TotalPages: pagesLen}
		c.genFrontPageFunc(frontpageFunctionArgs)
		n += len(c.pages)
		c.pages = c2.pages
		c.activePage = c2.activePage
		c.chapters = c2.chapters
	}
	if c.AddTOC {
		c.initContext()
		c.context.Page = n + 1
		if c.CustomTOC && c.genTOCFunc != nil {
			c2 := *c
			c.pages = nil
			c.activePage = nil
			if err := c.genTOCFunc(c.toc); err != nil {
				return err
			}
			n += len(c.pages)
			c.pages = c2.pages
			c.activePage = c2.activePage
			c.chapters = c2.chapters
		} else {
			if c.genTOCFunc != nil {
				if err := c.genTOCFunc(c.toc); err != nil {
					return err
				}
			}
			pageBlocks, _, err := c.toc.GeneratePageBlocks(c.context)
			if err != nil {
				common.Log.Debug("Failed to generate blocks: %v", err)
				return err
			}
			n += len(pageBlocks)
		}
		lines := c.toc.Lines()
		for _, line := range lines {
			n2, err := strconv.Atoi(line.Page.Text)
			if err != nil {
				continue
			}
			line.Page.Text = strconv.Itoa(n2 + n)
			line.linkPage += int64(n)
		}
	}
	ok := false
	var pages []*model.PdfPage
	if c.genFrontPageFunc != nil {
		c2 := *c
		c.pages = nil
		c.activePage = nil
		frontpageFunctionArgs := FrontpageFunctionArgs{PageNum: 1, TotalPages: pagesLen}
		c.genFrontPageFunc(frontpageFunctionArgs)
		pagesLen += len(c.pages)
		pages = c.pages
		c.pages = append(c.pages, c2.pages...)
		c.activePage = c2.activePage
		c.chapters = c2.chapters
		ok = true
	}
	var pages2 []*model.PdfPage
	if c.AddTOC {
		c.initContext()
		if c.CustomTOC && c.genTOCFunc != nil {
			c2 := *c
			c.pages = nil
			c.activePage = nil
			if err := c.genTOCFunc(c.toc); err != nil {
				common.Log.Debug("Error generating TOC: %v", err)
				return err
			}
			pages2 = c.pages
			pagesLen += len(pages2)
			c.pages = c2.pages
			c.activePage = c2.activePage
			c.chapters = c2.chapters
		} else {
			if c.genTOCFunc != nil {
				if err := c.genTOCFunc(c.toc); err != nil {
					common.Log.Debug("Error generating TOC: %v", err)
					return err
				}
			}
			pageBlocks, _, _ := c.toc.GeneratePageBlocks(c.context)
			for _, pageBlock := range pageBlocks {
				pageBlock.SetPos(0, 0)
				pagesLen++
				page := c.newPage()
				pages2 = append(pages2, page)
				c.setActivePage(page)
				c.Draw(pageBlock)
			}
		}
		if ok {
			pages4 := pages
			pages3 := c.pages[len(pages):]
			c.pages = append([]*model.PdfPage{}, pages4...)
			c.pages = append(c.pages, pages2...)
			c.pages = append(c.pages, pages3...)
		} else {
			c.pages = append(pages2, c.pages...)
		}
	}
	if c.outline != nil && c.AddOutlines {
		var fn func(outlineItem *model.OutlineItem)
		fn = func(outlineItem *model.OutlineItem) {
			outlineItem.Dest.Page += int64(n)
			if page := int(outlineItem.Dest.Page); page >= 0 && page < len(c.pages) {
				outlineItem.Dest.PageObj = c.pages[page].GetPageAsIndirectObject()
			} else {
				common.Log.Debug("WARN: could not get page container for page %d", page)
			}
			outlineItem.Dest.Y = precision.RoundDefault(c.pageHeight - outlineItem.Dest.Y)
			items := outlineItem.Items()
			for _, item := range items {
				fn(item)
			}
		}
		items := c.outline.Items()
		for _, item := range items {
			fn(item)
		}
		if c.AddTOC {
			var pagesLen int
			if ok {
				pagesLen = len(pages)
			}
			outlineDest := model.NewOutlineDest(int64(pagesLen), 0, c.pageHeight)
			if pagesLen >= 0 && pagesLen < len(c.pages) {
				outlineDest.PageObj = c.pages[pagesLen].GetPageAsIndirectObject()
			} else {
				common.Log.Debug("WARN: could not get page container for page %d", pagesLen)
			}
			c.outline.Insert(0, model.NewOutlineItem("Table of Contents", outlineDest))
		}
	}
	if n > 0 {
		for i := range c.chapters {
			c.chapters[i].page += n
		}
	}
	for i, page := range c.pages {
		c.setActivePage(page)
		chapter := c.ChapterForPage(i + 1)
		if c.pageFinalizeFunc != nil {
			size, pageHeight, err := page.Size()
			if err != nil {
				return err
			}
			pageFinalizeFunctionArgs := PageFinalizeFunctionArgs{PageNum: i + 1, PageWidth: size, PageHeight: pageHeight, TOCPages: len(pages2), TotalPages: pagesLen, Chapter: chapter}
			if err := c.pageFinalizeFunc(pageFinalizeFunctionArgs); err != nil {
				common.Log.Debug("ERROR: page finalize callback: %v", err)
				return err
			}
		}
		if c.drawHeaderFunc != nil {
			block := NewBlock(c.pageWidth, c.pageMargins.Top)
			headerFunctionArgs := HeaderFunctionArgs{PageNum: i + 1, TotalPages: pagesLen, Chapter: chapter}
			c.drawHeaderFunc(block, headerFunctionArgs)
			block.SetPos(0, 0)
			if c.taggingEnabled && block.GetArtifact() == nil {
				block.MarkAsArtifact(model.ArtifactTypePagination)
			}
			if err := c.Draw(block); err != nil {
				common.Log.Debug("ERROR: drawing header: %v", err)
				return err
			}
		}
		if c.drawFooterFunc != nil {
			block := NewBlock(c.pageWidth, c.pageMargins.Bottom)
			footerFunctionArgs := FooterFunctionArgs{PageNum: i + 1, TotalPages: pagesLen, Chapter: chapter}
			c.drawFooterFunc(block, footerFunctionArgs)
			block.SetPos(0, c.pageHeight-block.height)
			if c.taggingEnabled && block.GetArtifact() == nil {
				block.MarkAsArtifact(model.ArtifactTypePagination)
			}
			if err := c.Draw(block); err != nil {
				common.Log.Debug("ERROR: drawing footer: %v", err)
				return err
			}
		}
		pageTransform, ok := c.pageTransforms[page]
		if block, ok2 := c.blocks[page]; ok2 {
			if ok {
				pageTransform.transformBlock(block)
			}
			if err := block.drawToPage(page); err != nil {
				common.Log.Debug("ERROR: drawing page %d blocks: %v", i+1, err)
				return err
			}
		}
		if ok {
			if err := pageTransform.transformPage(page); err != nil {
				common.Log.Debug("ERROR: could not transform page: %v", err)
				return err
			}
		}
	}
	c.finalized = true
	return nil
}

// AddColorStop add color stop info for rendering gradient color.
func (ls *LinearShading) AddColorStop(color Color, point float64) {
	ls.base.AddColorStop(color, point)
}

// SetLineMargins sets the margins for all new lines of the table of contents.
func (to *TOC) SetLineMargins(left, right, top, bottom float64) {
	lineMargins := &to.lineMargins
	lineMargins.Left = left
	lineMargins.Right = right
	lineMargins.Top = top
	lineMargins.Bottom = bottom
}
func (t *Table) addRowTag() *model.KDict {
	kDictionary := model.NewKDictionary()
	kDictionary.S = core.MakeName(string(model.StructureTypeTableRow))
	t.tagInfo.ComponentKObj.AddKChild(kDictionary)
	return kDictionary
}

// SetHeaderRows turns the selected table rows into headers that are repeated
// for every page the table spans. startRow and endRow are inclusive.
func (t *Table) SetHeaderRows(startRow, endRow int) error {
	if startRow <= 0 {
		return errors.New("header start row must be greater than 0")
	}
	if endRow <= 0 {
		return errors.New("header end row must be greater than 0")
	}
	if startRow > endRow {
		return errors.New("header start row  must be less than or equal to the end row")
	}
	t.hasHeaderRows = true
	t.headerStartRow = startRow
	t.headerEndRow = endRow
	return nil
}
func parseSVGLength(value string, bitSize int) (float64, error) {
	s, unit := splitUnitSuffix(value)
	float, err := strconv.ParseFloat(s, bitSize)
	if err != nil {
		return 0, err
	}
	if unitToPoint, ok := unitToPoints[unit]; ok {
		float = float * unitToPoint
	} else {
		float = float * pxToPt
	}
	return float, nil
}
func (i *Invoice) setCellBorder(cell *TableCell, invoiceCell *InvoiceCell) {
	for _, borderSide := range invoiceCell.BorderSides {
		cell.SetBorder(borderSide, CellBorderStyleSingle, invoiceCell.BorderWidth)
	}
	cell.SetBorderColor(invoiceCell.BorderColor)
}

var chapterAttrs = map[string]attrApplyFunc{"show-numbering": boolAttr(func(chapter *Chapter, show bool) { chapter.SetShowNumbering(show) }), "include-in-toc": boolAttr(func(chapter *Chapter, includeInTOC bool) { chapter.SetIncludeInTOC(includeInTOC) }), "margin": marginAttr(func(chapter *Chapter, margins Margins) {
	chapter.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

// CellBorderStyle defines the table cell's border style.
type CellBorderStyle int

// SetLanguageIdentifier sets the language identifier for the paragraph.
func (sp *StyledParagraph) SetLanguageIdentifier(id string) {
	if sp.tagInfo == nil {
		sp.tagInfo = model.NewStructureTagInfo()
		sp.tagInfo.StructureType = model.StructureTypeParagraph
	}
	sp.languageID = id
}

type templateText struct {
	text   string
	line   int
	col    int
	offset int64
}

// Line defines a line between point 1 (X1, Y1) and point 2 (X2, Y2).
// The line width, color, style (solid or dashed) and opacity can be
// configured. Implements the Drawable interface.
type Line struct {
	taggedDrawable
	x1          float64
	y1          float64
	x2          float64
	y2          float64
	color       Color
	style       contentdraw.LineStyle
	opacity     float64
	dashArray   []int64
	dashPhase   int64
	lineWidth   float64
	positioning Positioning
	fitMode     FitMode
	margins     Margins
}

func newGraphicSVGElement(startElement xml.StartElement) *GraphicSVGElement {
	graphicSVGElement := &GraphicSVGElement{}
	m := make(map[string]string)
	for _, attr := range startElement.Attr {
		m[attr.Name.Local] = attr.Value
	}
	graphicSVGElement.Name = startElement.Name.Local
	graphicSVGElement.Attributes = m
	graphicSVGElement.scale = 1
	if graphicSVGElement.Name == "svg" {
		svgPoints, err := parseSVGPoints(m["viewBox"])
		if err != nil {
			common.Log.Debug("Unable to parse viewBox attribute: %v", err)
			return nil
		}
		if len(svgPoints) >= 4 {
			graphicSVGElement.ViewBox.X = svgPoints[0]
			graphicSVGElement.ViewBox.Y = svgPoints[1]
			graphicSVGElement.ViewBox.W = svgPoints[2]
			graphicSVGElement.ViewBox.H = svgPoints[3]
		}
		graphicSVGElement.Width = graphicSVGElement.ViewBox.W
		graphicSVGElement.Height = graphicSVGElement.ViewBox.H
		if s, ok := m["width"]; ok {
			if strings.HasSuffix(s, "%") {
				float, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
				if err != nil {
					common.Log.Debug("Unable to parse width attribute: %v", err)
					return nil
				}
				graphicSVGElement.Width = float * graphicSVGElement.ViewBox.W
			} else {
				svgLength, err := parseSVGLength(s, 64)
				if err != nil {
					common.Log.Debug("Unable to parse width attribute: %v", err)
					return nil
				}
				graphicSVGElement.Width = svgLength
				if len(svgPoints) < 4 {
					graphicSVGElement.ViewBox.W = svgLength
				}
			}
		}
		if s, ok := m["height"]; ok {
			if strings.HasSuffix(s, "%") {
				float, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
				if err != nil {
					common.Log.Debug("Unable to parse height attribute: %v", err)
					return nil
				}
				graphicSVGElement.Height = float * graphicSVGElement.ViewBox.H
			} else {
				svgLength, err := parseSVGLength(s, 64)
				if err != nil {
					common.Log.Debug("Unable to parse height attribute: %v", err)
					return nil
				}
				graphicSVGElement.Height = svgLength
				if len(svgPoints) < 4 {
					graphicSVGElement.ViewBox.H = svgLength
				}
			}
		}
		if graphicSVGElement.Width > 0 && graphicSVGElement.Height > 0 {
			graphicSVGElement.scale = graphicSVGElement.Width / graphicSVGElement.ViewBox.W
		}
	}
	return graphicSVGElement
}

// SetFillOpacity sets the fill opacity.
func (pbc *PolyBezierCurve) SetFillOpacity(opacity float64) { pbc.fillOpacity = opacity }

// BuyerAddress returns the buyer address used in the invoice template.
func (i *Invoice) BuyerAddress() *InvoiceAddress { return i.buyerAddress }

var (
	boolType          = reflect.TypeOf(false)
	int64Type         = reflect.TypeOf(int64(0))
	float64Type       = reflect.TypeOf(float64(0))
	int64SliceType    = reflect.TypeOf([]int64(nil))
	float64SliceType  = reflect.TypeOf([]float64(nil))
	stringType        = reflect.TypeOf("")
	marginsType       = reflect.TypeOf(Margins{})
	errorIfaceType    = reflect.TypeOf((*error)(nil)).Elem()
	colorType         = reflect.TypeOf((*Color)(nil)).Elem()
	streamEncoderType = reflect.TypeOf((*core.StreamEncoder)(nil)).Elem()
	pdfFontPtrType    = reflect.TypeOf((*model.PdfFont)(nil))
)

// CellHorizontalAlignment defines the table cell's horizontal alignment.
type CellHorizontalAlignment int
type imageMask struct {
	kind      imageMaskKind
	img       *model.Image
	threshold *uint8
	colorKey  []int
}

// Draw draws the drawable d on the block.
// Note that the drawable must not wrap, i.e. only return one block. Otherwise an error is returned.
func (b *Block) Draw(d Drawable) error {
	ctx := DrawContext{}
	ctx.Width = b.width
	ctx.Height = b.height
	ctx.PageWidth = b.width
	ctx.PageHeight = b.height
	ctx.X = 0
	ctx.Y = 0
	pageBlocks, _, err := d.GeneratePageBlocks(ctx)
	if err != nil {
		return err
	}
	if len(pageBlocks) != 1 {
		return ErrContentNotFit
	}
	for _, pageBlock := range pageBlocks {
		if err := b.mergeBlocks(pageBlock); err != nil {
			return err
		}
	}
	return nil
}

// SetBackgroundColor sets the cell's background color.
func (tc *TableCell) SetBackgroundColor(col Color) { tc.backgroundColor = col }

// SetPos sets the absolute position. Changes object positioning to absolute.
func (im *Image) SetPos(x, y float64) {
	im.positioning = PositionAbsolute
	im.xPos = x
	im.yPos = y
}

// SetLevelOffset sets the amount of space an indentation level occupies.
func (tl *TOCLine) SetLevelOffset(levelOffset float64) {
	tl.levelOffset = levelOffset
	tl.paragraph.margins.Left = tl.left + float64(tl.level-1)*tl.levelOffset
}
func argOrZero(argOrZero reflect.Value, typ reflect.Type) reflect.Value {
	if !argOrZero.IsValid() {
		return reflect.Zero(typ)
	}
	return argOrZero
}

// SetMargins sets the margins of the rectangle.
// NOTE: rectangle margins are only applied if relative positioning is used.
func (r *Rectangle) SetMargins(left, right, top, bottom float64) {
	r.margins.Left = left
	r.margins.Right = right
	r.margins.Top = top
	r.margins.Bottom = bottom
}

// SetShowLinks sets visibility of links for the TOC lines.
func (to *TOC) SetShowLinks(showLinks bool) { to.showLinks = showLinks }
func (sp *StyledParagraph) wrapText() error { return sp.wrapChunks(true) }

// FitMode defines resizing options of an object inside a container.
type FitMode int

// SetColorRight sets border color for right.
func (bo *border) SetColorRight(col Color) { bo.colorRight = col }

// Fit fits the chunk into the specified bounding box, cropping off the
// remainder in a new chunk, if it exceeds the specified dimensions.
// The line height is taken from Style.LineHeight when set (> 0); otherwise a
// multiplier of 1.0 is assumed, matching the historical behavior. To account
// for a line height applied at the paragraph level, propagate it into
// Style.LineHeight before calling Fit.
func (tec *TextChunk) Fit(width, height float64) (*TextChunk, error) {
	strs, err := tec.Wrap(width)
	if err != nil {
		return nil, err
	}
	height2 := tec.Height()
	if height2 <= 0 {
		return nil, errors.New("text chunk fit: non-positive effective line height")
	}
	height3 := int(height / height2)
	if height3 >= len(strs) {
		return nil, nil
	}
	fresh := "\n"
	tec.Text = strings.ReplaceAll(strings.Join(strs[:height3], " "), fresh+" ", fresh)
	text := strings.ReplaceAll(strings.Join(strs[height3:], " "), fresh+" ", fresh)
	return NewTextChunk(text, tec.Style), nil
}

// SetHeight sets the height of the ellipse.
func (e *Ellipse) SetHeight(height float64) { e.height = height }

// SetTOC sets the table of content component of the creator.
// This method should be used when building a custom table of contents.
func (c *Creator) SetTOC(toc *TOC) {
	if toc == nil {
		return
	}
	c.toc = toc
}

// NewImageFromGoImage creates an Image from a go image.Image data structure.
func (c *Creator) NewImageFromGoImage(goimg image.Image) (*Image, error) {
	return newImageFromGoImage(goimg)
}

// GetMargins returns the Block's margins: left, right, top, bottom.
func (b *Block) GetMargins() (float64, float64, float64, float64) {
	return b.margins.Left, b.margins.Right, b.margins.Top, b.margins.Bottom
}

// InvoiceAddress contains contact information that can be displayed
// in an invoice. It is used for the seller and buyer information in the
// invoice template.
type InvoiceAddress struct {
	Heading string
	Name    string
	Street  string
	Street2 string
	Zip     string
	City    string
	State   string
	Country string
	Phone   string
	Email   string

	// Separator defines the separator between different address components,
	// such as the city, state and zip code. It defaults to ", " when the
	// field is an empty string.
	Separator string

	// If enabled, the Phone field label (`Phone: `) is not displayed.
	HidePhoneLabel bool

	// If enabled, the Email field label (`Email: `) is not displayed.
	HideEmailLabel bool
}

func (i *Invoice) generateLineBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	table := newTable(len(i.columns))
	table.SetMargins(0, 0, 25, 0)
	for _, column := range i.columns {
		styledParagraph := newStyledParagraph(column.TextStyle)
		styledParagraph.SetMargins(0, 0, 1, 0)
		styledParagraph.Append(column.Value)
		cell := table.NewCell()
		cell.SetHorizontalAlignment(column.Alignment)
		cell.SetBackgroundColor(column.BackgroundColor)
		i.setCellBorder(cell, column)
		cell.SetContent(styledParagraph)
	}
	for _, line := range i.lines {
		for _, invoiceCell := range line {
			styledParagraph := newStyledParagraph(invoiceCell.TextStyle)
			styledParagraph.SetMargins(0, 0, 3, 2)
			styledParagraph.Append(invoiceCell.Value)
			cell := table.NewCell()
			cell.SetHorizontalAlignment(invoiceCell.Alignment)
			cell.SetBackgroundColor(invoiceCell.BackgroundColor)
			i.setCellBorder(cell, invoiceCell)
			cell.SetContent(styledParagraph)
		}
	}
	return table.GeneratePageBlocks(ctx)
}
func newTemplateText(data []byte, text string, line, col int, offset int64) *templateText {
	text2 := text[:len(text)-len(strings.TrimLeft(text, whitespaceChars))]
	offset2 := offset + int64(len(text2))
	if offset2 > int64(len(data)) || string(data[offset:offset2]) != text2 {
		return &templateText{text: text, line: line, col: col, offset: offset}
	}
	if line != 0 {
		for _, r := range text2 {
			if r == '\n' {
				line, col = line+1, 1
			} else {
				col++
			}
		}
	}
	return &templateText{text: text, line: line, col: col, offset: offset + int64(len(text2))}
}

// ColorGrayFromArithmetic creates a Color from a grayscale value (0-1).
// Example:
//
//	gray := ColorGrayFromArithmetic(0.7)
func ColorGrayFromArithmetic(g float64) Color { return grayColor{g} }

// SetBackgroundColor set background color of the shading area.
//
// By default the background color is set to white.
func (ls *LinearShading) SetBackgroundColor(backgroundColor Color) {
	ls.base.SetBackgroundColor(backgroundColor)
}
func (tp *templateProcessor) parseLine(node *templateNode) (interface{}, error) {
	line := tp.creator.NewLine(0, 0, 0, 0)
	if err := tp.applyAttrs(node, line, lineAttrs); err != nil {
		return nil, err
	}
	return line, nil
}

// SetTerms sets the terms and conditions section of the invoice.
func (i *Invoice) SetTerms(title, content string) { i.terms = [2]string{title, content} }

// HeaderFunctionArgs holds the input arguments to a header drawing function.
// It is designed as a struct, so additional parameters can be added in the future with backwards
// compatibility.
type HeaderFunctionArgs struct {
	PageNum    int
	TotalPages int

	// Chapter describes the chapter the current page belongs to, resolved via
	// Creator.ChapterForPage. It is nil on pages that precede the first chapter.
	Chapter *ChapterInfo
}

func validateColorKeyMask(nums []int, xObjectImage *model.XObjectImage) error {
	if xObjectImage.ColorSpace == nil {
		return errors.New("image has no colorspace")
	}
	if n := 2 * xObjectImage.ColorSpace.GetNumComponents(); len(nums) != n {
		return fmt.Errorf("color key mask needs %d values for colorspace %s, got %d", n, xObjectImage.ColorSpace.String(), len(nums))
	}
	if xObjectImage.BitsPerComponent == nil {
		return errors.New("image has no bits per component")
	}
	n := (1 << uint(*xObjectImage.BitsPerComponent)) - 1
	for _, n2 := range nums {
		if n2 > n {
			return fmt.Errorf("color key mask value %d out of range [0, %d]", n2, n)
		}
	}
	return nil
}

// Scale scales the ellipse dimensions by the specified factors.
func (e *Ellipse) Scale(xFactor, yFactor float64) {
	e.width = xFactor * e.width
	e.height = yFactor * e.height
}

// AddShadingResource adds shading dictionary inside the resources dictionary.
func (ls *LinearShading) AddShadingResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingResource(block, ls.shadingModel().ToPdfObject())
}

type pathParserError struct{ msg string }

func splitIntoWords(s string) ([]string, error) {
	if !utf8.ValidString(s) {
		return []string{s}, errors.New("input is not valid UTF-8")
	}
	var (
		strs []string
		rs   []rune
	)
	for _, r := range s {
		if r == '\u000A' {
			if len(rs) > 0 {
				strs = append(strs, string(rs))
			}
			strs = append(strs, string(r))
			rs = nil
			continue
		}
		rs = append(rs, r)
	}
	if len(rs) > 0 {
		strs = append(strs, string(rs))
	}
	var intoWords []string
	for _, str := range strs {
		r := []rune(str)
		scanner := linebreak.NewScanner(r)
		var rs []rune
		for i := 0; i < len(r); i++ {
			_, action, err := scanner.Next()
			if err != nil {
				return nil, err
			}
			if action == linebreak.BreakProhibited || unicode.IsSpace(r[i]) {
				rs = append(rs, r[i])
				if unicode.IsSpace(r[i]) {
					intoWords = append(intoWords, string(rs))
					rs = []rune{}
				}
				continue
			} else {
				if len(rs) > 0 {
					intoWords = append(intoWords, string(rs))
				}
				rs = []rune{r[i]}
			}
		}
		if len(rs) > 0 {
			intoWords = append(intoWords, string(rs))
		}
	}
	return intoWords, nil
}

// NewEllipse creates a new ellipse with the center at (`xc`, `yc`),
// having the specified width and height.
// NOTE: In relative positioning mode, `xc` and `yc` are calculated using the
// current context. Furthermore, when the fit mode is set to fill the available
// space, the ellipse is scaled so that it occupies the entire context width
// while maintaining the original aspect ratio.
func (c *Creator) NewEllipse(xc, yc, width, height float64) *Ellipse {
	return newEllipse(xc, yc, width, height)
}
func (cc cmykColor) ToRGB() (float64, float64, float64) {
	k := cc.k
	return 1 - (cc.c*(1-k) + k), 1 - (cc.m*(1-k) + k), 1 - (cc.y*(1-k) + k)
}
func rotatedCorners(rect *model.PdfRectangle, value float64) [4]contentdraw.Point {
	x := value * math.Pi / 180.0
	width := rect.Width()
	height := rect.Height()
	value2 := math.Sin(x)
	value3 := math.Cos(x)
	points := [4]contentdraw.Point{{X: precision.RoundFloat(rect.Llx, 3), Y: precision.RoundFloat(rect.Lly, 3)}, {X: precision.RoundFloat(rect.Llx+width*value3, 3), Y: precision.RoundFloat(rect.Lly+width*value2, 3)}, {X: precision.RoundFloat(rect.Llx+width*value3-height*value2, 3), Y: precision.RoundFloat(rect.Lly+width*value2+height*value3, 3)}, {X: precision.RoundFloat(rect.Llx-height*value2, 3), Y: precision.RoundFloat(rect.Lly+height*value3, 3)}}
	return points
}

var (
	attrFuncRe         = regexp.MustCompile("([\\w-]+)\\('(.+)'\\)")
	errInvalidCreator  = errors.New("invalid template creator instance")
	errInvalidParent   = errors.New("invalid template parent node")
	errInvalidChild    = errors.New("invalid template child node")
	errInvalidResource = errors.New("invalid template resource")
	errInvalidReader   = errors.New("invalid template reader")
)

// SetColorBottom sets border color for bottom.
func (bo *border) SetColorBottom(col Color) { bo.colorBottom = col }

// SetSideBorderWidth sets the cell's side border width.
func (tc *TableCell) SetSideBorderWidth(side CellBorderSide, width float64) {
	switch side {
	case CellBorderSideAll:
		tc.borderWidthTop = width
		tc.borderWidthBottom = width
		tc.borderWidthLeft = width
		tc.borderWidthRight = width
	case CellBorderSideTop:
		tc.borderWidthTop = width
	case CellBorderSideBottom:
		tc.borderWidthBottom = width
	case CellBorderSideLeft:
		tc.borderWidthLeft = width
	case CellBorderSideRight:
		tc.borderWidthRight = width
	}
}

// GetSideBorderStyle returns the cell's border style for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (tc *TableCell) GetSideBorderStyle(side CellBorderSide) CellBorderStyle {
	switch side {
	case CellBorderSideBottom:
		return tc.borderStyleBottom
	case CellBorderSideLeft:
		return tc.borderStyleLeft
	case CellBorderSideRight:
		return tc.borderStyleRight
	default:
		return tc.borderStyleTop
	}
}

// NewCellProps returns the default properties of an invoice cell.
func (i *Invoice) NewCellProps() InvoiceCellProps {
	colorRGBFrom8bit := ColorRGBFrom8bit(255, 255, 255)
	return InvoiceCellProps{TextStyle: i.defaultStyle, Alignment: CellHorizontalAlignmentLeft, BackgroundColor: colorRGBFrom8bit, BorderColor: colorRGBFrom8bit, BorderWidth: 1, BorderSides: []CellBorderSide{CellBorderSideAll}}
}
func (lii *listItem) ctxHeight(width float64) float64 {
	var height float64
	switch content := lii.content.(type) {
	case *StyledParagraph:
		if content.enableWrap {
			content.SetWidth(width - content.margins.Horizontal())
		}
		height = content.Height() + content.margins.Vertical()
		height += 0.5 * content.getTextHeight()
	case *List:
		height = content.ctxHeight(width) + content.margins.Vertical()
	case *Image:
		height = content.Height() + content.margins.Vertical()
	case *Division:
		height = content.ctxHeight(width) + content.margins.Vertical() + content.padding.Vertical()
	case *Table:
		content.updateRowHeights(width - content.margins.Horizontal())
		height = content.Height() + content.margins.Vertical()
	default:
		height = lii.content.Height()
	}
	return height
}
func (tp *templateProcessor) parseListMarker(node *templateNode) (interface{}, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "List marker parent cannot be nil.")
		return nil, errInvalidParent
	}
	var marker *TextChunk
	switch value := componentParent.value.(type) {
	case *List:
		marker = &value.marker
	case *listItem:
		marker = &value.marker
	default:
		tp.nodeLogError(node, "%v is not a valid parent node for list marker.", value)
		return nil, errInvalidParent
	}
	if _, err := tp.parseTextChunk(node, marker); err != nil {
		if tp.options.Strict {
			return nil, err
		}
		tp.nodeLogError(node, "Could not parse list marker: `%v`.", err)
		return nil, nil
	}
	return marker, nil
}

// NewMultiCell makes a new cell with given colspan and rowspan and inserts it into the row at the current position.
func (gr *GridRow) NewMultiCell(colspan, rowspan int) (*GridCell, error) {
	cell := &GridCell{col: len(gr.cells), row: gr.index, colSpan: colspan, rowSpan: rowspan}
	cell.row = gr.index
	cell.col = 0
	for _, cell2 := range gr.cells {
		cell.col += cell2.colSpan
	}
	for i, row := range gr.grid.rows {
		if i == gr.index+1 {
			break
		}
		for _, cell2 := range row.cells {
			if cell2.row+cell2.rowSpan > cell.row {
				if cell2.col+cell2.colSpan > cell.col {
					cell.col = cell2.col + cell2.colSpan
				}
			}
		}
	}
	if cell.col >= gr.grid.cols {
		return nil, errors.New("can't add any more cells to this row")
	}
	cell.indent = 5
	cell.borderStyleLeft = CellBorderStyleNone
	cell.borderLineStyle = contentdraw.LineStyleSolid
	cell.horizontalAlignment = CellHorizontalAlignmentLeft
	cell.verticalAlignment = CellVerticalAlignmentTop
	cell.borderWidthLeft = 0
	cell.borderWidthBottom = 0
	cell.borderWidthRight = 0
	cell.borderWidthTop = 0
	borderColorLeft := ColorBlack
	cell.borderColorLeft = borderColorLeft
	cell.borderColorBottom = borderColorLeft
	cell.borderColorRight = borderColorLeft
	cell.borderColorTop = borderColorLeft
	cell.opacity = 1.0
	gr.cells = append(gr.cells, cell)
	cell.structType = model.StructureTypeTableData
	return cell, nil
}
func (tp *templateProcessor) typographyValueErr(s2, s string) error {
	switch s2 {
	case "font-size", "line-height":
		_, err := parseFloat64(s)
		return err
	case "color":
		_, err := tp.resolveColor(s)
		return err
	case "font":
		return tp.validateFontValue(s)
	case "text-align":
		if _, ok := textAlignmentValues[s]; !ok {
			return fmt.Errorf("invalid value %q", s)
		}
	}
	return nil
}

type subtableSplit struct {
	index  int
	table  *Table
	ctx    DrawContext
	height float64
}

func (_caaccfb *templateText) pos() *templateNode {
	return &templateNode{line: _caaccfb.line, col: _caaccfb.col, offset: _caaccfb.offset}
}

// Horizontal returns total horizontal (left + right) margin.
func (m *Margins) Horizontal() float64 { return m.Left + m.Right }

// SetLinePageStyle sets the style for the page part of all new lines
// of the table of contents.
func (to *TOC) SetLinePageStyle(style TextStyle) { to.linePageStyle = style }
func uniqueFontName(s string, obj core.PdfObject, resources *model.PdfPageResources) core.PdfObjectName {
	s2 := strings.TrimRightFunc(strings.TrimSpace(s), func(r rune) bool { return unicode.IsNumber(r) })
	if s2 == "" {
		s2 = "Font"
	}
	n := 0
	keyName := core.PdfObjectName(s)
	for {
		font, ok := resources.GetFontByName(keyName)
		if !ok || font == obj {
			break
		}
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("%s%d", s2, n))
	}
	return keyName
}

// OptimizeAlphaMask converts a binary alpha channel — one where every pixel
// is either fully opaque or fully transparent — into a 1-bit stencil mask
// (see SetStencilMask), which stores one bit per alpha sample instead of the
// full-depth SMask the alpha channel would otherwise produce. It reports
// whether the conversion applies: an image without an alpha channel, with
// partial transparency anywhere, with an alpha depth other than 8 or 16 bits
// per sample, or with a mask already set is left unchanged.
//
// The stencil is derived from the image's alpha channel when the image is
// drawn, so later operations that change the pixels and alpha together (such
// as Crop) stay consistent with the mask. The underlying model.Image keeps
// its alpha channel; when no encoder is set with SetEncoder, the image is
// flate encoded, matching the encoding an alpha-carrying image receives.
func (im *Image) OptimizeAlphaMask() bool {
	if im.mask != nil {
		return false
	}
	if _, ok := alphaToBitmask(im.img); !ok {
		return false
	}
	return im.setMask(&imageMask{kind: imageMaskOptimizedAlpha}) == nil
}

type GridRowSection int

func (pt *pageTransformations) transformPage(page *model.PdfPage) error {
	if err := pt.applyFlip(page); err != nil {
		return err
	}
	return nil
}

// RenderTemplate draws the parsed template into the block using the specified
// creator and `data`. The handle comes from ParseTemplate and can be rendered
// repeatedly with different data. The `data` parameter can be nil.
func (b *Block) RenderTemplate(c *Creator, pt *ParsedTemplate, data interface{}) error {
	return pt.renderInto(c, b, data)
}

// NewImageFromData creates an Image from image data.
func (c *Creator) NewImageFromData(data []byte) (*Image, error) { return newImageFromBytes(data) }

// ParseFromSVGString creates a GraphicSVG instance from string SVG.
func ParseFromSVGString(svgStr string) (*GraphicSVGElement, error) {
	return ParseFromSVGStream(strings.NewReader(svgStr))
}

// ScaleToHeight scale Image to a specified height h, maintaining the aspect ratio.
func (im *Image) ScaleToHeight(h float64) {
	width := im.width / im.height
	im.height = h
	im.width = h * width
}
func toPdfColor(color Color) model.PdfColor {
	if color == nil {
		color = ColorBlack
	}
	switch grayColor2 := color.(type) {
	case grayColor:
		return model.NewPdfColorDeviceGray(grayColor2.gray)
	case cmykColor:
		return model.NewPdfColorDeviceCMYK(grayColor2.c, grayColor2.m, grayColor2.y, grayColor2.k)
	case *FunctionBasedShading:
		return model.NewPdfColorPatternType1()
	case *LinearShading:
		return model.NewPdfColorPatternType2()
	case *RadialShading:
		return model.NewPdfColorPatternType3()
	}
	return model.NewPdfColorDeviceRGB(color.ToRGB())
}
func (gse *GraphicSVGElement) drawPath(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	applySVGTransform(gse, cc)
	svgPath, err := parseSVGPath(gse.Attributes["d"])
	if err != nil {
		common.Log.Error("ERROR: %s", err.Error())
	}
	var (
		startX, startY = 0.0, 0.0
		param, y       = 0.0, 0.0
		command2       *Command
	)
	for _, subpath := range svgPath.Subpaths {
		for _, command := range subpath.Commands {
			switch strings.ToLower(command.Symbol) {
			case "m":
				param, y = command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, y = startX+param-gse.ViewBox.X, startY+y-gse.ViewBox.Y
				}
				cc.Add_m(precision.RoundFloat(param, 3), precision.RoundFloat(y, 3))
				startX, startY = param, y
			case "c":
				param, value, param2, param3, param4, param5 := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale, command.Params[4]*gse.scale, command.Params[5]*gse.scale
				if !command.isAbsolute() {
					param, value, param2, param3, param4, param5 = startX+param, startY+value, startX+param2, startY+param3, startX+param4, startY+param5
				}
				cc.Add_c(precision.RoundFloat(param, 3), precision.RoundFloat(value, 3), precision.RoundFloat(param2, 3), precision.RoundFloat(param3, 3), precision.RoundFloat(param4, 3), precision.RoundFloat(param5, 3))
				startX, startY = param4, param5
			case "s":
				param, value, param2, param3 := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale
				if !command.isAbsolute() {
					param, value, param2, param3 = startX+param, startY+value, startX+param2, startY+param3
				}
				cc.Add_c(precision.RoundFloat(startX, 3), precision.RoundFloat(startY, 3), precision.RoundFloat(param, 3), precision.RoundFloat(value, 3), precision.RoundFloat(param2, 3), precision.RoundFloat(param3, 3))
				startX, startY = param2, param3
			case "l":
				param, value := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, value = startX+param, startY+value
				}
				cc.Add_l(precision.RoundFloat(param, 3), precision.RoundFloat(value, 3))
				startX, startY = param, value
			case "h":
				param := command.Params[0] * gse.scale
				if !command.isAbsolute() {
					param = startX + param
				}
				cc.Add_l(precision.RoundFloat(param, 3), precision.RoundFloat(startY, 3))
				startX = param
			case "v":
				param := command.Params[0] * gse.scale
				if !command.isAbsolute() {
					param = startY + param
				}
				cc.Add_l(precision.RoundFloat(startX, 3), precision.RoundFloat(param, 3))
				startY = param
			case "q":
				param, y1, x, y := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale
				if !command.isAbsolute() {
					param, y1, x, y = startX+param, startY+y1, startX+x, startY+y
				}
				quadraticToCubicBezier, point := graphic2d.QuadraticToCubicBezier(startX, startY, param, y1, x, y)
				cc.Add_c(precision.RoundFloat(quadraticToCubicBezier.X, 3), precision.RoundFloat(quadraticToCubicBezier.Y, 3), precision.RoundFloat(point.X, 3), precision.RoundFloat(point.Y, 3), precision.RoundFloat(x, 3), precision.RoundFloat(y, 3))
				startX, startY = x, y
			case "t":
				var point3, point4 graphic2d.Point
				param, y := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, y = startX+param, startY+y
				}
				if command2 != nil && strings.ToLower(command2.Symbol) == "q" {
					point := graphic2d.Point{X: command2.Params[0] * gse.scale, Y: command2.Params[1] * gse.scale}
					point2 := graphic2d.Point{X: command2.Params[2] * gse.scale, Y: command2.Params[3] * gse.scale}
					sub := point2.Mul(2.0).Sub(point)
					point3, point4 = graphic2d.QuadraticToCubicBezier(startX, startY, sub.X, sub.Y, param, y)
				}
				cc.Add_c(precision.RoundFloat(point3.X, 3), precision.RoundFloat(point3.Y, 3), precision.RoundFloat(point4.X, 3), precision.RoundFloat(point4.Y, 3), precision.RoundFloat(param, 3), precision.RoundFloat(y, 3))
				startX, startY = param, y
			case "a":
				param, ry := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				rot := command.Params[2]
				large := command.Params[3] > 0
				sweep := command.Params[4] > 0
				endX, endY := command.Params[5]*gse.scale, command.Params[6]*gse.scale
				if !command.isAbsolute() {
					endX, endY = startX+endX, startY+endY
				}
				ellipseToCubicBeziers := graphic2d.EllipseToCubicBeziers(startX, startY, param, ry, rot, large, sweep, endX, endY)
				for _, ellipseToCubicBezier := range ellipseToCubicBeziers {
					cc.Add_c(precision.RoundFloat(ellipseToCubicBezier[1].X, 3), precision.RoundFloat((ellipseToCubicBezier[1].Y), 3), precision.RoundFloat((ellipseToCubicBezier[2].X), 3), precision.RoundFloat((ellipseToCubicBezier[2].Y), 3), precision.RoundFloat((ellipseToCubicBezier[3].X), 3), precision.RoundFloat((ellipseToCubicBezier[3].Y), 3))
				}
				startX, startY = endX, endY
			case "z":
				cc.Add_h()
			}
			command2 = command
		}
	}
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

// Drawable is a widget that can be used to draw with the Creator.
type Drawable interface {

	// GeneratePageBlocks draw onto blocks representing Page contents. As the content can wrap over many pages, multiple
	// templates are returned, one per Page.  The function also takes a draw context containing information
	// where to draw (if relative positioning) and the available height to draw on accounting for Margins etc.
	GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error)

	// SetMarkedContentID sets the marked content id for the drawable.
	SetMarkedContentID(mcid int64)

	// SetStructureType sets the structure type for the drawable.
	SetStructureType(structType model.StructureType)

	// GenerateKDict generates a K dictionary for the drawable.
	GenerateKDict() (*model.KDict, error)

	// SetStructPageNumber sets the page number where the structure element for this drawable is located.
	SetStructPageNumber(pageNum *int64)
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F600 && r <= 0x1F64F:
		return true
	case r >= 0x1F300 && r <= 0x1F5FF:
		return true
	case r >= 0x1F680 && r <= 0x1F6FF:
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF:
		return true
	case r >= 0x1F900 && r <= 0x1F9FF:
		return true
	case r >= 0x1FA70 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x26FF:
		return true
	case r >= 0x2700 && r <= 0x27BF:
		return true
	default:
		return false
	}
}

// SetBorderRadius sets the radius of the rectangle corners.
func (r *Rectangle) SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight float64) {
	r.borderRadiusTopLeft = topLeft
	r.borderRadiusTopRight = topRight
	r.borderRadiusBottomLeft = bottomLeft
	r.borderRadiusBottomRight = bottomRight
}

// GetVerticalAlignment returns the cell's vertical alignment of content.
func (gc *GridCell) GetVerticalAlignment() CellVerticalAlignment { return gc.verticalAlignment }

type graphemeCluster struct {
	runes []rune
	text  string
}

// AddColorStop add color stop information for rendering gradient.
func (s *shading) AddColorStop(color Color, point float64) {
	s.colorPoints = append(s.colorPoints, newColorPoint(color, point))
}

// SetPadding sets the padding of the component. The padding represents
// inner margins which are applied around the contents of the division.
// The background of the component is not affected by its padding.
func (d *Division) SetPadding(left, right, top, bottom float64) {
	d.padding.Left = left
	d.padding.Right = right
	d.padding.Top = top
	d.padding.Bottom = bottom
}
func (g *Grid) updateRowHeights(totalWidth float64) {
	for _, row := range g.rows {
		row.updateRowHeight(totalWidth)
	}
}
func parseSVGStyle(m map[string]string, value2 float64) (*GraphicSVGStyle, error) {
	graphicSVGStyle := newGraphicSVGStyle()
	fillColor, ok := m["fill"]
	if ok {
		graphicSVGStyle.FillColor = fillColor
		if fillColor == "none" {
			graphicSVGStyle.FillColor = ""
		}
	}
	s, ok2 := m["fill-opacity"]
	if ok2 {
		percentOrFloat, err := parsePercentOrFloat(s)
		if err != nil {
			return nil, err
		}
		graphicSVGStyle.FillOpacity = percentOrFloat
	}
	strokeColor, ok3 := m["stroke"]
	if ok3 {
		graphicSVGStyle.StrokeColor = strokeColor
		if strokeColor == "none" {
			graphicSVGStyle.StrokeColor = ""
		}
	}
	value, ok4 := m["stroke-width"]
	if ok4 {
		svgLength, err := parseSVGLength(value, 64)
		if err != nil {
			return nil, err
		}
		graphicSVGStyle.StrokeWidth = svgLength * value2
	}
	return graphicSVGStyle, nil
}
func newFilledCurve() *FilledCurve {
	filledCurve := FilledCurve{}
	filledCurve.curves = []contentdraw.CubicBezierCurve{}
	return &filledCurve
}
func (d *Division) ctxHeight(right float64) float64 {
	right -= d.margins.Left + d.margins.Right + d.padding.Left + d.padding.Right
	var ctxHeight float64
	for _, component := range d.components {
		ctxHeight += drawableHeight(component, right)
	}
	return ctxHeight
}
func newEllipse(xc, yc, width, height float64) *Ellipse {
	return &Ellipse{xc: xc, yc: yc, width: width, height: height, positioning: PositionAbsolute, fillOpacity: 1.0, borderColor: ColorBlack, borderWidth: 1.0, borderOpacity: 1.0}
}

// GetPos returns the absolute coordinates (x, y) of the paragraph. The
// coordinates are only meaningful when the paragraph uses absolute
// positioning (see GetPositioning).
func (sp *StyledParagraph) GetPos() (float64, float64) { return sp.xPos, sp.yPos }
func (pa *Path) compare(path *Path) bool {
	if len(pa.Subpaths) != len(path.Subpaths) {
		return false
	}
	for i, subpath := range pa.Subpaths {
		if !subpath.compare(path.Subpaths[i]) {
			return false
		}
	}
	return true
}
func (tp *templateProcessor) processGradientColorPair(strs []string) (colors []Color, positions []float64, err error) {
	var flags []bool
	for _, s := range strs {
		fields := strings.Fields(s)
		if len(fields) == 0 {
			setFirstError(&err, errors.New("empty gradient color stop"))
			continue
		}
		if len(fields) > 2 {
			setFirstError(&err, fmt.Errorf("gradient stop %q has extra values", strings.TrimSpace(s)))
		}
		finiteFloat2, ok := 0.0, false
		if len(fields) > 1 {
			field := fields[1]
			if field == "0" {
				ok = true
			} else if !strings.HasSuffix(field, "%") {
				setFirstError(&err, fmt.Errorf("gradient stop position %q must be a percentage", field))
			} else if finiteFloat, err2 := parseFiniteFloat(field[:len(field)-1]); err2 != nil {
				common.Log.Debug("Failed parsing point value: %v", err2)
				setFirstError(&err, fmt.Errorf("invalid gradient stop position %q", field))
			} else {
				finiteFloat2, ok = finiteFloat/100.0, true
			}
		}
		color := tp.parseColor(fields[0])
		if color == nil {
			setFirstError(&err, fmt.Errorf("unresolved gradient stop color %q", fields[0]))
			continue
		}
		colors = append(colors, color)
		positions = append(positions, finiteFloat2)
		flags = append(flags, ok)
	}
	colorsLen := len(colors)
	if colorsLen == 0 {
		common.Log.Debug("Invalid gradient color definition!")
		setFirstError(&err, errors.New("gradient has no color stops"))
		return nil, nil, err
	}
	if colorsLen == 1 {
		setFirstError(&err, errors.New("gradient needs at least two color stops"))
	}
	if !flags[0] {
		positions[0], flags[0] = 0, true
	}
	if !flags[colorsLen-1] {
		positions[colorsLen-1], flags[colorsLen-1] = 1, true
	}
	position2 := 0.0
	for i, position := range positions {
		if !flags[i] {
			continue
		}
		if position < 0 || position > 1 {
			setFirstError(&err, fmt.Errorf("gradient stop position %g%% is outside 0-100%%", position*100))
			position = math.Max(0, math.Min(1, position))
		}
		if position < position2 {
			setFirstError(&err, fmt.Errorf("gradient stop position %g%% is less than an earlier stop", position*100))
			position = position2
		}
		positions[i], position2 = position, position
	}
	n := 0
	for i := 1; i < colorsLen; i++ {
		if !flags[i] {
			continue
		}
		for j := n + 1; j < i; j++ {
			positions[j] = positions[n] + float64(j-n)*(positions[i]-positions[n])/float64(i-n)
		}
		n = i
	}
	return colors, positions, err
}

// Width returns the width of the graphic svg.
func (gs *GraphicSVG) Width() float64 { return gs.root.Width }
func parseNumberList(s string) (numbers []float64, err error) {
	var value float64
	n := 0
	ok := true
	for i, r := range s {
		if r == '.' {
			if ok {
				ok = false
				continue
			}
			value, err = parseSVGLength(s[n:i], 64)
			if err != nil {
				return
			}
			numbers = append(numbers, value)
			n = i
		}
	}
	value, err = parseSVGLength(s[n:], 64)
	if err != nil {
		return
	}
	numbers = append(numbers, value)
	return
}
func (t *Table) sortCells() {
	sort.Slice(t.cells, func(i, j int) bool {
		row := t.cells[i].row
		row2 := t.cells[j].row
		if row < row2 {
			return true
		}
		if row > row2 {
			return false
		}
		return t.cells[i].col < t.cells[j].col
	})
}

// Lines returns all the rows of the invoice line items table.
func (i *Invoice) Lines() [][]*InvoiceCell { return i.lines }

// Block contains a portion of PDF Page contents. It has a width and a position and can
// be placed anywhere on a Page.  It can even contain a whole Page, and is used in the creator
// where each Drawable object can output one or more blocks, each representing content for separate pages
// (typically needed when Page breaks occur).
type Block struct {
	contents    *contentstream.ContentStreamOperations
	resources   *model.PdfPageResources
	positioning Positioning
	xPos, yPos  float64
	width       float64
	height      float64
	angle       float64
	margins     Margins
	annotations []*model.PdfAnnotation
	artifact    *model.Artifact
	background  *Background
}

func parseEllipseTag(proc *templateProcessor, node *templateNode) (interface{}, error) {
	return proc.parseEllipse(node)
}
func (i *Invoice) newCell(value string, invoiceCellProps InvoiceCellProps) *InvoiceCell {
	return &InvoiceCell{invoiceCellProps, value}
}
func (im *Image) applyMask(xObjectImage *model.XObjectImage, img2 *model.Image) error {
	switch im.mask.kind {
	case imageMaskSoft:
		grayMaskXObject, err := makeGrayMaskXObject(im.mask.img)
		if err != nil {
			return err
		}
		xObjectImage.SMask = grayMaskXObject.ToPdfObject()
	case imageMaskStencil, imageMaskOptimizedAlpha:
		img := img2
		var data []uint8
		if im.mask.kind == imageMaskStencil {
			img = im.mask.img
			if im.mask.threshold != nil {
				data = []uint8{*im.mask.threshold}
			}
		}
		xObjectImageStencilMask, err := model.NewXObjectImageStencilMask(img, nil, data...)
		if err != nil {
			return err
		}
		xObjectImage.Mask = xObjectImageStencilMask.ToPdfObject()
	case imageMaskColorKey:
		if err := validateColorKeyMask(im.mask.colorKey, xObjectImage); err != nil {
			return err
		}
		xObjectImage.Mask = core.MakeArrayFromIntegers(im.mask.colorKey)
	}
	return nil
}

// VectorDrawable is a Drawable with a specified width and height.
type VectorDrawable interface {
	Drawable

	// Width returns the width of the Drawable.
	Width() float64

	// Height returns the height of the Drawable.
	Height() float64
}

const (
	TextRenderingModeFill TextRenderingMode = iota
	TextRenderingModeStroke
	TextRenderingModeFillStroke
	TextRenderingModeInvisible
	TextRenderingModeFillClip
	TextRenderingModeStrokeClip
	TextRenderingModeFillStrokeClip
	TextRenderingModeClip
)
const (
	DefaultHorizontalScaling = 100
)

// TemplateOptions contains options and resources to use when rendering
// a template with a Creator instance.
// All the resources in the map fields can be referenced by their
// name/key in the template which is rendered using the options instance.
type TemplateOptions struct {

	// HelperFuncMap is used to define functions which can be accessed
	// inside the rendered templates by their assigned names.
	HelperFuncMap template.FuncMap

	// SubtemplateMap contains templates which can be rendered alongside
	// the main template. They can be accessed using their assigned names
	// in the main template or in the other subtemplates.
	// Subtemplates defined inside the subtemplates specified in the map
	// can be accessed directly.
	// All resources available to the main template are also available
	// to the subtemplates.
	SubtemplateMap map[string]io.Reader

	// FontMap contains pre-loaded fonts which can be accessed
	// inside the rendered templates by their assigned names.
	// The assigned names must not contain commas, as the font
	// attribute is interpreted as a comma-separated fallback list.
	FontMap map[string]*model.PdfFont

	// ImageMap contains pre-loaded images which can be accessed
	// inside the rendered templates by their assigned names.
	ImageMap map[string]*model.Image

	// ColorMap contains colors which can be accessed
	// inside the rendered templates by their assigned names.
	ColorMap map[string]Color

	// ChartMap contains charts which can be accessed
	// inside the rendered templates by their assigned names.
	ChartMap map[string]chartrender.ChartRenderable

	// DisableAutoEscape turns off automatic XML-escaping of template action
	// output.
	//
	// By default, the output of every template action is XML-escaped so that
	// values coming from the template data cannot break the XML stream (e.g. a
	// bare `&`) or inject their own tags. The `raw` helper opts a single action
	// out of escaping; it is honored only as the final command of the action's
	// pipeline (optionally wrapped in parentheses), so `{{raw .v}}` and
	// `{{(raw .v)}}` are unescaped, while `{{raw .v | printf "%s"}}` and
	// `{{printf "[%s]" (raw .v)}}` are still escaped.
	//
	// Set this to true to restore the previous behavior, where action output is
	// written to the XML stream verbatim. This is an upgrade escape hatch for
	// templates whose data supplies pre-escaped XML (for example the "&#xA;"
	// newline idiom) or intentionally emits markup. When disabled, `raw` is not
	// reserved and a HelperFuncMap entry of that name is used as provided.
	DisableAutoEscape bool

	// Strict makes the processor return an error, located at the template file
	// and line:column, on an attribute parse failure, unknown attribute or tag,
	// invalid nesting, invalid table structure, inapplicable attribute or
	// unresolved image or chart resource. Execution also fails when an action
	// references a key missing from map data, including in `{{if .Key}}` and
	// `{{with .Key}}`; read optional keys with `{{with index . "Key"}}`.
	//
	// When false (the default), these problems are logged and skipped, a bad
	// attribute value falls back to its default, and a missing key renders as
	// empty text. With DisableAutoEscape it writes text/template's `<no value>`
	// into the XML, which fails to parse.
	Strict bool

	// FileName sets the template name used in error locations. Use it when the
	// template is read from a buffer or embedded filesystem, which carry no
	// file name of their own.
	FileName string
}

const (
	pxToPt             = 0.72
	cmToPt             = 28.3464
	mmToPt             = cmToPt / 10
	bezierCircleKappa  = 0.551784
	svgDPI             = 96
	defaultSVGFontSize = 16.0
)

// TextAlignment options for paragraph.
type TextAlignment int

// SetFontColor sets the font color for the paragraph.
func (sp *StyledParagraph) SetFontColor(color Color) {
	sp.defaultStyle.Color = color
	for _, chunk := range sp.chunks {
		chunk.Style.Color = color
	}
}
func callSetter(value reflect.Value, i interface{}, values2 ...reflect.Value) []reflect.Value {
	if i == nil {
		return nil
	}
	typ := value.Type()
	values := make([]reflect.Value, 0, len(values2)+1)
	values = append(values, argOrZero(reflect.ValueOf(i), typ.In(0)))
	for i, argOrZero2 := range values2 {
		values = append(values, argOrZero(argOrZero2, typ.In(i+1)))
	}
	return value.Call(values)
}
func mergeColorspaces(resources, resources2 *model.PdfPageResources) error {
	colorspaces, _ := resources.GetColorspaces()
	if colorspaces != nil && len(colorspaces.Colorspaces) > 0 {
		for colorspaceKey, colorspace := range colorspaces.Colorspaces {
			name := *core.MakeName(colorspaceKey)
			if resources2.HasColorspaceByName(name) {
				continue
			}
			err := resources2.SetColorspaceByName(name, colorspace)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ColorPoint is a pair of Color and a relative point where the color
// would be rendered.
type ColorPoint struct {
	color Color
	point float64
}

// Width is not used. The list component is designed to fill into the available
// width depending on the context. Returns 0.
func (li *List) Width() float64 { return 0 }

// SetHeight sets the custom height for the row.
func (gr *GridRow) SetHeight(h float64) { gr.height = h }

// SetOpacity sets opacity for border in range 0-1.
func (bo *border) SetOpacity(opacity float64) { bo.opacity = opacity }

// GridRow defines a row which can contain cells.
type GridRow struct {
	cells         []*GridCell
	height        float64
	contentHeight float64
	index         int
	grid          *Grid
	tagInfo       *model.StructureTagInfo
	section       GridRowSection
}

func (li *List) markerWidth() float64 {
	var markerWidth float64
	for _, item := range li.items {
		para := StyledParagraph{chunks: []*TextChunk{&item.marker}}
		markerWidth2 := para.getTextWidth() / 1000.0
		if markerWidth < markerWidth2 {
			markerWidth = markerWidth2
		}
	}
	return markerWidth
}

// Context returns the current drawing context.
func (c *Creator) Context() DrawContext { return c.context }

// SetHorizontalAlignment sets the cell's horizontal alignment of content.
// Can be one of:
// - CellHorizontalAlignmentLeft
// - CellHorizontalAlignmentCenter
// - CellHorizontalAlignmentRight
func (tc *TableCell) SetHorizontalAlignment(halign CellHorizontalAlignment) {
	tc.horizontalAlignment = halign
}
