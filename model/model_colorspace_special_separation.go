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

// GetNumComponents returns the number of color components (1 for Separation).
func (cs *PdfColorspaceSpecialSeparation) GetNumComponents() int { return 1 }

// DecodeArray returns the component range values for the Separation colorspace.
func (cs *PdfColorspaceSpecialSeparation) DecodeArray() []float64 { return []float64{0, 1.0} }
