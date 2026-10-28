package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
)

// EncodeBytes encodes data into ASCII85 encoded format.
func (ascii85 *ASCII85Encoder) EncodeBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	for i := 0; i < len(data); i += 4 {
		b := data[i]
		count := 1
		b2 := byte(0)
		if i+1 < len(data) {
			b2 = data[i+1]
			count++
		}
		b3 := byte(0)
		if i+2 < len(data) {
			b3 = data[i+2]
			count++
		}
		b4 := byte(0)
		if i+3 < len(data) {
			b4 = data[i+3]
			count++
		}
		word := (uint32(b) << 24) | (uint32(b2) << 16) | (uint32(b3) << 8) | uint32(b4)
		if word == 0 {
			buf.WriteByte('z')
		} else {
			group := ascii85.base256Tobase85(word)
			for _, c := range group[:count+1] {
				buf.WriteByte(c + '!')
			}
		}
	}
	buf.WriteString("~>")
	return buf.Bytes(), nil
}

// DecodeBytes decodes byte array with ASCII85. 5 ASCII characters -> 4 raw binary bytes
func (ascii85 *ASCII85Encoder) DecodeBytes(encoded []byte) ([]byte, error) {
	var data []byte
	remaining := ascii85.limits.remaining()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ASCII85 Decode")
	}
	pos := 0
	ok := false
	for pos < len(encoded) && !ok {
		group := [5]byte{0, 0, 0, 0, 0}
		skipped := 0
		idx := 0
		outLen := 4
		for idx < 5+skipped {
			if pos+idx == len(encoded) {
				break
			}
			b := encoded[pos+idx]
			if IsWhiteSpace(b) {
				skipped++
				idx++
				continue
			} else if b == '~' && pos+idx+1 < len(encoded) && encoded[pos+idx+1] == '>' {
				outLen = max((idx-skipped)-1, 0)
				ok = true
				break
			} else if b >= '!' && b <= 'u' {
				b -= '!'
			} else if b == 'z' && idx-skipped == 0 {
				outLen = 4
				idx++
				break
			} else {
				common.Log.Error("Failed decoding, invalid code")
				return nil, errors.New("invalid code encountered")
			}
			group[idx-skipped] = b
			idx++
		}
		pos += idx
		for i := outLen + 1; i < 5; i++ {
			group[i] = 84
		}
		word := uint32(group[0])*85*85*85*85 + uint32(group[1])*85*85*85 + uint32(group[2])*85*85 + uint32(group[3])*85 + uint32(group[4])
		wordBytes := []byte{byte((word >> 24) & 0xff), byte((word >> 16) & 0xff), byte((word >> 8) & 0xff), byte(word & 0xff)}
		if int64(len(data)+outLen) > remaining {
			return nil, ascii85.limits.exceeded(StreamEncodingFilterNameASCII85, remaining)
		}
		data = append(data, wordBytes[:outLen]...)
	}
	ascii85.limits.charge(int64(len(data)))
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ASCII85, encoded: % X", encoded)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ASCII85, decoded: % X", data)
	}
	return data, nil
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (asciiHex *ASCIIHexEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(asciiHex.GetFilterName()))
	return dict
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (asciiHex *ASCIIHexEncoder) MakeDecodeParams() PdfObject         { return nil }
func (asciiHex *ASCIIHexEncoder) setDecodeLimits(limits decodeLimits) { asciiHex.limits = limits }

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

// DecodeStream implements ASCII85 stream decoding.
func (ascii85 *ASCII85Encoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return ascii85.DecodeBytes(streamObj.Stream)
}

// GetFilterName returns the name of the encoding filter.
func (asciiHex *ASCIIHexEncoder) GetFilterName() string { return StreamEncodingFilterNameASCIIHex }

func (r *ascii85Reader) fillGroup() error {
	var data [5]byte
	n := 0
	ok := false
	for n < 5 {
		b, err := r.src.ReadByte()
		if err != nil {
			if err == io.EOF {
				if n == 0 {
					return io.EOF
				}
				ok = true
				break
			}
			return err
		}
		if IsWhiteSpace(b) {
			continue
		}
		if b == '~' {
			next, err2 := r.src.ReadByte()
			if err2 != nil || next != '>' {
				return errors.New("invalid code encountered")
			}
			ok = true
			break
		}
		if b == 'z' && n == 0 {
			r.group = [4]byte{0, 0, 0, 0}
			r.groupLen = 4
			r.groupPos = 0
			return nil
		}
		if b >= '!' && b <= 'u' {
			data[n] = b - '!'
			n++
			continue
		}
		return errors.New("invalid code encountered")
	}
	outLen := 4
	if ok {
		outLen = max(n-1, 0)
	}
	for i := n; i < 5; i++ {
		data[i] = 84
	}
	word := uint32(data[0])*85*85*85*85 + uint32(data[1])*85*85*85 + uint32(data[2])*85*85 + uint32(data[3])*85 + uint32(data[4])
	r.group[0] = byte(word >> 24)
	r.group[1] = byte(word >> 16)
	r.group[2] = byte(word >> 8)
	r.group[3] = byte(word)
	r.groupLen = outLen
	r.groupPos = 0
	if ok {
		return io.EOF
	}
	return nil
}

// ASCIIHexEncoder implements ASCII hex encoder/decoder.
type ASCIIHexEncoder struct{ limits decodeLimits }

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

func (ascii85 *ASCII85Encoder) base256Tobase85(val uint32) [5]byte {
	data := [5]byte{0, 0, 0, 0, 0}
	rem := val
	for i := 0; i < 5; i++ {
		div := uint32(1)
		for j := 0; j < 4-i; j++ {
			div *= 85
		}
		digit := rem / div
		rem = rem % div
		data[i] = byte(digit)
	}
	return data
}

// NewASCIIHexEncoder makes a new ASCII hex encoder.
func NewASCIIHexEncoder() *ASCIIHexEncoder { asciiHex := &ASCIIHexEncoder{}; return asciiHex }

// ASCII85Encoder implements ASCII85 encoder/decoder.
type ASCII85Encoder struct{ limits decodeLimits }

func (ascii85 *ASCII85Encoder) setDecodeLimits(limits decodeLimits) { ascii85.limits = limits }

type ascii85Reader struct {
	src      byteReader
	err      error
	group    [4]byte
	groupLen int
	groupPos int
}

func (r *ascii85Reader) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if r.groupPos < r.groupLen {
			copied := copy(p[total:], r.group[r.groupPos:r.groupLen])
			total += copied
			r.groupPos += copied
			continue
		}
		if r.err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, r.err
		}
		r.err = r.fillGroup()
	}
	return total, nil
}

// NewASCII85Encoder makes a new ASCII85 encoder.
func NewASCII85Encoder() *ASCII85Encoder { ascii85 := &ASCII85Encoder{}; return ascii85 }

// DecodeReader returns a reader that ASCII85-decodes from r. Handles the
// PDF/Adobe variant: 5-char groups, 'z' shortcut for four zero bytes, '~>'
// EOD marker, whitespace skipped. The stdlib's encoding/ascii85 does not
// recognize '~>' (it treats '~' as a corrupt byte), so this decoder is
// hand-rolled to match DecodeBytes.
func (ascii85 *ASCII85Encoder) DecodeReader(r io.Reader) io.Reader {
	return ascii85.limits.reader(StreamEncodingFilterNameASCII85, &ascii85Reader{src: newByteReader(r)})
}

// UpdateParams updates the parameter values of the encoder.
func (ascii85 *ASCII85Encoder) UpdateParams(params *PdfObjectDictionary) {}

type asciiHexReader struct {
	src byteReader
	err error
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (ascii85 *ASCII85Encoder) MakeDecodeParams() PdfObject { return nil }

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

// MakeStreamDict make a new instance of an encoding dictionary for a stream object.
func (ascii85 *ASCII85Encoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(ascii85.GetFilterName()))
	return dict
}

// GetFilterName returns the name of the encoding filter.
func (ascii85 *ASCII85Encoder) GetFilterName() string { return StreamEncodingFilterNameASCII85 }
