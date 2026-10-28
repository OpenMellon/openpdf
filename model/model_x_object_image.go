package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
)

// NewXObjectImageFromImageLazy creates a new XObject Image from an image object
// with default options. If encoder is nil, uses raw encoding (none).
// If lazy is true, then lazy mode is enabled for XObject.
// Lazy mode allows to reduce memory usage with the help of temporary files.
func NewXObjectImageFromImageLazy(img *Image, cs PdfColorspace, encoder core.StreamEncoder, lazy bool) (*XObjectImage, error) {
	xObjectImage := NewXObjectImage()
	if lazy {
		updateXObjectImage, err := UpdateXObjectImageFromImage(xObjectImage, img, cs, encoder)
		if err != nil {
			return nil, err
		}
		if err := updateXObjectImage.MakeLazy(); err != nil {
			return nil, err
		}
		return updateXObjectImage, nil
	}
	return UpdateXObjectImageFromImage(xObjectImage, img, cs, encoder)
}

// NewXObjectImageFromImage creates a new XObject Image from an image object
// with default options. If encoder is nil, uses raw encoding (none).
func NewXObjectImageFromImage(img *Image, cs PdfColorspace, encoder core.StreamEncoder) (*XObjectImage, error) {
	xObjectImage := NewXObjectImage()
	return UpdateXObjectImageFromImage(xObjectImage, img, cs, encoder)
}

// NewXObjectImage returns a new XObjectImage.
func NewXObjectImage() *XObjectImage {
	xobj := &XObjectImage{}
	stream := &core.PdfObjectStream{}
	stream.PdfObjectDictionary = core.MakeDict()
	xobj.primitive = stream
	return xobj
}

// GetContainingPdfObject returns the container of the image object (indirect object).
func (x *XObjectImage) GetContainingPdfObject() core.PdfObject { return x.primitive }

// NewXObjectImageFromStream builds the image xobject from a stream object.
// An image dictionary is the dictionary portion of a stream object representing an image XObject.
func NewXObjectImageFromStream(stream *core.PdfObjectStream) (*XObjectImage, error) {
	xobj := &XObjectImage{}
	xobj.primitive = stream
	dict := stream.PdfObjectDictionary
	encoder, err := core.NewEncoderFromStream(stream)
	if err != nil {
		return nil, err
	}
	xobj.Filter = encoder
	if width := core.TraceToDirectObject(dict.Get("Width")); width != nil {
		num, ok := width.(*core.PdfObjectInteger)
		if !ok {
			return nil, errors.New("invalid image width object")
		}
		n := int64(*num)
		xobj.Width = &n
	} else {
		return nil, errors.New("width missing")
	}
	if height := core.TraceToDirectObject(dict.Get("Height")); height != nil {
		num, ok := height.(*core.PdfObjectInteger)
		if !ok {
			return nil, errors.New("invalid image height object")
		}
		n := int64(*num)
		xobj.Height = &n
	} else {
		return nil, errors.New("height missing")
	}
	if colorSpace := core.TraceToDirectObject(dict.Get("ColorSpace")); colorSpace != nil {
		cs, err := NewPdfColorspaceFromPdfObject(colorSpace)
		if err != nil {
			return nil, err
		}
		xobj.ColorSpace = cs
	} else {
		common.Log.Debug("XObject Image colorspace not specified - assuming 1 color component - DeviceGray")
		xobj.ColorSpace = NewPdfColorspaceDeviceGray()
	}
	if bitsPerComponent := core.TraceToDirectObject(dict.Get("BitsPerComponent")); bitsPerComponent != nil {
		num, ok := bitsPerComponent.(*core.PdfObjectInteger)
		if !ok {
			return nil, errors.New("invalid image height object")
		}
		n := int64(*num)
		xobj.BitsPerComponent = &n
	}
	xobj.Intent = dict.Get("Intent")
	xobj.ImageMask = dict.Get("ImageMask")
	xobj.Mask = dict.Get("Mask")
	xobj.Decode = dict.Get("Decode")
	xobj.Interpolate = dict.Get("Interpolate")
	xobj.Alternatives = dict.Get("Alternatives")
	xobj.SMask = dict.Get("SMask")
	xobj.SMaskInData = dict.Get("SMaskInData")
	xobj.Matte = dict.Get("Matte")
	xobj.Name = dict.Get("Name")
	xobj.StructParent = dict.Get("StructParent")
	xobj.ID = dict.Get("ID")
	xobj.OPI = dict.Get("OPI")
	xobj.Metadata = dict.Get("Metadata")
	xobj.OC = dict.Get("OC")
	xobj.Stream = stream.Stream
	return xobj, nil
}

// XObjectImage (Table 89 in 8.9.5.1).
// Implements PdfModel interface.
type XObjectImage struct {
	// ColorSpace       PdfObject
	Width            *int64
	Height           *int64
	ColorSpace       PdfColorspace
	BitsPerComponent *int64
	Filter           core.StreamEncoder
	Intent           core.PdfObject
	ImageMask        core.PdfObject
	Mask             core.PdfObject
	Matte            core.PdfObject
	Decode           core.PdfObject
	Interpolate      core.PdfObject
	Alternatives     core.PdfObject
	SMask            core.PdfObject
	SMaskInData      core.PdfObject
	Name             core.PdfObject
	StructParent     core.PdfObject
	ID               core.PdfObject
	OPI              core.PdfObject
	Metadata         core.PdfObject
	OC               core.PdfObject
	Stream           []byte
	primitive        *core.PdfObjectStream
	building         bool
}

// ToPdfObject returns a stream object.
func (x *XObjectImage) ToPdfObject() core.PdfObject {
	stream := x.primitive
	if x.building {
		return stream
	}
	dict := stream.PdfObjectDictionary
	if x.Filter != nil {
		dict = x.Filter.MakeStreamDict()
		stream.PdfObjectDictionary = dict
	}
	dict.Set("Type", core.MakeName("XObject"))
	dict.Set("Subtype", core.MakeName("Image"))
	dict.Set("Width", core.MakeInteger(*x.Width))
	dict.Set("Height", core.MakeInteger(*x.Height))
	if x.BitsPerComponent != nil {
		dict.Set("BitsPerComponent", core.MakeInteger(*x.BitsPerComponent))
	}
	if x.ColorSpace != nil {
		dict.SetIfNotNil("ColorSpace", x.ColorSpace.ToPdfObject())
	}
	dict.SetIfNotNil("Intent", x.Intent)
	dict.SetIfNotNil("ImageMask", x.ImageMask)
	dict.SetIfNotNil("Mask", x.Mask)
	ok := dict.Get("Decode") != nil
	if x.Decode == nil && ok {
		dict.Remove("Decode")
	} else if x.Decode != nil {
		dict.Set("Decode", x.Decode)
	}
	dict.SetIfNotNil("Interpolate", x.Interpolate)
	dict.SetIfNotNil("Alternatives", x.Alternatives)
	dict.SetIfNotNil("SMask", x.SMask)
	dict.SetIfNotNil("SMaskInData", x.SMaskInData)
	dict.SetIfNotNil("Matte", x.Matte)
	dict.SetIfNotNil("Name", x.Name)
	dict.SetIfNotNil("StructParent", x.StructParent)
	dict.SetIfNotNil("ID", x.ID)
	dict.SetIfNotNil("OPI", x.OPI)
	dict.SetIfNotNil("Metadata", x.Metadata)
	dict.SetIfNotNil("OC", x.OC)
	dict.Set("Length", core.MakeInteger(int64(len(x.Stream))))
	stream.Stream = x.Stream
	x.building = true
	return stream
}

// MakeLazy prepares the XObject's stream object and moves the encoded stream
// data into a temporary file, reducing memory usage. The stream dictionary is
// frozen by this call: field changes made afterwards are not reflected in the
// written object, so attach entries such as SMask or Mask before calling it.
func (x *XObjectImage) MakeLazy() error {
	x.ToPdfObject()
	if err := x.primitive.MakeLazy(); err != nil {
		return err
	}
	x.Stream = nil
	return nil
}

// SetFilter sets compression filter. Decodes with current filter sets and
// encodes the data with the new filter.
func (x *XObjectImage) SetFilter(encoder core.StreamEncoder) error {
	stream := x.Stream
	data, err := x.Filter.DecodeBytes(stream)
	if err != nil {
		return err
	}
	x.Filter = encoder
	encoder.UpdateParams(x.getParamsDict())
	stream, err = encoder.EncodeBytes(data)
	if err != nil {
		return err
	}
	x.Stream = stream
	return nil
}

// SetImage updates XObject Image with new image data.
//
// If `img` was loaded from a JPEG and the current filter is a DCTEncoder, the
// original JPEG bytes are embedded unchanged (filter settings such as quality
// are not applied); set a non-DCT filter to force a re-encode. When `cs` is nil
// and `img` carries a compatible embedded ICC profile, the colorspace is an
// ICCBased wrapping the device colorspace.
func (x *XObjectImage) SetImage(img *Image, cs PdfColorspace) error {
	x.Filter.UpdateParams(img.GetParamsDict())
	data, err := encodeImageData(x.Filter, img)
	if err != nil {
		return err
	}
	x.Stream = data
	width := img.Width
	x.Width = &width
	height := img.Height
	x.Height = &height
	bitsPerComponent := img.BitsPerComponent
	x.BitsPerComponent = &bitsPerComponent
	if cs == nil {
		if img.ColorComponents == 1 {
			x.ColorSpace = NewPdfColorspaceDeviceGray()
		} else if img.ColorComponents == 3 {
			x.ColorSpace = NewPdfColorspaceDeviceRGB()
		} else if img.ColorComponents == 4 {
			x.ColorSpace = NewPdfColorspaceDeviceCMYK()
		} else {
			return errors.New("colorspace undefined")
		}
		x.ColorSpace = resolveColorspaceWithICC(x.ColorSpace, img)
	} else {
		x.ColorSpace = cs
	}
	return nil
}

// NewXObjectImageStencilMask creates a 1-bit stencil mask XObject
// (/ImageMask true) from `img`, suitable for use as the /Mask entry of a base
// image XObject (explicit masking, PDF 32000-1:2008 8.9.6).
//
// The mask follows soft-mask conventions: white (high) samples mark areas of
// the base image that are painted, black (low) samples mark areas that are
// masked out (a Decode array maps them to the stencil semantics of 8.9.6.4).
// The optional `threshold` selects the grayscale luminance (0-255) at or
// above which a sample counts as white; when omitted, an image with more than
// one bit per component is binarized using a histogram-based auto threshold
// (see Image.ConvertToBinary). An image that is already 1 bit per component
// is used bit-for-bit (its decode array, if any, is honored) and `threshold`
// is ignored. `img` itself is left unmodified.
//
// If `encoder` is nil, flate encoding is used. DCT encoding cannot represent
// 1-bit data, and predictor-based flate/LZW encoding does not support the
// packed 1-bit rows; both are rejected.
func NewXObjectImageStencilMask(img *Image, encoder core.StreamEncoder, threshold ...uint8) (*XObjectImage, error) {
	if img == nil {
		return nil, errors.New("image is nil")
	}
	if len(threshold) > 1 {
		return nil, fmt.Errorf("expected at most one threshold value, got %d", len(threshold))
	}
	if encoder == nil {
		encoder = core.NewFlateEncoder()
	} else if err := validateStencilMaskEncoder(encoder); err != nil {
		return nil, err
	}
	img2 := *img
	nums := []int{1, 0}
	if img2.ColorComponents == 1 && img2.BitsPerComponent == 1 {
		if len(img2.decode) == 2 && img2.decode[0] > img2.decode[1] {
			nums = []int{0, 1}
		}
	} else if len(threshold) == 1 {
		img3, err := thresholdImage(&img2, threshold[0])
		if err != nil {
			return nil, err
		}
		img2 = *img3
	} else if err := img2.ConvertToBinary(); err != nil {
		return nil, err
	}
	encoder.UpdateParams(img2.GetParamsDict())
	encodeBytes, err := encoder.EncodeBytes(img2.Data)
	if err != nil {
		common.Log.Debug("Error with encoding: %v", err)
		return nil, err
	}
	xObjectImage := NewXObjectImage()
	xObjectImage.Width = &img2.Width
	xObjectImage.Height = &img2.Height
	xObjectImage.BitsPerComponent = &img2.BitsPerComponent
	xObjectImage.ImageMask = core.MakeBool(true)
	xObjectImage.Decode = core.MakeArrayFromIntegers(nums)
	xObjectImage.Filter = encoder
	xObjectImage.Stream = encodeBytes
	return xObjectImage, nil
}

func (x *XObjectImage) getParamsDict() *core.PdfObjectDictionary {
	dict := core.MakeDict()
	dict.Set("Width", core.MakeInteger(*x.Width))
	dict.Set("Height", core.MakeInteger(*x.Height))
	dict.Set("ColorComponents", core.MakeInteger(int64(x.ColorSpace.GetNumComponents())))
	dict.Set("BitsPerComponent", core.MakeInteger(*x.BitsPerComponent))
	return dict
}

// ToImage converts an object to an Image which can be transformed or saved out.
// The image data is decoded and the Image returned.
func (x *XObjectImage) ToImage() (*Image, error) {
	img := &Image{}
	if x.Height == nil {
		return nil, errors.New("height attribute missing")
	}
	img.Height = *x.Height
	if x.Width == nil {
		return nil, errors.New("width attribute missing")
	}
	img.Width = *x.Width
	if x.BitsPerComponent == nil {
		switch x.Filter.(type) {
		case *core.CCITTFaxEncoder, *core.JBIG2Encoder:
			img.BitsPerComponent = 1
		case *core.LZWEncoder, *core.RunLengthEncoder:
			img.BitsPerComponent = 8
		default:
			return nil, errors.New("bits per component missing")
		}
	} else {
		img.BitsPerComponent = *x.BitsPerComponent
	}
	img.ColorComponents = x.ColorSpace.GetNumComponents()
	x.primitive.Set("ColorComponents", core.MakeInteger(int64(img.ColorComponents)))
	stream, err := core.DecodeStream(x.primitive)
	if err != nil {
		return nil, err
	}
	img.Data = stream
	if x.Decode != nil {
		arr, ok := x.Decode.(*core.PdfObjectArray)
		if !ok {
			common.Log.Debug("Invalid Decode object")
			return nil, errors.New("invalid type")
		}
		floats, err := arr.ToFloat64Array()
		if err != nil {
			return nil, err
		}
		switch x.ColorSpace.(type) {
		case *PdfColorspaceSpecialIndexed:
			if normalizeIndexedDecode, ok := imageutil.NormalizeIndexedDecode(floats, int(img.BitsPerComponent)); ok {
				img.decode = normalizeIndexedDecode
			}
		case *PdfColorspaceDeviceCMYK:
			array := x.ColorSpace.DecodeArray()
			if array[0] == floats[0] && array[1] == floats[1] && array[2] == floats[2] && array[3] == floats[3] {
				img.decode = floats
			}
		default:
			img.decode = floats
		}
	}
	return img, nil
}
