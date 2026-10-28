package core

import (
	"fmt"
	"io"
	"slices"
	"strings"

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

// UpdateParams updates the parameter values of the encoder.
func (multi *MultiEncoder) UpdateParams(params *PdfObjectDictionary) {
	for _, encoder := range multi.encoders {
		encoder.UpdateParams(params)
	}
}

// NewMultiEncoder returns a new instance of MultiEncoder.
func NewMultiEncoder() *MultiEncoder {
	multi := MultiEncoder{}
	multi.encoders = []StreamEncoder{}
	return &multi
}

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

// DecodeStream decodes a multi-encoded stream by passing it through the
// DecodeStream method of the underlying encoders.
func (multi *MultiEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return multi.DecodeBytes(streamObj.Stream)
}

type multiFallbackReader struct {
	src       io.Reader
	fallbacks []StreamEncoder
	started   bool
	err       error
	data      []byte
	pos       int
}

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
