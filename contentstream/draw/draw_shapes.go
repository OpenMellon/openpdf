package draw

import (
	"math"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

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

const (
	LineEndingStyleNone  LineEndingStyle = 0
	LineEndingStyleArrow LineEndingStyle = 1
	LineEndingStyleButt  LineEndingStyle = 2
)

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

// Draw draws the circle. Can specify a graphics state (gsName) for setting opacity etc.  Otherwise leave empty ("").
// Returns the content stream as a byte array, the bounding box and an error on failure.
func (c Circle) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return c.MarkedDraw(gsName, nil)
}

// LineStyle refers to how the line will be created.
type LineStyle int

// Draw draws the basic line to PDF. Generates the content stream which can be used in page contents or appearance
// stream of annotation. Returns the stream content, XForm bounding box (local), bounding box and an error if
// one occurred.
func (l BasicLine) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return l.MarkedDraw(gsName, nil)
}

const (
	LineStyleSolid  LineStyle = 0
	LineStyleDashed LineStyle = 1
)

// Draw draws the rectangle. A graphics state can be specified for
// setting additional properties (e.g. opacity). Otherwise pass an empty string
// for the `gsName` parameter. The method returns the content stream as a byte
// array and the bounding box of the shape.
func (r Rectangle) Draw(gsName string) ([]byte, *model.PdfRectangle, error) {
	return r.MarkedDraw(gsName, nil)
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
