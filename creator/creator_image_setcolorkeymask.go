// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

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
	parse                 func(*templateProcessor, *templateNode) (any, error)
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

func parseListMarkerTag(proc *templateProcessor, node *templateNode) (any, error) {
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
