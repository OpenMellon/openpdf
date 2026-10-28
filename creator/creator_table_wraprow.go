// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func (t *Table) wrapRow(i2 int, ctx DrawContext, totalWidth, height float64) (bool, error) {
	if !t.enableRowWrap {
		return false, nil
	}
	row := t.cells[i2].row
	for _, cell := range t.cells {
		if cell.rowSpan > 1 && cell.row <= row && row < cell.row+cell.rowSpan {
			return false, nil
		}
	}
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		if t.cells[i].wrapDisabled {
			return false, nil
		}
	}
	var (
		cell     = t.cells[i2]
		cellsLen = -1
		cells    []*TableCell
		value    float64
		row2     bool
		floats   = make([]float64, 0, len(t.colWidths))
	)
	fn := func(cell *TableCell, content VectorDrawable, ok bool) *TableCell {
		cell2 := *cell
		cell2.content = content
		if ok {
			cell2.row++
		}
		return &cell2
	}
	fn2 := func(n int, content VectorDrawable) {
		var height float64 = -1
		if content == nil {
			if float := floats[n-i2]; float > ctx.Height {
				content = t.cells[n].content
				t.cells[n].content = nil
				floats[n-i2] = 0
				height = float
			}
		}
		fn2 := fn(t.cells[n], content, true)
		cells = append(cells, fn2)
		if height < 0 {
			height = fn2.height(ctx.Width)
		}
		if height > value {
			value = height
		}
	}
	type savedCell struct {
		target *TableCell
		saved  TableCell
	}
	var savedCells []savedCell
	for i := i2; i < len(t.cells) && t.cells[i].row == row; i++ {
		savedCells = append(savedCells, savedCell{t.cells[i], *t.cells[i]})
	}
	subtables := t.splitSubtables(i2, ctx, totalWidth, height)
	for i := i2; i < len(t.cells); i++ {
		cell2 := t.cells[i]
		if cell.row != cell2.row {
			cellsLen = i
			break
		}
		ctx.Width = cell2.width(t.colWidths, totalWidth)
		height2 := cell2.height(ctx.Width)
		var content VectorDrawable
		switch content2 := cell2.content.(type) {
		case *StyledParagraph:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom - 0.5*content2.getTextHeight())
				head, tail, err := content2.split(ctx2)
				if err != nil {
					return false, err
				}
				if head != nil && tail != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
					content = tail
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		case *Division:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Width = ctx.Width - cell2.indent
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Top - content2.margins.Bottom)
				head, tail := content2.split(ctx2, height-content2.margins.Vertical())
				if head != nil && tail != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
					content = tail
					row2 = true
					if head.background != nil {
						head.background.BorderRadiusBottomLeft = 0
						head.background.BorderRadiusBottomRight = 0
					}
					if tail.background != nil {
						tail.background.BorderRadiusTopLeft = 0
						tail.background.BorderRadiusTopRight = 0
					}
					height2 = cell2.height(ctx.Width)
				}
			}
		case *List:
			if height2 > ctx.Height {
				ctx2 := ctx
				ctx2.Width = ctx.Width - cell2.indent
				ctx2.Height = math.Floor(ctx.Height - content2.margins.Vertical())
				head, tail := content2.split(ctx2, height-content2.margins.Vertical())
				if head != nil {
					content2 = head
					cell2 = fn(cell2, head, false)
					t.cells[i] = cell2
				}
				if tail != nil {
					content = tail
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		case *Table:
			if subtable, ok := subtables[i]; ok {
				content2, content3 := subtable[0], subtable[1]
				if content2 != nil {
					cell2 = fn(cell2, content2, false)
				} else {
					cell2 = fn(cell2, nil, false)
				}
				t.cells[i] = cell2
				if content3 != nil {
					content = content3
					row2 = true
				}
				height2 = cell2.height(ctx.Width)
			}
		}
		floats = append(floats, height2)
		if row2 {
			if cells == nil {
				cells = make([]*TableCell, 0, len(t.colWidths))
				for j := i2; j < i; j++ {
					fn2(j, nil)
				}
			}
			fn2(i, content)
		}
	}
	var value2 float64
	for _, float := range floats {
		if float > value2 {
			value2 = float
		}
	}
	if row2 && value2 > ctx.Height {
		for i, savedCell := range savedCells {
			*savedCell.target = savedCell.saved
			t.cells[i2+i] = savedCell.target
		}
		return false, nil
	}
	if row2 {
		if cellsLen < 0 {
			cellsLen = len(t.cells)
		}
		row := t.cells[cellsLen-1].row
		for i := cellsLen; i < len(t.cells); i++ {
			t.cells[i].row++
		}
		t.cells = append(t.cells[:cellsLen], append(cells, t.cells[cellsLen:]...)...)
		t.rowHeights = append(t.rowHeights[:row], append([]float64{value}, t.rowHeights[row:]...)...)
		if len(t.rowHeights) > t.rows {
			t.rows = len(t.rowHeights)
		}
		t.rowHeights[cell.row-1] = value2
	}
	return row2, nil
}

// AddAnnotation adds an annotation to the current block.
// The annotation will be added to the page the block will be rendered on.
func (b *Block) AddAnnotation(annotation *model.PdfAnnotation) {
	if slices.Contains(b.annotations, annotation) {
		return
	}
	b.annotations = append(b.annotations, annotation)
}

// FillOpacity returns the fill opacity of the rectangle (0-1).
func (r *Rectangle) FillOpacity() float64 { return r.fillOpacity }

func templateDict(vals ...any) (map[string]any, error) {
	n := len(vals)
	if n%2 != 0 {
		common.Log.Error("Invalid number of parameters for creating map: %d.", n)
		return nil, core.ErrRangeError
	}
	m := map[string]any{}
	for i := 0; i < n; i += 2 {
		s, ok := vals[i].(string)
		if !ok {
			common.Log.Error("Invalid map key type (%T). Expected string.", vals[i])
			return nil, core.ErrTypeError
		}
		m[s] = vals[i+1]
	}
	return m, nil
}

// InvoiceCellProps holds all style properties for an invoice cell.
type InvoiceCellProps struct {
	TextStyle       TextStyle
	Alignment       CellHorizontalAlignment
	BackgroundColor Color
	BorderColor     Color
	BorderWidth     float64
	BorderSides     []CellBorderSide
}

// ScaleToWidth scale Image to a specified width w, maintaining the aspect ratio.
func (im *Image) ScaleToWidth(w float64) {
	height := im.height / im.width
	im.width = w
	im.height = w * height
}

// Total returns the invoice total description and value cells.
// The returned values can be used to customize the styles of the cells.
func (i *Invoice) Total() (*InvoiceCell, *InvoiceCell) { return i.total[0], i.total[1] }

func (tn *templateNode) classNames() []string {
	for _, attr := range tn.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == classAttrName {
			return strings.Fields(attr.Value)
		}
	}
	return nil
}

func applyShapeColor(block *Block, color model.PdfColor, color2 Color, fn func() Rectangle) error {
	switch colorPatternType1 := color.(type) {
	case *model.PdfColorPatternType1:
		functionBasedShading, ok := color2.(*FunctionBasedShading)
		if !ok {
			return fmt.Errorf("Color is not FunctionBasedShading")
		}
		rect := fn()
		functionBasedShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := functionBasedShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	case *model.PdfColorPatternType2:
		linearShading, ok := color2.(*LinearShading)
		if !ok {
			return fmt.Errorf("Color is not LinearShading")
		}
		rect := fn()
		linearShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := linearShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	case *model.PdfColorPatternType3:
		radialShading, ok := color2.(*RadialShading)
		if !ok {
			return fmt.Errorf("Color is not RadialShading")
		}
		rect := fn()
		radialShading.SetBoundingBox(rect.x, rect.y, rect.width, rect.height)
		name, err := radialShading.AddPatternResource(block)
		if err != nil {
			return fmt.Errorf("failed adding pattern to resources: %v", err)
		}
		colorPatternType1.PatternName = name
	}
	return nil
}

func (gc *GridCell) height(value float64) float64 {
	var bottom float64
	switch content := gc.content.(type) {
	case *StyledParagraph:
		if content.enableWrap {
			content.SetWidth(value - gc.indent - content.margins.Left - content.margins.Right)
		}
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Image:
		content.applyFitMode(value - gc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Table:
		content.updateRowHeights(value - gc.indent - content.margins.Left - content.margins.Right)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *List:
		bottom = content.ctxHeight(value-gc.indent) + content.margins.Top + content.margins.Bottom
	case *Division:
		bottom = content.ctxHeight(value-gc.indent) + content.margins.Top + content.margins.Bottom + content.padding.Top + content.padding.Bottom
	case *Chart:
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Rectangle:
		content.applyFitMode(value - gc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom + content.borderWidth
	case *Ellipse:
		content.applyFitMode(value - gc.indent)
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	case *Line:
		bottom = content.Height() + content.margins.Top + content.margins.Bottom
	}
	return bottom
}

// GetContent returns the cell's content. Returns nil if no content has been
// set.
func (gc *GridCell) GetContent() VectorDrawable { return gc.content }

func copyAnnotations(annotations2 []*model.PdfAnnotation) []*model.PdfAnnotation {
	var annotations []*model.PdfAnnotation
	for _, annotation := range annotations2 {
		annotations = append(annotations, copyAnnotation(annotation))
	}
	return annotations
}

func maxMCID(kDict *model.KDict) int64 {
	maxMCID2 := int64(-1)
	if kDict == nil {
		return maxMCID2
	}
	if val, ok := core.GetIntVal(kDict.K); ok {
		maxMCID2 = int64(val)
	} else if arr, ok := core.GetArray(kDict.K); ok {
		for _, obj := range arr.Elements() {
			if val, ok := core.GetIntVal(obj); ok && int64(val) > maxMCID2 {
				maxMCID2 = int64(val)
			}
		}
	}
	for _, kValue := range kDict.GetChildren() {
		if mcid := kValue.GetMCID(); mcid != nil {
			if maxMCID := int64(*mcid); maxMCID > maxMCID2 {
				maxMCID2 = maxMCID
			}
		}
		if kDict := kValue.GetKDict(); kDict != nil {
			if maxMCID3 := maxMCID(kDict); maxMCID3 > maxMCID2 {
				maxMCID2 = maxMCID3
			}
		}
	}
	return maxMCID2
}

type commands struct {
	names       []string
	paramCounts map[string]int
	moveSymbol  string
	closeSymbol string
}
