package core

import (
	"bytes"
	"maps"
	"strings"
	"sync"

	"github.com/OpenMellon/openpdf/common"
)

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

// MakeDict creates and returns an empty PdfObjectDictionary.
func MakeDict() *PdfObjectDictionary {
	dict := &PdfObjectDictionary{}
	dict.dict = map[PdfObjectName]PdfObject{}
	dict.keys = []PdfObjectName{}
	return dict
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

// PdfObjectDictionary represents the primitive PDF dictionary/map object.
type PdfObjectDictionary struct {
	dict   map[PdfObjectName]PdfObject
	keys   []PdfObjectName
	mu     sync.RWMutex
	parser *PdfParser
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

// MakeDictMap creates a PdfObjectDictionary initialized from a map of keys to values.
func MakeDictMap(objmap map[string]PdfObject) *PdfObjectDictionary {
	dict := MakeDict()
	return dict.Update(objmap)
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
