package model

import (
	"errors"
	_ "image/gif"
	"math"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
)

// PdfRectangle is a definition of a rectangle.
type PdfRectangle struct {
	Llx float64
	Lly float64
	Urx float64
	Ury float64
}

// Height returns the height of `rect`.
func (r *PdfRectangle) Height() float64 { return math.Abs(r.Ury - r.Lly) }

// Width returns the width of `rect`.
func (r *PdfRectangle) Width() float64 { return math.Abs(r.Urx - r.Llx) }

// ToPdfObject converts rectangle to a PDF object.
func (r *PdfRectangle) ToPdfObject() core.PdfObject {
	return core.MakeArray(core.MakeFloat(r.Llx), core.MakeFloat(r.Lly), core.MakeFloat(r.Urx), core.MakeFloat(r.Ury))
}

// Transform rectangle with the supplied matrix.
func (r *PdfRectangle) Transform(transformMatrix transform.Matrix) {
	r.Llx, r.Lly = transformMatrix.Transform(r.Llx, r.Lly)
	r.Urx, r.Ury = transformMatrix.Transform(r.Urx, r.Ury)
	r.Normalize()
}

// Normalize swaps (Llx,Urx) if Urx < Llx, and (Lly,Ury) if Ury < Lly.
func (r *PdfRectangle) Normalize() {
	if r.Llx > r.Urx {
		r.Llx, r.Urx = r.Urx, r.Llx
	}
	if r.Lly > r.Ury {
		r.Lly, r.Ury = r.Ury, r.Lly
	}
}

// NewPdfRectangle creates a PDF rectangle object based on an input array of 4 integers.
// Defining the lower left (LL) and upper right (UR) corners with
// floating point numbers.
func NewPdfRectangle(arr core.PdfObjectArray) (*PdfRectangle, error) {
	rect := PdfRectangle{}
	if arr.Len() != 4 {
		return nil, errors.New("invalid rectangle array, len != 4")
	}
	var err error
	rect.Llx, err = core.GetNumberAsFloat(arr.Get(0))
	if err != nil {
		return nil, err
	}
	rect.Lly, err = core.GetNumberAsFloat(arr.Get(1))
	if err != nil {
		return nil, err
	}
	rect.Urx, err = core.GetNumberAsFloat(arr.Get(2))
	if err != nil {
		return nil, err
	}
	rect.Ury, err = core.GetNumberAsFloat(arr.Get(3))
	if err != nil {
		return nil, err
	}
	return &rect, nil
}

var (
	TabOrderRow       TabOrderType = "R"
	TabOrderColumn    TabOrderType = "C"
	TabOrderStructure TabOrderType = "S"
)

// Tab order types.
type TabOrderType string

// ContentStreamWrapper wraps the Page's contentstream into q ... Q blocks.
type ContentStreamWrapper interface{ WrapContentStream(page *PdfPage) error }
