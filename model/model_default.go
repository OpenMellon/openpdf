package model

import (
	"bytes"
	"image"
	_ "image/gif"
	"io"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/imageutil/iccextract"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

// NewImageFromGoImage creates a new NRGBA32 OpenPDF Image from a golang Image.
// If `goimg` is grayscale (*goimage.Gray8) then calls NewGrayImageFromGoImage instead.
func (h DefaultImageHandler) NewImageFromGoImage(goimg image.Image) (*Image, error) {
	fromGoImage, err := imageutil.FromGoImage(goimg)
	if err != nil {
		return nil, err
	}
	img := imageFromImageBase(fromGoImage.Base())
	return &img, nil
}

func parseDefaultAppearance(s string) (s2 string, f float64, col PdfColor) {
	if s == "" {
		return s2, f, col
	}
	var strs []string
	fn := func(n int) ([]string, bool) {
		if len(strs) < n {
			return nil, false
		}
		strs2 := append([]string(nil), strs[len(strs)-n:]...)
		strs = strs[:len(strs)-n]
		return strs2, true
	}
	fn2 := func(strs []string) ([]float64, bool) {
		floats := make([]float64, len(strs))
		for i, str := range strs {
			f, err := strconv.ParseFloat(str, 64)
			if err != nil {
				return nil, false
			}
			floats[i] = f
		}
		return floats, true
	}
	for field := range strings.FieldsSeq(s) {
		switch field {
		case "Tf":
			strs, ok := fn(2)
			if !ok {
				continue
			}
			if strings.HasPrefix(strs[0], "/") {
				s2 = strs[0][1:]
			}
			if v, err := strconv.ParseFloat(strs[1], 64); err == nil {
				f = v
			}
		case "g", "G":
			strs, ok := fn(1)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceGray().ColorFromFloats(floats); err == nil {
				col = col2
			}
		case "rg", "RG":
			strs, ok := fn(3)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceRGB().ColorFromFloats(floats); err == nil {
				col = col2
			}
		case "k", "K":
			strs, ok := fn(4)
			if !ok {
				continue
			}
			floats, ok := fn2(strs)
			if !ok {
				continue
			}
			if col2, err := NewPdfColorspaceDeviceCMYK().ColorFromFloats(floats); err == nil {
				col = col2
			}
		default:
			strs = append(strs, field)
		}
	}
	return s2, f, col
}

// DefaultImageHandler is the default implementation of the ImageHandler using the standard go library.
type DefaultImageHandler struct{}

// Read reads an image and loads into a new Image object with an RGB
// colormap and 8 bits per component.
//
// Read buffers the whole stream so it can decode the pixels and also retain the
// original encoded bytes and any embedded ICC profile on the returned Image
// (EncodedData/EncodedFormat/ICCProfile). This lets the profile be preserved
// and JPEG sources be embedded without a lossy re-encode. The retained data is
// cleared by pixel-mutating operations.
func (h DefaultImageHandler) Read(reader io.Reader) (*Image, error) {
	all, err := io.ReadAll(reader)
	if err != nil {
		common.Log.Debug("Error reading image data: %s", err)
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(all))
	if err != nil {
		common.Log.Debug("Error decoding file: %s", err)
		return nil, err
	}
	img2, err := h.NewImageFromGoImage(img)
	if err != nil {
		return nil, err
	}
	extract, format := iccextract.Extract(all)
	if format == iccextract.FormatJPEG {
		img2.encodedData = all
		img2.encodedFormat = string(format)
	}
	img2.iccProfile = extract
	return img2, nil
}

// DefaultFont returns the default font, which is currently the built in Helvetica.
func DefaultFont() *PdfFont {
	stdFontByName, ok := fonts.NewStdFontByName(HelveticaName)
	if !ok {
		panic("Helvetica should always be available")
	}
	fontSimple := newPdfFontSimpleFromStdFont(stdFontByName)
	return &PdfFont{context: &fontSimple}
}

// Compress is yet to be implemented.
// Should be able to compress in terms of JPEG quality parameter,
// and DPI threshold (need to know bounding area dimensions).
func (h DefaultImageHandler) Compress(input *Image, quality int64) (*Image, error) {
	return input, nil
}

// NewGrayImageFromGoImage creates a new grayscale OpenPDF Image from a golang Image.
func (h DefaultImageHandler) NewGrayImageFromGoImage(goimg image.Image) (*Image, error) {
	bounds := goimg.Bounds()
	img := &Image{Width: int64(bounds.Dx()), Height: int64(bounds.Dy()), ColorComponents: 1, BitsPerComponent: 8}
	switch v := goimg.(type) {
	case *image.Gray:
		if len(v.Pix) != bounds.Dx()*bounds.Dy() {
			convert, err := imageutil.GrayConverter.Convert(goimg)
			if err != nil {
				return nil, err
			}
			img.Data = convert.Pix()
		} else {
			img.Data = v.Pix
		}
	case *image.Gray16:
		img.BitsPerComponent = 16
		if len(v.Pix) != bounds.Dx()*bounds.Dy()*2 {
			convert, err := imageutil.Gray16Converter.Convert(goimg)
			if err != nil {
				return nil, err
			}
			img.Data = convert.Pix()
		} else {
			img.Data = v.Pix
		}
	case imageutil.Image:
		base := v.Base()
		if base.ColorComponents == 1 {
			img.BitsPerComponent = int64(base.BitsPerComponent)
			img.Data = base.Data
			return img, nil
		}
		convert, err := imageutil.GrayConverter.Convert(goimg)
		if err != nil {
			return nil, err
		}
		img.Data = convert.Pix()
	default:
		convert, err := imageutil.GrayConverter.Convert(goimg)
		if err != nil {
			return nil, err
		}
		img.Data = convert.Pix()
	}
	return img, nil
}
