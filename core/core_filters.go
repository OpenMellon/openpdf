package core

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/OpenMellon/openpdf/common"
)

// MultiEncoder supports serial encoding.
type MultiEncoder struct{ encoders []StreamEncoder }

// GetFilterName returns the names of the underlying encoding filters,
// separated by spaces.
// Note: This is just a string, should not be used in /Filter dictionary entry. Use GetFilterArray for that.
// TODO(v4): Refactor to GetFilter() which can be used for /Filter (either Name or Array), this can be
// renamed to String() as a pretty string to use in debugging etc.
func (multi *MultiEncoder) GetFilterName() string {
	var name strings.Builder
	for i, encoder := range multi.encoders {
		name.WriteString(encoder.GetFilterName())
		if i < len(multi.encoders)-1 {
			name.WriteString(" ")
		}
	}
	return name.String()
}

// DecodeBytes decodes a multi-encoded slice of bytes by passing it through the
// DecodeBytes method of the underlying encoders.
func (multi *MultiEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	data := encoded
	var err error
	for _, encoder := range multi.encoders {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Multi Encoder Decode: Applying Filter: %v %T", encoder, encoder)
		}
		data, err = encoder.DecodeBytes(data)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

// DecodeBytes returns the passed in slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) DecodeBytes(encoded []byte) ([]byte, error) { return encoded, nil }

// UpdateParams updates the parameter values of the encoder.
func (multi *MultiEncoder) UpdateParams(params *PdfObjectDictionary) {
	for _, encoder := range multi.encoders {
		encoder.UpdateParams(params)
	}
}

// UpdateParams updates the parameter values of the encoder.
func (rawEncoder *RawEncoder) UpdateParams(params *PdfObjectDictionary) {}

// NewMultiEncoder returns a new instance of MultiEncoder.
func NewMultiEncoder() *MultiEncoder {
	multi := MultiEncoder{}
	multi.encoders = []StreamEncoder{}
	return &multi
}

type multiFallbackReader struct {
	src       io.Reader
	fallbacks []StreamEncoder
	started   bool
	err       error
	data      []byte
	pos       int
}

// EncodeStream encodes the stream data using the encoded specified by the stream's dictionary.
func EncodeStream(streamObj *PdfObjectStream) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encode stream")
	}
	encoderFromStream, err := NewEncoderFromStream(streamObj)
	if err != nil {
		common.Log.Debug("Stream decoding failed: %v", err)
		return err
	}
	if lzw, ok := encoderFromStream.(*LZWEncoder); ok {
		lzw.EarlyChange = 0
		streamObj.Set("EarlyChange", MakeInteger(0))
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encoder: %+v\n", encoderFromStream)
	}
	bytes, err := encoderFromStream.EncodeBytes(streamObj.Stream)
	if err != nil {
		common.Log.Debug("Stream encoding failed: %v", err)
		return err
	}
	streamObj.Stream = bytes
	streamObj.Set("Length", MakeInteger(int64(len(bytes))))
	return nil
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (rawEncoder *RawEncoder) MakeStreamDict() *PdfObjectDictionary { return MakeDict() }

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (multi *MultiEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", multi.GetFilterArray())
	for _, encoder := range multi.encoders {
		streamDict := encoder.MakeStreamDict()
		for _, key := range streamDict.Keys() {
			obj := streamDict.Get(key)
			if key != "Filter" && key != "DecodeParms" {
				dict.Set(key, obj)
			}
		}
	}
	decodeParams := multi.MakeDecodeParams()
	if decodeParams != nil {
		dict.Set("DecodeParms", decodeParams)
	}
	return dict
}

func newMultiEncoderFromStream(streamObj *PdfObjectStream) (*MultiEncoder, error) {
	multiEncoder := NewMultiEncoder()
	limitsFromStream := decodeLimitsFromStream(streamObj)
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return multiEncoder, nil
	}
	var dict *PdfObjectDictionary
	var objs []PdfObject
	decodeParms := pdfObjectDictionary.Get("DecodeParms")
	if decodeParms != nil {
		single, ok := decodeParms.(*PdfObjectDictionary)
		if ok {
			dict = single
		}
		arr, isArr := decodeParms.(*PdfObjectArray)
		if isArr {
			for _, elem := range arr.Elements() {
				elem = TraceToDirectObject(elem)
				if elemDict, isDict := elem.(*PdfObjectDictionary); isDict {
					objs = append(objs, elemDict)
				} else {
					objs = append(objs, MakeDict())
				}
			}
		}
	}
	decodeParms = pdfObjectDictionary.Get("Filter")
	if decodeParms == nil {
		return nil, fmt.Errorf("filter missing")
	}
	arr, ok := decodeParms.(*PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("multi filter can only be made from array")
	}
	for i, elem := range arr.Elements() {
		name, isName := elem.(*PdfObjectName)
		if !isName {
			return nil, fmt.Errorf("multi filter array element not a name")
		}
		var obj PdfObject
		if dict != nil {
			obj = dict
		} else {
			if len(objs) > 0 {
				if i >= len(objs) {
					return nil, fmt.Errorf("missing elements in decode params array")
				}
				obj = objs[i]
			}
		}
		var current *PdfObjectDictionary
		if d, isDict := obj.(*PdfObjectDictionary); isDict {
			current = d
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Next name: %s, dp: %v, dParams: %v", *name, obj, current)
		}
		switch *name {
		case StreamEncodingFilterNameFlate:
			flateEncoderFromStream, err := newFlateEncoderFromStream(streamObj, current)
			if err != nil {
				return nil, err
			}
			applyDecodeLimits(flateEncoderFromStream, limitsFromStream)
			multiEncoder.AddEncoder(flateEncoderFromStream)
		case StreamEncodingFilterNameLZW:
			lzwEncoderFromStream, err := newLZWEncoderFromStream(streamObj, current)
			if err != nil {
				return nil, err
			}
			applyDecodeLimits(lzwEncoderFromStream, limitsFromStream)
			multiEncoder.AddEncoder(lzwEncoderFromStream)
		case StreamEncodingFilterNameASCIIHex:
			asciiHexEncoder := NewASCIIHexEncoder()
			applyDecodeLimits(asciiHexEncoder, limitsFromStream)
			multiEncoder.AddEncoder(asciiHexEncoder)
		case StreamEncodingFilterNameASCII85:
			asciI85Encoder := NewASCII85Encoder()
			applyDecodeLimits(asciI85Encoder, limitsFromStream)
			multiEncoder.AddEncoder(asciI85Encoder)
		case StreamEncodingFilterNameDCT:
			dctEncoderFromStream, err := newDCTEncoderFromStream(streamObj, multiEncoder)
			if err != nil {
				return nil, err
			}
			applyDecodeLimits(dctEncoderFromStream, limitsFromStream)
			multiEncoder.AddEncoder(dctEncoderFromStream)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Added DCT encoder...")
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Multi encoder: %#v", multiEncoder)
			}
		case StreamEncodingFilterNameCCITTFax:
			ccittFaxEncoderFromStream, err := newCCITTFaxEncoderFromStream(streamObj, current)
			if err != nil {
				return nil, err
			}
			applyDecodeLimits(ccittFaxEncoderFromStream, limitsFromStream)
			multiEncoder.AddEncoder(ccittFaxEncoderFromStream)
		default:
			common.Log.Error("Unsupported filter %s", *name)
			return nil, fmt.Errorf("invalid filter in multi filter array")
		}
	}
	return multiEncoder, nil
}

// EncodeBytes returns the passed in slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) EncodeBytes(data []byte) ([]byte, error) { return data, nil }

func (r *multiFallbackReader) Read(p []byte) (int, error) {
	if !r.started {
		r.started = true
		all, err := io.ReadAll(r.src)
		if err != nil {
			r.err = err
			return 0, err
		}
		for _, fallback := range r.fallbacks {
			all, err = fallback.DecodeBytes(all)
			if err != nil {
				r.err = err
				return 0, err
			}
		}
		r.data = all
	}
	if r.err != nil {
		return 0, r.err
	}
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// GetFilterArray returns the names of the underlying encoding filters in an array that
// can be used as /Filter entry.
func (multi *MultiEncoder) GetFilterArray() *PdfObjectArray {
	objs := make([]PdfObject, len(multi.encoders))
	for i, encoder := range multi.encoders {
		objs[i] = MakeName(encoder.GetFilterName())
	}
	return MakeArray(objs...)
}

// DecodeReader returns a reader that decodes the chain of sub-encoders in
// forward (decode) order: r → encoders[0].DecodeReader → ... → output.
//
// The first sub-encoder that does NOT implement StreamingDecoder (typically
// DCT, JPX, CCITTFax, JBIG2) terminates the streaming portion: the upstream
// reader is drained into memory and the remaining encoders run via their
// DecodeBytes. The end-to-end output is therefore identical to DecodeBytes,
// but only the portion of the chain up to (and not including) the first
// non-streaming encoder runs without full materialization.
func (multi *MultiEncoder) DecodeReader(r io.Reader) io.Reader {
	cur := r
	for i, encoder := range multi.encoders {
		streamingDecoder, ok := encoder.(StreamingDecoder)
		if !ok {
			return &multiFallbackReader{src: cur, fallbacks: multi.encoders[i:]}
		}
		cur = streamingDecoder.DecodeReader(cur)
	}
	return cur
}

// RegisterCustomStreamEncoder register a custom encoder handler for certain filter.
func RegisterCustomStreamEncoder(filterName string, customStreamEncoder StreamEncoder) {
	customStreamEncoders.Store(filterName, customStreamEncoder)
}

// AddEncoder adds the passed in encoder to the underlying encoder slice.
func (multi *MultiEncoder) AddEncoder(encoder StreamEncoder) {
	multi.encoders = append(multi.encoders, encoder)
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (multi *MultiEncoder) MakeDecodeParams() PdfObject {
	if len(multi.encoders) == 0 {
		return nil
	}
	if len(multi.encoders) == 1 {
		return multi.encoders[0].MakeDecodeParams()
	}
	array := MakeArray()
	ok := true
	for _, encoder := range multi.encoders {
		decodeParams := encoder.MakeDecodeParams()
		if decodeParams == nil {
			array.Append(MakeNull())
		} else {
			ok = false
			array.Append(decodeParams)
		}
	}
	if ok {
		return nil
	}
	return array
}

// DecodeStream decodes the stream data and returns the decoded data.
// An error is returned upon failure.
func DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Decode stream")
	}
	encoderFromStream, err := NewEncoderFromStream(streamObj)
	if err != nil {
		common.Log.Debug("ERROR: Stream decoding failed: %v", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encoder: %#v\n", encoderFromStream)
	}
	stream, err := encoderFromStream.DecodeStream(streamObj)
	if err != nil {
		common.Log.Debug("ERROR: Stream decoding failed: %v", err)
		return nil, err
	}
	return stream, nil
}

// RawEncoder implements Raw encoder/decoder (no encoding, pass through)
type RawEncoder struct{}

// EncodeBytes encodes the passed in slice of bytes by passing it through the
// EncodeBytes method of the underlying encoders.
func (multi *MultiEncoder) EncodeBytes(data []byte) ([]byte, error) {
	encoded := data
	var err error
	for _, encoder := range slices.Backward(multi.encoders) {

		encoded, err = encoder.EncodeBytes(encoded)
		if err != nil {
			return nil, err
		}
	}
	return encoded, nil
}

// DecodeStream returns the passed in stream as a slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return streamObj.Stream, nil
}

// StreamEncoder represents the interface for all PDF stream encoders.
type StreamEncoder interface {
	GetFilterName() string
	MakeDecodeParams() PdfObject
	MakeStreamDict() *PdfObjectDictionary
	UpdateParams(dict *PdfObjectDictionary)
	EncodeBytes(data []byte) ([]byte, error)
	DecodeBytes(data []byte) ([]byte, error)
	DecodeStream(stream *PdfObjectStream) ([]byte, error)
}

// NewRawEncoder returns a new instace of RawEncoder.
func NewRawEncoder() *RawEncoder { return &RawEncoder{} }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (rawEncoder *RawEncoder) MakeDecodeParams() PdfObject { return nil }

const (
	StreamEncodingFilterNameFlate     = "FlateDecode"
	StreamEncodingFilterNameLZW       = "LZWDecode"
	StreamEncodingFilterNameDCT       = "DCTDecode"
	StreamEncodingFilterNameRunLength = "RunLengthDecode"
	StreamEncodingFilterNameASCIIHex  = "ASCIIHexDecode"
	StreamEncodingFilterNameASCII85   = "ASCII85Decode"
	StreamEncodingFilterNameCCITTFax  = "CCITTFaxDecode"
	StreamEncodingFilterNameJBIG2     = "JBIG2Decode"
	StreamEncodingFilterNameJPX       = "JPXDecode"
	StreamEncodingFilterNameRaw       = "Raw"
)

// GetFilterName returns the name of the encoding filter.
func (rawEncoder *RawEncoder) GetFilterName() string { return StreamEncodingFilterNameRaw }

var customStreamEncoders sync.Map

// DecodeReader returns r unchanged.
func (rawEncoder *RawEncoder) DecodeReader(r io.Reader) io.Reader { return r }

// DecodeStream decodes a multi-encoded stream by passing it through the
// DecodeStream method of the underlying encoders.
func (multi *MultiEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return multi.DecodeBytes(streamObj.Stream)
}

// NewEncoderFromStream creates a StreamEncoder based on the stream's dictionary.
func NewEncoderFromStream(streamObj *PdfObjectStream) (StreamEncoder, error) {
	toDirectObject := TraceToDirectObject(streamObj.Get("Filter"))
	if toDirectObject == nil {
		return NewRawEncoder(), nil
	}
	if _, ok := toDirectObject.(*PdfObjectNull); ok {
		return NewRawEncoder(), nil
	}
	name, ok := toDirectObject.(*PdfObjectName)
	if !ok {
		arr, isArr := toDirectObject.(*PdfObjectArray)
		if !isArr {
			return nil, fmt.Errorf("filter not a Name or Array object")
		}
		if arr.Len() == 0 {
			return NewRawEncoder(), nil
		}
		if arr.Len() != 1 {
			multiEncoderFromStream, err := newMultiEncoderFromStream(streamObj)
			if err != nil {
				common.Log.Error("Failed creating multi encoder: %v", err)
				return nil, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Multi enc: %s\n", multiEncoderFromStream)
			}
			return multiEncoderFromStream, nil
		}
		toDirectObject = arr.Get(0)
		name, isArr = toDirectObject.(*PdfObjectName)
		if !isArr {
			return nil, fmt.Errorf("filter array member not a Name object")
		}
	}
	if encoder, ok2 := customStreamEncoders.Load(name.String()); ok2 {
		return encoder.(StreamEncoder), nil
	}
	var encoder StreamEncoder
	var err error
	switch *name {
	case StreamEncodingFilterNameFlate:
		encoder, err = newFlateEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameLZW:
		encoder, err = newLZWEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameDCT:
		encoder, err = newDCTEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameRunLength:
		encoder, err = newRunLengthEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameASCIIHex:
		encoder = NewASCIIHexEncoder()
	case StreamEncodingFilterNameASCII85, "A85":
		encoder = NewASCII85Encoder()
	case StreamEncodingFilterNameCCITTFax:
		encoder, err = newCCITTFaxEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameJBIG2:
		encoder, err = newJBIG2EncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameJPX:
		encoder = NewJPXEncoder()
	default:
		common.Log.Debug("ERROR: Unsupported encoding method!")
		return nil, fmt.Errorf("unsupported encoding method (%s)", *name)
	}
	if err != nil {
		return nil, err
	}
	applyDecodeLimits(encoder, decodeLimitsFromStream(streamObj))
	return encoder, nil
}
