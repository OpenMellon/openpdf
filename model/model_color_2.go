package model

import (
	_ "image/gif"

	"github.com/OpenMellon/openpdf/core"
)

func colorFromArray(arr *core.PdfObjectArray) PdfColor {
	if arr == nil {
		return nil
	}
	floats := make([]float64, 0, arr.Len())
	for _, obj := range arr.Elements() {
		f, err := core.GetNumberAsFloat(obj)
		if err != nil {
			return nil
		}
		floats = append(floats, f)
	}
	switch len(floats) {
	case 1:
		col, err := NewPdfColorspaceDeviceGray().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	case 3:
		col, err := NewPdfColorspaceDeviceRGB().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	case 4:
		col, err := NewPdfColorspaceDeviceCMYK().ColorFromFloats(floats)
		if err != nil {
			return nil
		}
		return col
	}
	return nil
}

// PdfColor interface represents a generic color in PDF.
type PdfColor interface {
	// Components returns the color's component values in its own colorspace.
	Components() []float64

	// Colorspace returns the colorspace family this color belongs to.
	// For Cal/Lab colors this is a best-effort, parameterless instance; the
	// authoritative parameterized colorspace is tracked by the graphics state.
	Colorspace() PdfColorspace
}
