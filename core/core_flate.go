package core

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/imageutil"
)

// FlateEncoder represents Flate encoding.
type FlateEncoder struct {
	Predictor        int
	BitsPerComponent int

	// For predictors
	Columns int
	Rows    int
	Colors  int
	image   *imageutil.ImageBase
	limits  decodeLimits
}

// DecodeBytes decodes a slice of Flate encoded bytes and returns the result.
func (flate *FlateEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("FlateDecode bytes")
	}
	if len(encoded) == 0 {
		common.Log.Debug("ERROR: empty Flate encoded buffer. Returning empty byte slice.")
		return []byte{}, nil
	}
	reader := bytes.NewReader(encoded)
	zlibReader, err := acquireZlibReader(reader)
	if err != nil {
		common.Log.Debug("Decoding error %v\n", err)
		common.Log.Debug("Stream (%d) % x", len(encoded), encoded)
		return nil, err
	}
	all, err := flate.limits.readAll(StreamEncodingFilterNameFlate, zlibReader)
	zlibReaderPool.Put(zlibReader)
	if errors.Is(err, ErrDecodedSizeLimitExceeded) {
		return nil, err
	}
	flate.limits.charge(int64(len(all)))
	return all, nil
}

// SetPredictor sets the predictor function.  Specify the number of columns per row.
// The columns indicates the number of samples per row.
// Used for grouping data together for compression.
func (flate *FlateEncoder) SetPredictor(columns int) { flate.Predictor = 11; flate.Columns = columns }

// DecodeStream decodes a FlateEncoded stream object and give back decoded bytes.
func (flate *FlateEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("FlateDecode stream")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Predictor: %d", flate.Predictor)
	}
	bytes, err := flate.DecodeBytes(streamObj.Stream)
	if err != nil {
		return nil, err
	}
	bytes, err = flate.postDecodePredict(bytes)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

// GetFilterName returns the name of the encoding filter.
func (flate *FlateEncoder) GetFilterName() string { return StreamEncodingFilterNameFlate }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (flate *FlateEncoder) MakeDecodeParams() PdfObject {
	if flate.Predictor > 1 {
		dict := MakeDict()
		dict.Set("Predictor", MakeInteger(int64(flate.Predictor)))
		if flate.BitsPerComponent != 8 {
			dict.Set("BitsPerComponent", MakeInteger(int64(flate.BitsPerComponent)))
		}
		if flate.Columns != 1 {
			dict.Set("Columns", MakeInteger(int64(flate.Columns)))
		}
		if flate.Colors != 1 {
			dict.Set("Colors", MakeInteger(int64(flate.Colors)))
		}
		return dict
	}
	return nil
}

// DecodeReader returns a reader that Flate-decodes (zlib) from r. Reading
// past the decoded size limit fails with ErrDecodedSizeLimitExceeded. The
// predictor configured on enc is NOT applied here — DecodeReader parities
// DecodeBytes, which is also raw zlib decompression. Callers that need
// predictor application must continue to use DecodeStream.
//
// A fresh zlib reader is allocated per call rather than reusing
// zlibReaderPool: the returned reader's lifetime is unbounded and the pool
// requires explicit return-on-close that an io.Reader cannot express.
//
// An empty input stream yields an empty payload (zero bytes, io.EOF),
// matching DecodeBytes.
func (flate *FlateEncoder) DecodeReader(r io.Reader) io.Reader {
	if r == nil {
		return errorReader{err: errors.New("FlateDecode: nil input")}
	}
	reader := bufio.NewReader(r)
	if _, err := reader.Peek(1); err != nil {
		if err == io.EOF {
			return emptyReader{}
		}
		return errorReader{err: err}
	}
	rc, err := zlib.NewReader(reader)
	if err != nil {
		return errorReader{err: err}
	}
	return flate.limits.reader(StreamEncodingFilterNameFlate, rc)
}

func (flate *FlateEncoder) setDecodeLimits(limits decodeLimits) { flate.limits = limits }

// UpdateParams updates the parameter values of the encoder.
func (flate *FlateEncoder) UpdateParams(params *PdfObjectDictionary) {
	predictor, err := GetNumberAsInt64(params.Get("Predictor"))
	if err == nil {
		flate.Predictor = int(predictor)
	}
	bitsPerComponent, err := GetNumberAsInt64(params.Get("BitsPerComponent"))
	if err == nil {
		flate.BitsPerComponent = int(bitsPerComponent)
	}
	width, err := GetNumberAsInt64(params.Get("Width"))
	if err == nil {
		flate.Columns = int(width)
	}
	colorComponents, err := GetNumberAsInt64(params.Get("ColorComponents"))
	if err == nil {
		flate.Colors = int(colorComponents)
	}
}

// SetImage sets the image base for given flate encoder.
func (flate *FlateEncoder) SetImage(img *imageutil.ImageBase) { flate.image = img }

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
// Has the Filter set and the DecodeParms.
func (flate *FlateEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(flate.GetFilterName()))
	decodeParams := flate.MakeDecodeParams()
	if decodeParams != nil {
		dict.Set("DecodeParms", decodeParams)
	}
	return dict
}

func (flate *FlateEncoder) postDecodePredict(data []byte) ([]byte, error) {
	if flate.Predictor <= 1 {
		return data, nil
	}
	predictor, err := decodePredictor(data, flate.Predictor, flate.Colors, flate.BitsPerComponent, flate.Columns)
	if err != nil {
		ReleaseBytes(data)
		return nil, err
	}
	return predictor, nil
}

// NewFlateEncoder makes a new flate encoder with default parameters, predictor 1 and bits per component 8.
func NewFlateEncoder() *FlateEncoder {
	flate := &FlateEncoder{}
	flate.Predictor = 1
	flate.BitsPerComponent = 8
	flate.Colors = 1
	flate.Columns = 1
	return flate
}

// EncodeBytes encodes a bytes array and return the encoded value based on the encoder parameters.
func (flate *FlateEncoder) EncodeBytes(data []byte) ([]byte, error) {
	if flate.Predictor != 1 && flate.Predictor != 11 {
		common.Log.Debug("Encoding error: FlateEncoder Predictor = 1, 11 only supported")
		return nil, ErrUnsupportedEncodingParameters
	}
	if flate.Predictor == 11 {
		pngPredictor, err := encodePNGPredictor(data, flate.Colors, flate.BitsPerComponent, flate.Columns)
		if err != nil {
			common.Log.Debug("ERROR: PNG predictor: %v", err)
			return nil, err
		}
		data = pngPredictor
	}
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	writer.Write(data)
	writer.Close()
	return buf.Bytes(), nil
}

func newFlateEncoderFromStream(streamObj *PdfObjectStream, decodeParams *PdfObjectDictionary) (*FlateEncoder, error) {
	flateEncoder := NewFlateEncoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return flateEncoder, nil
	}
	flateEncoder.image = imageBaseFromDict(pdfObjectDictionary)
	if decodeParams == nil {
		toDirectObject := TraceToDirectObject(pdfObjectDictionary.Get("DecodeParms"))
		switch v := toDirectObject.(type) {
		case *PdfObjectArray:
			if v.Len() != 1 {
				common.Log.Debug("Error: DecodeParms array length != 1 (%d)", v.Len())
				return nil, errors.New("range check error")
			}
			if dict, ok := GetDict(v.Get(0)); ok {
				decodeParams = dict
			}
		case *PdfObjectDictionary:
			decodeParams = v
		case *PdfObjectNull, nil:
		default:
			common.Log.Debug("Error: DecodeParms not a dictionary (%T)", toDirectObject)
			return nil, fmt.Errorf("invalid DecodeParms")
		}
	}
	if decodeParams == nil {
		return flateEncoder, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", decodeParams.String())
	}
	predictor := decodeParams.Get("Predictor")
	if predictor == nil {
		common.Log.Debug("Error: Predictor missing from DecodeParms - Continue with default (1)")
	} else {
		num, ok := predictor.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: Predictor specified but not numeric (%T)", predictor)
			return nil, fmt.Errorf("invalid Predictor")
		}
		flateEncoder.Predictor = int(*num)
	}
	predictor = decodeParams.Get("BitsPerComponent")
	if predictor != nil {
		num, ok := predictor.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("ERROR: Invalid BitsPerComponent")
			return nil, fmt.Errorf("invalid BitsPerComponent")
		}
		flateEncoder.BitsPerComponent = int(*num)
	}
	if flateEncoder.Predictor > 1 {
		flateEncoder.Columns = 1
		predictor = decodeParams.Get("Columns")
		if predictor != nil {
			num, ok := predictor.(*PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor column invalid")
			}
			flateEncoder.Columns = int(*num)
		}
		flateEncoder.Colors = 1
		predictor = decodeParams.Get("Colors")
		if predictor != nil {
			num, ok := predictor.(*PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor colors not an integer")
			}
			flateEncoder.Colors = int(*num)
		}
	}
	return flateEncoder, nil
}
