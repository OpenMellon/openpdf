// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"image"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func cropImage(img *model.Image, rect2 image.Rectangle) (*model.Image, error) {
	rect := image.Rect(0, 0, int(img.Width), int(img.Height))
	rect3 := rect2.Intersect(rect)
	switch {
	case rect3.Empty():
		return model.ImageHandling.NewImageFromGoImage(&image.RGBA{})
	case img.BitsPerComponent == 8 && len(img.Decode()) == 0 && (img.ColorComponents == 1 || img.ColorComponents == 3) && len(img.Data) >= int(img.Width)*int(img.Height)*img.ColorComponents:
		return cropImageFast(img, rect2, rect3), nil
	default:
		return cropImageGeneric(img, rect2, rect3)
	}
}

// SetRowWrapDisabled excludes a row from row wrapping: when the table has row
// wrapping enabled (see EnableRowWrap), a disabled row moves to the next page
// whole instead of splitting at the page boundary. Splittable content
// (styled paragraphs, divisions, lists) taller than a full page is clipped;
// other content taller than a full page is not rendered. Applies to the cells
// currently in the row, so call it after the row's cells have been added.
func (t *Table) SetRowWrapDisabled(row int, disabled bool) error {
	if row < 1 || row > t.rows {
		return errors.New("range check error")
	}
	for _, cell := range t.cells {
		if cell.row == row {
			cell.wrapDisabled = disabled
		}
	}
	return nil
}

// BorderWidth returns the border width of the ellipse.
func (e *Ellipse) BorderWidth() float64 { return e.borderWidth }

// SetPos set position of the element on PDF page
func (gse *GraphicSVGElement) SetPos(x, y float64) { gse.xPos = x; gse.yPos = y }

// DrawWithContext draws the Block using the specified drawing context.
func (b *Block) DrawWithContext(d Drawable, ctx DrawContext) error {
	pageBlocks, _, err := d.GeneratePageBlocks(ctx)
	if err != nil {
		return err
	}
	if len(pageBlocks) != 1 {
		return ErrContentNotFit
	}
	for _, pageBlock := range pageBlocks {
		if err := b.mergeBlocks(pageBlock); err != nil {
			return err
		}
	}
	return nil
}

func (t *Table) splitSpanningCellsAtBreaks(ctx DrawContext, fn func(VectorDrawable, float64, float64, float64) (VectorDrawable, VectorDrawable)) {
	contentHeight := ctx.contentHeight()
	headerHeight := t.headerHeight()
	headerEndRow := 1
	if t.hasHeaderRows {
		headerEndRow = t.headerEndRow + 1
	}
	height := ctx.Height - headerHeight
	contentHeight2 := contentHeight - headerHeight
	if height <= 0 {
		height = contentHeight2
	}
	if contentHeight2 <= 0 {
		return
	}
	value := height
	value2 := 0.0
	m := make(map[int]bool)
	for i := headerEndRow; i <= t.rows && i-1 < len(t.rowHeights); i++ {
		rowHeight := t.rowHeights[i-1]
		if value2 > 0 && value2+rowHeight > value {
			m[i] = true
			value = contentHeight2
			value2 = 0
		}
		value2 += rowHeight
	}
	if len(m) == 0 {
		return
	}
	for i := 0; i < len(t.cells); i++ {
		cell := t.cells[i]
		if cell.rowSpan < 2 || cell.row < headerEndRow {
			continue
		}
		row := cell.row
		n := cell.row + cell.rowSpan - 1
		row2 := -1
		for i := row + 1; i <= n; i++ {
			if m[i] {
				row2 = i
				break
			}
		}
		if row2 < 0 {
			continue
		}
		rowHeight := 0.0
		for i := row; i < row2 && i-1 < len(t.rowHeights); i++ {
			rowHeight += t.rowHeights[i-1]
		}
		width := cell.width(t.colWidths, ctx.Width)
		var content VectorDrawable
		if content2, content3 := fn(cell.content, width, cell.indent, rowHeight); content2 == nil {
			content = cell.content
			cell.content = nil
		} else if content3 != nil {
			cell.content = content2
			content = content3
		}
		props := cell.cloneProps(content)
		props.row = row2
		props.col = cell.col
		props.rowSpan = n - row2 + 1
		cell.rowSpan = row2 - row
		t.cells = append(t.cells, props)
	}
}

const (
	CellHorizontalAlignmentLeft CellHorizontalAlignment = iota
	CellHorizontalAlignmentCenter
	CellHorizontalAlignmentRight
)

func beginTaggedContent(block *Block, structureTagInfo *model.StructureTagInfo) {
	contentCreator := contentstream.NewContentCreator()
	m := map[string]core.PdfObject{}
	if structureTagInfo != nil {
		m["MCID"] = core.MakeInteger(structureTagInfo.Mcid)
	}
	contentCreator.Add_BDC(*core.MakeName(string(structureTagInfo.StructureType)), m)
	block.addContents(contentCreator.Operations())
}

func parseListItemTag(proc *templateProcessor, node *templateNode) (any, error) {
	return proc.parseListItem(node)
}

func splitDrawableToFit(drawable VectorDrawable, ctx DrawContext) VectorDrawable {
	switch para := drawable.(type) {
	case *StyledParagraph:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Top - para.margins.Bottom - 0.5*para.getTextHeight())
		if ctx2.Height <= 0 {
			return nil
		}
		head, _, err := para.split(ctx2)
		if err != nil || head == nil {
			return nil
		}
		return head
	case *Division:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Top - para.margins.Bottom)
		if ctx2.Height <= 0 {
			return nil
		}
		head, _ := para.split(ctx2, 0)
		if head == nil {
			return nil
		}
		return head
	case *List:
		ctx2 := ctx
		ctx2.Height = math.Floor(ctx.Height - para.margins.Vertical())
		if ctx2.Height <= 0 {
			return nil
		}
		head, _ := para.split(ctx2, 0)
		if head == nil {
			return nil
		}
		return head
	case *Table:
		head, _ := para.split(ctx, 0)
		if head == nil {
			return nil
		}
		return head
	}
	return nil
}

// Height returns the height of the graphic svg.
func (gs *GraphicSVG) Height() float64 { return gs.root.Height }

// SetBackground sets the background properties of the component.
func (d *Division) SetBackground(background *Background) { d.background = background }

// Vertical returns total vertical (top + bottom) margin.
func (m *Margins) Vertical() float64 { return m.Bottom + m.Top }

// SetOpacity sets opacity for Image.
func (im *Image) SetOpacity(opacity float64) { im.opacity = opacity }

func templateExtendDict(templateExtendDict map[string]any, vals ...any) (map[string]any, error) {
	n := len(vals)
	if n%2 != 0 {
		return nil, core.ErrRangeError
	}
	for i := 0; i < n; i += 2 {
		s, ok := vals[i].(string)
		if !ok {
			return nil, core.ErrTypeError
		}
		templateExtendDict[s] = vals[i+1]
	}
	return templateExtendDict, nil
}

const (
	PositionRelative Positioning = iota
	PositionAbsolute
)

func (gse *GraphicSVGElement) drawLine(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	svgLength, err := parseSVGLength(gse.Attributes["x1"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cx` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["y1"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cy` value: %v", err.Error())
	}
	svgLength3, err := parseSVGLength(gse.Attributes["x2"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `rx` value: %v", err.Error())
	}
	svgLength4, err := parseSVGLength(gse.Attributes["y2"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `ry` value: %v", err.Error())
	}
	cc.Add_m(svgLength*gse.scale, svgLength2*gse.scale)
	cc.Add_l(svgLength3*gse.scale, svgLength4*gse.scale)
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

// GeneratePageBlocks draw graphic svg into block.
func (gs *GraphicSVG) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	isRelative := gs.positioning.IsRelative()
	var blocks []*Block
	if isRelative {
		value := 1.0
		top := gs.margins.Top
		if gs.root.Height > ctx.Height-gs.margins.Top {
			blocks = []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y)}
			var err error
			if _, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			top = 0
		}
		ctx.X += gs.margins.Left + value
		ctx.Y += top
		ctx.Width -= gs.margins.Left + gs.margins.Right + 2*value
		ctx.Height -= top
	} else {
		ctx.X = gs.xPos
		ctx.Y = gs.yPos
	}
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Translate(0, ctx.PageHeight)
	contentCreator.Scale(1, -1)
	contentCreator.Translate(ctx.X, ctx.Y)
	width := gs.root.Width / gs.root.ViewBox.W
	height := gs.root.Height / gs.root.ViewBox.H
	xPos := 0.0
	yPos := 0.0
	if isRelative {
		xPos = gs.xPos - (gs.root.ViewBox.X * math.Max(width, height))
		yPos = gs.yPos - (gs.root.ViewBox.Y * math.Max(width, height))
	}
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if gs.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(gs.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(gs.tagInfo.Mcid)})
	}
	gs.root.SetPos(ctx.X, ctx.Y)
	gs.root.ToContentCreator(contentCreator, block.resources, width, height, xPos, yPos)
	if gs.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	operations := contentCreator.Operations()
	operations.WrapIfNeeded()
	block.addWrappedContents(operations)
	if isRelative {
		bottom := gs.Height() + gs.margins.Bottom
		ctx.Y += bottom
		ctx.Height -= bottom
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

// AddressStyle returns the style properties used to render the content of
// the invoice address sections.
func (i *Invoice) AddressStyle() TextStyle { return i.addressStyle }

func copyHighlightAnnotation(annotationHighlight *model.PdfAnnotationHighlight) *model.PdfAnnotationHighlight {
	if annotationHighlight == nil {
		return nil
	}
	pdfAnnotationHighlight := model.NewPdfAnnotationHighlight()
	pdfAnnotationHighlight.CopyFrom(annotationHighlight.PdfAnnotation)
	pdfAnnotationHighlight.CA = annotationHighlight.CA
	pdfAnnotationHighlight.QuadPoints = annotationHighlight.QuadPoints
	return pdfAnnotationHighlight
}

func (gc *GridCell) width(colWidths []float64, totalWidth float64) float64 {
	width := float64(0.0)
	for i := 0; i < gc.colSpan; i++ {
		width += colWidths[gc.col+i]
	}
	return width * totalWidth
}

// CellBorderSide defines the table cell's border side.
type CellBorderSide int

// GeneratePageBlocks generate the Page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages.
func (i *Invoice) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	fns := []func(ctx DrawContext) ([]*Block, DrawContext, error){i.generateHeaderBlocks, i.generateInformationBlocks, i.generateLineBlocks, i.generateTotalBlocks, i.generateNoteBlocks}
	var blocks []*Block
	for _, fn := range fns {
		fn2, ctx2, err := fn(ctx)
		if err != nil {
			return blocks, ctx, err
		}
		if len(blocks) == 0 {
			blocks = fn2
		} else if len(fn2) > 0 {
			blocks[len(blocks)-1].mergeBlocks(fn2[0])
			blocks = append(blocks, fn2[1:]...)
		}
		ctx = ctx2
	}
	if i.positioning.IsRelative() {
		ctx.X = ctx2.X
	}
	if i.positioning.IsAbsolute() {
		return blocks, ctx2, nil
	}
	return blocks, ctx, nil
}

// GetContent returns the cell's content. Returns nil if no content has been
// set.
func (tc *TableCell) GetContent() VectorDrawable { return tc.content }
