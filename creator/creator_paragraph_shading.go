// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package creator

import (
	"github.com/OpenMellon/openpdf/model"
)

// registerShadings registers a pattern resource for every gradient colour
// used by the lines being drawn.
func (d *paragraphDrawer) registerShadings() error {
	bounds := func() Rectangle {
		return Rectangle{x: d.ctx.X, y: d.baseY, width: d.para.getMaxLineWidth() / 1000.0, height: d.height}
	}
	for _, chunks := range d.lines {
		for _, chunk := range chunks {
			switch chunk.Style.Color.(type) {
			case *LinearShading, *RadialShading, *FunctionBasedShading:
				color := chunk.Style.Color
				if _, ok := d.colors[color]; ok {
					continue
				}
				pdfColor := toPdfColor(color)
				if err := applyShapeColor(d.block, pdfColor, color, bounds); err != nil {
					return err
				}
				d.colors[color] = pdfColor
				d.colorSpans[color] = nil
			}
		}
	}
	return nil
}

// extendColorSpan widens the horizontal extent covered by a gradient colour.
func (d *paragraphDrawer) extendColorSpan(color Color, start, width float64) {
	if _, ok := d.colors[color]; !ok {
		return
	}
	span := d.colorSpans[color]
	if span == nil {
		d.colorSpans[color] = &[2]float64{start, start + width}
		return
	}
	if start < span[0] {
		span[0] = start
	}
	if start+width > span[1] {
		span[1] = start + width
	}
}

// pdfColorFor returns the PDF colour for c, using the registered pattern for
// gradients.
func (d *paragraphDrawer) pdfColorFor(color Color) model.PdfColor {
	if pdfColor, ok := d.colors[color]; ok {
		return pdfColor
	}
	return toPdfColor(color)
}

// finalizeShadings sets the bounding box of each gradient to the area its
// text covers and stores the resulting pattern in the block resources.
func (d *paragraphDrawer) finalizeShadings() error {
	for key, span := range d.colorSpans {
		if span == nil {
			continue
		}
		switch shading := key.(type) {
		case *LinearShading:
			shading.SetBoundingBox(span[0], d.baseY, span[1]-span[0], d.height)
			patternName := d.colors[key].(*model.PdfColorPatternType2).PatternName
			if err := d.block.resources.SetPatternByName(patternName, shading.ToPdfShadingPattern().ToPdfObject()); err != nil {
				return err
			}
		case *RadialShading:
			shading.SetBoundingBox(span[0], d.baseY, span[1]-span[0], d.height)
			patternName := d.colors[key].(*model.PdfColorPatternType3).PatternName
			if err := d.block.resources.SetPatternByName(patternName, shading.ToPdfShadingPattern().ToPdfObject()); err != nil {
				return err
			}
		case *FunctionBasedShading:
			shading.SetBoundingBox(span[0], d.baseY, span[1]-span[0], d.height)
			patternName := d.colors[key].(*model.PdfColorPatternType1).PatternName
			pdfShadingPattern, err := shading.ToPdfShadingPattern()
			if err != nil {
				return err
			}
			if err := d.block.resources.SetPatternByName(patternName, pdfShadingPattern.ToPdfObject()); err != nil {
				return err
			}
		}
	}
	return nil
}
