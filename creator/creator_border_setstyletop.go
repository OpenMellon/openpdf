// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
	chartrender "github.com/OpenMellon/unichart/render"
)

// SetStyleTop sets border style for top side.
func (bo *border) SetStyleTop(style CellBorderStyle) { bo.styleTop = style }

func tokenizePathData(s2 string) []token {
	var (
		tokens []token
		s      string
	)
	for _, r := range s2 {
		text := string(r)
		switch {
		case svgCommands.isCommand(text):
			tokens, s = flushToken(tokens, s)
			tokens = append(tokens, token{text, true})
		case text == ".":
			if s == "" {
				s = "0"
			}
			if strings.Contains(s, text) {
				tokens = append(tokens, token{s, false})
				s = "0"
			}
			fallthrough
		case text >= "0" && text <= "9" || text == "e":
			s += text
		case text == "-":
			if strings.HasSuffix(s, "e") {
				s += text
			} else {
				tokens, _ = flushToken(tokens, s)
				s = text
			}
		default:
			tokens, s = flushToken(tokens, s)
		}
	}
	tokens, _ = flushToken(tokens, s)
	return tokens
}

// SetForms adds an Acroform to a PDF file.  Sets the specified form for writing.
func (c *Creator) SetForms(form *model.PdfAcroForm) error { c.acroForm = form; return nil }

// TOC returns the table of contents component of the creator.
func (c *Creator) TOC() *TOC { return c.toc }

// Insert adds a new text chunk at the specified position in the paragraph.
func (sp *StyledParagraph) Insert(index uint, text string) *TextChunk {
	chunksLen := uint(len(sp.chunks))
	if index > chunksLen {
		index = chunksLen
	}
	textChunk := NewTextChunk(text, sp.defaultStyle)
	sp.chunks = append(sp.chunks[:index], append([]*TextChunk{textChunk}, sp.chunks[index:]...)...)
	sp.wrapText()
	return textChunk
}

// GetHorizontalAlignment returns the cell's horizontal alignment of content.
func (gc *GridCell) GetHorizontalAlignment() CellHorizontalAlignment { return gc.horizontalAlignment }

type chapterRecordFunc func(chapter *Chapter, page int)

func (ch *Chapter) drawBackground(background []*Block, ctx2, ctx3 DrawContext, m map[int]float64, m2 map[int]bool) ([]*Block, error) {
	backgroundLen := len(background)
	if backgroundLen == 0 || ch.background == nil {
		return background, nil
	}
	n, n2 := -1, -1
	for i, block := range background {
		if !block.isEmpty() {
			if n == -1 {
				n = i
			}
			n2 = i
		}
	}
	blocks := make([]*Block, 0, backgroundLen)
	ok2 := false
	for i, block := range background {
		if block.isEmpty() {
			if m2[i] {
				ok2 = true
			}
			blocks = append(blocks, block)
			continue
		}
		var (
			borderRadiusTopLeft     = ch.background.BorderRadiusTopLeft
			borderRadiusTopRight    = ch.background.BorderRadiusTopRight
			borderRadiusBottomLeft  = ch.background.BorderRadiusBottomLeft
			borderRadiusBottomRight = ch.background.BorderRadiusBottomRight
		)
		ctx := ctx2
		ctx.Page += i
		value, ok := m[i]
		ok3 := m2[i] || ok2 || i == n
		ok2 = false
		y := ctx2.Y
		if i != 0 {
			ctx.X = ctx.Margins.Left
			ctx.Y = ctx.Margins.Top
			ctx.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right
			y = ctx.Margins.Top
		}
		switch {
		case ok:
			ctx.Height = value - y
		case i == n2:
			ctx.Height = ctx3.Y - y
		default:
			ctx.Height = ctx.PageHeight - ctx.Margins.Bottom - y
		}
		if ctx.Height <= 0 {
			blocks = append(blocks, block)
			continue
		}
		if !ok3 {
			borderRadiusTopLeft = 0
			borderRadiusTopRight = 0
		}
		if !(i == n2 || ok) {
			borderRadiusBottomLeft = 0
			borderRadiusBottomRight = 0
		}
		rectangle := newRectangle(ctx.X, ctx.Y, ctx.Width, ctx.Height)
		rectangle.SetFillColor(ch.background.FillColor)
		rectangle.SetBorderColor(ch.background.BorderColor)
		rectangle.SetBorderWidth(ch.background.BorderSize)
		rectangle.SetBorderRadius(borderRadiusTopLeft, borderRadiusTopRight, borderRadiusBottomLeft, borderRadiusBottomRight)
		pageBlocks, _, err := rectangle.GeneratePageBlocks(ctx)
		if err != nil {
			return nil, err
		}
		if len(pageBlocks) == 0 {
			blocks = append(blocks, block)
			continue
		}
		pageBlock := pageBlocks[0]
		contentStreamOperations := &contentstream.ContentStreamOperations{}
		if err = mergeContentStreams(contentStreamOperations, block.resources, pageBlock.contents, pageBlock.resources); err != nil {
			return nil, err
		}
		*block.contents = append(*contentStreamOperations, *block.contents...)
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// SetBorderColor sets the border color of the ellipse.
func (e *Ellipse) SetBorderColor(col Color) { e.borderColor = col }

func (t *Table) topHeaderRows() int {
	if t.hasHeaderRows && t.headerStartRow == 1 {
		return t.headerEndRow
	}
	return 0
}

// TOCLine represents a line in a table of contents.
// The component can be used both in the context of a
// table of contents component and as a standalone component.
// The representation of a table of contents line is as follows:
/*
         [number] [title]      [separator] [page]
   e.g.: Chapter1 Introduction ........... 1
*/
type TOCLine struct {
	paragraph *StyledParagraph

	// Holds the text and style of the number part of the TOC line.
	Number TextChunk

	// Holds the text and style of the title part of the TOC line.
	Title TextChunk

	// Holds the text and style of the separator part of the TOC line.
	Separator TextChunk

	// Holds the text and style of the page part of the TOC line.
	Page        TextChunk
	left        float64
	level       uint
	levelOffset float64
	positioning Positioning
	linkX       float64
	linkY       float64
	linkPage    int64
}

// GeneratePageBlocks draws the line on a new block representing the page.
// Implements the Drawable interface.
func (l *Line) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	var (
		blocks         []*Block
		block          = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx2           = ctx
		x1, pageHeight = l.x1, ctx.PageHeight - l.y1
		x2, y2         = l.x2, ctx.PageHeight - l.y2
	)
	isRelative := l.positioning.IsRelative()
	if isRelative {
		ctx.X += l.margins.Left
		ctx.Y += l.margins.Top
		ctx.Width -= l.margins.Left + l.margins.Right
		ctx.Height -= l.margins.Top + l.margins.Bottom
		x1, pageHeight, x2, y2 = l.computeCoords(ctx)
		if l.Height() > ctx.Height {
			blocks = append(blocks, block)
			block = NewBlock(ctx.PageWidth, ctx.PageHeight)
			ctx.Page++
			ctx2 := ctx
			ctx2.Y = ctx.Margins.Top + l.margins.Top
			ctx2.X = ctx.Margins.Left + l.margins.Left
			ctx2.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom - l.margins.Top - l.margins.Bottom
			ctx2.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - l.margins.Left - l.margins.Right
			ctx = ctx2
			x1, pageHeight, x2, y2 = l.computeCoords(ctx)
		}
	}
	basicLine := contentdraw.BasicLine{X1: x1, Y1: pageHeight, X2: x2, Y2: y2, LineColor: toPdfColor(l.color), Opacity: l.opacity, LineWidth: l.lineWidth, LineStyle: l.style, DashArray: l.dashArray, DashPhase: l.dashPhase}
	gsName, err := block.setOpacity(1.0, l.opacity)
	if err != nil {
		return nil, ctx, err
	}
	markedDraw, _, err := basicLine.MarkedDraw(gsName, l.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		ctx.X = ctx2.X
		ctx.Width = ctx2.Width
		height := l.Height()
		ctx.Y += height + l.margins.Bottom
		ctx.Height -= height
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

func (tp *templateProcessor) parseRectangle(node *templateNode) (any, error) {
	rectangle := tp.creator.NewRectangle(0, 0, 0, 0)
	if err := tp.applyAttrs(node, rectangle, rectangleAttrs); err != nil {
		return nil, err
	}
	return rectangle, nil
}

func parseRGBColor(s string) (float64, float64, float64) {
	s2 := strings.TrimPrefix(s, "rgb(")
	s2 = strings.TrimSuffix(s2, ")")
	strs := strings.Split(s2, ",")
	if len(strs) != 3 {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	var value, value2, value3 float64
	value, err := parseColorComponent(strs[0])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value2, err = parseColorComponent(strs[1])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value3, err = parseColorComponent(strs[2])
	if err != nil {
		common.Log.Debug("Invalid rgb color specification: %s", s)
		return 0, 0, 0
	}
	value4 := value / 255.0
	value5 := value2 / 255.0
	value6 := value3 / 255.0
	return value4, value5, value6
}

// NewChart creates a new creator drawable based on the provided
// unichart chart component.
func NewChart(chart chartrender.ChartRenderable) *Chart { return newChart(chart) }

// CreateTableOfContents sets a function to generate table of contents.
//
// Chapters drawn inside the callback (via NewChapter + Draw) are not tracked
// by Creator.ChapterForPage and will not appear in header/footer callback
// args. The callback is expected to customize the TOC layout, not add
// chapter bodies.
func (c *Creator) CreateTableOfContents(genTOCFunc func(toc *TOC) error) {
	c.genTOCFunc = genTOCFunc
}

// SetFillColor sets the fill color of the rectangle.
func (r *Rectangle) SetFillColor(col Color) { r.fillColor = col }

// GetPosition returns the 1-based row and column the cell starts from.
// Consistent with TableCell.GetPosition.
func (gc *GridCell) GetPosition() (row, col int) {
	return gc.row + 1, gc.col + 1
}

// AddInfo is used to append a piece of invoice information in the template
// information table.
func (i *Invoice) AddInfo(description, value string) (*InvoiceCell, *InvoiceCell) {
	invoiceCells := [2]*InvoiceCell{i.newCell(description, i.defaultCellProps), i.newCell(value, i.defaultCellProps)}
	i.infoLines = append(i.infoLines, invoiceCells)
	return invoiceCells[0], invoiceCells[1]
}

// SetAltText sets the alternative text for the text chunk.
func (tec *TextChunk) SetAltText(text string) { tec.altText = &text }
