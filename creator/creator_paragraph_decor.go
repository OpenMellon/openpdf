// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package creator

import (
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// drawUnderline queues an underline below the chunk; it is stroked after the
// text object ends.
func (d *paragraphDrawer) drawUnderline(run *chunkRun, width float64) {
	style := run.style
	if !style.Underline {
		return
	}
	color := style.UnderlineStyle.Color
	if color == nil {
		color = run.chunk.Style.Color
	}
	r, g, b := color.ToRGB()
	x1 := run.ls.x - d.ctx.X
	offset := d.lineY - d.baseY + style.TextRise - style.UnderlineStyle.Offset
	d.underlines = append(d.underlines, &contentdraw.BasicLine{X1: x1, Y1: offset, X2: x1 + width, Y2: offset, LineWidth: run.chunk.Style.UnderlineStyle.Thickness, LineColor: model.NewPdfColorDeviceRGB(r, g, b)})
}

// addAnnotations sets the rectangle of each annotation attached to the chunk
// and adds it to the block.
func (d *paragraphDrawer) addAnnotations(run *chunkRun, width float64) {
	chunk, style, ls := run.chunk, run.style, run.ls
	for k, annotation := range chunk.annotations {
		var array *core.PdfObjectArray
		if len(chunk.lineBreaks) == k {
			switch context := annotation.GetContext().(type) {
			case *model.PdfAnnotationLink:
				array = core.MakeArray()
				context.Rect = array
				d.flipLinkDestination(context)
			case *model.PdfAnnotationHighlight:
				array = core.MakeArray()
				context.Rect = array
				llx := ls.x
				textRise := d.lineY + style.TextRise
				points := rotatedCorners(&model.PdfRectangle{Llx: llx, Lly: textRise, Urx: llx + width, Ury: textRise + ls.advance}, d.para.angle)
				context.QuadPoints = core.MakeArrayFromFloats([]float64{points[0].X, points[0].Y, points[1].X, points[1].Y, points[3].X, points[3].Y, points[2].X, points[2].Y})
			}
			chunk.lineBreaks = append(chunk.lineBreaks, true)
		}
		if array != nil {
			d.setAnnotationRect(array, ls.x, style.TextRise, width, ls.advance)
		}
		d.block.AddAnnotation(annotation)
	}
}

// flipLinkDestination converts the y coordinate of an XYZ destination from
// the creator's top-down system to PDF's bottom-up one.
func (d *paragraphDrawer) flipLinkDestination(link *model.PdfAnnotationLink) {
	dest, ok := link.Dest.(*core.PdfObjectArray)
	if !ok || dest.Len() != 5 {
		return
	}
	name, ok := dest.Get(1).(*core.PdfObjectName)
	if !ok || name.String() != "XYZ" {
		return
	}
	if top, err := core.GetNumberAsFloat(dest.Get(3)); err == nil {
		dest.Set(3, core.MakeFloat(d.ctx.PageHeight-top))
	}
}

// setAnnotationRect fills array with the rotated bounds of a chunk of the
// given width and height that starts at chunkX on the current line.
func (d *paragraphDrawer) setAnnotationRect(array *core.PdfObjectArray, chunkX, textRise, width, height float64) {
	angle := d.para.angle
	point := contentdraw.NewPoint(chunkX-d.ctx.X, d.lineY+textRise-d.baseY).Rotate(angle)
	point.X += d.ctx.X
	point.Y += d.baseY
	bx, by, bw, bh := rotatedBounds(width, height, angle)
	point.X += bx
	point.Y += by
	array.Clear()
	array.Append(core.MakeFloat(point.X))
	array.Append(core.MakeFloat(point.Y))
	array.Append(core.MakeFloat(point.X + bw))
	array.Append(core.MakeFloat(point.Y + bh))
}
