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
	"slices"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func (p *fdfParser) parseNull() (core.PdfObjectNull, error) {
	_, err := p.reader.Discard(4)
	return core.PdfObjectNull{}, err
}

func (p *fdfParser) trace(obj core.PdfObject) core.PdfObject {
	switch o := obj.(type) {
	case *core.PdfObjectReference:
		ind, ok := p.objects[o.ObjectNumber].(*core.PdfIndirectObject)
		if ok {
			return ind.PdfObject
		}
		common.Log.Debug("Type error")
		return nil
	case *core.PdfIndirectObject:
		return o.PdfObject
	}
	return obj
}

func newParser(rs io.ReadSeeker) (*fdfParser, error) {
	p := &fdfParser{}
	p.rs = rs
	p.objects = map[int64]core.PdfObject{}
	major, minor, err := p.parseFdfVersion()
	if err != nil {
		common.Log.Error("Unable to parse version: %v", err)
		return nil, err
	}
	p.versionMajor = major
	p.versionMinor = minor
	err = p.parse()
	return p, err
}

func toFileSpecPath(path string) string {
	vol := filepath.VolumeName(path)
	rest := path[len(vol):]
	if len(vol) == 2 && vol[1] == ':' && rest != "" && os.IsPathSeparator(rest[0]) {
		return "/" + vol[:1] + filepath.ToSlash(rest)
	}
	return filepath.ToSlash(path)
}

func makeTextString(s string) *core.PdfObjectString {
	if isASCII(s) {
		return core.MakeString(s)
	}
	return core.MakeEncodedString(s, true)
}

func (p *fdfParser) parseArray() (*core.PdfObjectArray, error) {
	arr := core.MakeArray()
	p.reader.ReadByte()
	for {
		p.skipSpaces()
		peek, err := p.reader.Peek(1)
		if err != nil {
			return arr, err
		}
		if peek[0] == ']' {
			p.reader.ReadByte()
			break
		}
		obj, err := p.parseObject()
		if err != nil {
			return arr, err
		}
		arr.Append(obj)
	}
	return arr, nil
}

// SetSourceFile records the path of the PDF form the data belongs to, as the
// importing application should resolve it (typically the file name, or a
// path relative to the FDF file), in the /F entry. The path is converted to
// file specification syntax: "/" separators, a drive letter "C:" written as
// "/C". An ASCII path is written as a file specification string; any other
// path as a file specification dictionary holding the PDFDocEncoded /F and
// the Unicode /UF. An empty path removes the entry.
func (fdf *Data) SetSourceFile(path string) {
	if fdf.fdfDict == nil {
		return
	}
	if path == "" {
		fdf.fdfDict.Remove("F")
		return
	}
	path = toFileSpecPath(path)
	if isASCII(path) {
		fdf.fdfDict.Set("F", core.MakeString(path))
		return
	}
	spec := core.MakeDict()
	spec.Set("Type", core.MakeName("Filespec"))
	spec.Set("F", core.MakeEncodedString(path, false))
	spec.Set("UF", core.MakeEncodedString(path, true))
	fdf.fdfDict.Set("F", spec)
}

func (p *fdfParser) parseHexString() (*core.PdfObjectString, error) {
	p.reader.ReadByte()
	var buf bytes.Buffer
	for {
		peek, err := p.reader.Peek(1)
		if err != nil {
			return core.MakeHexString(""), err
		}
		if peek[0] == '>' {
			p.reader.ReadByte()
			break
		}
		c, _ := p.reader.ReadByte()
		if !core.IsWhiteSpace(c) {
			buf.WriteByte(c)
		}
	}
	if buf.Len()%2 == 1 {
		buf.WriteRune('0')
	}
	decoded, err := hex.DecodeString(buf.String())
	if err != nil {
		common.Log.Debug("ERROR Parsing hex string: '%s' - returning an empty string", buf.String())
		return core.MakeHexString(""), nil
	}
	return core.MakeHexString(string(decoded)), nil
}

func parserFromString(s string) (*fdfParser, error) {
	p := fdfParser{}
	b := []byte(s)
	rd := bytes.NewReader(b)
	p.rs = rd
	p.objects = map[int64]core.PdfObject{}
	br := bufio.NewReader(rd)
	p.reader = br
	p.fileSize = int64(len(s))
	return &p, p.parse()
}

// Root returns the Root of the FDF document.
func (p *fdfParser) Root() (*core.PdfObjectDictionary, error) {
	cat, err := p.catalog()
	if err != nil {
		return nil, err
	}
	return cat.fdf, nil
}

func (p *fdfParser) parseDict() (*core.PdfObjectDictionary, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Reading FDF Dict!")
	}
	dict := core.MakeDict()
	c, _ := p.reader.ReadByte()
	if c != '<' {
		return nil, errors.New("invalid dict")
	}
	c, _ = p.reader.ReadByte()
	if c != '<' {
		return nil, errors.New("invalid dict")
	}
	for {
		p.skipSpaces()
		p.skipComments()
		peek, err := p.reader.Peek(2)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Dict peek: %s (% x)!", string(peek), string(peek))
		}
		if (peek[0] == '>') && (peek[1] == '>') {
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
		key, err := p.parseName()
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Key: %s", key)
		}
		if err != nil {
			common.Log.Debug("ERROR Returning name err %s", err)
			return nil, err
		}
		if len(key) > 4 && key[len(key)-4:] == "null" {
			trimmedKey := key[0 : len(key)-4]
			common.Log.Debug("Taking care of null bug (%s)", key)
			common.Log.Debug("New key \"%s\" = null", trimmedKey)
			p.skipSpaces()
			next, _ := p.reader.Peek(1)
			if next[0] == '/' {
				dict.Set(trimmedKey, core.MakeNull())
				continue
			}
		}
		p.skipSpaces()
		val, err := p.parseObject()
		if err != nil {
			return nil, err
		}
		dict.Set(key, val)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("dict[%s] = %s", key, val.String())
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("returning FDF Dict!")
	}
	return dict, nil
}

var reEOFMarker = regexp.MustCompile("%%EOF")

// LoadFromPath loads FDF form data from file path `fdfPath`.
func LoadFromPath(fdfPath string) (*Data, error) {
	f, err := os.Open(fdfPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(f)
}

// FieldValues implements interface model.FieldValueProvider.
// Returns a map of field names to values (PdfObjects). Fields without a
// value (/V) are left out, so filling a form from the data never clears a
// field the file carries no value for.
func (fdf *Data) FieldValues() (map[string]core.PdfObject, error) {
	dicts, err := fdf.FieldDictionaries()
	if err != nil {
		return nil, err
	}
	var names []string
	for name := range dicts {
		names = append(names, name)
	}
	sort.Strings(names)
	values := map[string]core.PdfObject{}
	for _, name := range names {
		dict := dicts[name]
		v := core.TraceToDirectObject(dict.Get("V"))
		if v == nil {
			continue
		}
		values[name] = v
	}
	return values, nil
}

func copyDocumentID(trailer *core.PdfObjectDictionary) *core.PdfObjectArray {
	id, ok := core.GetArray(trailer.Get("ID"))
	if !ok || id.Len() != 2 {
		return nil
	}
	out := core.MakeArray()
	for _, elem := range id.Elements() {
		s, ok := core.GetString(elem)
		if !ok {
			return nil
		}
		if s.IsHexadecimal() {
			out.Append(core.MakeHexString(s.Str()))
		} else {
			out.Append(core.MakeString(s.Str()))
		}
	}
	return out
}

func (fdf *Data) serialize() ([]byte, error) {
	if fdf.fdfDict == nil {
		return nil, errors.New("FDF dictionary missing")
	}
	major, minor := 1, 2
	if fdf.versionMajor > 0 {
		major, minor = fdf.versionMajor, fdf.versionMinor
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%%FDF-%d.%d\n", major, minor)
	buf.Write([]byte{'%', 0xE2, 0xE3, 0xCF, 0xD3, '\n'})
	nums := make([]int64, 0, len(fdf.objects))
	for num := range fdf.objects {
		nums = append(nums, num)
	}
	slices.Sort(nums)
	var lastNum int64
	for _, num := range nums {
		if err := writeObject(&buf, fdf.objects[num]); err != nil {
			return nil, err
		}
		lastNum = num
	}
	rootNum, rootGen := fdf.rootObjNum, fdf.rootGenNum
	if rootNum == 0 {
		rootNum = lastNum + 1
		catalog := fdf.catalogDict
		if catalog == nil {
			catalog = core.MakeDict()
			catalog.Set("FDF", fdf.fdfDict)
		}
		fmt.Fprintf(&buf, "%d 0 obj\n", rootNum)
		buf.Write(catalog.Write())
		buf.WriteString("\nendobj\n")
	}
	fmt.Fprintf(&buf, "trailer\n<</Root %d %d R>>\n%%%%EOF\n", rootNum, rootGen)
	return buf.Bytes(), nil
}

func (p *fdfParser) parseBool() (core.PdfObjectBool, error) {
	peek, err := p.reader.Peek(4)
	if err != nil {
		return core.PdfObjectBool(false), err
	}
	if (len(peek) >= 4) && (string(peek[:4]) == "true") {
		p.reader.Discard(4)
		return core.PdfObjectBool(true), nil
	}
	peek, err = p.reader.Peek(5)
	if err != nil {
		return core.PdfObjectBool(false), err
	}
	if (len(peek) >= 5) && (string(peek[:5]) == "false") {
		p.reader.Discard(5)
		return core.PdfObjectBool(false), nil
	}
	return core.PdfObjectBool(false), errors.New("unexpected boolean string")
}

func writeObject(buf *bytes.Buffer, obj core.PdfObject) error {
	switch o := obj.(type) {
	case *core.PdfIndirectObject:
		fmt.Fprintf(buf, "%d %d obj\n", o.ObjectNumber, o.GenerationNumber)
		if o.PdfObject == nil {
			buf.WriteString("null")
		} else {
			buf.Write(o.PdfObject.Write())
		}
		buf.WriteString("\nendobj\n")
	case *core.PdfObjectStream:
		o.Set("Length", core.MakeInteger(int64(len(o.Stream))))
		fmt.Fprintf(buf, "%d %d obj\n", o.ObjectNumber, o.GenerationNumber)
		buf.Write(o.PdfObjectDictionary.Write())
		buf.WriteString("\nstream\n")
		buf.Write(o.Stream)
		buf.WriteString("\nendstream\nendobj\n")
	default:
		return fmt.Errorf("unexpected FDF object type %T", obj)
	}
	return nil
}

var reIndirectObj = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s+obj")

// Load loads FDF form data from `r`.
func Load(r io.ReadSeeker) (*Data, error) {
	p, err := newParser(r)
	if err != nil {
		return nil, err
	}
	return dataFromParser(p)
}

func (p *fdfParser) seekToEOFMarker(size int64) error {
	offset := int64(0)
	chunk := int64(1000)
	for offset < size {
		if size <= (chunk + offset) {
			chunk = size - offset
		}
		_, err := p.rs.Seek(-offset-chunk, io.SeekEnd)
		if err != nil {
			return err
		}
		data := make([]byte, chunk)
		p.rs.Read(data)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking for EOF marker: \"%s\"", string(data))
		}
		matches := reEOFMarker.FindAllStringIndex(string(data), -1)
		if matches != nil {
			last := matches[len(matches)-1]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Ind: % d", matches)
			}
			p.rs.Seek(-offset-chunk+int64(last[0]), io.SeekEnd)
			return nil
		}
		common.Log.Debug("Warning: EOF marker not found! - continue seeking")
		offset += chunk
	}
	common.Log.Debug("Error: EOF marker was not found.")
	return errors.New("EOF not found")
}
