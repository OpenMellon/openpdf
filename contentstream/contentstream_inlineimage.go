package contentstream

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/model"
)

// NewInlineImageFromImage makes a new content stream inline image object from an image.
func NewInlineImageFromImage(img model.Image, encoder core.StreamEncoder) (*ContentStreamInlineImage, error) {
	if encoder == nil {
		encoder = core.NewRawEncoder()
	}
	encoder.UpdateParams(img.GetParamsDict())
	v := ContentStreamInlineImage{}
	switch img.ColorComponents {
	case 1:
		v.ColorSpace = core.MakeName("G")
	case 3:
		v.ColorSpace = core.MakeName("RGB")
	case 4:
		v.ColorSpace = core.MakeName("CMYK")
	default:
		common.Log.Debug("Invalid number of color components for inline image: %d", img.ColorComponents)
		return nil, errors.New("invalid number of color components")
	}
	v.BitsPerComponent = core.MakeInteger(img.BitsPerComponent)
	v.Width = core.MakeInteger(img.Width)
	v.Height = core.MakeInteger(img.Height)
	buf, err := encoder.EncodeBytes(img.Data)
	if err != nil {
		return nil, err
	}
	v.data = buf
	s := encoder.GetFilterName()
	if s != core.StreamEncodingFilterNameRaw {
		v.Filter = core.MakeName(s)
	}
	return &v, nil
}

// GetColorSpace returns the colorspace of the inline image.
func (img *ContentStreamInlineImage) GetColorSpace(resources *model.PdfPageResources) (model.PdfColorspace, error) {
	if img.ColorSpace == nil {
		common.Log.Debug("Inline image not having specified colorspace, assuming Gray")
		return model.NewPdfColorspaceDeviceGray(), nil
	}
	if arr, ok := img.ColorSpace.(*core.PdfObjectArray); ok {
		return newIndexedColorspaceFromArray(arr)
	}
	name, ok := img.ColorSpace.(*core.PdfObjectName)
	if !ok {
		common.Log.Debug("Error: Invalid object type (%T;%+v)", img.ColorSpace, img.ColorSpace)
		return nil, errors.New("type check error")
	}
	switch *name {
	case "G", "DeviceGray":
		return model.NewPdfColorspaceDeviceGray(), nil
	case "RGB", "DeviceRGB":
		return model.NewPdfColorspaceDeviceRGB(), nil
	case "CMYK", "DeviceCMYK":
		return model.NewPdfColorspaceDeviceCMYK(), nil
	case "I", "Indexed":
		return nil, errors.New("unsupported Index colorspace")
	default:
		if resources.ColorSpace == nil {
			common.Log.Debug("Error, unsupported inline image colorspace: %s", *name)
			return nil, errors.New("unknown colorspace")
		}
		cs, ok2 := resources.GetColorspaceByName(*name)
		if !ok2 {
			common.Log.Debug("Error, unsupported inline image colorspace: %s", *name)
			return nil, errors.New("unknown colorspace")
		}
		return cs, nil
	}
}

// ToImage exports the inline image to Image which can be transformed or exported easily.
// Page resources are needed to look up colorspace information.
func (img *ContentStreamInlineImage) ToImage(resources *model.PdfPageResources) (*model.Image, error) {
	v, err := img.toImageBase(resources)
	if err != nil {
		return nil, err
	}
	v2, err := newEncoderFromInlineImage(img)
	if err != nil {
		return nil, err
	}
	dict, ok := core.GetDict(img.DecodeParms)
	if ok {
		v2.UpdateParams(dict)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("encoder: %+v %T", v2, v2)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("inline image: %+v", img)
	}
	buf, err := v2.DecodeBytes(img.data)
	if err != nil {
		return nil, err
	}
	v3 := &model.Image{Width: int64(v.Width), Height: int64(v.Height), BitsPerComponent: int64(v.BitsPerComponent), ColorComponents: v.ColorComponents, Data: buf}
	if len(v.Decode) > 0 {
		floats := append([]float64(nil), v.Decode...)
		ok2 := false
		switch icc := img.colorspace.(type) {
		case *model.PdfColorspaceSpecialIndexed:
			floats, _ = imageutil.NormalizeIndexedDecode(floats, v.BitsPerComponent)
		case *model.PdfColorspaceDeviceRGB, *model.PdfColorspaceDeviceCMYK:
			ok2 = true
		case *model.PdfColorspaceICCBased:
			ok2 = !icc.IsColorManaged() && (icc.N == 3 || icc.N == 4)
		}
		if ok2 {
			f := float64((int(1) << uint(v.BitsPerComponent)) - 1)
			for n := range floats {
				floats[n] *= f
			}
		}
		if floats != nil {
			v3.SetDecode(floats)
		}
	}
	return v3, nil
}

// IsMask checks if an image is a mask.
// The image mask entry in the image dictionary specifies that the image data shall be used as a stencil
// mask for painting in the current color. The mask data is 1bpc, grayscale.
func (img *ContentStreamInlineImage) IsMask() (bool, error) {
	if img.ImageMask != nil {
		v, ok := img.ImageMask.(*core.PdfObjectBool)
		if !ok {
			common.Log.Debug("Image mask not a boolean")
			return false, errors.New("invalid object type")
		}
		return bool(*v), nil
	}
	return false, nil
}

// Direct returns the object itself; an inline image is always a direct object.
func (img *ContentStreamInlineImage) Direct() core.PdfObject { return img }

// ContentStreamInlineImage is a representation of an inline image in a Content stream. Everything between the BI and EI operands.
// ContentStreamInlineImage implements the core.PdfObject interface although strictly it is not a PDF object.
type ContentStreamInlineImage struct {
	colorspace       model.PdfColorspace
	BitsPerComponent core.PdfObject
	ColorSpace       core.PdfObject
	Decode           core.PdfObject
	DecodeParms      core.PdfObject
	Filter           core.PdfObject
	Height           core.PdfObject
	ImageMask        core.PdfObject
	Intent           core.PdfObject
	Interpolate      core.PdfObject
	Width            core.PdfObject
	data             []byte
	imageBase        *imageutil.ImageBase
}

// Equals reports whether other is an inline image with the same serialized contents.
func (img *ContentStreamInlineImage) Equals(other core.PdfObject) bool {
	img2, ok := other.(*ContentStreamInlineImage)
	if !ok {
		return false
	}
	if img == nil || img2 == nil {
		return img == nil && img2 == nil
	}
	return bytes.Equal(img.Write(), img2.Write())
}

// Write outputs the object as a byte array.
func (img *ContentStreamInlineImage) Write() []byte {
	var buf bytes.Buffer
	if img.BitsPerComponent != nil {
		buf.WriteString("/BPC ")
		buf.Write(img.BitsPerComponent.Write())
		buf.WriteByte('\n')
	}
	if img.ColorSpace != nil {
		buf.WriteString("/CS ")
		buf.Write(img.ColorSpace.Write())
		buf.WriteByte('\n')
	}
	if img.Decode != nil {
		buf.WriteString("/D ")
		buf.Write(img.Decode.Write())
		buf.WriteByte('\n')
	}
	if img.DecodeParms != nil {
		buf.WriteString("/DP ")
		buf.Write(img.DecodeParms.Write())
		buf.WriteByte('\n')
	}
	if img.Filter != nil {
		buf.WriteString("/F ")
		buf.Write(img.Filter.Write())
		buf.WriteByte('\n')
	}
	if img.Height != nil {
		buf.WriteString("/H ")
		buf.Write(img.Height.Write())
		buf.WriteByte('\n')
	}
	if img.ImageMask != nil {
		buf.WriteString("/IM ")
		buf.Write(img.ImageMask.Write())
		buf.WriteByte('\n')
	}
	if img.Intent != nil {
		buf.WriteString("/Intent ")
		buf.Write(img.Intent.Write())
		buf.WriteByte('\n')
	}
	if img.Interpolate != nil {
		buf.WriteString("/I ")
		buf.Write(img.Interpolate.Write())
		buf.WriteByte('\n')
	}
	if img.Width != nil {
		buf.WriteString("/W ")
		buf.Write(img.Width.Write())
		buf.WriteByte('\n')
	}
	buf.WriteString("ID ")
	buf.Write(img.data)
	buf.WriteString("\nEI\n")
	return buf.Bytes()
}

func (img *ContentStreamInlineImage) String() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("InlineImage(len=%d)\n", len(img.data)))
	if img.BitsPerComponent != nil {
		buf.WriteString("/BPC ")
		buf.Write(img.BitsPerComponent.Write())
		buf.WriteByte('\n')
	}
	if img.ColorSpace != nil {
		buf.WriteString("/CS ")
		buf.Write(img.ColorSpace.Write())
		buf.WriteByte('\n')
	}
	if img.Decode != nil {
		buf.WriteString("/D ")
		buf.Write(img.Decode.Write())
		buf.WriteByte('\n')
	}
	if img.DecodeParms != nil {
		buf.WriteString("/DP ")
		buf.Write(img.DecodeParms.Write())
		buf.WriteByte('\n')
	}
	if img.Filter != nil {
		buf.WriteString("/F ")
		buf.Write(img.Filter.Write())
		buf.WriteByte('\n')
	}
	if img.Height != nil {
		buf.WriteString("/H ")
		buf.Write(img.Height.Write())
		buf.WriteByte('\n')
	}
	if img.ImageMask != nil {
		buf.WriteString("/IM ")
		buf.Write(img.ImageMask.Write())
		buf.WriteByte('\n')
	}
	if img.Intent != nil {
		buf.WriteString("/Intent ")
		buf.Write(img.Intent.Write())
		buf.WriteByte('\n')
	}
	if img.Interpolate != nil {
		buf.WriteString("/I ")
		buf.Write(img.Interpolate.Write())
		buf.WriteByte('\n')
	}
	if img.Width != nil {
		buf.WriteString("/W ")
		buf.Write(img.Width.Write())
		buf.WriteByte('\n')
	}
	return buf.String()
}

func (img *ContentStreamInlineImage) toImageBase(resources *model.PdfPageResources) (*imageutil.ImageBase, error) {
	if img.imageBase != nil {
		return img.imageBase, nil
	}
	v := imageutil.ImageBase{}
	if img.Height == nil {
		return nil, errors.New("height attribute missing")
	}
	num, ok := img.Height.(*core.PdfObjectInteger)
	if !ok {
		return nil, errors.New("invalid height")
	}
	v.Height = int(*num)
	if img.Width == nil {
		return nil, errors.New("width attribute missing")
	}
	num2, ok := img.Width.(*core.PdfObjectInteger)
	if !ok {
		return nil, errors.New("invalid width")
	}
	v.Width = int(*num2)
	ok2, err := img.IsMask()
	if err != nil {
		return nil, err
	}
	if ok2 {
		v.BitsPerComponent = 1
		v.ColorComponents = 1
	} else {
		if img.BitsPerComponent == nil {
			common.Log.Debug("Inline Bits per component missing - assuming 8")
			v.BitsPerComponent = 8
		} else {
			num3, ok3 := img.BitsPerComponent.(*core.PdfObjectInteger)
			if !ok3 {
				common.Log.Debug("Error invalid bits per component value, type %T", img.BitsPerComponent)
				return nil, errors.New("BPC Type error")
			}
			v.BitsPerComponent = int(*num3)
		}
		if img.ColorSpace != nil {
			cs, err2 := img.GetColorSpace(resources)
			if err2 != nil {
				return nil, err2
			}
			img.colorspace = cs
			v.ColorComponents = cs.GetNumComponents()
		} else {
			common.Log.Debug("Inline Image colorspace not specified - assuming 1 color component")
			v.ColorComponents = 1
		}
	}
	if arr, ok3 := core.GetArray(img.Decode); ok3 {
		v.Decode, err = arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
	}
	img.imageBase = &v
	return img.imageBase, nil
}
