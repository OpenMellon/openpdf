package xmputil

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/OpenMellon/go-xmp/xmp"
	"github.com/OpenMellon/openpdf/core"
)

func utf32ToUTF8(data []byte, order binary.ByteOrder) []byte {
	if len(data)%4 != 0 {
		return data
	}
	runes := make([]rune, len(data)/4)
	for i := range runes {
		runes[i] = rune(order.Uint32(data[i*4:]))
		if !utf8.ValidRune(runes[i]) {
			return data
		}
	}
	return runesToUTF8(runes)
}

const (
	EncodingUTF8  = "UTF-8"
	EncodingUTF16 = "UTF-16"
	EncodingUTF32 = "UTF-32"
)

// PdfAID is the result of the XMP pdfaid metadata.
type PdfAID struct {
	Part        int
	Conformance string
	Rev         int
}

// PdfInfoOptions are the options used for setting pdf info.
type PdfInfoOptions struct {
	InfoDict   core.PdfObject
	PdfVersion string
	Copyright  string
	Marked     bool

	// Overwrite if set to true, overwrites all values found in the current pdf info xmp model to the ones provided.
	Overwrite bool
}

func trimLeadingWhitespace(data []byte) []byte {
	for _, n := range []int{4, 2, 1} {
		for len(data) >= n && isWhitespaceRun(data[:n]) {
			data = data[n:]
		}
	}
	return data
}

// PdfUAID is the result of the XMP pdfuaid metadata.
type PdfUAID struct {
	Part int
	Amd  string
	Corr string
	Rev  int
}

func altStringText(alt xmp.AltString) string {
	if def := alt.Default(); def != "" {
		return def
	}
	for _, item := range alt {
		if item.Value != "" {
			return item.Value
		}
	}
	return ""
}

// PacketEncoding reports the character encoding an XMP packet is stored in, without
// parsing it. A packet that cannot be parsed still has a detectable encoding.
func PacketEncoding(stream []byte) string { _, enc := decodePacket(stream); return enc }

// PdfInfo is the xmp document pdf info.
type PdfInfo struct {
	InfoDict   core.PdfObject
	PdfVersion string
	Copyright  string
	Marked     bool
}

func isWhitespaceRun(data []byte) bool {
	var seen byte
	for _, b := range data {
		switch {
		case b == 0x00:
		case b == 0x09 || b == 0x0A || b == 0x0D || b == 0x20:
			if seen != 0 {
				return false
			}
			seen = b
		default:
			return false
		}
	}
	return seen != 0
}

func utf16ToUTF8(data []byte, order binary.ByteOrder) []byte {
	if len(data)%2 != 0 {
		return data
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[i*2:])
	}
	if hasInvalidSurrogates(units) {
		return data
	}
	return runesToUTF8(utf16.Decode(units))
}

func runesToUTF8(runes []rune) []byte {
	var buf bytes.Buffer
	buf.Grow(len(runes) * utf8.UTFMax)
	for _, r := range runes {
		buf.WriteRune(r)
	}
	return buf.Bytes()
}

func decodePacket(data []byte) ([]byte, string) {
	data = trimLeadingWhitespace(data)
	switch {
	case bytes.HasPrefix(data, []byte{0x00, 0x00, 0xFE, 0xFF}):
		return utf32ToUTF8(data[4:], binary.BigEndian), EncodingUTF32
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE, 0x00, 0x00}):
		return utf32ToUTF8(data[4:], binary.LittleEndian), EncodingUTF32
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return utf16ToUTF8(data[2:], binary.BigEndian), EncodingUTF16
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return utf16ToUTF8(data[2:], binary.LittleEndian), EncodingUTF16
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return data[3:], EncodingUTF8
	case bytes.HasPrefix(data, []byte{0x00, 0x00, 0x00, '<'}):
		return utf32ToUTF8(data, binary.BigEndian), EncodingUTF32
	case bytes.HasPrefix(data, []byte{'<', 0x00, 0x00, 0x00}):
		return utf32ToUTF8(data, binary.LittleEndian), EncodingUTF32
	case bytes.HasPrefix(data, []byte{0x00, '<', 0x00}):
		return utf16ToUTF8(data, binary.BigEndian), EncodingUTF16
	case bytes.HasPrefix(data, []byte{'<', 0x00}):
		return utf16ToUTF8(data, binary.LittleEndian), EncodingUTF16
	}
	return data, EncodingUTF8
}

func hasInvalidSurrogates(units []uint16) bool {
	for i := 0; i < len(units); i++ {
		switch {
		case units[i] >= 0xD800 && units[i] < 0xDC00:
			if i+1 >= len(units) || units[i+1] < 0xDC00 || units[i+1] > 0xDFFF {
				return true
			}
			i++
		case units[i] >= 0xDC00 && units[i] <= 0xDFFF:
			return true
		}
	}
	return false
}

// GUID is a string representing a globally unique identifier.
type GUID string
