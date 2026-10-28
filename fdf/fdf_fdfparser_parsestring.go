// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package fdf provides support for loading form field data from Forms Data Format (FDF) files
// and for exporting the field data of a PDF form to an FDF file.
package fdf

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func (p *fdfParser) parseString() (*core.PdfObjectString, error) {
	p.reader.ReadByte()
	var buf bytes.Buffer
	depth := 1
	for {
		peek, err := p.reader.Peek(1)
		if err != nil {
			return core.MakeString(buf.String()), err
		}
		if peek[0] == '\\' {
			p.reader.ReadByte()
			c, err := p.reader.ReadByte()
			if err != nil {
				return core.MakeString(buf.String()), err
			}
			if core.IsOctalDigit(c) {
				next, err := p.reader.Peek(2)
				if err != nil {
					return core.MakeString(buf.String()), err
				}
				var digits []byte
				digits = append(digits, c)
				for _, d := range next {
					if core.IsOctalDigit(d) {
						digits = append(digits, d)
					} else {
						break
					}
				}
				p.reader.Discard(len(digits) - 1)
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Numeric string \"%s\"", digits)
				}
				v, err := strconv.ParseUint(string(digits), 8, 32)
				if err != nil {
					return core.MakeString(buf.String()), err
				}
				buf.WriteByte(byte(v))
				continue
			}
			switch c {
			case 'n':
				buf.WriteRune('\n')
			case 'r':
				buf.WriteRune('\r')
			case 't':
				buf.WriteRune('\t')
			case 'b':
				buf.WriteRune('\b')
			case 'f':
				buf.WriteRune('\f')
			case '(':
				buf.WriteRune('(')
			case ')':
				buf.WriteRune(')')
			case '\\':
				buf.WriteRune('\\')
			}
			continue
		} else if peek[0] == '(' {
			depth++
		} else if peek[0] == ')' {
			depth--
			if depth == 0 {
				p.reader.ReadByte()
				break
			}
		}
		b, _ := p.reader.ReadByte()
		buf.WriteByte(b)
	}
	return core.MakeString(buf.String()), nil
}

func writeFileDirect(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (p *fdfParser) getFileOffset() int64 {
	off, _ := p.rs.Seek(0, io.SeekCurrent)
	off -= int64(p.reader.Buffered())
	return off
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Write serialises the FDF form data to `w`.
//
// The output is laid out as Acrobat writes it: an %FDF header, the indirect
// objects and a trailer pointing at the catalog, with no cross-reference
// table. Data built by NewFromForm or NewFromReader is a single catalog
// object. Data read by Load keeps its indirect objects and their numbers, and
// field dictionaries modified in place are written with their changes.
func (fdf *Data) Write(w io.Writer) error {
	data, err := fdf.serialize()
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func exportFieldValue(fv model.FieldValue) (core.PdfObject, bool) {
	if _, isSig := fv.Field.GetContext().(*model.PdfFieldSignature); isSig {
		return nil, false
	}
	switch v := fv.Value.(type) {
	case nil, *core.PdfObjectNull:
		return nil, false
	case *core.PdfObjectName:
		return v, true
	case *core.PdfObjectString:
		return normalizeTextString(v), true
	case *core.PdfObjectArray:
		arr := core.MakeArray()
		for _, elem := range v.Elements() {
			elem = core.TraceToDirectObject(elem)
			if elem == nil {
				continue
			}
			if str, ok := elem.(*core.PdfObjectString); ok {
				elem = normalizeTextString(str)
			}
			arr.Append(elem)
		}
		return arr, true
	case *core.PdfObjectStream:
		decoded, err := core.DecodeStream(v)
		if err != nil {
			common.Log.Debug("WARN: FDF export: skipping field %q, cannot decode value stream: %v", fv.Name, err)
			return nil, false
		}
		return normalizeTextString(core.MakeStringFromBytes(decoded)), true
	default:
		common.Log.Debug("WARN: FDF export: skipping field %q with unsupported value type %T", fv.Name, fv.Value)
		return nil, false
	}
}

func (p *fdfParser) setFileOffset(off int64) {
	p.rs.Seek(off, io.SeekStart)
	p.reader = bufio.NewReader(p.rs)
}

type fdfCatalog struct {
	root   *core.PdfObjectDictionary
	fdf    *core.PdfObjectDictionary
	objNum int64
	genNum int64
}

func dataFromParser(p *fdfParser) (*Data, error) {
	cat, err := p.catalog()
	if err != nil {
		return nil, err
	}
	fields, ok := core.GetArray(cat.fdf.Get("Fields"))
	if !ok {
		return nil, errors.New("fields missing")
	}
	return &Data{fields: fields, fdfDict: cat.fdf, objects: p.objects, catalogDict: cat.root, rootObjNum: cat.objNum, rootGenNum: cat.genNum, versionMajor: p.versionMajor, versionMinor: p.versionMinor}, nil
}

// NewFromForm builds FDF form data from the terminal fields of `form`.
//
// Each field is written with its fully qualified name (e.g. "parent.child")
// as /T and its value as /V: text as a PDF text string (non-ASCII text as
// UTF-16BE), check box and radio button states as names and multi-select
// choice values as arrays. Fields without a value, signature fields and
// non-terminal fields are omitted.
func NewFromForm(form *model.PdfAcroForm) (*Data, error) {
	if form == nil {
		return nil, errors.New("form is nil")
	}
	fields := core.MakeArray()
	for _, fv := range form.AllFieldValues() {
		val, ok := exportFieldValue(fv)
		if !ok {
			continue
		}
		entry := core.MakeDict()
		entry.Set("T", makeTextString(fv.Name))
		entry.Set("V", val)
		fields.Append(entry)
	}
	fdfDict := core.MakeDict()
	fdfDict.Set("Fields", fields)
	return &Data{fdfDict: fdfDict, fields: fields}, nil
}

func collectFieldDicts(fields *core.PdfObjectArray, prefix string, out map[string]*core.PdfObjectDictionary) {
	for i := 0; i < fields.Len(); i++ {
		dict, ok := core.GetDict(fields.Get(i))
		if !ok {
			continue
		}
		name := prefix
		if t, _ := core.GetString(dict.Get("T")); t != nil {
			name = t.Decoded()
			if prefix != "" {
				name = prefix + "." + name
			}
		}
		if name != "" {
			if existing, found := out[name]; !found || existing.Get("V") == nil {
				out[name] = dict
			}
		}
		if kids, ok := core.GetArray(dict.Get("Kids")); ok {
			collectFieldDicts(kids, name, out)
		}
	}
}

func (p *fdfParser) catalog() (*fdfCatalog, error) {
	if p.trailer != nil {
		rootObj := p.trailer.Get("Root")
		if root, isDict := p.trace(rootObj).(*core.PdfObjectDictionary); isDict {
			if fdfDict, hasFDF := p.trace(root.Get("FDF")).(*core.PdfObjectDictionary); hasFDF {
				cat := &fdfCatalog{root: root, fdf: fdfDict}
				if ref, isRef := rootObj.(*core.PdfObjectReference); isRef {
					cat.objNum = ref.ObjectNumber
					cat.genNum = ref.GenerationNumber
				}
				return cat, nil
			}
		}
	}
	var nums []int64
	for n := range p.objects {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	for _, num := range nums {
		obj := p.objects[num]
		if root, isDict := p.trace(obj).(*core.PdfObjectDictionary); isDict {
			if fdfDict, hasFDF := p.trace(root.Get("FDF")).(*core.PdfObjectDictionary); hasFDF {
				cat := &fdfCatalog{root: root, fdf: fdfDict, objNum: num}
				if ind, isInd := obj.(*core.PdfIndirectObject); isInd {
					cat.genNum = ind.GenerationNumber
				}
				return cat, nil
			}
		}
	}
	return nil, errors.New("FDF not found")
}

func (p *fdfParser) skipComments() error {
	if _, err := p.skipSpaces(); err != nil {
		return err
	}
	first := true
	for {
		peek, err := p.reader.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return err
		}
		if first && peek[0] != '%' {
			return nil
		}
		first = false
		if (peek[0] != '\r') && (peek[0] != '\n') {
			p.reader.ReadByte()
		} else {
			break
		}
	}
	return p.skipComments()
}

func (p *fdfParser) readTextLine() (string, error) {
	var buf bytes.Buffer
	for {
		peek, err := p.reader.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return buf.String(), err
		}
		if (peek[0] != '\r') && (peek[0] != '\n') {
			c, _ := p.reader.ReadByte()
			buf.WriteByte(c)
		} else {
			break
		}
	}
	return buf.String(), nil
}

func (p *fdfParser) readComment() (string, error) {
	var buf bytes.Buffer
	_, err := p.skipSpaces()
	if err != nil {
		return buf.String(), err
	}
	first := true
	for {
		peek, err := p.reader.Peek(1)
		if err != nil {
			common.Log.Debug("Error %s", err.Error())
			return buf.String(), err
		}
		if first && peek[0] != '%' {
			return buf.String(), errors.New("comment should start with %")
		}
		first = false
		if (peek[0] != '\r') && (peek[0] != '\n') {
			c, _ := p.reader.ReadByte()
			buf.WriteByte(c)
		} else {
			break
		}
	}
	return buf.String(), nil
}

func hasUTF16BOM(s *core.PdfObjectString) bool {
	b := s.Bytes()
	return len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF
}

type fdfParser struct {
	versionMajor int
	versionMinor int
	objects      map[int64]core.PdfObject
	rs           io.ReadSeeker
	reader       *bufio.Reader
	fileSize     int64
	trailer      *core.PdfObjectDictionary
}
