// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/license"
	"github.com/OpenMellon/openpdf/model"
)

// Creator is a wrapper around functionality for creating PDF reports and/or adding new
// content onto imported PDF pages, etc.
type Creator struct {
	// Errors keeps error messages that should not interrupt pdf processing and to be checked later.
	Errors []error

	// UnsupportedCharacterReplacement is character that will be used to replace unsupported glyph.
	// The value will be passed to drawing context.
	UnsupportedCharacterReplacement rune
	pages                           []*model.PdfPage
	blocks                          map[*model.PdfPage]*Block
	pageTransforms                  map[*model.PdfPage]*pageTransformations
	activePage                      *model.PdfPage
	pageSize                        PageSize
	context                         DrawContext
	pageMargins                     Margins
	pageWidth, pageHeight           float64
	chapterCount                    int
	chapters                        []chapterInfoEntry
	genFrontPageFunc                func(frontpageFunctionArgs FrontpageFunctionArgs)
	genTOCFunc                      func(toc *TOC) error
	drawHeaderFunc                  func(block *Block, headerFunctionArgs HeaderFunctionArgs)
	drawFooterFunc                  func(block *Block, footerFunctionArgs FooterFunctionArgs)
	pageFinalizeFunc                func(pageFinalizeFunctionArgs PageFinalizeFunctionArgs) error
	pdfWriterAccessFunc             func(writer *model.PdfWriter) error
	finalized                       bool

	// Controls whether a table of contents will be generated.
	AddTOC bool

	// CustomTOC specifies if the TOC is rendered by the user.
	// When the `CustomTOC` field is set to `true`, the default TOC component is not rendered.
	// Instead the TOC is drawn by the user, in the callback provided to
	// the `Creator.CreateTableOfContents` method.
	// If `CustomTOC` is set to `false`, the callback provided to
	// `Creator.CreateTableOfContents` customizes the style of the automatically generated TOC component.
	CustomTOC bool
	toc       *TOC

	// Controls whether outlines will be generated.
	AddOutlines       bool
	outline           *model.Outline
	outlineTree       *model.PdfOutlineTreeNode
	acroForm          *model.PdfAcroForm
	pageLabels        core.PdfObject
	optimizer         model.Optimizer
	subsetFonts       []*model.PdfFont
	fontRegular       *model.PdfFont
	fontBold          *model.PdfFont
	taggingEnabled    bool
	docKDict          *model.KDict
	pageMCIDs         map[int64]int64
	structTreeRoot    *model.StructTreeRoot
	structNamespace   *model.PdfStructureNamespace
	viewerPreferences *model.ViewerPreferences
	language          string

	// AutofixPageContentStream indicates whether the creator should attempt to fix
	// page content streams that have unclosed `q` and `Q` commands.
	// The default value is `true`.
	// When set to `false`, the creator will skip the content stream checking and wrapping.
	// This will speed up and optimize memory usage the creation of PDF, the drawback is
	// need to ensure that the source of PDF content streams are well-formed.
	AutofixPageContentStream bool
}

func (ppe pathParserError) Error() string { return ppe.msg }

// LevelOffset returns the amount of space an indentation level occupies.
func (tl *TOCLine) LevelOffset() float64 { return tl.levelOffset }

// GetDropCapsOptions returns the drop caps options set on the paragraph, if any.
func (sp *StyledParagraph) GetDropCapsOptions() *DropCapsOptions { return sp.dropCaps }

// SetActualText sets the actual text for the text chunk.
func (tec *TextChunk) SetActualText(text string) { tec.actualText = &text }

// New creates a new instance of the PDF Creator.
func New() *Creator {
	const trackName = "creator.New"
	c := &Creator{}
	c.pages = []*model.PdfPage{}
	c.blocks = map[*model.PdfPage]*Block{}
	c.pageTransforms = map[*model.PdfPage]*pageTransformations{}
	c.pageMCIDs = map[int64]int64{}
	c.SetPageSize(PageSizeLetter)
	pageWidth := 0.1 * c.pageWidth
	c.pageMargins.Left = pageWidth
	c.pageMargins.Right = pageWidth
	c.pageMargins.Top = pageWidth
	c.pageMargins.Bottom = pageWidth
	var err error
	c.fontRegular, err = model.NewStandard14Font(model.HelveticaName)
	if err != nil {
		c.fontRegular = model.DefaultFont()
	}
	c.fontBold, err = model.NewStandard14Font(model.HelveticaBoldName)
	if err != nil {
		c.fontBold = model.DefaultFont()
	}
	c.toc = c.NewTOC("Table of Contents")
	c.AddOutlines = true
	c.outline = model.NewOutline()
	c.AutofixPageContentStream = true
	license.TrackUse(trackName)
	return c
}

func (li *List) ctxHeight(width float64) float64 {
	itemWidth := li.itemWidth(width, li.markerWidth())
	var ctxHeight float64
	for _, item := range li.items {
		ctxHeight += item.ctxHeight(itemWidth)
	}
	return ctxHeight
}

func (tp *templateProcessor) validateCarrierValue(node *templateNode, s2, s string) error {
	if !tp.options.Strict {
		return nil
	}
	if err := tp.typographyValueErr(s2, s); err != nil {
		return tp.nodeError(node, "invalid value %q for attribute %q: %v", s, s2, err)
	}
	return nil
}

// MultiCell makes a new cell with the specified row span and col span
// and inserts it into the table at the current position.
func (t *Table) MultiCell(rowspan, colspan int) *TableCell {
	t.curCell++
	col := (t.moveToNextAvailableCell()-1)%t.cols + 1
	row := (t.curCell-1)/t.cols + 1
	for row > t.rows {
		t.rows++
		t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
	}
	cell := &TableCell{}
	cell.row = row
	cell.col = col
	cell.structType = model.StructureTypeTableData
	cell.tagInfo = model.NewStructureTagInfo()
	cell.tagInfo.StructureType = model.StructureTypeTableData
	cell.indent = 5
	cell.borderStyleLeft = CellBorderStyleNone
	cell.borderLineStyle = contentdraw.LineStyleSolid
	cell.horizontalAlignment = CellHorizontalAlignmentLeft
	cell.verticalAlignment = CellVerticalAlignmentTop
	cell.borderWidthLeft = 0
	cell.borderWidthBottom = 0
	cell.borderWidthRight = 0
	cell.borderWidthTop = 0
	borderColorLeft := ColorBlack
	cell.borderColorLeft = borderColorLeft
	cell.borderColorBottom = borderColorLeft
	cell.borderColorRight = borderColorLeft
	cell.borderColorTop = borderColorLeft
	cell.opacity = 1.0
	if rowspan < 1 {
		common.Log.Debug("Table: cell rowspan less that 1 (%d). Setting cell rowspan to 1.", rowspan)
		rowspan = 1
	}
	rows := t.rows - (cell.row - 1)
	if rowspan > rows {
		common.Log.Debug("Table: cell rowspan (%d) exceeds remaining rows (%d). Adding rows.", rowspan, rows)
		t.rows += rowspan - rows
		for i := 0; i < rowspan-rows; i++ {
			t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
		}
	}
	for i := 0; i < colspan && col+i-1 < len(t.rowSpanRemaining); i++ {
		t.rowSpanRemaining[col+i-1] = rowspan - 1
	}
	cell.rowSpan = rowspan
	if colspan < 1 {
		common.Log.Debug("Table: cell colspan less than 1 (%d). Setting cell colspan to 1.", colspan)
		colspan = 1
	}
	cols := t.cols - (cell.col - 1)
	if colspan > cols {
		common.Log.Debug("Table: cell colspan (%d) exceeds remaining row cols (%d). Adjusting colspan.", colspan, cols)
		colspan = cols
	}
	cell.colSpan = colspan
	t.curCell += colspan - 1
	t.cells = append(t.cells, cell)
	cell.table = t
	return cell
}

func (t *Table) withPartialRow(n int, ctx2 DrawContext, value float64) *Table {
	n2 := n + 1
	if n2 > t.rows || n2-1 >= len(t.rowHeights) {
		return nil
	}
	if t.hasHeaderRows && n2 >= t.headerStartRow && n2 <= t.headerEndRow {
		return nil
	}
	if !t.enableRowWrap && t.rowHeights[n2-1] <= value-t.headerHeight()-t.margins.Top-t.margins.Bottom {
		return nil
	}
	ctx := ctx2
	ctx.Width = ctx2.Width - t.margins.Left - t.margins.Right
	ctx.Height = ctx2.Height - t.margins.Top - t.margins.Bottom
	for i := 0; i < n; i++ {
		ctx.Height -= t.rowHeights[i]
	}
	if ctx.Height <= 0 {
		return nil
	}
	withPartialRow := t.clone()
	withPartialRow.enableRowWrap = true
	withPartialRow.sortCells()
	i2 := -1
	for i, cell := range withPartialRow.cells {
		if cell.row == n2 {
			i2 = i
			break
		}
	}
	if i2 < 0 {
		return nil
	}
	row, err := withPartialRow.wrapRow(i2, ctx, ctx.Width, value-t.margins.Top-t.margins.Bottom-t.headerHeight())
	if err != nil || !row || withPartialRow.rows == t.rows {
		return nil
	}
	for _, cell := range withPartialRow.cells {
		if cell.row == n2 && cell.content != nil {
			withPartialRow.enableRowWrap = t.enableRowWrap
			return withPartialRow
		}
	}
	return nil
}

type taggedDrawable struct {
	structType model.StructureType
	tagInfo    *model.StructureTagInfo
}

func copyAnnotation(annotation *model.PdfAnnotation) *model.PdfAnnotation {
	if annotation == nil {
		return nil
	}
	var pdfAnnotation *model.PdfAnnotation
	switch context := annotation.GetContext().(type) {
	case *model.PdfAnnotationLink:
		if linkAnnotation := copyLinkAnnotation(context); linkAnnotation != nil {
			pdfAnnotation = linkAnnotation.PdfAnnotation
		}
	case *model.PdfAnnotationHighlight:
		if highlightAnnotation := copyHighlightAnnotation(context); highlightAnnotation != nil {
			pdfAnnotation = highlightAnnotation.PdfAnnotation
		}
	}
	return pdfAnnotation
}

// SetEnableWrap sets the line wrapping enabled flag.
func (sp *StyledParagraph) SetEnableWrap(enableWrap bool) {
	sp.enableWrap = enableWrap
	sp.defaultWrap = false
}

type templateProcessor struct {
	creator     *Creator
	data        []byte
	options     *TemplateOptions
	renderer    componentRenderer
	filename    string
	classStyles map[string][]xml.Attr
	fontCache   map[string]*model.PdfFont
}

const (
	xmlEscapeFuncName = "__xmlescape"
	rawFuncName       = "raw"
)

func parseChartTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseChart(node)
}

var styledTextTags = map[string]struct{}{"paragraph": {}, "text-chunk": {}, "chapter-heading": {}, "list-marker": {}}
