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
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/linebreak"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

func validateColorKeyMask(nums []int, xObjectImage *model.XObjectImage) error {
	if xObjectImage.ColorSpace == nil {
		return errors.New("image has no colorspace")
	}
	if n := 2 * xObjectImage.ColorSpace.GetNumComponents(); len(nums) != n {
		return fmt.Errorf("color key mask needs %d values for colorspace %s, got %d", n, xObjectImage.ColorSpace.String(), len(nums))
	}
	if xObjectImage.BitsPerComponent == nil {
		return errors.New("image has no bits per component")
	}
	n := (1 << uint(*xObjectImage.BitsPerComponent)) - 1
	for _, n2 := range nums {
		if n2 > n {
			return fmt.Errorf("color key mask value %d out of range [0, %d]", n2, n)
		}
	}
	return nil
}

// Scale scales the ellipse dimensions by the specified factors.
func (e *Ellipse) Scale(xFactor, yFactor float64) {
	e.width = xFactor * e.width
	e.height = yFactor * e.height
}

// AddShadingResource adds shading dictionary inside the resources dictionary.
func (ls *LinearShading) AddShadingResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingResource(block, ls.shadingModel().ToPdfObject())
}

type pathParserError struct{ msg string }

func splitIntoWords(s string) ([]string, error) {
	if !utf8.ValidString(s) {
		return []string{s}, errors.New("input is not valid UTF-8")
	}
	var (
		strs []string
		rs   []rune
	)
	for _, r := range s {
		if r == '\u000A' {
			if len(rs) > 0 {
				strs = append(strs, string(rs))
			}
			strs = append(strs, string(r))
			rs = nil
			continue
		}
		rs = append(rs, r)
	}
	if len(rs) > 0 {
		strs = append(strs, string(rs))
	}
	var intoWords []string
	for _, str := range strs {
		r := []rune(str)
		scanner := linebreak.NewScanner(r)
		var rs []rune
		for i := 0; i < len(r); i++ {
			_, action, err := scanner.Next()
			if err != nil {
				return nil, err
			}
			if action == linebreak.BreakProhibited || unicode.IsSpace(r[i]) {
				rs = append(rs, r[i])
				if unicode.IsSpace(r[i]) {
					intoWords = append(intoWords, string(rs))
					rs = []rune{}
				}
				continue
			} else {
				if len(rs) > 0 {
					intoWords = append(intoWords, string(rs))
				}
				rs = []rune{r[i]}
			}
		}
		if len(rs) > 0 {
			intoWords = append(intoWords, string(rs))
		}
	}
	return intoWords, nil
}

// NewEllipse creates a new ellipse with the center at (`xc`, `yc`),
// having the specified width and height.
// NOTE: In relative positioning mode, `xc` and `yc` are calculated using the
// current context. Furthermore, when the fit mode is set to fill the available
// space, the ellipse is scaled so that it occupies the entire context width
// while maintaining the original aspect ratio.
func (c *Creator) NewEllipse(xc, yc, width, height float64) *Ellipse {
	return newEllipse(xc, yc, width, height)
}

func (cc cmykColor) ToRGB() (float64, float64, float64) {
	k := cc.k
	return 1 - (cc.c*(1-k) + k), 1 - (cc.m*(1-k) + k), 1 - (cc.y*(1-k) + k)
}

func rotatedCorners(rect *model.PdfRectangle, value float64) [4]contentdraw.Point {
	x := value * math.Pi / 180.0
	width := rect.Width()
	height := rect.Height()
	value2 := math.Sin(x)
	value3 := math.Cos(x)
	points := [4]contentdraw.Point{{X: precision.RoundFloat(rect.Llx, 3), Y: precision.RoundFloat(rect.Lly, 3)}, {X: precision.RoundFloat(rect.Llx+width*value3, 3), Y: precision.RoundFloat(rect.Lly+width*value2, 3)}, {X: precision.RoundFloat(rect.Llx+width*value3-height*value2, 3), Y: precision.RoundFloat(rect.Lly+width*value2+height*value3, 3)}, {X: precision.RoundFloat(rect.Llx-height*value2, 3), Y: precision.RoundFloat(rect.Lly+height*value3, 3)}}
	return points
}

var (
	attrFuncRe         = regexp.MustCompile("([\\w-]+)\\('(.+)'\\)")
	errInvalidCreator  = errors.New("invalid template creator instance")
	errInvalidParent   = errors.New("invalid template parent node")
	errInvalidChild    = errors.New("invalid template child node")
	errInvalidResource = errors.New("invalid template resource")
	errInvalidReader   = errors.New("invalid template reader")
)

// SetColorBottom sets border color for bottom.
func (bo *border) SetColorBottom(col Color) { bo.colorBottom = col }

// SetSideBorderWidth sets the cell's side border width.
func (tc *TableCell) SetSideBorderWidth(side CellBorderSide, width float64) {
	switch side {
	case CellBorderSideAll:
		tc.borderWidthTop = width
		tc.borderWidthBottom = width
		tc.borderWidthLeft = width
		tc.borderWidthRight = width
	case CellBorderSideTop:
		tc.borderWidthTop = width
	case CellBorderSideBottom:
		tc.borderWidthBottom = width
	case CellBorderSideLeft:
		tc.borderWidthLeft = width
	case CellBorderSideRight:
		tc.borderWidthRight = width
	}
}

// GetSideBorderStyle returns the cell's border style for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (tc *TableCell) GetSideBorderStyle(side CellBorderSide) CellBorderStyle {
	switch side {
	case CellBorderSideBottom:
		return tc.borderStyleBottom
	case CellBorderSideLeft:
		return tc.borderStyleLeft
	case CellBorderSideRight:
		return tc.borderStyleRight
	default:
		return tc.borderStyleTop
	}
}

// NewCellProps returns the default properties of an invoice cell.
func (i *Invoice) NewCellProps() InvoiceCellProps {
	colorRGBFrom8bit := ColorRGBFrom8bit(255, 255, 255)
	return InvoiceCellProps{TextStyle: i.defaultStyle, Alignment: CellHorizontalAlignmentLeft, BackgroundColor: colorRGBFrom8bit, BorderColor: colorRGBFrom8bit, BorderWidth: 1, BorderSides: []CellBorderSide{CellBorderSideAll}}
}

func (lii *listItem) ctxHeight(width float64) float64 {
	var height float64
	switch content := lii.content.(type) {
	case *StyledParagraph:
		if content.enableWrap {
			content.SetWidth(width - content.margins.Horizontal())
		}
		height = content.Height() + content.margins.Vertical()
		height += 0.5 * content.getTextHeight()
	case *List:
		height = content.ctxHeight(width) + content.margins.Vertical()
	case *Image:
		height = content.Height() + content.margins.Vertical()
	case *Division:
		height = content.ctxHeight(width) + content.margins.Vertical() + content.padding.Vertical()
	case *Table:
		content.updateRowHeights(width - content.margins.Horizontal())
		height = content.Height() + content.margins.Vertical()
	default:
		height = lii.content.Height()
	}
	return height
}

func (tp *templateProcessor) parseListMarker(node *templateNode) (any, error) {
	componentParent := node.componentParent()
	if componentParent == nil {
		tp.nodeLogError(node, "List marker parent cannot be nil.")
		return nil, errInvalidParent
	}
	var marker *TextChunk
	switch value := componentParent.value.(type) {
	case *List:
		marker = &value.marker
	case *listItem:
		marker = &value.marker
	default:
		tp.nodeLogError(node, "%v is not a valid parent node for list marker.", value)
		return nil, errInvalidParent
	}
	if _, err := tp.parseTextChunk(node, marker); err != nil {
		if tp.options.Strict {
			return nil, err
		}
		tp.nodeLogError(node, "Could not parse list marker: `%v`.", err)
		return nil, nil
	}
	return marker, nil
}

// NewMultiCell makes a new cell with given colspan and rowspan and inserts it into the row at the current position.
func (gr *GridRow) NewMultiCell(colspan, rowspan int) (*GridCell, error) {
	cell := &GridCell{col: len(gr.cells), row: gr.index, colSpan: colspan, rowSpan: rowspan}
	cell.row = gr.index
	cell.col = 0
	for _, cell2 := range gr.cells {
		cell.col += cell2.colSpan
	}
	for i, row := range gr.grid.rows {
		if i == gr.index+1 {
			break
		}
		for _, cell2 := range row.cells {
			if cell2.row+cell2.rowSpan > cell.row {
				if cell2.col+cell2.colSpan > cell.col {
					cell.col = cell2.col + cell2.colSpan
				}
			}
		}
	}
	if cell.col >= gr.grid.cols {
		return nil, errors.New("can't add any more cells to this row")
	}
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
	gr.cells = append(gr.cells, cell)
	cell.structType = model.StructureTypeTableData
	return cell, nil
}

func (tp *templateProcessor) typographyValueErr(s2, s string) error {
	switch s2 {
	case "font-size", "line-height":
		_, err := parseFloat64(s)
		return err
	case "color":
		_, err := tp.resolveColor(s)
		return err
	case "font":
		return tp.validateFontValue(s)
	case "text-align":
		if _, ok := textAlignmentValues[s]; !ok {
			return fmt.Errorf("invalid value %q", s)
		}
	}
	return nil
}

type subtableSplit struct {
	index  int
	table  *Table
	ctx    DrawContext
	height float64
}

func (text *templateText) pos() *templateNode {
	return &templateNode{line: text.line, col: text.col, offset: text.offset}
}

// Horizontal returns total horizontal (left + right) margin.
func (m *Margins) Horizontal() float64 { return m.Left + m.Right }

// SetLinePageStyle sets the style for the page part of all new lines
// of the table of contents.
func (to *TOC) SetLinePageStyle(style TextStyle) { to.linePageStyle = style }

func uniqueFontName(s string, obj core.PdfObject, resources *model.PdfPageResources) core.PdfObjectName {
	s2 := strings.TrimRightFunc(strings.TrimSpace(s), func(r rune) bool { return unicode.IsNumber(r) })
	if s2 == "" {
		s2 = "Font"
	}
	n := 0
	keyName := core.PdfObjectName(s)
	for {
		font, ok := resources.GetFontByName(keyName)
		if !ok || font == obj {
			break
		}
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("%s%d", s2, n))
	}
	return keyName
}
