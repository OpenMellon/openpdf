package core

import (
	"github.com/OpenMellon/openpdf/common"
)

func getNonNegativeInt(dict *PdfObjectDictionary, key PdfObjectName) (int, bool) {
	obj := dict.Get(key)
	if obj == nil {
		return 0, false
	}
	n, err := GetNumberAsInt64(obj)
	if err != nil || n < 0 {
		return 0, false
	}
	return int(n), true
}

// GetStringBytes is like GetStringVal except that it returns the string as a []byte.
// It is for convenience.
func GetStringBytes(obj PdfObject) (data []byte, ok bool) {
	str, ok := TraceToDirectObject(obj).(*PdfObjectString)
	if ok {
		return str.Bytes(), true
	}
	return
}

// GetStream returns the *PdfObjectStream represented by the PdfObject. On type mismatch the found bool flag is
// false and a nil pointer is returned.
func GetStream(obj PdfObject) (stream *PdfObjectStream, ok bool) {
	obj = ResolveReference(obj)
	stream, ok = obj.(*PdfObjectStream)
	return stream, ok
}

// GetNumberAsInt64 returns the contents of `obj` as an int64 if it is an integer or float, or an
// error if it isn't. This is for cases where expecting an integer, but some implementations
// actually store the number in a floating point format.
func GetNumberAsInt64(obj PdfObject) (int64, error) {
	switch v := obj.(type) {
	case *PdfObjectFloat:
		common.Log.Debug("Number expected as integer was stored as float (type casting used)")
		return int64(*v), nil
	case *PdfObjectInteger:
		return int64(*v), nil
	case *PdfObjectReference:
		toDirectObject := TraceToDirectObject(obj)
		return GetNumberAsInt64(toDirectObject)
	case *PdfIndirectObject:
		return GetNumberAsInt64(v.PdfObject)
	}
	return 0, ErrNotANumber
}

// GetIntVal returns the int value represented by the PdfObject directly or indirectly if contained within an
// indirect object. On type mismatch the found bool flag returned is false and a nil pointer is returned.
func GetIntVal(obj PdfObject) (val int, ok bool) {
	num, ok := TraceToDirectObject(obj).(*PdfObjectInteger)
	if ok && num != nil {
		return int(*num), true
	}
	return 0, false
}

// GetNumbersAsFloat converts a list of pdf objects representing floats or integers to a slice of
// float64 values.
func GetNumbersAsFloat(objects []PdfObject) (floats []float64, err error) {
	for _, object := range objects {
		numberAsFloat, err2 := GetNumberAsFloat(object)
		if err2 != nil {
			return nil, err2
		}
		floats = append(floats, numberAsFloat)
	}
	return floats, nil
}

// GetName returns the *PdfObjectName represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetName(obj PdfObject) (name *PdfObjectName, ok bool) {
	name, ok = TraceToDirectObject(obj).(*PdfObjectName)
	return name, ok
}

// GetBool returns the *PdfObjectBool object that is represented by a PdfObject directly or indirectly
// within an indirect object. The bool flag indicates whether a match was found.
func GetBool(obj PdfObject) (boolObj *PdfObjectBool, ok bool) {
	boolObj, ok = TraceToDirectObject(obj).(*PdfObjectBool)
	return boolObj, ok
}

// GetDict returns the *PdfObjectDictionary represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetDict(obj PdfObject) (dict *PdfObjectDictionary, ok bool) {
	dict, ok = TraceToDirectObject(obj).(*PdfObjectDictionary)
	return dict, ok
}

// GetArray returns the *PdfObjectArray represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetArray(obj PdfObject) (arr *PdfObjectArray, ok bool) {
	arr, ok = TraceToDirectObject(obj).(*PdfObjectArray)
	return arr, ok
}

// GetNameVal returns the string value represented by the PdfObject directly or indirectly if
// contained within an indirect object. On type mismatch the found bool flag returned is false and
// an empty string is returned.
func GetNameVal(obj PdfObject) (val string, ok bool) {
	name, ok := TraceToDirectObject(obj).(*PdfObjectName)
	if ok {
		return string(*name), true
	}
	return
}

// GetNumberAsFloat returns the contents of `obj` as a float if it is an integer or float, or an
// error if it isn't.
func GetNumberAsFloat(obj PdfObject) (float64, error) {
	switch v := obj.(type) {
	case *PdfObjectFloat:
		return float64(*v), nil
	case *PdfObjectInteger:
		return float64(*v), nil
	case *PdfObjectReference:
		toDirectObject := TraceToDirectObject(obj)
		return GetNumberAsFloat(toDirectObject)
	case *PdfIndirectObject:
		return GetNumberAsFloat(v.PdfObject)
	}
	return 0, ErrNotANumber
}

// GetStringVal returns the string value represented by the PdfObject directly or indirectly if
// contained within an indirect object. On type mismatch the found bool flag returned is false and
// an empty string is returned.
func GetStringVal(obj PdfObject) (val string, ok bool) {
	str, ok := TraceToDirectObject(obj).(*PdfObjectString)
	if ok {
		return str.Str(), true
	}
	return
}

// GetFloatVal returns the float64 value represented by the PdfObject directly or indirectly if contained within an
// indirect object. On type mismatch the found bool flag returned is false and a nil pointer is returned.
func GetFloatVal(obj PdfObject) (val float64, ok bool) {
	num, ok := TraceToDirectObject(obj).(*PdfObjectFloat)
	if ok {
		return float64(*num), true
	}
	return 0, false
}

// GetString returns the *PdfObjectString represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetString(obj PdfObject) (str *PdfObjectString, ok bool) {
	str, ok = TraceToDirectObject(obj).(*PdfObjectString)
	return str, ok
}

// GetBoolVal returns the bool value within a *PdObjectBool represented by an PdfObject interface directly or indirectly.
// If the PdfObject does not represent a bool value, a default value of false is returned (found = false also).
func GetBoolVal(obj PdfObject) (val bool, ok bool) {
	boolObj, ok := TraceToDirectObject(obj).(*PdfObjectBool)
	if ok {
		return bool(*boolObj), true
	}
	return false, false
}

// GetInt returns the *PdfObjectBool object that is represented by a PdfObject either directly or indirectly
// within an indirect object. The bool flag indicates whether a match was found.
func GetInt(obj PdfObject) (num *PdfObjectInteger, ok bool) {
	num, ok = TraceToDirectObject(obj).(*PdfObjectInteger)
	return num, ok
}

// GetIndirect returns the *PdfIndirectObject represented by the PdfObject. On type mismatch the found bool flag is
// false and a nil pointer is returned.
func GetIndirect(obj PdfObject) (indirect *PdfIndirectObject, ok bool) {
	obj = ResolveReference(obj)
	indirect, ok = obj.(*PdfIndirectObject)
	return indirect, ok
}

// GetFloat returns the *PdfObjectFloat represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetFloat(obj PdfObject) (num *PdfObjectFloat, ok bool) {
	num, ok = TraceToDirectObject(obj).(*PdfObjectFloat)
	return num, ok
}
