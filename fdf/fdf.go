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

func (p *fdfParser) parseIndirectObject() (core.PdfObject, error) {
	ind := core.PdfIndirectObject{}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("-Read indirect obj")
	}
	peek, err := p.reader.Peek(20)
	if err != nil {
		common.Log.Debug("ERROR: Fail to read indirect obj")
		return &ind, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("(indirect obj peek \"%s\"", string(peek))
	}
	loc := reIndirectObj.FindStringSubmatchIndex(string(peek))
	if len(loc) < 6 {
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(peek))
		return &ind, errors.New("unable to detect indirect object signature")
	}
	p.reader.Discard(loc[0])
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Offsets % d", loc)
	}
	sigLen := loc[1] - loc[0]
	sig := make([]byte, sigLen)
	_, err = p.readAtLeast(sig, sigLen)
	if err != nil {
		common.Log.Debug("ERROR: unable to read - %s", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("textline: %s", sig)
	}
	m := reIndirectObj.FindStringSubmatch(string(sig))
	if len(m) < 3 {
		common.Log.Debug("ERROR: Unable to find object signature (%s)", string(sig))
		return &ind, errors.New("unable to detect indirect object signature")
	}
	objNum, _ := strconv.Atoi(m[1])
	genNum, _ := strconv.Atoi(m[2])
	ind.ObjectNumber = int64(objNum)
	ind.GenerationNumber = int64(genNum)
	for {
		next, err := p.reader.Peek(2)
		if err != nil {
			return &ind, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Ind. peek: %s (% x)!", string(next), string(next))
		}
		if core.IsWhiteSpace(next[0]) {
			p.skipSpaces()
		} else if next[0] == '%' {
			p.skipComments()
		} else if (next[0] == '<') && (next[1] == '<') {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Call ParseDict")
			}
			ind.PdfObject, err = p.parseDict()
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF Call ParseDict: %v", err)
			}
			if err != nil {
				return &ind, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Parsed dictionary... finished.")
			}
		} else if (next[0] == '/') || (next[0] == '(') || (next[0] == '[') || (next[0] == '<') {
			ind.PdfObject, err = p.parseObject()
			if err != nil {
				return &ind, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Parsed object ... finished.")
			}
		} else {
			if next[0] == 'e' {
				line, lineErr := p.readTextLine()
				if lineErr != nil {
					return nil, lineErr
				}
				if len(line) >= 6 && line[0:6] == "endobj" {
					break
				}
			} else if next[0] == 's' {
				next, _ = p.reader.Peek(10)
				if string(next[:6]) == "stream" {
					off := 6
					if len(next) > 6 {
						if core.IsWhiteSpace(next[off]) && next[off] != '\r' && next[off] != '\n' {
							common.Log.Debug("Non-conformant FDF not ending stream line properly with EOL marker")
							off++
						}
						switch next[off] {
						case '\r':
							off++
							if next[off] == '\n' {
								off++
							}
						case '\n':
							off++
						}
					}
					p.reader.Discard(off)
					dict, isDict := ind.PdfObject.(*core.PdfObjectDictionary)
					if !isDict {
						return nil, errors.New("stream object missing dictionary")
					}
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Stream dict %s", dict)
					}
					lengthObj, isInt := dict.Get("Length").(*core.PdfObjectInteger)
					if !isInt {
						return nil, errors.New("stream length needs to be an integer")
					}
					length := *lengthObj
					if length < 0 {
						return nil, errors.New("stream needs to be longer than 0")
					}
					if int64(length) > p.fileSize {
						common.Log.Debug("ERROR: Stream length cannot be larger than file size")
						return nil, errors.New("invalid stream length, larger than file size")
					}
					data := make([]byte, length)
					_, err = p.readAtLeast(data, int(length))
					if err != nil {
						common.Log.Debug("ERROR stream (%d): %X", len(data), data)
						common.Log.Debug("ERROR: %v", err)
						return nil, err
					}
					stream := core.PdfObjectStream{
						Stream:              data,
						PdfObjectDictionary: ind.PdfObject.(*core.PdfObjectDictionary),
						ObjectNumber:        ind.ObjectNumber,
						GenerationNumber:    ind.GenerationNumber,
					}
					p.skipSpaces()
					p.reader.Discard(9)
					p.skipSpaces()
					return &stream, nil
				}
			}
			ind.PdfObject, err = p.parseObject()
			return &ind, err
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Returning indirect!")
	}
	return &ind, nil
}

func (p *fdfParser) parseFdfVersion() (int, int, error) {
	p.rs.Seek(0, io.SeekStart)
	n := 20
	head := make([]byte, n)
	p.rs.Read(head)
	m := reFdfVersion.FindStringSubmatch(string(head))
	if len(m) < 3 {
		fbMajor, fbMinor, err := p.seekFdfVersionTopDown()
		if err != nil {
			common.Log.Debug("Failed recovery - unable to find version")
			return 0, 0, err
		}
		return fbMajor, fbMinor, nil
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, err
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, err
	}
	common.Log.Debug("Fdf version %d.%d", major, minor)
	return major, minor, nil
}
