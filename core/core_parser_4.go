package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
)

func (parser *PdfParser) readComment(cursor *readerAtCursor) (string, error) {
	var buffer bytes.Buffer
	_, err := parser.skipSpaces(cursor)
	if err != nil {
		return buffer.String(), err
	}
	first := true
	for {
		next, err := cursor.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return buffer.String(), err
		}
		if first && next[0] != '%' {
			return buffer.String(), errors.New("comment should start with %")
		}
		first = false
		if (next[0] != '\r') && (next[0] != '\n') {
			value, _ := cursor.ReadByte()
			buffer.WriteByte(value)
		} else {
			break
		}
	}
	return buffer.String(), nil
}

// PdfVersion returns version of the PDF file.
func (parser *PdfParser) PdfVersion() Version { return parser.version }

func (parser *PdfParser) parseDetailedHeader() (err error) {
	cursor := parser.newCursor(0)
	winLen := 20
	data := make([]byte, winLen)
	var (
		ok  bool
		pos int
	)
	for {
		b, err2 := cursor.ReadByte()
		if err2 != nil {
			if err2 == io.EOF {
				break
			} else {
				return err2
			}
		}
		if IsDecimalDigit(b) && data[winLen-1] == '.' && IsDecimalDigit(data[winLen-2]) && data[winLen-3] == '-' && data[winLen-4] == 'F' && data[winLen-5] == 'D' && data[winLen-6] == 'P' && data[winLen-7] == '%' {
			parser.version = Version{Major: int(data[winLen-2] - '0'), Minor: int(b - '0')}
			parser.metadata.headerPosition = pos - 7
			ok = true
			break
		}
		pos++
		data = append(data[1:winLen], b)
	}
	if !ok {
		return fmt.Errorf("no file header found")
	}
	b, err := cursor.ReadByte()
	if err == io.EOF {
		return fmt.Errorf("not a valid Pdf document")
	}
	if err != nil {
		return err
	}
	parser.metadata.eolAfterHeader = b == '\n'
	b, err = cursor.ReadByte()
	if err != nil {
		return fmt.Errorf("not a valid pdf document: %w", err)
	}
	if b != '%' {
		return nil
	}
	commentBytes := make([]byte, 4)
	_, err = cursor.Read(commentBytes)
	if err != nil {
		return fmt.Errorf("not a valid pdf document: %w", err)
	}
	parser.metadata.headerCommentBytes = [4]byte{commentBytes[0], commentBytes[1], commentBytes[2], commentBytes[3]}
	return nil
}

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
