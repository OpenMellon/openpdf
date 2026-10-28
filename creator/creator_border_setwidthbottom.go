// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/model"
)

// SetWidthBottom sets border width for bottom.
func (bo *border) SetWidthBottom(bw float64) { bo.widthBottom = bw }

// AddLine appends a new line to the invoice line items table.
func (i *Invoice) AddLine(values ...string) []*InvoiceCell {
	columnsLen := len(i.columns)
	var invoiceCells []*InvoiceCell
	for j, value := range values {
		cell := i.newCell(value, i.lineProps)
		if j < columnsLen {
			cell.Alignment = i.columns[j].Alignment
		}
		invoiceCells = append(invoiceCells, cell)
	}
	i.lines = append(i.lines, invoiceCells)
	return invoiceCells
}

// SetLanguage sets the language identifier that will be stored inside document catalog.
func (c *Creator) SetLanguage(language string) { c.language = language }

// GetMargins returns the margins of the rectangle: left, right, top, bottom.
func (r *Rectangle) GetMargins() (float64, float64, float64, float64) {
	return r.margins.Left, r.margins.Right, r.margins.Top, r.margins.Bottom
}

var tableAttrs = map[string]attrApplyFunc{"columns": nil, "column-widths": floatArrayAttrErr(func(table *Table, widths []float64) error { return table.SetColumnWidths(widths...) }), "margin": marginAttr(func(table *Table, margins Margins) {
	table.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "x": floatAttr(func(table *Table, x float64) { table.SetPos(x, table.yPos) }), "y": floatAttr(func(table *Table, y float64) { table.SetPos(table.xPos, y) }), "header-start-row": intAttr(func(table *Table, n int64) { table.headerStartRow = int(n) }), "header-end-row": intAttr(func(table *Table, n int64) { table.headerEndRow = int(n) }), "enable-row-wrap": boolAttr(func(table *Table, enable bool) { table.EnableRowWrap(enable) }), "enable-page-wrap": boolAttr(func(table *Table, enable bool) { table.EnablePageWrap(enable) })}

func (gse *GraphicSVGElement) toContentStream(creator *contentstream.ContentCreator, resources *model.PdfPageResources) {
	svgStyle, err := parseSVGStyle(gse.Attributes, gse.scale)
	if err != nil {
		common.Log.Debug("Unable to parse style attribute: %v", err)
	}
	gse.Style = svgStyle
	switch gse.Name {
	case "path":
		gse.drawPath(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "rect":
		gse.drawRect(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "circle":
		gse.drawCircle(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "ellipse":
		gse.drawEllipse(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "polyline":
		gse.drawPolyline(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "polygon":
		gse.drawPolygon(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "line":
		gse.drawLine(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "text":
		gse.drawText(creator, resources)
		for _, graphicSVGElement := range gse.Children {
			graphicSVGElement.toContentStream(creator, resources)
		}
	case "g":
		attribute, ok := gse.Attributes["fill"]
		attribute2, ok2 := gse.Attributes["stroke"]
		attribute3, ok3 := gse.Attributes["stroke-width"]
		attribute4, ok4 := gse.Attributes["transform"]
		for _, graphicSVGElement := range gse.Children {
			if _, ok2 := graphicSVGElement.Attributes["fill"]; !ok2 && ok {
				graphicSVGElement.Attributes["fill"] = attribute
			}
			if _, ok := graphicSVGElement.Attributes["stroke"]; !ok && ok2 {
				graphicSVGElement.Attributes["stroke"] = attribute2
			}
			if _, ok := graphicSVGElement.Attributes["stroke-width"]; !ok && ok3 {
				graphicSVGElement.Attributes["stroke-width"] = attribute3
			}
			if _, ok := graphicSVGElement.Attributes["transform"]; !ok && ok4 {
				graphicSVGElement.Attributes["transform"] = attribute4
			}
			graphicSVGElement.toContentStream(creator, resources)
		}
	}
}

// TextRenderingMode determines whether showing text shall cause glyph
// outlines to be stroked, filled, used as a clipping boundary, or some
// combination of the three.
// See section 9.3 "Text State Parameters and Operators" and
// Table 106 (pp. 254-255 PDF32000_2008).
type TextRenderingMode int

func (tl *TOCLine) getLineLink() *model.PdfAnnotation {
	if tl.linkPage <= 0 {
		return nil
	}
	return newInternalLinkAnnotation(tl.linkPage-1, tl.linkX, tl.linkY, 0, "")
}

// SetColPosition sets cell column position.
func (tc *TableCell) SetColPosition(col int) { tc.col = col }

func (tp *templateProcessor) parsePageBreak(node *templateNode) (any, error) {
	pageBreak := newPageBreak()
	if err := tp.applyAttrs(node, pageBreak, emptyAttrs); err != nil {
		return nil, err
	}
	return pageBreak, nil
}

func (sp *StyledParagraph) getLineMetrics(n int) (maxCapHeight, maxFontSize, minDescent float64) {
	if sp.lines == nil || (sp.lines != nil && len(sp.lines) == 0) {
		sp.wrapText()
	}
	if n < 0 || n > len(sp.lines)-1 {
		common.Log.Debug("ERROR: invalid paragraph line index %d. Returning 0, 0", n)
		return 0, 0, 0
	}
	return chunksFontMetrics(sp.lines[n])
}

func (li *List) itemWidth(width, markerWidth float64) float64 {
	indent := width - li.margins.Horizontal() - li.indent
	return indent * (1 - markerWidth/width)
}

func (tp *templateProcessor) validateFontValue(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	ok := false
	for s2 := range strings.SplitSeq(s, ",") {
		s2 = strings.TrimSpace(s2)
		if s2 == "" {
			continue
		}
		ok = true
		if _, ok := tp.options.FontMap[s2]; ok {
			return nil
		}
		if _, ok := stdFontNames[s2]; ok {
			return nil
		}
		if attrPropList := tp.parseAttrPropList(s2); len(attrPropList) > 0 {
			if _, ok := attrPropList["path"]; ok {
				return nil
			}
		}
	}
	if ok {
		return fmt.Errorf("unresolved font %q", s)
	}
	return nil
}

// SetMargins sets the margins of the graphic svg component.
func (gs *GraphicSVG) SetMargins(left, right, top, bottom float64) {
	gs.margins.Left = left
	gs.margins.Right = right
	gs.margins.Top = top
	gs.margins.Bottom = bottom
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (rs *RadialShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }

// GetCells returns the cells of the table. The returned slice references the
// table's internal cells; modifying its elements modifies the table.
func (t *Table) GetCells() []*TableCell { return t.cells }

// GetHeaderRows returns the inclusive start and end rows of the table header.
// The returned values are only meaningful when HasHeader returns true.
func (t *Table) GetHeaderRows() (startRow, endRow int) { return t.headerStartRow, t.headerEndRow }
