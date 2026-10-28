package core

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
)

// ASCIIHexEncoder implements ASCII hex encoder/decoder.
type ASCIIHexEncoder struct{ limits decodeLimits }

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (asciiHex *ASCIIHexEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(asciiHex.GetFilterName()))
	return dict
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (asciiHex *ASCIIHexEncoder) MakeDecodeParams() PdfObject { return nil }

func (asciiHex *ASCIIHexEncoder) setDecodeLimits(limits decodeLimits) { asciiHex.limits = limits }

// GetFilterName returns the name of the encoding filter.
func (asciiHex *ASCIIHexEncoder) GetFilterName() string { return StreamEncodingFilterNameASCIIHex }

// DecodeReader returns a reader that ASCIIHex-decodes from r. Whitespace is
// skipped, pairs of hex digits become one output byte, and decoding stops
// at the '>' EOD marker. An odd trailing digit before '>' is padded with
// '0' to match DecodeBytes. An input that ends without '>' surfaces as
// io.ErrUnexpectedEOF, matching DecodeBytes' error return for that case.
func (asciiHex *ASCIIHexEncoder) DecodeReader(r io.Reader) io.Reader {
	return asciiHex.limits.reader(StreamEncodingFilterNameASCIIHex, &asciiHexReader{src: newByteReader(r)})
}

// EncodeBytes ASCII encodes the passed in slice of bytes.
func (asciiHex *ASCIIHexEncoder) EncodeBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	for _, b := range data {
		buf.WriteString(fmt.Sprintf("%.2X ", b))
	}
	buf.WriteByte('>')
	return buf.Bytes(), nil
}

// NewASCIIHexEncoder makes a new ASCII hex encoder.
func NewASCIIHexEncoder() *ASCIIHexEncoder { asciiHex := &ASCIIHexEncoder{}; return asciiHex }

// UpdateParams updates the parameter values of the encoder.
func (asciiHex *ASCIIHexEncoder) UpdateParams(params *PdfObjectDictionary) {}

// DecodeStream implements ASCII hex decoding.
func (asciiHex *ASCIIHexEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return asciiHex.DecodeBytes(streamObj.Stream)
}

// DecodeBytes decodes a slice of ASCII encoded bytes and returns the result.
func (asciiHex *ASCIIHexEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	reader := bytes.NewReader(encoded)
	var data []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == '>' {
			break
		}
		if IsWhiteSpace(b) {
			continue
		}
		if (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') || (b >= '0' && b <= '9') {
			data = append(data, b)
		} else {
			common.Log.Debug("ERROR: Invalid ascii hex character (%c)", b)
			return nil, fmt.Errorf("invalid ascii hex character (%c)", b)
		}
	}
	if len(data)%2 == 1 {
		data = append(data, '0')
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Inbound %s", data)
	}
	decodedLen := hex.DecodedLen(len(data))
	if err := asciiHex.limits.check(StreamEncodingFilterNameASCIIHex, int64(decodedLen)); err != nil {
		return nil, err
	}
	decoded := make([]byte, decodedLen)
	_, err := hex.Decode(decoded, data)
	if err != nil {
		return nil, err
	}
	asciiHex.limits.charge(int64(decodedLen))
	return decoded, nil
}

type asciiHexReader struct {
	src byteReader
	err error
}

func (r *asciiHexReader) nextDigit() (byte, bool) {
	for {
		b, err := r.src.ReadByte()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			r.err = err
			return 0, false
		}
		if b == '>' {
			r.err = io.EOF
			return 0, false
		}
		if IsWhiteSpace(b) {
			continue
		}
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') {
			return b, true
		}
		common.Log.Debug("ERROR: Invalid ascii hex character (%c)", b)
		r.err = fmt.Errorf("invalid ascii hex character (%c)", b)
		return 0, false
	}
}

func (r *asciiHexReader) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if r.err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, r.err
		}
		b, ok := r.nextDigit()
		if !ok {
			continue
		}
		lo, ok := r.nextDigit()
		if !ok {
			if r.err == io.EOF {
				p[total] = hexDigitValue(b) << 4
				total++
			}
			continue
		}
		p[total] = hexDigitValue(b)<<4 | hexDigitValue(lo)
		total++
	}
	return total, nil
}
