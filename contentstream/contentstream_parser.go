package contentstream

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
	"github.com/OpenMellon/openpdf/core"
)

func (p *ContentStreamParser) parseBool() (core.PdfObjectBool, error) {
	buf, err := p.reader.Peek(4)
	if err != nil {
		return core.PdfObjectBool(false), err
	}
	if (len(buf) >= 4) && (string(buf[:4]) == "true") {
		p.reader.Discard(4)
		return core.PdfObjectBool(true), nil
	}
	buf, err = p.reader.Peek(5)
	if err != nil {
		return core.PdfObjectBool(false), err
	}
	if (len(buf) >= 5) && (string(buf[:5]) == "false") {
		p.reader.Discard(5)
		return core.PdfObjectBool(false), nil
	}
	return core.PdfObjectBool(false), errors.New("unexpected boolean string")
}

func (p *ContentStreamParser) parseObject() (obj core.PdfObject, ok bool, err error) {
	p.skipSpaces()
	for {
		buf, err2 := p.reader.Peek(2)
		if err2 != nil {
			return nil, false, err2
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Peek string: %s", string(buf))
		}
		if buf[0] == '%' {
			p.skipComments()
			continue
		} else if buf[0] == '/' {
			name, err3 := p.parseName()
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Name: '%s'", name)
			}
			return &name, false, err3
		} else if buf[0] == '(' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->String!")
			}
			str, err3 := p.parseString()
			return str, false, err3
		} else if buf[0] == '<' && buf[1] != '<' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Hex String!")
			}
			str, err3 := p.parseHexString()
			return str, false, err3
		} else if buf[0] == '[' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Array!")
			}
			arr, err3 := p.parseArray()
			return arr, false, err3
		} else if core.IsFloatDigit(buf[0]) || (buf[0] == '-' && core.IsFloatDigit(buf[1])) || (buf[0] == '+' && core.IsFloatDigit(buf[1])) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Number!")
			}
			obj2, err3 := p.parseNumber()
			return obj2, false, err3
		} else if buf[0] == '<' && buf[1] == '<' {
			dict, err3 := p.parseDict()
			return dict, false, err3
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Operand or bool?")
			}
			buf, _ = p.reader.Peek(5)
			s := string(buf)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("cont Peek str: %s", s)
			}
			if (len(s) > 3) && (s[:4] == "null") {
				v, err3 := p.parseNull()
				return &v, false, err3
			} else if (len(s) > 4) && (s[:5] == "false") {
				v, err3 := p.parseBool()
				return &v, false, err3
			} else if (len(s) > 3) && (s[:4] == "true") {
				v, err3 := p.parseBool()
				return &v, false, err3
			}
			str, err3 := p.parseOperand()
			if err3 != nil {
				return str, false, err3
			}
			if len(str.String()) < 1 {
				return str, false, ErrInvalidOperand
			}
			return str, true, nil
		}
	}
}

func (p *ContentStreamParser) parseString() (*core.PdfObjectString, error) {
	p.reader.ReadByte()
	var buf []byte
	n := 1
	for {
		buf2, err := p.reader.Peek(1)
		if err != nil {
			return core.MakeString(string(buf)), err
		}
		if buf2[0] == '\\' {
			p.reader.ReadByte()
			b, err2 := p.reader.ReadByte()
			if err2 != nil {
				return core.MakeString(string(buf)), err2
			}
			if core.IsOctalDigit(b) {
				buf3, err3 := p.reader.Peek(2)
				if err3 != nil {
					return core.MakeString(string(buf)), err3
				}
				var buf4 []byte
				buf4 = append(buf4, b)
				for _, b2 := range buf3 {
					if core.IsOctalDigit(b2) {
						buf4 = append(buf4, b2)
					} else {
						break
					}
				}
				p.reader.Discard(len(buf4) - 1)
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Numeric string \"%s\"", buf4)
				}
				v, err3 := strconv.ParseUint(string(buf4), 8, 16)
				if err3 != nil {
					return core.MakeString(string(buf)), err3
				}
				buf = append(buf, byte(v&0xff))
				continue
			}
			switch b {
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case '(':
				buf = append(buf, '(')
			case ')':
				buf = append(buf, ')')
			case '\\':
				buf = append(buf, '\\')
			}
			continue
		} else if buf2[0] == '(' {
			n++
		} else if buf2[0] == ')' {
			n--
			if n == 0 {
				p.reader.ReadByte()
				break
			}
		}
		b, _ := p.reader.ReadByte()
		buf = append(buf, b)
	}
	return core.MakeString(string(buf)), nil
}

func (p *ContentStreamParser) parseOperand() (*core.PdfObjectString, error) {
	var buf []byte
	for {
		buf2, err := p.reader.Peek(1)
		if err != nil {
			return core.MakeString(string(buf)), err
		}
		if core.IsDelimiter(buf2[0]) {
			break
		}
		if core.IsWhiteSpace(buf2[0]) {
			break
		}
		b, _ := p.reader.ReadByte()
		buf = append(buf, b)
	}
	return core.MakeString(string(buf)), nil
}

func (p *ContentStreamParser) parseNumber() (core.PdfObject, error) {
	return core.ParseNumber(p.reader)
}

func (p *ContentStreamParser) parseNull() (core.PdfObjectNull, error) {
	_, err := p.reader.Discard(4)
	return core.PdfObjectNull{}, err
}

func (p *ContentStreamParser) parseArray() (*core.PdfObjectArray, error) {
	arr := core.MakeArray()
	p.reader.ReadByte()
	for {
		p.skipSpaces()
		buf, err := p.reader.Peek(1)
		if err != nil {
			return arr, err
		}
		if buf[0] == ']' {
			p.reader.ReadByte()
			break
		}
		obj, _, err := p.parseObject()
		if err != nil {
			return arr, err
		}
		arr.Append(obj)
	}
	return arr, nil
}

func (p *ContentStreamParser) parseHexString() (*core.PdfObjectString, error) {
	p.reader.ReadByte()
	buf := []byte("0123456789abcdefABCDEF")
	var buf2 []byte
	for {
		p.skipSpaces()
		buf3, err := p.reader.Peek(1)
		if err != nil {
			return core.MakeString(""), err
		}
		if buf3[0] == '>' {
			p.reader.ReadByte()
			break
		}
		b, _ := p.reader.ReadByte()
		if bytes.IndexByte(buf, b) >= 0 {
			buf2 = append(buf2, b)
		}
	}
	if len(buf2)%2 == 1 {
		buf2 = append(buf2, '0')
	}
	buf3, _ := hex.DecodeString(string(buf2))
	return core.MakeHexString(string(buf3)), nil
}

func (p *ContentStreamParser) skipComments() error {
	if _, err := p.skipSpaces(); err != nil {
		return err
	}
	ok := true
	for {
		buf, err := p.reader.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return err
		}
		if ok && buf[0] != '%' {
			return nil
		}
		ok = false
		if (buf[0] != '\r') && (buf[0] != '\n') {
			p.reader.ReadByte()
		} else {
			break
		}
	}
	return p.skipComments()
}

func (p *ContentStreamParser) skipSpaces() (int, error) {
	n := 0
	for {
		buf, err := p.reader.Peek(1)
		if err != nil {
			return 0, err
		}
		if core.IsWhiteSpace(buf[0]) {
			p.reader.ReadByte()
			n++
		} else {
			break
		}
	}
	return n, nil
}

func (p *ContentStreamParser) parseDict() (*core.PdfObjectDictionary, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Reading content stream dict!")
	}
	dict := core.MakeDict()
	b, _ := p.reader.ReadByte()
	if b != '<' {
		return nil, errors.New("invalid dict")
	}
	b, _ = p.reader.ReadByte()
	if b != '<' {
		return nil, errors.New("invalid dict")
	}
	for {
		p.skipSpaces()
		buf, err := p.reader.Peek(2)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Dict peek: %s (% x)!", string(buf), string(buf))
		}
		if (buf[0] == '>') && (buf[1] == '>') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF dictionary")
			}
			p.reader.ReadByte()
			p.reader.ReadByte()
			break
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Parse the name!")
		}
		name, err := p.parseName()
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Key: %s", name)
		}
		if err != nil {
			common.Log.Debug("ERROR Returning name err %s", err)
			return nil, err
		}
		if len(name) > 4 && name[len(name)-4:] == "null" {
			name2 := name[0 : len(name)-4]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Taking care of null bug (%s)", name)
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("New key \"%s\" = null", name2)
			}
			p.skipSpaces()
			buf2, _ := p.reader.Peek(1)
			if buf2[0] == '/' {
				dict.Set(name2, core.MakeNull())
				continue
			}
		}
		p.skipSpaces()
		obj, _, err := p.parseObject()
		if err != nil {
			return nil, err
		}
		dict.Set(name, obj)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("dict[%s] = %s", name, obj.String())
		}
	}
	return dict, nil
}

func (p *ContentStreamParser) parseName() (core.PdfObjectName, error) {
	s := ""
	ok := false
	for {
		buf, err := p.reader.Peek(1)
		if err == io.EOF {
			break
		}
		if err != nil {
			return core.PdfObjectName(s), err
		}
		if !ok {
			if buf[0] == '/' {
				ok = true
				p.reader.ReadByte()
			} else {
				common.Log.Error("Name starting with %s (% x)", buf, buf)
				return core.PdfObjectName(s), fmt.Errorf("invalid name: (%c)", buf[0])
			}
		} else {
			if core.IsWhiteSpace(buf[0]) {
				break
			} else if (buf[0] == '/') || (buf[0] == '[') || (buf[0] == '(') || (buf[0] == ']') || (buf[0] == '<') || (buf[0] == '>') {
				break
			} else if buf[0] == '#' {
				buf2, err2 := p.reader.Peek(3)
				if err2 != nil {
					return core.PdfObjectName(s), err2
				}
				p.reader.Discard(3)
				buf3, err2 := hex.DecodeString(string(buf2[1:3]))
				if err2 != nil {
					return core.PdfObjectName(s), err2
				}
				s += string(buf3)
			} else {
				b, _ := p.reader.ReadByte()
				s += string(b)
			}
		}
	}
	return core.PdfObjectName(s), nil
}

// ContentStreamParser represents a content stream parser for parsing content streams in PDFs.
type ContentStreamParser struct{ reader *bufio.Reader }

// Parse parses all commands in content stream, returning a list of operation data.
func (p *ContentStreamParser) Parse() (*ContentStreamOperations, error) {
	v := ContentStreamOperations{}
	for {
		op := ContentStreamOperation{}
		for {
			obj, ok, err := p.parseObject()
			if err != nil {
				if err == io.EOF {
					return &v, nil
				}
				return &v, err
			}
			if ok {
				op.Operand, _ = core.GetStringVal(obj)
				v = append(v, &op)
				break
			} else {
				op.Params = append(op.Params, obj)
			}
		}
		if op.Operand == "BI" {
			img, err := p.ParseInlineImage()
			if err != nil {
				return &v, err
			}
			op.Params = append(op.Params, img)
		}
	}
}

// NewContentStreamParser creates a new instance of the content stream parser from an input content
// stream string.
func NewContentStreamParser(contentStr string) *ContentStreamParser {
	v := ContentStreamParser{}
	contentStr = string(leadingSlashesRegexp.ReplaceAll([]byte(contentStr), []byte("/")))
	buf := bytes.NewBufferString(contentStr + "\n")
	v.reader = bufio.NewReader(buf)
	return &v
}

var leadingSlashesRegexp = regexp.MustCompile("^/{2,}")

// ParseInlineImage parses an inline image from a content stream, both reading its properties and binary data.
// When called, "BI" has already been read from the stream.  This function
// finishes reading through "EI" and then returns the ContentStreamInlineImage.
func (p *ContentStreamParser) ParseInlineImage() (*ContentStreamInlineImage, error) {
	v := ContentStreamInlineImage{}
	for {
		p.skipSpaces()
		obj, ok, err := p.parseObject()
		if err != nil {
			return nil, err
		}
		if !ok {
			name, ok2 := core.GetName(obj)
			if !ok2 {
				common.Log.Debug("Invalid inline image property (expecting name) - %T", obj)
				return nil, fmt.Errorf("invalid inline image property (expecting name) - %T", obj)
			}
			obj2, ok3, err2 := p.parseObject()
			if err2 != nil {
				return nil, err2
			}
			if ok3 {
				return nil, fmt.Errorf("not expecting an operand")
			}
			switch *name {
			case "BPC", "BitsPerComponent":
				v.BitsPerComponent = obj2
			case "CS", "ColorSpace":
				v.ColorSpace = obj2
			case "D", "Decode":
				v.Decode = obj2
			case "DP", "DecodeParms":
				v.DecodeParms = obj2
			case "F", "Filter":
				v.Filter = obj2
			case "H", "Height":
				v.Height = obj2
			case "IM", "ImageMask":
				v.ImageMask = obj2
			case "Intent":
				v.Intent = obj2
			case "I", "Interpolate":
				v.Interpolate = obj2
			case "W", "Width":
				v.Width = obj2
			case "Length", "L", "Subtype", "Type":
				common.Log.Debug("Ignoring inline parameter %s", *name)
			default:
				return nil, fmt.Errorf("unknown inline image parameter %s", *name)
			}
		}
		if ok {
			str, ok2 := obj.(*core.PdfObjectString)
			if !ok2 {
				return nil, fmt.Errorf("failed to read inline image - invalid operand")
			}
			if str.Str() == "EI" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Inline image finished...")
				}
				return &v, nil
			} else if str.Str() == "ID" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("ID start")
				}
				buf, err2 := p.reader.Peek(1)
				if err2 != nil {
					return nil, err2
				}
				if core.IsWhiteSpace(buf[0]) {
					p.reader.Discard(1)
				}
				v.data = []byte{}
				n := 0
				var buf2 []byte
				for {
					b, err3 := p.reader.ReadByte()
					if err3 != nil {
						common.Log.Debug("Unable to find end of image EI in inline image data")
						return nil, err3
					}
					if n == 0 {
						if core.IsWhiteSpace(b) {
							buf2 = []byte{}
							buf2 = append(buf2, b)
							n = 1
						} else if b == 'E' {
							buf2 = append(buf2, b)
							n = 2
						} else {
							v.data = append(v.data, b)
						}
					} else if n == 1 {
						buf2 = append(buf2, b)
						if b == 'E' {
							n = 2
						} else {
							v.data = append(v.data, buf2...)
							buf2 = []byte{}
							if core.IsWhiteSpace(b) {
								n = 1
							} else {
								n = 0
							}
						}
					} else if n == 2 {
						buf2 = append(buf2, b)
						if b == 'I' {
							n = 3
						} else {
							v.data = append(v.data, buf2...)
							buf2 = []byte{}
							n = 0
						}
					} else if n == 3 {
						buf2 = append(buf2, b)
						if core.IsWhiteSpace(b) {
							buf3, err4 := p.reader.Peek(20)
							if err4 != nil && err4 != io.EOF {
								return nil, err4
							}
							p2 := NewContentStreamParser(string(buf3))
							ok3 := true
							for n2 := 0; n2 < 3; n2++ {
								obj2, ok4, err5 := p2.parseObject()
								if err5 != nil {
									if err5 == io.EOF {
										break
									}
									ok3 = false
									continue
								}
								if ok4 && !isKnownOperand(obj2.String()) {
									ok3 = false
									break
								}
							}
							if ok3 {
								if len(v.data) > 100 {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Image stream (%d): % x ...", len(v.data), v.data[:100])
									}
								} else {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Image stream (%d): % x", len(v.data), v.data)
									}
								}
								return &v, nil
							}
						}
						v.data = append(v.data, buf2...)
						buf2 = []byte{}
						n = 0
					}
				}
			}
		}
	}
}
