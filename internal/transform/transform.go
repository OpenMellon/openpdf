// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package transform

import (
	"fmt"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (m Matrix) Unrealistic() bool {
	xx, xy, yx, yy := math.Abs(m[0]), math.Abs(m[1]), math.Abs(m[3]), math.Abs(m[4])
	diagonal := xx > minimumScale && yy > minimumScale
	offDiagonal := xy > minimumScale && yx > minimumScale
	return !(diagonal || offDiagonal)
}

func (p Point) Interpolate(b Point, t float64) Point {
	return Point{X: (1-t)*p.X + t*b.X, Y: (1-t)*p.Y + t*b.Y}
}

func (m *Matrix) Concat(b Matrix) {
	*m = Matrix{b[0]*m[0] + b[1]*m[3], b[0]*m[1] + b[1]*m[4], 0, b[3]*m[0] + b[4]*m[3], b[3]*m[1] + b[4]*m[4], 0, b[6]*m[0] + b[7]*m[3] + m[6], b[6]*m[1] + b[7]*m[4] + m[7], 1}
	m.clampRange()
}
func (m *Matrix) Shear(x, y float64) { m.Concat(ShearMatrix(x, y)) }
func (m Matrix) Angle() float64 {
	angle := math.Atan2(-m[1], m[0])
	if angle < 0.0 {
		angle += 2 * math.Pi
	}
	return angle / math.Pi * 180.0
}

func (m Matrix) String() string {
	a, b, c, d, tx, ty := m[0], m[1], m[3], m[4], m[6], m[7]
	return fmt.Sprintf("[%7.4f,%7.4f,%7.4f,%7.4f:%7.4f,%7.4f]", a, b, c, d, tx, ty)
}
func (m Matrix) Rotate(theta float64) Matrix { return m.Mult(RotationMatrix(theta)) }

const singularTolerance = 1e-10

func NewMatrix(a, b, c, d, tx, ty float64) Matrix {
	m := Matrix{a, b, 0, c, d, 0, tx, ty, 1}
	m.clampRange()
	return m
}
func TranslationMatrix(tx, ty float64) Matrix { return NewMatrix(1, 0, 0, 1, tx, ty) }
func ShearMatrix(x, y float64) Matrix         { return NewMatrix(1, y, x, 1, 0, 0) }
func RotationMatrix(angle float64) Matrix {
	cosine := math.Cos(angle)
	sine := math.Sin(angle)
	return NewMatrix(cosine, sine, -sine, cosine, 0, 0)
}

type Matrix [9]float64

func (m *Matrix) clampRange() {
	for i, value := range m {
		if value > maximumComponent {
			common.Log.Debug("CLAMP: %g -> %g", value, maximumComponent)
			m[i] = maximumComponent
		} else if value < -maximumComponent {
			common.Log.Debug("CLAMP: %g -> %g", value, -maximumComponent)
			m[i] = -maximumComponent
		}
	}
}
func (p *Point) transformByMatrix(m Matrix) { p.X, p.Y = m.Transform(p.X, p.Y) }
func NewMatrixFromTransforms(xScale, yScale, theta, tx, ty float64) Matrix {
	return IdentityMatrix().Scale(xScale, yScale).Rotate(theta).Translate(tx, ty)
}

const maximumComponent = 1e9

func NewPoint(x, y float64) Point { return Point{X: x, Y: y} }
func (m *Matrix) Set(a, b, c, d, tx, ty float64) {
	m[0], m[1] = a, b
	m[3], m[4] = c, d
	m[6], m[7] = tx, ty
	m.clampRange()
}

func (m *Matrix) Clone() Matrix {
	return NewMatrix(m[0], m[1], m[3], m[4], m[6], m[7])
}

type Point struct {
	X float64
	Y float64
}

func (p *Point) Set(x, y float64) { p.X, p.Y = x, y }
func (m Matrix) Singular() bool   { return math.Abs(m[0]*m[4]-m[1]*m[3]) < singularTolerance }
func (p *Point) Transform(a, b, c, d, tx, ty float64) {
	m := NewMatrix(a, b, c, d, tx, ty)
	p.transformByMatrix(m)
}

func (m Matrix) Identity() bool {
	return m[0] == 1 && m[1] == 0 && m[2] == 0 && m[3] == 0 && m[4] == 1 && m[5] == 0 && m[6] == 0 && m[7] == 0 && m[8] == 1
}
func (m Matrix) Translation() (float64, float64) { return m[6], m[7] }
func ScaleMatrix(x, y float64) Matrix            { return NewMatrix(x, 0, 0, y, 0, 0) }
func (p Point) Distance(b Point) float64         { return math.Hypot(p.X-b.X, p.Y-b.Y) }
func (m Matrix) ScalingFactorY() float64         { return math.Hypot(m[3], m[4]) }
func (p Point) Rotate(theta float64) Point {
	radius := math.Hypot(p.X, p.Y)
	angle := math.Atan2(p.Y, p.X)
	sine, cosine := math.Sincos(angle + theta/180.0*math.Pi)
	return Point{radius * cosine, radius * sine}
}

const inverseTolerance = 1.0e-6

func (m Matrix) ScalingFactorX() float64 { return math.Hypot(m[0], m[1]) }
func (m Matrix) Round(precision float64) Matrix {
	for i := range m {
		m[i] = math.Round(m[i]/precision) * precision
	}
	return m
}
func (p Point) Displace(delta Point) Point { return Point{p.X + delta.X, p.Y + delta.Y} }
func (p Point) String() string {
	return fmt.Sprintf("(%.2f,%.2f)", p.X, p.Y)
}
func (m Matrix) Translate(tx, ty float64) Matrix { return m.Mult(TranslationMatrix(tx, ty)) }
func (m Matrix) Inverse() (Matrix, bool) {
	a, b := m[0], m[1]
	c, d := m[3], m[4]
	tx, ty := m[6], m[7]
	determinant := a*d - b*c
	if math.Abs(determinant) < inverseTolerance {
		return Matrix{}, false
	}
	inverseA, inverseB := d/determinant, -b/determinant
	inverseC, inverseD := -c/determinant, a/determinant
	inverseTX := -(inverseA*tx + inverseC*ty)
	inverseTY := -(inverseB*tx + inverseD*ty)
	return NewMatrix(inverseA, inverseB, inverseC, inverseD, inverseTX, inverseTY), true
}

const minimumScale = 1e-6

func (m Matrix) Scale(xScale, yScale float64) Matrix { return m.Mult(ScaleMatrix(xScale, yScale)) }
func (m Matrix) Mult(b Matrix) Matrix                { m.Concat(b); return m }
func (m Matrix) Transform(x, y float64) (float64, float64) {
	transformedX := x*m[0] + y*m[3] + m[6]
	transformedY := x*m[1] + y*m[4] + m[7]
	return transformedX, transformedY
}

func MatrixFromPdfObject(obj core.PdfObject) (Matrix, error) {
	if obj == nil {
		return IdentityMatrix(), nil
	}
	array, ok := core.GetArray(obj)
	if !ok {
		return IdentityMatrix(), fmt.Errorf("%w: /Matrix is %T, not an array", core.ErrTypeError, core.TraceToDirectObject(obj))
	}
	if array == nil {
		return IdentityMatrix(), nil
	}
	numbers, err := core.GetNumbersAsFloat(array.Elements())
	if err != nil {
		return IdentityMatrix(), fmt.Errorf("%w: /Matrix %s: %v", core.ErrTypeError, array, err)
	}
	if len(numbers) != 6 {
		return IdentityMatrix(), fmt.Errorf("%w: /Matrix has %d numbers, not 6", core.ErrRangeError, len(numbers))
	}
	return NewMatrix(numbers[0], numbers[1], numbers[2], numbers[3], numbers[4], numbers[5]), nil
}
func IdentityMatrix() Matrix { return NewMatrix(1, 0, 0, 1, 0, 0) }
