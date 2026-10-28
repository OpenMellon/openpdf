// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"reflect"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// NewFunctionBasedGradient creates a function-based (Type 1) gradient color from a
// PostScript calculator program, which maps a point in the shading's domain to
// the colour components at that point. The program's output count selects the
// colourspace: 1 for DeviceGray, 3 for DeviceRGB and 4 for DeviceCMYK.
func (c *Creator) NewFunctionBasedGradient(content string) *FunctionBasedShading {
	return newFunctionBasedShading(content)
}

// SetBoundingBox set gradient color bounding box where the gradient would be rendered.
func (ls *LinearShading) SetBoundingBox(x, y, width, height float64) {
	ls.boundingBox = &model.PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

func (tp *templateProcessor) checkNodeText(node *templateNode, s string, templateText2 *templateText) (bool, error) {
	local := node.start.Name.Local
	if _, ok := textContentTags[local]; ok {
		return true, nil
	}
	if isBlank(s) {
		return false, nil
	}
	if tp.options.Strict {
		return false, tp.nodeError(templateText2.pos(), "tag <%s> does not take text content", local)
	}
	tp.nodeLogDebug(templateText2.pos(), "Tag <%s> does not take text content. Skipping.", local)
	return false, nil
}

// ScaleToHeight sets the graphic svg scaling factor with the given height.
func (gs *GraphicSVG) ScaleToHeight(h float64) {
	width := gs.root.Width / gs.root.Height
	gs.root.Height = h
	gs.root.Width = h * width
	gs.root.SetScaling(width, width)
}

type cmykColor struct{ c, m, y, k float64 }

func isKeycapSequence(s string) bool {
	rs := []rune(s)
	if len(rs) < 2 {
		return false
	}
	if !isKeycapBase(rs[0]) {
		return false
	}
	if len(rs) == 3 && rs[1] == 0xFE0F && isKeycapEnclosing(rs[2]) {
		return true
	}
	if len(rs) == 2 && isKeycapEnclosing(rs[1]) {
		return true
	}
	return false
}

// RenderTemplate draws the parsed template into the creator using the specified
// `data`. The handle comes from ParseTemplate and can be rendered repeatedly,
// with different data, and into different creators. The `data` parameter can be
// nil.
func (c *Creator) RenderTemplate(pt *ParsedTemplate, data any) error {
	return pt.renderInto(c, c, data)
}

// SetBackground sets the background properties of the block. The background is
// drawn behind the block content, covering the block bounds.
func (b *Block) SetBackground(background *Background) { b.background = background }

var chapterHeadingAttrs = map[string]attrApplyFunc{"font": fontAttr(func(para *StyledParagraph, font *model.PdfFont) { para.SetFont(font) }), "font-size": floatAttr(func(para *StyledParagraph, fontSize float64) { para.SetFontSize(fontSize) }), "text-align": enumAttr(textAlignmentValues, func(para *StyledParagraph, align TextAlignment) { para.SetTextAlignment(align) }), "line-height": floatAttr(func(para *StyledParagraph, lineheight float64) { para.SetLineHeight(lineheight) }), "enable-wrap": boolAttr(func(para *StyledParagraph, enableWrap bool) { para.SetEnableWrap(enableWrap) }), "color": colorAttr(func(para *StyledParagraph, color Color) { para.SetFontColor(color) }), "x": floatAttr(func(para *StyledParagraph, x float64) { para.SetPos(x, para.yPos) }), "y": floatAttr(func(para *StyledParagraph, y float64) { para.SetPos(para.xPos, y) }), "angle": floatAttr(func(para *StyledParagraph, angle float64) { para.SetAngle(angle) }), "margin": marginAttr(func(para *StyledParagraph, margins Margins) {
	para.SetMargins(margins.Left, margins.Right, margins.Top, margins.Bottom)
}), "max-lines": intAttr(func(para *StyledParagraph, n int64) { para.SetMaxLines(int(n)) })}

// Style returns the style of the line.
func (l *Line) Style() contentdraw.LineStyle { return l.style }

func newStyledParagraph(defaultStyle TextStyle) *StyledParagraph {
	return &StyledParagraph{chunks: []*TextChunk{}, defaultStyle: defaultStyle, linkStyle: linkTextStyle(defaultStyle.Font), lineHeight: 1.0, alignment: TextAlignmentLeft, enableWrap: true, defaultWrap: true, enableWordWrap: false, angle: 0, unusedA: 1, unusedB: 1, positioning: PositionRelative, languageID: "", artifact: nil, taggedDrawable: taggedDrawable{structType: model.StructureTypeParagraph}}
}

func (tp *templateProcessor) effectiveAttrValue(node *templateNode, s string) (string, bool) {
	var (
		value string
		ok    bool
	)
	for _, s2 := range node.classNames() {
		for _, attr := range tp.classStyles[s2] {
			if attr.Name.Local == s {
				value, ok = attr.Value, true
			}
		}
	}
	for _, attr := range node.start.Attr {
		if attr.Name.Space == "" && attr.Name.Local == s {
			value, ok = attr.Value, true
		}
	}
	return value, ok
}

var PPI float64 = 72

// Width returns the width of the Paragraph.
func (sp *StyledParagraph) Width() float64 {
	if sp.enableWrap && int(sp.width) > 0 {
		return sp.width
	}
	return sp.getTextWidth() / 1000.0
}

func copyLinkAnnotation(annotationLink *model.PdfAnnotationLink) *model.PdfAnnotationLink {
	if annotationLink == nil {
		return nil
	}
	pdfAnnotationLink := model.NewPdfAnnotationLink()
	pdfAnnotationLink.CopyFrom(annotationLink.PdfAnnotation)
	pdfAnnotationLink.BS = annotationLink.BS
	pdfAnnotationLink.A = annotationLink.A
	if action, err := annotationLink.GetAction(); err == nil && action != nil {
		pdfAnnotationLink.SetAction(action)
	}
	if pdfObjectArray, ok := annotationLink.Dest.(*core.PdfObjectArray); ok {
		pdfAnnotationLink.Dest = core.MakeArray(pdfObjectArray.Elements()...)
	}
	return pdfAnnotationLink
}

// SetPageMargins sets the page margins: left, right, top, bottom.
// The default page margins are 10% of document width.
func (c *Creator) SetPageMargins(left, right, top, bottom float64) {
	c.pageMargins.Left = left
	c.pageMargins.Right = right
	c.pageMargins.Top = top
	c.pageMargins.Bottom = bottom
}

type listItem struct {
	content VectorDrawable
	marker  TextChunk
}

// GetSideBorderColor returns the cell's border color for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (tc *TableCell) GetSideBorderColor(side CellBorderSide) Color {
	switch side {
	case CellBorderSideBottom:
		return tc.borderColorBottom
	case CellBorderSideLeft:
		return tc.borderColorLeft
	case CellBorderSideRight:
		return tc.borderColorRight
	default:
		return tc.borderColorTop
	}
}

func (tp *templateProcessor) parseParagraph(node *templateNode, styledParagraph *StyledParagraph) (any, error) {
	if styledParagraph == nil {
		styledParagraph = tp.creator.NewStyledParagraph()
	}
	if err := tp.applyAttrs(node, styledParagraph, chapterHeadingAttrs); err != nil {
		return nil, err
	}
	return styledParagraph, nil
}

// GetRowspan returns the number of rows the cell spans.
func (gc *GridCell) GetRowspan() int { return gc.rowSpan }

func (tp *templateProcessor) setTableHeaderRows(node *templateNode, table *Table, startRow, endRow int) error {
	if startRow != 0 && endRow == 0 {
		endRow = startRow
	} else if startRow == 0 && endRow != 0 {
		startRow = 1
	}
	table.headerStartRow, table.headerEndRow = 0, 0
	if startRow == 0 {
		return nil
	}
	if err := table.SetHeaderRows(startRow, endRow); err != nil {
		if tp.options.Strict {
			return tp.nodeError(node, "invalid table header rows: %v", err)
		}
		tp.nodeLogDebug(node, "Could not set table header rows: %v.", err)
	}
	return nil
}

func (fbs *FunctionBasedShading) shadingModel() (*model.PdfShadingType1, error) {
	pdfFunction, colorSpace, err := fbs.pdfFunction()
	if err != nil {
		return nil, err
	}
	pdfShadingType1 := model.NewPdfShadingType1()
	pdfShadingType1.ShadingType = core.MakeInteger(1)
	pdfShadingType1.ColorSpace = colorSpace
	pdfShadingType1.AntiAlias = core.MakeBool(fbs.base.antiAlias)
	pdfShadingType1.Domain = core.MakeArrayFromFloats(functionDomain)
	pdfShadingType1.Matrix = core.MakeArrayFromFloats([]float64{fbs.boundingBox.Width(), 0, 0, fbs.boundingBox.Height(), fbs.boundingBox.Llx, fbs.boundingBox.Lly})
	pdfShadingType1.Function = []model.PdfFunction{pdfFunction}
	return pdfShadingType1, nil
}

// GetPositioning returns the positioning mode (relative or absolute) of the
// grid.
func (g *Grid) GetPositioning() Positioning { return g.positioning }

func intAttr(i any) attrApplyFunc {
	value2 := validateSetter("intAttr", i, 0, int64Type)
	return func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error {
		i, err := parseInt64(value)
		if err != nil {
			if err2 := proc.strictErr(node, attrName, value, err); err2 != nil {
				return err2
			}
			i = 0
		}
		callSetter(value2, target, reflect.ValueOf(i))
		return nil
	}
}

// GeneratePageBlocks generates the page blocks. Multiple blocks are generated
// if the contents wrap over multiple pages. Implements the Drawable interface.
func (sp *StyledParagraph) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	var blocks []*Block
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	if sp.positioning.IsRelative() {
		ctx.X += sp.margins.Left
		ctx.Y += sp.margins.Top
		ctx.Width -= sp.margins.Left + sp.margins.Right
		ctx.Height -= sp.margins.Top
		sp.SetWidth(ctx.Width)
	} else {
		if int(sp.width) <= 0 {
			sp.SetWidth(sp.getTextWidth() / 1000.0)
		}
		ctx.X = sp.xPos
		ctx.Y = sp.yPos
	}
	if sp.beforeRender != nil {
		sp.beforeRender(sp, ctx)
	}
	if err := sp.wrapText(); err != nil {
		return nil, ctx, err
	}
	lines := sp.lines
	n := 0
	for {
		paragraphChunks, lines2, err := drawParagraphChunks(block, sp, lines, ctx)
		if err != nil {
			common.Log.Debug("ERROR: %v", err)
			return nil, ctx, err
		}
		ctx = paragraphChunks
		blocks = append(blocks, block)
		if lines = lines2; len(lines2) == 0 {
			break
		}
		if len(lines2) == n {
			return nil, ctx, errors.New("not enough space for paragraph")
		}
		sp.dropCaps = nil
		block = NewBlock(ctx.PageWidth, ctx.PageHeight)
		ctx.Page++
		paragraphChunks = ctx
		paragraphChunks.Y = ctx.Margins.Top
		paragraphChunks.X = ctx.Margins.Left + sp.margins.Left
		paragraphChunks.Height = ctx.PageHeight - ctx.Margins.Top - ctx.Margins.Bottom
		paragraphChunks.Width = ctx.PageWidth - ctx.Margins.Left - ctx.Margins.Right - sp.margins.Left - sp.margins.Right
		ctx = paragraphChunks
		n = len(lines2)
	}
	if sp.positioning.IsRelative() {
		ctx.Y += sp.margins.Bottom
		ctx.Height -= sp.margins.Bottom
		if !ctx.Inline {
			ctx.X = ctx2.X
			ctx.Width = ctx2.Width
		}
		return blocks, ctx, nil
	}
	return blocks, ctx2, nil
}

func (tp *templateProcessor) parseAttrPropList(s string) map[string]string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	m := map[string]string{}
	for _, field := range fields {
		stringSubmatch := attrFuncRe.FindStringSubmatch(field)
		if len(stringSubmatch) < 3 {
			continue
		}
		s, s2 := strings.TrimSpace(stringSubmatch[1]), stringSubmatch[2]
		if s == "" {
			continue
		}
		m[s] = s2
	}
	return m
}
