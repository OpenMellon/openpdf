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

// PdfColorspaceLab is a L*, a*, b* 3 component colorspace.
type PdfColorspaceLab struct {
	WhitePoint []float64
	BlackPoint []float64
	Range      []float64
	container  *core.PdfIndirectObject
}

func newPdfColorspaceLabFromPdfObject(obj core.PdfObject) (*PdfColorspaceLab, error) {
	labCS := NewPdfColorspaceLab()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		labCS.container = indirect
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
		return nil, fmt.Errorf("lab name not a Name object")
	}
	if *name != "Lab" {
		return nil, fmt.Errorf("not a Lab colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(1))
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("colorspace dictionary missing or invalid")
	}
	obj = dict.Get("WhitePoint")
	obj = core.TraceToDirectObject(obj)
	arr2, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("lab Invalid WhitePoint")
	}
	if arr2.Len() != 3 {
		return nil, fmt.Errorf("lab: Invalid WhitePoint array")
	}
	floats, err := arr2.GetAsFloat64Slice()
	if err != nil {
		return nil, err
	}
	labCS.WhitePoint = floats
	obj = dict.Get("BlackPoint")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("lab: Invalid BlackPoint")
		}
		if arr.Len() != 3 {
			return nil, fmt.Errorf("lab: Invalid BlackPoint array")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		labCS.BlackPoint = floats
	}
	obj = dict.Get("Range")
	if obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			common.Log.Error("Range type error")
			return nil, fmt.Errorf("lab: Type error")
		}
		if arr.Len() != 4 {
			common.Log.Error("Range range error")
			return nil, fmt.Errorf("lab: Range error")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		labCS.Range = floats
	}
	return labCS, nil
}

func (cs *PdfColorspaceLab) String() string { return "Lab" }

// NewPdfColorspaceLab returns a new Lab colorspace object.
func NewPdfColorspaceLab() *PdfColorspaceLab {
	labCS := &PdfColorspaceLab{}
	labCS.BlackPoint = []float64{0.0, 0.0, 0.0}
	labCS.Range = []float64{-100, 100, -100, 100}
	return labCS
}

// ToPdfObject returns colorspace in a PDF object format [name dictionary]
func (cs *PdfColorspaceLab) ToPdfObject() core.PdfObject {
	array := core.MakeArray()
	array.Append(core.MakeName("Lab"))
	dict := core.MakeDict()
	if cs.WhitePoint != nil {
		array := core.MakeArray(core.MakeFloat(cs.WhitePoint[0]), core.MakeFloat(cs.WhitePoint[1]), core.MakeFloat(cs.WhitePoint[2]))
		dict.Set("WhitePoint", array)
	} else {
		common.Log.Error("Lab: Missing WhitePoint (Required)")
	}
	if cs.BlackPoint != nil {
		array := core.MakeArray(core.MakeFloat(cs.BlackPoint[0]), core.MakeFloat(cs.BlackPoint[1]), core.MakeFloat(cs.BlackPoint[2]))
		dict.Set("BlackPoint", array)
	}
	if cs.Range != nil {
		array := core.MakeArray(core.MakeFloat(cs.Range[0]), core.MakeFloat(cs.Range[1]), core.MakeFloat(cs.Range[2]), core.MakeFloat(cs.Range[3]))
		dict.Set("Range", array)
	}
	array.Append(dict)
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain three elements representing the
// L (range 0-100), A (range -100-100) and B (range -100-100) components of
// the color.
func (cs *PdfColorspaceLab) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 3 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 100.0 {
		common.Log.Debug("L out of range (got %v should be 0-100)", val)
		return nil, ErrColorOutOfRange
	}
	f := vals[1]
	v := float64(-100)
	x := float64(100)
	if len(cs.Range) > 1 {
		v = cs.Range[0]
		x = cs.Range[1]
	}
	if f < v || f > x {
		common.Log.Debug("A out of range (got %v; range %v to %v)", f, v, x)
		return nil, ErrColorOutOfRange
	}
	y := vals[2]
	y2 := float64(-100)
	y3 := float64(100)
	if len(cs.Range) > 3 {
		y2 = cs.Range[2]
		y3 = cs.Range[3]
	}
	if y < y2 || y > y3 {
		common.Log.Debug("b out of range (got %v; range %v to %v)", y, y2, y3)
		return nil, ErrColorOutOfRange
	}
	labColor := NewPdfColorLab(val, f, y)
	return labColor, nil
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain three PdfObjectFloat elements representing
// the L, A and B components of the color.
func (cs *PdfColorspaceLab) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 3 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// ColorToRGB converts a Lab color to an RGB color.
func (cs *PdfColorspaceLab) ColorToRGB(color PdfColor) (PdfColor, error) {
	fn := func(f float64) float64 {
		if f >= 6.0/29 {
			return f * f * f
		}
		return 108.0 / 841 * (f - 4.0/29.0)
	}
	labColor, ok := color.(*PdfColorLab)
	if !ok {
		common.Log.Debug("input color not lab")
		return nil, errors.New("type check error")
	}
	LStar := labColor.L()
	AStar := labColor.A()
	BStar := labColor.B()
	L := (LStar+16)/116 + AStar/500
	M := (LStar + 16) / 116
	N := (LStar+16)/116 - BStar/200
	X := cs.WhitePoint[0] * fn(L)
	Y := cs.WhitePoint[1] * fn(M)
	Z := cs.WhitePoint[2] * fn(N)
	f := 3.240479*X + -1.537150*Y + -0.498535*Z
	v := -0.969256*X + 1.875992*Y + 0.041556*Z
	val := 0.055648*X + -0.204043*Y + 1.057311*Z
	f = math.Min(math.Max(f, 0), 1.0)
	v = math.Min(math.Max(v, 0), 1.0)
	val = math.Min(math.Max(val, 0), 1.0)
	return NewPdfColorDeviceRGB(f, v, val), nil
}

// ImageToRGB converts Lab colorspace image to RGB and returns the result.
func (cs *PdfColorspaceLab) ImageToRGB(img Image) (Image, error) {
	fn := func(f float64) float64 {
		if f >= 6.0/29 {
			return f * f * f
		}
		return 108.0 / 841 * (f - 4.0/29.0)
	}
	floats := img.decode
	if len(floats) != 6 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Image - Lab Decode range != 6... use [0 100 amin amax bmin bmax] default decode array")
		}
		floats = cs.DecodeArray()
	}
	reader := sampling.NewReader(img.getBase())
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), 3, nil, img.alpha, img.decode)
	writer := sampling.NewWriter(imageBase)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	nums := make([]uint32, 3)
	var (
		err                                     error
		Ls, As, Bs, L, M, N, X, Y, Z, v, val, x float64
	)
	for {
		err = reader.ReadSamples(nums)
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		Ls = float64(nums[0]) / f
		As = float64(nums[1]) / f
		Bs = float64(nums[2]) / f
		Ls = imageutil.LinearInterpolate(Ls, 0.0, 1.0, floats[0], floats[1])
		As = imageutil.LinearInterpolate(As, 0.0, 1.0, floats[2], floats[3])
		Bs = imageutil.LinearInterpolate(Bs, 0.0, 1.0, floats[4], floats[5])
		L = (Ls+16)/116 + As/500
		M = (Ls + 16) / 116
		N = (Ls+16)/116 - Bs/200
		X = cs.WhitePoint[0] * fn(L)
		Y = cs.WhitePoint[1] * fn(M)
		Z = cs.WhitePoint[2] * fn(N)
		v = 3.240479*X + -1.537150*Y + -0.498535*Z
		val = -0.969256*X + 1.875992*Y + 0.041556*Z
		x = 0.055648*X + -0.204043*Y + 1.057311*Z
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

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 3 for a Lab device.
func (cs *PdfColorspaceLab) GetNumComponents() int { return 3 }

// DecodeArray returns the range of color component values in the Lab colorspace.
func (cs *PdfColorspaceLab) DecodeArray() []float64 {
	floats := []float64{0, 100}
	if cs != nil && cs.Range != nil && len(cs.Range) == 4 {
		floats = append(floats, cs.Range...)
	} else {
		floats = append(floats, -100, 100, -100, 100)
	}
	return floats
}

// PdfColorspaceSpecialIndexed is an indexed color space is a lookup table, where the input element
// is an index to the lookup table and the output is a color defined in the lookup table in the Base
// colorspace.
// [/Indexed base hival lookup]
type PdfColorspaceSpecialIndexed struct {
	Base        PdfColorspace
	HiVal       int
	Lookup      core.PdfObject
	lookupTable []byte
	container   *core.PdfIndirectObject
}

// ToPdfObject converts colorspace to a PDF object. [/Indexed base hival lookup]
func (cs *PdfColorspaceSpecialIndexed) ToPdfObject() core.PdfObject {
	array := core.MakeArray(core.MakeName("Indexed"))
	array.Append(cs.Base.ToPdfObject())
	array.Append(core.MakeInteger(int64(cs.HiVal)))
	array.Append(cs.Lookup)
	if cs.container != nil {
		cs.container.PdfObject = array
		return cs.container
	}
	return array
}

// ColorToRGB converts an Indexed color to an RGB color.
func (cs *PdfColorspaceSpecialIndexed) ColorToRGB(color PdfColor) (PdfColor, error) {
	if cs.Base == nil {
		return nil, errors.New("indexed base colorspace undefined")
	}
	return cs.Base.ColorToRGB(color)
}

func (cs *PdfColorspaceSpecialIndexed) String() string { return "Indexed" }

// NewPdfColorspaceSpecialIndexed returns a new Indexed color.
func NewPdfColorspaceSpecialIndexed() *PdfColorspaceSpecialIndexed {
	return &PdfColorspaceSpecialIndexed{HiVal: 255}
}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain a single element.
func (cs *PdfColorspaceSpecialIndexed) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 1 {
		return nil, errors.New("range check")
	}
	N := cs.Base.GetNumComponents()
	n := int(vals[0]) * N
	if n < 0 || (n+N-1) >= len(cs.lookupTable) {
		common.Log.Debug("color out of CS range: ", n)
		return nil, ErrColorOutOfRange
	}
	data := cs.lookupTable[n : n+N]
	var floats []float64
	for _, b := range data {
		floats = append(floats, float64(b)/255.0)
	}
	col, err := cs.Base.ColorFromFloats(floats)
	if err != nil {
		return nil, err
	}
	return col, nil
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain a single PdfObjectFloat element.
func (cs *PdfColorspaceSpecialIndexed) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 1 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// ImageToRGB convert an indexed image to RGB.
func (cs *PdfColorspaceSpecialIndexed) ImageToRGB(img Image) (Image, error) {
	N := cs.Base.GetNumComponents()
	if N < 1 {
		return Image{}, fmt.Errorf("bad base colorspace NumComponents=%d", N)
	}
	n := len(cs.lookupTable) / N
	if n < 1 {
		imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), 8, N, nil, img.alpha, img.decode)
		return cs.Base.ImageToRGB(imageFromImageBase(&imageBase))
	}
	imageBase := imageutil.NewImageBase(n, 1, 8, N, nil, nil, img.decode)
	writer := sampling.NewWriter(imageBase)
	for i := 0; i < n*N; i++ {
		if err := writer.WriteSample(uint32(cs.lookupTable[i])); err != nil {
			return img, err
		}
	}
	imageToRGB, err := cs.Base.ImageToRGB(imageFromImageBase(&imageBase))
	if err != nil {
		return img, err
	}
	base := imageToRGB.getBase()
	reader := sampling.NewReader(base)
	numsList := make([][3]uint32, n)
	for i := 0; i < n; i++ {
		for j := 0; j < 3; j++ {
			sample, err := reader.ReadSample()
			if err != nil {
				return img, err
			}
			numsList[i][j] = sample
		}
	}
	base2 := imageutil.NewImageBase(int(img.Width), int(img.Height), base.BitsPerComponent, 3, nil, img.alpha, base.Decode)
	r := sampling.NewReader(img.getBase())
	w := sampling.NewWriter(base2)
	count := n - 1
	var (
		num  uint32
		err2 error
	)
	for {
		num, err2 = r.ReadSample()
		if err2 == io.EOF {
			break
		} else if err2 != nil {
			return img, err2
		}
		size := min(int(num), count)
		if err := w.WriteSamples(numsList[size][:]); err != nil {
			return img, err
		}
	}
	return imageFromImageBase(&base2), nil
}

// DecodeArray returns the component range values for the Indexed colorspace.
func (cs *PdfColorspaceSpecialIndexed) DecodeArray() []float64 {
	return []float64{0, float64(cs.HiVal)}
}

// GetNumComponents returns the number of color components (1 for Indexed).
func (cs *PdfColorspaceSpecialIndexed) GetNumComponents() int { return 1 }

func newPdfColorspaceSpecialIndexedFromPdfObject(obj core.PdfObject) (*PdfColorspaceSpecialIndexed, error) {
	specialIndexedCS := NewPdfColorspaceSpecialIndexed()
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		specialIndexedCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("type error")
	}
	if arr.Len() != 4 {
		return nil, fmt.Errorf("indexed CS: invalid array length")
	}
	obj = arr.Get(0)
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("indexed CS: invalid name")
	}
	if *name != "Indexed" {
		return nil, fmt.Errorf("indexed CS: wrong name")
	}
	obj = arr.Get(1)
	determineColorspaceName, err := DetermineColorspaceNameFromPdfObject(obj)
	if err != nil {
		return nil, err
	}
	if determineColorspaceName == "Indexed" || determineColorspaceName == "Pattern" {
		common.Log.Debug("Error: Indexed colorspace cannot have Indexed/Pattern CS as base (%v)", determineColorspaceName)
		return nil, errRangeCheck
	}
	cs, err := NewPdfColorspaceFromPdfObject(obj)
	if err != nil {
		return nil, err
	}
	specialIndexedCS.Base = cs
	obj = arr.Get(2)
	n, err := core.GetNumberAsInt64(obj)
	if err != nil {
		return nil, err
	}
	if n > 255 {
		return nil, fmt.Errorf("indexed CS: Invalid hival")
	}
	specialIndexedCS.HiVal = int(n)
	obj = arr.Get(3)
	specialIndexedCS.Lookup = obj
	obj = core.TraceToDirectObject(obj)
	var data []byte
	if str, ok := obj.(*core.PdfObjectString); ok {
		data = str.Bytes()
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Indexed string color data: % d", data)
		}
	} else if stream, ok := obj.(*core.PdfObjectStream); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Indexed stream: %s", obj.String())
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encoded (%d) : %# x", len(stream.Stream), stream.Stream)
		}
		data2, err := core.DecodeStream(stream)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decoded (%d) : % X", len(data2), data2)
		}
		data = data2
	} else {
		common.Log.Debug("Type: %T", obj)
		return nil, fmt.Errorf("indexed CS: Invalid table format")
	}
	if len(data) < specialIndexedCS.Base.GetNumComponents()*(specialIndexedCS.HiVal+1) {
		common.Log.Debug("PDF Incompatibility: Index stream too short")
		common.Log.Debug("Fail, len(data): %d, components: %d, hiVal: %d", len(data), specialIndexedCS.Base.GetNumComponents(), specialIndexedCS.HiVal)
	} else {
		data = data[:specialIndexedCS.Base.GetNumComponents()*(specialIndexedCS.HiVal+1)]
	}
	specialIndexedCS.lookupTable = data
	return specialIndexedCS, nil
}
