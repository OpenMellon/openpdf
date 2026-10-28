package creator

import (
	"math"
	"sort"

	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// RadialShading holds information that will be used to render a radial shading.
type RadialShading struct {
	base        *shading
	boundingBox *model.PdfRectangle
	anchor      AnchorPoint
	x0          float64
	y0          float64
	r0          float64
	r1          float64
}

// SetBackgroundColor set background color of the shading area.
//
// By default the background color is set to white.
func (rs *RadialShading) SetBackgroundColor(backgroundColor Color) {
	rs.base.SetBackgroundColor(backgroundColor)
}

// AddShadingResource adds shading dictionary inside the resources dictionary.
func (rs *RadialShading) AddShadingResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingResource(block, rs.shadingModel().ToPdfObject())
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (rs *RadialShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }

// SetExtends specifies whether ot extend the shading beyond the starting and ending points.
//
// Text extends is set to `[]bool{false, false}` by default.
func (rs *RadialShading) SetExtends(start bool, end bool) { rs.base.SetExtends(start, end) }

// SetBoundingBox set gradient color bounding box where the gradient would be rendered.
func (rs *RadialShading) SetBoundingBox(x, y, width, height float64) {
	rs.boundingBox = &model.PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

func newRadialShading(x0 float64, y0 float64, r0 float64, r1 float64, colorPoints []*ColorPoint) *RadialShading {
	return &RadialShading{base: &shading{backgroundColor: ColorWhite, antiAlias: false, extends: []bool{false, false}, colorPoints: colorPoints}, x0: x0, y0: y0, r0: r0, r1: r1, anchor: AnchorCenter}
}

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (rs *RadialShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingPatternResource(block, rs.ToPdfShadingPattern().ToPdfObject())
}

func (rs *RadialShading) shadingModel() *model.PdfShadingType3 {
	r, g, b := rs.base.backgroundColor.ToRGB()
	var point contentdraw.Point
	switch rs.anchor {
	case AnchorBottomLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly}
	case AnchorBottomRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Ury - rs.boundingBox.Height()}
	case AnchorTopLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()}
	case AnchorTopRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Ury}
	case AnchorLeft:
		point = contentdraw.Point{X: rs.boundingBox.Llx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()/2}
	case AnchorTop:
		point = contentdraw.Point{X: rs.boundingBox.Llx + rs.boundingBox.Width()/2, Y: rs.boundingBox.Ury}
	case AnchorRight:
		point = contentdraw.Point{X: rs.boundingBox.Urx, Y: rs.boundingBox.Lly + rs.boundingBox.Height()/2}
	case AnchorBottom:
		point = contentdraw.Point{X: rs.boundingBox.Urx + rs.boundingBox.Width()/2, Y: rs.boundingBox.Lly}
	default:
		point = contentdraw.NewPoint(rs.boundingBox.Llx+rs.boundingBox.Width()/2, rs.boundingBox.Lly+rs.boundingBox.Height()/2)
	}
	r0 := rs.r0
	r1 := rs.r1
	x := point.X + rs.x0
	y := point.Y + rs.y0
	if r0 == -1.0 {
		r0 = 0.0
	}
	if r1 == -1.0 {
		var floats []float64
		x2 := x - rs.boundingBox.Llx
		urx := rs.boundingBox.Urx - x
		y2 := y - rs.boundingBox.Lly
		x := x2*x2 + y2*y2
		floats = append(floats, math.Abs(x))
		y3 := rs.boundingBox.Lly + rs.boundingBox.Height() - y
		x3 := x2*x2 + y3*y3
		floats = append(floats, math.Abs(x3))
		y4 := y - rs.boundingBox.Lly
		x4 := urx*urx + y4*y4
		floats = append(floats, math.Abs(x4))
		ury := rs.boundingBox.Ury - y
		x5 := urx*urx + ury*ury
		floats = append(floats, math.Abs(x5))
		sort.Slice(floats, func(i, j int) bool { return floats[i] > floats[j] })
		r1 = math.Sqrt(floats[0])
	}
	pdfShadingType3 := model.NewPdfShadingType3()
	pdfShadingType3.ShadingType = core.MakeInteger(3)
	pdfShadingType3.ColorSpace = model.NewPdfColorspaceDeviceRGB()
	pdfShadingType3.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingType3.AntiAlias = core.MakeBool(rs.base.antiAlias)
	pdfShadingType3.Coords = core.MakeArrayFromFloats([]float64{x, y, r0, x, y, r1})
	pdfShadingType3.Domain = core.MakeArrayFromFloats([]float64{0.0, 1.0})
	pdfShadingType3.Extend = core.MakeArray(core.MakeBool(rs.base.extends[0]), core.MakeBool(rs.base.extends[1]))
	pdfShadingType3.Function = rs.base.generatePdfFunctions()
	return pdfShadingType3
}

// ToPdfShadingPattern generates a new model.PdfShadingPatternType3 object.
func (rs *RadialShading) ToPdfShadingPattern() *model.PdfShadingPatternType3 {
	r, g, b := rs.base.backgroundColor.ToRGB()
	shadingModel := rs.shadingModel()
	shadingModel.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingPatternType3 := model.NewPdfShadingPatternType3()
	pdfShadingPatternType3.Shading = shadingModel
	return pdfShadingPatternType3
}

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (rs *RadialShading) SetAntiAlias(enable bool) { rs.base.SetAntiAlias(enable) }

// SetAnchor set gradient position anchor.
// Default to center.
func (rs *RadialShading) SetAnchor(anchor AnchorPoint) { rs.anchor = anchor }

// AddColorStop add color stop info for rendering gradient color.
func (rs *RadialShading) AddColorStop(color Color, point float64) {
	rs.base.AddColorStop(color, point)
}
