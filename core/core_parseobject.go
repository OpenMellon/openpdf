package core

import (
	"errors"
	"io"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
)

// ParseIndirectObjectAt parses an indirect object starting at the given byte
// offset in the source. Returns the indirect object (*PdfIndirectObject) or
// the stream object (*PdfObjectStream).
func (parser *PdfParser) ParseIndirectObjectAt(offset int64) (PdfObject, error) {
	return parser.parseIndirectObject(parser.newCursor(offset), map[int]struct{}{})
}

// ParseDictAt reads and parses a PDF dictionary object enclosed with '<<' and '>>'
// starting at the given offset.
func (parser *PdfParser) ParseDictAt(offset int64) (*PdfObjectDictionary, error) {
	return parser.parseDict(parser.newCursor(offset), 0)
}

func (parser *PdfParser) parseBool(cursor *readerAtCursor) (PdfObjectBool, error) {
	lookahead, err := cursor.Peek(4)
	if err != nil {
		return PdfObjectBool(false), err
	}
	if (len(lookahead) >= 4) && (string(lookahead[:4]) == "true") {
		cursor.Discard(4)
		return PdfObjectBool(true), nil
	}
	lookahead, err = cursor.Peek(5)
	if err != nil {
		return PdfObjectBool(false), err
	}
	if (len(lookahead) >= 5) && (string(lookahead[:5]) == "false") {
		cursor.Discard(5)
		return PdfObjectBool(false), nil
	}
	return PdfObjectBool(false), errors.New("unexpected boolean string")
}

func (parser *PdfParser) parseDict(cursor *readerAtCursor, depth int) (*PdfObjectDictionary, error) {
	if depth >= maxObjectNestingDepth {
		return nil, errObjectNestingTooDeep
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Reading PDF Dict!")
	}
	dictionary := MakeDict()
	dictionary.parser = parser
	opening, _ := cursor.ReadByte()
	if opening != '<' {
		return nil, errors.New("invalid dict")
	}
	opening, _ = cursor.ReadByte()
	if opening != '<' {
		return nil, errors.New("invalid dict")
	}
	var previousKey PdfObjectName
	var pendingName *PdfObjectName
	var previousValue PdfObject
	for {
		parser.skipSpaces(cursor)
		parser.skipComments(cursor)
		lookahead, err := cursor.Peek(2)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Dict peek: %s (% x)!", string(lookahead), string(lookahead))
		}
		if (lookahead[0] == '>') && (lookahead[1] == '>') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF dictionary")
			}
			cursor.ReadByte()
			cursor.ReadByte()
			break
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Parse the name!")
		}
		var key PdfObjectName
		if pendingName != nil && lookahead[0] != '/' {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Dictionary key /%s has no value, dropping it", previousKey)
			}
			if previousValue != nil {
				dictionary.Set(previousKey, previousValue)
			} else {
				dictionary.Remove(previousKey)
			}
			key = *pendingName
		} else {
			key, err = parser.parseName(cursor)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Key: %s", key)
			}
			if err != nil {
				common.Log.Debug("ERROR Returning name err %s", err)
				return nil, err
			}
		}
		pendingName = nil
		if len(key) > 4 && key[len(key)-4:] == "null" {
			trimmedKey := key[0 : len(key)-4]
			common.Log.Debug("Taking care of null bug (%s)", key)
			common.Log.Debug("New key \"%s\" = null", trimmedKey)
			parser.skipSpaces(cursor)
			next, _ := cursor.Peek(1)
			if next[0] == '/' {
				dictionary.Set(trimmedKey, MakeNull())
				continue
			}
		}
		parser.skipSpaces(cursor)
		if closing, _ := cursor.Peek(2); len(closing) == 2 && closing[0] == '>' && closing[1] == '>' {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Dictionary key /%s has no value, dropping it", key)
			}
			continue
		}
		value, err := parser.parseObject(cursor, depth+1)
		if err != nil {
			return nil, err
		}
		if name, ok := value.(*PdfObjectName); ok {
			pendingName = name
			previousValue = dictionary.Get(key)
		}
		dictionary.Set(key, value)
		previousKey = key
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("dict[%s] = %s", key, value.String())
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("returning PDF Dict!")
	}
	return dictionary, nil
}

func (parser *PdfParser) parseArray(cursor *readerAtCursor, depth int) (*PdfObjectArray, error) {
	if depth >= maxObjectNestingDepth {
		return nil, errObjectNestingTooDeep
	}
	array := MakeArray()
	cursor.ReadByte()
	for {
		parser.skipSpaces(cursor)
		next, err := cursor.Peek(1)
		if err != nil {
			return array, err
		}
		if next[0] == ']' {
			cursor.ReadByte()
			break
		}
		element, err := parser.parseObject(cursor, depth+1)
		if err != nil {
			return array, err
		}
		array.Append(element)
	}
	return array, nil
}

func (parser *PdfParser) parseObject(cursor *readerAtCursor, depth int) (PdfObject, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Read direct object")
	}
	parser.skipSpaces(cursor)
	for {
		lookahead, err := cursor.Peek(2)
		if err != nil {
			if err != io.EOF || len(lookahead) == 0 {
				return nil, err
			}
			if len(lookahead) == 1 {
				lookahead = append(lookahead, ' ')
			}
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Peek string: %s", string(lookahead))
		}
		if lookahead[0] == '/' {
			name, err := parser.parseName(cursor)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Name: '%s'", name)
			}
			return &name, err
		} else if lookahead[0] == '(' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->String!")
			}
			value, err := parser.parseString(cursor)
			return value, err
		} else if lookahead[0] == '[' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Array!")
			}
			array, err := parser.parseArray(cursor, depth)
			return array, err
		} else if (lookahead[0] == '<') && (lookahead[1] == '<') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Dict!")
			}
			dictionary, err := parser.parseDict(cursor, depth)
			return dictionary, err
		} else if lookahead[0] == '<' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Hex string!")
			}
			value, err := parser.parseHexString(cursor)
			return value, err
		} else if lookahead[0] == '%' {
			parser.readComment(cursor)
			parser.skipSpaces(cursor)
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Number or ref?")
			}
			lookahead, _ = cursor.Peek(15)
			text := string(lookahead)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Peek str: %s", text)
			}
			if (len(text) > 3) && (text[:4] == "null") {
				value, err := parser.parseNull(cursor)
				return &value, err
			} else if (len(text) > 4) && (text[:5] == "false") {
				value, err := parser.parseBool(cursor)
				return &value, err
			} else if (len(text) > 3) && (text[:4] == "true") {
				value, err := parser.parseBool(cursor)
				return &value, err
			}
			referenceMatch := referencePattern.FindStringSubmatchIndex(text)
			if len(referenceMatch) >= 6 {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("-> !Ref: '%s'", text[:referenceMatch[1]])
				}
				objectNumber, _ := strconv.Atoi(text[referenceMatch[2]:referenceMatch[3]])
				generationNumber, _ := strconv.Atoi(text[referenceMatch[4]:referenceMatch[5]])
				cursor.Discard(referenceMatch[1])
				reference := PdfObjectReference{ObjectNumber: int64(objectNumber), GenerationNumber: int64(generationNumber), parser: parser}
				return &reference, nil
			}
			numberMatch := numberPattern.FindStringSubmatch(text)
			if len(numberMatch) > 1 {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("-> Number!")
				}
				number, err := parser.parseNumber(cursor)
				return number, err
			}
			numberMatch = exponentialNumberPattern.FindStringSubmatch(text)
			if len(numberMatch) > 1 {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("-> Exponential Number!")
				}
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("% s", numberMatch)
				}
				number, err := parser.parseNumber(cursor)
				return number, err
			}
			common.Log.Debug("ERROR Unknown (peek \"%s\")", text)
			return nil, errors.New("object parsing error - unexpected pattern")
		}
	}
}

// ParseIndirectObject parses an indirect object at the start of the source
// (offset 0). Returns the indirect object (*PdfIndirectObject) or the stream
// object (*PdfObjectStream). Convenience wrapper for ParseIndirectObjectAt(0),
// intended for parsers built from a buffer containing a single object. To
// parse at a non-zero offset use ParseIndirectObjectAt(offset); to walk many
// objects sequentially use ParseIndirectObjectsSequential. The parser no
// longer carries an internal cursor, so SetFileOffset-then-ParseIndirectObject
// patterns must be rewritten in offset-explicit form.
func (parser *PdfParser) ParseIndirectObject() (PdfObject, error) {
	return parser.ParseIndirectObjectAt(0)
}

func (parser *PdfParser) parseIndirectObject(cursor *readerAtCursor, visited map[int]struct{}) (PdfObject, error) {
	indirect := PdfIndirectObject{
		PdfObjectReference: PdfObjectReference{parser: parser},
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("-Read indirect obj")
	}
	data, err := cursor.Peek(20)
	if err != nil {
		if err != io.EOF {
			common.Log.Debug("ERROR: Fail to read indirect obj")
			return &indirect, err
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("(indirect obj peek \"%s\"", string(data))
	}
	stringSubmatchIndex := objHeaderRegexp.FindStringSubmatchIndex(string(data))
	if len(stringSubmatchIndex) < 6 {
		if err == io.EOF {
			return nil, err
		}
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(data))
		return &indirect, errors.New("unable to detect indirect object signature")
	}
	cursor.Discard(stringSubmatchIndex[0])
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Offsets % d", stringSubmatchIndex)
	}
	length := stringSubmatchIndex[1] - stringSubmatchIndex[0]
	peeked := make([]byte, length)
	_, err = cursor.ReadAtLeast(peeked, length)
	if err != nil {
		common.Log.Debug("ERROR: unable to read - %s", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("textline: %s", peeked)
	}
	stringSubmatch := objHeaderRegexp.FindStringSubmatch(string(peeked))
	if len(stringSubmatch) < 3 {
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(peeked))
		return &indirect, errors.New("unable to detect indirect object signature")
	}
	objNum, _ := strconv.Atoi(stringSubmatch[1])
	genNum, _ := strconv.Atoi(stringSubmatch[2])
	indirect.ObjectNumber = int64(objNum)
	indirect.GenerationNumber = int64(genNum)
	for {
		afterObj, err2 := cursor.Peek(2)
		if err2 != nil {
			return &indirect, err2
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ind. peek: %s (% x)!", string(afterObj), string(afterObj))
		}
		if IsWhiteSpace(afterObj[0]) {
			parser.skipSpaces(cursor)
		} else if afterObj[0] == '%' {
			parser.skipComments(cursor)
		} else if (afterObj[0] == '<') && (afterObj[1] == '<') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Call parseDict")
			}
			indirect.PdfObject, err2 = parser.parseDict(cursor, 0)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF Call parseDict: %v", err2)
			}
			if err2 != nil {
				return &indirect, err2
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Parsed dictionary... finished.")
			}
		} else if (afterObj[0] == '/') || (afterObj[0] == '(') || (afterObj[0] == '[') || (afterObj[0] == '<') {
			indirect.PdfObject, err2 = parser.parseObject(cursor, 0)
			if err2 != nil {
				return &indirect, err2
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Parsed object ... finished.")
			}
		} else if afterObj[0] == ']' {
			common.Log.Debug("WARNING: ']' character not being used as an array ending marker. Skipping.")
			cursor.Discard(1)
		} else {
			if afterObj[0] == 'e' {
				textLine, err3 := parser.readTextLine(cursor)
				if err3 != nil {
					return nil, err3
				}
				if len(textLine) >= 6 && textLine[0:6] == "endobj" {
					break
				}
			} else if afterObj[0] == 's' {
				afterObj, _ = cursor.Peek(10)
				if string(afterObj[:6]) == "stream" {
					skip := 6
					if len(afterObj) > 6 {
						if IsWhiteSpace(afterObj[skip]) && afterObj[skip] != '\r' && afterObj[skip] != '\n' {
							common.Log.Debug("Non-conformant PDF not ending stream line properly with EOL marker")
							parser.metadata.nonConformantStream = true
							skip++
						}
						switch afterObj[skip] {
						case '\r':
							skip++
							if afterObj[skip] == '\n' {
								skip++
							}
						case '\n':
							skip++
						default:
							parser.metadata.nonConformantStream = true
						}
					}
					cursor.Discard(skip)
					dict, ok := indirect.PdfObject.(*PdfObjectDictionary)
					if !ok {
						return nil, errors.New("stream object missing dictionary")
					}
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Stream dict %s", dict)
					}
					streamLength, err3 := parser.traceStreamLength(dict.Get("Length"), visited)
					if err3 != nil {
						common.Log.Debug("Fail to trace stream length: %v", err3)
						return nil, err3
					}
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Stream length? %s", streamLength)
					}
					num, isNum := streamLength.(*PdfObjectInteger)
					if !isNum {
						return nil, errors.New("stream length needs to be an integer")
					}
					length2 := *num
					if length2 < 0 {
						return nil, errors.New("stream needs to be longer than 0")
					}
					fileOffset := cursor.GetFileOffset()
					nextOffset := parser.xrefNextObjectOffset(fileOffset)
					if fileOffset+int64(length2) > nextOffset && nextOffset > fileOffset {
						common.Log.Debug("Expected ending at %d", fileOffset+int64(length2))
						common.Log.Debug("Next object starting at %d", nextOffset)
						corrected := nextOffset - fileOffset - 17
						if corrected < 0 {
							return nil, errors.New("invalid stream length, going past boundaries")
						}
						common.Log.Debug("Attempting a length correction to %d...", corrected)
						length2 = PdfObjectInteger(corrected)
						dict.Set("Length", MakeInteger(corrected))
					}
					if int64(length2) > parser.fileSize {
						common.Log.Debug("ERROR: Stream length cannot be larger than file size")
						return nil, errors.New("invalid stream length, larger than file size")
					}
					streamData := make([]byte, length2)
					_, err3 = cursor.ReadAtLeast(streamData, int(length2))
					if err3 != nil {
						common.Log.Debug("ERROR stream (%d): %X", len(streamData), streamData)
						common.Log.Debug("ERROR: %v", err3)
						return nil, err3
					}
					stream := PdfObjectStream{
						Stream:              streamData,
						PdfObjectDictionary: indirect.PdfObject.(*PdfObjectDictionary),
						PdfObjectReference: PdfObjectReference{
							ObjectNumber:     indirect.ObjectNumber,
							GenerationNumber: indirect.GenerationNumber,
						},
					}
					stream.PdfObjectReference.parser = parser
					parser.skipSpaces(cursor)
					cursor.Discard(9)
					parser.skipSpaces(cursor)
					return &stream, nil
				}
			}
			if indirect.PdfObject != nil && afterObj[0] >= '0' && afterObj[0] <= '9' {
				tail := afterObj
				if len(afterObj) < 20 {
					tail, _ = cursor.Peek(20)
				}
				if objHeaderRegexp.Match(tail) {
					common.Log.Debug("WARNING: Missing endobj marker, detected new object starting")
					break
				}
			}
			indirect.PdfObject, err2 = parser.parseObject(cursor, 0)
			if indirect.PdfObject == nil {
				common.Log.Debug("INCOMPATIBILITY: Indirect object not containing an object - assuming null object")
				indirect.PdfObject = MakeNull()
			}
			return &indirect, err2
		}
	}
	if indirect.PdfObject == nil {
		common.Log.Debug("INCOMPATIBILITY: Indirect object not containing an object - assuming null object")
		indirect.PdfObject = MakeNull()
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Returning indirect!")
	}
	return &indirect, nil
}

func (parser *PdfParser) parseNull(cursor *readerAtCursor) (PdfObjectNull, error) {
	_, err := cursor.Discard(4)
	return PdfObjectNull{}, err
}

// ParseDict reads and parses a PDF dictionary object enclosed with '<<' and
// '>>' at the start of the source (offset 0). Convenience wrapper for
// ParseDictAt(0), intended for parsers built from a buffer where the dict
// is the entire content (e.g. NewParserFromString-based tests). Callers that
// need to parse at a different offset must use ParseDictAt(offset); the
// parser no longer carries an internal cursor for SetFileOffset-then-ParseDict
// patterns to work against.
func (parser *PdfParser) ParseDict() (*PdfObjectDictionary, error) { return parser.ParseDictAt(0) }
