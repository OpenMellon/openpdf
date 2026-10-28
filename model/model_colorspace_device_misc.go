package model

import (
	"errors"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
)

// ImageToRGB convert 1-component grayscale data to 3-component RGB.
func (cs *PdfColorspaceDeviceGray) ImageToRGB(img Image) (Image, error) {
	if img.ColorComponents != 1 {
		return img, errors.New("the provided image is not gray scale")
	}
	img2, err := imageutil.NewImage(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
	if err != nil {
		return img, err
	}
	convert, err := imageutil.NRGBAConverter.Convert(img2)
	if err != nil {
		return img, err
	}
	img3 := imageFromImageBase(convert.Base())
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("DeviceGray -> RGB")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("samples: %v", img.Data)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("RGB samples: %v", img3.Data)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%v -> %v", img, img3)
	}
	return img3, nil
}

// NewPdfColorspaceDeviceGray returns a new grayscale colorspace.
func NewPdfColorspaceDeviceGray() *PdfColorspaceDeviceGray { return &PdfColorspaceDeviceGray{} }

// NewPdfColorspaceDeviceCMYK returns a new CMYK32 colorspace object.
func NewPdfColorspaceDeviceCMYK() *PdfColorspaceDeviceCMYK { return &PdfColorspaceDeviceCMYK{} }

// ColorToRGB converts gray -> rgb for a single color component.
func (cs *PdfColorspaceDeviceGray) ColorToRGB(color PdfColor) (PdfColor, error) {
	deviceGrayColor, ok := color.(*PdfColorDeviceGray)
	if !ok {
		common.Log.Debug("Input color not device gray %T", color)
		return nil, errors.New("type check error")
	}
	return NewPdfColorDeviceRGB(float64(*deviceGrayColor), float64(*deviceGrayColor), float64(*deviceGrayColor)), nil
}

func (cs *PdfColorspaceDeviceGray) String() string {
	return "DeviceGray"
}

func (cs *PdfColorspaceDeviceCMYK) String() string {
	return "DeviceCMYK"
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceDeviceCMYK) ToPdfObject() core.PdfObject {
	return core.MakeName("DeviceCMYK")
}

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 4 for a CMYK32 device.
func (cs *PdfColorspaceDeviceCMYK) GetNumComponents() int { return 4 }

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain three elements representing the
// red, green and blue components of the color. The values of the elements
// should be between 0 and 1.
func (cs *PdfColorspaceDeviceRGB) ColorFromFloats(vals []float64) (PdfColor, error) {
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
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	v := vals[2]
	if v < 0.0 || v > 1.0 {
		common.Log.Debug("color out of CS range: ", val)
		return nil, ErrColorOutOfRange
	}
	deviceRGBColor := NewPdfColorDeviceRGB(val, f, v)
	return deviceRGBColor, nil
}

// ImageToRGB converts an image in CMYK32 colorspace to an RGB image.
func (cs *PdfColorspaceDeviceCMYK) ImageToRGB(img Image) (Image, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("CMYK32 -> RGB")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Image BPC: %d, Color components: %d", img.BitsPerComponent, img.ColorComponents)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Len data: %d", len(img.Data))
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Height: %d, Width: %d", img.Height, img.Width)
	}
	img2, err := imageutil.NewImage(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
	if err != nil {
		return Image{}, err
	}
	convert, err := imageutil.NRGBAConverter.Convert(img2)
	if err != nil {
		return Image{}, err
	}
	return imageFromImageBase(convert.Base()), nil
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceDeviceRGB) ToPdfObject() core.PdfObject { return core.MakeName("DeviceRGB") }

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 3 for an RGB device.
func (cs *PdfColorspaceDeviceRGB) GetNumComponents() int { return 3 }

// PdfColorspaceDeviceCMYK represents a CMYK32 colorspace.
type PdfColorspaceDeviceCMYK struct{}

// ColorFromFloats returns a new PdfColor based on the input slice of color
// components. The slice should contain a single element between 0 and 1.
func (cs *PdfColorspaceDeviceGray) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 1 {
		return nil, errors.New("range check")
	}
	val := vals[0]
	if val < 0.0 || val > 1.0 {
		common.Log.Debug("Incompatibility: Range outside [0,1]")
	}
	if val < 0.0 {
		val = 0.0
	} else if val > 1.0 {
		val = 1.0
	}
	return NewPdfColorDeviceGray(val), nil
}

// GetNumComponents returns the number of color components of the colorspace device.
// Returns 1 for a grayscale device.
func (cs *PdfColorspaceDeviceGray) GetNumComponents() int { return 1 }

// DecodeArray returns the range of color component values in DeviceRGB colorspace.
func (cs *PdfColorspaceDeviceRGB) DecodeArray() []float64 {
	return []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
}

// PdfColorspaceDeviceGray represents a grayscale colorspace.
type PdfColorspaceDeviceGray struct{}

// ColorToRGB converts a CMYK32 color to an RGB color.
func (cs *PdfColorspaceDeviceCMYK) ColorToRGB(color PdfColor) (PdfColor, error) {
	deviceCMYKColor, ok := color.(*PdfColorDeviceCMYK)
	if !ok {
		common.Log.Debug("Input color not device cmyk")
		return nil, errors.New("type check error")
	}
	f := deviceCMYKColor.C()
	v := deviceCMYKColor.M()
	val := deviceCMYKColor.Y()
	x := deviceCMYKColor.K()
	f = f*(1-x) + x
	v = v*(1-x) + x
	val = val*(1-x) + x
	y := 1 - f
	y2 := 1 - v
	y3 := 1 - val
	return NewPdfColorDeviceRGB(y, y2, y3), nil
}

// NewPdfColorspaceDeviceRGB returns a new RGB colorspace object.
func NewPdfColorspaceDeviceRGB() *PdfColorspaceDeviceRGB { return &PdfColorspaceDeviceRGB{} }

// ImageToGray returns a new grayscale image based on the passed in RGB image.
func (cs *PdfColorspaceDeviceRGB) ImageToGray(img Image) (Image, error) {
	if img.ColorComponents != 3 {
		return img, errors.New("provided image is not a DeviceRGB")
	}
	img2, err := imageutil.NewImage(int(img.Width), int(img.Height), int(img.BitsPerComponent), img.ColorComponents, img.Data, img.alpha, img.decode)
	if err != nil {
		return img, err
	}
	convert, err := imageutil.GrayConverter.Convert(img2)
	if err != nil {
		return img, err
	}
	return imageFromImageBase(convert.Base()), nil
}

// ToPdfObject returns the PDF representation of the colorspace.
func (cs *PdfColorspaceDeviceGray) ToPdfObject() core.PdfObject {
	return core.MakeName("DeviceGray")
}

// DecodeArray returns the range of color component values in DeviceCMYK colorspace.
func (cs *PdfColorspaceDeviceCMYK) DecodeArray() []float64 {
	return []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
}

// ColorFromPdfObjects gets the color from a series of pdf objects (4 for cmyk).
func (cs *PdfColorspaceDeviceCMYK) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 4 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// PdfColorspaceDeviceRGB represents an RGB colorspace.
type PdfColorspaceDeviceRGB struct{}

// ColorFromPdfObjects gets the color from a series of pdf objects (3 for rgb).
func (cs *PdfColorspaceDeviceRGB) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 3 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}

// ColorToRGB verifies that the input color is an RGB color. Method exists in
// order to satisfy the PdfColorspace interface.
func (cs *PdfColorspaceDeviceRGB) ColorToRGB(color PdfColor) (PdfColor, error) {
	deviceRGBColor, ok := color.(*PdfColorDeviceRGB)
	if !ok {
		common.Log.Debug("Input color not device RGB")
		return nil, errors.New("type check error")
	}
	return deviceRGBColor, nil
}

func (cs *PdfColorspaceDeviceRGB) String() string { return "DeviceRGB" }

// DecodeArray returns the range of color component values in DeviceGray colorspace.
func (cs *PdfColorspaceDeviceGray) DecodeArray() []float64 { return []float64{0, 1.0} }

// ColorFromFloats returns a new PdfColorDevice based on the input slice of
// color components. The slice should contain four elements representing the
// cyan, magenta, yellow and key components of the color. The values of the
// elements should be between 0 and 1.
func (cs *PdfColorspaceDeviceCMYK) ColorFromFloats(vals []float64) (PdfColor, error) {
	if len(vals) != 4 {
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
	x := vals[3]
	if x < 0.0 || x > 1.0 {
		common.Log.Debug("color out of CS range: ", x)
		return nil, ErrColorOutOfRange
	}
	deviceCMYKColor := NewPdfColorDeviceCMYK(val, f, v, x)
	return deviceCMYKColor, nil
}

// ImageToRGB returns the passed in image. Method exists in order to satisfy
// the PdfColorspace interface.
func (cs *PdfColorspaceDeviceRGB) ImageToRGB(img Image) (Image, error) { return img, nil }

// ColorFromPdfObjects returns a new PdfColor based on the input slice of color
// components. The slice should contain a single PdfObjectFloat element in
// range 0-1.
func (cs *PdfColorspaceDeviceGray) ColorFromPdfObjects(objects []core.PdfObject) (PdfColor, error) {
	if len(objects) != 1 {
		return nil, errors.New("range check")
	}
	numbersAsFloat, err := core.GetNumbersAsFloat(objects)
	if err != nil {
		return nil, err
	}
	return cs.ColorFromFloats(numbersAsFloat)
}
