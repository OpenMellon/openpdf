// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"encoding/xml"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"text/template"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
)

func parseSVGElementTree(decoder *xml.Decoder) (*GraphicSVGElement, error) {
	for {
		token, err := decoder.Token()
		if token == nil && err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token2 := token.(type) {
		case xml.StartElement:
			return newGraphicSVGElement(token2), nil
		}
	}
	return &GraphicSVGElement{}, nil
}

// NewCell returns a new invoice table cell.
func (i *Invoice) NewCell(value string) *InvoiceCell {
	return i.newCell(value, i.NewCellProps())
}

// GetOpacity returns the cell's opacity in the range 0-1.
func (gc *GridCell) GetOpacity() float64 { return gc.opacity }

func fontAttr(i any) attrApplyFunc {
	value2 := validateSetter("fontAttr", i, 0, pdfFontPtrType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		if target == nil {
			if err := proc.validateFontValue(value); err != nil {
				if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
					return err2
				}
			}
			return nil
		}
		font, err := proc.resolveFont(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(font))
		return nil
	}
}

// GetMargins returns the margins of the ellipse: left, right, top, bottom.
func (e *Ellipse) GetMargins() (float64, float64, float64, float64) {
	return e.margins.Left, e.margins.Right, e.margins.Top, e.margins.Bottom
}

// SetCompactMode sets the compact mode flag for this table.
//
// By enabling compact mode, a table cell that contains a StyledParagraph
// will not add extra height when calculating its height.
//
// The default value is false.
func (t *Table) SetCompactMode(enable bool) { t.compactMode = enable }

func borderRadiusAttr(i any) attrApplyFunc {
	value2 := validateSetter("borderRadiusAttr", i, 0, float64Type, float64Type, float64Type, float64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		topLeft, topRight, bottomRight, bottomLeft, err := parseBorderRadius(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
		}
		callSetter(value2, target, reflect.ValueOf(topLeft), reflect.ValueOf(topRight), reflect.ValueOf(bottomRight), reflect.ValueOf(bottomLeft))
		return nil
	}
}

func parseColorComponent(s string) (float64, error) {
	s = strings.TrimSpace(s)
	var colorComponent float64
	if before, ok := strings.CutSuffix(s, "%"); ok {
		float, err := strconv.ParseFloat(before, 64)
		if err != nil {
			return 0, err
		}
		colorComponent = (float * 255.0) / 100.0
	} else {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		colorComponent = float64(n)
	}
	return colorComponent, nil
}

// SetPositioning sets the positioning of the line (absolute or relative).
func (l *Line) SetPositioning(positioning Positioning) { l.positioning = positioning }

// SetStyle sets the style of the line (solid or dashed).
func (l *Line) SetStyle(style contentdraw.LineStyle) { l.style = style }

// GetMargins returns the margins of the line: left, right, top, bottom.
func (l *Line) GetMargins() (float64, float64, float64, float64) {
	return l.margins.Left, l.margins.Right, l.margins.Top, l.margins.Bottom
}

// ParsedTemplate is a template read and compiled once that can be rendered many
// times, into different creators, with different data. Compiling (reading the
// source, compiling the text/template and its subtemplates, and installing the
// escaping helpers) is done up front by ParseTemplate; a creator's RenderTemplate
// or ValidateTemplate (or the Block equivalents) executes the compiled template
// with the caller's data and builds the resulting XML tree, so the read and
// compile cost is not paid per render. A parsed template is not bound to a
// creator and may be reused across documents.
//
// Fonts referenced by a path are loaded per render (once per RenderTemplate
// call), so the same font is embedded independently in each document.
type ParsedTemplate struct {
	options *TemplateOptions
	tmpl    *template.Template
	source  string
}

// PageFinalizeFunctionArgs holds the input arguments provided to the page
// finalize callback function which can be set using Creator.PageFinalize.
type PageFinalizeFunctionArgs struct {
	PageNum    int
	PageWidth  float64
	PageHeight float64
	TOCPages   int
	TotalPages int

	// Chapter describes the chapter the current page belongs to, resolved via
	// Creator.ChapterForPage. It is nil on pages that precede the first chapter.
	Chapter *ChapterInfo
}

// FillOpacity returns the fill opacity of the ellipse (0-1).
func (e *Ellipse) FillOpacity() float64 { return e.fillOpacity }

// AddSubtable copies the cells of the subtable in the table, starting with the
// specified position. The table row and column indices are 1-based, which
// makes the position of the first cell of the first row of the table 1,1.
// The table is automatically extended if the subtable exceeds its columns.
// This can happen when the subtable has more columns than the table or when
// one or more columns of the subtable starting from the specified position
// exceed the last column of the table.
func (t *Table) AddSubtable(row, col int, subtable *Table) {
	for _, cell := range subtable.cells {
		cell2 := &TableCell{}
		*cell2 = *cell
		cell2.table = t
		cell2.col += col - 1
		if cols := t.cols - (cell2.col - 1); cols < cell2.colSpan {
			t.cols += cell2.colSpan - cols
			t.resetColumnWidths()
			common.Log.Debug("Table: subtable exceeds destination table. Expanding table to %d columns.", t.cols)
		}
		cell2.row += row - 1
		rowHeight := subtable.rowHeights[cell.row-1]
		if cell2.row > t.rows {
			for cell2.row > t.rows {
				t.rows++
				t.rowHeights = append(t.rowHeights, t.defaultRowHeight)
			}
			t.rowHeights[cell2.row-1] = rowHeight
		} else {
			t.rowHeights[cell2.row-1] = math.Max(t.rowHeights[cell2.row-1], rowHeight)
		}
		t.cells = append(t.cells, cell2)
	}
	t.sortCells()
}

// Width returns the width of the specified text chunk.
func (tec *TextChunk) Width() float64 {
	var (
		value float64
		style = tec.Style
	)
	for _, r := range tec.Text {
		runeMetrics, ok := style.Font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("ERROR: Rune char metrics not found! rune=0x%04x=%c font=%s %#q", r, r, style.Font.BaseFont(), style.Font.Subtype())
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Font: %#v", style.Font)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Encoder: %#v", style.Font.Encoder())
			}
		}
		fontSize := style.FontSize * runeMetrics.Wx
		fontSize2 := fontSize
		if r != ' ' {
			fontSize2 = fontSize + style.CharSpacing*1000.0
		}
		value += fontSize2
	}
	return value / 1000.0
}

func boolAttr(i any) attrApplyFunc {
	value2 := validateSetter("boolAttr", i, 0, boolType)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		flag, err := parseBool(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
			flag = false
		}
		callSetter(value2, target, reflect.ValueOf(flag))
		return nil
	}
}

func (tp *templateProcessor) parseTable(node *templateNode) (any, error) {
	tableColumns, err := tp.tableColumns(node)
	if err != nil {
		return nil, err
	}
	table := tp.creator.NewTable(int(tableColumns))
	if err := tp.applyAttrs(node, table, tableAttrs); err != nil {
		return nil, err
	}
	if err := tp.setTableHeaderRows(node, table, table.headerStartRow, table.headerEndRow); err != nil {
		return nil, err
	}
	return table, nil
}

// Padding returns the padding of the component.
func (d *Division) Padding() (left, right, top, bottom float64) {
	return d.padding.Left, d.padding.Right, d.padding.Top, d.padding.Bottom
}

// SetMargins sets the Block's left, right, top, bottom, margins.
func (b *Block) SetMargins(left, right, top, bottom float64) {
	b.margins.Left = left
	b.margins.Right = right
	b.margins.Top = top
	b.margins.Bottom = bottom
}

// HasHeader returns whether the table has header rows that are repeated for
// every page the table spans. See SetHeaderRows. Note that Table defines its
// header at the table level over a row range, whereas Grid defines headers
// per row via the row's section (see GridRow.IsHeader).
func (t *Table) HasHeader() bool { return t.hasHeaderRows }

// SetTextVerticalAlignment sets the vertical alignment of the text within the
// bounds of the styled paragraph.
//
// Note: Currently Styled Paragraph doesn't support TextVerticalAlignmentBottom
// as that option only used for aligning text chunks.
//
// In order to change the vertical alignment of individual text chunks, use TextChunk.VerticalAlignment.
func (sp *StyledParagraph) SetTextVerticalAlignment(align TextVerticalAlignment) {
	sp.verticalAlignment = align
}

func newTOC(text string, lineNumberStyle, style2 TextStyle) *TOC {
	style := style2
	style.FontSize = 14
	styledParagraph := newStyledParagraph(style)
	styledParagraph.SetEnableWrap(true)
	styledParagraph.SetTextAlignment(TextAlignmentLeft)
	styledParagraph.SetMargins(0, 0, 0, 5)
	chunk := styledParagraph.Append(text)
	chunk.Style = style
	return &TOC{heading: styledParagraph, lines: []*TOCLine{}, lineNumberStyle: lineNumberStyle, lineTitleStyle: lineNumberStyle, lineSeparatorStyle: lineNumberStyle, linePageStyle: lineNumberStyle, lineSeparator: ".", lineLevelOffset: 10, lineMargins: Margins{0, 0, 2, 2}, positioning: PositionRelative, defaultStyle: lineNumberStyle, showLinks: true, taggedDrawable: taggedDrawable{structType: model.StructureTypeTOC}}
}

// SetDefaultFont sets the fonts used by default by creator components which
// have no font specified, replacing the standard Helvetica and Helvetica-Bold
// fonts. This covers text styles created via NewTextStyle, components built
// on them (e.g. NewStyledParagraph, NewTOC, NewInvoice) and template
// components with no `font` attribute. Setting a composite font enables
// scripts the standard fonts do not cover, such as Cyrillic or CJK. The
// method should be called before any components are created or drawn. A nil
// argument leaves the corresponding default font unchanged.
func (c *Creator) SetDefaultFont(fontRegular, fontBold *model.PdfFont) {
	fontRegular2 := c.fontRegular
	fontBold2 := c.fontBold
	if fontRegular != nil {
		c.fontRegular = fontRegular
	}
	if fontBold != nil {
		c.fontBold = fontBold
	}
	c.toc.replaceFonts(fontRegular2, c.fontRegular, fontBold2, c.fontBold)
}

type componentRenderer interface{ Draw(drawable Drawable) error }

const (
	TextAlignmentLeft TextAlignment = iota
	TextAlignmentRight
	TextAlignmentCenter
	TextAlignmentJustify
)
