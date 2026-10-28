package core

import (
	"bytes"
	"errors"
	"maps"
	"reflect"
	"slices"

	"github.com/OpenMellon/openpdf/common"
)

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

// IsNullObject returns true if `obj` is a PdfObjectNull.
func IsNullObject(obj PdfObject) bool {
	_, ok := TraceToDirectObject(obj).(*PdfObjectNull)
	return ok
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

func resolveReferencesRecursive(pdfObject PdfObject, depth int) PdfObject {
	if depth > maxTraceDepth {
		common.Log.Error("Trace depth level beyond %d - error!", maxTraceDepth)
		return MakeNull()
	}
	switch v := pdfObject.(type) {
	case *PdfIndirectObject:
		pdfObject = resolveReferencesRecursive((*v).PdfObject, depth+1)
	case *PdfObjectArray:
		for i, elem := range (*v).elements {
			(*v).elements[i] = resolveReferencesRecursive(elem, depth+1)
		}
	case *PdfObjectDictionary:
		v.mu.RLock()
		m := make(map[PdfObjectName]PdfObject, len(v.dict))
		maps.Copy(m, v.dict)
		v.mu.RUnlock()
		for nameKey, obj := range m {
			m[nameKey] = resolveReferencesRecursive(obj, depth+1)
		}
		v.mu.Lock()
		maps.Copy(v.dict, m)
		slices.Sort(v.keys)
		v.mu.Unlock()
	}
	return pdfObject
}

func equalObjects(pdfObjectDictionary, other PdfObject, depth int, seen map[indirectPair]bool) bool {
	if depth > maxTraceDepth {
		common.Log.Error("Trace depth level beyond %d - error!", maxTraceDepth)
		return false
	}
	if pdfObjectDictionary == nil && other == nil {
		return true
	} else if pdfObjectDictionary == nil || other == nil {
		return false
	}
	if reflect.TypeOf(pdfObjectDictionary) != reflect.TypeOf(other) {
		return false
	}
	switch v := pdfObjectDictionary.(type) {
	case *PdfObjectNull:
		return true
	case *PdfObjectReference:
		ref := other.(*PdfObjectReference)
		return v.ObjectNumber == ref.ObjectNumber && v.GenerationNumber == ref.GenerationNumber
	case *PdfObjectName:
		return *v == *other.(*PdfObjectName)
	case *PdfObjectString:
		return *v == *other.(*PdfObjectString)
	case *PdfObjectInteger:
		return *v == *other.(*PdfObjectInteger)
	case *PdfObjectBool:
		return *v == *other.(*PdfObjectBool)
	case *PdfObjectFloat:
		return *v == *other.(*PdfObjectFloat)
	case *PdfIndirectObject:
		if seen == nil {
			seen = map[indirectPair]bool{}
		}
		indirect := other.(*PdfIndirectObject)
		pair := indirectPair{v, indirect}
		if seen[pair] {
			return true
		}
		seen[pair] = true
		return equalObjects(TraceToDirectObject(pdfObjectDictionary), TraceToDirectObject(other), depth+1, seen)
	case *PdfObjectArray:
		arr := other.(*PdfObjectArray)
		if len((*v).elements) != len((*arr).elements) {
			return false
		}
		for i, elem := range (*v).elements {
			if !equalObjects(elem, (*arr).elements[i], depth+1, seen) {
				return false
			}
		}
		return true
	case *PdfObjectDictionary:
		dict := other.(*PdfObjectDictionary)
		names := v.Keys()
		otherNames := dict.Keys()
		if len(names) != len(otherNames) {
			return false
		}
		for _, name := range names {
			obj := v.Get(name)
			otherVal := dict.Get(name)
			if otherVal == nil || !equalObjects(obj, otherVal, depth+1, seen) {
				return false
			}
		}
		return true
	case *PdfObjectStream:
		stream := other.(*PdfObjectStream)
		if !bytes.Equal(v.Stream, stream.Stream) {
			return false
		}
		return equalObjects(v.PdfObjectDictionary, stream.PdfObjectDictionary, depth+1, seen)
	case *PdfObjectStreams:
		streams := other.(*PdfObjectStreams)
		if len(v.vec) != len(streams.vec) {
			return false
		}
		for i, obj := range v.vec {
			if !equalObjects(obj, streams.vec[i], depth+1, seen) {
				return false
			}
		}
		return true
	default:
		common.Log.Error("ERROR: Unknown type: %T - should never happen!", pdfObjectDictionary)
	}
	return false
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

// ResolveReferencesDeep traverses through object `o` and everything it references, looking up and replacing
// references with indirect objects.
// Optionally a map of already deep-resolved objects can be provided via `traversed`. The `traversed` map
// is updated while traversing the objects to avoid traversing same objects multiple times.
func ResolveReferencesDeep(o PdfObject, traversed map[PdfObject]struct{}) error {
	if traversed == nil {
		traversed = map[PdfObject]struct{}{}
	}
	return resolveReferencesIterative(o, traversed)
}

// ShallowEqualObjects returns true if `obj1` and `obj2` are equal at the top level
// without recursing into nested objects. Indirect objects are compared by object number
// (identity), not by content. Dictionary and array values are compared by pointer equality.
func ShallowEqualObjects(obj1, obj2 PdfObject) bool {
	if obj1 == nil && obj2 == nil {
		return true
	} else if obj1 == nil || obj2 == nil {
		return false
	}
	if reflect.TypeOf(obj1) != reflect.TypeOf(obj2) {
		return false
	}
	switch v := obj1.(type) {
	case *PdfObjectNull:
		return true
	case *PdfObjectReference:
		ref := obj2.(*PdfObjectReference)
		return v.ObjectNumber == ref.ObjectNumber && v.GenerationNumber == ref.GenerationNumber
	case *PdfObjectName:
		return *v == *obj2.(*PdfObjectName)
	case *PdfObjectString:
		return *v == *obj2.(*PdfObjectString)
	case *PdfObjectInteger:
		return *v == *obj2.(*PdfObjectInteger)
	case *PdfObjectBool:
		return *v == *obj2.(*PdfObjectBool)
	case *PdfObjectFloat:
		return *v == *obj2.(*PdfObjectFloat)
	case *PdfIndirectObject:
		indirect := obj2.(*PdfIndirectObject)
		return v.ObjectNumber == indirect.ObjectNumber && v.GenerationNumber == indirect.GenerationNumber
	case *PdfObjectArray:
		arr := obj2.(*PdfObjectArray)
		if len(v.elements) != len(arr.elements) {
			return false
		}
		for i, elem := range v.elements {
			if elem != arr.elements[i] {
				return false
			}
		}
		return true
	case *PdfObjectDictionary:
		dict := obj2.(*PdfObjectDictionary)
		names := v.Keys()
		otherKeys := dict.Keys()
		if len(names) != len(otherKeys) {
			return false
		}
		for _, name := range names {
			obj := v.Get(name)
			otherVal := dict.Get(name)
			if obj != otherVal {
				return false
			}
		}
		return true
	case *PdfObjectStream:
		stream := obj2.(*PdfObjectStream)
		if v.PdfObjectDictionary != stream.PdfObjectDictionary {
			return false
		}
		return bytes.Equal(v.Stream, stream.Stream)
	default:
		common.Log.Error("ERROR: Unknown type: %T - should never happen!", obj1)
	}
	return false
}

// ResolveReference resolves reference if `o` is a *PdfObjectReference and returns the object referenced to.
// Otherwise returns back `o`.
func ResolveReference(obj PdfObject) PdfObject {
	if ref, ok := obj.(*PdfObjectReference); ok {
		return ref.Resolve()
	}
	return obj
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

// EqualObjects returns true if `obj1` and `obj2` have the same contents.
// It performs a deep recursive comparison, dereferencing indirect objects and
// comparing stream data. Circular references are detected and handled safely.
//
// NOTE: It is a good idea to flatten obj1 and obj2 with FlattenObject before calling this function
// so that contents, rather than references, can be compared.
func EqualObjects(obj1, obj2 PdfObject) bool { return equalObjects(obj1, obj2, 0, nil) }

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

// FlattenObject returns the contents of `obj`. In other words, `obj` with indirect objects replaced
// by their values.
// The replacements are made recursively to a depth of traceMaxDepth.
// NOTE: Dicts are sorted to make objects with same contents have the same PDF object strings.
func FlattenObject(obj PdfObject) PdfObject { return resolveReferencesRecursive(obj, 0) }

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

func equalIDObjects(a, b PdfObject) bool {
	a = TraceToDirectObject(a)
	b = TraceToDirectObject(b)
	switch v := a.(type) {
	case *PdfObjectString:
		str, ok := b.(*PdfObjectString)
		return ok && v.Str() == str.Str()
	case *PdfObjectArray:
		arr, ok := b.(*PdfObjectArray)
		if !ok || v.Len() != arr.Len() {
			return false
		}
		for i := 0; i < v.Len(); i++ {
			if !equalIDObjects(v.Get(i), arr.Get(i)) {
				return false
			}
		}
		return true
	default:
		if a == nil || b == nil {
			return a == b
		}
		return string(a.Write()) == string(b.Write())
	}
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

// TraceToDirectObject traces a PdfObject to a direct object.  For example direct objects contained
// in indirect objects (can be double referenced even).
func TraceToDirectObject(obj PdfObject) PdfObject {
	if ref, ok := obj.(*PdfObjectReference); ok {
		obj = ref.Resolve()
	}
	indirect, ok := obj.(*PdfIndirectObject)
	depth := 0
	for ok {
		obj = indirect.PdfObject
		indirect, ok = GetIndirect(obj)
		depth++
		if depth > maxTraceDepth {
			common.Log.Error("ERROR: Trace depth level beyond %d - not going deeper!", maxTraceDepth)
			return nil
		}
	}
	return obj
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

func resolveReferencesIterative(obj PdfObject, visited map[PdfObject]struct{}) error {
	objs := []PdfObject{obj}
	for len(objs) > 0 {
		last := objs[len(objs)-1]
		objs = objs[:len(objs)-1]
		if _, ok := visited[last]; ok {
			continue
		}
		visited[last] = struct{}{}
		switch v := last.(type) {
		case *PdfObjectReference:
			common.Log.Debug("ERROR: Tracing a reference!")
			return errors.New("error tracing a reference")
		case *PdfIndirectObject:
			objs = append(objs, v.PdfObject)
		case *PdfObjectStream:
			objs = append(objs, v.PdfObjectDictionary)
		case *PdfObjectDictionary:
			names := v.Keys()
			for _, name := range slices.Backward(names) {
				child := v.Get(name)
				if ref, ok := child.(*PdfObjectReference); ok {
					child = ref.Resolve()
					v.Set(name, child)
				}
				objs = append(objs, child)
			}
		case *PdfObjectArray:
			elems := v.Elements()
			for i, elem := range slices.Backward(elems) {

				if ref, ok := elem.(*PdfObjectReference); ok {
					elem = ref.Resolve()
					v.Set(i, elem)
				}
				objs = append(objs, elem)
			}
		}
	}
	return nil
}

// GetFloat returns the *PdfObjectFloat represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetFloat(obj PdfObject) (num *PdfObjectFloat, ok bool) {
	num, ok = TraceToDirectObject(obj).(*PdfObjectFloat)
	return num, ok
}
