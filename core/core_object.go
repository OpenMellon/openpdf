package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/strutils"
)

// PdfObjectArray represents the primitive PDF array object.
type PdfObjectArray struct{ elements []PdfObject }

// Len returns the number of elements in the array.
func (arr *PdfObjectArray) Len() int {
	if arr == nil {
		return 0
	}
	return len(arr.elements)
}

// Clear resets the array to an empty state.
func (arr *PdfObjectArray) Clear() { arr.elements = []PdfObject{} }

// String returns a string describing `array`.
func (arr *PdfObjectArray) String() string {
	var out strings.Builder
	out.WriteString("[")
	for i, elem := range arr.Elements() {
		out.WriteString(elem.String())
		if i < (arr.Len() - 1) {
			out.WriteString(", ")
		}
	}
	out.WriteString("]")
	return out.String()
}

// Elements returns a slice of the PdfObject elements in the array.
func (arr *PdfObjectArray) Elements() []PdfObject {
	if arr == nil {
		return nil
	}
	return arr.elements
}

// ToInt64Slice returns a slice of all array elements as an int64 slice. An error is returned if the
// array non-integer objects. Each element can only be PdfObjectInteger.
func (arr *PdfObjectArray) ToInt64Slice() ([]int64, error) {
	var nums []int64
	for _, elem := range arr.Elements() {
		if num, ok := elem.(*PdfObjectInteger); ok {
			nums = append(nums, int64(*num))
		} else {
			return nil, ErrTypeError
		}
	}
	return nums, nil
}

// Get returns the i-th element of the array or nil if out of bounds (by index).
func (arr *PdfObjectArray) Get(i int) PdfObject {
	if arr == nil || i >= len(arr.elements) || i < 0 {
		return nil
	}
	return arr.elements[i]
}

// ToFloat64Array returns a slice of all elements in the array as a float64 slice.  An error is
// returned if the array contains non-numeric objects (each element can be either PdfObjectInteger
// or PdfObjectFloat).
func (arr *PdfObjectArray) ToFloat64Array() ([]float64, error) {
	var floats []float64
	for _, elem := range arr.Elements() {
		switch v := elem.(type) {
		case *PdfObjectInteger:
			floats = append(floats, float64(*v))
		case *PdfObjectFloat:
			floats = append(floats, float64(*v))
		default:
			return nil, ErrTypeError
		}
	}
	return floats, nil
}

// GetAsFloat64Slice returns the array as []float64 slice.
// Returns an error if not entirely numeric (only PdfObjectIntegers, PdfObjectFloats).
func (arr *PdfObjectArray) GetAsFloat64Slice() ([]float64, error) {
	var floats []float64
	for _, elem := range arr.Elements() {
		numberAsFloat, err := GetNumberAsFloat(TraceToDirectObject(elem))
		if err != nil {
			return nil, fmt.Errorf("array element not a number")
		}
		floats = append(floats, numberAsFloat)
	}
	return floats, nil
}

// Append appends PdfObject(s) to the array.
func (arr *PdfObjectArray) Append(objects ...PdfObject) {
	if arr == nil {
		common.Log.Debug("Warn - Attempt to append to a nil array")
		return
	}
	arr.elements = append(arr.elements, objects...)
}

// Write outputs the object as it is to be written to file.
func (arr *PdfObjectArray) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString("[")
		for i, elem := range arr.Elements() {
			buf.Write(elem.Write())
			if i < (arr.Len() - 1) {
				buf.WriteString(" ")
			}
		}
		buf.WriteString("]")
	})
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (arr *PdfObjectArray) Equals(other PdfObject) bool {
	if arr == nil {
		return other == nil
	}
	return equalObjects(arr, other, 0, nil)
}

// Set sets the PdfObject at index i of the array. An error is returned if the index is outside bounds.
func (arr *PdfObjectArray) Set(i int, obj PdfObject) error {
	if i < 0 || i >= len(arr.elements) {
		return errors.New("outside bounds")
	}
	arr.elements[i] = obj
	return nil
}

func (arr *PdfObjectArray) Direct() PdfObject { return arr }

// ToIntegerArray returns a slice of all array elements as an int slice. An error is returned if the
// array non-integer objects. Each element can only be PdfObjectInteger.
func (arr *PdfObjectArray) ToIntegerArray() ([]int, error) {
	var nums []int
	for _, elem := range arr.Elements() {
		if num, ok := elem.(*PdfObjectInteger); ok {
			nums = append(nums, int(*num))
		} else {
			return nil, ErrTypeError
		}
	}
	return nums, nil
}

// PdfObjectDictionary represents the primitive PDF dictionary/map object.
type PdfObjectDictionary struct {
	dict   map[PdfObjectName]PdfObject
	keys   []PdfObjectName
	mu     sync.RWMutex
	parser *PdfParser
}

// GetString is a helper for Get that returns a string value.
// Returns false if the key is missing or a value is not a string.
func (dict *PdfObjectDictionary) GetString(key PdfObjectName) (string, bool) {
	obj := dict.Get(key)
	if obj == nil {
		return "", false
	}
	str, ok := obj.(*PdfObjectString)
	if !ok {
		return "", false
	}
	return str.Str(), true
}

// Get returns the PdfObject corresponding to the specified key.
// Returns a nil value if the key is not set.
func (dict *PdfObjectDictionary) Get(key PdfObjectName) PdfObject {
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	obj, ok := dict.dict[key]
	if !ok {
		return nil
	}
	return obj
}

// Clear resets the dictionary to an empty state.
func (dict *PdfObjectDictionary) Clear() {
	dict.mu.Lock()
	defer dict.mu.Unlock()
	dict.keys = []PdfObjectName{}
	dict.dict = map[PdfObjectName]PdfObject{}
}

// Update updates multiple keys and returns the dictionary back so can be used in a chained fashion.
func (dict *PdfObjectDictionary) Update(objmap map[string]PdfObject) *PdfObjectDictionary {
	dict.mu.Lock()
	defer dict.mu.Unlock()
	for sKey, obj := range objmap {
		dict.setWithLock(PdfObjectName(sKey), obj, false)
	}
	return dict
}

func (dict *PdfObjectDictionary) Direct() PdfObject { return dict }

// Equals reports whether other has the same contents as this object (deep comparison).
func (dict *PdfObjectDictionary) Equals(other PdfObject) bool {
	if dict == nil {
		return other == nil
	}
	return equalObjects(dict, other, 0, nil)
}

func (dict *PdfObjectDictionary) setWithLock(key PdfObjectName, val PdfObject, lock bool) {
	if lock {
		dict.mu.Lock()
		defer dict.mu.Unlock()
	}
	_, ok := dict.dict[key]
	if !ok {
		dict.keys = append(dict.keys, key)
	}
	dict.dict[key] = val
}

// Set sets the dictionary's key -> val mapping entry. Overwrites if key already set.
func (dict *PdfObjectDictionary) Set(key PdfObjectName, val PdfObject) {
	dict.setWithLock(key, val, true)
}

// String returns a string describing `d`.
func (dict *PdfObjectDictionary) String() string {
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	var builder strings.Builder
	builder.WriteString("Dict(")
	for _, key := range dict.keys {
		obj := dict.dict[key]
		builder.WriteString("\"" + key.String() + "\": ")
		builder.WriteString(obj.String())
		builder.WriteString(", ")
	}
	builder.WriteString(")")
	return builder.String()
}

// Merge merges in key/values from another dictionary. Overwriting if has same keys.
// The mutated dictionary (d) is returned in order to allow method chaining.
func (dict *PdfObjectDictionary) Merge(another *PdfObjectDictionary) *PdfObjectDictionary {
	if another != nil {
		for _, key := range another.Keys() {
			obj := another.Get(key)
			dict.Set(key, obj)
		}
	}
	return dict
}

// ShallowCopy returns a new PdfObjectDictionary with the same key-value mappings.
// The new dictionary has its own mutex and independent keys slice, but values
// are shared (not deep-copied).
func (dict *PdfObjectDictionary) ShallowCopy() *PdfObjectDictionary {
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	clone := &PdfObjectDictionary{}
	clone.dict = make(map[PdfObjectName]PdfObject, len(dict.dict))
	clone.keys = make([]PdfObjectName, len(dict.keys))
	copy(clone.keys, dict.keys)
	maps.Copy(clone.dict, dict.dict)
	clone.parser = dict.parser
	return clone
}

// Write outputs the object as it is to be written to file.
func (dict *PdfObjectDictionary) Write() []byte {
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	var buf bytes.Buffer
	buf.WriteString("<<")
	for _, key := range dict.keys {
		obj := dict.dict[key]
		buf.Write(key.Write())
		buf.WriteString(" ")
		buf.Write(obj.Write())
	}
	buf.WriteString(">>")
	return buf.Bytes()
}

// Remove removes an element specified by key.
func (dict *PdfObjectDictionary) Remove(key PdfObjectName) {
	dict.mu.Lock()
	defer dict.mu.Unlock()
	idx := -1
	for i, name := range dict.keys {
		if name == key {
			idx = i
			break
		}
	}
	if idx >= 0 {
		dict.keys = append(dict.keys[:idx], dict.keys[idx+1:]...)
		delete(dict.dict, key)
	}
}

// Keys returns the list of keys in the dictionary.
// If `d` is nil returns a nil slice.
func (dict *PdfObjectDictionary) Keys() []PdfObjectName {
	if dict == nil {
		return nil
	}
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	names := make([]PdfObjectName, len(dict.keys))
	copy(names, dict.keys)
	return names
}

// SetIfNotNil sets the dictionary's key -> val mapping entry -IF- val is not nil.
// Note that we take care to perform a type switch.  Otherwise if we would supply a nil value
// of another type, e.g. (PdfObjectArray*)(nil), then it would not be a PdfObject(nil) and thus
// would get set.
func (dict *PdfObjectDictionary) SetIfNotNil(key PdfObjectName, val PdfObject) {
	if val != nil {
		switch v := val.(type) {
		case *PdfObjectName:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectDictionary:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectStream:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectString:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectNull:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectInteger:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectArray:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectBool:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectFloat:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectReference:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfIndirectObject:
			if v != nil {
				dict.Set(key, val)
			}
		default:
			common.Log.Error("ERROR: Unknown type: %T - should never happen!", val)
		}
	}
}

// PdfObjectString represents the primitive PDF string object.
type PdfObjectString struct {
	val        string
	isHex      bool
	hexEncoded bool
}

// Str returns the string value of the PdfObjectString. Defined in addition to String() function to clarify that
// this function returns the underlying string directly, whereas the String function technically could include
// debug info.
func (str *PdfObjectString) Str() string { return str.val }

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

// Equals reports whether other has the same contents as this object (deep comparison).
func (str *PdfObjectString) Equals(other PdfObject) bool {
	if str == nil {
		return other == nil
	}
	return equalObjects(str, other, 0, nil)
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

// IsHexadecimal checks if the PdfObjectString contains Hexadecimal data.
func (str *PdfObjectString) IsHexadecimal() bool { return str.isHex }

func (str *PdfObjectString) Direct() PdfObject { return str }

// String returns a string representation of the *PdfObjectString.
func (str *PdfObjectString) String() string { return str.val }

// PdfObjectName represents the primitive PDF name object.
type PdfObjectName string

// String returns a string representation of `name`.
func (name *PdfObjectName) String() string { return string(*name) }

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

// Equals reports whether other has the same contents as this object (deep comparison).
func (name *PdfObjectName) Equals(other PdfObject) bool {
	if name == nil {
		return other == nil
	}
	return equalObjects(name, other, 0, nil)
}

func (name *PdfObjectName) Direct() PdfObject { return name }

// PdfObjectFloat represents the primitive PDF floating point numerical object.
type PdfObjectFloat float64

func (num *PdfObjectFloat) String() string { return fmt.Sprintf("%f", *num) }

// Equals reports whether other has the same contents as this object (deep comparison).
func (num *PdfObjectFloat) Equals(other PdfObject) bool {
	if num == nil {
		return other == nil
	}
	return equalObjects(num, other, 0, nil)
}

func (num *PdfObjectFloat) Direct() PdfObject { return num }

// Write outputs the object as it is to be written to file.
func (num *PdfObjectFloat) Write() []byte {
	return []byte(strconv.FormatFloat(float64(*num), 'f', -1, 64))
}

// PdfObjectReference represents the primitive PDF reference object.
type PdfObjectReference struct {
	parser           *PdfParser
	ObjectNumber     int64
	GenerationNumber int64
}

// String returns a string describing `ref`.
func (ref *PdfObjectReference) String() string {
	return fmt.Sprintf("Ref(%d %d)", ref.ObjectNumber, ref.GenerationNumber)
}

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

// Write outputs the object as it is to be written to file.
func (ref *PdfObjectReference) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(ref.ObjectNumber, 10))
		buf.WriteString(" ")
		buf.WriteString(strconv.FormatInt(ref.GenerationNumber, 10))
		buf.WriteString(" R")
	})
}

// Equals compares reference identity (ObjectNumber and GenerationNumber), not the
// contents of the referenced object. This is consistent with EqualObjects for
// references. For content equality, use ref.Direct().Equals(other.Direct()) instead.
func (ref *PdfObjectReference) Equals(other PdfObject) bool {
	if ref == nil {
		return other == nil
	}
	return equalObjects(ref, other, 0, nil)
}

// Direct resolves the reference and returns the direct object it points to.
func (ref *PdfObjectReference) Direct() PdfObject { return TraceToDirectObject(ref) }

// GetParser returns the parser for lazy-loading or compare references.
func (ref *PdfObjectReference) GetParser() *PdfParser { return ref.parser }

// PdfObjectInteger represents the primitive PDF integer numerical object.
type PdfObjectInteger int64

func (num *PdfObjectInteger) String() string { return fmt.Sprintf("%d", *num) }

// Write outputs the object as it is to be written to file.
func (num *PdfObjectInteger) Write() []byte { return []byte(strconv.FormatInt(int64(*num), 10)) }

// Equals reports whether other has the same contents as this object (deep comparison).
func (num *PdfObjectInteger) Equals(other PdfObject) bool {
	if num == nil {
		return other == nil
	}
	return equalObjects(num, other, 0, nil)
}

func (num *PdfObjectInteger) Direct() PdfObject { return num }

// PdfObjectBool represents the primitive PDF boolean object.
type PdfObjectBool bool

// Direct returns the direct object this object represents. For leaf and container
// objects this is the object itself.
func (b *PdfObjectBool) Direct() PdfObject { return b }

// String returns the state of the bool as "true" or "false".
func (b *PdfObjectBool) String() string {
	if *b {
		return "true"
	}
	return "false"
}

// Write outputs the object as it is to be written to file.
func (b *PdfObjectBool) Write() []byte {
	if *b {
		return []byte("true")
	}
	return []byte("false")
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (b *PdfObjectBool) Equals(other PdfObject) bool {
	if b == nil {
		return other == nil
	}
	return equalObjects(b, other, 0, nil)
}

// PdfObjectNull represents the primitive PDF null object.
type PdfObjectNull struct{}

// Write outputs the object as it is to be written to file.
func (null *PdfObjectNull) Write() []byte { return []byte("null") }

// Equals reports whether other has the same contents as this object (deep comparison).
func (null *PdfObjectNull) Equals(other PdfObject) bool {
	if null == nil {
		return other == nil
	}
	return equalObjects(null, other, 0, nil)
}

// String returns a string describing `null`.
func (null *PdfObjectNull) String() string { return "null" }

func (null *PdfObjectNull) Direct() PdfObject { return null }
