package core

import (
	"bytes"
	"fmt"

	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/core/security/crypt"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/strutils"
)

func makeCryptFilterDict(filter crypt.Filter, event security.AuthEvent) *PdfObjectDictionary {
	if event == "" {
		event = security.EventDocOpen
	}
	dict := MakeDict()
	dict.Set("Type", MakeName("CryptFilter"))
	dict.Set("AuthEvent", MakeName(string(event)))
	dict.Set("CFM", MakeName(filter.Name()))
	dict.Set("Length", MakeInteger(int64(filter.KeyLength())))
	return dict
}

// MakeArrayFromIntegers64 creates an PdfObjectArray from a slice of int64s, where each array element
// is an PdfObjectInteger.
func MakeArrayFromIntegers64(vals []int64) *PdfObjectArray {
	array := MakeArray()
	for _, val := range vals {
		array.Append(MakeInteger(val))
	}
	return array
}

// MakeArray creates an PdfObjectArray from a list of PdfObjects.
func MakeArray(objects ...PdfObject) *PdfObjectArray { return &PdfObjectArray{elements: objects} }

// MakeArrayFromFloats creates an PdfObjectArray from a slice of float64s, where each array element is an
// PdfObjectFloat.
func MakeArrayFromFloats(vals []float64) *PdfObjectArray {
	array := MakeArray()
	for _, val := range vals {
		array.Append(MakeFloat(val))
	}
	return array
}

// MakeArrayFromIntegers creates an PdfObjectArray from a slice of ints, where each array element is
// an PdfObjectInteger.
func MakeArrayFromIntegers(vals []int) *PdfObjectArray {
	array := MakeArray()
	for _, val := range vals {
		array.Append(MakeInteger(int64(val)))
	}
	return array
}

// MakeDict creates and returns an empty PdfObjectDictionary.
func MakeDict() *PdfObjectDictionary {
	dict := &PdfObjectDictionary{}
	dict.dict = map[PdfObjectName]PdfObject{}
	dict.keys = []PdfObjectName{}
	return dict
}

// MakeDictMap creates a PdfObjectDictionary initialized from a map of keys to values.
func MakeDictMap(objmap map[string]PdfObject) *PdfObjectDictionary {
	dict := MakeDict()
	return dict.Update(objmap)
}

// MakeName creates a PdfObjectName from a string.
func MakeName(s string) *PdfObjectName { name := PdfObjectName(s); return &name }

// MakeBool creates a PdfObjectBool from a bool value.
func MakeBool(val bool) *PdfObjectBool { boolObj := PdfObjectBool(val); return &boolObj }

// MakeHexStringFromHexNumber creates an PdfObjectString from a numeric value intended for output as a hexadecimal string.
// The value is formatted as a hex string with padding to match the specified byte width (byteWidth * 2 digits).
func MakeHexStringFromHexNumber(val uint32, byteWidth int) *PdfObjectString {
	hex := fmt.Sprintf("%0*x", byteWidth*2, val)
	str := PdfObjectString{val: hex, isHex: true, hexEncoded: true}
	return &str
}

// MakeStringFromBytes creates an PdfObjectString from a byte array.
// This is more natural than MakeString as `data` is usually not utf-8 encoded.
func MakeStringFromBytes(data []byte) *PdfObjectString { return MakeString(string(data)) }

// MakeNull creates an PdfObjectNull.
func MakeNull() *PdfObjectNull { null := PdfObjectNull{}; return &null }

// MakeHexString creates an PdfObjectString from a string intended for output as a hexadecimal string.
func MakeHexString(s string) *PdfObjectString {
	str := PdfObjectString{val: s, isHex: true}
	return &str
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

// MakeInteger creates a PdfObjectInteger from an int64.
func MakeInteger(val int64) *PdfObjectInteger { num := PdfObjectInteger(val); return &num }

// MakeFloat creates an PdfObjectFloat from a float64.
func MakeFloat(val float64) *PdfObjectFloat {
	val = precision.RoundDefault(val)
	num := PdfObjectFloat(val)
	return &num
}

// MakeString creates an PdfObjectString from a string.
// NOTE: PDF does not use utf-8 string encoding like Go so `s` will often not be a utf-8 encoded
// string.
func MakeString(s string) *PdfObjectString { str := PdfObjectString{val: s}; return &str }

// MakeStream creates an PdfObjectStream with specified contents and encoding. If encoding is nil, then raw encoding
// will be used (i.e. no encoding applied).
func MakeStream(contents []byte, encoder StreamEncoder) (*PdfObjectStream, error) {
	stream := &PdfObjectStream{}
	if encoder == nil {
		encoder = NewRawEncoder()
	}
	stream.PdfObjectDictionary = encoder.MakeStreamDict()
	bytes, err := encoder.EncodeBytes(contents)
	if err != nil {
		return nil, err
	}
	stream.Set("Length", MakeInteger(int64(len(bytes))))
	stream.Stream = bytes
	return stream, nil
}
