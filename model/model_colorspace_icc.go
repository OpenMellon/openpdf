package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"math"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/sampling"
	"github.com/OpenMellon/openpdf/model/internal/icctransform"
)

func newPdfColorspaceICCBasedFromPdfObject(obj core.PdfObject) (*PdfColorspaceICCBased, error) {
	iccBasedCS := &PdfColorspaceICCBased{}
	if indirect, ok := obj.(*core.PdfIndirectObject); ok {
		iccBasedCS.container = indirect
	}
	obj = core.TraceToDirectObject(obj)
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("type error")
	}
	if arr.Len() != 2 {
		return nil, fmt.Errorf("invalid ICCBased colorspace")
	}
	obj = core.TraceToDirectObject(arr.Get(0))
	name, ok := obj.(*core.PdfObjectName)
	if !ok {
		return nil, fmt.Errorf("ICCBased name not a Name object")
	}
	if *name != "ICCBased" {
		return nil, fmt.Errorf("not an ICCBased colorspace")
	}
	obj = arr.Get(1)
	stream, ok := core.GetStream(obj)
	if !ok {
		common.Log.Error("ICCBased not pointing to stream: %T", obj)
		return nil, fmt.Errorf("ICCBased stream invalid")
	}
	dict := stream.PdfObjectDictionary
	num, ok := dict.Get("N").(*core.PdfObjectInteger)
	if !ok {
		return nil, fmt.Errorf("ICCBased missing N from stream dict")
	}
	if *num != 1 && *num != 3 && *num != 4 {
		return nil, fmt.Errorf("ICCBased colorspace invalid N (not 1,3,4)")
	}
	iccBasedCS.N = int(*num)
	if alternate := dict.Get("Alternate"); alternate != nil {
		cs, err := NewPdfColorspaceFromPdfObject(alternate)
		if err != nil {
			return nil, err
		}
		iccBasedCS.Alternate = cs
	}
	if obj := dict.Get("Range"); obj != nil {
		obj = core.TraceToDirectObject(obj)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			return nil, fmt.Errorf("ICCBased Range not an array")
		}
		if arr.Len() != 2*iccBasedCS.N {
			return nil, fmt.Errorf("ICCBased Range wrong number of elements")
		}
		floats, err := arr.GetAsFloat64Slice()
		if err != nil {
			return nil, err
		}
		iccBasedCS.Range = floats
	} else {
		iccBasedCS.Range = make([]float64, 2*iccBasedCS.N)
		for i := 0; i < iccBasedCS.N; i++ {
			iccBasedCS.Range[2*i] = 0.0
			iccBasedCS.Range[2*i+1] = 1.0
		}
	}
	if metadata := dict.Get("Metadata"); metadata != nil {
		stream, ok := metadata.(*core.PdfObjectStream)
		if !ok {
			return nil, fmt.Errorf("ICCBased Metadata not a stream")
		}
		iccBasedCS.Metadata = stream
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	iccBasedCS.Data = data
	iccBasedCS.stream = stream
	return iccBasedCS, nil
}

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// component PDF objects.
func (cs *PdfColorspaceICCBased) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if cs.Alternate == nil {
		if cs.N == 1 {
			deviceGrayCS := NewPdfColorspaceDeviceGray()
			return deviceGrayCS.ColorFromPdfObjects(objects)
		} else if cs.N == 3 {
			deviceRGBCS := NewPdfColorspaceDeviceRGB()
			return deviceRGBCS.ColorFromPdfObjects(objects)
		} else if cs.N == 4 {
			deviceCMYKCS := NewPdfColorspaceDeviceCMYK()
			return deviceCMYKCS.ColorFromPdfObjects(objects)
		} else {
			return nil, errors.New("ICC Based colorspace missing alternative")
		}
	}
	return cs.Alternate.ColorFromPdfObjects(objects)
}

// GetNumComponents returns the number of color components.
func (cs *PdfColorspaceICCBased) GetNumComponents() int { return cs.N }

// NewPdfColorspaceICCBased returns a new ICCBased colorspace object.
func NewPdfColorspaceICCBased(N int) (*PdfColorspaceICCBased, error) {
	iccBasedCS := &PdfColorspaceICCBased{}
	if N != 1 && N != 3 && N != 4 {
		return nil, fmt.Errorf("invalid N (1/3/4)")
	}
	iccBasedCS.N = N
	return iccBasedCS, nil
}

// GetContainingPdfObject gets the primitive used to parse the color space.
func (cs *PdfColorspaceICCBased) GetContainingPdfObject() core.PdfObject { return cs.stream }

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components.
func (cs *PdfColorspaceICCBased) ColorFromFloats(vals []float64) (PdfColor, error) {
	if cs.Alternate == nil {
		if cs.N == 1 {
			deviceGrayCS := NewPdfColorspaceDeviceGray()
			return deviceGrayCS.ColorFromFloats(vals)
		} else if cs.N == 3 {
			deviceRGBCS := NewPdfColorspaceDeviceRGB()
			return deviceRGBCS.ColorFromFloats(vals)
		} else if cs.N == 4 {
			deviceCMYKCS := NewPdfColorspaceDeviceCMYK()
			return deviceCMYKCS.ColorFromFloats(vals)
		} else {
			return nil, errors.New("ICC Based colorspace missing alternative")
		}
	}
	return cs.Alternate.ColorFromFloats(vals)
}

func (cs *PdfColorspaceICCBased) String() string { return "ICCBased" }

// PdfColorspaceICCBased format [/ICCBased stream]
//
// The stream shall contain the ICC profile.
// A conforming reader shall support ICC.1:2004:10 as required by PDF 1.7, which will enable it
// to properly render all embedded ICC profiles regardless of the PDF version
//
// In the current implementation, we rely on the alternative colormap provided.
type PdfColorspaceICCBased struct {
	N         int
	Alternate PdfColorspace

	// If omitted ICC not supported: then use DeviceGray,
	// DeviceRGB or DeviceCMYK for N=1,3,4 respectively.
	Range          []float64
	Metadata       *core.PdfObjectStream
	Data           []byte
	container      *core.PdfIndirectObject
	stream         *core.PdfObjectStream
	iccTransform   *icctransform.Transform
	transformOnce  sync.Once
	transformReady bool
}

// ToPdfObject returns colorspace in a PDF object format [name stream]
func (cs *PdfColorspaceICCBased) ToPdfObject() core.PdfObject {
	arr := &core.PdfObjectArray{}
	arr.Append(core.MakeName("ICCBased"))
	var stream *core.PdfObjectStream
	if cs.stream != nil {
		stream = cs.stream
	} else {
		stream = &core.PdfObjectStream{}
	}
	dict := core.MakeDict()
	dict.Set("N", core.MakeInteger(int64(cs.N)))
	if cs.Alternate != nil {
		dict.Set("Alternate", cs.Alternate.ToPdfObject())
	}
	if cs.Metadata != nil {
		dict.Set("Metadata", cs.Metadata)
	}
	if cs.Range != nil {
		var objs []core.PdfObject
		for _, f := range cs.Range {
			objs = append(objs, core.MakeFloat(f))
		}
		dict.Set("Range", core.MakeArray(objs...))
	}
	dict.Set("Length", core.MakeInteger(int64(len(cs.Data))))
	stream.Stream = cs.Data
	stream.PdfObjectDictionary = dict
	arr.Append(stream)
	if cs.container != nil {
		cs.container.PdfObject = arr
		return cs.container
	}
	return arr
}

func (cs *PdfColorspaceICCBased) ImageToRGB(img Image) (Image, error) {
	cs.buildTransform()
	if cs.transformReady && img.ColorComponents == 3 {
		imageToRGBManaged, err := cs.imageToRGBManaged(img)
		if err == nil {
			return imageToRGBManaged, nil
		}
		common.Log.Debug("ICCBased colour-managed conversion failed (%v); using alternate", err)
	}
	if cs.Alternate == nil {
		common.Log.Debug("ICC Based colorspace missing alternative")
		if cs.N == 1 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceGray (N=1)")
			deviceGrayCS := NewPdfColorspaceDeviceGray()
			return deviceGrayCS.ImageToRGB(img)
		} else if cs.N == 3 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceRGB (N=3)")
			return img, nil
		} else if cs.N == 4 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceCMYK (N=4)")
			deviceCMYKCS := NewPdfColorspaceDeviceCMYK()
			return deviceCMYKCS.ImageToRGB(img)
		} else {
			return img, errors.New("ICC Based colorspace missing alternative")
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ICC Based colorspace with alternative: %#v", cs)
	}
	imageToRGB, err := cs.Alternate.ImageToRGB(img)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ICC Input image: %+v", img)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ICC Output image: %+v", imageToRGB)
	}
	return imageToRGB, err
}

// ColorToRGB converts a ICCBased color to an RGB color.
func (cs *PdfColorspaceICCBased) ColorToRGB(color PdfColor) (PdfColor, error) {
	cs.buildTransform()
	if cs.transformReady {
		if components := color.Components(); len(components) == 3 {
			convert, f, v := cs.iccTransform.Convert(components[0], components[1], components[2])
			return NewPdfColorDeviceRGB(convert, f, v), nil
		}
	}
	if cs.Alternate == nil {
		common.Log.Debug("ICC Based colorspace missing alternative")
		if cs.N == 1 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceGray (N=1)")
			deviceGrayCS := NewPdfColorspaceDeviceGray()
			return deviceGrayCS.ColorToRGB(color)
		} else if cs.N == 3 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceRGB (N=3)")
			return color, nil
		} else if cs.N == 4 {
			common.Log.Debug("ICC Based colorspace missing alternative - using DeviceCMYK (N=4)")
			deviceCMYKCS := NewPdfColorspaceDeviceCMYK()
			return deviceCMYKCS.ColorToRGB(color)
		} else {
			return nil, errors.New("ICC Based colorspace missing alternative")
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ICC Based colorspace with alternative: %#v", cs)
	}
	return cs.Alternate.ColorToRGB(color)
}

// DecodeArray returns the range of color component values in the ICCBased colorspace.
func (cs *PdfColorspaceICCBased) DecodeArray() []float64 { return cs.Range }

func (cs *PdfColorspaceICCBased) imageToRGBManaged(img Image) (Image, error) {
	floats := img.decode
	if len(floats) != 6 {
		floats = cs.Range
	}
	if len(floats) != 6 {
		floats = []float64{0, 1, 0, 1, 0, 1}
	}
	imageBase := imageutil.NewImageBase(int(img.Width), int(img.Height), int(img.BitsPerComponent), 3, nil, img.alpha, nil)
	f := math.Pow(2, float64(img.BitsPerComponent)) - 1
	var floats2, floats3, floats4 []float64
	if img.BitsPerComponent <= maxBitsPerComponent {
		n := int(f) + 1
		floats2 = make([]float64, n)
		floats3 = make([]float64, n)
		floats4 = make([]float64, n)
		for i := 0; i < n; i++ {
			v := float64(i) / f
			floats2[i] = cs.iccTransform.LinearizeChannel(0, imageutil.LinearInterpolate(v, 0.0, 1.0, floats[0], floats[1]))
			floats3[i] = cs.iccTransform.LinearizeChannel(1, imageutil.LinearInterpolate(v, 0.0, 1.0, floats[2], floats[3]))
			floats4[i] = cs.iccTransform.LinearizeChannel(2, imageutil.LinearInterpolate(v, 0.0, 1.0, floats[4], floats[5]))
		}
	}
	base := img.getBase()
	if floats2 != nil && img.BitsPerComponent == 8 && base.BytesPerLine == int(img.Width)*3 && imageBase.BytesPerLine == int(img.Width)*3 && len(base.Data) >= imageBase.BytesPerLine*int(img.Height) {
		data := imageBase.Data
		data2 := base.Data
		n := int(img.Width) * int(img.Height)
		for i := 0; i < n; i++ {
			n := i * 3
			encodeLinear, v, val := cs.iccTransform.EncodeLinear(floats2[data2[n]], floats3[data2[n+1]], floats4[data2[n+2]])
			data[n] = byte(uint32(encodeLinear * f))
			data[n+1] = byte(uint32(v * f))
			data[n+2] = byte(uint32(val * f))
		}
		return imageFromImageBase(&imageBase), nil
	}
	reader := sampling.NewReader(base)
	writer := sampling.NewWriter(imageBase)
	nums := make([]uint32, 3)
	var err error
	for {
		err = reader.ReadSamples(nums)
		if err == io.EOF {
			break
		} else if err != nil {
			return img, err
		}
		var v, val, x float64
		if floats2 != nil {
			v, val, x = cs.iccTransform.EncodeLinear(floats2[nums[0]], floats3[nums[1]], floats4[nums[2]])
		} else {
			linearInterpolate := imageutil.LinearInterpolate(float64(nums[0])/f, 0.0, 1.0, floats[0], floats[1])
			y := imageutil.LinearInterpolate(float64(nums[1])/f, 0.0, 1.0, floats[2], floats[3])
			y2 := imageutil.LinearInterpolate(float64(nums[2])/f, 0.0, 1.0, floats[4], floats[5])
			v, val, x = cs.iccTransform.Convert(linearInterpolate, y, y2)
		}
		nums[0] = uint32(v * f)
		nums[1] = uint32(val * f)
		nums[2] = uint32(x * f)
		if err = writer.WriteSamples(nums); err != nil {
			return img, err
		}
	}
	return imageFromImageBase(&imageBase), nil
}

func (cs *PdfColorspaceICCBased) buildTransform() {
	cs.transformOnce.Do(func() {
		if cs.N != 3 || len(cs.Data) < 128 {
			return
		}
		xform, err := icctransform.NewTransform(cs.Data)
		if err != nil {
			common.Log.Debug("ICCBased: profile not colour-managed (%v); using alternate", err)
			return
		}
		cs.iccTransform = xform
		cs.transformReady = true
	})
}

// IsColorManaged reports whether the embedded profile is interpreted, so that
// ImageToRGB and ColorToRGB convert through it rather than through the
// alternate colorspace. Only 3-component matrix/TRC RGB profiles are
// interpreted.
func (cs *PdfColorspaceICCBased) IsColorManaged() bool {
	cs.buildTransform()
	return cs.transformReady && cs.N == 3
}
