// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func applySVGTransform(graphicSVGElement *GraphicSVGElement, cc *contentstream.ContentCreator) {
	attribute, ok := graphicSVGElement.Attributes["transform"]
	if ok {
		fields := strings.FieldsSeq(attribute)
		for field := range fields {
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
	value       any
	start       xml.StartElement
	parent      *templateNode
	children    []*templateNode
	childValues []any
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

func isTransformSeparator(r rune) bool { return r == '(' || r == ',' || r == ')' }

// IsAbsolute checks if the positioning is absolute.
func (pos Positioning) IsAbsolute() bool { return pos == PositionAbsolute }

// DrawTemplate renders the template provided through the specified reader,
// using the specified `data` and `options`.
// Creator templates are first executed as text/template *Template instances,
// so the specified `data` is inserted within the template.
// The second phase of processing is actually parsing the template, translating
// it into creator components and rendering them using the provided options.
// Both the `data` and `options` parameters can be nil.
func (b *Block) DrawTemplate(c *Creator, r io.Reader, data any, options *TemplateOptions) error {
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

func (tp *templateProcessor) parseImage(node *templateNode) (any, error) {
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
