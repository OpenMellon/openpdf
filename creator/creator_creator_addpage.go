// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/ps"
	"golang.org/x/net/html/charset"
)

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

func templateAdd(v, v2 any) (any, error) {
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
