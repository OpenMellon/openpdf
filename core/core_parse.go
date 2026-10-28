package core

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	"golang.org/x/xerrors"
)

func parseReferenceText(text string) (PdfObjectReference, error) {
	reference := PdfObjectReference{}
	matches := referencePattern.FindStringSubmatch(text)
	if len(matches) < 3 {
		common.Log.Debug("Error parsing reference")
		return reference, errors.New("unable to parse reference")
	}
	objectNumber, _ := strconv.Atoi(matches[1])
	generationNumber, _ := strconv.Atoi(matches[2])
	reference.ObjectNumber = int64(objectNumber)
	reference.GenerationNumber = int64(generationNumber)
	return reference, nil
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
				value, err := strconv.ParseUint(string(digits), 8, 32)
				if err != nil {
					return MakeString(buffer.String()), err
				}
				buffer.WriteByte(byte(value))
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

var numberPattern = regexp.MustCompile("^[\\+-.]*([0-9.]+)")

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

// IsFloatDigit checks if a character can be a part of a float number string.
func IsFloatDigit(c byte) bool { return ('0' <= c && c <= '9') || c == '.' }

var pdfVersionRegexp = regexp.MustCompile("%PDF-(\\d)\\.(\\d)")

// Version represents a version of a PDF standard.
type Version struct {
	Major int
	Minor int
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

// IsDecimalDigit checks if the character is a part of a decimal number string.
func IsDecimalDigit(c byte) bool { return '0' <= c && c <= '9' }

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

var (
	ErrUnsupportedEncodingParameters = errors.New("unsupported encoding parameters")
	ErrNoCCITTFaxDecode              = errors.New("CCITTFaxDecode encoding is not yet implemented")
	ErrNoJBIG2Decode                 = errors.New("JBIG2Decode encoding is not yet implemented")
	ErrNoJPXDecode                   = errors.New("JPXDecode encoding is not yet implemented")
	ErrNoPdfVersion                  = errors.New("version not found")
	ErrTypeError                     = errors.New("type check error")
	ErrRangeError                    = errors.New("range check error")
	ErrNotSupported                  = xerrors.New("feature not currently supported")
	ErrNotANumber                    = errors.New("not a number")
	ErrRevisionBoundaryNotFound      = errors.New("revision boundary not found")
	ErrDecodedSizeLimitExceeded      = errors.New("decoded stream size exceeds limit")
)

// IsOctalDigit checks if a character can be part of an octal digit string.
func IsOctalDigit(c byte) bool { return '0' <= c && c <= '7' }

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

var exponentialNumberPattern = regexp.MustCompile("^[\\+-.]*([0-9.]+)[eE][\\+-.]*([0-9.]+)")

// IsWhiteSpace checks if byte represents a white space character.
func IsWhiteSpace(ch byte) bool {
	if (ch == 0x00) || (ch == 0x09) || (ch == 0x0A) || (ch == 0x0C) || (ch == 0x0D) || (ch == 0x20) {
		return true
	}
	return false
}

var errObjectNestingTooDeep = errors.New("arrays and dictionaries nested too deeply")

var referencePattern = regexp.MustCompile("^\\s*[-]*(\\d+)\\s+(\\d+)\\s+R")

const maxObjectNestingDepth = 500

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

// String returns the PDF version as a string. Implements interface fmt.Stringer.
func (version Version) String() string { return fmt.Sprintf("%0d.%0d", version.Major, version.Minor) }

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
func libraryVersion() string { return common.Version }

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

// IsDelimiter checks if a character represents a delimiter.
func IsDelimiter(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}

// ParseNumber parses a numeric objects from a buffered stream.
// Section 7.3.3.
// Integer or Float.
//
// An integer shall be written as one or more decimal digits optionally
// preceded by a sign. The value shall be interpreted as a signed
// decimal integer and shall be converted to an integer object.
//
// A real value shall be written as one or more decimal digits with an
// optional sign and a leading, trailing, or embedded PERIOD (2Eh)
// (decimal point). The value shall be interpreted as a real number
// and shall be converted to a real object.
//
// Regarding exponential numbers: 7.3.3 Numeric Objects:
// A conforming writer shall not use the PostScript syntax for numbers
// with non-decimal radices (such as 16#FFFE) or in exponential format
// (such as 6.02E23).
// Nonetheless, we sometimes get numbers with exponential format, so
// we will support it in the reader (no confusion with other types, so
// no compromise).
func ParseNumber(buf *bufio.Reader) (PdfObject, error) {
	ok := false
	ok2 := true
	var numBuf bytes.Buffer
	for {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Parsing number \"%s\"", numBuf.String())
		}
		data, err := buf.Peek(1)
		if err == io.EOF {
			break
		}
		if err != nil {
			common.Log.Debug("ERROR %s", err)
			return nil, err
		}
		if ok2 && (data[0] == '-' || data[0] == '+') {
			b, _ := buf.ReadByte()
			numBuf.WriteByte(b)
			ok2 = false
		} else if IsDecimalDigit(data[0]) {
			b, _ := buf.ReadByte()
			numBuf.WriteByte(b)
		} else if data[0] == '.' {
			b, _ := buf.ReadByte()
			numBuf.WriteByte(b)
			ok = true
		} else if data[0] == 'e' || data[0] == 'E' {
			b, _ := buf.ReadByte()
			numBuf.WriteByte(b)
			ok = true
			ok2 = true
		} else {
			break
		}
	}
	var obj PdfObject
	if ok {
		float, err := strconv.ParseFloat(numBuf.String(), 64)
		if err != nil {
			common.Log.Debug("Error parsing number %v err=%v. Using 0.0. Output may be incorrect", numBuf.String(), err)
			float = 0.0
		}
		num := PdfObjectFloat(float)
		obj = &num
	} else {
		val, err := strconv.ParseInt(numBuf.String(), 10, 64)
		if err != nil {
			common.Log.Debug("Error parsing number %v err=%v. Using 0. Output may be incorrect", numBuf.String(), err)
			val = 0
		}
		num := PdfObjectInteger(val)
		obj = &num
	}
	return obj, nil
}
