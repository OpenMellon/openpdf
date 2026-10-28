package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/sampling"
)

// PdfColorspaceSpecialPattern is a Pattern colorspace.
// Can be defined either as /Pattern or with an underlying colorspace [/Pattern cs].
type PdfColorspaceSpecialPattern struct {
	UnderlyingCS PdfColorspace
	container    *core.PdfIndirectObject
}

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

// PdfColorspaceSpecialSeparation is a Separation colorspace.
// At the moment the colour space is set to a Separation space, the conforming reader shall determine whether the
// device has an available colorant (e.g. dye) corresponding to the name of the requested space. If so, the conforming
// reader shall ignore the alternateSpace and tintTransform parameters; subsequent painting operations within the
// space shall apply the designated colorant directly, according to the tint values supplied.
//
// Format: [/Separation name alternateSpace tintTransform]
type PdfColorspaceSpecialSeparation struct {
	ColorantName   *core.PdfObjectName
	AlternateSpace PdfColorspace
	TintTransform  PdfFunction
	container      *core.PdfIndirectObject
}

func newPdfColorspaceSpecialSeparationFromPdfObject(obj core.PdfObject) (*PdfColorspaceSpecialSeparation, error) {
	specialSeparationCS := NewPdfColorspaceSpecialSeparation()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		specialSeparationCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("separation CS: Invalid object")
	}
	if arr.Len() != 4 {
		return nil, fmt.Errorf("separation CS: Incorrect array length")
	}
	obj = arr.Get(0)
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("separation CS: invalid family name")
	}
	if *name != "Separation" {
		return nil, fmt.Errorf("separation CS: wrong family name")
	}
	obj = arr.Get(1)
	name, ok = obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("separation CS: Invalid colorant name")
	}
	specialSeparationCS.ColorantName = name
	obj = arr.Get(2)
	cs, err := NewPdfColorspaceFromPdfObject(obj)
	if err != nil {
		return nil, err
	}
	specialSeparationCS.AlternateSpace = cs
	function, err := newPdfFunctionFromPdfObject(arr.Get(3))
	if err != nil {
		return nil, err
	}
	specialSeparationCS.TintTransform = function
	return specialSeparationCS, nil
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain a single PdfObjectFloat element.
func (cs *PdfColorspaceSpecialSeparation) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 1 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// ImageToRGB converts an image with samples in Separation CS to an image with samples specified in
// DeviceRGB CS.
func (cs *PdfColorspaceSpecialSeparation) ImageToRGB(img Image) (Image, error) {
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), cs.AlternateSpace.GetNumComponents(), nil, img.alpha, nil)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Separation color space -> ToRGB conversion")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("TintTransform: %+v", cs.TintTransform)
	}
	array := cs.AlternateSpace.DecodeArray()
	var (
		n   uint32
		err error
	)
	for {
		n, err = reader.ReadSample()
		if err == io.EOF {
			break
		}
		if err != nil {
			return img, err
		}
		v := float64(n) / f
		evaluate, err := cs.TintTransform.Evaluate([]float64{v})
		if err != nil {
			return img, err
		}
		for i, v := range evaluate {
			linearInterpolate := imageutil.LinearInterpolate(v, array[i*2], array[i*2+1], 0, 1)
			if err = writer.WriteSample(uint32(linearInterpolate * f)); err != nil {
				return img, err
			}
		}
	}
	return cs.AlternateSpace.ImageToRGB(imageFromImageBase(&imageBase))
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceSpecialSeparation) ToPdfObject() core.PdfObject {
	array := core.MakeArray(core.MakeName("Separation"))
	array.Append(cs.ColorantName)
	array.Append(cs.AlternateSpace.ToPdfObject())
	array.Append(cs.TintTransform.ToPdfObject())
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

func (cs *PdfColorspaceSpecialSeparation) String() string { return "Separation" }

// ColorToRGB converts a color in Separation colorspace to RGB colorspace.
func (cs *PdfColorspaceSpecialSeparation) ColorToRGB(color PdfColor) (PdfColor, error) {
	if cs.AlternateSpace == nil {
		return nil, errors.New("alternate colorspace undefined")
	}
	return cs.AlternateSpace.ColorToRGB(color)
}

// NewPdfColorspaceSpecialSeparation returns a new separation color.
func NewPdfColorspaceSpecialSeparation() *PdfColorspaceSpecialSeparation {
	specialSeparationCS := &PdfColorspaceSpecialSeparation{}
	return specialSeparationCS
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain a single element.
func (cs *PdfColorspaceSpecialSeparation) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 1 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	floats := []float64{val}
	evaluate, err := cs.TintTransform.Evaluate(floats)
	if err != nil {
		common.Log.Debug("Error, failed to evaluate: %v", err)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Tint transform: %+v", cs.TintTransform)
		}
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Processing ColorFromFloats(%+v) on AlternateSpace: %#v", evaluate, cs.AlternateSpace)
	}
	col, err := cs.AlternateSpace.ColorFromFloats(evaluate)
	if err != nil {
		common.Log.Debug("Error, failed to evaluate in alternate space: %v", err)
		return nil, err
	}
	return col, nil
}

// GetNumComponents returns the number of color components (1 for Separation).
func (cs *PdfColorspaceSpecialSeparation) GetNumComponents() int { return 1 }

// DecodeArray returns the component range values for the Separation colorspace.
func (cs *PdfColorspaceSpecialSeparation) DecodeArray() []float64 { return []float64{0, 1.0} }

// PdfColorspace interface defines the common methods of a PDF colorspace.
// The colorspace defines the data storage format for each color and color representation.
//
// Device based colorspace, specified by name
// - /DeviceGray
// - /DeviceRGB
// - /DeviceCMYK
//
// CIE based colorspace specified by [name, dictionary]
// - [/CalGray dict]
// - [/CalRGB dict]
// - [/Lab dict]
// - [/ICCBased dict]
//
// Special colorspaces
// - /Pattern
// - /Indexed
// - /Separation
// - /DeviceN
//
// Work is in progress to support all colorspaces. At the moment ICCBased color spaces fall back to the alternate
// colorspace which works OK in most cases. For full color support, will need fully featured ICC support.
type PdfColorspace interface {
	// String returns the PdfColorspace's name.
	String() string

	// ImageToRGB converts an Image in a given PdfColorspace to an RGB image.
	ImageToRGB(Image) (Image, error)

	// ColorToRGB converts a single color in a given PdfColorspace to an RGB color.
	ColorToRGB(col PdfColor) (PdfColor, error)

	// GetNumComponents returns the number of components in the PdfColorspace.
	GetNumComponents() int

	// ToPdfObject returns a PdfObject representation of the PdfColorspace.
	ToPdfObject() core.PdfObject

	// ColorFromPdfObjects returns a PdfColor in the given PdfColorspace from an array of PdfObject where each
	// PdfObject represents a numeric value.
	ColorFromPdfObjects(objs []core.PdfObject) (PdfColor, error)

	// ColorFromFloats returns a new PdfColor based on input color components for a given PdfColorspace.
	ColorFromFloats(floats []float64) (PdfColor, error)

	// DecodeArray returns the Decode array for the PdfColorSpace, i.e. the range of each component.
	DecodeArray() []float64
}

// NewPdfColorspaceFromPdfObject loads a PdfColorspace from a PdfObject.  Returns an error if there is
// a failure in loading.
func NewPdfColorspaceFromPdfObject(obj core.PdfObject) (PdfColorspace, error) {
	if obj == nil {
		return nil, nil
	}
	var indirect *core.PdfIndirectObject
	var name *core.PdfObjectName
	var arr *core.PdfObjectArray
	if indirect2, ok := obj.(*core.PdfIndirectObject); ok {
		indirect = indirect2
	}
	obj = core.TraceToDirectObject(obj)
	switch v := obj.(type) {
	case *core.PdfObjectArray:
		arr = v
	case *core.PdfObjectName:
		name = v
	}
	if name != nil {
		switch *name {
		case "DeviceGray":
			return NewPdfColorspaceDeviceGray(), nil
		case "DeviceRGB":
			return NewPdfColorspaceDeviceRGB(), nil
		case "DeviceCMYK":
			return NewPdfColorspaceDeviceCMYK(), nil
		case "Pattern":
			return NewPdfColorspaceSpecialPattern(), nil
		default:
			common.Log.Debug("ERROR: Unknown colorspace %s", *name)
			return nil, errRangeCheck
		}
	}
	if arr != nil && arr.Len() > 0 {
		var obj core.PdfObject = indirect
		if indirect == nil {
			obj = arr
		}
		if name, ok := core.GetName(arr.Get(0)); ok {
			switch name.String() {
			case "DeviceGray":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceGray(), nil
				}
			case "DeviceRGB":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceRGB(), nil
				}
			case "DeviceCMYK":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceCMYK(), nil
				}
			case "CalGray":
				return newPdfColorspaceCalGrayFromPdfObject(obj)
			case "CalRGB":
				return newPdfColorspaceCalRGBFromPdfObject(obj)
			case "Lab":
				return newPdfColorspaceLabFromPdfObject(obj)
			case "ICCBased":
				return newPdfColorspaceICCBasedFromPdfObject(obj)
			case "Pattern":
				return newPdfColorspaceSpecialPatternFromPdfObject(obj)
			case "Indexed":
				return newPdfColorspaceSpecialIndexedFromPdfObject(obj)
			case "Separation":
				return newPdfColorspaceSpecialSeparationFromPdfObject(obj)
			case "DeviceN":
				return newPdfColorspaceDeviceNFromPdfObject(obj)
			default:
				common.Log.Debug("Array with invalid name: %s", *name)
			}
		}
	}
	common.Log.Debug("PDF File Error: Colorspace type error: %s", obj.String())
	return nil, ErrTypeCheck
}
