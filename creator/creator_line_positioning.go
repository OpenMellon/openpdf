// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/model"
	chartrender "github.com/OpenMellon/unichart/render"
)

// Positioning returns the type of positioning the line is set to use.
func (l *Line) Positioning() Positioning { return l.positioning }

var tableCellAttrs = map[string]attrApplyFunc{"colspan": nil, "rowspan": nil, "indent": floatAttr(func(cell *TableCell, indent float64) { cell.SetIndent(indent) }), "align": enumAttr(cellHorizontalAlignmentValues, func(cell *TableCell, halign CellHorizontalAlignment) { cell.SetHorizontalAlignment(halign) }), "vertical-align": enumAttr(cellVerticalAlignmentValues, func(cell *TableCell, valign CellVerticalAlignment) { cell.SetVerticalAlignment(valign) }), "border-style": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) { cell.SetSideBorderStyle(CellBorderSideAll, style) }), "border-style-top": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideTop, style)
}), "border-style-bottom": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideBottom, style)
}), "border-style-left": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideLeft, style)
}), "border-style-right": enumAttr(cellBorderStyleValues, func(cell *TableCell, style CellBorderStyle) {
	cell.SetSideBorderStyle(CellBorderSideRight, style)
}), "border-width": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideAll, width) }), "border-width-top": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideTop, width) }), "border-width-bottom": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideBottom, width) }), "border-width-left": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideLeft, width) }), "border-width-right": floatAttr(func(cell *TableCell, width float64) { cell.SetSideBorderWidth(CellBorderSideRight, width) }), "border-color": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideAll, col) }), "border-color-top": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideTop, col) }), "border-color-bottom": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideBottom, col) }), "border-color-left": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideLeft, col) }), "border-color-right": colorAttr(func(cell *TableCell, col Color) { cell.SetSideBorderColor(CellBorderSideRight, col) }), "border-line-style": enumAttr(lineStyleValues, func(cell *TableCell, style contentdraw.LineStyle) { cell.SetBorderLineStyle(style) }), "background-color": colorAttr(func(cell *TableCell, col Color) { cell.SetBackgroundColor(col) })}

func parsePercentOrFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	var percentOrFloat float64
	if before, ok := strings.CutSuffix(s, "%"); ok {
		float, err := strconv.ParseFloat(before, 64)
		if err != nil {
			return 0, err
		}
		percentOrFloat = float / 100.0
	} else {
		float, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, err
		}
		percentOrFloat = float
	}
	return percentOrFloat, nil
}

var containerTags = map[string]struct{}{}

// MultiColCell makes a new cell with the specified column span and inserts it
// into the table at the current position.
func (t *Table) MultiColCell(colspan int) *TableCell { return t.MultiCell(1, colspan) }

var divisionAttrs = map[string]attrApplyFunc{"enable-page-wrap": boolAttr(func(division *Division, enable bool) { division.EnablePageWrap(enable) }), "margin": marginAttr(func(division *Division, margins Margins) {
	division.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "padding": marginAttr(func(division *Division, margins Margins) {
	division.SetPadding(margins.Left, margins.Right, margins.Top, margins.Bottom)
})}

func isKeycapBase(r rune) bool { return (r >= '0' && r <= '9') || r == '#' || r == '*' }

// NewStyledTOCLine creates a new table of contents line with the provided style.
func (c *Creator) NewStyledTOCLine(number, title, page TextChunk, level uint, style TextStyle) *TOCLine {
	return newTOCLineFromChunks(number, title, page, level, style)
}

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) SetStructPageNumber(pageNumber *int64)              {}
func (i *Invoice) SetStructureType(structureType model.StructureType) {}
func parseFloatFields(s string) ([]float64, error) {
	fields := strings.Fields(s)
	floats := make([]float64, len(fields))
	var err error
	for i, field := range fields {
		float, err2 := strconv.ParseFloat(field, 64)
		if err2 != nil {
			setFirstError(&err, err2)
			float = 0
		}
		floats[i] = float
	}
	return floats, err
}

// GetMargins returns the margins of the graphic svg (left, right, top, bottom).
func (gs *GraphicSVG) GetMargins() (float64, float64, float64, float64) {
	return gs.margins.Left, gs.margins.Right, gs.margins.Top, gs.margins.Bottom
}

func drawableHeight(drawable VectorDrawable, left float64) float64 {
	switch para := drawable.(type) {
	case *StyledParagraph:
		if para.enableWrap {
			para.SetWidth(left - para.margins.Left - para.margins.Right)
		}
		return para.Height() + para.margins.Top + para.margins.Bottom + (0.5 * para.getTextHeight())
	case *Image:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *Rectangle:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom + para.borderWidth
	case *Ellipse:
		para.applyFitMode(left)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *Division:
		return para.ctxHeight(left) + para.margins.Top + para.margins.Bottom + para.padding.Top + para.padding.Bottom
	case *Table:
		para.updateRowHeights(left - para.margins.Left - para.margins.Right)
		return para.Height() + para.margins.Top + para.margins.Bottom
	case *List:
		return para.ctxHeight(left) + para.margins.Top + para.margins.Bottom
	case marginDrawable:
		_, _, top, bottom := para.GetMargins()
		return para.Height() + top + bottom
	default:
		return para.Height()
	}
}

// SetShowNumbering sets a flag to indicate whether or not to show chapter numbers as part of title.
func (ch *Chapter) SetShowNumbering(show bool) {
	ch.showNumbering = show
	ch.heading.SetText(ch.headingText())
}

func (tp *templateProcessor) resolveFontUncached(s string) (*model.PdfFont, error) {
	fontRegular := tp.creator.fontRegular
	if strings.TrimSpace(s) == "" {
		return fontRegular, nil
	}
	ok := false
	for s2 := range strings.SplitSeq(s, ",") {
		s2 = strings.TrimSpace(s2)
		if s2 == "" {
			continue
		}
		ok = true
		if fontUncached, ok := tp.options.FontMap[s2]; ok {
			return fontUncached, nil
		}
		stdFontName, ok := stdFontNames[s2]
		if ok {
			if standard14Font, err := model.NewStandard14Font(stdFontName); err == nil {
				return standard14Font, nil
			}
		}
		if attrPropList := tp.parseAttrPropList(s2); len(attrPropList) > 0 {
			if filePath, ok := attrPropList["path"]; ok {
				fn := model.NewPdfFontFromTTFFile
				if s, ok := attrPropList["type"]; ok && s == "composite" {
					fn = model.NewCompositePdfFontFile
				}
				if fontUncached, err := fn(filePath); err != nil {
					common.Log.Debug("Could not load font `%s`: %v.", filePath, err)
				} else {
					return fontUncached, nil
				}
			}
		}
	}
	if ok {
		return fontRegular, fmt.Errorf("unresolved font %q", s)
	}
	return fontRegular, nil
}

// This method is not supported by Border component and exists solely to satisfy the Drawable interface.
func (bo *border) SetStructureType(structureType model.StructureType) {}

// Height returns the height of the Paragraph. The height is calculated based on the input text and how it is wrapped
// within the container. Does not include Margins.
func (sp *StyledParagraph) Height() float64 {
	sp.wrapText()
	var height float64
	for _, line := range sp.lines {
		var height2 float64
		for _, chunk := range line {
			fontSize := chunk.Style.effectiveLineHeight(sp.lineHeight) * chunk.Style.FontSize
			if fontSize > height2 {
				height2 = fontSize
			}
		}
		height += height2
	}
	return height
}

// HorizontalAlignment represents the horizontal alignment of components
// within a page.
type HorizontalAlignment int

func (teo *TemplateOptions) init() {
	if teo.SubtemplateMap == nil {
		teo.SubtemplateMap = map[string]io.Reader{}
	}
	if teo.FontMap == nil {
		teo.FontMap = map[string]*model.PdfFont{}
	}
	if teo.ImageMap == nil {
		teo.ImageMap = map[string]*model.Image{}
	}
	if teo.ColorMap == nil {
		teo.ColorMap = map[string]Color{}
	}
	if teo.ChartMap == nil {
		teo.ChartMap = map[string]chartrender.ChartRenderable{}
	}
}

// AddHighlightedText adds a new highlighted text to the paragraph.
func (sp *StyledParagraph) AddHighlightedText(text string, color Color, alpha float64) *TextChunk {
	textChunk := NewTextChunk(text, sp.defaultStyle)
	textChunk.Highlight(color, alpha)
	return sp.appendChunk(textChunk)
}

// PageBreak represents a page break for a chapter.
type PageBreak struct{}
