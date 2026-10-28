package core

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/imageutil"
)

// DCTEncoder provides a DCT (JPG) encoding/decoding functionality for images.
type DCTEncoder struct {
	ColorComponents  int
	BitsPerComponent int
	Width            int
	Height           int
	Quality          int
	Decode           []float64
	limits           decodeLimits
}

func newDCTEncoderFromStream(streamObj *PdfObjectStream, multi *MultiEncoder) (*DCTEncoder, error) {
	dctEncoder := NewDCTEncoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return dctEncoder, nil
	}
	var reader io.Reader = bytes.NewReader(streamObj.Stream)
	if multi != nil {
		reader = multi.DecodeReader(reader)
		if c, ok := reader.(io.Closer); ok {
			defer c.Close()
		}
	}
	config, err := jpeg.DecodeConfig(reader)
	if err != nil {
		common.Log.Debug("Error decoding file: %s", err)
		return nil, err
	}
	switch config.ColorModel {
	case color.RGBAModel:
		dctEncoder.BitsPerComponent = 8
		dctEncoder.ColorComponents = 3
		dctEncoder.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	case color.RGBA64Model:
		dctEncoder.BitsPerComponent = 16
		dctEncoder.ColorComponents = 3
		dctEncoder.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	case color.GrayModel:
		dctEncoder.BitsPerComponent = 8
		dctEncoder.ColorComponents = 1
		dctEncoder.Decode = []float64{0.0, 1.0}
	case color.Gray16Model:
		dctEncoder.BitsPerComponent = 16
		dctEncoder.ColorComponents = 1
		dctEncoder.Decode = []float64{0.0, 1.0}
	case color.CMYKModel:
		dctEncoder.BitsPerComponent = 8
		dctEncoder.ColorComponents = 4
		dctEncoder.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	case color.YCbCrModel:
		dctEncoder.BitsPerComponent = 8
		dctEncoder.ColorComponents = 3
		dctEncoder.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	default:
		return nil, errors.New("unsupported color model")
	}
	dctEncoder.Width = config.Width
	dctEncoder.Height = config.Height
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("DCT Encoder: %+v", dctEncoder)
	}
	dctEncoder.Quality = DefaultJPEGQuality
	decode, ok := GetArray(pdfObjectDictionary.Get("Decode"))
	if ok {
		floats, err2 := decode.ToFloat64Array()
		if err2 != nil {
			return dctEncoder, err2
		}
		dctEncoder.Decode = floats
	}
	return dctEncoder, nil
}

// DecodeStream decodes a DCT encoded stream and returns the result as a
// slice of bytes.
func (dct *DCTEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return dct.DecodeBytes(streamObj.Stream)
}

func (dct *DCTEncoder) setDecodeLimits(limits decodeLimits) { dct.limits = limits }

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
// Has the Filter set.  Some other parameters are generated elsewhere.
func (dct *DCTEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(dct.GetFilterName()))
	return dict
}

// EncodeBytes DCT encodes the passed in slice of bytes.
func (dct *DCTEncoder) EncodeBytes(data []byte) ([]byte, error) {
	var img image.Image
	if dct.ColorComponents == 1 && dct.BitsPerComponent == 8 {
		img = &image.Gray{Rect: image.Rect(0, 0, dct.Width, dct.Height), Pix: data, Stride: imageutil.BytesPerLine(dct.Width, dct.BitsPerComponent, dct.ColorComponents)}
	} else {
		var err error
		img, err = imageutil.NewImage(dct.Width, dct.Height, dct.BitsPerComponent, dct.ColorComponents, data, nil, nil)
		if err != nil {
			return nil, err
		}
	}
	options := jpeg.Options{}
	options.Quality = dct.Quality
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &options); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UpdateParams updates the parameter values of the encoder.
func (dct *DCTEncoder) UpdateParams(params *PdfObjectDictionary) {
	colorComponents, err := GetNumberAsInt64(params.Get("ColorComponents"))
	if err == nil {
		dct.ColorComponents = int(colorComponents)
	}
	bitsPerComponent, err := GetNumberAsInt64(params.Get("BitsPerComponent"))
	if err == nil {
		dct.BitsPerComponent = int(bitsPerComponent)
	}
	width, err := GetNumberAsInt64(params.Get("Width"))
	if err == nil {
		dct.Width = int(width)
	}
	height, err := GetNumberAsInt64(params.Get("Height"))
	if err == nil {
		dct.Height = int(height)
	}
	quality, err := GetNumberAsInt64(params.Get("Quality"))
	if err == nil {
		dct.Quality = int(quality)
	}
	decode, ok := GetArray(params.Get("Decode"))
	if ok {
		dct.Decode, err = decode.ToFloat64Array()
		if err != nil {
			common.Log.Error("Failed converting decode object into arrays: %v", err)
		}
	}
}

// DecodeBytes decodes a slice of DCT encoded bytes and returns the result.
func (dct *DCTEncoder) DecodeBytes(encoded []byte) (decoded []byte, err error) {
	config, err := jpeg.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		common.Log.Debug("Error decoding image: %s", err)
		return nil, err
	}
	components := int64(dct.ColorComponents)
	switch config.ColorModel {
	case color.CMYKModel:
		components = max(components, 4)
	case color.YCbCrModel, color.RGBAModel:
		components = max(components, 3)
	}
	size := int64(config.Width) * int64(config.Height) * components * int64(max(dct.BitsPerComponent, 8)) / 8
	if err2 := dct.limits.check(StreamEncodingFilterNameDCT, size); err2 != nil {
		return nil, err2
	}
	reader := bytes.NewReader(encoded)
	img, err := jpeg.Decode(reader)
	if err != nil {
		common.Log.Debug("Error decoding image: %s", err)
		return nil, err
	}
	rectangle := img.Bounds()
	bytes := AcquireBytes(rectangle.Dx() * rectangle.Dy() * dct.ColorComponents * dct.BitsPerComponent / 8)
	defer func() {
		if err != nil {
			ReleaseBytes(bytes)
		}
	}()
	pos := 0
	switch dct.ColorComponents {
	case 1:
		floats := []float64{dct.Decode[0], dct.Decode[1]}
		for i := rectangle.Min.Y; i < rectangle.Max.Y; i++ {
			for j := rectangle.Min.X; j < rectangle.Max.X; j++ {
				c := img.At(j, i)
				if dct.BitsPerComponent == 16 {
					gray16, ok := c.(color.Gray16)
					if !ok {
						return nil, errors.New("color type error")
					}
					v8 := scaleDecodeValue(uint(gray16.Y>>8), floats[0], floats[1])
					v16 := scaleDecodeValue(uint(gray16.Y), floats[0], floats[1])
					bytes[pos] = byte(v8)
					pos++
					bytes[pos] = byte(v16)
					pos++
				} else {
					gray, ok := c.(color.Gray)
					if !ok {
						return nil, errors.New("color type error")
					}
					bytes[pos] = byte(scaleDecodeValue(uint(gray.Y), floats[0], floats[1]))
					pos++
				}
			}
		}
	case 3:
		floats := []float64{dct.Decode[0], dct.Decode[1]}
		decG := []float64{dct.Decode[2], dct.Decode[3]}
		decB := []float64{dct.Decode[4], dct.Decode[5]}
		for i := rectangle.Min.Y; i < rectangle.Max.Y; i++ {
			for j := rectangle.Min.X; j < rectangle.Max.X; j++ {
				c := img.At(j, i)
				if dct.BitsPerComponent == 16 {
					rgbA64, ok := c.(color.RGBA64)
					if !ok {
						return nil, errors.New("color type error")
					}
					r8 := scaleDecodeValue(uint(rgbA64.R>>8), floats[0], floats[1])
					r16 := scaleDecodeValue(uint(rgbA64.R), floats[0], floats[1])
					g8 := scaleDecodeValue(uint(rgbA64.G>>8), decG[0], decG[1])
					g16 := scaleDecodeValue(uint(rgbA64.G), decG[0], decG[1])
					b8 := scaleDecodeValue(uint(rgbA64.B>>8), decB[0], decB[1])
					b16 := scaleDecodeValue(uint(rgbA64.B), decB[0], decB[1])
					bytes[pos] = byte(r8)
					pos++
					bytes[pos] = byte(r16)
					pos++
					bytes[pos] = byte(g8)
					pos++
					bytes[pos] = byte(g16)
					pos++
					bytes[pos] = byte(b8)
					pos++
					bytes[pos] = byte(b16)
					pos++
				} else {
					rgba, ok := c.(color.RGBA)
					if ok {
						f := scaleDecodeValue(uint(rgba.R), floats[0], floats[1])
						g := scaleDecodeValue(uint(rgba.G), decG[0], decG[1])
						b := scaleDecodeValue(uint(rgba.B), decB[0], decB[1])
						bytes[pos] = byte(f)
						pos++
						bytes[pos] = byte(g)
						pos++
						bytes[pos] = byte(b)
						pos++
					} else {
						yCbCr, isYCbCr := c.(color.YCbCr)
						if !isYCbCr {
							return nil, errors.New("color type error")
						}
						yv, cbv, crv, _ := yCbCr.RGBA()
						y := scaleDecodeValue(uint(yv>>8), floats[0], floats[1])
						cb := scaleDecodeValue(uint(cbv>>8), decG[0], decG[1])
						cr := scaleDecodeValue(uint(crv>>8), decB[0], decB[1])
						bytes[pos] = byte(y)
						pos++
						bytes[pos] = byte(cb)
						pos++
						bytes[pos] = byte(cr)
						pos++
					}
				}
			}
		}
	case 4:
		floats := []float64{dct.Decode[0], dct.Decode[1]}
		decM := []float64{dct.Decode[2], dct.Decode[3]}
		decY := []float64{dct.Decode[4], dct.Decode[5]}
		decK := []float64{dct.Decode[6], dct.Decode[7]}
		for i := rectangle.Min.Y; i < rectangle.Max.Y; i++ {
			for j := rectangle.Min.X; j < rectangle.Max.X; j++ {
				c := img.At(j, i)
				cmyk, ok := c.(color.CMYK)
				if !ok {
					return nil, errors.New("color type error")
				}
				f := 255 - scaleDecodeValue(uint(cmyk.C), floats[0], floats[1])
				m := 255 - scaleDecodeValue(uint(cmyk.M), decM[0], decM[1])
				y := 255 - scaleDecodeValue(uint(cmyk.Y), decY[0], decY[1])
				k := 255 - scaleDecodeValue(uint(cmyk.K), decK[0], decK[1])
				bytes[pos] = byte(f)
				pos++
				bytes[pos] = byte(m)
				pos++
				bytes[pos] = byte(y)
				pos++
				bytes[pos] = byte(k)
				pos++
			}
		}
	}
	dct.limits.charge(int64(len(bytes)))
	return bytes, nil
}

// NewDCTEncoder makes a new DCT encoder with default parameters.
func NewDCTEncoder() *DCTEncoder {
	dct := &DCTEncoder{}
	dct.ColorComponents = 3
	dct.BitsPerComponent = 8
	dct.Quality = DefaultJPEGQuality
	dct.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	return dct
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (dct *DCTEncoder) MakeDecodeParams() PdfObject { return nil }

// GetFilterName returns the name of the encoding filter.
func (dct *DCTEncoder) GetFilterName() string { return StreamEncodingFilterNameDCT }
