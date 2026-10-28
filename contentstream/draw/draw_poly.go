package draw

import (
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

// Draw draws the composite curve polygon. A graphics state name can be
// specified for setting the curve properties (e.g. setting the opacity).
// Otherwise leave empty (""). Returns the content stream as a byte array
// and the bounding box of the polygon.
func (p CurvePolygon) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return p.MarkedDraw(gsName, nil)
}

// Polygon is a multi-point shape that can be drawn to a PDF content stream.
type Polygon struct {
	Points        [][]Point
	FillEnabled   bool
	FillColor     model.PdfColor
	BorderEnabled bool
	BorderColor   model.PdfColor
	BorderWidth   float64
}

// Draw draws the polygon. A graphics state name can be specified for
// setting the polygon properties (e.g. setting the opacity). Otherwise leave
// empty (""). Returns the content stream as a byte array and the polygon
// bounding box.
func (p Polygon) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return p.MarkedDraw(gsName, nil)
}

// Draw draws the composite curve polygon and marked the content using the specified marked content id.
// A graphics state name can be specified for setting the curve properties (e.g. setting the opacity).
// Otherwise leave empty ("").
//
// If mcid is nil, no marked content is added.
//
// Returns the content stream as a byte array and the bounding box of the polygon.
func (p CurvePolygon) MarkedDraw(gsName string, structureInfo *model.StructureTagInfo) ([]byte, *model.PdfRectangle, error) {
	creator := contentstream.NewContentCreator()
	if structureInfo != nil {
		creator.Add_BDC(*core.MakeName(string(structureInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(structureInfo.Mcid)})
	}
	creator.Add_q()
	p.FillEnabled = p.FillEnabled && p.FillColor != nil
	if p.FillEnabled {
		creator.SetNonStrokingColor(p.FillColor)
	}
	p.BorderEnabled = p.BorderEnabled && p.BorderColor != nil
	if p.BorderEnabled {
		creator.SetStrokingColor(p.BorderColor)
		creator.Add_w(p.BorderWidth)
	}
	if len(gsName) > 1 {
		creator.Add_gs(core.PdfObjectName(gsName))
	}
	path := NewCubicBezierPath()
	for _, ring := range p.Rings {
		for i, curve := range ring {
			if i == 0 {
				creator.Add_m(curve.P0.X, curve.P0.Y)
			} else {
				creator.Add_l(curve.P0.X, curve.P0.Y)
			}
			creator.Add_c(curve.P1.X, curve.P1.Y, curve.P2.X, curve.P2.Y, curve.P3.X, curve.P3.Y)
			path = path.AppendCurve(curve)
		}
		creator.Add_h()
	}
	if p.FillEnabled && p.BorderEnabled {
		creator.Add_B()
	} else if p.FillEnabled {
		creator.Add_f()
	} else if p.BorderEnabled {
		creator.Add_S()
	}
	creator.Add_Q()
	if structureInfo != nil {
		creator.Add_EMC()
	}
	return creator.Bytes(), path.GetBoundingBox().ToPdfRectangle(), nil
}

// Draw draws the polyline and marked the content using the specified marked content id..
// A graphics state name can be specified for setting the polyline properties (e.g. setting the opacity).
// Otherwise leave empty ("").
//
// If mcid is nil, no marked content is added.
//
// Returns the content stream as a byte array and the polyline bounding box.
func (p Polyline) MarkedDraw(gsName string, structureInfo *model.StructureTagInfo) ([]byte, *model.PdfRectangle, error) {
	if p.LineColor == nil {
		p.LineColor = model.NewPdfColorDeviceRGB(0, 0, 0)
	}
	path := NewPath()
	for _, pt := range p.Points {
		path = path.AppendPoint(pt)
	}
	creator := contentstream.NewContentCreator()
	if structureInfo != nil {
		creator.Add_BDC(*core.MakeName(string(structureInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(structureInfo.Mcid)})
	}
	creator.Add_q().SetStrokingColor(p.LineColor).Add_w(p.LineWidth)
	if len(gsName) > 1 {
		creator.Add_gs(core.PdfObjectName(gsName))
	}
	DrawPathWithCreator(path, creator)
	creator.Add_S()
	creator.Add_Q()
	if structureInfo != nil {
		creator.Add_EMC()
	}
	return creator.Bytes(), path.GetBoundingBox().ToPdfRectangle(), nil
}

// Polyline defines a slice of points that are connected as straight lines.
type Polyline struct {
	Points    []Point
	LineColor model.PdfColor
	LineWidth float64
}

// Draw draws the polygon and marked the content using the specified marked content id.
// A graphics state name can be specified for setting the polygon properties (e.g. setting the opacity). Otherwise leave
// empty ("").
//
// If mcid is nil, no marked content is added.
//
// Returns the content stream as a byte array and the polygon bounding box.
func (p Polygon) MarkedDraw(gsName string, structureInfo *model.StructureTagInfo) ([]byte, *model.PdfRectangle, error) {
	creator := contentstream.NewContentCreator()
	if structureInfo != nil {
		creator.Add_BDC(*core.MakeName(string(structureInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(structureInfo.Mcid)})
	}
	creator.Add_q()
	p.FillEnabled = p.FillEnabled && p.FillColor != nil
	if p.FillEnabled {
		creator.SetNonStrokingColor(p.FillColor)
	}
	p.BorderEnabled = p.BorderEnabled && p.BorderColor != nil
	if p.BorderEnabled {
		creator.SetStrokingColor(p.BorderColor)
		creator.Add_w(p.BorderWidth)
	}
	if len(gsName) > 1 {
		creator.Add_gs(core.PdfObjectName(gsName))
	}
	path := NewPath()
	for _, ring := range p.Points {
		for i, pt := range ring {
			path = path.AppendPoint(pt)
			if i == 0 {
				creator.Add_m(pt.X, pt.Y)
			} else {
				creator.Add_l(pt.X, pt.Y)
			}
		}
		creator.Add_h()
	}
	if p.FillEnabled && p.BorderEnabled {
		creator.Add_B()
	} else if p.FillEnabled {
		creator.Add_f()
	} else if p.BorderEnabled {
		creator.Add_S()
	}
	creator.Add_Q()
	if structureInfo != nil {
		creator.Add_EMC()
	}
	return creator.Bytes(), path.GetBoundingBox().ToPdfRectangle(), nil
}

// Draw draws the composite Bezier curve and marked the content using the specified marked content id.
// A graphics state name can be specified for setting the curve properties (e.g. setting the opacity).
// Otherwise leave empty ("").
//
// If mcid is nil, no marked content is added.
//
// Returns the content stream as a byte array and the curve bounding box.
func (c PolyBezierCurve) MarkedDraw(gsName string, structureInfo *model.StructureTagInfo) ([]byte, *model.PdfRectangle, error) {
	if c.BorderColor == nil {
		c.BorderColor = model.NewPdfColorDeviceRGB(0, 0, 0)
	}
	path := NewCubicBezierPath()
	for _, src := range c.Curves {
		path = path.AppendCurve(src)
	}
	creator := contentstream.NewContentCreator()
	if structureInfo != nil {
		creator.Add_BDC(*core.MakeName(string(structureInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(structureInfo.Mcid)})
	}
	creator.Add_q()
	c.FillEnabled = c.FillEnabled && c.FillColor != nil
	if c.FillEnabled {
		creator.SetNonStrokingColor(c.FillColor)
	}
	creator.SetStrokingColor(c.BorderColor)
	creator.Add_w(c.BorderWidth)
	if len(gsName) > 1 {
		creator.Add_gs(core.PdfObjectName(gsName))
	}
	for i, curve := range path.Curves {
		if i == 0 {
			creator.Add_m(curve.P0.X, curve.P0.Y)
		} else {
			creator.Add_l(curve.P0.X, curve.P0.Y)
		}
		creator.Add_c(curve.P1.X, curve.P1.Y, curve.P2.X, curve.P2.Y, curve.P3.X, curve.P3.Y)
	}
	if c.FillEnabled {
		creator.Add_h()
		creator.Add_B()
	} else {
		creator.Add_S()
	}
	creator.Add_Q()
	if structureInfo != nil {
		creator.Add_EMC()
	}
	return creator.Bytes(), path.GetBoundingBox().ToPdfRectangle(), nil
}

// Draw draws the composite Bezier curve. A graphics state name can be
// specified for setting the curve properties (e.g. setting the opacity).
// Otherwise leave empty (""). Returns the content stream as a byte array and
// the curve bounding box.
func (c PolyBezierCurve) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return c.MarkedDraw(gsName, nil)
}

// CurvePolygon is a multi-point shape with rings containing curves that can be
// drawn to a PDF content stream.
type CurvePolygon struct {
	Rings         [][]CubicBezierCurve
	FillEnabled   bool
	FillColor     model.PdfColor
	BorderEnabled bool
	BorderColor   model.PdfColor
	BorderWidth   float64
}

// Draw draws the polyline. A graphics state name can be specified for
// setting the polyline properties (e.g. setting the opacity). Otherwise leave
// empty (""). Returns the content stream as a byte array and the polyline
// bounding box.
func (p Polyline) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return p.MarkedDraw(gsName, nil)
}
