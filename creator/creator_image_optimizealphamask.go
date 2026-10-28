// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package creator is used for quickly generating pages and content with a simple interface.
// It is built on top of the model package to provide access to the most common
// operations such as creating text and image reports and manipulating existing pages.
package creator

import (
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/internal/graphic2d"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

// OptimizeAlphaMask converts a binary alpha channel — one where every pixel
// is either fully opaque or fully transparent — into a 1-bit stencil mask
// (see SetStencilMask), which stores one bit per alpha sample instead of the
// full-depth SMask the alpha channel would otherwise produce. It reports
// whether the conversion applies: an image without an alpha channel, with
// partial transparency anywhere, with an alpha depth other than 8 or 16 bits
// per sample, or with a mask already set is left unchanged.
//
// The stencil is derived from the image's alpha channel when the image is
// drawn, so later operations that change the pixels and alpha together (such
// as Crop) stay consistent with the mask. The underlying model.Image keeps
// its alpha channel; when no encoder is set with SetEncoder, the image is
// flate encoded, matching the encoding an alpha-carrying image receives.
func (im *Image) OptimizeAlphaMask() bool {
	if im.mask != nil {
		return false
	}
	if _, ok := alphaToBitmask(im.img); !ok {
		return false
	}
	return im.setMask(&imageMask{kind: imageMaskOptimizedAlpha}) == nil
}

type GridRowSection int

func (pt *pageTransformations) transformPage(page *model.PdfPage) error {
	if err := pt.applyFlip(page); err != nil {
		return err
	}
	return nil
}

// RenderTemplate draws the parsed template into the block using the specified
// creator and `data`. The handle comes from ParseTemplate and can be rendered
// repeatedly with different data. The `data` parameter can be nil.
func (b *Block) RenderTemplate(c *Creator, pt *ParsedTemplate, data any) error {
	return pt.renderInto(c, b, data)
}

// NewImageFromData creates an Image from image data.
func (c *Creator) NewImageFromData(data []byte) (*Image, error) { return newImageFromBytes(data) }

// ParseFromSVGString creates a GraphicSVG instance from string SVG.
func ParseFromSVGString(svgStr string) (*GraphicSVGElement, error) {
	return ParseFromSVGStream(strings.NewReader(svgStr))
}

// ScaleToHeight scale Image to a specified height h, maintaining the aspect ratio.
func (im *Image) ScaleToHeight(h float64) {
	width := im.width / im.height
	im.height = h
	im.width = h * width
}

func toPdfColor(color Color) model.PdfColor {
	if color == nil {
		color = ColorBlack
	}
	switch grayColor2 := color.(type) {
	case grayColor:
		return model.NewPdfColorDeviceGray(grayColor2.gray)
	case cmykColor:
		return model.NewPdfColorDeviceCMYK(grayColor2.c, grayColor2.m, grayColor2.y, grayColor2.k)
	case *FunctionBasedShading:
		return model.NewPdfColorPatternType1()
	case *LinearShading:
		return model.NewPdfColorPatternType2()
	case *RadialShading:
		return model.NewPdfColorPatternType3()
	}
	return model.NewPdfColorDeviceRGB(color.ToRGB())
}

func (gse *GraphicSVGElement) drawPath(cc *contentstream.ContentCreator, resources *model.PdfPageResources) {
	cc.Add_q()
	gse.Style.toContentStream(cc, resources, gse)
	applySVGTransform(gse, cc)
	svgPath, err := parseSVGPath(gse.Attributes["d"])
	if err != nil {
		common.Log.Error("ERROR: %s", err.Error())
	}
	var (
		startX, startY = 0.0, 0.0
		param, y       = 0.0, 0.0
		command2       *Command
	)
	for _, subpath := range svgPath.Subpaths {
		for _, command := range subpath.Commands {
			switch strings.ToLower(command.Symbol) {
			case "m":
				param, y = command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, y = startX+param-gse.ViewBox.X, startY+y-gse.ViewBox.Y
				}
				cc.Add_m(precision.RoundFloat(param, 3), precision.RoundFloat(y, 3))
				startX, startY = param, y
			case "c":
				param, value, param2, param3, param4, param5 := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale, command.Params[4]*gse.scale, command.Params[5]*gse.scale
				if !command.isAbsolute() {
					param, value, param2, param3, param4, param5 = startX+param, startY+value, startX+param2, startY+param3, startX+param4, startY+param5
				}
				cc.Add_c(precision.RoundFloat(param, 3), precision.RoundFloat(value, 3), precision.RoundFloat(param2, 3), precision.RoundFloat(param3, 3), precision.RoundFloat(param4, 3), precision.RoundFloat(param5, 3))
				startX, startY = param4, param5
			case "s":
				param, value, param2, param3 := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale
				if !command.isAbsolute() {
					param, value, param2, param3 = startX+param, startY+value, startX+param2, startY+param3
				}
				cc.Add_c(precision.RoundFloat(startX, 3), precision.RoundFloat(startY, 3), precision.RoundFloat(param, 3), precision.RoundFloat(value, 3), precision.RoundFloat(param2, 3), precision.RoundFloat(param3, 3))
				startX, startY = param2, param3
			case "l":
				param, value := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, value = startX+param, startY+value
				}
				cc.Add_l(precision.RoundFloat(param, 3), precision.RoundFloat(value, 3))
				startX, startY = param, value
			case "h":
				param := command.Params[0] * gse.scale
				if !command.isAbsolute() {
					param = startX + param
				}
				cc.Add_l(precision.RoundFloat(param, 3), precision.RoundFloat(startY, 3))
				startX = param
			case "v":
				param := command.Params[0] * gse.scale
				if !command.isAbsolute() {
					param = startY + param
				}
				cc.Add_l(precision.RoundFloat(startX, 3), precision.RoundFloat(param, 3))
				startY = param
			case "q":
				param, y1, x, y := command.Params[0]*gse.scale, command.Params[1]*gse.scale, command.Params[2]*gse.scale, command.Params[3]*gse.scale
				if !command.isAbsolute() {
					param, y1, x, y = startX+param, startY+y1, startX+x, startY+y
				}
				quadraticToCubicBezier, point := graphic2d.QuadraticToCubicBezier(startX, startY, param, y1, x, y)
				cc.Add_c(precision.RoundFloat(quadraticToCubicBezier.X, 3), precision.RoundFloat(quadraticToCubicBezier.Y, 3), precision.RoundFloat(point.X, 3), precision.RoundFloat(point.Y, 3), precision.RoundFloat(x, 3), precision.RoundFloat(y, 3))
				startX, startY = x, y
			case "t":
				var point3, point4 graphic2d.Point
				param, y := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				if !command.isAbsolute() {
					param, y = startX+param, startY+y
				}
				if command2 != nil && strings.ToLower(command2.Symbol) == "q" {
					point := graphic2d.Point{X: command2.Params[0] * gse.scale, Y: command2.Params[1] * gse.scale}
					point2 := graphic2d.Point{X: command2.Params[2] * gse.scale, Y: command2.Params[3] * gse.scale}
					sub := point2.Mul(2.0).Sub(point)
					point3, point4 = graphic2d.QuadraticToCubicBezier(startX, startY, sub.X, sub.Y, param, y)
				}
				cc.Add_c(precision.RoundFloat(point3.X, 3), precision.RoundFloat(point3.Y, 3), precision.RoundFloat(point4.X, 3), precision.RoundFloat(point4.Y, 3), precision.RoundFloat(param, 3), precision.RoundFloat(y, 3))
				startX, startY = param, y
			case "a":
				param, ry := command.Params[0]*gse.scale, command.Params[1]*gse.scale
				rot := command.Params[2]
				large := command.Params[3] > 0
				sweep := command.Params[4] > 0
				endX, endY := command.Params[5]*gse.scale, command.Params[6]*gse.scale
				if !command.isAbsolute() {
					endX, endY = startX+endX, startY+endY
				}
				ellipseToCubicBeziers := graphic2d.EllipseToCubicBeziers(startX, startY, param, ry, rot, large, sweep, endX, endY)
				for _, ellipseToCubicBezier := range ellipseToCubicBeziers {
					cc.Add_c(precision.RoundFloat(ellipseToCubicBezier[1].X, 3), precision.RoundFloat(ellipseToCubicBezier[1].Y, 3), precision.RoundFloat(ellipseToCubicBezier[2].X, 3), precision.RoundFloat(ellipseToCubicBezier[2].Y, 3), precision.RoundFloat(ellipseToCubicBezier[3].X, 3), precision.RoundFloat(ellipseToCubicBezier[3].Y, 3))
				}
				startX, startY = endX, endY
			case "z":
				cc.Add_h()
			}
			command2 = command
		}
	}
	gse.Style.fillStroke(cc)
	cc.Add_h()
	cc.Add_Q()
}

// Drawable is a widget that can be used to draw with the Creator.
type Drawable interface {
	// GeneratePageBlocks draw onto blocks representing Page contents. As the content can wrap over many pages, multiple
	// templates are returned, one per Page.  The function also takes a draw context containing information
	// where to draw (if relative positioning) and the available height to draw on accounting for Margins etc.
	GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error)

	// SetMarkedContentID sets the marked content id for the drawable.
	SetMarkedContentID(mcid int64)

	// SetStructureType sets the structure type for the drawable.
	SetStructureType(structType model.StructureType)

	// GenerateKDict generates a K dictionary for the drawable.
	GenerateKDict() (*model.KDict, error)

	// SetStructPageNumber sets the page number where the structure element for this drawable is located.
	SetStructPageNumber(pageNum *int64)
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F600 && r <= 0x1F64F:
		return true
	case r >= 0x1F300 && r <= 0x1F5FF:
		return true
	case r >= 0x1F680 && r <= 0x1F6FF:
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF:
		return true
	case r >= 0x1F900 && r <= 0x1F9FF:
		return true
	case r >= 0x1FA70 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x26FF:
		return true
	case r >= 0x2700 && r <= 0x27BF:
		return true
	default:
		return false
	}
}

// SetBorderRadius sets the radius of the rectangle corners.
func (r *Rectangle) SetBorderRadius(topLeft, topRight, bottomLeft, bottomRight float64) {
	r.borderRadiusTopLeft = topLeft
	r.borderRadiusTopRight = topRight
	r.borderRadiusBottomLeft = bottomLeft
	r.borderRadiusBottomRight = bottomRight
}

// GetVerticalAlignment returns the cell's vertical alignment of content.
func (gc *GridCell) GetVerticalAlignment() CellVerticalAlignment { return gc.verticalAlignment }

type graphemeCluster struct {
	runes []rune
	text  string
}

// AddColorStop add color stop information for rendering gradient.
func (s *shading) AddColorStop(color Color, point float64) {
	s.colorPoints = append(s.colorPoints, newColorPoint(color, point))
}

// SetPadding sets the padding of the component. The padding represents
// inner margins which are applied around the contents of the division.
// The background of the component is not affected by its padding.
func (d *Division) SetPadding(left, right, top, bottom float64) {
	d.padding.Left = left
	d.padding.Right = right
	d.padding.Top = top
	d.padding.Bottom = bottom
}

func (g *Grid) updateRowHeights(totalWidth float64) {
	for _, row := range g.rows {
		row.updateRowHeight(totalWidth)
	}
}
