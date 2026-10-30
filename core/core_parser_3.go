package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/intconv"
)

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
		for i := 0; i < intconv.ClampInt(int64(*N)); i++ {
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
			m[intconv.ClampInt(int64(*num))] = int64(*first + *relOffset)
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

func (parser *PdfParser) parseString(cursor *readerAtCursor) (*PdfObjectString, error) {
	cursor.ReadByte()
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)
	depth := 1
	for {
		next, err := cursor.Peek(1)
		if err != nil {
			return MakeString(buffer.String()), err
		}
		if next[0] == '\\' {
			cursor.ReadByte()
			escaped, err := cursor.ReadByte()
			if err != nil {
				return MakeString(buffer.String()), err
			}
			if IsOctalDigit(escaped) {
				lookahead, err := cursor.Peek(2)
				if err != nil {
					return MakeString(buffer.String()), err
				}
				var digits []byte
				digits = append(digits, escaped)
				for _, digit := range lookahead {
					if IsOctalDigit(digit) {
						digits = append(digits, digit)
					} else {
						break
					}
				}
				cursor.Discard(len(digits) - 1)
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Numeric string \"%s\"", digits)
				}
				value, err := strconv.ParseUint(string(digits), 8, 16)
				if err != nil {
					return MakeString(buffer.String()), err
				}
				buffer.WriteByte(byte(value & 0xff))
				continue
			}
			switch escaped {
			case 'n':
				buffer.WriteRune('\n')
			case 'r':
				buffer.WriteRune('\r')
			case 't':
				buffer.WriteRune('\t')
			case 'b':
				buffer.WriteRune('\b')
			case 'f':
				buffer.WriteRune('\f')
			case '(':
				buffer.WriteRune('(')
			case ')':
				buffer.WriteRune(')')
			case '\\':
				buffer.WriteRune('\\')
			}
			continue
		} else if next[0] == '(' {
			depth++
		} else if next[0] == ')' {
			depth--
			if depth == 0 {
				cursor.ReadByte()
				break
			}
		}
		value, _ := cursor.ReadByte()
		buffer.WriteByte(value)
	}
	return MakeString(buffer.String()), nil
}

func (parser *PdfParser) parseName(cursor *readerAtCursor) (PdfObjectName, error) {
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)
	started := false
	for {
		next, err := cursor.Peek(1)
		if err == io.EOF {
			break
		}
		if err != nil {
			return PdfObjectName(buffer.String()), err
		}
		if !started {
			switch next[0] {
			case '/':
				started = true
				cursor.ReadByte()
			case '%':
				parser.readComment(cursor)
				parser.skipSpaces(cursor)
			default:
				common.Log.Debug("ERROR Name starting with %s (% x)", next, next)
				return PdfObjectName(buffer.String()), fmt.Errorf("invalid name: (%c)", next[0])
			}
		} else {
			if IsWhiteSpace(next[0]) {
				break
			} else if (next[0] == '/') || (next[0] == '[') || (next[0] == '(') || (next[0] == ']') || (next[0] == '<') || (next[0] == '>') {
				break
			} else if next[0] == '#' {
				escape, err := cursor.Peek(3)
				if err != nil {
					return PdfObjectName(buffer.String()), err
				}
				var decoded [1]byte
				_, err = hex.Decode(decoded[:], escape[1:3])
				if err != nil {
					common.Log.Debug("ERROR: Invalid hex following '#', continuing using literal - Output may be incorrect")
					buffer.WriteByte('#')
					cursor.Discard(1)
					continue
				}
				cursor.Discard(3)
				buffer.WriteByte(decoded[0])
			} else {
				value, _ := cursor.ReadByte()
				buffer.WriteByte(value)
			}
		}
	}
	return PdfObjectName(buffer.String()), nil
}

func (parser *PdfParser) readTextLine(cursor *readerAtCursor) (string, error) {
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)
	for {
		data, err := cursor.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return buffer.String(), err
		}
		if (data[0] != '\r') && (data[0] != '\n') {
			b, _ := cursor.ReadByte()
			buffer.WriteByte(b)
		} else {
			break
		}
	}
	return buffer.String(), nil
}

func (parser *PdfParser) parseHexString(cursor *readerAtCursor) (*PdfObjectString, error) {
	cursor.ReadByte()
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)
	for {
		next, err := cursor.Peek(1)
		if err != nil {
			return MakeString(""), err
		}
		if next[0] == '>' {
			cursor.ReadByte()
			break
		}
		value, _ := cursor.ReadByte()
		if parser.compliance {
			if bytes.IndexByte(hexDigits, value) == -1 {
				parser.metadata.invalidHexRunes = true
			}
		}
		if !IsWhiteSpace(value) {
			buffer.WriteByte(value)
		}
	}
	if buffer.Len()%2 == 1 {
		parser.metadata.oddLengthHexStrings = true
		buffer.WriteRune('0')
	}
	decoded, _ := hex.DecodeString(buffer.String())
	return MakeHexString(string(decoded)), nil
}

func (parser *PdfParser) seekPdfVersionTopDown(cursor *readerAtCursor) (int, int, error) {
	cursor.SetFileOffset(0)
	probeLen := 20
	data := make([]byte, probeLen)
	for {
		b, err := cursor.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return 0, 0, err
			}
		}
		if IsDecimalDigit(b) && data[probeLen-1] == '.' && IsDecimalDigit(data[probeLen-2]) && data[probeLen-3] == '-' && data[probeLen-4] == 'F' && data[probeLen-5] == 'D' && data[probeLen-6] == 'P' {
			major := int(data[probeLen-2] - '0')
			minor := int(b - '0')
			return major, minor, nil
		}
		data = append(data[1:probeLen], b)
	}
	return 0, 0, errors.New("version not found")
}

func (parser *PdfParser) skipComments(cursor *readerAtCursor) error {
	if _, err := parser.skipSpaces(cursor); err != nil {
		return err
	}
	first := true
	for {
		next, err := cursor.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return err
		}
		if first && next[0] != '%' {
			return nil
		}
		first = false
		if (next[0] != '\r') && (next[0] != '\n') {
			cursor.ReadByte()
		} else {
			break
		}
	}
	return parser.skipComments(cursor)
}

func (parser *PdfParser) parseNumber(cursor *readerAtCursor) (PdfObject, error) {
	return ParseNumber(cursor.Underlying())
}

func (parser *PdfParser) skipSpaces(cursor *readerAtCursor) (int, error) {
	count := 0
	for {
		value, err := cursor.ReadByte()
		if err != nil {
			return 0, err
		}
		if IsWhiteSpace(value) {
			count++
		} else {
			cursor.UnreadByte()
			break
		}
	}
	return count, nil
}

func (parser *PdfParser) parsePdfVersion() (int, int, error) {
	cursor := parser.newCursor(0)
	var probeLen int64 = 20
	data := make([]byte, probeLen)
	cursor.Read(data)
	var err error
	var major, minor int
	if stringSubmatch := pdfVersionRegexp.FindStringSubmatch(string(data)); len(stringSubmatch) < 3 {
		if major, minor, err = parser.seekPdfVersionTopDown(cursor); err != nil {
			common.Log.Debug("Failed recovery - unable to find version")
			return 0, 0, err
		}
		headerOffset := cursor.GetFileOffset() - 8
		if headerOffset > 0 {
			parser.reader = newOffsetReaderAt(parser.reader, headerOffset)
			parser.fileSize -= headerOffset
		}
	} else {
		if major, err = strconv.Atoi(stringSubmatch[1]); err != nil {
			return 0, 0, err
		}
		if minor, err = strconv.Atoi(stringSubmatch[2]); err != nil {
			return 0, 0, err
		}
	}
	common.Log.Debug("Pdf version %d.%d", major, minor)
	return major, minor, nil
}
