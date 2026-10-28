// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package fdf provides support for loading form field data from Forms Data Format (FDF) files
// and for exporting the field data of a PDF form to an FDF file.
package fdf

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func normalizeTextString(s *core.PdfObjectString) *core.PdfObjectString {
	b := s.Bytes()
	if hasUTF16BOM(s) {
		return s
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return core.MakeEncodedString(string(b[3:]), true)
	}
	hasHigh, needsEncoding := false, false
	for _, c := range b {
		switch {
		case c >= 0x80:
			hasHigh, needsEncoding = true, true
		case c >= 0x18 && c <= 0x1F:
			needsEncoding = true
		}
	}
	if !needsEncoding {
		return s
	}
	if hasHigh && utf8.Valid(b) {
		return core.MakeEncodedString(string(b), true)
	}
	return core.MakeEncodedString(s.Decoded(), true)
}

var (
	reReference  = regexp.MustCompile("^\\s*(\\d+)\\s+(\\d+)\\s+R")
	reNumber     = regexp.MustCompile("^[\\+-.]*([0-9.]+)")
	reExponent   = regexp.MustCompile("^[\\+-.]*([0-9.]+)e[\\+-.]*([0-9.]+)")
	reFdfVersion = regexp.MustCompile("%FDF-(\\d)\\.(\\d)")
)

func (p *fdfParser) parseName() (core.PdfObjectName, error) {
	var buf bytes.Buffer
	started := false
	for {
		peek, err := p.reader.Peek(1)
		if err == io.EOF {
			break
		}
		if err != nil {
			return core.PdfObjectName(buf.String()), err
		}
		if !started {
			switch peek[0] {
			case '/':
				started = true
				p.reader.ReadByte()
			case '%':
				p.readComment()
				p.skipSpaces()
			default:
				common.Log.Debug("ERROR Name starting with %s (% x)", peek, peek)
				return core.PdfObjectName(buf.String()), fmt.Errorf("invalid name: (%c)", peek[0])
			}
		} else {
			if core.IsWhiteSpace(peek[0]) {
				break
			} else if (peek[0] == '/') || (peek[0] == '[') || (peek[0] == '(') || (peek[0] == ']') || (peek[0] == '<') || (peek[0] == '>') {
				break
			} else if peek[0] == '#' {
				hexBytes, err := p.reader.Peek(3)
				if err != nil {
					return core.PdfObjectName(buf.String()), err
				}
				p.reader.Discard(3)
				decoded, err := hex.DecodeString(string(hexBytes[1:3]))
				if err != nil {
					return core.PdfObjectName(buf.String()), err
				}
				buf.Write(decoded)
			} else {
				c, _ := p.reader.ReadByte()
				buf.WriteByte(c)
			}
		}
	}
	return core.PdfObjectName(buf.String()), nil
}

// NewFromReader builds FDF form data from the AcroForm of `reader`, see
// NewFromForm. The document ID of the source PDF, when present, is recorded
// as the /ID entry of the FDF dictionary so importing applications can match
// the data to the form. Use SetSourceFile to record the form's path (/F).
func NewFromReader(reader *model.PdfReader) (*Data, error) {
	if reader == nil {
		return nil, errors.New("reader is nil")
	}
	if reader.AcroForm == nil {
		return nil, errors.New("pdf has no AcroForm")
	}
	data, err := NewFromForm(reader.AcroForm)
	if err != nil {
		return nil, err
	}
	if trailer, trailerErr := reader.GetTrailer(); trailerErr == nil {
		if docID := copyDocumentID(trailer); docID != nil {
			data.fdfDict.Set("ID", docID)
		}
	}
	return data, nil
}

func (p *fdfParser) parseObject() (core.PdfObject, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Read direct object")
	}
	p.skipSpaces()
	for {
		peek, err := p.reader.Peek(2)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Peek string: %s", string(peek))
		}
		if peek[0] == '/' {
			name, err := p.parseName()
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Name: '%s'", name)
			}
			return &name, err
		} else if peek[0] == '(' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->String!")
			}
			return p.parseString()
		} else if peek[0] == '[' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Array!")
			}
			return p.parseArray()
		} else if (peek[0] == '<') && (peek[1] == '<') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Dict!")
			}
			return p.parseDict()
		} else if peek[0] == '<' {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Hex string!")
			}
			return p.parseHexString()
		} else if peek[0] == '%' {
			p.readComment()
			p.skipSpaces()
		} else {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("->Number or ref?")
			}
			peek, _ = p.reader.Peek(15)
			peekStr := string(peek)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Peek str: %s", peekStr)
			}
			if (len(peekStr) > 3) && (peekStr[:4] == "null") {
				null, err := p.parseNull()
				return &null, err
			} else if (len(peekStr) > 4) && (peekStr[:5] == "false") {
				b, err := p.parseBool()
				return &b, err
			} else if (len(peekStr) > 3) && (peekStr[:4] == "true") {
				b, err := p.parseBool()
				return &b, err
			}
			refMatch := reReference.FindStringSubmatch(peekStr)
			if len(refMatch) > 1 {
				peek, _ = p.reader.ReadBytes('R')
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("-> !Ref: '%s'", string(peek[:]))
				}
				ref, err := parseReference(string(peek))
				return &ref, err
			}
			numMatch := reNumber.FindStringSubmatch(peekStr)
			if len(numMatch) > 1 {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("-> Number!")
				}
				return p.parseNumber()
			}
			numMatch = reExponent.FindStringSubmatch(peekStr)
			if len(numMatch) > 1 {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("-> Exponential Number!")
				}
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("% s", numMatch)
				}
				return p.parseNumber()
			}
			common.Log.Debug("ERROR Unknown (peek \"%s\")", peekStr)
			return nil, errors.New("object parsing error - unexpected pattern")
		}
	}
}

func (p *fdfParser) skipSpaces() (int, error) {
	n := 0
	for {
		c, err := p.reader.ReadByte()
		if err != nil {
			return 0, err
		}
		if core.IsWhiteSpace(c) {
			n++
		} else {
			p.reader.UnreadByte()
			break
		}
	}
	return n, nil
}

func (p *fdfParser) parse() error {
	p.rs.Seek(0, io.SeekStart)
	p.reader = bufio.NewReader(p.rs)
	for {
		p.skipComments()
		peek, err := p.reader.Peek(20)
		if err != nil {
			common.Log.Debug("ERROR: Fail to read indirect obj")
			return err
		}
		if strings.HasPrefix(string(peek), "trailer") {
			p.reader.Discard(7)
			p.skipSpaces()
			p.skipComments()
			trailer, _ := p.parseDict()
			p.trailer = trailer
			break
		}
		loc := reIndirectObj.FindStringSubmatchIndex(string(peek))
		if len(loc) < 6 {
			common.Log.Debug("ERROR: Unable to find object signature (%s)", string(peek))
			return errors.New("unable to detect indirect object signature")
		}
		obj, err := p.parseIndirectObject()
		if err != nil {
			return err
		}
		switch o := obj.(type) {
		case *core.PdfIndirectObject:
			p.objects[o.ObjectNumber] = o
		case *core.PdfObjectStream:
			p.objects[o.ObjectNumber] = o
		default:
			return errors.New("type error")
		}
	}
	return nil
}

func (p *fdfParser) seekFdfVersionTopDown() (int, int, error) {
	p.rs.Seek(0, io.SeekStart)
	p.reader = bufio.NewReader(p.rs)
	bufLen := 20
	window := make([]byte, bufLen)
	for {
		c, err := p.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return 0, 0, err
			}
		}
		if core.IsDecimalDigit(c) && window[bufLen-1] == '.' && core.IsDecimalDigit(window[bufLen-2]) && window[bufLen-3] == '-' && window[bufLen-4] == 'F' && window[bufLen-5] == 'D' && window[bufLen-6] == 'P' {
			major := int(window[bufLen-2] - '0')
			minor := int(c - '0')
			return major, minor, nil
		}
		window = append(window[1:bufLen], c)
	}
	return 0, 0, errors.New("version not found")
}

func parseReference(s string) (core.PdfObjectReference, error) {
	ref := core.PdfObjectReference{}
	m := reReference.FindStringSubmatch(s)
	if len(m) < 3 {
		common.Log.Debug("Error parsing reference")
		return ref, errors.New("unable to parse reference")
	}
	objNum, err := strconv.Atoi(m[1])
	if err != nil {
		common.Log.Debug("Error parsing object number '%s' - Using object num = 0", m[1])
		return ref, nil
	}
	ref.ObjectNumber = int64(objNum)
	genNum, err := strconv.Atoi(m[2])
	if err != nil {
		common.Log.Debug("Error parsing generation number '%s' - Using gen = 0", m[2])
		return ref, nil
	}
	ref.GenerationNumber = int64(genNum)
	return ref, nil
}
func (p *fdfParser) parseNumber() (core.PdfObject, error) { return core.ParseNumber(p.reader) }

// WriteToFile serialises the FDF form data to the file at `fdfPath`, see
// Write. An existing file (a symbolic link's target, if `fdfPath` is one) is
// replaced only once the new content has been written completely, and keeps
// its permissions. When no temporary file can be created beside it, the
// file is rewritten in place.
func (fdf *Data) WriteToFile(fdfPath string) error {
	data, err := fdf.serialize()
	if err != nil {
		return err
	}
	path := fdfPath
	if resolved, symErr := filepath.EvalSymlinks(fdfPath); symErr == nil {
		path = resolved
	}
	info, err := os.Stat(path)
	if err != nil {
		return writeFileDirect(path, data)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return writeFileDirect(path, data)
	}
	tmpName := tmp.Name()
	if _, writeErr := tmp.Write(data); writeErr != nil {
		tmp.Close()
		os.Remove(tmpName)
		return writeErr
	}
	if closeErr := tmp.Close(); closeErr != nil {
		os.Remove(tmpName)
		return closeErr
	}
	if chmodErr := os.Chmod(tmpName, info.Mode().Perm()); chmodErr != nil {
		os.Remove(tmpName)
		return chmodErr
	}
	if renameErr := os.Rename(tmpName, path); renameErr != nil {
		os.Remove(tmpName)
		return renameErr
	}
	return nil
}

// Data represents forms data format (FDF) file data.
type Data struct {
	fdfDict      *core.PdfObjectDictionary
	fields       *core.PdfObjectArray
	objects      map[int64]core.PdfObject
	catalogDict  *core.PdfObjectDictionary
	rootObjNum   int64
	rootGenNum   int64
	versionMajor int
	versionMinor int
}

func (p *fdfParser) readAtLeast(buf []byte, n int) (int, error) {
	remaining := n
	total := 0
	reads := 0
	for remaining > 0 {
		nr, err := p.reader.Read(buf[total:])
		if err != nil {
			common.Log.Debug("ERROR Failed reading (%d;%d) %s", nr, reads, err.Error())
			return total, errors.New("failed reading")
		}
		reads++
		total += nr
		remaining -= nr
	}
	return total, nil
}

// FieldDictionaries returns a map of field names to field dictionaries.
// Every named entry is included, top-level or nested through /Kids (as
// Acrobat writes hierarchical forms), keyed by its fully qualified name,
// e.g. "parent.child"; a kid without /T shares its parent's name. When
// several entries share a name, the first one with a /V is kept, and an
// entry without /V gives way to a later one that has one. Names are decoded
// like model.PdfField.PartialName, so they match the form's names.
func (fdf *Data) FieldDictionaries() (map[string]*core.PdfObjectDictionary, error) {
	dicts := map[string]*core.PdfObjectDictionary{}
	collectFieldDicts(fdf.fields, "", dicts)
	return dicts, nil
}
