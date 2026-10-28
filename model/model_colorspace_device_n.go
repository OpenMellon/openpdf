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

func newPdfColorspaceDeviceNFromPdfObject(obj core.PdfObject) (*PdfColorspaceDeviceN, error) {
	deviceNCS := NewPdfColorspaceDeviceN()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		deviceNCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("deviceN CS: Invalid object")
	}
	if arr.Len() != 4 && arr.Len() != 5 {
		return nil, fmt.Errorf("deviceN CS: Incorrect array length")
	}
	obj = arr.Get(0)
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("deviceN CS: invalid family name")
	}
	if *name != "DeviceN" {
		return nil, fmt.Errorf("deviceN CS: wrong family name")
	}
	obj = arr.Get(1)
	obj = core.TraceToDirectObject(obj)
	arr2, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("deviceN CS: Invalid names array")
	}
	deviceNCS.ColorantNames = arr2
	obj = arr.Get(2)
	cs, err := NewPdfColorspaceFromPdfObject(obj)
	if err != nil {
		return nil, err
	}
	deviceNCS.AlternateSpace = cs
	function, err := newPdfFunctionFromPdfObject(arr.Get(3))
	if err != nil {
		return nil, err
	}
	deviceNCS.TintTransform = function
	if arr.Len() == 5 {
		colorspaceDeviceNAttributes, err := newPdfColorspaceDeviceNAttributesFromPdfObject(arr.Get(4))
		if err != nil {
			return nil, err
		}
		deviceNCS.Attributes = colorspaceDeviceNAttributes
	}
	return deviceNCS, nil
}

// DecodeArray returns the component range values for the DeviceN colorspace.
// [0 1.0 0 1.0 ...] for each color component.
func (cs *PdfColorspaceDeviceN) DecodeArray() []float64 {
	var floats []float64
	for i := 0; i < cs.GetNumComponents(); i++ {
		floats = append(floats, 0.0, 1.0)
	}
	return floats
}

// ImageToRGB converts an Image in a given PdfColorspace to an RGB image.
func (cs *PdfColorspaceDeviceN) ImageToRGB(img Image) (Image, error) {
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, nil, img.alpha, img.decode)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	numComponents := cs.GetNumComponents()
	nums := make([]uint32, numComponents)
	floats := make([]float64, numComponents)
	for {
		err := reader.ReadSamples(nums)
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		for i := 0; i < numComponents; i++ {
			v := float64(nums[i]) / f
			floats[i] = v
		}
		evaluate, err := cs.TintTransform.Evaluate(floats)
		if err != nil {
			return img, err
		}
		for _, v := range evaluate {
			v = math.Min(math.Max(0, v), 1.0)
			if err = writer.WriteSample(uint32(v * f)); err != nil {
				return img, err
			}
		}
	}
	return cs.AlternateSpace.ImageToRGB(imageFromImageBase(&imageBase))
}

// GetNumComponents returns the number of input color components, i.e. that are input to the tint transform.
func (cs *PdfColorspaceDeviceN) GetNumComponents() int { return cs.ColorantNames.Len() }

// ColorFromFloats returns a new PdfColor based on input color components.
func (cs *PdfColorspaceDeviceN) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != cs.GetNumComponents() {
		return nil, errors.New("range check")
	}
	evaluate, err := cs.TintTransform.Evaluate(vals)
	if err != nil {
		return nil, err
	}
	col, err := cs.AlternateSpace.ColorFromFloats(evaluate)
	if err != nil {
		return nil, err
	}
	return col, nil
}

// ColorFromPdfObjects returns a new PdfColor based on input color components. The input PdfObjects should
// be numeric.
func (cs *PdfColorspaceDeviceN) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != cs.GetNumComponents() {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

func newPdfColorspaceDeviceNAttributesFromPdfObject(obj core.PdfObject) (*PdfColorspaceDeviceNAttributes, error) {
	deviceNAttributesCS := &PdfColorspaceDeviceNAttributes{}
	var dict *core.PdfObjectDictionary
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		deviceNAttributesCS.container = v
		var ok bool
		dict, ok = v.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			common.Log.Error("DeviceN attribute type error")
			return nil, errors.New("type error")
		}
	case *core.PdfObjectDictionary:
		dict = v
	case *core.PdfObjectReference:
		resolve := v.Resolve()
		return newPdfColorspaceDeviceNAttributesFromPdfObject(resolve)
	default:
		common.Log.Error("DeviceN attribute type error")
		return nil, errors.New("type error")
	}
	if subtype := dict.Get("Subtype"); subtype != nil {
		name, ok := core.TraceToDirectObject(subtype).(*core.PdfObjectName)
		if !ok {
			common.Log.Error("DeviceN attribute Subtype type error")
			return nil, errors.New("type error")
		}
		deviceNAttributesCS.Subtype = name
	}
	if colorants := dict.Get("Colorants"); colorants != nil {
		deviceNAttributesCS.Colorants = colorants
	}
	if process := dict.Get("Process"); process != nil {
		deviceNAttributesCS.Process = process
	}
	if mixingHints := dict.Get("MixingHints"); mixingHints != nil {
		deviceNAttributesCS.MixingHints = mixingHints
	}
	return deviceNAttributesCS, nil
}

// PdfColorspaceDeviceN represents a DeviceN color space. DeviceN color spaces are similar to Separation color
// spaces, except they can contain an arbitrary number of color components.
/*
	Format: [/DeviceN names alternateSpace tintTransform]
        or: [/DeviceN names alternateSpace tintTransform attributes]
*/
type PdfColorspaceDeviceN struct {
	ColorantNames  *core.PdfObjectArray
	AlternateSpace PdfColorspace
	TintTransform  PdfFunction
	Attributes     *PdfColorspaceDeviceNAttributes
	container      *core.PdfIndirectObject
}

// PdfColorspaceDeviceNAttributes contains additional information about the components of colour space that
// conforming readers may use. Conforming readers need not use the alternateSpace and tintTransform parameters,
// and may instead use a custom blending algorithms, along with other information provided in the attributes
// dictionary if present.
type PdfColorspaceDeviceNAttributes struct {
	Subtype     *core.PdfObjectName
	Colorants   core.PdfObject
	Process     core.PdfObject
	MixingHints core.PdfObject
	container   *core.PdfIndirectObject
}

// ToPdfObject returns a PdfObject representation of PdfColorspaceDeviceNAttributes as a PdfObjectDictionary directly
// or indirectly within an indirect object container.
func (cs *PdfColorspaceDeviceNAttributes) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if cs.Subtype != nil {
		dict.Set("Subtype", cs.Subtype)
	}
	dict.SetIfNotNil("Colorants", cs.Colorants)
	dict.SetIfNotNil("Process", cs.Process)
	dict.SetIfNotNil("MixingHints", cs.MixingHints)
	if cs.container != nil {
		cs.container.PdfObject = dict
		return cs.container
	}
	return dict
}

// ToPdfObject returns a *PdfIndirectObject containing a *PdfObjectArray representation of the DeviceN colorspace.
/*
	Format: [/DeviceN names alternateSpace tintTransform]
	    or: [/DeviceN names alternateSpace tintTransform attributes]
*/
func (cs *PdfColorspaceDeviceN) ToPdfObject() core.PdfObject {
	array := core.MakeArray(core.MakeName("DeviceN"))
	array.Append(cs.ColorantNames)
	array.Append(cs.AlternateSpace.ToPdfObject())
	array.Append(cs.TintTransform.ToPdfObject())
	if cs.Attributes != nil {
		array.Append(cs.Attributes.ToPdfObject())
	}
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

// NewPdfColorspaceDeviceN returns an initialized PdfColorspaceDeviceN.
func NewPdfColorspaceDeviceN() *PdfColorspaceDeviceN {
	deviceNCS := &PdfColorspaceDeviceN{}
	return deviceNCS
}

// String returns the name of the colorspace (DeviceN).
func (cs *PdfColorspaceDeviceN) String() string { return "DeviceN" }

// ColorToRGB converts a DeviceN color to an RGB color.
func (cs *PdfColorspaceDeviceN) ColorToRGB(color PdfColor) (PdfColor, error) {
	if cs.AlternateSpace == nil {
		return nil, errors.New("DeviceN alternate space undefined")
	}
	return cs.AlternateSpace.ColorToRGB(color)
}
