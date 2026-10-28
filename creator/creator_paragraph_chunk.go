// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package creator

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
	"github.com/unidoc/typesetting/di"
	"github.com/unidoc/typesetting/shaping"
)

// chunkRun is the state for drawing one text chunk.
type chunkRun struct {
	d     *paragraphDrawer
	ls    *lineState
	chunk *TextChunk
	style *TextStyle

	// index is the chunk's position in its line (including a drop cap).
	index int

	hasOutline, hasHScale, hasOutlineSize  bool
	hasRenderMode, hasCharSpacing, hasRise bool

	keyName   core.PdfObjectName
	fontSize2 float64

	props    map[string]core.PdfObject
	spanType model.StructureType

	data       []byte
	font       *model.PdfFont
	encoder    textencoding.TextEncoder
	switchFont bool
}

func (d *paragraphDrawer) drawChunk(ls *lineState, j int, chunk *TextChunk) error {
	run := &chunkRun{d: d, ls: ls, chunk: chunk, style: &chunk.Style, index: j}
	run.hasOutline = run.style.OutlineColor != nil
	run.hasHScale = run.style.HorizontalScaling != DefaultHorizontalScaling
	run.hasOutlineSize = run.style.OutlineSize != 1
	run.hasRenderMode = run.style.RenderingMode != TextRenderingModeFill
	run.hasCharSpacing = run.style.CharSpacing != 0
	run.hasRise = run.style.TextRise != 0
	run.beginStyle()
	descent := run.applyVerticalAlignment()
	if err := run.selectFont(); err != nil {
		return err
	}
	run.openSpan()
	if err := run.writeText(); err != nil {
		return err
	}
	if err := run.flushData(); err != nil {
		return err
	}
	if len(run.props) > 0 {
		d.cc.Add_EMC()
	}
	width := ls.widths[ls.chunkIndex] / 1000.0
	d.drawUnderline(run, width)
	d.addAnnotations(run, width)
	d.extendColorSpan(run.style.Color, ls.x, width)
	ls.x += width
	run.endStyle(descent)
	ls.chunkIndex++
	return nil
}

// beginStyle sets the graphics state operators for the chunk's style.
func (r *chunkRun) beginStyle() {
	cc := r.d.cc
	if r.hasOutlineSize {
		cc.Add_w(r.style.OutlineSize)
	}
	if r.hasRenderMode {
		cc.Add_Tr(int64(r.style.RenderingMode))
	}
	if r.hasCharSpacing {
		cc.Add_Tc(r.style.CharSpacing)
	}
	if r.hasRise {
		cc.Add_Ts(r.style.TextRise)
	}
}

// endStyle restores the graphics state changed by beginStyle.
func (r *chunkRun) endStyle(descent float64) {
	cc := r.d.cc
	if r.hasOutlineSize {
		cc.Add_w(1.0)
	}
	if r.hasOutline {
		cc.Add_RG(0.0, 0.0, 0.0)
	}
	if r.hasRenderMode {
		cc.Add_Tr(int64(TextRenderingModeFill))
	}
	if r.hasCharSpacing {
		cc.Add_Tc(0)
	}
	if r.hasRise {
		cc.Add_Ts(0)
	}
	if r.hasHScale {
		cc.Add_Tz(DefaultHorizontalScaling)
	}
	if descent != 0.0 {
		cc.Translate(0, -descent)
	}
}

// applyVerticalAlignment shifts the chunk relative to the largest chunk on
// the line and returns the shift so that it can be undone.
func (r *chunkRun) applyVerticalAlignment() float64 {
	chunk, style := r.chunk, r.style
	if chunk.VerticalAlignment == TextVerticalAlignmentBaseline {
		return 0
	}
	var descent float64
	fontMetrics := newFontMetrics(chunk.Style.Font, style.FontSize)
	ref := r.ls.refMetrics
	switch chunk.VerticalAlignment {
	case TextVerticalAlignmentCenter:
		descent = ref.xHeight/2 - fontMetrics.xHeight/2
	case TextVerticalAlignmentBottom:
		descent = ref.descent - fontMetrics.descent
	case TextVerticalAlignmentTop:
		descent = r.d.firstLineFontSize - style.FontSize
	}
	if descent != 0.0 {
		r.d.cc.Translate(0, descent)
	}
	return descent
}

// selectFont picks the font resource and space width used for the chunk.
func (r *chunkRun) selectFont() error {
	r.keyName = r.d.fontName
	r.fontSize2 = r.d.fontSize
	if r.d.para.alignment != TextAlignmentJustify || r.ls.isLast {
		runeMetrics, ok := r.style.Font.GetRuneMetrics(' ')
		if !ok {
			return errors.New("the font does not have a space glyph")
		}
		r.keyName = r.d.fontNames[r.ls.index][r.ls.chunkIndex]
		r.fontSize2 = r.style.FontSize
		r.ls.spaceWx = runeMetrics.Wx * r.style.horizontalScale()
	}
	r.font = r.style.Font
	r.encoder = r.style.Font.Encoder()
	return nil
}

// openSpan opens a marked-content span when the chunk carries tagging or
// accessibility properties.
func (r *chunkRun) openSpan() {
	chunk := r.chunk
	r.props = map[string]core.PdfObject{}
	if chunk.textExpansion != nil {
		r.props["E"] = core.MakeString(*chunk.textExpansion)
	}
	if chunk.actualText != nil {
		r.props["ActualText"] = core.MakeString(*chunk.actualText)
	}
	if chunk.altText != nil {
		r.props["Alt"] = core.MakeString(*chunk.altText)
	}
	if chunk.tagInfo != nil {
		r.props["MCID"] = core.MakeInteger(chunk.tagInfo.Mcid)
	}
	r.spanType = model.StructureTypeSpan
	if chunk.tagInfo != nil && chunk.tagInfo.StructureType != model.StructureTypeUnknown {
		r.spanType = chunk.tagInfo.StructureType
	}
	if len(r.props) > 0 {
		r.d.cc.Add_BDC(*core.MakeName(string(r.spanType)), r.props)
	}
}

// writeText emits the chunk's text, either as shaped glyphs or as encoded runes.
func (r *chunkRun) writeText() error {
	d := r.d
	if d.para.enableShaping && d.shaped[r.ls.index][r.index] != nil {
		r.writeShaped(d.shaped[r.ls.index][r.index])
		return nil
	}
	if hasEmojiSequenceMarker(r.chunk.Text) {
		for _, cluster := range splitGraphemeClusters(r.chunk.Text) {
			if len(cluster.runes) == 1 && cluster.runes[0] == '\u000A' {
				continue
			}
			drawn, err := r.drawColorGlyph(cluster.text)
			if err != nil {
				return err
			}
			if drawn {
				continue
			}
			for _, ch := range cluster.runes {
				if err := r.writeRune(ch); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, ch := range r.chunk.Text {
		if ch == '\u000A' {
			continue
		}
		if isEmoji(ch) {
			drawn, err := r.drawColorGlyph(string(ch))
			if err != nil {
				return err
			}
			if drawn {
				continue
			}
		}
		if err := r.writeRune(ch); err != nil {
			return err
		}
	}
	return nil
}

func (r *chunkRun) drawColorGlyph(text string) (bool, error) {
	return drawColorGlyph(r.d.ctx, r.d.cc, r.d.block, text, r.ls.x, r.style, r.props, r.spanType)
}

// writeShaped emits the glyph IDs of already shaped text.
func (r *chunkRun) writeShaped(line shaping.Line) {
	style := r.style
	keyName := r.d.fontNames[r.ls.index][r.ls.chunkIndex]
	r.d.cc = r.d.cc.SetNonStrokingColor(r.d.pdfColorFor(style.Color)).Add_Tf(keyName, style.FontSize)
	objs := shapedGlyphObjects([]rune(r.chunk.Text), line)
	if objs != nil {
		r.d.cc.Add_TJ(objs...)
	}
}

// shapedGlyphObjects converts shaped output to TJ operands. Right-to-left
// runs are reversed, keeping leading and trailing spaces in place.
func shapedGlyphObjects(text []rune, line shaping.Line) []core.PdfObject {
	isSpace := func(glyph shaping.Glyph) bool {
		return glyph.RunesCount() == 1 && glyph.TextIndex() < len(text) && text[glyph.TextIndex()] == ' '
	}
	var objs []core.PdfObject
	add := func(glyph shaping.Glyph) {
		if glyph.GlyphID > 0xffff {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Shaped glyph ID %d exceeds uint16 - skipping", glyph.GlyphID)
			}
			return
		}
		objs = append(objs, core.MakeHexStringFromHexNumber(uint32(glyph.GlyphID), 2))
	}
	for _, output := range line {
		if output.Direction.Progression() != di.TowardTopLeft || len(output.Glyphs) == 0 {
			for _, glyph := range output.Glyphs {
				add(glyph)
			}
			continue
		}
		leading := 0
		for _, glyph := range output.Glyphs {
			if !isSpace(glyph) {
				break
			}
			leading++
		}
		trailing := 0
		for k := len(output.Glyphs) - 1; k >= leading; k-- {
			if !isSpace(output.Glyphs[k]) {
				break
			}
			trailing++
		}
		if leading == 0 && trailing == 0 {
			for _, glyph := range output.Glyphs {
				add(glyph)
			}
			continue
		}
		for _, glyph := range output.Glyphs[len(output.Glyphs)-trailing:] {
			add(glyph)
		}
		for _, glyph := range output.Glyphs[leading : len(output.Glyphs)-trailing] {
			add(glyph)
		}
		for _, glyph := range output.Glyphs[:leading] {
			add(glyph)
		}
	}
	return objs
}

// writeRune appends a rune to the pending text, emitting spaces as kerning
// adjustments and switching fonts for multi-font text.
func (r *chunkRun) writeRune(ch rune) error {
	style := r.style
	if ch == ' ' {
		if err := r.writeSpace(); err != nil {
			return err
		}
	} else {
		if _, ok := r.encoder.RuneToCharcode(ch); !ok {
			if style.MultiFont != nil {
				if err := r.switchToMultiFont(ch); err != nil {
					return err
				}
			} else {
				unsupportedRuneError := UnsupportedRuneError{Message: fmt.Sprintf("unsupported rune in text encoding: %#x (%c)", ch, ch), Rune: ch}
				r.d.ctx.errs = append(r.d.ctx.errs, unsupportedRuneError)
				common.Log.Debug(unsupportedRuneError.Error())
				if r.d.ctx.unsupportedRune <= 0 {
					return nil
				}
				ch = r.d.ctx.unsupportedRune
			}
		}
		r.data = append(r.data, r.encoder.Encode(string(ch))...)
	}
	if style.MultiFont != nil {
		style.MultiFont.Reset()
	}
	return nil
}

// writeSpace flushes pending text and emits a space as a TJ adjustment.
func (r *chunkRun) writeSpace() error {
	cc := r.d.cc
	if err := r.flushData(); err != nil {
		return err
	}
	if r.hasHScale {
		cc.Add_Tz(DefaultHorizontalScaling)
	}
	cc.Add_Tf(r.keyName, r.fontSize2).Add_TJ([]core.PdfObject{core.MakeFloat(-r.ls.spaceWx)}...)
	r.ls.widths[r.ls.chunkIndex] += r.ls.spaceWx * r.fontSize2
	return nil
}

// switchToMultiFont starts a new font run when a rune is only available in
// another font of the style's multi-font encoder.
func (r *chunkRun) switchToMultiFont(ch rune) error {
	style := r.style
	encoder, ok := style.MultiFont.Encoder(ch)
	if !ok {
		return nil
	}
	if len(r.data) != 0 {
		fontName := r.d.newFontName()
		if err := r.d.block.resources.SetFontByName(r.keyName, r.font.ToPdfObject()); err != nil {
			return err
		}
		r.d.cc.SetNonStrokingColor(r.d.pdfColorFor(style.Color)).Add_Tf(fontName, style.FontSize).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(r.data)}...)
		r.d.nextFont++
		r.data = nil
	}
	r.encoder = encoder
	r.switchFont = true
	r.font = style.MultiFont.CurrentFont
	return nil
}

// flushData writes any pending encoded text with the chunk's colour and font.
func (r *chunkRun) flushData() error {
	if len(r.data) == 0 {
		return nil
	}
	style, d := r.style, r.d
	if r.hasOutline {
		d.cc.SetStrokingColor(toPdfColor(style.OutlineColor))
	}
	if r.hasHScale {
		d.cc.Add_Tz(style.HorizontalScaling)
	}
	keyName := d.fontNames[r.ls.index][r.ls.chunkIndex]
	if r.switchFont {
		keyName = d.newFontName()
		if err := d.block.resources.SetFontByName(keyName, r.font.ToPdfObject()); err != nil {
			return err
		}
		d.nextFont++
		r.switchFont = false
		r.encoder = style.Font.Encoder()
	}
	d.cc.SetNonStrokingColor(d.pdfColorFor(style.Color)).Add_Tf(keyName, style.FontSize).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(r.data)}...)
	r.data = nil
	return nil
}
