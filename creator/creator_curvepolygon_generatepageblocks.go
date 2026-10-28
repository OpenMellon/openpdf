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
	"strings"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
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

func (tp *templateProcessor) nodeLogDebug(node *templateNode, format string, a ...any) {
	common.Log.Debug(tp.getNodeErrorLocation(node, format, a...))
}

func (tp *templateProcessor) nodeLogError(node *templateNode, format string, a ...any) {
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
