// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package draw has handy features for defining paths which can be used to draw content on a PDF page.  Handles
// defining paths as points, vector calculations and conversion to PDF content stream data which can be used in
// page content streams and XObject forms and thus also in annotation appearance streams.
//
// Also defines utility functions for drawing common shapes such as rectangles, lines and circles (ovals).
package draw

import (
	"fmt"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"math"
)

// Draw draws the composite curve polygon. A graphics state name can be
// specified for setting the curve properties (e.g. setting the opacity).
// Otherwise leave empty (""). Returns the content stream as a byte array
// and the bounding box of the polygon.
func (p CurvePolygon) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return p.MarkedDraw(gsName, nil)
}

// ToPdfRectangle returns the rectangle as a PDF rectangle.
func (r Rectangle) ToPdfRectangle() *model.PdfRectangle {
	return &model.PdfRectangle{Llx: r.X, Lly: r.Y, Urx: r.X + r.Width, Ury: r.Y + r.Height}
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

// AppendPoint adds the specified point to the path.
func (p Path) AppendPoint(point Point) Path { p.Points = append(p.Points, point); return p }

// Line defines a line shape between point 1 (X1,Y1) and point 2 (X2,Y2).  The line ending styles can be none (regular line),
// or arrows at either end.  The line also has a specified width, color and opacity.
type Line struct {
	X1               float64
	Y1               float64
	X2               float64
	Y2               float64
	LineColor        model.PdfColor
	Opacity          float64
	LineWidth        float64
	LineEndingStyle1 LineEndingStyle
	LineEndingStyle2 LineEndingStyle
	LineStyle        LineStyle
}

// Draw draws the polygon. A graphics state name can be specified for
// setting the polygon properties (e.g. setting the opacity). Otherwise leave
// empty (""). Returns the content stream as a byte array and the polygon
// bounding box.
func (p Polygon) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return p.MarkedDraw(gsName, nil)
}

// Path consists of straight line connections between each point defined in an array of points.
type Path struct{ Points []Point }

// Copy returns a clone of the Bezier path.
func (p CubicBezierPath) Copy() CubicBezierPath {
	c := CubicBezierPath{}
	c.Curves = append(c.Curves, p.Curves...)
	return c
}

// ToPdfRectangle returns the bounding box as a PDF rectangle.
func (b BoundingBox) ToPdfRectangle() *model.PdfRectangle {
	return &model.PdfRectangle{Llx: b.X, Lly: b.Y, Urx: b.X + b.Width, Ury: b.Y + b.Height}
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

// AppendCurve appends the specified Bezier curve to the path.
func (p CubicBezierPath) AppendCurve(curve CubicBezierCurve) CubicBezierPath {
	p.Curves = append(p.Curves, curve)
	return p
}

// NewCubicBezierCurve returns a new cubic Bezier curve.
func NewCubicBezierCurve(x0, y0, x1, y1, x2, y2, x3, y3 float64) CubicBezierCurve {
	c := CubicBezierCurve{}
	c.P0 = NewPoint(x0, y0)
	c.P1 = NewPoint(x1, y1)
	c.P2 = NewPoint(x2, y2)
	c.P3 = NewPoint(x3, y3)
	return c
}

// Length returns the number of points in the path.
func (p Path) Length() int { return len(p.Points) }

// BasicLine defines a line between point 1 (X1,Y1) and point 2 (X2,Y2). The line has a specified width, color and opacity.
type BasicLine struct {
	X1        float64
	Y1        float64
	X2        float64
	Y2        float64
	LineColor model.PdfColor
	Opacity   float64
	LineWidth float64
	LineStyle LineStyle
	DashArray []int64
	DashPhase int64
}

// RemovePoint removes the point at the index specified by number from the
// path. The index is 1-based.
func (p Path) RemovePoint(number int) Path {
	if number < 1 || number > len(p.Points) {
		return p
	}
	idx := number - 1
	p.Points = append(p.Points[:idx], p.Points[idx+1:]...)
	return p
}

// Rotate returns a new Point at `p` rotated by `theta` degrees.
func (p Point) Rotate(theta float64) Point {
	rotated := transform.NewPoint(p.X, p.Y).Rotate(theta)
	return NewPoint(rotated.X, rotated.Y)
}

// Magnitude returns the magnitude of the vector.
func (v Vector) Magnitude() float64 {
	return math.Sqrt(math.Pow(v.Dx, 2.0) + math.Pow(v.Dy, 2.0))
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

// Draw draws the circle and marked the content using the specified marked content id.
// Can specify a graphics state (gsName) for setting opacity etc.  Otherwise leave empty ("").
//
// If mcid is nil, no marked content is added.
//
// Returns the content stream as a byte array, the bounding box and an error on failure.
func (c Circle) MarkedDraw(gsName string, structureInfo *model.StructureTagInfo) ([]byte, *model.PdfRectangle, error) {
	rx := c.Width / 2
	ry := c.Height / 2
	if c.BorderEnabled {
		rx -= c.BorderWidth / 2
		ry -= c.BorderWidth / 2
	}
	kappa := 0.551784
	kx := rx * kappa
	ky := ry * kappa
	path := NewCubicBezierPath()
	path = path.AppendCurve(NewCubicBezierCurve(-rx, 0, -rx, ky, -kx, ry, 0, ry))
	path = path.AppendCurve(NewCubicBezierCurve(0, ry, kx, ry, rx, ky, rx, 0))
	path = path.AppendCurve(NewCubicBezierCurve(rx, 0, rx, -ky, kx, -ry, 0, -ry))
	path = path.AppendCurve(NewCubicBezierCurve(0, -ry, -kx, -ry, -rx, -ky, -rx, 0))
	path = path.Offset(rx, ry)
	if c.BorderEnabled {
		path = path.Offset(c.BorderWidth/2, c.BorderWidth/2)
	}
	if c.X != 0 || c.Y != 0 {
		path = path.Offset(c.X, c.Y)
	}
	creator := contentstream.NewContentCreator()
	if structureInfo != nil {
		creator.Add_BDC(*core.MakeName(string(structureInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(structureInfo.Mcid)})
	}
	creator.Add_q()
	if c.FillEnabled {
		creator.SetNonStrokingColor(c.FillColor)
	}
	if c.BorderEnabled {
		creator.SetStrokingColor(c.BorderColor)
		creator.Add_w(c.BorderWidth)
	}
	if len(gsName) > 1 {
		creator.Add_gs(core.PdfObjectName(gsName))
	}
	DrawBezierPathWithCreator(path, creator)
	creator.Add_h()
	if c.FillEnabled && c.BorderEnabled {
		creator.Add_B()
	} else if c.FillEnabled {
		creator.Add_f()
	} else if c.BorderEnabled {
		creator.Add_S()
	}
	creator.Add_Q()
	if structureInfo != nil {
		creator.Add_EMC()
	}
	bbox := path.GetBoundingBox()
	if c.BorderEnabled {
		bbox.Height += c.BorderWidth
		bbox.Width += c.BorderWidth
		bbox.X -= c.BorderWidth / 2
		bbox.Y -= c.BorderWidth / 2
	}
	return creator.Bytes(), bbox.ToPdfRectangle(), nil
}

// GetBoundingBox returns the bounding box of the path.
func (p Path) GetBoundingBox() BoundingBox {
	bbox := BoundingBox{}
	minX := 0.0
	maxX := 0.0
	minY := 0.0
	maxY := 0.0
	for i, pt := range p.Points {
		if i == 0 {
			minX = pt.X
			maxX = pt.X
			minY = pt.Y
			maxY = pt.Y
			continue
		}
		if pt.X < minX {
			minX = pt.X
		}
		if pt.X > maxX {
			maxX = pt.X
		}
		if pt.Y < minY {
			minY = pt.Y
		}
		if pt.Y > maxY {
			maxY = pt.Y
		}
	}
	bbox.X = minX
	bbox.Y = minY
	bbox.Width = maxX - minX
	bbox.Height = maxY - minY
	return bbox
}

const (
	LineEndingStyleNone  LineEndingStyle = 0
	LineEndingStyleArrow LineEndingStyle = 1
	LineEndingStyleButt  LineEndingStyle = 2
)

// PolyBezierCurve represents a composite curve that is the result of
// joining multiple cubic Bezier curves.
type PolyBezierCurve struct {
	Curves      []CubicBezierCurve
	BorderWidth float64
	BorderColor model.PdfColor
	FillEnabled bool
	FillColor   model.PdfColor
}

// Offset shifts the path with the specified offsets.
func (p Path) Offset(offX, offY float64) Path {
	for i, pt := range p.Points {
		p.Points[i] = pt.Add(offX, offY)
	}
	return p
}

// NewPoint returns a new point with the coordinates x, y.
func NewPoint(x, y float64) Point { return Point{X: x, Y: y} }

// AddOffsetXY adds X,Y offset to all points on a curve.
func (c CubicBezierCurve) AddOffsetXY(offX, offY float64) CubicBezierCurve {
	c.P0.X += offX
	c.P1.X += offX
	c.P2.X += offX
	c.P3.X += offX
	c.P0.Y += offY
	c.P1.Y += offY
	c.P2.Y += offY
	c.P3.Y += offY
	return c
}

// LineEndingStyle defines the line ending style for lines.
// The currently supported line ending styles are None, Arrow (ClosedArrow) and Butt.
type LineEndingStyle int

// Draw draws the rectangle and marked the content using the specified marked content id.
// A graphics state can be specified for setting additional properties (e.g. opacity).
// Otherwise pass an empty string for the `gsName` parameter.
//
// If `mcid` is nil, no marked content is added.
//
// The method returns the content stream as a byte array and the bounding box of the shape.
func (r Rectangle) MarkedDraw(gsName string, structureInfo *model.StructureTagInfo) ([]byte, *model.PdfRectangle, error) {
	creator := contentstream.NewContentCreator()
	if structureInfo != nil {
		creator.Add_BDC(*core.MakeName(string(structureInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(structureInfo.Mcid)})
	}
	creator.Add_q()
	if r.FillEnabled {
		creator.SetNonStrokingColor(r.FillColor)
	}
	if r.BorderEnabled {
		creator.SetStrokingColor(r.BorderColor)
		creator.Add_w(r.BorderWidth)
	}
	if len(gsName) > 1 {
		creator.Add_gs(core.PdfObjectName(gsName))
	}
	var (
		x, y  = r.X, r.Y
		w, h  = r.Width, r.Height
		radTL = math.Abs(r.BorderRadiusTopLeft)
		radTR = math.Abs(r.BorderRadiusTopRight)
		radBL = math.Abs(r.BorderRadiusBottomLeft)
		radBR = math.Abs(r.BorderRadiusBottomRight)
		kappa = 0.4477
	)
	path := Path{Points: []Point{{X: x + w - radBR, Y: y}, {X: x + w, Y: y + h - radTR}, {X: x + radTL, Y: y + h}, {X: x, Y: y + radBL}}}
	corners := [][7]float64{{radBR, x + w - radBR*kappa, y, x + w, y + radBR*kappa, x + w, y + radBR}, {radTR, x + w, y + h - radTR*kappa, x + w - radTR*kappa, y + h, x + w - radTR, y + h}, {radTL, x + radTL*kappa, y + h, x, y + h - radTL*kappa, x, y + h - radTL}, {radBL, x, y + radBL*kappa, x + radBL*kappa, y, x + radBL, y}}
	creator.Add_m(x+radBL, y)
	for i := 0; i < 4; i++ {
		pt := path.Points[i]
		creator.Add_l(pt.X, pt.Y)
		corner := corners[i]
		if radius := corner[0]; radius != 0 {
			creator.Add_c(corner[1], corner[2], corner[3], corner[4], corner[5], corner[6])
		}
	}
	creator.Add_h()
	if r.FillEnabled && r.BorderEnabled {
		creator.Add_B()
	} else if r.FillEnabled {
		creator.Add_f()
	} else if r.BorderEnabled {
		creator.Add_S()
	}
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

// Draw draws the line to PDF contentstream. Generates the content stream which can be used in page contents or
// appearance stream of annotation. Returns the stream content, XForm bounding box (local), bounding box and an error
// if one occurred.
func (l Line) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	x1, x2 := l.X1, l.X2
	y1, y2 := l.Y1, l.Y2
	dy := y2 - y1
	dx := x2 - x1
	theta := math.Atan2(dy, dx)
	L := math.Sqrt(math.Pow(dx, 2.0) + math.Pow(dy, 2.0))
	width := l.LineWidth
	pi := math.Pi
	sign := 1.0
	if dx < 0 {
		sign *= -1.0
	}
	if dy < 0 {
		sign *= -1.0
	}
	VsX := sign * (-width / 2 * math.Cos(theta+pi/2))
	VsY := sign * (-width/2*math.Sin(theta+pi/2) + width*math.Sin(theta+pi/2))
	V1X := VsX + width/2*math.Cos(theta+pi/2)
	V1Y := VsY + width/2*math.Sin(theta+pi/2)
	V2X := VsX + width/2*math.Cos(theta+pi/2) + L*math.Cos(theta)
	V2Y := VsY + width/2*math.Sin(theta+pi/2) + L*math.Sin(theta)
	V3X := VsX + width/2*math.Cos(theta+pi/2) + L*math.Cos(theta) + width*math.Cos(theta-pi/2)
	V3Y := VsY + width/2*math.Sin(theta+pi/2) + L*math.Sin(theta) + width*math.Sin(theta-pi/2)
	V4X := VsX + width/2*math.Cos(theta-pi/2)
	V4Y := VsY + width/2*math.Sin(theta-pi/2)
	path := NewPath()
	path = path.AppendPoint(NewPoint(V1X, V1Y))
	path = path.AppendPoint(NewPoint(V2X, V2Y))
	path = path.AppendPoint(NewPoint(V3X, V3Y))
	path = path.AppendPoint(NewPoint(V4X, V4Y))
	startStyle := l.LineEndingStyle1
	endStyle := l.LineEndingStyle2
	arrowLen := 3 * width
	arrowWidth := 3 * width
	arrowOverhang := (arrowWidth - width) / 2
	if endStyle == LineEndingStyleArrow {
		endPt := path.GetPointNumber(2)
		backVec := NewVectorPolar(arrowLen, theta+pi)
		tip1 := endPt.AddVector(backVec)
		perpVec := NewVectorPolar(arrowWidth/2, theta+pi/2)
		forwardVec := NewVectorPolar(arrowLen, theta)
		overhangVec := NewVectorPolar(arrowOverhang, theta+pi/2)
		tip2 := tip1.AddVector(overhangVec)
		sideVec := forwardVec.Add(perpVec.Flip())
		tip3 := tip2.AddVector(sideVec)
		returnVec := perpVec.Scale(2).Flip().Add(sideVec.Flip())
		tip4 := tip3.AddVector(returnVec)
		tip5 := tip1.AddVector(NewVectorPolar(width, theta-pi/2))
		arrowPath := NewPath()
		arrowPath = arrowPath.AppendPoint(path.GetPointNumber(1))
		arrowPath = arrowPath.AppendPoint(tip1)
		arrowPath = arrowPath.AppendPoint(tip2)
		arrowPath = arrowPath.AppendPoint(tip3)
		arrowPath = arrowPath.AppendPoint(tip4)
		arrowPath = arrowPath.AppendPoint(tip5)
		arrowPath = arrowPath.AppendPoint(path.GetPointNumber(4))
		path = arrowPath
	}
	if startStyle == LineEndingStyleArrow {
		startPt := path.GetPointNumber(1)
		lastPt := path.GetPointNumber(path.Length())
		startPerpVec := NewVectorPolar(width/2, theta+pi+pi/2)
		head1 := startPt.AddVector(startPerpVec)
		headVec := NewVectorPolar(arrowLen, theta).Add(NewVectorPolar(arrowWidth/2, theta+pi/2))
		head2 := head1.AddVector(headVec)
		startOverhangVec := NewVectorPolar(arrowOverhang, theta-pi/2)
		head3 := head2.AddVector(startOverhangVec)
		tailFwdVec := NewVectorPolar(arrowLen, theta)
		tail1 := lastPt.AddVector(tailFwdVec)
		tailOverhangVec := NewVectorPolar(arrowOverhang, theta+pi+pi/2)
		tail2 := tail1.AddVector(tailOverhangVec)
		closePt := head1
		newPath := NewPath()
		newPath = newPath.AppendPoint(head1)
		newPath = newPath.AppendPoint(head2)
		newPath = newPath.AppendPoint(head3)
		for _, pt := range path.Points[1 : len(path.Points)-1] {
			newPath = newPath.AppendPoint(pt)
		}
		newPath = newPath.AppendPoint(tail1)
		newPath = newPath.AppendPoint(tail2)
		newPath = newPath.AppendPoint(closePt)
		path = newPath
	}
	creator := contentstream.NewContentCreator()
	creator.Add_q().SetNonStrokingColor(l.LineColor)
	if len(gsName) > 1 {
		creator.Add_gs(core.PdfObjectName(gsName))
	}
	path = path.Offset(l.X1, l.Y1)
	bbox := path.GetBoundingBox()
	DrawPathWithCreator(path, creator)
	if l.LineStyle == LineStyleDashed {
		creator.Add_d([]int64{1, 1}, 0).Add_S().Add_f().Add_Q()
	} else {
		creator.Add_f().Add_Q()
	}
	return creator.Bytes(), bbox.ToPdfRectangle(), nil
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

// Copy returns a clone of the path.
func (p Path) Copy() Path {
	c := Path{}
	c.Points = append(c.Points, p.Points...)
	return c
}

// CubicBezierPath represents a collection of cubic Bezier curves.
type CubicBezierPath struct{ Curves []CubicBezierCurve }

// Offset shifts the Bezier path with the specified offsets.
func (p CubicBezierPath) Offset(offX, offY float64) CubicBezierPath {
	for i, curve := range p.Curves {
		p.Curves[i] = curve.AddOffsetXY(offX, offY)
	}
	return p
}

// DrawBezierPathWithCreator makes the bezier path with the content creator.
// Adds the PDF commands to draw the path to the creator instance.
func DrawBezierPathWithCreator(bpath CubicBezierPath, creator *contentstream.ContentCreator) {
	for i, curve := range bpath.Curves {
		if i == 0 {
			creator.Add_m(curve.P0.X, curve.P0.Y)
		}
		creator.Add_c(curve.P1.X, curve.P1.Y, curve.P2.X, curve.P2.Y, curve.P3.X, curve.P3.Y)
	}
}

// Circle represents a circle shape with fill and border properties that can be drawn to a PDF content stream.
type Circle struct {
	X             float64
	Y             float64
	Width         float64
	Height        float64
	FillEnabled   bool
	FillColor     model.PdfColor
	BorderEnabled bool
	BorderWidth   float64
	BorderColor   model.PdfColor
	Opacity       float64
}

// FlipX flips the sign of the Dx component of the vector.
func (v Vector) FlipX() Vector { v.Dx = -v.Dx; return v }

// GetBoundingBox returns the bounding box of the Bezier path.
func (p CubicBezierPath) GetBoundingBox() Rectangle {
	bbox := Rectangle{}
	minX := 0.0
	maxX := 0.0
	minY := 0.0
	maxY := 0.0
	for i, curve := range p.Curves {
		bounds := curve.GetBounds()
		if i == 0 {
			minX = bounds.Llx
			maxX = bounds.Urx
			minY = bounds.Lly
			maxY = bounds.Ury
			continue
		}
		if bounds.Llx < minX {
			minX = bounds.Llx
		}
		if bounds.Urx > maxX {
			maxX = bounds.Urx
		}
		if bounds.Lly < minY {
			minY = bounds.Lly
		}
		if bounds.Ury > maxY {
			maxY = bounds.Ury
		}
	}
	bbox.X = precision.RoundDefault(minX)
	bbox.Y = precision.RoundDefault(minY)
	bbox.Width = precision.RoundDefault(maxX - minX)
	bbox.Height = precision.RoundDefault(maxY - minY)
	return bbox
}
func (p Point) String() string { return fmt.Sprintf("(%.1f,%.1f)", p.X, p.Y) }

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

// Draw draws the circle. Can specify a graphics state (gsName) for setting opacity etc.  Otherwise leave empty ("").
// Returns the content stream as a byte array, the bounding box and an error on failure.
func (c Circle) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return c.MarkedDraw(gsName, nil)
}

// NewVectorBetween returns a new vector with the direction specified by
// the subtraction of point a from point b (b-a).
func NewVectorBetween(a Point, b Point) Vector {
	v := Vector{}
	v.Dx = b.X - a.X
	v.Dy = b.Y - a.Y
	return v
}

// Add adds the specified vector to the current one and returns the result.
func (v Vector) Add(other Vector) Vector {
	v.Dx += other.Dx
	v.Dy += other.Dy
	return v
}

// Vector represents a two-dimensional vector.
type Vector struct {
	Dx float64
	Dy float64
}

// FlipY flips the sign of the Dy component of the vector.
func (v Vector) FlipY() Vector { v.Dy = -v.Dy; return v }

// GetBounds returns the bounding box of the Bezier curve.
func (c CubicBezierCurve) GetBounds() model.PdfRectangle {
	minX := c.P0.X
	maxX := c.P0.X
	minY := c.P0.Y
	maxY := c.P0.Y
	for t := 0.0; t <= 1.0; t += 0.001 {
		mt := 1 - t
		mt2 := mt * mt
		mt3 := mt2 * mt
		t2 := t * t
		t3 := t2 * t
		Rx := c.P0.X*mt3 + c.P1.X*3*t*mt2 + c.P2.X*3*t2*mt + c.P3.X*t3
		Ry := c.P0.Y*mt3 + c.P1.Y*3*t*mt2 + c.P2.Y*3*t2*mt + c.P3.Y*t3
		if Rx < minX {
			minX = Rx
		}
		if Rx > maxX {
			maxX = Rx
		}
		if Ry < minY {
			minY = Ry
		}
		if Ry > maxY {
			maxY = Ry
		}
	}
	bounds := model.PdfRectangle{}
	bounds.Llx = minX
	bounds.Lly = minY
	bounds.Urx = maxX
	bounds.Ury = maxY
	return bounds
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

// GetPolarAngle returns the angle the magnitude of the vector forms with the
// positive X-axis going counterclockwise.
func (v Vector) GetPolarAngle() float64 { return math.Atan2(v.Dy, v.Dx) }

// NewCubicBezierPath returns a new empty cubic Bezier path.
func NewCubicBezierPath() CubicBezierPath {
	p := CubicBezierPath{}
	p.Curves = []CubicBezierCurve{}
	return p
}

// LineStyle refers to how the line will be created.
type LineStyle int

// CubicBezierCurve is defined by:
// R(t) = P0*(1-t)^3 + P1*3*t*(1-t)^2 + P2*3*t^2*(1-t) + P3*t^3
// where P0 is the current point, P1, P2 control points and P3 the final point.
type CubicBezierCurve struct {
	P0 Point
	P1 Point
	P2 Point
	P3 Point
}

// BoundingBox represents the smallest rectangular area that encapsulates an object.
type BoundingBox struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// NewVectorPolar returns a new vector calculated from the specified
// magnitude and angle.
func NewVectorPolar(length float64, theta float64) Vector {
	v := Vector{}
	v.Dx = length * math.Cos(theta)
	v.Dy = length * math.Sin(theta)
	return v
}

// Draw draws the basic line to PDF. Generates the content stream which can be used in page contents or appearance
// stream of annotation. Returns the stream content, XForm bounding box (local), bounding box and an error if
// one occurred.
func (l BasicLine) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return l.MarkedDraw(gsName, nil)
}

// GetPointNumber returns the path point at the index specified by number.
// The index is 1-based.
func (p Path) GetPointNumber(number int) Point {
	if number < 1 || number > len(p.Points) {
		return Point{}
	}
	return p.Points[number-1]
}

// DrawPathWithCreator makes the path with the content creator.
// Adds the PDF commands to draw the path to the creator instance.
func DrawPathWithCreator(path Path, creator *contentstream.ContentCreator) {
	for i, pt := range path.Points {
		if i == 0 {
			creator.Add_m(pt.X, pt.Y)
		} else {
			creator.Add_l(pt.X, pt.Y)
		}
	}
}

// NewVector returns a new vector with the direction specified by dx and dy.
func NewVector(dx, dy float64) Vector { v := Vector{}; v.Dx = dx; v.Dy = dy; return v }

// Rotate rotates the vector by the specified angle.
func (v Vector) Rotate(phi float64) Vector {
	mag := v.Magnitude()
	angle := v.GetPolarAngle()
	return NewVectorPolar(mag, angle+phi)
}

// Add shifts the coordinates of the point with dx, dy and returns the result.
func (p Point) Add(dx, dy float64) Point { p.X += dx; p.Y += dy; return p }

// NewPath returns a new empty path.
func NewPath() Path { return Path{} }

// Scale scales the vector by the specified factor.
func (v Vector) Scale(factor float64) Vector {
	mag := v.Magnitude()
	angle := v.GetPolarAngle()
	v.Dx = factor * mag * math.Cos(angle)
	v.Dy = factor * mag * math.Sin(angle)
	return v
}

const (
	LineStyleSolid  LineStyle = 0
	LineStyleDashed LineStyle = 1
)

// AddVector adds vector to a point.
func (p Point) AddVector(v Vector) Point { p.X += v.Dx; p.Y += v.Dy; return p }

// Draw draws the rectangle. A graphics state can be specified for
// setting additional properties (e.g. opacity). Otherwise pass an empty string
// for the `gsName` parameter. The method returns the content stream as a byte
// array and the bounding box of the shape.
func (r Rectangle) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return r.MarkedDraw(gsName, nil)
}

// Draw draws the polyline. A graphics state name can be specified for
// setting the polyline properties (e.g. setting the opacity). Otherwise leave
// empty (""). Returns the content stream as a byte array and the polyline
// bounding box.
func (p Polyline) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return p.MarkedDraw(gsName, nil)
}

// Rectangle is a shape with a specified Width and Height and a lower left corner at (X,Y) that can be
// drawn to a PDF content stream.  The rectangle can optionally have a border and a filling color.
// The Width/Height includes the border (if any specified), i.e. is positioned inside.
type Rectangle struct {

	// Position and size properties.
	X      float64
	Y      float64
	Width  float64
	Height float64

	// Fill properties.
	FillEnabled bool
	FillColor   model.PdfColor

	// Border properties.
	BorderEnabled           bool
	BorderColor             model.PdfColor
	BorderWidth             float64
	BorderRadiusTopLeft     float64
	BorderRadiusTopRight    float64
	BorderRadiusBottomLeft  float64
	BorderRadiusBottomRight float64

	// Shape opacity (0-1 interval).
	Opacity float64
}

// Flip changes the sign of the vector: -vector.
func (v Vector) Flip() Vector {
	mag := v.Magnitude()
	angle := v.GetPolarAngle()
	v.Dx = mag * math.Cos(angle+math.Pi)
	v.Dy = mag * math.Sin(angle+math.Pi)
	return v
}

// Point represents a two-dimensional point.
type Point struct {
	X float64
	Y float64
}

// Draw draws the basic line to PDF and marked the content using the specified marked content id.
// Generates the content stream which can be used in page contents or appearance stream of annotation.
//
// If mcid is nil, no marked content is added.
//
// Returns the stream content, XForm bounding box (local), bounding box and an error if one occurred.
func (l BasicLine) MarkedDraw(gsName string, structureInfo *model.StructureTagInfo) ([]byte, *model.PdfRectangle, error) {
	path := NewPath()
	path = path.AppendPoint(NewPoint(l.X1, l.Y1))
	path = path.AppendPoint(NewPoint(l.X2, l.Y2))
	creator := contentstream.NewContentCreator()
	if structureInfo != nil {
		creator.Add_BDC(*core.MakeName(string(structureInfo.StructureType)), map[string]core.PdfObject{"MCID": core.MakeInteger(structureInfo.Mcid)})
	}
	creator.Add_q().Add_w(l.LineWidth).SetStrokingColor(l.LineColor)
	if l.LineStyle == LineStyleDashed {
		if l.DashArray == nil {
			l.DashArray = []int64{1, 1}
		}
		creator.Add_d(l.DashArray, l.DashPhase)
	}
	if len(gsName) > 1 {
		creator.Add_gs(core.PdfObjectName(gsName))
	}
	DrawPathWithCreator(path, creator)
	creator.Add_S().Add_Q()
	if structureInfo != nil {
		creator.Add_EMC()
	}
	return creator.Bytes(), path.GetBoundingBox().ToPdfRectangle(), nil
}
