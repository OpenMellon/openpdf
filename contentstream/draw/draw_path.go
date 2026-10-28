package draw

import (
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

// AppendPoint adds the specified point to the path.
func (p Path) AppendPoint(point Point) Path { p.Points = append(p.Points, point); return p }

// Path consists of straight line connections between each point defined in an array of points.
type Path struct{ Points []Point }

// Copy returns a clone of the Bezier path.
func (p CubicBezierPath) Copy() CubicBezierPath {
	c := CubicBezierPath{}
	c.Curves = append(c.Curves, p.Curves...)
	return c
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

// NewCubicBezierPath returns a new empty cubic Bezier path.
func NewCubicBezierPath() CubicBezierPath {
	p := CubicBezierPath{}
	p.Curves = []CubicBezierCurve{}
	return p
}

// CubicBezierCurve is defined by:
// R(t) = P0*(1-t)^3 + P1*3*t*(1-t)^2 + P2*3*t^2*(1-t) + P3*t^3
// where P0 is the current point, P1, P2 control points and P3 the final point.
type CubicBezierCurve struct {
	P0 Point
	P1 Point
	P2 Point
	P3 Point
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

// NewPath returns a new empty path.
func NewPath() Path { return Path{} }
