package core

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
)

func (parser *PdfParser) traceStreamLength(obj PdfObject, visited map[int]struct{}) (PdfObject, error) {
	internal, err := parser.resolveInternal(obj, visited)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Stream length? %s", internal)
	}
	return internal, nil
}

func (parser *PdfParser) resolveReference(ref *PdfObjectReference) (PdfObject, bool, error) {
	if cachedObject, ok := parser.LoadCachedObject(int(ref.ObjectNumber)); ok {
		return cachedObject, true, nil
	}
	byReference, err := parser.LookupByReference(*ref)
	if err != nil {
		return nil, false, err
	}
	parser.StoreCachedObject(int(ref.ObjectNumber), byReference)
	return byReference, false, nil
}

const maxTraceDepth = 10

// LookupByReference looks up a PdfObject by a reference.
func (parser *PdfParser) LookupByReference(ref PdfObjectReference) (PdfObject, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Looking up reference %s", ref.String())
	}
	return parser.LookupByNumber(int(ref.ObjectNumber))
}

func (parser *PdfParser) lookupObjectViaOS(osObjNumber int, index int, visited map[int]struct{}) (PdfObject, error) {
	var objStm objectStream
	var ok bool
	parser.objStreamMu.Lock()
	objStm, ok = parser.objStreams[osObjNumber]
	parser.objStreamMu.Unlock()
	if !ok {
		byNumberWrapperInternal, _, err := parser.lookupByNumberWrapperInternal(osObjNumber, true, visited)
		if err != nil {
			common.Log.Debug("Missing object stream with number %d", osObjNumber)
			return nil, err
		}
		stream, isStream := byNumberWrapperInternal.(*PdfObjectStream)
		if !isStream {
			return nil, errors.New("invalid object stream")
		}
		if parser.crypter != nil && !parser.crypter.isDecrypted(stream) {
			return nil, errors.New("need to decrypt the stream")
		}
		pdfObjectDictionary := stream.PdfObjectDictionary
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("so d: %s\n", pdfObjectDictionary.String())
		}
		typeName, isStream := pdfObjectDictionary.Get("Type").(*PdfObjectName)
		if !isStream {
			common.Log.Debug("ERROR: Object stream should always have a Type")
			return nil, errors.New("object stream missing Type")
		}
		if strings.ToLower(string(*typeName)) != "objstm" {
			common.Log.Debug("ERROR: Object stream type shall always be ObjStm !")
			return nil, errors.New("object stream type != ObjStm")
		}
		N, isStream := pdfObjectDictionary.Get("N").(*PdfObjectInteger)
		if !isStream {
			return nil, errors.New("invalid N in stream dictionary")
		}
		first, isStream := pdfObjectDictionary.Get("First").(*PdfObjectInteger)
		if !isStream {
			return nil, errors.New("invalid First in stream dictionary")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("type: %s number of objects: %d", typeName, *N)
		}
		data, err := DecodeStream(stream)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decoded: %s", data)
		}
		readerAtCursor := newReaderAtCursor(bytes.NewReader(data), int64(len(data)), 0)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Parsing offset map")
		}
		m := map[int]int64{}
		for i := 0; i < int(*N); i++ {
			parser.skipSpaces(readerAtCursor)
			number, err2 := parser.parseNumber(readerAtCursor)
			if err2 != nil {
				return nil, err2
			}
			num, isNum := number.(*PdfObjectInteger)
			if !isNum {
				return nil, errors.New("invalid object stream offset table")
			}
			parser.skipSpaces(readerAtCursor)
			number, err2 = parser.parseNumber(readerAtCursor)
			if err2 != nil {
				return nil, err2
			}
			relOffset, isNum := number.(*PdfObjectInteger)
			if !isNum {
				return nil, errors.New("invalid object stream offset table")
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("obj %d offset %d", *num, *relOffset)
			}
			m[int(*num)] = int64(*first + *relOffset)
		}
		objStm = objectStream{N: int(*N), data: data, offsets: m}
		parser.objStreamMu.Lock()
		parser.objStreams[osObjNumber] = objStm
		parser.objStreamMu.Unlock()
	}
	offset := objStm.offsets[index]
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ACTUAL offset[%d] = %d", index, offset)
	}
	readerAtCursor := newReaderAtCursor(bytes.NewReader(objStm.data), int64(len(objStm.data)), offset)
	data, _ := readerAtCursor.Peek(100)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("OBJ peek \"%s\"", string(data))
	}
	object, err := parser.parseObject(readerAtCursor, 0)
	if err != nil {
		common.Log.Debug("ERROR Fail to read object (%s)", err)
		return nil, err
	}
	if object == nil {
		return nil, errors.New("object cannot be null")
	}
	indirect := PdfIndirectObject{
		PdfObjectReference: PdfObjectReference{ObjectNumber: int64(index), parser: parser},
		PdfObject:          object,
	}
	return &indirect, nil
}

func (parser *PdfParser) lookupByNumber(objNum int, retry bool, visited map[int]struct{}) (PdfObject, bool, error) {
	if cachedObject, ok := parser.LoadCachedObject(objNum); ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Returning cached object %d", objNum)
		}
		return cachedObject, false, nil
	}
	xrefObj, ok := parser.currentXrefs().ObjectMap[objNum]
	if !ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Unable to locate object in xrefs! - Returning null object")
		}
		var null PdfObjectNull
		return &null, false, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Lookup obj number %d", objNum)
	}
	switch xrefObj.XType {
	case XrefTypeTableEntry:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("xrefobj obj num %d", xrefObj.ObjectNumber)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("xrefobj gen %d", xrefObj.Generation)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("xrefobj offset %d", xrefObj.Offset)
		}
		cursor := parser.newCursor(xrefObj.Offset)
		indirectObject, err := parser.parseIndirectObject(cursor, visited)
		if err != nil {
			common.Log.Debug("ERROR Failed reading xref (%s)", err)
			if retry && !errors.Is(err, errObjectNestingTooDeep) {
				common.Log.Debug("Attempting to repair xrefs (top down)")
				if err2 := parser.rebuildXrefTableTopDown(); err2 != nil {
					return nil, false, err2
				}
				return parser.lookupByNumber(objNum, false, visited)
			}
			return nil, false, err
		}
		if retry {
			num, _, _ := indirectObjectIDs(indirectObject)
			if int(num) != objNum {
				common.Log.Debug("Invalid xrefs: Rebuilding")
				err2 := parser.rebuildXrefTable(visited)
				if err2 != nil {
					return nil, false, err2
				}
				return parser.lookupByNumber(objNum, false, visited)
			}
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Returning obj")
		}
		parser.StoreCachedObject(objNum, indirectObject)
		return indirectObject, false, nil
	case XrefTypeObjectStream:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("xref from object stream!")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace(">Load via OS!")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Object stream available in object %d/%d", xrefObj.OsObjNumber, xrefObj.OsObjIndex)
		}
		if xrefObj.OsObjNumber == objNum {
			common.Log.Debug("ERROR Circular reference!?!")
			return nil, true, errors.New("xref circular reference")
		}
		if _, ok2 := parser.currentXrefs().ObjectMap[xrefObj.OsObjNumber]; ok2 {
			objectViaOS, err := parser.lookupObjectViaOS(xrefObj.OsObjNumber, objNum, visited)
			if err != nil {
				common.Log.Debug("ERROR Returning ERR (%s)", err)
				return nil, true, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("<Loaded via OS")
			}
			parser.StoreCachedObject(objNum, objectViaOS)
			if parser.crypter != nil {
				parser.crypter.decryptedObjects[objectViaOS] = true
			}
			return objectViaOS, true, nil
		}
		common.Log.Debug("?? Belongs to a non-cross referenced object ...!")
		return nil, true, errors.New("os belongs to a non cross referenced object")
	}
	return nil, false, errors.New("unknown xref type")
}

func (parser *PdfParser) objectFileOffset(objNum int) (int64, bool) {
	xrefObj, ok := parser.currentXrefs().ObjectMap[objNum]
	if !ok || xrefObj.XType != XrefTypeTableEntry {
		return 0, false
	}
	return xrefObj.Offset, true
}

func parseObjSignature(line string) (int, int, error) {
	stringSubmatch := objHeaderRegexp.FindStringSubmatch(line)
	if len(stringSubmatch) < 3 {
		return 0, 0, errors.New("unable to detect indirect object signature")
	}
	objNum, _ := strconv.Atoi(stringSubmatch[1])
	genNum, _ := strconv.Atoi(stringSubmatch[2])
	return objNum, genNum, nil
}

type lookupResult struct {
	obj       PdfObject
	decrypted bool
}

func (parser *PdfParser) lookupByNumberWrapperInternal(objNumber int, retry bool, visited map[int]struct{}) (PdfObject, bool, error) {
	if cachedObject, ok := parser.LoadCachedObject(objNumber); ok {
		return cachedObject, false, nil
	}
	if _, ok := visited[objNumber]; ok {
		common.Log.Debug("ERROR: Lookup of %d is already in progress on this call stack — recursive lookup attempt blocked", objNumber)
		return nil, false, errors.New("recursive lookup attempt blocked")
	}
	visited[objNumber] = struct{}{}
	defer delete(visited, objNumber)
	res, err, _ := parser.lookupGroup.Do(strconv.Itoa(objNumber), func() (any, error) {
		if cachedObject, ok := parser.LoadCachedObject(objNumber); ok {
			return lookupResult{obj: cachedObject, decrypted: false}, nil
		}
		byNumber, ok, err2 := parser.lookupByNumber(objNumber, retry, visited)
		if err2 != nil {
			return nil, err2
		}
		if !ok && parser.crypter != nil && parser.crypter.authenticated && !parser.crypter.isDecrypted(byNumber) {
			if err3 := parser.crypter.Decrypt(byNumber, 0, 0); err3 != nil {
				return nil, err3
			}
		}
		return lookupResult{obj: byNumber, decrypted: ok}, nil
	})
	if err != nil {
		return nil, false, err
	}
	result := res.(lookupResult)
	return result.obj, result.decrypted, nil
}

var objHeaderRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s+obj")

// ParseIndirectObjectsSequential reads indirect objects sequentially from
// the parser's source starting at startOffset and invokes fn for each one.
// Stops when fn returns an error, on io.EOF, or on any other parse error.
func (parser *PdfParser) ParseIndirectObjectsSequential(startOffset int64, fn func(obj PdfObject) error) error {
	cursor := parser.newCursor(startOffset)
	m := map[int]struct{}{}
	for {
		indirectObject, err := parser.parseIndirectObject(cursor, m)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err2 := fn(indirectObject); err2 != nil {
			return err2
		}
	}
}

func (parser *PdfParser) resolveInternal(obj PdfObject, visited map[int]struct{}) (PdfObject, error) {
	ref, ok := obj.(*PdfObjectReference)
	if !ok {
		return obj, nil
	}
	byNumberWrapperInternal, _, err := parser.lookupByNumberWrapperInternal(int(ref.ObjectNumber), true, visited)
	if err != nil {
		return nil, err
	}
	indirect, isIndirect := byNumberWrapperInternal.(*PdfIndirectObject)
	if !isIndirect {
		return byNumberWrapperInternal, nil
	}
	byNumberWrapperInternal = indirect.PdfObject
	_, ok = byNumberWrapperInternal.(*PdfObjectReference)
	if ok {
		return indirect, errors.New("multi depth trace pointer to pointer")
	}
	return byNumberWrapperInternal, nil
}

func indirectObjectIDs(obj PdfObject) (int64, int64, error) {
	if indirect, ok := obj.(*PdfIndirectObject); ok {
		return indirect.ObjectNumber, indirect.GenerationNumber, nil
	}
	if stream, ok := obj.(*PdfObjectStream); ok {
		return stream.ObjectNumber, stream.GenerationNumber, nil
	}
	return 0, 0, errors.New("not an indirect/stream object")
}

// Resolve resolves a PdfObject to direct object, looking up and resolving references as needed (unlike TraceToDirect).
func (parser *PdfParser) Resolve(obj PdfObject) (PdfObject, error) {
	return parser.resolveInternal(obj, map[int]struct{}{})
}

// LookupByNumber looks up a PdfObject by object number. Returns an error on failure.
// Safe for concurrent use; cache hits are lock-free, cache misses are deduped
// via singleflight (one goroutine parses, others wait and share the result).
func (parser *PdfParser) LookupByNumber(objNumber int) (PdfObject, error) {
	byNumberWrapperInternal, _, err := parser.lookupByNumberWrapperInternal(objNumber, true, map[int]struct{}{})
	return byNumberWrapperInternal, err
}
