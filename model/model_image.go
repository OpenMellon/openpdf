package model

import (
	"bytes"
	"image"
	_ "image/gif"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/imageutil/iccextract"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
)

// DefaultImageHandler is the default implementation of the ImageHandler using the standard go library.
type DefaultImageHandler struct{}

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

// DefaultFont returns the default font, which is currently the built in Helvetica.
func DefaultFont() *PdfFont {
	stdFontByName, ok := fonts.NewStdFontByName(HelveticaName)
	if !ok {
		panic("Helvetica should always be available")
	}
	fontSimple := newPdfFontSimpleFromStdFont(stdFontByName)
	return &PdfFont{context: &fontSimple}
}
