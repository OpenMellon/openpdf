package core

import (
	"bytes"
	"errors"
	"io"

	"github.com/OpenMellon/openpdf/common"
)

// ASCII85Encoder implements ASCII85 encoder/decoder.
type ASCII85Encoder struct{ limits decodeLimits }

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

// DecodeStream implements ASCII85 stream decoding.
func (ascii85 *ASCII85Encoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return ascii85.DecodeBytes(streamObj.Stream)
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

func (ascii85 *ASCII85Encoder) setDecodeLimits(limits decodeLimits) { ascii85.limits = limits }

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

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (ascii85 *ASCII85Encoder) MakeDecodeParams() PdfObject { return nil }

// MakeStreamDict make a new instance of an encoding dictionary for a stream object.
func (ascii85 *ASCII85Encoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(ascii85.GetFilterName()))
	return dict
}

// GetFilterName returns the name of the encoding filter.
func (ascii85 *ASCII85Encoder) GetFilterName() string { return StreamEncodingFilterNameASCII85 }
