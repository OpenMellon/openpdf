// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"reflect"
	"text/template"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

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

func drawTemplate(creator *Creator, r io.Reader, data any, options *TemplateOptions, renderer componentRenderer) error {
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

func parseChapterHeadingTag(proc *templateProcessor, node *templateNode) (any, error) {
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
