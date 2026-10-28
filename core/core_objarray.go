package core

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenMellon/openpdf/common"
)

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

// PdfObjectArray represents the primitive PDF array object.
type PdfObjectArray struct{ elements []PdfObject }

// Elements returns a slice of the PdfObject elements in the array.
func (arr *PdfObjectArray) Elements() []PdfObject {
	if arr == nil {
		return nil
	}
	return arr.elements
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

// MakeArray creates an PdfObjectArray from a list of PdfObjects.
func MakeArray(objects ...PdfObject) *PdfObjectArray { return &PdfObjectArray{elements: objects} }

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
