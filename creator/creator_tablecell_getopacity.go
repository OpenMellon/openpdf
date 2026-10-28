// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"reflect"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

// GetOpacity returns the cell's opacity in the range 0-1.
func (tc *TableCell) GetOpacity() float64 { return tc.opacity }

func colorAttr(i any) attrApplyFunc {
	value2 := validateSetter("colorAttr", i, 0, colorType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
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

func floatArrayAttrErr(i any) attrApplyFunc {
	value2 := validateSetter("floatArrayAttrErr", i, 1, float64SliceType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
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

func parseStyledParagraphTag(proc *templateProcessor, node *templateNode) (any, error) {
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
