package creator

import (
	"math"

	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// CurvePolygon represents a curve polygon shape.
// Implements the Drawable interface and can be drawn on PDF using the Creator.
type CurvePolygon struct {
	taggedDrawable
	polygon       *contentdraw.CurvePolygon
	fillOpacity   float64
	borderOpacity float64
	fillColor     Color
}

// SetBorderColor sets the border color.
func (cp *CurvePolygon) SetBorderColor(color Color) { cp.polygon.BorderColor = toPdfColor(color) }

// SetBorderOpacity sets the border opacity.
func (cp *CurvePolygon) SetBorderOpacity(opacity float64) { cp.borderOpacity = opacity }

// GeneratePageBlocks draws the composite curve polygon on a new block
// representing the page. Implements the Drawable interface.
func (cp *CurvePolygon) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	gsName, err := block.setOpacity(cp.fillOpacity, cp.borderOpacity)
	if err != nil {
		return nil, ctx, err
	}
	polygon := cp.polygon
	polygon.FillEnabled = polygon.FillColor != nil
	polygon.BorderEnabled = polygon.BorderColor != nil && polygon.BorderWidth > 0
	var (
		pageHeight = ctx.PageHeight
		rings      = polygon.Rings
		rows       = make([][]contentdraw.CubicBezierCurve, 0, len(polygon.Rings))
	)
	rect := model.PdfRectangle{}
	if len(rings) > 0 && len(rings[0]) > 0 {
		cubicBezierCurve := rings[0][0]
		cubicBezierCurve.P0.Y = pageHeight - cubicBezierCurve.P0.Y
		cubicBezierCurve.P1.Y = pageHeight - cubicBezierCurve.P1.Y
		cubicBezierCurve.P2.Y = pageHeight - cubicBezierCurve.P2.Y
		cubicBezierCurve.P3.Y = pageHeight - cubicBezierCurve.P3.Y
		rect = cubicBezierCurve.GetBounds()
	}
	for _, ring := range rings {
		cubicBezierCurves := make([]contentdraw.CubicBezierCurve, 0, len(ring))
		for _, cubicBezierCurve := range ring {
			cubicBezierCurve2 := cubicBezierCurve
			cubicBezierCurve2.P0.Y = pageHeight - cubicBezierCurve2.P0.Y
			cubicBezierCurve2.P1.Y = pageHeight - cubicBezierCurve2.P1.Y
			cubicBezierCurve2.P2.Y = pageHeight - cubicBezierCurve2.P2.Y
			cubicBezierCurve2.P3.Y = pageHeight - cubicBezierCurve2.P3.Y
			cubicBezierCurves = append(cubicBezierCurves, cubicBezierCurve2)
			bounds := cubicBezierCurve2.GetBounds()
			rect.Llx = math.Min(rect.Llx, bounds.Llx)
			rect.Lly = math.Min(rect.Lly, bounds.Lly)
			rect.Urx = math.Max(rect.Urx, bounds.Urx)
			rect.Ury = math.Max(rect.Ury, bounds.Ury)
		}
		rows = append(rows, cubicBezierCurves)
	}
	polygon.Rings = rows
	defer func() { polygon.Rings = rings }()
	if polygon.FillEnabled {
		err := applyShapeColor(block, cp.polygon.FillColor, cp.fillColor, func() Rectangle {
			return Rectangle{x: rect.Llx, y: rect.Lly, width: rect.Width(), height: rect.Height()}
		})
		if err != nil {
			return nil, ctx, err
		}
	}
	markedDraw, _, err := polygon.MarkedDraw(gsName, cp.tagInfo)
	if err != nil {
		return nil, ctx, err
	}
	if err = block.addContentsByString(string(markedDraw)); err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// SetBorderWidth sets the border width.
func (cp *CurvePolygon) SetBorderWidth(borderWidth float64) { cp.polygon.BorderWidth = borderWidth }

// SetFillColor sets the fill color.
func (cp *CurvePolygon) SetFillColor(color Color) {
	cp.fillColor = color
	cp.polygon.FillColor = toPdfColor(color)
}

// SetFillOpacity sets the fill opacity.
func (cp *CurvePolygon) SetFillOpacity(opacity float64) { cp.fillOpacity = opacity }

func newCurvePolygon(rings [][]contentdraw.CubicBezierCurve) *CurvePolygon {
	return &CurvePolygon{polygon: &contentdraw.CurvePolygon{Rings: rings}, fillOpacity: 1.0, borderOpacity: 1.0}
}

// Curve represents a cubic Bezier curve with a control point.
type Curve struct {
	taggedDrawable
	x1    float64
	y1    float64
	cx    float64
	cy    float64
	x2    float64
	y2    float64
	color Color
	width float64
}

func newCurve(x1, y1, cx, cy, x, y float64) *Curve {
	curve := &Curve{}
	curve.x1 = x1
	curve.y1 = y1
	curve.cx = cx
	curve.cy = cy
	curve.x2 = x
	curve.y2 = y
	curve.color = ColorBlack
	curve.width = 1.0
	return curve
}

// SetColor sets the line color.
func (cu *Curve) SetColor(col Color) { cu.color = col }

// GeneratePageBlocks draws the curve onto page blocks.
func (cu *Curve) GeneratePageBlocks(ctx DrawContext) ([]*Block, DrawContext, error) {
	block := NewBlock(ctx.PageWidth, ctx.PageHeight)
	contentCreator := contentstream.NewContentCreator()
	if cu.tagInfo != nil {
		contentCreator.Add_BDC(*core.MakeName(string(cu.tagInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(cu.tagInfo.Mcid)})
	}
	contentCreator.Add_q().Add_w(cu.width).SetStrokingColor(toPdfColor(cu.color)).Add_m(cu.x1, ctx.PageHeight-cu.y1).Add_v(cu.cx, ctx.PageHeight-cu.cy, cu.x2, ctx.PageHeight-cu.y2).Add_S().Add_Q()
	if cu.tagInfo != nil {
		contentCreator.Add_EMC()
	}
	err := block.addContentsByString(contentCreator.String())
	if err != nil {
		return nil, ctx, err
	}
	return []*Block{block}, ctx, nil
}

// SetWidth sets line width.
func (cu *Curve) SetWidth(width float64) { cu.width = width }
