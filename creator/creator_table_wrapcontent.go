// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"math"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func (t *Table) wrapContent(ctx2 DrawContext) error {
	t.sortCells()
	value := ctx2.contentHeight() - t.headerHeight()
	fn := func(head2 VectorDrawable, width, value2, height float64) (head, tail VectorDrawable) {
		switch head3 := head2.(type) {
		case *StyledParagraph:
			ctx := ctx2
			ctx.Width = width
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom - 0.5*head3.getTextHeight())
			head4, tail2, err := head3.split(ctx)
			if err != nil {
				common.Log.Error("Error wrap styled paragraph: %v", err.Error())
				return head2, nil
			}
			if head4 != nil {
				head = head4
			}
			if tail2 != nil {
				tail = tail2
			}
		case *Division:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom)
			head2, tail2 := head3.split(ctx, value-head3.margins.Vertical())
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		case *List:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = math.Floor(height - head3.margins.Top - head3.margins.Bottom)
			head2, tail2 := head3.split(ctx, value-head3.margins.Vertical())
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		case *Table:
			ctx := ctx2
			ctx.Width = width - value2
			ctx.Height = height
			head2, tail2 := head3.split(ctx, value)
			if head2 != nil {
				head = head2
			}
			if tail2 != nil {
				tail = tail2
			}
		default:
			return head2, nil
		}
		return head, tail
	}
	fn2 := func(head2 VectorDrawable, width, value, height float64) []VectorDrawable {
		if para, ok := head2.(*StyledParagraph); ok {
			ctx := ctx2
			ctx.Width = width
			ctx.Height = math.Floor(height - para.margins.Top - para.margins.Bottom - 0.5*para.getTextHeight())
			toPieces, err := para.splitToPieces(ctx)
			if err != nil {
				common.Log.Error("Error splitting styled paragraph: %v", err.Error())
				return []VectorDrawable{head2}
			}
			drawables := make([]VectorDrawable, len(toPieces))
			for i, toPiece := range toPieces {
				drawables[i] = toPiece
			}
			return drawables
		}
		var drawables []VectorDrawable
		head := head2
		for {
			head2, tail := fn(head, width, value, height)
			if head2 == nil {
				drawables = append(drawables, head)
				break
			}
			drawables = append(drawables, head2)
			if tail == nil {
				break
			}
			head = tail
		}
		return drawables
	}
	contentHeight := ctx2.contentHeight()
	height2 := contentHeight
	if headerHeight := t.headerHeight(); headerHeight < height2 {
		height2 -= headerHeight
	}
	type cellSplit struct {
		cell  *TableCell
		parts []VectorDrawable
	}
	fn3 := func() {
		if t.enableRowWrap {
			return
		}
		ok := false
		for i := t.rows; i >= 1; i-- {
			height := height2
			if t.hasHeaderRows && i >= t.headerStartRow && i <= t.headerEndRow {
				height = contentHeight
			}
			if i-1 >= len(t.rowHeights) || t.rowHeights[i-1] <= height {
				continue
			}
			var cellSplits []cellSplit
			n := 1
			rowSpan := 1
			for _, cell := range t.cells {
				if cell.row != i {
					continue
				}
				width := cell.width(t.colWidths, ctx2.Width)
				if cell.height(width) <= height {
					continue
				}
				fn := fn2(cell.content, width, cell.indent, height)
				if len(fn) < 2 {
					continue
				}
				cellSplits = append(cellSplits, cellSplit{cell, fn})
				if len(fn) > n {
					n = len(fn)
				}
				if cell.rowSpan > rowSpan {
					rowSpan = cell.rowSpan
				}
			}
			if n < 2 {
				continue
			}
			t.insertRows(i, n-1)
			n2 := i + (n - 1) + (rowSpan - 1)
			for j := i; j <= n2 && j-1 < len(t.rowHeights); j++ {
				t.rowHeights[j-1] = t.defaultRowHeight
			}
			ok = true
			for _, cellSplit := range cellSplits {
				cellSplit.cell.content = cellSplit.parts[0]
				cellSplit.cell.rowSpan = 1
				for j := 1; j < len(cellSplit.parts); j++ {
					props := cellSplit.cell.cloneProps(cellSplit.parts[j])
					props.rowSpan = 1
					props.row = i + j
					props.col = cellSplit.cell.col
					t.cells = append(t.cells, props)
				}
			}
		}
		if ok {
			t.sortCells()
			t.updateRowHeights(ctx2.Width)
		}
	}
	fn3()
	t.splitSpanningCellsAtBreaks(ctx2, fn)
	fn3()
	t.sortCells()
	return nil
}

// GetColspan returns the number of columns the cell spans.
func (tc *TableCell) GetColspan() int { return tc.colSpan }

// GetTextAlignment returns the horizontal alignment of the text within the
// paragraph.
func (sp *StyledParagraph) GetTextAlignment() TextAlignment { return sp.alignment }

func flushToken(tokens []token, text string) ([]token, string) {
	if text != "" {
		tokens = append(tokens, token{text, false})
		text = ""
	}
	return tokens, text
}

// StyledParagraph represents text drawn with a specified font and can wrap across lines and pages.
// By default occupies the available width in the drawing context.
type StyledParagraph struct {
	taggedDrawable
	chunks            []*TextChunk
	defaultStyle      TextStyle
	linkStyle         TextStyle
	alignment         TextAlignment
	verticalAlignment TextVerticalAlignment
	lineHeight        float64
	enableWrap        bool
	width             float64
	enableWordWrap    bool
	maxLines          int
	defaultWrap       bool
	textOverflow      TextOverflow
	angle             float64
	margins           Margins
	positioning       Positioning
	xPos              float64
	yPos              float64
	unusedA           float64
	unusedB           float64
	lines             [][]*TextChunk
	beforeRender      func(para *StyledParagraph, ctx DrawContext)
	languageID        string
	artifact          *model.Artifact
	enableShaping     bool
	dropCaps          *DropCapsOptions
	dropCapsEnabled   bool
}

// SetStructureType sets the structure type for the drawable.
func (td *taggedDrawable) SetStructureType(structureType model.StructureType) {
	if td.tagInfo == nil {
		td.tagInfo = model.NewStructureTagInfo()
	}
	td.tagInfo.StructureType = structureType
}

var (
	positioningValues             = map[string]Positioning{"relative": PositionRelative, "absolute": PositionAbsolute}
	textRenderingModeValues       = map[string]TextRenderingMode{"fill": TextRenderingModeFill, "stroke": TextRenderingModeStroke, "fill-stroke": TextRenderingModeFillStroke, "invisible": TextRenderingModeInvisible, "fill-clip": TextRenderingModeFillClip, "stroke-clip": TextRenderingModeStrokeClip, "fill-stroke-clip": TextRenderingModeFillStrokeClip, "clip": TextRenderingModeClip}
	textAlignmentValues           = map[string]TextAlignment{"left": TextAlignmentLeft, "right": TextAlignmentRight, "center": TextAlignmentCenter, "justify": TextAlignmentJustify}
	textVerticalAlignmentValues   = map[string]TextVerticalAlignment{"baseline": TextVerticalAlignmentBaseline, "center": TextVerticalAlignmentCenter}
	textOverflowValues            = map[string]TextOverflow{"visible": TextOverflowVisible, "hidden": TextOverflowHidden}
	fitModeValues                 = map[string]FitMode{"none": FitModeNone, "fill-width": FitModeFillWidth}
	lineStyleValues               = map[string]contentdraw.LineStyle{"solid": contentdraw.LineStyleSolid, "dashed": contentdraw.LineStyleDashed}
	cellHorizontalAlignmentValues = map[string]CellHorizontalAlignment{"left": CellHorizontalAlignmentLeft, "center": CellHorizontalAlignmentCenter, "right": CellHorizontalAlignmentRight}
	cellVerticalAlignmentValues   = map[string]CellVerticalAlignment{"top": CellVerticalAlignmentTop, "middle": CellVerticalAlignmentMiddle, "bottom": CellVerticalAlignmentBottom}
	cellBorderStyleValues         = map[string]CellBorderStyle{"none": CellBorderStyleNone, "single": CellBorderStyleSingle, "double": CellBorderStyleDouble}
	horizontalAlignmentValues     = map[string]HorizontalAlignment{"left": HorizontalAlignmentLeft, "center": HorizontalAlignmentCenter, "right": HorizontalAlignmentRight}
	encoderFactories              = map[string]func() core.StreamEncoder{"flate": func() core.StreamEncoder { return core.NewFlateEncoder() }, "dct": func() core.StreamEncoder { return core.NewDCTEncoder() }}
)
var templateTags = map[string]*templateTag{"paragraph": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseStyledParagraphTag, attrs: styledParagraphAttrs}, "text-chunk": {allowedParents: map[string]struct{}{"paragraph": {}}, parse: parseTextChunkTag, attrs: textChunkAttrs}, "division": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseDivisionTag, attrs: divisionAttrs}, "table": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseTableTag, attrs: tableAttrs}, "table-cell": {allowedParents: map[string]struct{}{"table": {}}, parse: parseTableCellTag, attrs: tableCellAttrs, singleChild: true}, "line": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}}, parse: parseLineTag, attrs: lineAttrs}, "rectangle": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}}, parse: parseRectangleTag, attrs: rectangleAttrs}, "ellipse": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}}, parse: parseEllipseTag, attrs: ellipseAttrs}, "image": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseImageTag, attrs: imageAttrs}, "chapter": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "chapter": {}}, parse: parseChapterTag, attrs: chapterAttrs}, "chapter-heading": {allowedParents: map[string]struct{}{"chapter": {}}, parse: parseChapterHeadingTag, attrs: chapterHeadingAttrs}, "chart": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}}, parse: parseChartTag, attrs: chartAttrs}, "page-break": {allowedParents: map[string]struct{}{"creator": {}, "chapter": {}, "division": {}}, parse: parsePageBreakTag, attrs: emptyAttrs}, "background": {allowedParents: map[string]struct{}{"block": {}, "division": {}, "chapter": {}}, parse: parseBackgroundTag, attrs: backgroundAttrs}, "list": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseListTag, attrs: listAttrs}, "list-item": {allowedParents: map[string]struct{}{"list": {}}, parse: parseListItemTag, attrs: emptyAttrs, singleChild: true}, "list-marker": {allowedParents: map[string]struct{}{"list": {}, "list-item": {}}, parse: parseListMarkerTag, attrs: textChunkAttrs, allowMultipleChildren: true}}
