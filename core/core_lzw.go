package core

import (
	"bytes"
	"compress/lzw"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
	tifflzw "golang.org/x/image/tiff/lzw"
)

// LZWEncoder provides LZW encoding/decoding functionality.
type LZWEncoder struct {
	Predictor        int
	BitsPerComponent int

	// For predictors
	Columns int
	Colors  int

	// LZW algorithm setting.
	EarlyChange int
	limits      decodeLimits
}

// DecodeStream decodes a LZW encoded stream and returns the result as a
// slice of bytes.
func (enc *LZWEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("LZW Decoding")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Predictor: %d", enc.Predictor)
	}
	bytes, err := enc.DecodeBytes(streamObj.Stream)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace(" IN: (%d) % x", len(streamObj.Stream), streamObj.Stream)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("OUT: (%d) % x", len(bytes), bytes)
	}
	if enc.Predictor > 1 {
		predictor, err2 := decodePredictor(bytes, enc.Predictor, enc.Colors, enc.BitsPerComponent, enc.Columns)
		if err2 != nil {
			ReleaseBytes(bytes)
			return nil, err2
		}
		return predictor, nil
	}
	return bytes, nil
}

// GetFilterName returns the name of the encoding filter.
func (enc *LZWEncoder) GetFilterName() string { return StreamEncodingFilterNameLZW }

// DecodeBytes decodes a slice of LZW encoded bytes and returns the result.
func (enc *LZWEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	reader := bytes.NewReader(encoded)
	var rc io.ReadCloser
	if enc.EarlyChange == 1 {
		rc = tifflzw.NewReader(reader, tifflzw.MSB, 8)
	} else {
		rc = lzw.NewReader(reader, lzw.MSB, 8)
	}
	defer rc.Close()
	all, err := enc.limits.readAll(StreamEncodingFilterNameLZW, rc)
	if err != nil {
		if err != io.ErrUnexpectedEOF || len(all) == 0 {
			ReleaseBytes(all)
			return nil, err
		}
		common.Log.Debug("WARN: LZW decoding error: %v. Output may be incorrect.", err)
	}
	enc.limits.charge(int64(len(all)))
	return all, nil
}

// NewLZWEncoder makes a new LZW encoder with default parameters.
func NewLZWEncoder() *LZWEncoder {
	lzw := &LZWEncoder{}
	lzw.Predictor = 1
	lzw.BitsPerComponent = 8
	lzw.Colors = 1
	lzw.Columns = 1
	lzw.EarlyChange = 1
	return lzw
}

// EncodeBytes implements support for LZW encoding.  Currently not supporting predictors (raw compressed data only).
// Only supports the Early change = 1 algorithm (compress/lzw) as the other implementation
// does not have a write method.
// TODO: Consider refactoring compress/lzw to allow both.
func (enc *LZWEncoder) EncodeBytes(data []byte) ([]byte, error) {
	if enc.Predictor != 1 {
		return nil, fmt.Errorf("LZW Predictor = 1 only supported yet")
	}
	if enc.EarlyChange == 1 {
		return nil, fmt.Errorf("LZW Early Change = 0 only supported yet")
	}
	var buf bytes.Buffer
	writer := lzw.NewWriter(&buf, lzw.MSB, 8)
	writer.Write(data)
	writer.Close()
	return buf.Bytes(), nil
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (enc *LZWEncoder) MakeDecodeParams() PdfObject {
	if enc.Predictor > 1 {
		dict := MakeDict()
		dict.Set("Predictor", MakeInteger(int64(enc.Predictor)))
		if enc.BitsPerComponent != 8 {
			dict.Set("BitsPerComponent", MakeInteger(int64(enc.BitsPerComponent)))
		}
		if enc.Columns != 1 {
			dict.Set("Columns", MakeInteger(int64(enc.Columns)))
		}
		if enc.Colors != 1 {
			dict.Set("Colors", MakeInteger(int64(enc.Colors)))
		}
		return dict
	}
	return nil
}

// DecodeReader returns a reader that LZW-decodes from r. EarlyChange selects
// which LZW variant (one-code-early vs postponed code-length increase),
// matching DecodeBytes. The predictor is NOT applied — see FlateEncoder's
// DecodeReader for rationale.
func (enc *LZWEncoder) DecodeReader(r io.Reader) io.Reader {
	if enc.EarlyChange == 1 {
		return enc.limits.reader(StreamEncodingFilterNameLZW, tifflzw.NewReader(r, tifflzw.MSB, 8))
	}
	return enc.limits.reader(StreamEncodingFilterNameLZW, lzw.NewReader(r, lzw.MSB, 8))
}

func newLZWEncoderFromStream(streamObj *PdfObjectStream, decodeParams *PdfObjectDictionary) (*LZWEncoder, error) {
	lzwEncoder := NewLZWEncoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return lzwEncoder, nil
	}
	if decodeParams == nil {
		toDirectObject := TraceToDirectObject(pdfObjectDictionary.Get("DecodeParms"))
		if toDirectObject != nil {
			if dict, ok := toDirectObject.(*PdfObjectDictionary); ok {
				decodeParams = dict
			} else if arr, isArr := toDirectObject.(*PdfObjectArray); isArr {
				if arr.Len() == 1 {
					if first, hasDict := GetDict(arr.Get(0)); hasDict {
						decodeParams = first
					}
				}
			}
			if decodeParams == nil {
				common.Log.Error("DecodeParms not a dictionary %#v", toDirectObject)
				return nil, fmt.Errorf("invalid DecodeParms")
			}
		}
	}
	earlyChange := pdfObjectDictionary.Get("EarlyChange")
	if earlyChange != nil {
		num, ok := earlyChange.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: EarlyChange specified but not numeric (%T)", earlyChange)
			return nil, fmt.Errorf("invalid EarlyChange")
		}
		if *num != 0 && *num != 1 {
			return nil, fmt.Errorf("invalid EarlyChange value (not 0 or 1)")
		}
		lzwEncoder.EarlyChange = int(*num)
	} else {
		lzwEncoder.EarlyChange = 1
	}
	if decodeParams == nil {
		return lzwEncoder, nil
	}
	if intVal, ok := GetIntVal(decodeParams.Get("EarlyChange")); ok {
		if intVal == 0 || intVal == 1 {
			lzwEncoder.EarlyChange = intVal
		} else {
			common.Log.Debug("WARN: invalid EarlyChange value: %d", intVal)
		}
	}
	earlyChange = decodeParams.Get("Predictor")
	if earlyChange != nil {
		num, ok := earlyChange.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: Predictor specified but not numeric (%T)", earlyChange)
			return nil, fmt.Errorf("invalid Predictor")
		}
		lzwEncoder.Predictor = int(*num)
	}
	earlyChange = decodeParams.Get("BitsPerComponent")
	if earlyChange != nil {
		num, ok := earlyChange.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("ERROR: Invalid BitsPerComponent")
			return nil, fmt.Errorf("invalid BitsPerComponent")
		}
		lzwEncoder.BitsPerComponent = int(*num)
	}
	if lzwEncoder.Predictor > 1 {
		lzwEncoder.Columns = 1
		earlyChange = decodeParams.Get("Columns")
		if earlyChange != nil {
			num, ok := earlyChange.(*PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor column invalid")
			}
			lzwEncoder.Columns = int(*num)
		}
		lzwEncoder.Colors = 1
		earlyChange = decodeParams.Get("Colors")
		if earlyChange != nil {
			num, ok := earlyChange.(*PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor colors not an integer")
			}
			lzwEncoder.Colors = int(*num)
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", decodeParams.String())
	}
	return lzwEncoder, nil
}

func (enc *LZWEncoder) setDecodeLimits(limits decodeLimits) { enc.limits = limits }

// UpdateParams updates the parameter values of the encoder.
func (enc *LZWEncoder) UpdateParams(params *PdfObjectDictionary) {
	predictor, err := GetNumberAsInt64(params.Get("Predictor"))
	if err == nil {
		enc.Predictor = int(predictor)
	}
	bitsPerComponent, err := GetNumberAsInt64(params.Get("BitsPerComponent"))
	if err == nil {
		enc.BitsPerComponent = int(bitsPerComponent)
	}
	width, err := GetNumberAsInt64(params.Get("Width"))
	if err == nil {
		enc.Columns = int(width)
	}
	colorComponents, err := GetNumberAsInt64(params.Get("ColorComponents"))
	if err == nil {
		enc.Colors = int(colorComponents)
	}
	earlyChange, err := GetNumberAsInt64(params.Get("EarlyChange"))
	if err == nil {
		enc.EarlyChange = int(earlyChange)
	}
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
// Has the Filter set and the DecodeParms.
func (enc *LZWEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(enc.GetFilterName()))
	decodeParams := enc.MakeDecodeParams()
	if decodeParams != nil {
		dict.Set("DecodeParms", decodeParams)
	}
	dict.Set("EarlyChange", MakeInteger(int64(enc.EarlyChange)))
	return dict
}
