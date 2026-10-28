package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// GetNumComponents returns the number of color components of the underlying
// colorspace device. Returns 0 if there is no underlying colorspace (e.g. an
// uncolored pattern).
func (cs *PdfColorspaceSpecialPattern) GetNumComponents() int {
	if cs.UnderlyingCS == nil {
		return 0
	}
	return cs.UnderlyingCS.GetNumComponents()
}

// ImageToRGB returns an error since an image cannot be defined in a pattern colorspace.
func (cs *PdfColorspaceSpecialPattern) ImageToRGB(img Image) (Image, error) {
	common.Log.Debug("Error: Image cannot be specified in Pattern colorspace")
	return img, errors.New("invalid colorspace for image (pattern)")
}

// ColorFromPdfObjects loads the color from PDF objects.
// The first objects (if present) represent the color in underlying colorspace.  The last one represents
// the name of the pattern.
func (cs *PdfColorspaceSpecialPattern) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) < 1 {
		return nil, errors.New("invalid number of parameters")
	}
	patternColor := &PdfColorPattern{}
	name, ok := objects[len(objects)-1].(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("Pattern name not a name (got %T)", objects[len(objects)-1])
		return nil, ErrTypeCheck
	}
	patternColor.PatternName = *name
	if len(objects) > 1 {
		objs := objects[0 : len(objects)-1]
		if cs.UnderlyingCS == nil {
			common.Log.Debug("Pattern color with defined color components but underlying cs missing")
			return nil, errors.New("underlying CS not defined")
		}
		col, err := cs.UnderlyingCS.ColorFromPdfObjects(objs)
		if err != nil {
			common.Log.Debug("ERROR: Unable to convert color via underlying cs: %v", err)
			return nil, err
		}
		patternColor.Color = col
	}
	return patternColor, nil
}

// NewPdfColorspaceSpecialPattern returns a new pattern color.
func NewPdfColorspaceSpecialPattern() *PdfColorspaceSpecialPattern {
	return &PdfColorspaceSpecialPattern{}
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceSpecialPattern) ToPdfObject() core.PdfObject {
	if cs.UnderlyingCS == nil {
		return core.MakeName("Pattern")
	}
	array := core.MakeArray(core.MakeName("Pattern"))
	array.Append(cs.UnderlyingCS.ToPdfObject())
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

// DecodeArray returns an empty slice as there are no components associated with pattern colorspace.
func (cs *PdfColorspaceSpecialPattern) DecodeArray() []float64 { return []float64{} }

// ColorToRGB only converts color used with uncolored patterns (defined in underlying colorspace).  Does not go into the
// pattern objects and convert those.  If that is desired, needs to be done separately.  See for example
// grayscale conversion example in examples.
func (cs *PdfColorspaceSpecialPattern) ColorToRGB(color PdfColor) (PdfColor, error) {
	patternColor, ok := color.(*PdfColorPattern)
	if !ok {
		common.Log.Debug("Color not pattern (got %T)", color)
		return nil, ErrTypeCheck
	}
	if patternColor.Color == nil {
		return color, nil
	}
	if cs.UnderlyingCS == nil {
		return nil, errors.New("underlying CS not defined")
	}
	return cs.UnderlyingCS.ColorToRGB(patternColor.Color)
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components.
func (cs *PdfColorspaceSpecialPattern) ColorFromFloats(vals []float64) (PdfColor, error) {
	if cs.UnderlyingCS == nil {
		return nil, errors.New("underlying CS not specified")
	}
	return cs.UnderlyingCS.ColorFromFloats(vals)
}

func (cs *PdfColorspaceSpecialPattern) String() string { return "Pattern" }

// PdfColorspaceSpecialPattern is a Pattern colorspace.
// Can be defined either as /Pattern or with an underlying colorspace [/Pattern cs].
type PdfColorspaceSpecialPattern struct {
	UnderlyingCS PdfColorspace
	container    *core.PdfIndirectObject
}

func newPdfColorspaceSpecialPatternFromPdfObject(obj core.PdfObject) (*PdfColorspaceSpecialPattern, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("New Pattern CS from obj: %s %T", obj.String(), obj)
	}
	specialPatternCS := NewPdfColorspaceSpecialPattern()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		specialPatternCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	if name, ok := obj.(*core.PdfObjectName); ok {
		if *name != "Pattern" {
			return nil, fmt.Errorf("invalid name")
		}
		return specialPatternCS, nil
	}
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		common.Log.Error("Invalid Pattern CS Object: %#v", obj)
		return nil, fmt.Errorf("invalid Pattern CS object")
	}
	if arr.Len() != 1 && arr.Len() != 2 {
		common.Log.Error("Invalid Pattern CS array: %#v", arr)
		return nil, fmt.Errorf("invalid Pattern CS array")
	}
	obj = arr.Get(0)
	if name, ok := obj.(*core.PdfObjectName); ok {
		if *name != "Pattern" {
			common.Log.Error("Invalid Pattern CS array name: %#v", name)
			return nil, fmt.Errorf("invalid name")
		}
	}
	if arr.Len() > 1 {
		obj = arr.Get(1)
		obj = core.TraceToDirectObject(obj)
		cs, err := NewPdfColorspaceFromPdfObject(obj)
		if err != nil {
			return nil, err
		}
		specialPatternCS.UnderlyingCS = cs
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Returning Pattern with underlying cs: %T", specialPatternCS.UnderlyingCS)
	}
	return specialPatternCS, nil
}
