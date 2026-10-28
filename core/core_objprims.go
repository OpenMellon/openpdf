package core

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/strutils"
)

// Str returns the string value of the PdfObjectString. Defined in addition to String() function to clarify that
// this function returns the underlying string directly, whereas the String function technically could include
// debug info.
func (str *PdfObjectString) Str() string { return str.val }

// Direct traces the indirect object to the direct object it contains.
func (indirectObject *PdfIndirectObject) Direct() PdfObject {
	return TraceToDirectObject(indirectObject)
}

// String returns a string representation of `name`.
func (name *PdfObjectName) String() string { return string(*name) }

// Decoded returns the PDFDocEncoding or UTF-16BE decoded string contents.
// UTF-16BE is applied when the first two bytes are 0xFE, 0XFF, otherwise decoding of
// PDFDocEncoding is performed.
func (str *PdfObjectString) Decoded() string {
	if str == nil {
		return ""
	}
	data := []byte(str.val)
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return strutils.UTF16ToString(data[2:])
	}
	return strutils.PDFDocEncodingToString(data)
}

// Bytes returns the PdfObjectString content as a []byte array.
func (str *PdfObjectString) Bytes() []byte { return []byte(str.val) }

// PdfObjectFloat represents the primitive PDF floating point numerical object.
type PdfObjectFloat float64

// Write outputs the object as it is to be written to file.
func (name *PdfObjectName) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		if len(*name) > 127 {
			common.Log.Debug("ERROR: Name too long (%s)", *name)
		}
		buf.WriteString("/")
		for i := 0; i < len(*name); i++ {
			b := (*name)[i]
			if !IsPrintable(b) || b == '#' || IsDelimiter(b) {
				fmt.Fprintf(buf, "#%.2x", b)
			} else {
				buf.WriteByte(b)
			}
		}
	})
}

// MakeName creates a PdfObjectName from a string.
func MakeName(s string) *PdfObjectName { name := PdfObjectName(s); return &name }

// Equals reports whether other has the same contents as this object (deep comparison).
func (str *PdfObjectString) Equals(other PdfObject) bool {
	if str == nil {
		return other == nil
	}
	return equalObjects(str, other, 0, nil)
}

// MakeBool creates a PdfObjectBool from a bool value.
func MakeBool(val bool) *PdfObjectBool { boolObj := PdfObjectBool(val); return &boolObj }

// String returns a string describing `ref`.
func (ref *PdfObjectReference) String() string {
	return fmt.Sprintf("Ref(%d %d)", ref.ObjectNumber, ref.GenerationNumber)
}
func (num *PdfObjectInteger) String() string { return fmt.Sprintf("%d", *num) }

// Resolve resolves the reference and returns the indirect or stream object.
// If the reference cannot be resolved, a *PdfObjectNull object is returned.
func (ref *PdfObjectReference) Resolve() PdfObject {
	if ref.parser == nil {
		return MakeNull()
	}
	reference, _, err := ref.parser.resolveReference(ref)
	if err != nil {
		common.Log.Debug("ERROR resolving reference: %v - returning null object", err)
		return MakeNull()
	}
	if reference == nil {
		common.Log.Debug("ERROR resolving reference: nil object - returning a null object")
		return MakeNull()
	}
	return reference
}

// Direct returns the direct object this object represents. For leaf and container
// objects this is the object itself.
func (b *PdfObjectBool) Direct() PdfObject { return b }

// PdfObjectReference represents the primitive PDF reference object.
type PdfObjectReference struct {
	parser           *PdfParser
	ObjectNumber     int64
	GenerationNumber int64
}

// Write outputs the object as it is to be written to file.
func (null *PdfObjectNull) Write() []byte { return []byte("null") }

var stringEscapes = map[byte][]byte{'\n': []byte("\\n"), '\r': []byte("\\r"), '\t': []byte("\\t"), '\b': []byte("\\b"), '\f': []byte("\\f"), '(': []byte("\\("), ')': []byte("\\)"), '\\': []byte("\\\\")}

// Write outputs the object as it is to be written to file.
func (ref *PdfObjectReference) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(ref.ObjectNumber, 10))
		buf.WriteString(" ")
		buf.WriteString(strconv.FormatInt(ref.GenerationNumber, 10))
		buf.WriteString(" R")
	})
}

// String returns the state of the bool as "true" or "false".
func (b *PdfObjectBool) String() string {
	if *b {
		return "true"
	}
	return "false"
}

// PdfObject is an interface which all primitive PDF objects must implement.
type PdfObject interface {
	// String outputs a string representation of the primitive (for debugging).
	String() string

	// Write outputs the PDF primitive as written to file as expected by the standard.
	Write() []byte

	// Direct returns the direct object this object represents, dereferencing
	// references and indirect objects. Leaf and container objects return themselves.
	// It is equivalent to TraceToDirectObject(obj).
	Direct() PdfObject

	// Equals reports whether other has the same contents as this object. It performs
	// the same deep comparison as EqualObjects(obj, other).
	Equals(obj PdfObject) bool
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (ind *PdfIndirectObject) Equals(other PdfObject) bool {
	if ind == nil {
		return other == nil
	}
	return equalObjects(ind, other, 0, nil)
}

// Write outputs the object as it is to be written to file.
func (ind *PdfIndirectObject) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(ind.ObjectNumber, 10))
		buf.WriteString(" 0 R")
	})
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (null *PdfObjectNull) Equals(other PdfObject) bool {
	if null == nil {
		return other == nil
	}
	return equalObjects(null, other, 0, nil)
}
func (num *PdfObjectFloat) String() string { return fmt.Sprintf("%f", *num) }

// PdfObjectNull represents the primitive PDF null object.
type PdfObjectNull struct{}

// String returns a string describing `null`.
func (null *PdfObjectNull) String() string { return "null" }

// Write outputs the object as it is to be written to file.
func (b *PdfObjectBool) Write() []byte {
	if *b {
		return []byte("true")
	}
	return []byte("false")
}

// Write outputs the object as it is to be written to file.
func (str *PdfObjectString) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		if str.isHex {
			val := str.val
			if !str.hexEncoded {
				val = hex.EncodeToString(str.Bytes())
			}
			buf.Grow(len(val) + 2)
			buf.WriteString("<")
			buf.WriteString(val)
			buf.WriteString(">")
			return
		}
		buf.Grow(len(str.val) + len(str.val)/5 + 2)
		buf.WriteString("(")
		for i := 0; i < len(str.val); i++ {
			b := str.val[i]
			if data, ok := stringEscapes[b]; ok {
				buf.Write(data)
			} else {
				buf.WriteByte(b)
			}
		}
		buf.WriteString(")")
	})
}

// Write outputs the object as it is to be written to file.
func (num *PdfObjectInteger) Write() []byte { return []byte(strconv.FormatInt(int64(*num), 10)) }

// MakeHexStringFromHexNumber creates an PdfObjectString from a numeric value intended for output as a hexadecimal string.
// The value is formatted as a hex string with padding to match the specified byte width (byteWidth * 2 digits).
func MakeHexStringFromHexNumber(val uint32, byteWidth int) *PdfObjectString {
	hex := fmt.Sprintf("%0*x", byteWidth*2, val)
	str := PdfObjectString{val: hex, isHex: true, hexEncoded: true}
	return &str
}

func hexDigitValue(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c <= 'F':
		return c - 'A' + 10
	default:
		return c - 'a' + 10
	}
}

// MakeStringFromBytes creates an PdfObjectString from a byte array.
// This is more natural than MakeString as `data` is usually not utf-8 encoded.
func MakeStringFromBytes(data []byte) *PdfObjectString { return MakeString(string(data)) }

// PdfObjectBool represents the primitive PDF boolean object.
type PdfObjectBool bool

// PdfObjectInteger represents the primitive PDF integer numerical object.
type PdfObjectInteger int64

// Equals reports whether other has the same contents as this object (deep comparison).
func (num *PdfObjectFloat) Equals(other PdfObject) bool {
	if num == nil {
		return other == nil
	}
	return equalObjects(num, other, 0, nil)
}

// MakeNull creates an PdfObjectNull.
func MakeNull() *PdfObjectNull                { null := PdfObjectNull{}; return &null }
func (null *PdfObjectNull) Direct() PdfObject { return null }

// PdfObjectName represents the primitive PDF name object.
type PdfObjectName string

// Equals compares reference identity (ObjectNumber and GenerationNumber), not the
// contents of the referenced object. This is consistent with EqualObjects for
// references. For content equality, use ref.Direct().Equals(other.Direct()) instead.
func (ref *PdfObjectReference) Equals(other PdfObject) bool {
	if ref == nil {
		return other == nil
	}
	return equalObjects(ref, other, 0, nil)
}

// IsHexadecimal checks if the PdfObjectString contains Hexadecimal data.
func (str *PdfObjectString) IsHexadecimal() bool { return str.isHex }

// MakeHexString creates an PdfObjectString from a string intended for output as a hexadecimal string.
func MakeHexString(s string) *PdfObjectString {
	str := PdfObjectString{val: s, isHex: true}
	return &str
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (name *PdfObjectName) Equals(other PdfObject) bool {
	if name == nil {
		return other == nil
	}
	return equalObjects(name, other, 0, nil)
}

// PdfObjectString represents the primitive PDF string object.
type PdfObjectString struct {
	val        string
	isHex      bool
	hexEncoded bool
}

// PdfIndirectObject represents the primitive PDF indirect object.
type PdfIndirectObject struct {
	PdfObjectReference
	PdfObject
}

// MakeEncodedString creates a PdfObjectString with encoded content, which can be either
// UTF-16BE or PDFDocEncoding depending on whether `utf16BE` is true or false respectively.
func MakeEncodedString(s string, utf16BE bool) *PdfObjectString {
	if utf16BE {
		var buf bytes.Buffer
		buf.Write([]byte{0xFE, 0xFF})
		buf.WriteString(strutils.StringToUTF16(s))
		return &PdfObjectString{val: buf.String(), isHex: true}
	}
	return &PdfObjectString{val: string(strutils.StringToPDFDocEncoding(s)), isHex: false}
}

// MakeIndirectObject creates an PdfIndirectObject with a specified direct object PdfObject.
func MakeIndirectObject(obj PdfObject) *PdfIndirectObject {
	indirect := &PdfIndirectObject{}
	indirect.PdfObject = obj
	return indirect
}

var hexDigits = []byte("0123456789abcdefABCDEF")

// Equals reports whether other has the same contents as this object (deep comparison).
func (num *PdfObjectInteger) Equals(other PdfObject) bool {
	if num == nil {
		return other == nil
	}
	return equalObjects(num, other, 0, nil)
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (b *PdfObjectBool) Equals(other PdfObject) bool {
	if b == nil {
		return other == nil
	}
	return equalObjects(b, other, 0, nil)
}
func (str *PdfObjectString) Direct() PdfObject  { return str }
func (num *PdfObjectFloat) Direct() PdfObject   { return num }
func (num *PdfObjectInteger) Direct() PdfObject { return num }

func numberToFloatPtr(obj PdfObject) (*float64, error) {
	switch v := obj.(type) {
	case *PdfObjectFloat:
		f := float64(*v)
		return &f, nil
	case *PdfObjectInteger:
		f := float64(*v)
		return &f, nil
	case *PdfObjectNull:
		return nil, nil
	}
	return nil, ErrNotANumber
}

// MakeInteger creates a PdfObjectInteger from an int64.
func MakeInteger(val int64) *PdfObjectInteger { num := PdfObjectInteger(val); return &num }

// String returns a string representation of the *PdfObjectString.
func (str *PdfObjectString) String() string { return str.val }

// MakeFloat creates an PdfObjectFloat from a float64.
func MakeFloat(val float64) *PdfObjectFloat {
	val = precision.RoundDefault(val)
	num := PdfObjectFloat(val)
	return &num
}

func (name *PdfObjectName) Direct() PdfObject { return name }

// Direct resolves the reference and returns the direct object it points to.
func (ref *PdfObjectReference) Direct() PdfObject { return TraceToDirectObject(ref) }

// GetParser returns the parser for lazy-loading or compare references.
func (ref *PdfObjectReference) GetParser() *PdfParser { return ref.parser }

// Write outputs the object as it is to be written to file.
func (num *PdfObjectFloat) Write() []byte {
	return []byte(strconv.FormatFloat(float64(*num), 'f', -1, 64))
}

// String returns a string describing `ind`.
func (ind *PdfIndirectObject) String() string {
	return fmt.Sprintf("IObject:%d", (*ind).ObjectNumber)
}

// MakeString creates an PdfObjectString from a string.
// NOTE: PDF does not use utf-8 string encoding like Go so `s` will often not be a utf-8 encoded
// string.
func MakeString(s string) *PdfObjectString { str := PdfObjectString{val: s}; return &str }

// IsPrintable checks if a character is printable.
// Regular characters that are outside the range EXCLAMATION MARK(21h)
// (!) to TILDE (7Eh) (~) should be written using the hexadecimal notation.
func IsPrintable(c byte) bool { return 0x21 <= c && c <= 0x7E }
