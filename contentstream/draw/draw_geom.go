package draw

import (
	"fmt"
	"math"

	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
)

// ToPdfRectangle returns the rectangle as a PDF rectangle.
func (r Rectangle) ToPdfRectangle() *model.PdfRectangle {
	return &model.PdfRectangle{Llx: r.X, Lly: r.Y, Urx: r.X + r.Width, Ury: r.Y + r.Height}
}

// ToPdfRectangle returns the bounding box as a PDF rectangle.
func (b BoundingBox) ToPdfRectangle() *model.PdfRectangle {
	return &model.PdfRectangle{Llx: b.X, Lly: b.Y, Urx: b.X + b.Width, Ury: b.Y + b.Height}
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

// NewPoint returns a new point with the coordinates x, y.
func NewPoint(x, y float64) Point { return Point{X: x, Y: y} }

// FlipX flips the sign of the Dx component of the vector.
func (v Vector) FlipX() Vector { v.Dx = -v.Dx; return v }
func (p Point) String() string { return fmt.Sprintf("(%.1f,%.1f)", p.X, p.Y) }

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

// GetPolarAngle returns the angle the magnitude of the vector forms with the
// positive X-axis going counterclockwise.
func (v Vector) GetPolarAngle() float64 { return math.Atan2(v.Dy, v.Dx) }

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

// Scale scales the vector by the specified factor.
func (v Vector) Scale(factor float64) Vector {
	mag := v.Magnitude()
	angle := v.GetPolarAngle()
	v.Dx = factor * mag * math.Cos(angle)
	v.Dy = factor * mag * math.Sin(angle)
	return v
}

// AddVector adds vector to a point.
func (p Point) AddVector(v Vector) Point { p.X += v.Dx; p.Y += v.Dy; return p }

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
