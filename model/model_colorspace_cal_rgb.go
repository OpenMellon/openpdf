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

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain three PdfObjectFloat elements representing
// the A, B and C components of the color.
func (cs *PdfColorspaceCalRGB) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 3 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// ColorToRGB converts a CalRGB color to an RGB color.
func (cs *PdfColorspaceCalRGB) ColorToRGB(color PdfColor) (PdfColor, error) {
	calRGBColor, ok := color.(*PdfColorCalRGB)
	if !ok {
		common.Log.Debug("Input color not cal rgb")
		return nil, errors.New("type check error")
	}
	f := calRGBColor.A()
	v := calRGBColor.B()
	val := calRGBColor.C()
	X := cs.Matrix[0]*math.Pow(f, cs.Gamma[0]) + cs.Matrix[3]*math.Pow(v, cs.Gamma[1]) + cs.Matrix[6]*math.Pow(val, cs.Gamma[2])
	Y := cs.Matrix[1]*math.Pow(f, cs.Gamma[0]) + cs.Matrix[4]*math.Pow(v, cs.Gamma[1]) + cs.Matrix[7]*math.Pow(val, cs.Gamma[2])
	Z := cs.Matrix[2]*math.Pow(f, cs.Gamma[0]) + cs.Matrix[5]*math.Pow(v, cs.Gamma[1]) + cs.Matrix[8]*math.Pow(val, cs.Gamma[2])
	x := 3.240479*X + -1.537150*Y + -0.498535*Z
	y := -0.969256*X + 1.875992*Y + 0.041556*Z
	y2 := 0.055648*X + -0.204043*Y + 1.057311*Z
	x = math.Min(math.Max(x, 0), 1.0)
	y = math.Min(math.Max(y, 0), 1.0)
	y2 = math.Min(math.Max(y2, 0), 1.0)
	return NewPdfColorDeviceRGB(x, y, y2), nil
}

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 3 for a CalRGB device.
func (cs *PdfColorspaceCalRGB) GetNumComponents() int { return 3 }

// PdfColorspaceCalRGB stores A, B, C components
type PdfColorspaceCalRGB struct {
	WhitePoint []float64
	BlackPoint []float64
	Gamma      []float64
	Matrix     []float64
	dict       *core.PdfObjectDictionary
	container  *core.PdfIndirectObject
}

// ImageToRGB converts CalRGB colorspace image to RGB and returns the result.
func (cs *PdfColorspaceCalRGB) ImageToRGB(img Image) (Image, error) {
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), 3, nil, nil, nil)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	nums := make([]uint32, 3)
	var (
		err                  error
		v, val, x, y, y2, y3 float64
	)
	for {
		err = reader.ReadSamples(nums)
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		v = float64(nums[0]) / f
		val = float64(nums[1]) / f
		x = float64(nums[2]) / f
		y = cs.Matrix[0]*math.Pow(v, cs.Gamma[0]) + cs.Matrix[3]*math.Pow(val, cs.Gamma[1]) + cs.Matrix[6]*math.Pow(x, cs.Gamma[2])
		y2 = cs.Matrix[1]*math.Pow(v, cs.Gamma[0]) + cs.Matrix[4]*math.Pow(val, cs.Gamma[1]) + cs.Matrix[7]*math.Pow(x, cs.Gamma[2])
		y3 = cs.Matrix[2]*math.Pow(v, cs.Gamma[0]) + cs.Matrix[5]*math.Pow(val, cs.Gamma[1]) + cs.Matrix[8]*math.Pow(x, cs.Gamma[2])
		v = 3.240479*y + -1.537150*y2 + -0.498535*y3
		val = -0.969256*y + 1.875992*y2 + 0.041556*y3
		x = 0.055648*y + -0.204043*y2 + 1.057311*y3
		v = math.Min(math.Max(v, 0), 1.0)
		val = math.Min(math.Max(val, 0), 1.0)
		x = math.Min(math.Max(x, 0), 1.0)
		nums[0] = uint32(v * f)
		nums[1] = uint32(val * f)
		nums[2] = uint32(x * f)
		if err = writer.WriteSamples(nums); err != nil {
			return img, err
		}
	}
	return imageFromImageBase(&imageBase), nil
}

func newPdfColorspaceCalRGBFromPdfObject(obj core.PdfObject) (*PdfColorspaceCalRGB, error) {
	calRGBCS := NewPdfColorspaceCalRGB()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		calRGBCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("type error")
	}
	if arr.Len() != 2 {
		return nil, fmt.Errorf("invalid CalRGB colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(0))
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("CalRGB name not a Name object")
	}
	if *name != "CalRGB" {
		return nil, fmt.Errorf("not a CalRGB colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(1))
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("CalRGB name not a Name object")
	}
	obj = dict.Get("WhitePoint")
	obj = core.TraceToDirectObject(obj)
	arr2, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("CalRGB: Invalid WhitePoint")
	}
	if arr2.Len() != 3 {
		return nil, fmt.Errorf("CalRGB: Invalid WhitePoint array")
	}
	floats, err := arr2.GetAsFloat64Slice()
	if err != nil {
		return nil, err
	}
	calRGBCS.WhitePoint = floats
	obj = dict.Get("BlackPoint")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("CalRGB: Invalid BlackPoint")
		}
		if arr.Len() != 3 {
			return nil, fmt.Errorf("CalRGB: Invalid BlackPoint array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		calRGBCS.BlackPoint = floats
	}
	obj = dict.Get("Gamma")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("CalRGB: Invalid Gamma")
		}
		if arr.Len() != 3 {
			return nil, fmt.Errorf("CalRGB: Invalid Gamma array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		calRGBCS.Gamma = floats
	}
	obj = dict.Get("Matrix")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("CalRGB: Invalid Matrix")
		}
		if arr.Len() != 9 {
			common.Log.Error("Matrix array: %s", arr.String())
			return nil, fmt.Errorf("CalRGB: Invalid Matrix array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		calRGBCS.Matrix = floats
	}
	return calRGBCS, nil
}

// NewPdfColorspaceCalRGB returns a new CalRGB colorspace object.
func NewPdfColorspaceCalRGB() *PdfColorspaceCalRGB {
	calRGBCS := &PdfColorspaceCalRGB{}
	calRGBCS.BlackPoint = []float64{0.0, 0.0, 0.0}
	calRGBCS.Gamma = []float64{1.0, 1.0, 1.0}
	calRGBCS.Matrix = []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	return calRGBCS
}

// DecodeArray returns the range of color component values in CalRGB colorspace.
func (cs *PdfColorspaceCalRGB) DecodeArray() []float64 {
	return []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
}

// ToPdfObject returns colorspace in a PDF object format [name dictionary]
func (cs *PdfColorspaceCalRGB) ToPdfObject() core.PdfObject {
	arr := &core.PdfObjectArray{}
	arr.Append(core.MakeName("CalRGB"))
	dict := core.MakeDict()
	if cs.WhitePoint != nil {
		array := core.MakeArray(core.MakeFloat(cs.WhitePoint[0]), core.MakeFloat(cs.WhitePoint[1]), core.MakeFloat(cs.WhitePoint[2]))
		dict.Set("WhitePoint", array)
	} else {
		common.Log.Error("CalRGB: Missing WhitePoint (Required)")
	}
	if cs.BlackPoint != nil {
		array := core.MakeArray(core.MakeFloat(cs.BlackPoint[0]), core.MakeFloat(cs.BlackPoint[1]), core.MakeFloat(cs.BlackPoint[2]))
		dict.Set("BlackPoint", array)
	}
	if cs.Gamma != nil {
		array := core.MakeArray(core.MakeFloat(cs.Gamma[0]), core.MakeFloat(cs.Gamma[1]), core.MakeFloat(cs.Gamma[2]))
		dict.Set("Gamma", array)
	}
	if cs.Matrix != nil {
		array := core.MakeArray(core.MakeFloat(cs.Matrix[0]), core.MakeFloat(cs.Matrix[1]), core.MakeFloat(cs.Matrix[2]), core.MakeFloat(cs.Matrix[3]), core.MakeFloat(cs.Matrix[4]), core.MakeFloat(cs.Matrix[5]), core.MakeFloat(cs.Matrix[6]), core.MakeFloat(cs.Matrix[7]), core.MakeFloat(cs.Matrix[8]))
		dict.Set("Matrix", array)
	}
	arr.Append(dict)
	if cs.container != nil {
		cs.container.PdfObject = arr
		return cs.container
	}
	return arr
}

func (cs *PdfColorspaceCalRGB) String() string { return "CalRGB" }

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain three elements representing the
// A, B and C components of the color. The values of the elements should be
// between 0 and 1.
func (cs *PdfColorspaceCalRGB) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 3 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 1.0 {
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	f := vals[1]
	if f < 0.0 || f > 1.0 {
		common.Log.Debug("color out of CS range: ", f)
		return nil, ErrColorOutOfRange
	}
	v := vals[2]
	if v < 0.0 || v > 1.0 {
		common.Log.Debug("color out of CS range: ", v)
		return nil, ErrColorOutOfRange
	}
	calRGBColor := NewPdfColorCalRGB(val, f, v)
	return calRGBColor, nil
}
