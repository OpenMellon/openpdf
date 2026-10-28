// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package creator

import (
	"github.com/OpenMellon/openpdf/core"
)

// drawDropCap draws the drop cap character at the start of the first line
// and records the horizontal gap the following lines must leave for it.
func (d *paragraphDrawer) drawDropCap() error {
	chunk := d.dropCap
	style := &chunk.Style
	dropCaps := d.para.dropCaps
	fontName := d.newFontName()
	if err := d.block.resources.SetFontByName(fontName, style.Font.ToPdfObject()); err != nil {
		return err
	}
	d.nextFont++
	data := style.Font.Encoder().Encode(chunk.Text)
	var charSpacing float64
	text := []rune(chunk.Text)
	for i, r := range text {
		runeMetrics, ok := style.Font.GetRuneMetrics(r)
		if ok {
			charSpacing += style.FontSize * runeMetrics.Wx * style.horizontalScale() / 1000.0
			if i < len(text)-1 {
				charSpacing += style.CharSpacing
			}
		}
	}
	ref := d.dropCapReference()
	refMetrics := newFontMetrics(ref.Style.Font, ref.Style.FontSize)
	dropMetrics := newFontMetrics(style.Font, style.FontSize)
	var lift float64
	switch dropCaps.Type {
	case DropCapsDrop:
		lift = refMetrics.ascent - dropMetrics.ascent
	case DropCapsInline:
		lift = refMetrics.capHeight - dropMetrics.capHeight
	default:
		return nil
	}
	d.cc.SetNonStrokingColor(d.pdfColorFor(style.Color)).Add_Tf(fontName, style.FontSize).Add_Td(0, lift).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(data)}...)
	d.cc.Add_Td(charSpacing+dropCaps.Gap, -lift)
	d.dropGap = charSpacing + dropCaps.Gap
	d.extendColorSpan(style.Color, d.ctx.X, charSpacing)
	return nil
}

// dropCapIndent returns how far line i must be indented and how far the
// previous line was indented to leave room for the drop cap.
func (d *paragraphDrawer) dropCapIndent(i int) (indent, prevIndent float64) {
	if d.dropCap == nil {
		return 0, 0
	}
	dropCaps := d.para.dropCaps
	if i > 0 {
		if dropCaps.Type == DropCapsDrop && i-1 < dropCaps.NumLines {
			prevIndent = d.dropGap
		} else if dropCaps.Type == DropCapsInline {
			prevIndent = d.dropGap
		}
	}
	switch dropCaps.Type {
	case DropCapsDrop:
		if i < dropCaps.NumLines {
			indent = d.dropGap
		}
	case DropCapsInline:
		indent = d.dropGap
	}
	return indent, prevIndent
}
