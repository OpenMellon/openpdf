package core

import (
	"io"

	"github.com/OpenMellon/openpdf/common"
)

// JPXEncoder implements JPX encoder/decoder (dummy, for now)
// FIXME: implement
type JPXEncoder struct{}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (jpxEncoder *JPXEncoder) MakeDecodeParams() PdfObject { return nil }

// GetFilterName returns the name of the encoding filter.
func (jpxEncoder *JPXEncoder) GetFilterName() string { return StreamEncodingFilterNameJPX }

// DecodeBytes decodes a slice of JPX encoded bytes and returns the result.
func (jpxEncoder *JPXEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	common.Log.Debug("Error: Attempting to use unsupported encoding %s", jpxEncoder.GetFilterName())
	return encoded, ErrNoJPXDecode
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (jpxEncoder *JPXEncoder) MakeStreamDict() *PdfObjectDictionary { return MakeDict() }

// UpdateParams updates the parameter values of the encoder.
func (jpxEncoder *JPXEncoder) UpdateParams(params *PdfObjectDictionary) {}

// DecodeStream decodes a JPX encoded stream and returns the result as a
// slice of bytes.
func (jpxEncoder *JPXEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	common.Log.Debug("Error: Attempting to use unsupported encoding %s", jpxEncoder.GetFilterName())
	return streamObj.Stream, ErrNoJPXDecode
}

// EncodeBytes JPX encodes the passed in slice of bytes.
func (jpxEncoder *JPXEncoder) EncodeBytes(data []byte) ([]byte, error) {
	common.Log.Debug("Error: Attempting to use unsupported encoding %s", jpxEncoder.GetFilterName())
	return data, ErrNoJPXDecode
}

// NewJPXEncoder returns a new instance of JPXEncoder.
func NewJPXEncoder() *JPXEncoder { return &JPXEncoder{} }

// RawEncoder implements Raw encoder/decoder (no encoding, pass through)
type RawEncoder struct{}

// DecodeBytes returns the passed in slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) DecodeBytes(encoded []byte) ([]byte, error) { return encoded, nil }

// UpdateParams updates the parameter values of the encoder.
func (rawEncoder *RawEncoder) UpdateParams(params *PdfObjectDictionary) {}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (rawEncoder *RawEncoder) MakeStreamDict() *PdfObjectDictionary { return MakeDict() }

// EncodeBytes returns the passed in slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) EncodeBytes(data []byte) ([]byte, error) { return data, nil }

// DecodeStream returns the passed in stream as a slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return streamObj.Stream, nil
}

// NewRawEncoder returns a new instace of RawEncoder.
func NewRawEncoder() *RawEncoder { return &RawEncoder{} }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (rawEncoder *RawEncoder) MakeDecodeParams() PdfObject { return nil }

// GetFilterName returns the name of the encoding filter.
func (rawEncoder *RawEncoder) GetFilterName() string { return StreamEncodingFilterNameRaw }

// DecodeReader returns r unchanged.
func (rawEncoder *RawEncoder) DecodeReader(r io.Reader) io.Reader { return r }

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

// StreamingDecoder is implemented by stream encoders that can decode their
// input incrementally, without materializing the full decoded output in
// memory. Decoders that do not implement this interface (image codecs such
// as DCT, JPX, CCITTFax, JBIG2) must be consumed via DecodeBytes.
//
// The reader returned by DecodeReader produces the same bytes that
// DecodeBytes would produce for the same valid input. Decoding errors are
// surfaced lazily via Read.
type StreamingDecoder interface {
	DecodeReader(r io.Reader) io.Reader
}
