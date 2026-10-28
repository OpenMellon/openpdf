// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package creator

import (
	"fmt"

	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/typesetting/shaping"
)

// paragraphDrawer holds the state needed to render the lines of a styled
// paragraph that fit on the current page into a block.
type paragraphDrawer struct {
	block *Block
	para  *StyledParagraph
	ctx   DrawContext
	cc    *contentstream.ContentCreator

	// lines are the lines drawn on this page; rest are the lines that did not fit.
	lines [][]*TextChunk
	rest  [][]*TextChunk

	fontName   core.PdfObjectName
	fontSize   float64
	nextFont   int
	isRelative bool

	// fontNames and shaped are indexed by line and then by chunk.
	fontNames [][]core.PdfObjectName
	shaped    [][]shaping.Line
	advances  []float64
	height    float64

	firstLineFontSize float64

	dropCap *TextChunk
	dropGap float64

	// baseY is the baseline of the first line, lineY the baseline of the current line.
	baseY float64
	lineY float64

	colors     map[Color]model.PdfColor
	colorSpans map[Color]*[2]float64
	underlines []*contentdraw.BasicLine
	openedProp bool
}

func drawParagraphChunks(block *Block, para *StyledParagraph, rows4 [][]*TextChunk, ctx DrawContext) (DrawContext, [][]*TextChunk, error) {
	d := &paragraphDrawer{
		block:      block,
		para:       para,
		ctx:        ctx,
		lines:      rows4,
		fontSize:   para.defaultStyle.FontSize,
		isRelative: para.positioning.IsRelative(),
		colors:     map[Color]model.PdfColor{},
		colorSpans: map[Color]*[2]float64{},
	}
	return d.draw()
}

func (d *paragraphDrawer) draw() (DrawContext, [][]*TextChunk, error) {
	para := d.para
	if err := d.registerDefaultFont(); err != nil {
		return d.ctx, nil, err
	}
	if err := d.measureLines(); err != nil {
		return d.ctx, nil, err
	}
	var capHeight, minDescent float64
	if len(d.lines) > 0 {
		capHeight, d.firstLineFontSize, minDescent = chunksFontMetrics(d.lines[0])
	}
	var firstAdvance float64
	if len(d.advances) > 0 {
		firstAdvance = d.advances[0]
	}
	lineHeight := capHeight * para.lineHeight
	if para.dropCapsActive() {
		d.dropCap = para.chunks[0]
		ref := d.dropCapReference()
		firstAdvance = ref.Style.FontSize * ref.Style.effectiveLineHeight(para.lineHeight)
	}
	if len(d.lines) == 0 {
		return d.ctx, d.rest, nil
	}
	d.cc = contentstream.NewContentCreator()
	d.cc.Add_q()
	value4 := firstAdvance
	if para.verticalAlignment == TextVerticalAlignmentCenter {
		value4 = d.firstLineFontSize + (capHeight+minDescent-d.firstLineFontSize)/2 + (firstAdvance-d.firstLineFontSize)/2
	}
	d.baseY = d.ctx.PageHeight - d.ctx.Y - value4
	d.cc.Translate(d.ctx.X, d.baseY)
	d.lineY = d.baseY
	if para.angle != 0 {
		d.cc.RotateDeg(para.angle)
	}
	if err := d.registerShadings(); err != nil {
		return d.ctx, nil, err
	}
	if para.textOverflow == TextOverflowHidden {
		d.cc.Add_re(0, -d.height+lineHeight+1, para.width, d.height).Add_W().Add_n()
	}
	d.cc.Add_BT()
	d.openMarkedContent()
	if d.dropCap != nil {
		if err := d.drawDropCap(); err != nil {
			return d.ctx, nil, err
		}
	}
	for i, chunks := range d.lines {
		if err := d.drawLine(i, chunks); err != nil {
			return d.ctx, nil, err
		}
	}
	if err := d.finalizeShadings(); err != nil {
		return d.ctx, nil, err
	}
	d.finish()
	return d.ctx, d.rest, nil
}

// registerDefaultFont registers the paragraph's default font under the first
// free Font<n> resource name.
func (d *paragraphDrawer) registerDefaultFont() error {
	n := 1
	keyName := core.PdfObjectName(fmt.Sprintf("Font%d", n))
	for d.block.resources.HasFontByName(keyName) {
		n++
		keyName = core.PdfObjectName(fmt.Sprintf("Font%d", n))
	}
	if err := d.block.resources.SetFontByName(keyName, d.para.defaultStyle.Font.ToPdfObject()); err != nil {
		return err
	}
	d.nextFont = n + 1
	d.fontName = keyName
	return nil
}

// newFontName returns the next unused Font<n> resource name.
func (d *paragraphDrawer) newFontName() core.PdfObjectName {
	return core.PdfObjectName(fmt.Sprintf("Font%d", d.nextFont))
}

// openMarkedContent starts the tagged or artifact marked-content sequence
// that wraps the paragraph text.
func (d *paragraphDrawer) openMarkedContent() {
	para := d.para
	propertyList := map[string]core.PdfObject{}
	if para.artifact == nil {
		if para.tagInfo != nil {
			propertyList["MCID"] = core.MakeInteger(para.tagInfo.Mcid)
		}
		if para.languageID != "" {
			propertyList["Lang"] = core.MakeString(para.languageID)
		}
		if len(propertyList) > 0 {
			d.cc.Add_BDC(*core.MakeName(string(para.tagInfo.StructureType)), propertyList)
		}
	} else {
		propertyList = para.artifact.GenerateMap()
		if len(propertyList) > 0 {
			d.cc.Add_BDC(*core.MakeName(string("Artifact")), propertyList)
		} else {
			d.cc.Add_BMC(*core.MakeName(string("Artifact")))
		}
	}
	d.openedProp = len(propertyList) > 0
}

// finish closes the text object and emits the underlines, then adds the
// content to the block and advances the draw context.
func (d *paragraphDrawer) finish() {
	if d.openedProp {
		d.cc.Add_EMC()
	}
	d.cc.Add_ET()
	for _, basicLine := range d.underlines {
		d.cc.SetStrokingColor(basicLine.LineColor).Add_w(basicLine.LineWidth).Add_m(basicLine.X1, basicLine.Y1).Add_l(basicLine.X2, basicLine.Y2).Add_s()
	}
	d.cc.Add_Q()
	operations := d.cc.Operations()
	operations.WrapIfNeeded()
	d.block.addWrappedContents(operations)
	if d.isRelative {
		d.ctx.Y += d.height
		d.ctx.Height -= d.height
		if d.ctx.Inline {
			d.ctx.X += d.para.Width() + d.para.margins.Right
		}
	}
}
