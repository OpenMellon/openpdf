// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package creator

import (
	"errors"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/typesetting/shaping"
)

// dropCapsActive reports whether the paragraph renders a drop cap.
func (sp *StyledParagraph) dropCapsActive() bool {
	return len(sp.chunks) > 0 && sp.dropCaps != nil && sp.dropCaps.Type != DropCapsNone && sp.dropCapsEnabled
}

// measureLines registers the font of every chunk, shapes the text when
// enabled and works out how many lines fit in the available height. Lines
// that do not fit are moved to d.rest.
func (d *paragraphDrawer) measureLines() error {
	d.shaped = [][]shaping.Line{}
	d.advances = make([]float64, 0, len(d.lines))
	for i, chunks := range d.lines {
		fontSize, advance := d.lineFontSizes(i, chunks)
		names, shapedLine, err := d.registerLineFonts(i, chunks, &fontSize, &advance)
		if err != nil {
			return err
		}
		d.shaped = append(d.shaped, shapedLine)
		if d.isRelative && d.height+advance > d.ctx.Height {
			d.rest = d.lines[i:]
			d.lines = d.lines[:i]
			break
		}
		d.height += advance
		d.advances = append(d.advances, advance)
		d.fontNames = append(d.fontNames, names)
	}
	return nil
}

// lineFontSizes returns the initial font size and vertical advance of a line,
// taken from its first chunk (or its second when the first is a drop cap).
func (d *paragraphDrawer) lineFontSizes(i int, chunks []*TextChunk) (fontSize, advance float64) {
	para := d.para
	if len(chunks) == 0 {
		return 0, 0
	}
	fontSize = chunks[0].Style.FontSize
	advance = chunks[0].Style.FontSize * chunks[0].Style.effectiveLineHeight(para.lineHeight)
	if i == 0 && para.dropCapsActive() && len(chunks) > 1 {
		fontSize = chunks[1].Style.FontSize
		advance = chunks[1].Style.FontSize * chunks[1].Style.effectiveLineHeight(para.lineHeight)
	}
	return fontSize, advance
}

// registerLineFonts registers the font of each chunk of a line as a page
// resource, growing fontSize and advance to the largest chunk.
func (d *paragraphDrawer) registerLineFonts(i int, chunks []*TextChunk, fontSize, advance *float64) ([]core.PdfObjectName, []shaping.Line, error) {
	para := d.para
	var names []core.PdfObjectName
	shapedLine := []shaping.Line{}
	for j, chunk := range chunks {
		if i == 0 && j == 0 && para.dropCapsActive() {
			continue
		}
		style := chunk.Style
		if chunk.Text != "" {
			if style.FontSize > *fontSize {
				*fontSize = style.FontSize
			}
			if size := style.FontSize * style.effectiveLineHeight(para.lineHeight); size > *advance {
				*advance = size
			}
		}
		if *fontSize > d.ctx.PageHeight {
			return nil, nil, errors.New("paragraph height can't be larger than page height")
		}
		keyName := d.newFontName()
		names = append(names, keyName)
		if para.enableShaping {
			line := style.Font.ShapeText(chunk.Text, style.FontSize)
			shapedLine = append(shapedLine, line)
			for _, shapingOutput := range line {
				chunk.Style.Font.RegisterLigatureMapping(shapingOutput, chunk.Text)
			}
		}
		if err := d.block.resources.SetFontByName(keyName, style.Font.ToPdfObject()); err != nil {
			return nil, nil, err
		}
		d.nextFont++
	}
	return names, shapedLine, nil
}

// dropCapReference returns the first chunk of the paragraph that is not the
// drop cap itself; the drop cap is sized relative to it.
func (d *paragraphDrawer) dropCapReference() *TextChunk {
	first := d.para.lines[0]
	ref := first[0]
	if len(first) > 1 && ref == d.dropCap {
		ref = first[1]
	}
	return ref
}
