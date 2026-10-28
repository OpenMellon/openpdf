package creator

import (
	"math"

	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// LinearShading holds data for rendering a linear shading gradient.
type LinearShading struct {
	base        *shading
	boundingBox *model.PdfRectangle
	angle       float64
}

// SetAngle would set the angle at which the gradient is rendered.
//
// The default angle would be 0 where the gradient would be rendered from left to right side.
func (ls *LinearShading) SetAngle(angle float64) { ls.angle = angle }

// SetAntiAlias enables anti alias config.
//
// Anti alias is disabled by default.
func (ls *LinearShading) SetAntiAlias(enable bool) { ls.base.SetAntiAlias(enable) }

// AddColorStop add color stop info for rendering gradient color.
func (ls *LinearShading) AddColorStop(color Color, point float64) {
	ls.base.AddColorStop(color, point)
}

// SetBoundingBox set gradient color bounding box where the gradient would be rendered.
func (ls *LinearShading) SetBoundingBox(x, y, width, height float64) {
	ls.boundingBox = &model.PdfRectangle{Llx: x, Lly: y, Urx: x + width, Ury: y + height}
}

// ToPdfShadingPattern generates a new model.PdfShadingPatternType2 object.
func (ls *LinearShading) ToPdfShadingPattern() *model.PdfShadingPatternType2 {
	r, g, b := ls.base.backgroundColor.ToRGB()
	shadingModel := ls.shadingModel()
	shadingModel.Background = core.MakeArrayFromFloats([]float64{r, g, b})
	pdfShadingPatternType2 := model.NewPdfShadingPatternType2()
	pdfShadingPatternType2.Shading = shadingModel
	return pdfShadingPatternType2
}

// SetExtends specifies whether ot extend the shading beyond the starting and ending points.
//
// Text extends is set to `[]bool{false, false}` by default.
func (ls *LinearShading) SetExtends(start bool, end bool) { ls.base.SetExtends(start, end) }

// AddPatternResource adds pattern dictionary inside the resources dictionary.
func (ls *LinearShading) AddPatternResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingPatternResource(block, ls.ToPdfShadingPattern().ToPdfObject())
}

// ToRGB implements interface Color.
// Note: It's not directly used since shading color works differently than regular color.
func (ls *LinearShading) ToRGB() (float64, float64, float64) { return 0, 0, 0 }

func newLinearShading(colorPoints []*ColorPoint) *LinearShading {
	return &LinearShading{base: &shading{backgroundColor: ColorWhite, antiAlias: false, extends: []bool{false, false}, colorPoints: colorPoints}, boundingBox: &model.PdfRectangle{}}
}

func (ls *LinearShading) shadingModel() *model.PdfShadingType2 {
	point := contentdraw.NewPoint(ls.boundingBox.Llx+ls.boundingBox.Width()/2, ls.boundingBox.Lly+ls.boundingBox.Height()/2)
	point2 := contentdraw.NewPoint(ls.boundingBox.Llx, ls.boundingBox.Lly+ls.boundingBox.Height()/2).Add(-point.X, -point.Y).Rotate(ls.angle).Add(point.X, point.Y)
	point2 = contentdraw.NewPoint(math.Max(math.Min(point2.X, ls.boundingBox.Urx), ls.boundingBox.Llx), math.Max(math.Min(point2.Y, ls.boundingBox.Ury), ls.boundingBox.Lly))
	point3 := contentdraw.NewPoint(ls.boundingBox.Urx, ls.boundingBox.Lly+ls.boundingBox.Height()/2).Add(-point.X, -point.Y).Rotate(ls.angle).Add(point.X, point.Y)
	point3 = contentdraw.NewPoint(math.Min(math.Max(point3.X, ls.boundingBox.Llx), ls.boundingBox.Urx), math.Min(math.Max(point3.Y, ls.boundingBox.Lly), ls.boundingBox.Ury))
	pdfShadingType2 := model.NewPdfShadingType2()
	pdfShadingType2.ShadingType = core.MakeInteger(2)
	pdfShadingType2.ColorSpace = model.NewPdfColorspaceDeviceRGB()
	pdfShadingType2.AntiAlias = core.MakeBool(ls.base.antiAlias)
	pdfShadingType2.Coords = core.MakeArrayFromFloats([]float64{point2.X, point2.Y, point3.X, point3.Y})
	pdfShadingType2.Extend = core.MakeArray(core.MakeBool(ls.base.extends[0]), core.MakeBool(ls.base.extends[1]))
	pdfShadingType2.Function = ls.base.generatePdfFunctions()
	return pdfShadingType2
}

// SetBackgroundColor set background color of the shading area.
//
// By default the background color is set to white.
func (ls *LinearShading) SetBackgroundColor(backgroundColor Color) {
	ls.base.SetBackgroundColor(backgroundColor)
}

// AddShadingResource adds shading dictionary inside the resources dictionary.
func (ls *LinearShading) AddShadingResource(block *Block) (name core.PdfObjectName, err error) {
	return addShadingResource(block, ls.shadingModel().ToPdfObject())
}
