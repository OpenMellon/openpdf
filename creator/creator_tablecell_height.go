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
	"reflect"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

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

func enumAttr(i any, i2 any) attrApplyFunc {
	valueOf := reflect.ValueOf(i)
	if !valueOf.IsValid() || valueOf.Kind() != reflect.Map || valueOf.Type().Key() != stringType {
		panic(fmt.Sprintf("creator: enumAttr requires a map[string]E value map, got %T", i))
	}
	elem := valueOf.Type().Elem()
	value2 := validateSetter("enumAttr", i2, 0, elem)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
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
