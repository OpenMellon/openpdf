package core

import (
	"image"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder"
	"github.com/OpenMellon/openpdf/internal/jbig2/document"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

// NewJBIG2Encoder creates a new JBIG2Encoder.
func NewJBIG2Encoder() *JBIG2Encoder { return &JBIG2Encoder{doc: document.InitEncodeDocument(false)} }

// DecodeGlobals decodes 'encoded' byte stream and returns their Globally defined segments ('Globals').
func (jbig22 *JBIG2Encoder) DecodeGlobals(encoded []byte) (jbig2.Globals, error) {
	return jbig2.DecodeGlobals(encoded)
}

func newJBIG2EncoderFromStream(streamObj *PdfObjectStream, decodeParams *PdfObjectDictionary) (*JBIG2Encoder, error) {
	const procNewJBIG2DecoderFromStream = "newJBIG2DecoderFromStream"
	jbiG2Encoder := NewJBIG2Encoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return jbiG2Encoder, nil
	}
	if decodeParams == nil {
		decodeParms := pdfObjectDictionary.Get("DecodeParms")
		if decodeParms != nil {
			switch v := decodeParms.(type) {
			case *PdfObjectDictionary:
				decodeParams = v
			case *PdfObjectArray:
				if v.Len() == 1 {
					if dict, ok := GetDict(v.Get(0)); ok {
						decodeParams = dict
					}
				}
			default:
				common.Log.Error("DecodeParams not a dictionary %#v", decodeParms)
				return nil, jbig2errors.Errorf(procNewJBIG2DecoderFromStream, "invalid DecodeParms type: %T", v)
			}
		}
	}
	if decodeParams == nil {
		return jbiG2Encoder, nil
	}
	jbiG2Encoder.UpdateParams(decodeParams)
	stream, ok := GetStream(decodeParams.Get("JBIG2Globals"))
	if !ok {
		return jbiG2Encoder, nil
	}
	data := stream.Stream
	var err error
	if stream.PdfObjectDictionary != nil && !isJBIG2Stream(stream) {
		data, err = DecodeStream(stream)
		if err != nil {
			err = jbig2errors.Wrap(err, procNewJBIG2DecoderFromStream, "decoding jbig2 globals stream failed")
			common.Log.Debug("ERROR: %v", err)
			return nil, err
		}
	}
	jbiG2Encoder.Globals, err = jbig2.DecodeGlobals(data)
	if err != nil {
		err = jbig2errors.Wrap(err, procNewJBIG2DecoderFromStream, "corrupted jbig2 encoded data")
		common.Log.Debug("ERROR: %v", err)
		return nil, err
	}
	return jbiG2Encoder, nil
}

// EncodeImage encodes 'img' golang image.Image into jbig2 encoded bytes document using default encoder settings.
func (jbig2 *JBIG2Encoder) EncodeImage(img image.Image) ([]byte, error) {
	return jbig2.encodeImage(img)
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (jbig2 *JBIG2Encoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(jbig2.GetFilterName()))
	return dict
}

// DecodeBytes decodes a slice of JBIG2 encoded bytes and returns the results.
func (jbig22 *JBIG2Encoder) DecodeBytes(encoded []byte) ([]byte, error) {
	if size, ok := jbig2FirstPageBitmapSize(encoded, jbig22.Globals); ok {
		if err := jbig22.limits.check(StreamEncodingFilterNameJBIG2, size); err != nil {
			return nil, err
		}
	}
	bytes, err := jbig2.DecodeBytes(encoded, decoder.Parameters{}, jbig22.Globals)
	if err != nil {
		return nil, err
	}
	if err2 := jbig22.limits.check(StreamEncodingFilterNameJBIG2, int64(len(bytes))); err2 != nil {
		return nil, err2
	}
	jbig22.limits.charge(int64(len(bytes)))
	return bytes, nil
}

// AddPageImage adds the page with the image 'img' to the encoder context in order to encode it jbig2 document.
// The 'settings' defines what encoding type should be used by the encoder.
func (jbig2 *JBIG2Encoder) AddPageImage(img *JBIG2Image, settings *JBIG2EncoderSettings) (err error) {
	const procJBIG2DocumentAddPageImage = "JBIG2Document.AddPageImage"
	if jbig2 == nil {
		return jbig2errors.Error(procJBIG2DocumentAddPageImage, "JBIG2Document is nil")
	}
	if settings == nil {
		settings = &jbig2.DefaultPageSettings
	}
	if jbig2.doc == nil {
		jbig2.doc = document.InitEncodeDocument(settings.FileMode)
	}
	if err = settings.Validate(); err != nil {
		return jbig2errors.Wrap(err, procJBIG2DocumentAddPageImage, "")
	}
	bm, err := img.toBitmap()
	if err != nil {
		return jbig2errors.Wrap(err, procJBIG2DocumentAddPageImage, "")
	}
	switch settings.Compression {
	case JB2Generic:
		if err = jbig2.doc.AddGenericPage(bm, settings.DuplicatedLinesRemoval); err != nil {
			return jbig2errors.Wrap(err, procJBIG2DocumentAddPageImage, "")
		}
	case JB2SymbolCorrelation:
		return jbig2errors.Error(procJBIG2DocumentAddPageImage, "symbol correlation encoding not implemented yet")
	case JB2SymbolRankHaus:
		return jbig2errors.Error(procJBIG2DocumentAddPageImage, "symbol rank haus encoding not implemented yet")
	default:
		return jbig2errors.Error(procJBIG2DocumentAddPageImage, "provided invalid compression")
	}
	return nil
}

// GetFilterName returns the name of the encoding filter.
func (jbig2 *JBIG2Encoder) GetFilterName() string { return StreamEncodingFilterNameJBIG2 }

// JBIG2Encoder implements both jbig2 encoder and the decoder. The encoder allows to encode
// provided images (best used document scans) in multiple way. By default it uses single page generic
// encoder. It allows to store lossless data as a single segment.
// In order to store multiple image pages use the 'FileMode' which allows to store more pages within single jbig2 document.
// WIP: In order to obtain better compression results the encoder would allow to encode the input in a
// lossy or lossless way with a component (symbol) mode. It divides the image into components.
// Then checks if any component is 'similar' to the others and maps them together. The symbol classes are stored
// in the dictionary. Then the encoder creates text regions which uses the related symbol classes to fill it's space.
// The similarity is defined by the 'Threshold' variable (default: 0.95). The less the value is, the more components
// matches to single class, thus the compression is better, but the result might become lossy.
type JBIG2Encoder struct {
	// These values are required to be set for the 'EncodeBytes' method.
	// ColorComponents defines the number of color components for provided image.
	ColorComponents int

	// BitsPerComponent is the number of bits that stores per color component
	BitsPerComponent int

	// Width is the width of the image to encode
	Width int

	// Height is the height of the image to encode.
	Height int
	doc    *document.Document

	// Globals are the JBIG2 global segments.
	Globals jbig2.Globals

	// IsChocolateData defines if the data is encoded such that
	// binary data '1' means black and '0' white.
	// otherwise the data is called vanilla.
	// Naming convention taken from: 'https://en.wikipedia.org/wiki/Binary_image#Interpretation'
	IsChocolateData bool

	// DefaultPageSettings are the settings parameters used by the jbig2 encoder.
	DefaultPageSettings JBIG2EncoderSettings
	limits              decodeLimits
}

// EncodeJBIG2Image encodes 'img' into jbig2 encoded bytes stream, using default encoder settings.
func (jbig2 *JBIG2Encoder) EncodeJBIG2Image(img *JBIG2Image) ([]byte, error) {
	const procEncodeJBIG2Image = "core.EncodeJBIG2Image"
	if err := jbig2.AddPageImage(img, &jbig2.DefaultPageSettings); err != nil {
		return nil, jbig2errors.Wrap(err, procEncodeJBIG2Image, "")
	}
	return jbig2.Encode()
}

// UpdateParams updates the parameter values of the encoder.
// Implements StreamEncoder interface.
func (jbig2 *JBIG2Encoder) UpdateParams(params *PdfObjectDictionary) {
	bitsPerComponent, err := GetNumberAsInt64(params.Get("BitsPerComponent"))
	if err == nil {
		jbig2.BitsPerComponent = int(bitsPerComponent)
	}
	width, err := GetNumberAsInt64(params.Get("Width"))
	if err == nil {
		jbig2.Width = int(width)
	}
	height, err := GetNumberAsInt64(params.Get("Height"))
	if err == nil {
		jbig2.Height = int(height)
	}
	colorComponents, err := GetNumberAsInt64(params.Get("ColorComponents"))
	if err == nil {
		jbig2.ColorComponents = int(colorComponents)
	}
}

// EncodeBytes encodes slice of bytes into JBIG2 encoding format.
// The input 'data' must be an image. In order to Decode it a user is responsible to
// load the codec ('png', 'jpg').
// Returns jbig2 single page encoded document byte slice. The encoder uses DefaultPageSettings
// to encode given image.
func (jbig2 *JBIG2Encoder) EncodeBytes(data []byte) ([]byte, error) {
	const procJBIG2EncoderEncodeBytes = "JBIG2Encoder.EncodeBytes"
	if jbig2.ColorComponents != 1 || jbig2.BitsPerComponent != 1 {
		return nil, jbig2errors.Errorf(procJBIG2EncoderEncodeBytes, "provided invalid input image. JBIG2 Encoder requires binary images data")
	}
	var (
		bm  *bitmap.Bitmap
		err error
	)
	ok := (jbig2.Width * jbig2.Height) == len(data)
	if ok {
		bm, err = bitmap.NewWithUnpaddedData(jbig2.Width, jbig2.Height, data)
	} else {
		bm, err = bitmap.NewWithData(jbig2.Width, jbig2.Height, data)
	}
	if err != nil {
		return nil, err
	}
	defaultPageSettings := jbig2.DefaultPageSettings
	if err = defaultPageSettings.Validate(); err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2EncoderEncodeBytes, "")
	}
	if jbig2.doc == nil {
		jbig2.doc = document.InitEncodeDocument(defaultPageSettings.FileMode)
	}
	switch defaultPageSettings.Compression {
	case JB2Generic:
		if err = jbig2.doc.AddGenericPage(bm, defaultPageSettings.DuplicatedLinesRemoval); err != nil {
			return nil, jbig2errors.Wrap(err, procJBIG2EncoderEncodeBytes, "")
		}
	case JB2SymbolCorrelation:
		return nil, jbig2errors.Error(procJBIG2EncoderEncodeBytes, "symbol correlation encoding not implemented yet")
	case JB2SymbolRankHaus:
		return nil, jbig2errors.Error(procJBIG2EncoderEncodeBytes, "symbol rank haus encoding not implemented yet")
	default:
		return nil, jbig2errors.Error(procJBIG2EncoderEncodeBytes, "provided invalid compression")
	}
	return jbig2.Encode()
}
func (jbig2 *JBIG2Encoder) setDecodeLimits(limits decodeLimits) { jbig2.limits = limits }

// MakeDecodeParams makes a new instance of an encoding dictionary based on the current encoder settings.
func (jbig2 *JBIG2Encoder) MakeDecodeParams() PdfObject { return MakeDict() }

// DecodeStream decodes a JBIG2 encoded stream and returns the result as a slice of bytes.
func (jbig2 *JBIG2Encoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return jbig2.DecodeBytes(streamObj.Stream)
}

// DecodeImages decodes the page images from the jbig2 'encoded' data input.
// The jbig2 document may contain multiple pages, thus the function can return multiple
// images. The images order corresponds to the page number.
func (jbig2 *JBIG2Encoder) DecodeImages(encoded []byte) ([]image.Image, error) {
	const procJBIG2EncoderDecodeImages = "JBIG2Encoder.DecodeImages"
	dec, err := decoder.Decode(encoded, decoder.Parameters{}, jbig2.Globals.ToDocumentGlobals())
	if err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2EncoderDecodeImages, "")
	}
	numPages, err := dec.PageNumber()
	if err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2EncoderDecodeImages, "")
	}
	imgs := []image.Image{}
	var img image.Image
	for i := 1; i <= numPages; i++ {
		img, err = dec.DecodePageImage(i)
		if err != nil {
			return nil, jbig2errors.Wrapf(err, procJBIG2EncoderDecodeImages, "page: '%d'", i)
		}
		imgs = append(imgs, img)
	}
	return imgs, nil
}

// Encode encodes previously prepare jbig2 document and stores it as the byte slice.
func (jbig2 *JBIG2Encoder) Encode() (data []byte, err error) {
	const procJBIG2DocumentEncode = "JBIG2Document.Encode"
	if jbig2.doc == nil {
		return nil, jbig2errors.Errorf(procJBIG2DocumentEncode, "document input data not defined")
	}
	jbig2.doc.FullHeaders = jbig2.DefaultPageSettings.FileMode
	data, err = jbig2.doc.Encode()
	if err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2DocumentEncode, "")
	}
	return data, nil
}

func (jbig2 *JBIG2Encoder) encodeImage(img image.Image) ([]byte, error) {
	const procEncodeImage = "encodeImage"
	jbig2Img, err := GoImageToJBIG2(img, JB2ImageAutoThreshold)
	if err != nil {
		return nil, jbig2errors.Wrap(err, procEncodeImage, "convert input image to jbig2 img")
	}
	if err = jbig2.AddPageImage(jbig2Img, &jbig2.DefaultPageSettings); err != nil {
		return nil, jbig2errors.Wrap(err, procEncodeImage, "")
	}
	return jbig2.Encode()
}
