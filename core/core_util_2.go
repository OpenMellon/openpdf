package core

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"sync"

	"github.com/OpenMellon/openpdf/common"
)

// ValidateLinearization checks whether r contains a correctly linearized PDF as
// defined in ISO 32000-2 Annex F. It returns a slice of violations; an empty
// slice means the PDF passes all checks. If the PDF is not linearized it returns
// exactly [LinErrNotLinearized].
//
// The returned error is non-nil only when the file cannot be parsed at all and
// validation could not run; spec violations are reported through the slice.
func ValidateLinearization(r io.ReaderAt, size int64) ([]LinearizationViolation, error) {
	parserAt, err := NewParserAt(r, size)
	if err != nil {
		return nil, err
	}
	return parserAt.validateLinearization()
}

func collectReferencedObjNums(dict *PdfObjectDictionary, keys ...string) []int {
	m := map[string]struct{}{}
	for _, key := range keys {
		m[key] = struct{}{}
	}
	seen := map[int]struct{}{}
	var nums []int
	var visit func(obj PdfObject)
	visit = func(obj PdfObject) {
		switch v := obj.(type) {
		case *PdfObjectReference:
			objNum := int(v.ObjectNumber)
			if _, ok := seen[objNum]; !ok {
				seen[objNum] = struct{}{}
				nums = append(nums, objNum)
			}
		case *PdfObjectArray:
			for _, elem := range v.Elements() {
				visit(elem)
			}
		case *PdfObjectDictionary:
			for _, key := range v.Keys() {
				visit(v.Get(key))
			}
		}
	}
	for _, key := range dict.Keys() {
		if _, ok := m[string(key)]; ok {
			continue
		}
		visit(dict.Get(key))
	}
	return nums
}

var objHeaderRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s+obj")

var stringEscapes = map[byte][]byte{'\n': []byte("\\n"), '\r': []byte("\\r"), '\t': []byte("\\t"), '\b': []byte("\\b"), '\f': []byte("\\f"), '(': []byte("\\("), ')': []byte("\\)"), '\\': []byte("\\\\")}

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

var hexDigits = []byte("0123456789abcdefABCDEF")

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

var numberPattern = regexp.MustCompile("^[\\+-.]*([0-9.]+)")

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

// ResolveReference resolves reference if `o` is a *PdfObjectReference and returns the object referenced to.
// Otherwise returns back `o`.
func ResolveReference(obj PdfObject) PdfObject {
	if ref, ok := obj.(*PdfObjectReference); ok {
		return ref.Resolve()
	}
	return obj
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

// EqualObjects returns true if `obj1` and `obj2` have the same contents.
// It performs a deep recursive comparison, dereferencing indirect objects and
// comparing stream data. Circular references are detected and handled safely.
//
// NOTE: It is a good idea to flatten obj1 and obj2 with FlattenObject before calling this function
// so that contents, rather than references, can be compared.
func EqualObjects(obj1, obj2 PdfObject) bool { return equalObjects(obj1, obj2, 0, nil) }

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

// FlattenObject returns the contents of `obj`. In other words, `obj` with indirect objects replaced
// by their values.
// The replacements are made recursively to a depth of traceMaxDepth.
// NOTE: Dicts are sorted to make objects with same contents have the same PDF object strings.
func FlattenObject(obj PdfObject) PdfObject { return resolveReferencesRecursive(obj, 0) }

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

var pdfVersionRegexp = regexp.MustCompile("%PDF-(\\d)\\.(\\d)")

var exponentialNumberPattern = regexp.MustCompile("^[\\+-.]*([0-9.]+)[eE][\\+-.]*([0-9.]+)")

var referencePattern = regexp.MustCompile("^\\s*[-]*(\\d+)\\s+(\\d+)\\s+R")

func libraryVersion() string { return common.Version }

const minPoolBufSize = poolSize16K / 4

func releaseBuffer(buffer *bytes.Buffer) {
	if buffer.Cap() > poolSize16M {
		return
	}
	buffer.Reset()
	bufferPool.Put(buffer)
}

// ReleaseBytes returns buf to the pool bucket whose capacity matches cap(buf).
// Buffers whose cap does not match any bucket (including nil and empty
// slices) are dropped silently, so this is safe to call with any []byte.
func ReleaseBytes(buf []byte) {
	c := cap(buf)
	if c == 0 {
		return
	}
	for i, bytePoolSize := range bytePoolSizes {
		if c == bytePoolSize {
			data := buf[:c]
			bytePools[i].Put(&data)
			return
		}
	}
}

func poolIndexFor(size int) int {
	if size < minPoolBufSize {
		return -1
	}
	for i, bytePoolSize := range bytePoolSizes {
		if size <= bytePoolSize {
			return i
		}
	}
	return -1
}

const (
	poolSize16K  = 16 << 10
	poolSize64K  = 64 << 10
	poolSize256K = 256 << 10
	poolSize1M   = 1 << 20
	poolSize4M   = 4 << 20
	poolSize16M  = 16 << 20
)

func withPooledBuffer(fn func(*bytes.Buffer)) []byte {
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)
	fn(buffer)
	data := buffer.Bytes()
	out := make([]byte, len(data))
	copy(out, data)
	return out
}

var bufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// UnpooledBytes returns buf, or a copy of it when ReleaseBytes would take
// buf's capacity into the pool. ReleaseBytes never pools the result, so it can
// be shared by values whose owners may each release it.
func UnpooledBytes(buf []byte) []byte {
	if !isPoolSize(cap(buf)) {
		return buf
	}
	size := len(buf)
	for isPoolSize(size) {
		size++
	}
	data := make([]byte, len(buf), size)
	copy(data, buf)
	return data
}

// SizeOfReaderAt auto-detects the byte length of an io.ReaderAt source by
// type-asserting for the common ways a source carries its size:
//   - interface{ Size() int64 } — *bytes.Reader, *io.SectionReader
//   - interface{ Stat() (os.FileInfo, error) } — *os.File
//   - io.Seeker — fallback for other seekable sources
//
// Stat is checked before Seek because *os.File satisfies both, and Stat is
// side-effect-free whereas Seek mutates the file position.
//
// Returns an error if the source supports none of these. Callers with such
// sources should use the explicit-size constructor (e.g. model.NewPdfReaderAt)
// and pass the size from their own knowledge.
func SizeOfReaderAt(r io.ReaderAt) (int64, error) {
	if r == nil {
		return 0, errors.New("core.SizeOfReaderAt: nil source")
	}
	if iface, ok := r.(interface{ Size() int64 }); ok {
		return iface.Size(), nil
	}
	if iface, ok := r.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, err := iface.Stat()
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	if seeker, ok := r.(io.Seeker); ok {
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, err
		}
		if _, err2 := seeker.Seek(0, io.SeekStart); err2 != nil {
			return 0, err2
		}
		return size, nil
	}
	return 0, errors.New("core.SizeOfReaderAt: source does not support Size(), Stat(), or Seek(); use the explicit-size constructor")
}

func sortedXrefObjects(objectMap map[int]XrefObject) []XrefObject {
	count := 0
	for _, xrefObj := range objectMap {
		if xrefObj.Offset > 0 {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	xrefObjs := make([]XrefObject, 0, count)
	for _, xrefObj := range objectMap {
		if xrefObj.Offset > 0 {
			xrefObjs = append(xrefObjs, xrefObj)
		}
	}
	sort.Slice(xrefObjs, func(i, j int) bool { return xrefObjs[i].Offset < xrefObjs[j].Offset })
	return xrefObjs
}

func logXrefTable(table XrefTable) {
	common.Log.Debug("=X=X=X=")
	common.Log.Debug("Xref table:")
	idx := 0
	for _, xrefObj := range table.ObjectMap {
		common.Log.Debug("i+1: %d (obj num: %d gen: %d) -> %d", idx+1, xrefObj.ObjectNumber, xrefObj.Generation, xrefObj.Offset)
		idx++
	}
}

var startxrefRegexp = regexp.MustCompile("startx?ref\\s*(\\d+)")
