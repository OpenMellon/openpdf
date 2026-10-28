package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/sampling"
)

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
