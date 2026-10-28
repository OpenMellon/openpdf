// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"errors"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	chartintegration "github.com/OpenMellon/openpdf/internal/integrations/unichart"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

func (e *Ellipse) applyFitMode(left float64) {
	left -= e.margins.Left + e.margins.Right
	switch e.fitMode {
	case FitModeFillWidth:
		e.ScaleToWidth(left)
	}
}

// SetWidth set the Image's document width to specified w. This does not change the raw image data, i.e.
// no actual scaling of data is performed. That is handled by the PDF viewer.
func (im *Image) SetWidth(w float64) { im.width = w }

// GeneratePageBlocks draws the chart onto a block.
func (cha *Chart) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	ctx2 := ctx
	isRelative := cha.positioning.IsRelative()
	var blocks []*Block
	if isRelative {
		value := 1.0
		top := cha.margins.Top
		if float64(cha.chart.Height()) > ctx.Height-cha.margins.Top {
			blocks = []*Block{NewBlock(ctx.PageWidth, ctx.PageHeight-ctx.Y)}
			var err error
			if _, ctx, err = newPageBreak().GeneratePageBlocks(ctx); err != nil {
				return nil, ctx, err
			}
			top = 0
		}
		ctx.X += cha.margins.Left + value
		ctx.Y += top
		ctx.Width -= cha.margins.Left + cha.margins.Right + 2*value
		ctx.Height -= top
		cha.chart.SetWidth(int(ctx.Width))
	} else {
		ctx.X = cha.xPos
		ctx.Y = cha.yPos
	}
	contentCreator := contentstream.NewContentCreator()
	if cha.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(cha.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(cha.tagInfo.Mcid)})
	}
	contentCreator.Translate(0, ctx.PageHeight)
	contentCreator.Scale(1, -1)
	contentCreator.Translate(ctx.X, ctx.Y)
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	cha.chart.Render(chartintegration.NewRenderer(contentCreator, block.resources), nil)
	if cha.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	if err := block.addContentsByString(contentCreator.String()); err != nil {
		return nil, ctx, err
	}
	if isRelative {
		bottom := cha.Height() + cha.margins.Bottom
		ctx.Y += bottom
		ctx.Height -= bottom
	} else {
		ctx = ctx2
	}
	blocks = append(blocks, block)
	return blocks, ctx, nil
}

var ErrContentNotFit = errors.New("cannot fit content into an existing space")

// NewBlock creates a new Block with specified width and height.
func NewBlock(width float64, height float64) *Block {
	block := &Block{}
	block.contents = &contentstream.ContentStreamOperations{}
	block.resources = model.NewPdfPageResources()
	block.width = width
	block.height = height
	return block
}

// SetTextOverflow controls the behavior of paragraph text which
// does not fit in the available space.
func (sp *StyledParagraph) SetTextOverflow(textOverflow TextOverflow) {
	sp.textOverflow = textOverflow
}

type pageTransformations struct {
	matrix *transform.Matrix
	flipH  bool
	flipV  bool
}

// GeneratePageBlocks draws the composite Bezier curve on a new block
// representing the page. Implements the Drawable interface.
func (pbc *PolyBezierCurve) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(pbc.fillOpacity, pbc.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	curve := pbc.curve
	curve.FillEnabled = curve.FillColor != nil
	var (
		pageHeight        = ctx.PageHeight
		curves            = curve.Curves
		cubicBezierCurves = make([]contentdraw.CubicBezierCurve, 0, len(curve.Curves))
	)
	rect := model.PdfRectangle{}
	for i := range curve.Curves {
		curve := curves[i]
		curve.P0.Y = pageHeight - curve.P0.Y
		curve.P1.Y = pageHeight - curve.P1.Y
		curve.P2.Y = pageHeight - curve.P2.Y
		curve.P3.Y = pageHeight - curve.P3.Y
		cubicBezierCurves = append(cubicBezierCurves, curve)
		bounds := curve.GetBounds()
		if i == 0 {
			rect = bounds
		} else {
			rect.Llx = math.Min(rect.Llx, bounds.Llx)
			rect.Lly = math.Min(rect.Lly, bounds.Lly)
			rect.Urx = math.Max(rect.Urx, bounds.Urx)
			rect.Ury = math.Max(rect.Ury, bounds.Ury)
		}
	}
	curve.Curves = cubicBezierCurves
	defer func() { curve.Curves = curves }()
	if curve.FillEnabled {
		err := applyShapeColor(block, pbc.curve.FillColor, pbc.fillColor, func() Rectangle {
			return Rectangle{x: rect.Llx, y: rect.Lly, width: rect.Width(), height: rect.Height()}
		})
		if err != nil {
			return nil, ctx, err
		}
	}
	markedDraw, _, err := curve.MarkedDraw(gsName, pbc.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// GetSideBorderWidth returns the cell's border width for the specified side.
// Use CellBorderSideTop/Bottom/Left/Right to read individual sides. Passing
// CellBorderSideAll returns the top side's value only; it is not a valid
// round-trip key — use individual side constants to verify per-side state.
func (gc *GridCell) GetSideBorderWidth(side CellBorderSide) float64 {
	switch side {
	case CellBorderSideBottom:
		return gc.borderWidthBottom
	case CellBorderSideLeft:
		return gc.borderWidthLeft
	case CellBorderSideRight:
		return gc.borderWidthRight
	default:
		return gc.borderWidthTop
	}
}

func (sp *StyledParagraph) split(ctx DrawContext) (head, tail *StyledParagraph, err error) {
	if err = sp.wrapChunks(false); err != nil {
		return nil, nil, err
	}
	if len(sp.lines) == 1 && sp.lineHeight > ctx.Height {
		return sp, nil, nil
	}
	fn := func(chunks []*TextChunk, chunks2 []*TextChunk) []*TextChunk {
		if len(chunks2) == 0 {
			return chunks
		}
		chunksLen := len(chunks)
		if chunksLen == 0 {
			return append(chunks, chunks2...)
		}
		if chunks[chunksLen-1].Style == chunks2[0].Style {
			chunks[chunksLen-1].Text += chunks2[0].Text
		} else {
			chunks = append(chunks, chunks2[0])
		}
		return append(chunks, chunks2[1:]...)
	}
	fn2 := func(para *StyledParagraph, chunks []*TextChunk) *StyledParagraph {
		if len(chunks) == 0 {
			return nil
		}
		para2 := *para
		para2.chunks = chunks
		return &para2
	}
	var (
		value   float64
		chunks  []*TextChunk
		chunks2 []*TextChunk
	)
	for _, line := range sp.lines {
		var value2 float64
		chunks3 := make([]*TextChunk, 0, len(line))
		for _, chunk := range line {
			if fontSize := chunk.Style.FontSize * chunk.Style.effectiveLineHeight(sp.lineHeight); fontSize > value2 {
				value2 = fontSize
			}
			chunks3 = append(chunks3, chunk.clone())
		}
		if sp.positioning.IsRelative() {
			if value+value2 >= ctx.Height {
				chunks2 = fn(chunks2, chunks3)
			} else {
				chunks = fn(chunks, chunks3)
			}
		}
		value += value2
	}
	sp.lines = nil
	if len(chunks2) == 0 {
		return sp, nil, nil
	}
	return fn2(sp, chunks), fn2(sp, chunks2), nil
}

// SetIndent sets the cell's left indent.
func (tc *TableCell) SetIndent(indent float64) { tc.indent = indent }

func (gse *GraphicSVGElement) drawEllipse(creator *contentstream.ContentCreator, resources *model.PdfPageResources) {
	creator.Add_q()
	gse.Style.toContentStream(creator, resources, gse)
	svgLength, err := parseSVGLength(gse.Attributes["cx"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cx` value: %v", err.Error())
	}
	svgLength2, err := parseSVGLength(gse.Attributes["cy"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `cy` value: %v", err.Error())
	}
	svgLength3, err := parseSVGLength(gse.Attributes["rx"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `rx` value: %v", err.Error())
	}
	svgLength4, err := parseSVGLength(gse.Attributes["ry"], 64)
	if err != nil {
		common.Log.Debug("Error while parsing `ry` value: %v", err.Error())
	}
	x2 := svgLength3 * gse.scale
	y2 := svgLength4 * gse.scale
	offX := svgLength * gse.scale
	offY := svgLength2 * gse.scale
	x := x2 * bezierCircleKappa
	y := y2 * bezierCircleKappa
	cubicBezierPath := contentdraw.NewCubicBezierPath()
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(-x2, 0, -x2, y, -x, y2, 0, y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, y2, x, y2, x2, y, x2, 0))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(x2, 0, x2, -y, x, -y2, 0, -y2))
	cubicBezierPath = cubicBezierPath.AppendCurve(contentdraw.NewCubicBezierCurve(0, -y2, -x, -y2, -x2, -y, -x2, 0))
	cubicBezierPath = cubicBezierPath.Offset(offX, offY)
	if gse.Style.StrokeWidth > 0 {
		cubicBezierPath = cubicBezierPath.Offset(gse.Style.StrokeWidth/2, gse.Style.StrokeWidth/2)
	}
	contentdraw.DrawBezierPathWithCreator(cubicBezierPath, creator)
	gse.Style.fillStroke(creator)
	creator.Add_h()
	creator.Add_Q()
}

func collapseWhitespace(s string, ok, ok2 bool) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	if ok {
		if s2 := strings.TrimLeft(s, whitespaceChars); strings.Contains(s[:len(s)-len(s2)], "\n") {
			s = s2
		}
	}
	if ok2 {
		if s2 := strings.TrimRight(s, whitespaceChars); strings.Contains(s[len(s2):], "\n") {
			s = s2
		}
	}
	return newlineWhitespaceRe.ReplaceAllString(s, " ")
}

// SetPdfWriterAccessFunc sets a PdfWriter access function/hook.
// Exposes the PdfWriter just prior to writing the PDF.  Can be used to encrypt the output PDF, etc.
//
// Example of encrypting with a user/owner password "password"
// Prior to calling c.WriteFile():
//
//	c.SetPdfWriterAccessFunc(func(w *model.PdfWriter) error {
//		userPass := []byte("password")
//		ownerPass := []byte("password")
//		err := w.Encrypt(userPass, ownerPass, nil)
//		return err
//	})
func (c *Creator) SetPdfWriterAccessFunc(pdfWriterAccessFunc func(writer *model.PdfWriter) error) {
	c.pdfWriterAccessFunc = pdfWriterAccessFunc
}

// SetExtends specifies whether ot extend the shading beyond the starting and ending points.
//
// Text extends is set to `[]bool{false, false}` by default.
func (ls *LinearShading) SetExtends(start bool, end bool) { ls.base.SetExtends(start, end) }

// ColorCMYKFrom8bit creates a Color from c,m,y,k values (0-100).
// Example:
//
//	red := ColorCMYKFrom8Bit(0, 100, 100, 0)
func ColorCMYKFrom8bit(c, m, y, k byte) Color {
	return cmykColor{c: math.Min(float64(c), 100) / 100.0, m: math.Min(float64(m), 100) / 100.0, y: math.Min(float64(y), 100) / 100.0, k: math.Min(float64(k), 100) / 100.0}
}

// SetWidth sets the width of the rectangle.
func (r *Rectangle) SetWidth(width float64) { r.width = width }

// RemoveArtifact removes the artifact associated with the image, if any.
func (im *Image) RemoveArtifact() { im.artifact = nil }

func endMarkedContent(block *Block) {
	contentCreator := contentstream.NewContentCreator()
	contentCreator.Add_EMC()
	block.addContents(contentCreator.Operations())
}

// EnableTextShaping enables or disables advanced text shaping for the paragraph.
//
// This only has effect if the fonts used in the paragraph support text shaping (OTF fonts).
// Advanced text shaping is useful for rendering complex scripts, ligatures,
// and proper character positioning.
//
// By default, text shaping is disabled.
func (sp *StyledParagraph) EnableTextShaping(enable bool) { sp.enableShaping = enable }
