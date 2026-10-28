package core

import (
	"image"
	"strings"

	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/jbig2"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/document"
	"github.com/OpenMellon/openpdf/internal/jbig2/document/segments"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"golang.org/x/text/unicode/bidi"
)

func (jbig2Img *JBIG2Image) toBitmap() (bm *bitmap.Bitmap, err error) {
	const procJBIG2ImageToBitmap = "JBIG2Image.toBitmap"
	if jbig2Img.Data == nil {
		return nil, jbig2errors.Error(procJBIG2ImageToBitmap, "image data not defined")
	}
	if jbig2Img.Width == 0 || jbig2Img.Height == 0 {
		return nil, jbig2errors.Error(procJBIG2ImageToBitmap, "image height or width not defined")
	}
	if jbig2Img.HasPadding {
		bm, err = bitmap.NewWithData(jbig2Img.Width, jbig2Img.Height, jbig2Img.Data)
	} else {
		bm, err = bitmap.NewWithUnpaddedData(jbig2Img.Width, jbig2Img.Height, jbig2Img.Data)
	}
	if err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2ImageToBitmap, "")
	}
	return bm, nil
}

// GoImageToJBIG2 creates a binary image on the base of 'i' golang image.Image.
// If the image is not a black/white image then the function converts provided input into
// JBIG2Image with 1bpp. For non grayscale images the function performs the conversion to the grayscale temp image.
// Then it checks the value of the gray image value if it's within bounds of the black white threshold.
// This 'bwThreshold' value should be in range (0.0, 1.0). The threshold checks if the grayscale pixel (uint) value
// is greater or smaller than 'bwThreshold' * 255. Pixels inside the range will be white, and the others will be black.
// If the 'bwThreshold' is equal to -1.0 - JB2ImageAutoThreshold then it's value would be set on the base of
// it's histogram using Triangle method. For more information go to:
//
//	https://www.mathworks.com/matlabcentral/fileexchange/28047-gray-image-thresholding-using-the-triangle-method
func GoImageToJBIG2(i image.Image, bwThreshold float64) (*JBIG2Image, error) {
	const procGoImageToJBIG2 = "GoImageToJBIG2"
	if i == nil {
		return nil, jbig2errors.Error(procGoImageToJBIG2, "image 'i' not defined")
	}
	var (
		b   uint8
		img imageutil.Image
		err error
	)
	if bwThreshold == JB2ImageAutoThreshold {
		img, err = imageutil.MonochromeConverter.Convert(i)
	} else if bwThreshold > 1.0 || bwThreshold < 0.0 {
		return nil, jbig2errors.Error(procGoImageToJBIG2, "provided threshold is not in a range {0.0, 1.0}")
	} else {
		b = uint8(255 * bwThreshold)
		img, err = imageutil.MonochromeThresholdConverter(b).Convert(i)
	}
	if err != nil {
		return nil, err
	}
	return newJBIG2ImageFromImage(img), nil
}

// Validate validates the page settings for the JBIG2 encoder.
func (jbiG2EncoderSettings JBIG2EncoderSettings) Validate() error {
	const procValidateEncoder = "validateEncoder"
	if jbiG2EncoderSettings.Threshold < 0 || jbiG2EncoderSettings.Threshold > 1.0 {
		return jbig2errors.Errorf(procValidateEncoder, "provided threshold value: '%v' must be in range [0.0, 1.0]", jbiG2EncoderSettings.Threshold)
	}
	if jbiG2EncoderSettings.ResolutionX < 0 {
		return jbig2errors.Errorf(procValidateEncoder, "provided x resolution: '%d' must be positive or zero value", jbiG2EncoderSettings.ResolutionX)
	}
	if jbiG2EncoderSettings.ResolutionY < 0 {
		return jbig2errors.Errorf(procValidateEncoder, "provided y resolution: '%d' must be positive or zero value", jbiG2EncoderSettings.ResolutionY)
	}
	if jbiG2EncoderSettings.DefaultPixelValue != 0 && jbiG2EncoderSettings.DefaultPixelValue != 1 {
		return jbig2errors.Errorf(procValidateEncoder, "default pixel value: '%d' must be a value for the bit: {0,1}", jbiG2EncoderSettings.DefaultPixelValue)
	}
	if jbiG2EncoderSettings.Compression != JB2Generic {
		return jbig2errors.Errorf(procValidateEncoder, "provided compression is not implemented yet")
	}
	return nil
}

const (
	JB2Generic JBIG2CompressionType = iota
	JB2SymbolCorrelation
	JB2SymbolRankHaus
)

// JBIG2EncoderSettings contains the parameters and settings used by the JBIG2Encoder.
// Current version works only on JB2Generic compression.
type JBIG2EncoderSettings struct {
	// FileMode defines if the jbig2 encoder should return full jbig2 file instead of
	// shortened pdf mode. This adds the file header to the jbig2 definition.
	FileMode bool

	// Compression is the setting that defines the compression type used for encoding the page.
	Compression JBIG2CompressionType

	// DuplicatedLinesRemoval code generic region in a way such that if the lines are duplicated the encoder
	// doesn't store it twice.
	DuplicatedLinesRemoval bool

	// DefaultPixelValue is the bit value initial for every pixel in the page.
	DefaultPixelValue uint8

	// ResolutionX optional setting that defines the 'x' axis input image resolution - used for single page encoding.
	ResolutionX int

	// ResolutionY optional setting that defines the 'y' axis input image resolution - used for single page encoding.
	ResolutionY int

	// Threshold defines the threshold of the image correlation for
	// non Generic compression.
	// User only for JB2SymbolCorrelation and JB2SymbolRankHaus methods.
	// Best results in range [0.7 - 0.98] - the less the better the compression would be
	// but the more lossy.
	// Default value: 0.95
	Threshold float64
}

// ToGoImage converts the JBIG2Image to the golang image.Image.
func (jbig2Img *JBIG2Image) ToGoImage() (image.Image, error) {
	const procJBIG2ImageToGoImage = "JBIG2Image.ToGoImage"
	if jbig2Img.Data == nil {
		return nil, jbig2errors.Error(procJBIG2ImageToGoImage, "image data not defined")
	}
	if jbig2Img.Width == 0 || jbig2Img.Height == 0 {
		return nil, jbig2errors.Error(procJBIG2ImageToGoImage, "image height or width not defined")
	}
	image, err := imageutil.NewImage(jbig2Img.Width, jbig2Img.Height, 1, 1, jbig2Img.Data, nil, nil)
	if err != nil {
		return nil, err
	}
	return image, nil
}

// FormatWriteDirectionLTR formats a string for left-to-right rendering, handling bidirectional runs.
func FormatWriteDirectionLTR(str string, isLTR bool) string {
	s := str
	if s == "" {
		return ""
	}
	paragraph := bidi.Paragraph{}
	_, err := paragraph.SetString(str)
	if err != nil {
		return s
	}
	ordering, err := paragraph.Order()
	if err != nil {
		return s
	}
	numRuns := ordering.NumRuns()
	strs := make([]string, numRuns)
	for i := 0; i < ordering.NumRuns(); i++ {
		run := ordering.Run(i)
		text := run.String()
		if run.Direction() == bidi.RightToLeft {
			text = bidi.ReverseString(text)
		}
		if isLTR {
			strs[i] = text
		} else {
			strs[numRuns-1] = text
		}
		numRuns--
	}
	if len(strs) != ordering.NumRuns() {
		return str
	}
	s = strings.Join(strs, "")
	return s
}

func newJBIG2ImageFromImage(img imageutil.Image) *JBIG2Image {
	base := img.Base()
	return &JBIG2Image{Data: base.Data, Width: base.Width, Height: base.Height, HasPadding: true}
}

// JBIG2CompressionType defines the enum compression type used by the JBIG2Encoder.
type JBIG2CompressionType int

func isJBIG2Stream(stream *PdfObjectStream) bool {
	switch v := TraceToDirectObject(stream.Get("Filter")).(type) {
	case *PdfObjectName:
		return v.String() == StreamEncodingFilterNameJBIG2
	case *PdfObjectArray:
		for _, elem := range v.Elements() {
			if name, ok := GetName(elem); ok && name.String() == StreamEncodingFilterNameJBIG2 {
				return true
			}
		}
	}
	return false
}

func jbig2FirstPageBitmapSize(encoded []byte, globals jbig2.Globals) (int64, bool) {
	doc, err := document.DecodeDocument(bitwise.NewReader(encoded), globals.ToDocumentGlobals())
	if err != nil || len(doc.Pages) == 0 {
		return 0, false
	}
	first := -1
	for i := range doc.Pages {
		if first < 0 || i < first {
			first = i
		}
	}
	for _, segment := range doc.Pages[first].Segments {
		if segment.Type != segments.TPageInformation {
			continue
		}
		segmentData, err2 := segment.GetSegmentData()
		if err2 != nil {
			return 0, false
		}
		pageInformationSegment, ok := segmentData.(*segments.PageInformationSegment)
		if !ok || pageInformationSegment.PageBMWidth <= 0 || pageInformationSegment.PageBMHeight <= 0 {
			return 0, false
		}
		return (int64(pageInformationSegment.PageBMWidth) + 7) / 8 * int64(pageInformationSegment.PageBMHeight), true
	}
	return 0, false
}

// JBIG2Image is the image structure used by the jbig2 encoder. Its Data must be in a
// 1 bit per component and 1 component per pixel (1bpp). In order to create binary image
// use GoImageToJBIG2 function. If the image data contains the row bytes padding set the HasPadding to true.
type JBIG2Image struct {
	// Width and Height defines the image boundaries.
	Width, Height int

	// Data is the byte slice data for the input image
	Data []byte

	// HasPadding is the attribute that defines if the last byte of the data in the row contains
	// 0 bits padding.
	HasPadding bool
}

const JB2ImageAutoThreshold = -1.0

// IsTextWriteDirectionLTR returns `true` if string write direction Left To Right.
// detect string write direction using golang.org/x/text/unicode/bidi library.
func IsTextWriteDirectionLTR(str string) bool {
	isNewline := func(r rune) bool { return r == '\u000A' }
	trimmed := strings.TrimFunc(str, isNewline)
	paragraph := bidi.Paragraph{}
	_, err := paragraph.SetString(trimmed)
	if err != nil {
		return true
	}
	ordering, err := paragraph.Order()
	if err != nil {
		return true
	}
	if ordering.NumRuns() < 1 {
		return true
	}
	return paragraph.IsLeftToRight()
}
