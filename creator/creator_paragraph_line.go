// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package creator

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// lineState is the per-line state shared by the chunks of one line.
type lineState struct {
	index   int
	chunks  []*TextChunk
	x       float64
	advance float64
	extent  float64
	isLast  bool

	// spaceWx is the width of a space, adjusted per chunk or for justification.
	spaceWx float64
	// widths holds the measured width of each chunk (in thousandths of a unit).
	widths []float64
	// refMetrics are the metrics of the largest chunk on the line.
	refMetrics *fontMetrics
	// chunkIndex counts the chunks drawn so far (excluding a drop cap).
	chunkIndex int
}

// lineWidths is the result of measuring the chunks of a line.
type lineWidths struct {
	total      float64
	spaceWx    float64
	spaceCount uint
	widths     []float64
	refMetrics *fontMetrics
}

func (d *paragraphDrawer) drawLine(i int, chunks []*TextChunk) error {
	para := d.para
	ascent, _, descent := para.getLineMetrics(i)
	ls := &lineState{index: i, chunks: chunks, x: d.ctx.X, isLast: i == len(d.lines)-1}
	ls.extent = ascent + descent
	lineHeight := ls.extent * para.lineHeight
	ls.advance = d.advances[i]
	if lineHeight > ls.advance {
		ls.advance = lineHeight
	}
	if i != 0 {
		d.cc.Add_TD(0, -ls.advance)
		d.lineY -= ls.advance
	}
	indent, prevIndent := d.dropCapIndent(i)
	if i > 0 {
		if shift := indent - prevIndent; shift != 0 {
			d.cc.Add_Td(shift, 0)
		}
	}
	ls.x += indent
	w, err := d.measureLineWidths(ls)
	if err != nil {
		return err
	}
	ls.widths = w.widths
	ls.refMetrics = w.refMetrics
	ls.spaceWx = d.alignLine(ls, w)
	for j, chunk := range chunks {
		if i == 0 && j == 0 && d.dropCap != nil {
			continue
		}
		if err := d.drawChunk(ls, j, chunk); err != nil {
			return err
		}
	}
	return nil
}

// measureLineWidths measures the width of every chunk on a line and totals
// the space characters, which justification later stretches.
func (d *paragraphDrawer) measureLineWidths(ls *lineState) (lineWidths, error) {
	var (
		w          lineWidths
		maxSize    float64
		spaceTotal float64
	)
	for j, chunk := range ls.chunks {
		if ls.index == 0 && j == 0 && d.dropCap != nil {
			continue
		}
		style := &chunk.Style
		if style.FontSize > maxSize {
			maxSize = style.FontSize
			w.refMetrics = newFontMetrics(chunk.Style.Font, style.FontSize)
		}
		if ls.extent > maxSize {
			maxSize = ls.extent
		}
		spaceMetrics, ok := style.Font.GetRuneMetrics(' ')
		if spaceMetrics.Wx == 0 && style.MultiFont != nil {
			spaceMetrics, ok = style.MultiFont.GetRuneMetrics(' ')
			style.MultiFont.Reset()
		}
		if !ok {
			return w, errors.New("the font does not have a space glyph")
		}
		var spaces uint
		var width float64
		textLen := len(chunk.Text)
		for k, r := range chunk.Text {
			if r == ' ' {
				spaces++
				continue
			}
			if r == '\u000A' {
				continue
			}
			runeMetrics, ok := style.Font.GetRuneMetrics(r)
			if runeMetrics.Wx == 0 && style.MultiFont != nil {
				runeMetrics, ok = style.MultiFont.GetRuneMetrics(' ')
				style.MultiFont.Reset()
			}
			if !ok {
				common.Log.Debug("Unsupported rune %v in font\n", r)
				return w, errors.New("unsupported text glyph")
			}
			width += style.FontSize * runeMetrics.Wx * style.horizontalScale()
			if k != textLen-1 {
				width += style.CharSpacing * 1000.0
			}
		}
		w.widths = append(w.widths, width)
		w.total += width
		spaceTotal += float64(spaces) * spaceMetrics.Wx * style.FontSize * style.horizontalScale()
		w.spaceCount += spaces
	}
	w.spaceWx = spaceTotal
	return w, nil
}

// alignLine applies the paragraph's horizontal alignment to the line. It
// emits the offset needed for centred and right-aligned text, moves ls.x and
// returns the space width to use (stretched when justifying).
func (d *paragraphDrawer) alignLine(ls *lineState, w lineWidths) float64 {
	para := d.para
	var objs []core.PdfObject
	spaceWx := w.spaceWx
	width := para.width * 1000.0
	switch para.alignment {
	case TextAlignmentJustify:
		if w.spaceCount > 0 && !ls.isLast {
			spaceWx = (width - w.total) / float64(w.spaceCount) / d.fontSize
		}
	case TextAlignmentCenter:
		offset := (width - w.total - spaceWx) / 2
		objs = append(objs, core.MakeFloat(-(offset / d.fontSize)))
		ls.x += offset / 1000.0
	case TextAlignmentRight:
		offset := width - w.total - spaceWx
		objs = append(objs, core.MakeFloat(-(offset / d.fontSize)))
		ls.x += offset / 1000.0
	}
	if len(objs) > 0 {
		d.cc.Add_Tf(d.fontName, d.fontSize).Add_TL(d.fontSize * para.lineHeight).Add_TJ(objs...)
	}
	return spaceWx
}
