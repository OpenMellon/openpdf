package contentstream

import (
	"math"

	"github.com/OpenMellon/openpdf/core"
)

// Add_re appends 're' operand to the content stream:
// Append a rectangle to the current path as a complete subpath, with lower left corner (x,y).
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_re(x, y, width, height float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "re"
	op.Params = floatsToObjects([]float64{x, y, width, height})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_M adds 'M' operand to the content stream: Set the miter limit (graphics state).
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_M(miterlimit float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "M"
	op.Params = floatsToObjects([]float64{miterlimit})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Bytes converts the content stream operations to a content stream byte presentation, i.e. the kind that can be
// stored as a PDF stream or string format.
func (cc *ContentCreator) Bytes() []byte { return cc.operations.Bytes() }

// String is same as Bytes() except returns as a string for convenience.
func (cc *ContentCreator) String() string { return string(cc.operations.Bytes()) }

// Scale applies x-y scaling to the transformation matrix.
func (cc *ContentCreator) Scale(sx, sy float64) *ContentCreator {
	return cc.Add_cm(sx, 0, 0, sy, 0, 0)
}

// Add_cm adds 'cm' operation to the content stream: Modifies the current transformation matrix (ctm)
// of the graphics state.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_cm(a, b, c, d, e, f float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "cm"
	op.Params = floatsToObjects([]float64{a, b, c, d, e, f})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_d adds 'd' operand to the content stream: Set the line dash pattern.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_d(dashArray []int64, dashPhase int64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "d"
	op.Params = []core.PdfObject{}
	op.Params = append(op.Params, core.MakeArrayFromIntegers64(dashArray))
	op.Params = append(op.Params, core.MakeInteger(dashPhase))
	cc.operations = append(cc.operations, &op)
	return cc
}

// Translate applies a simple x-y translation to the transformation matrix.
func (cc *ContentCreator) Translate(tx, ty float64) *ContentCreator {
	return cc.Add_cm(1, 0, 0, 1, tx, ty)
}

// Add_m adds 'm' operand to the content stream: Move the current point to (x,y).
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_m(x, y float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "m"
	op.Params = floatsToObjects([]float64{x, y})
	cc.operations = append(cc.operations, &op)
	return cc
}

// AddOperand adds a specified operand.
func (cc *ContentCreator) AddOperand(op ContentStreamOperation) *ContentCreator {
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_J adds 'J' operand to the content stream: Set the line cap style (graphics state).
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_J(lineCapStyle string) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "J"
	op.Params = namesToObjects([]core.PdfObjectName{core.PdfObjectName(lineCapStyle)})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_b_starred appends 'b*' operand to the content stream:
// Close, fill and then stroke the path (even-odd winding number rule).
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_b_starred() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "b*"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_c adds 'c' operand to the content stream: Append a Bezier curve to the current path from
// the current point to (x3,y3) with (x1,x1) and (x2,y2) as control points.
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_c(x1, y1, x2, y2, x3, y3 float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "c"
	op.Params = floatsToObjects([]float64{x1, y1, x2, y2, x3, y3})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_W appends 'W' operand to the content stream:
// Modify the current clipping path by intersecting with the current path (nonzero winding rule).
//
// See section 8.5.4 "Clipping Path Operators" and Table 61 (p. 146 PDF32000_2008).
func (cc *ContentCreator) Add_W() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "W"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_w adds 'w' operand to the content stream, which sets the line width.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_w(lineWidth float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "w"
	op.Params = floatsToObjects([]float64{lineWidth})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_B appends 'B' operand to the content stream:
// Fill and then stroke the path (nonzero winding number rule).
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_B() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "B"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_gs adds 'gs' operand to the content stream: Set the graphics state.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_gs(dictName core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "gs"
	op.Params = namesToObjects([]core.PdfObjectName{dictName})
	cc.operations = append(cc.operations, &op)
	return cc
}

// RotateDeg applies a rotation to the transformation matrix.
func (cc *ContentCreator) RotateDeg(angle float64) *ContentCreator {
	f := math.Cos(angle * math.Pi / 180.0)
	f2 := math.Sin(angle * math.Pi / 180.0)
	f3 := -math.Sin(angle * math.Pi / 180.0)
	f4 := math.Cos(angle * math.Pi / 180.0)
	return cc.Add_cm(f, f2, f3, f4, 0, 0)
}

// ContentCreator is a builder for PDF content streams.
type ContentCreator struct{ operations ContentStreamOperations }

// Add_v appends 'v' operand to the content stream: Append a Bezier curve to the current path from the
// current point to (x3,y3) with the current point and (x2,y2) as control points.
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_v(x2, y2, x3, y3 float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "v"
	op.Params = floatsToObjects([]float64{x2, y2, x3, y3})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_S appends 'S' operand to the content stream: Stroke the path.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_S() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "S"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_q adds 'q' operand to the content stream: Pushes the current graphics state on the stack.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_q() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "q"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_l adds 'l' operand to the content stream:
// Append a straight line segment from the current point to (x,y).
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_l(x, y float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "l"
	op.Params = floatsToObjects([]float64{x, y})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_ri adds 'ri' operand to the content stream, which sets the color rendering intent.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_ri(intent core.PdfObjectName) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "ri"
	op.Params = namesToObjects([]core.PdfObjectName{intent})
	cc.operations = append(cc.operations, &op)
	return cc
}

func integersToObjects(v []int64) []core.PdfObject {
	var objs []core.PdfObject
	for _, n := range v {
		objs = append(objs, core.MakeInteger(n))
	}
	return objs
}

// Add_Q adds 'Q' operand to the content stream: Pops the most recently stored state from the stack.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_Q() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "Q"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Operations returns the list of operations.
func (cc *ContentCreator) Operations() *ContentStreamOperations { return &cc.operations }

// Wrap ensures that the contentstream is wrapped within a balanced q ... Q expression.
func (cc *ContentCreator) Wrap() { cc.operations.WrapIfNeeded() }

// Add_W_starred appends 'W*' operand to the content stream:
// Modify the current clipping path by intersecting with the current path (even odd rule).
//
// See section 8.5.4 "Clipping Path Operators" and Table 61 (p. 146 PDF32000_2008).
func (cc *ContentCreator) Add_W_starred() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "W*"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_n appends 'n' operand to the content stream:
// End the path without filling or stroking.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_n() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "n"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_i adds 'i' operand to the content stream: Set the flatness tolerance in the graphics state.
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_i(flatness float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "i"
	op.Params = floatsToObjects([]float64{flatness})
	cc.operations = append(cc.operations, &op)
	return cc
}

func floatsToObjects(floats []float64) []core.PdfObject {
	var objs []core.PdfObject
	for _, f := range floats {
		objs = append(objs, core.MakeFloat(f))
	}
	return objs
}

// Add_f_starred appends 'f*' operand to the content stream.
// f*: Fill the path using the even-odd rule to determine fill region.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_f_starred() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "f*"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_b appends 'b' operand to the content stream:
// Close, fill and then stroke the path (nonzero winding number rule).
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_b() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "b"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_f appends 'f' operand to the content stream:
// Fill the path using the nonzero winding number rule to determine fill region.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_f() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "f"
	cc.operations = append(cc.operations, &op)
	return cc
}

func stringsToObjects(v []core.PdfObjectString) []core.PdfObject {
	var objs []core.PdfObject
	for _, v2 := range v {
		objs = append(objs, core.MakeString(v2.Str()))
	}
	return objs
}

// Add_h appends 'h' operand to the content stream:
// Close the current subpath by adding a line between the current position and the starting position.
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_h() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "h"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_s appends 's' operand to the content stream: Close and stroke the path.
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_s() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "s"
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_j adds 'j' operand to the content stream: Set the line join style (graphics state).
//
// See section 8.4.4 "Graphic State Operators" and Table 57 (pp. 135-136 PDF32000_2008).
func (cc *ContentCreator) Add_j(lineJoinStyle string) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "j"
	op.Params = namesToObjects([]core.PdfObjectName{core.PdfObjectName(lineJoinStyle)})
	cc.operations = append(cc.operations, &op)
	return cc
}

// NewContentCreator returns a new initialized ContentCreator.
func NewContentCreator() *ContentCreator {
	cc := &ContentCreator{}
	cc.operations = ContentStreamOperations{}
	return cc
}

// Add_y appends 'y' operand to the content stream: Append a Bezier curve to the current path from the
// current point to (x3,y3) with (x1, y1) and (x3,y3) as control points.
//
// See section 8.5.2 "Path Construction Operators" and Table 59 (pp. 140-141 PDF32000_2008).
func (cc *ContentCreator) Add_y(x1, y1, x3, y3 float64) *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "y"
	op.Params = floatsToObjects([]float64{x1, y1, x3, y3})
	cc.operations = append(cc.operations, &op)
	return cc
}

// Add_B_starred appends 'B*' operand to the content stream:
// Fill and then stroke the path (even-odd rule).
//
// See section 8.5.3 "Path Painting Operators" and Table 60 (p. 143 PDF32000_2008).
func (cc *ContentCreator) Add_B_starred() *ContentCreator {
	op := ContentStreamOperation{}
	op.Operand = "B*"
	cc.operations = append(cc.operations, &op)
	return cc
}

func namesToObjects(v []core.PdfObjectName) []core.PdfObject {
	var objs []core.PdfObject
	for _, name := range v {
		objs = append(objs, core.MakeName(string(name)))
	}
	return objs
}
