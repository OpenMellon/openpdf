// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"reflect"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/model"
)

var textChunkAttrs = map[string]attrApplyFunc{"color": colorAttr(func(chunk *TextChunk, color Color) { chunk.Style.Color = color }), "outline-color": colorAttr(func(chunk *TextChunk, outlineColor Color) { chunk.Style.OutlineColor = outlineColor }), "font": fontAttr(func(chunk *TextChunk, font *model.PdfFont) { chunk.Style.Font = font }), "font-size": floatAttr(func(chunk *TextChunk, fontSize float64) { chunk.Style.FontSize = fontSize }), "line-height": floatAttr(func(chunk *TextChunk, lineHeight float64) { chunk.Style.LineHeight = lineHeight }), "outline-size": floatAttr(func(chunk *TextChunk, outlineSize float64) { chunk.Style.OutlineSize = outlineSize }), "character-spacing": floatAttr(func(chunk *TextChunk, charSpacing float64) { chunk.Style.CharSpacing = charSpacing }), "horizontal-scaling": floatAttr(func(chunk *TextChunk, horizontalScaling float64) { chunk.Style.HorizontalScaling = horizontalScaling }), "rendering-mode": enumAttr(textRenderingModeValues, func(chunk *TextChunk, renderingMode TextRenderingMode) { chunk.Style.RenderingMode = renderingMode }), "underline": boolAttr(func(chunk *TextChunk, underline bool) { chunk.Style.Underline = underline }), "underline-color": colorAttr(func(chunk *TextChunk, color Color) { chunk.Style.UnderlineStyle.Color = color }), "underline-offset": floatAttr(func(chunk *TextChunk, offset float64) { chunk.Style.UnderlineStyle.Offset = offset }), "underline-thickness": floatAttr(func(chunk *TextChunk, thickness float64) { chunk.Style.UnderlineStyle.Thickness = thickness }), "text-rise": floatAttr(func(chunk *TextChunk, textRise float64) { chunk.Style.TextRise = textRise }), "link": nil}

func marginAttr(i any) attrApplyFunc {
	value2 := validateSetter("marginAttr", i, 0, marginsType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
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
	var renderer any
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
