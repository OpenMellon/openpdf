// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"errors"
	"image"
	"reflect"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

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
			if before, ok0 := strings.CutSuffix(s, "%"); ok0 {
				float, err := strconv.ParseFloat(before, 64)
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
			if before, ok0 := strings.CutSuffix(s, "%"); ok0 {
				float, err := strconv.ParseFloat(before, 64)
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
	int64Type         = reflect.TypeFor[int64]()
	float64Type       = reflect.TypeFor[float64]()
	int64SliceType    = reflect.TypeFor[[]int64]()
	float64SliceType  = reflect.TypeFor[[]float64]()
	stringType        = reflect.TypeFor[string]()
	marginsType       = reflect.TypeFor[Margins]()
	errorIfaceType    = reflect.TypeFor[error]()
	colorType         = reflect.TypeFor[Color]()
	streamEncoderType = reflect.TypeFor[core.StreamEncoder]()
	pdfFontPtrType    = reflect.TypeFor[*model.PdfFont]()
)

// CellHorizontalAlignment defines the table cell's horizontal alignment.
type (
	CellHorizontalAlignment int
	imageMask               struct {
		kind      imageMaskKind
		img       *model.Image
		threshold *uint8
		colorKey  []int
	}
)

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

func (tp *templateProcessor) parseLine(node *templateNode) (any, error) {
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
