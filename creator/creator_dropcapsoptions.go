// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"bytes"
	"encoding/xml"
	"image"
	"io"
	"strconv"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

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

func (tp *templateProcessor) parseChapterHeading(node *templateNode) (any, error) {
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
func (line *TOCLine) Level() uint { return line.level }

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
func (tp *templateProcessor) parseDivision(node *templateNode) (any, error) {
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
