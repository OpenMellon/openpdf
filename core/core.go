// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package core defines and implements the primitive PDF object types in golang, and provides functionality
// for parsing those from a PDF file stream. This includes I/O handling, cross references, repairs, encryption,
// encoding and other core capabilities.
package core

import (
	"bufio"
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/core/security/crypt"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/ccittfax"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/jbig2"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	"github.com/OpenMellon/openpdf/internal/jbig2/decoder"
	"github.com/OpenMellon/openpdf/internal/jbig2/document"
	"github.com/OpenMellon/openpdf/internal/jbig2/document/segments"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/OpenMellon/openpdf/internal/lazystream"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/strutils"
	tifflzw "golang.org/x/image/tiff/lzw"
	"golang.org/x/sync/singleflight"
	"golang.org/x/text/unicode/bidi"
	"golang.org/x/xerrors"
)

func (crypt *PdfCrypt) isPubSec() bool { return crypt.pubSec != nil }

// HasOddLengthHexStrings checks if the document has odd length hexadecimal strings.
func (meta ParserMetadata) HasOddLengthHexStrings() bool { return meta.oddLengthHexStrings }

func parseCryptFilterDict(filterDict *crypt.FilterDict, dict *PdfObjectDictionary) error {
	if typeName, ok := dict.Get("Type").(*PdfObjectName); ok {
		if typeStr := string(*typeName); typeStr != "CryptFilter" {
			common.Log.Debug("Invalid CF dict type: %s (should be CryptFilter)", typeStr)
		}
	}
	cfm, ok := dict.Get("CFM").(*PdfObjectName)
	if !ok {
		return fmt.Errorf("unsupported crypt filter (None)")
	}
	filterDict.CFM = string(*cfm)
	if authEvent, isName := dict.Get("AuthEvent").(*PdfObjectName); isName {
		filterDict.AuthEvent = security.AuthEvent(*authEvent)
	} else {
		filterDict.AuthEvent = security.EventDocOpen
	}
	if length, isNum := dict.Get("Length").(*PdfObjectInteger); isNum {
		filterDict.Length = int(*length)
	}
	return nil
}

// Seek mirrors io.Seeker.
func (cursor *readerAtCursor) Seek(offset int64, whence int) (int64, error) {
	var position int64
	switch whence {
	case io.SeekStart:
		position = offset
	case io.SeekCurrent:
		position = cursor.GetFileOffset() + offset
	case io.SeekEnd:
		position = cursor.size + offset
	default:
		return 0, fmt.Errorf("readerAtCursor.Seek: invalid whence %d", whence)
	}
	if position < 0 {
		return 0, errors.New("readerAtCursor.Seek: negative position")
	}
	cursor.SetFileOffset(position)
	return position, nil
}

// LinearizationDictObjNum returns the object number of the linearization
// parameter dictionary (the first indirect object after the header), or 0 if
// the document does not begin with a linearization dictionary. Note this is
// populated whenever a leading linearization dictionary is found, even if
// IsLinearized() is false (e.g. it was superseded by a later revision).
func (parser *PdfParser) LinearizationDictObjNum() int { return parser.linearizationObjNum }

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
func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

// DecodeBytes decodes a slice of Flate encoded bytes and returns the result.
func (flate *FlateEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("FlateDecode bytes")
	}
	if len(encoded) == 0 {
		common.Log.Debug("ERROR: empty Flate encoded buffer. Returning empty byte slice.")
		return []byte{}, nil
	}
	reader := bytes.NewReader(encoded)
	zlibReader, err := acquireZlibReader(reader)
	if err != nil {
		common.Log.Debug("Decoding error %v\n", err)
		common.Log.Debug("Stream (%d) % x", len(encoded), encoded)
		return nil, err
	}
	all, err := flate.limits.readAll(StreamEncodingFilterNameFlate, zlibReader)
	zlibReaderPool.Put(zlibReader)
	if errors.Is(err, ErrDecodedSizeLimitExceeded) {
		return nil, err
	}
	flate.limits.charge(int64(len(all)))
	return all, nil
}

// GetXrefOffset returns the offset of the xref table.
func (parser *PdfParser) GetXrefOffset() int64 { return parser.xrefOffset }

func (crypt *PdfCrypt) decryptBytes(stream []byte, filterName string, objKey []byte) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Decrypt bytes")
	}
	filter, ok := crypt.cryptFilters[filterName]
	if !ok {
		return nil, fmt.Errorf("unknown crypt filter (%s)", filterName)
	}
	return filter.DecryptBytes(stream, objKey)
}

func parsePubSecRecipients(dict *PdfObjectDictionary) ([][]byte, error) {
	recipients, ok := GetArray(dict.Get("Recipients"))
	if !ok || recipients.Len() == 0 {
		return nil, errors.New("public-key encrypt dictionary missing Recipients")
	}
	datas := make([][]byte, 0, recipients.Len())
	for i, elem := range recipients.Elements() {
		stringBytes, hasStringBytes := GetStringBytes(elem)
		if !hasStringBytes || len(stringBytes) == 0 {
			return nil, fmt.Errorf("invalid Recipients entry %d", i)
		}
		datas = append(datas, stringBytes)
	}
	return datas, nil
}

func newDCTEncoderFromStream(streamObj *PdfObjectStream, multi *MultiEncoder) (*DCTEncoder, error) {
	dctEncoder := NewDCTEncoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return dctEncoder, nil
	}
	var reader io.Reader = bytes.NewReader(streamObj.Stream)
	if multi != nil {
		reader = multi.DecodeReader(reader)
		if c, ok := reader.(io.Closer); ok {
			defer c.Close()
		}
	}
	config, err := jpeg.DecodeConfig(reader)
	if err != nil {
		common.Log.Debug("Error decoding file: %s", err)
		return nil, err
	}
	switch config.ColorModel {
	case color.RGBAModel:
		dctEncoder.BitsPerComponent = 8
		dctEncoder.ColorComponents = 3
		dctEncoder.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	case color.RGBA64Model:
		dctEncoder.BitsPerComponent = 16
		dctEncoder.ColorComponents = 3
		dctEncoder.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	case color.GrayModel:
		dctEncoder.BitsPerComponent = 8
		dctEncoder.ColorComponents = 1
		dctEncoder.Decode = []float64{0.0, 1.0}
	case color.Gray16Model:
		dctEncoder.BitsPerComponent = 16
		dctEncoder.ColorComponents = 1
		dctEncoder.Decode = []float64{0.0, 1.0}
	case color.CMYKModel:
		dctEncoder.BitsPerComponent = 8
		dctEncoder.ColorComponents = 4
		dctEncoder.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	case color.YCbCrModel:
		dctEncoder.BitsPerComponent = 8
		dctEncoder.ColorComponents = 3
		dctEncoder.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	default:
		return nil, errors.New("unsupported color model")
	}
	dctEncoder.Width = config.Width
	dctEncoder.Height = config.Height
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("DCT Encoder: %+v", dctEncoder)
	}
	dctEncoder.Quality = DefaultJPEGQuality
	decode, ok := GetArray(pdfObjectDictionary.Get("Decode"))
	if ok {
		floats, err2 := decode.ToFloat64Array()
		if err2 != nil {
			return dctEncoder, err2
		}
		dctEncoder.Decode = floats
	}
	return dctEncoder, nil
}

func (parser *PdfParser) afterAuthentication() error {
	parser.clearObjCache()
	var err error
	if parser.pendingXrefRepair {
		parser.pendingXrefRepair = false
		err = parser.checkAndRepairXrefTable(parser.trailer)
	}
	if parser.encryptObj != nil {
		parser.StoreCachedObject(int(parser.encryptObj.ObjectNumber), parser.encryptObj)
	}
	return err
}

func (jbig2Img *JBIG2Image) toBitmap() (bm *bitmap.Bitmap, err error) {
	const procJBIG2ImageToBitmap = "JBIG2Image.toBitmap"
	if jbig2Img.Data == nil {
		return nil, jbig2errors.Error(procJBIG2ImageToBitmap, "image data not defined")
	}
	if jbig2Img.Width == 0 || jbig2Img.Height == 0 {
		return nil, jbig2errors.Error(procJBIG2ImageToBitmap, "image height or width not defined")
	}
	if jbig2Img.HasPadding {
		bm, err = bitmap.NewWithData(jbig2Img.Width, jbig2Img.Height, jbig2Img.Data)
	} else {
		bm, err = bitmap.NewWithUnpaddedData(jbig2Img.Width, jbig2Img.Height, jbig2Img.Data)
	}
	if err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2ImageToBitmap, "")
	}
	return bm, nil
}

func acquireZlibReader(r io.Reader) (io.ReadCloser, error) {
	rc := zlibReaderPool.Get().(io.ReadCloser)
	if err := rc.(zlib.Resetter).Reset(r, nil); err != nil {
		zlibReaderPool.Put(rc)
		return nil, err
	}
	return rc, nil
}

// DecodeStream decodes a LZW encoded stream and returns the result as a
// slice of bytes.
func (enc *LZWEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("LZW Decoding")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Predictor: %d", enc.Predictor)
	}
	bytes, err := enc.DecodeBytes(streamObj.Stream)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace(" IN: (%d) % x", len(streamObj.Stream), streamObj.Stream)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("OUT: (%d) % x", len(bytes), bytes)
	}
	if enc.Predictor > 1 {
		predictor, err2 := decodePredictor(bytes, enc.Predictor, enc.Colors, enc.BitsPerComponent, enc.Columns)
		if err2 != nil {
			ReleaseBytes(bytes)
			return nil, err2
		}
		return predictor, nil
	}
	return bytes, nil
}

// IsEncrypted checks if the document is encrypted. A bool flag is returned indicating the result.
// First time when called, will check if the Encrypt dictionary is accessible through the trailer dictionary.
// If encrypted, prepares a crypt datastructure which can be used to authenticate and decrypt the document.
// On failure, an error is returned.
func (parser *PdfParser) IsEncrypted() (bool, error) {
	if parser.crypter != nil {
		return true, nil
	} else if parser.trailer == nil {
		return false, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Checking encryption dictionary!")
	}
	encrypt := parser.trailer.Get("Encrypt")
	if encrypt == nil {
		return false, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Is encrypted!")
	}
	var dict *PdfObjectDictionary
	switch v := encrypt.(type) {
	case *PdfObjectDictionary:
		dict = v
	case *PdfObjectReference:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("0: Look up ref %q", v)
		}
		byReference, err := parser.LookupByReference(*v)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("1: %q", byReference)
		}
		if err != nil {
			return false, err
		}
		indirect, ok := byReference.(*PdfIndirectObject)
		if !ok {
			common.Log.Debug("Encryption object not an indirect object")
			return false, errors.New("type check error")
		}
		indirectDict, ok := indirect.PdfObject.(*PdfObjectDictionary)
		parser.encryptObj = indirect
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("2: %q", indirectDict)
		}
		if !ok {
			return false, errors.New("trailer Encrypt object non dictionary")
		}
		dict = indirectDict
	case *PdfObjectNull:
		common.Log.Debug("Encrypt is a null object. File should not be encrypted.")
		return false, nil
	default:
		return false, fmt.Errorf("unsupported type: %T", v)
	}
	crypt, err := PdfCryptNewDecrypt(parser, dict, parser.trailer)
	if err != nil {
		return false, err
	}
	for _, key := range []string{"Encrypt"} {
		obj := parser.trailer.Get(PdfObjectName(key))
		if obj == nil {
			continue
		}
		switch v := obj.(type) {
		case *PdfObjectReference:
			crypt.encryptedObjNums[int(v.ObjectNumber)] = struct{}{}
		case *PdfIndirectObject:
			crypt.decryptedObjects[v] = true
			crypt.encryptedObjNums[int(v.ObjectNumber)] = struct{}{}
		}
	}
	parser.crypter = crypt
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Crypter object %b", crypt)
	}
	return true, nil
}

// Str returns the string value of the PdfObjectString. Defined in addition to String() function to clarify that
// this function returns the underlying string directly, whereas the String function technically could include
// debug info.
func (str *PdfObjectString) Str() string { return str.val }

// NewJBIG2Encoder creates a new JBIG2Encoder.
func NewJBIG2Encoder() *JBIG2Encoder { return &JBIG2Encoder{doc: document.InitEncodeDocument(false)} }

// LoadCachedObject returns the cached PdfObject for the given object number,
// if any.
func (parser *PdfParser) LoadCachedObject(objNumber int) (PdfObject, bool) {
	cached, ok := parser.objCache.Load(objNumber)
	if !ok {
		return nil, false
	}
	return cached.(PdfObject), true
}

// GetString is a helper for Get that returns a string value.
// Returns false if the key is missing or a value is not a string.
func (dict *PdfObjectDictionary) GetString(key PdfObjectName) (string, bool) {
	obj := dict.Get(key)
	if obj == nil {
		return "", false
	}
	str, ok := obj.(*PdfObjectString)
	if !ok {
		return "", false
	}
	return str.Str(), true
}

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

// GetXrefTable returns the PDF's current xref-table snapshot. The returned
// XrefTable is a shallow value copy: its ObjectMap and sortedObjects fields
// still alias the live snapshot's underlying map and slice, so the result
// should be treated as read-only. Mutating result.ObjectMap from the caller
// will corrupt parser state. Subsequent repair-driven snapshot swaps are
// not reflected in an already-returned copy.
func (parser *PdfParser) GetXrefTable() XrefTable { return *parser.currentXrefs() }

// Elements returns a slice of the PdfObject elements in the array.
// Preferred over accessing the array directly as type may be changed in future major versions (v3).
func (streams *PdfObjectStreams) Elements() []PdfObject {
	if streams == nil {
		return nil
	}
	return streams.vec
}

// GetReaderAt returns the source the parser reads the document through. For
// a parser returned by GetPreviousRevisionParser it is the section of the
// parent document holding that revision.
func (parser *PdfParser) GetReaderAt() io.ReaderAt { return parser.reader }

var eofRegexp = regexp.MustCompile("%%EOF?")

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

// DecodeStream decodes a DCT encoded stream and returns the result as a
// slice of bytes.
func (dct *DCTEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return dct.DecodeBytes(streamObj.Stream)
}

// Close closes the underlying source if it is an io.Closer.
func (cursor *readerAtCursor) Close() error {
	if closer, ok := cursor.source.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Write outputs the object as it is to be written to file.
func (streams *PdfObjectStreams) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(streams.ObjectNumber, 10))
		buf.WriteString(" 0 R")
	})
}

var xrefLineRegexp = regexp.MustCompile("[\\r\\n]\\s*(xref)\\s*[\\r\\n]")

func (parser *PdfParser) trailerTailOffset(start, end int64) (int64, bool) {
	if end <= start {
		return 0, false
	}
	readerAtCursor := newReaderAtCursor(parser.reader, end, start)
	parser.skipSpaces(readerAtCursor)
	if data, _ := readerAtCursor.Peek(6); string(data) == "endobj" {
		readerAtCursor.Discard(6)
	}
	if err := parser.skipComments(readerAtCursor); err != nil {
		return 0, false
	}
	if data, err := readerAtCursor.Peek(9); err != nil || string(data) != "startxref" {
		return 0, false
	}
	readerAtCursor.Discard(9)
	if skipped, err := parser.skipSpaces(readerAtCursor); err != nil || skipped == 0 {
		return 0, false
	}
	var data []byte
	for {
		b, err := readerAtCursor.ReadByte()
		if err != nil {
			break
		}
		if b < '0' || b > '9' {
			readerAtCursor.UnreadByte()
			break
		}
		if len(data) == 19 {
			return 0, false
		}
		data = append(data, b)
	}
	offset, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0, false
	}
	for {
		b, err2 := readerAtCursor.ReadByte()
		if err2 != nil {
			return offset, true
		}
		if !IsWhiteSpace(b) {
			return 0, false
		}
	}
}

// GetTrailer returns the PDFs trailer dictionary. The trailer dictionary is typically the starting point for a PDF,
// referencing other key objects that are important in the document structure.
func (parser *PdfParser) GetTrailer() *PdfObjectDictionary { return parser.trailer }

// ParseIndirectObjectAt parses an indirect object starting at the given byte
// offset in the source. Returns the indirect object (*PdfIndirectObject) or
// the stream object (*PdfObjectStream).
func (parser *PdfParser) ParseIndirectObjectAt(offset int64) (PdfObject, error) {
	return parser.parseIndirectObject(parser.newCursor(offset), map[int]struct{}{})
}

// UpdateParams updates the parameter values of the encoder.
func (runLength *RunLengthEncoder) UpdateParams(params *PdfObjectDictionary) {}

const stdCryptFilterName = "StdCF"

// SetPredictor sets the predictor function.  Specify the number of columns per row.
// The columns indicates the number of samples per row.
// Used for grouping data together for compression.
func (flate *FlateEncoder) SetPredictor(columns int) { flate.Predictor = 11; flate.Columns = columns }
func newBitReader(data []byte) *bitReader            { return &bitReader{data: data} }

// EncodeBytes encodes data into ASCII85 encoded format.
func (ascii85 *ASCII85Encoder) EncodeBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	for i := 0; i < len(data); i += 4 {
		b := data[i]
		count := 1
		b2 := byte(0)
		if i+1 < len(data) {
			b2 = data[i+1]
			count++
		}
		b3 := byte(0)
		if i+2 < len(data) {
			b3 = data[i+2]
			count++
		}
		b4 := byte(0)
		if i+3 < len(data) {
			b4 = data[i+3]
			count++
		}
		word := (uint32(b) << 24) | (uint32(b2) << 16) | (uint32(b3) << 8) | uint32(b4)
		if word == 0 {
			buf.WriteByte('z')
		} else {
			group := ascii85.base256Tobase85(word)
			for _, c := range group[:count+1] {
				buf.WriteByte(c + '!')
			}
		}
	}
	buf.WriteString("~>")
	return buf.Bytes(), nil
}
func (dct *DCTEncoder) setDecodeLimits(limits decodeLimits) { dct.limits = limits }
func defaultCryptFilters(length int) cryptFilters {
	return cryptFilters{stdCryptFilterName: crypt.NewFilterV2(length)}
}

// GetCrypter returns the PdfCrypt instance which has information about the PDFs encryption.
func (parser *PdfParser) GetCrypter() *PdfCrypt { return parser.crypter }

func applyDecodeLimits(encoder StreamEncoder, limits decodeLimits) {
	if limited, ok := encoder.(limitedDecoder); ok {
		limited.setDecodeLimits(limits)
	}
}

// DecodeBytes decodes byte array with ASCII85. 5 ASCII characters -> 4 raw binary bytes
func (ascii85 *ASCII85Encoder) DecodeBytes(encoded []byte) ([]byte, error) {
	var data []byte
	remaining := ascii85.limits.remaining()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ASCII85 Decode")
	}
	pos := 0
	ok := false
	for pos < len(encoded) && !ok {
		group := [5]byte{0, 0, 0, 0, 0}
		skipped := 0
		idx := 0
		outLen := 4
		for idx < 5+skipped {
			if pos+idx == len(encoded) {
				break
			}
			b := encoded[pos+idx]
			if IsWhiteSpace(b) {
				skipped++
				idx++
				continue
			} else if b == '~' && pos+idx+1 < len(encoded) && encoded[pos+idx+1] == '>' {
				outLen = max((idx-skipped)-1, 0)
				ok = true
				break
			} else if b >= '!' && b <= 'u' {
				b -= '!'
			} else if b == 'z' && idx-skipped == 0 {
				outLen = 4
				idx++
				break
			} else {
				common.Log.Error("Failed decoding, invalid code")
				return nil, errors.New("invalid code encountered")
			}
			group[idx-skipped] = b
			idx++
		}
		pos += idx
		for i := outLen + 1; i < 5; i++ {
			group[i] = 84
		}
		word := uint32(group[0])*85*85*85*85 + uint32(group[1])*85*85*85 + uint32(group[2])*85*85 + uint32(group[3])*85 + uint32(group[4])
		wordBytes := []byte{byte((word >> 24) & 0xff), byte((word >> 16) & 0xff), byte((word >> 8) & 0xff), byte(word & 0xff)}
		if int64(len(data)+outLen) > remaining {
			return nil, ascii85.limits.exceeded(StreamEncodingFilterNameASCII85, remaining)
		}
		data = append(data, wordBytes[:outLen]...)
	}
	ascii85.limits.charge(int64(len(data)))
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ASCII85, encoded: % X", encoded)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ASCII85, decoded: % X", data)
	}
	return data, nil
}

// ParserOpts defines configuration options for Parser.
// These options allow setting customization flags to control parsing behaviors.
type ParserOpts struct {
	// RelaxedMode enables tolerant parsing by ignoring certain non-critical PDF errors.
	// When set to true, the parser will attempt to continue processing despite certain
	// structural errors such as:
	// - Missing or invalid /XRefStm entries
	// - Non-/Page or non-/Pages objects found in the page tree (/Kids array)
	RelaxedMode bool

	// AutoRepairXrefTable enables automatic repair of the xref table when it is found to be malformed or missing objects.
	// The check runs when the parser is constructed; for encrypted files it is
	// deferred to Decrypt, so a file that is never decrypted is never repaired.
	AutoRepairXrefTable bool

	// MaxDecodedStreamSize is the maximum number of bytes a single stream, or a single filter
	// stage of a filter chain, may decode to. 0 selects DefaultMaxDecodedStreamSize.
	// Decoding past the limit fails with ErrDecodedSizeLimitExceeded.
	// Not covered: decoders registered with RegisterCustomStreamEncoder, and inline images,
	// which have no stream object and use DefaultMaxDecodedStreamSize. JBIG2 is checked against
	// the page size its page information segment declares; a striped page that leaves its
	// height open is checked after decoding, bounded by the decoder's 256 MiB per-bitmap cap.
	MaxDecodedStreamSize int64

	// MaxDecodedTotalSize is the maximum number of bytes that stream decodes through the parser
	// and its revisions may produce together. Every Flate, LZW, RunLength, ASCIIHex, ASCII85,
	// CCITTFax, DCT and JBIG2 decode counts, including repeated decodes of the same stream, each stage of
	// a filter chain, and the object, cross-reference and hint streams the parser decodes itself.
	// Concurrent decodes each check the budget when they start, so together they can exceed it
	// by up to one MaxDecodedStreamSize each. 0 disables the limit.
	MaxDecodedTotalSize int64
}

func checkFirstXrefPrev(present bool, prevOffset, endOfFirstPage, fileSize int64) *LinearizationViolation {
	ok := !present || prevOffset <= 0 || (endOfFirstPage > 0 && prevOffset < endOfFirstPage) || (fileSize > 0 && prevOffset >= fileSize)
	if ok {
		return &LinearizationViolation{Code: LinErrPrevChainBroken, Severity: SeverityError, Message: fmt.Sprintf("first-xref /Prev (present=%v, offset=%d) does not point past the first-page section into the file body (/E %d, size %d)", present, prevOffset, endOfFirstPage, fileSize)}
	}
	return nil
}

// MakeDict creates and returns an empty PdfObjectDictionary.
func MakeDict() *PdfObjectDictionary {
	dict := &PdfObjectDictionary{}
	dict.dict = map[PdfObjectName]PdfObject{}
	dict.keys = []PdfObjectName{}
	return dict
}

func (parser *PdfParser) rebuildXrefTableTopDown() error {
	xrefs, err := parser.repairRebuildXrefsTopDown()
	if err != nil {
		common.Log.Debug("ERROR: Failed xref rebuild repair (%s)", err)
		return err
	}
	if parser.currentXrefs() == parser.repairedXrefs.Load() {
		return nil
	}
	rebuilt := XrefTable{ObjectMap: make(map[int]XrefObject, len(xrefs.ObjectMap))}
	maps.Copy(rebuilt.ObjectMap, xrefs.ObjectMap)
	for i, xrefObj := range parser.currentXrefs().ObjectMap {
		if xrefObj.XType != XrefTypeObjectStream {
			continue
		}
		osXref, ok := xrefs.ObjectMap[xrefObj.OsObjNumber]
		if !ok {
			continue
		}
		if existing, ok2 := rebuilt.ObjectMap[i]; !ok2 || osXref.Offset > existing.Offset {
			rebuilt.ObjectMap[i] = xrefObj
		}
	}
	if latest := parser.currentXrefs(); maps.Equal(rebuilt.ObjectMap, latest.ObjectMap) {
		common.Log.Debug("Repaired xref table unchanged")
		parser.repairedXrefs.Store(latest)
		return nil
	}
	parser.setXrefs(rebuilt)
	parser.repairedXrefs.Store(parser.currentXrefs())
	parser.clearObjCache()
	common.Log.Debug("Repaired xref table built")
	return nil
}

// GoImageToJBIG2 creates a binary image on the base of 'i' golang image.Image.
// If the image is not a black/white image then the function converts provided input into
// JBIG2Image with 1bpp. For non grayscale images the function performs the conversion to the grayscale temp image.
// Then it checks the value of the gray image value if it's within bounds of the black white threshold.
// This 'bwThreshold' value should be in range (0.0, 1.0). The threshold checks if the grayscale pixel (uint) value
// is greater or smaller than 'bwThreshold' * 255. Pixels inside the range will be white, and the others will be black.
// If the 'bwThreshold' is equal to -1.0 - JB2ImageAutoThreshold then it's value would be set on the base of
// it's histogram using Triangle method. For more information go to:
//
//	https://www.mathworks.com/matlabcentral/fileexchange/28047-gray-image-thresholding-using-the-triangle-method
func GoImageToJBIG2(i image.Image, bwThreshold float64) (*JBIG2Image, error) {
	const procGoImageToJBIG2 = "GoImageToJBIG2"
	if i == nil {
		return nil, jbig2errors.Error(procGoImageToJBIG2, "image 'i' not defined")
	}
	var (
		b   uint8
		img imageutil.Image
		err error
	)
	if bwThreshold == JB2ImageAutoThreshold {
		img, err = imageutil.MonochromeConverter.Convert(i)
	} else if bwThreshold > 1.0 || bwThreshold < 0.0 {
		return nil, jbig2errors.Error(procGoImageToJBIG2, "provided threshold is not in a range {0.0, 1.0}")
	} else {
		b = uint8(255 * bwThreshold)
		img, err = imageutil.MonochromeThresholdConverter(b).Convert(i)
	}
	if err != nil {
		return nil, err
	}
	return newJBIG2ImageFromImage(img), nil
}

// ParserMetadata is the parser based metadata information about document.
// The data here could be used on document verification.
type ParserMetadata struct {
	headerPosition             int
	eolAfterHeader             bool
	headerCommentBytes         [4]byte
	dataAfterEOF               bool
	oddLengthHexStrings        bool
	invalidHexRunes            bool
	nonConformantStream        bool
	invalidSubsectionHeader    bool
	invalidSeparationAfterXRef bool
}

// Get returns the PdfObject corresponding to the specified key.
// Returns a nil value if the key is not set.
func (dict *PdfObjectDictionary) Get(key PdfObjectName) PdfObject {
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	obj, ok := dict.dict[key]
	if !ok {
		return nil
	}
	return obj
}

func newCompliancePdfParser(r io.ReaderAt, fileSize int64, decodedTotal *atomic.Int64) (parser *PdfParser, err error) {
	parser = &PdfParser{decodedTotal: decodedTotal, reader: r, fileSize: fileSize, compliance: true, revisionParsers: make(map[*PdfParser]*PdfParser)}
	if err = parser.parseDetailedHeader(); err != nil {
		return nil, err
	}
	if parser.trailer, err = parser.loadXrefs(); err != nil {
		common.Log.Debug("ERROR: Failed to load xref table! %s", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Trailer: %s", parser.trailer)
	}
	if len(parser.currentXrefs().ObjectMap) == 0 {
		return nil, fmt.Errorf("empty XREF table - Invalid")
	}
	linearizedDictionary, err := parser.parseLinearizedDictionary()
	if err != nil {
		return nil, err
	}
	if linearizedDictionary != nil {
		ok, err2 := parser.checkLinearizedInformation(linearizedDictionary)
		if err2 != nil {
			return nil, err2
		}
		if ok {
			parser.linearized = true
			parser.linearizationDict = linearizedDictionary
			parser.linearization = parser.buildLinearizationInfo(linearizedDictionary)
		}
	}
	parser.numRevisions = len(parser.xrefOffsets)
	if parser.linearized && parser.numRevisions != 0 {
		parser.numRevisions--
	}
	parser.revisions = make([]*PdfParser, parser.numRevisions)
	return parser, nil
}

// Len returns the number of elements in the array.
func (arr *PdfObjectArray) Len() int {
	if arr == nil {
		return 0
	}
	return len(arr.elements)
}

// LinearizationInfo holds the values from the linearization parameter
// dictionary (ISO 32000-2 Annex F, Table F.1) together with any decoded hint
// tables.
//
// Field correspondence:
//
//	FileLength       -> /L  (length of the entire file in bytes)
//	HintOffsets      -> /H  ([primary_offset primary_length] or [primary_offset primary_length secondary_offset secondary_length])
//	FirstPageObjNum  -> /O  (object number of the first page's page object)
//	EndOfFirstPage   -> /E  (offset of the end of the first page)
//	NumPages         -> /N  (number of pages in the document)
//	MainXrefOffset   -> /T  (offset of the first entry in the main cross-reference table)
//	FirstPageNum     -> /P  (page number of the first page; default 0)
type LinearizationInfo struct {
	FileLength      int64
	HintOffsets     []HintRange
	FirstPageObjNum int64
	EndOfFirstPage  int64
	NumPages        int
	MainXrefOffset  int64
	FirstPageNum    int
	HintTables      *HintTables
}

// JPXEncoder implements JPX encoder/decoder (dummy, for now)
// FIXME: implement
type JPXEncoder struct{}

func checkParamDictEnd(firstXrefOffset int64) *LinearizationViolation {
	if firstXrefOffset <= 0 || firstXrefOffset > maxLinearizationParamDictEnd {
		return &LinearizationViolation{Code: LinErrParamDictNotFirstObject, Severity: SeverityError, Message: fmt.Sprintf("linearization parameter dictionary does not fit within the first %d bytes (ends at %d)", maxLinearizationParamDictEnd, firstXrefOffset)}
	}
	return nil
}

// DecodeStream decodes a FlateEncoded stream object and give back decoded bytes.
func (flate *FlateEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("FlateDecode stream")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Predictor: %d", flate.Predictor)
	}
	bytes, err := flate.DecodeBytes(streamObj.Stream)
	if err != nil {
		return nil, err
	}
	bytes, err = flate.postDecodePredict(bytes)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

func (parser *PdfParser) validateFirstPageAndCount(linInfo *LinearizationInfo) []LinearizationViolation {
	var violations []LinearizationViolation
	leafCount, declaredCount, firstPageIndex, err := parser.walkPageTree()
	if err != nil {
		violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectInvalid, Severity: SeverityError, Message: fmt.Sprintf("unable to walk the page tree: %v", err)})
		return violations
	}
	if int64(leafCount) != int64(linInfo.NumPages) {
		violations = append(violations, LinearizationViolation{Code: LinErrPageCountMismatch, Severity: SeverityWarning, Message: fmt.Sprintf("/N (%d) does not match the page count from the page tree (%d)", linInfo.NumPages, leafCount)})
	}
	if violation := checkPageCount(declaredCount, int64(leafCount)); violation != nil {
		violations = append(violations, *violation)
	}
	ok := false
	if byNumber, err2 := parser.LookupByNumber(int(linInfo.FirstPageObjNum)); err2 == nil {
		if dict, hasDict := GetDict(byNumber); hasDict {
			if typeName, hasTypeName := GetName(dict.Get("Type")); hasTypeName && string(*typeName) == "Page" {
				ok = true
			}
		}
	}
	if !ok {
		violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectInvalid, Severity: SeverityError, Message: fmt.Sprintf("/O (%d) does not resolve to a /Type /Page object", linInfo.FirstPageObjNum)})
		return violations
	}
	if firstPageIndex >= 0 && linInfo.FirstPageObjNum != firstPageIndex {
		violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectNotFirstPage, Severity: SeverityError, Message: fmt.Sprintf("/O (%d) is not the first page in the page tree (first page is object %d)", linInfo.FirstPageObjNum, firstPageIndex)})
	}
	if offset, ok2 := parser.objectFileOffset(int(linInfo.FirstPageObjNum)); ok2 {
		if linInfo.EndOfFirstPage > 0 && offset >= linInfo.EndOfFirstPage {
			violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectOutsideSection, Severity: SeverityError, Message: fmt.Sprintf("first page object %d at offset %d is not within the first-page section (/E %d)", linInfo.FirstPageObjNum, offset, linInfo.EndOfFirstPage)})
		}
	}
	return violations
}

// GetStringBytes is like GetStringVal except that it returns the string as a []byte.
// It is for convenience.
func GetStringBytes(obj PdfObject) (data []byte, ok bool) {
	str, ok := TraceToDirectObject(obj).(*PdfObjectString)
	if ok {
		return str.Bytes(), true
	}
	return
}

// GetFilterName returns the name of the encoding filter.
func (flate *FlateEncoder) GetFilterName() string { return StreamEncodingFilterNameFlate }

// MultiEncoder supports serial encoding.
type MultiEncoder struct{ encoders []StreamEncoder }

// ParseDictAt reads and parses a PDF dictionary object enclosed with '<<' and '>>'
// starting at the given offset.
func (parser *PdfParser) ParseDictAt(offset int64) (*PdfObjectDictionary, error) {
	return parser.parseDict(parser.newCursor(offset), 0)
}

func (crypt *PdfCrypt) makeKey(filterName string, objNum, genNum uint32, fileKey []byte) ([]byte, error) {
	filter, ok := crypt.cryptFilters[filterName]
	if !ok {
		return nil, fmt.Errorf("unknown crypt filter (%s)", filterName)
	}
	return filter.MakeKey(objNum, genNum, fileKey)
}

var bytePoolSizes = [...]int{poolSize16K, poolSize64K, poolSize256K, poolSize1M, poolSize4M, poolSize16M}

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

// IsAuthenticated returns true if the PDF has already been authenticated for accessing.
func (parser *PdfParser) IsAuthenticated() bool { return parser.crypter.authenticated }

// Decrypt an object with specified key. For numbered objects,
// the key argument is not used and a new one is generated based
// on the object and generation number.
// Traverses through all the subobjects (recursive).
//
// Does not look up references..  That should be done prior to calling.
func (crypt *PdfCrypt) Decrypt(obj PdfObject, parentObjNum, parentGenNum int64) error {
	if crypt.isDecrypted(obj) {
		return nil
	}
	switch v := obj.(type) {
	case *PdfIndirectObject:
		crypt.decryptedObjects[v] = true
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decrypting indirect %d %d obj!", v.ObjectNumber, v.GenerationNumber)
		}
		objectNumber := v.ObjectNumber
		generationNumber := v.GenerationNumber
		err := crypt.Decrypt(v.PdfObject, objectNumber, generationNumber)
		if err != nil {
			return err
		}
		return nil
	case *PdfObjectStream:
		crypt.decryptedObjects[v] = true
		pdfObjectDictionary := v.PdfObjectDictionary
		if crypt.stdEncrypt.R != 5 {
			if typeName, ok := pdfObjectDictionary.Get("Type").(*PdfObjectName); ok && *typeName == "XRef" {
				return nil
			}
		}
		objectNumber := v.ObjectNumber
		generationNumber := v.GenerationNumber
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decrypting stream %d %d !", objectNumber, generationNumber)
		}
		filterName := stdCryptFilterName
		if crypt.encrypt.V >= 4 {
			filterName = crypt.streamFilter
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("this.streamFilter = %s", crypt.streamFilter)
			}
			if filter, ok := pdfObjectDictionary.Get("Filter").(*PdfObjectArray); ok {
				if name, hasName := GetName(filter.Get(0)); hasName {
					if *name == "Crypt" {
						filterName = "Identity"
						if decodeParms, isDict := pdfObjectDictionary.Get("DecodeParms").(*PdfObjectDictionary); isDict {
							if filterName2, isName := decodeParms.Get("Name").(*PdfObjectName); isName {
								if _, ok2 := crypt.cryptFilters[string(*filterName2)]; ok2 {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Using stream filter %s", *filterName2)
									}
									filterName = string(*filterName2)
								}
							}
						}
					}
				}
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("with %s filter", filterName)
			}
			if filterName == "Identity" {
				return nil
			}
		}
		err := crypt.Decrypt(pdfObjectDictionary, objectNumber, generationNumber)
		if err != nil {
			return err
		}
		key, err := crypt.makeKey(filterName, uint32(objectNumber), uint32(generationNumber), crypt.encryptionKey)
		if err != nil {
			return err
		}
		v.Stream, err = crypt.decryptBytes(v.Stream, filterName, key)
		if err != nil {
			return err
		}
		pdfObjectDictionary.Set("Length", MakeInteger(int64(len(v.Stream))))
		return nil
	case *PdfObjectString:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Decrypting string!")
		}
		filterName := stdCryptFilterName
		if crypt.encrypt.V >= 4 {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("with %s filter", crypt.stringFilter)
			}
			if crypt.stringFilter == "Identity" {
				return nil
			}
			filterName = crypt.stringFilter
		}
		key, err := crypt.makeKey(filterName, uint32(parentObjNum), uint32(parentGenNum), crypt.encryptionKey)
		if err != nil {
			return err
		}
		str := v.Str()
		data := make([]byte, len(str))
		for i := 0; i < len(str); i++ {
			data[i] = str[i]
		}
		if len(data) > 0 {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Decrypt string: %s : % x", data, data)
			}
			data, err = crypt.decryptBytes(data, filterName, key)
			if err != nil {
				return err
			}
		}
		v.val = string(data)
		return nil
	case *PdfObjectArray:
		for _, elem := range v.Elements() {
			err := crypt.Decrypt(elem, parentObjNum, parentGenNum)
			if err != nil {
				return err
			}
		}
		return nil
	case *PdfObjectDictionary:
		ok := false
		if typeVal := v.Get("Type"); typeVal != nil {
			name, isName := typeVal.(*PdfObjectName)
			if isName && *name == "Sig" {
				ok = true
			}
		}
		for _, key := range v.Keys() {
			child := v.Get(key)
			if ok && string(key) == "Contents" {
				continue
			}
			if string(key) != "Parent" && string(key) != "Prev" && string(key) != "Last" {
				err := crypt.Decrypt(child, parentObjNum, parentGenNum)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
	return nil
}

// NewParserWithOpts creates a parser with the given options. The source's
// byte length is auto-detected. For exotic io.ReaderAt sources that can't be
// auto-sized, use NewParserWithOptsAt.
func NewParserWithOpts(r io.ReaderAt, opts ParserOpts) (*PdfParser, error) {
	size, err := SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewParserWithOptsAt(r, size, opts)
}

func newCCITTFaxEncoderFromStream(streamObj *PdfObjectStream, decodeParams *PdfObjectDictionary) (*CCITTFaxEncoder, error) {
	ccittFaxEncoder := NewCCITTFaxEncoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return ccittFaxEncoder, nil
	}
	if decodeParams == nil {
		toDirectObject := TraceToDirectObject(pdfObjectDictionary.Get("DecodeParms"))
		if toDirectObject != nil {
			switch v := toDirectObject.(type) {
			case *PdfObjectDictionary:
				decodeParams = v
			case *PdfObjectArray:
				if v.Len() == 1 {
					if dict, ok := GetDict(v.Get(0)); ok {
						decodeParams = dict
					}
				}
			default:
				common.Log.Error("DecodeParms not a dictionary %#v", toDirectObject)
				return nil, errors.New("invalid DecodeParms")
			}
		}
		if decodeParams == nil {
			common.Log.Error("DecodeParms not specified %#v", toDirectObject)
			return nil, errors.New("invalid DecodeParms")
		}
	}
	if kN, err := GetNumberAsInt64(decodeParams.Get("K")); err == nil {
		ccittFaxEncoder.K = int(kN)
	}
	if columns, err := GetNumberAsInt64(decodeParams.Get("Columns")); err == nil {
		ccittFaxEncoder.Columns = int(columns)
	} else {
		ccittFaxEncoder.Columns = 1728
	}
	if blackIs1, err := GetNumberAsInt64(decodeParams.Get("BlackIs1")); err == nil {
		ccittFaxEncoder.BlackIs1 = blackIs1 > 0
	} else {
		if blackIs12, ok := GetBoolVal(decodeParams.Get("BlackIs1")); ok {
			ccittFaxEncoder.BlackIs1 = blackIs12
		} else {
			if decode, hasDecode := GetArray(decodeParams.Get("Decode")); hasDecode {
				nums, err2 := decode.ToIntegerArray()
				if err2 == nil {
					ccittFaxEncoder.BlackIs1 = nums[0] == 1 && nums[1] == 0
				}
			}
		}
	}
	if encodedByteAlign, err := GetNumberAsInt64(decodeParams.Get("EncodedByteAlign")); err == nil {
		ccittFaxEncoder.EncodedByteAlign = encodedByteAlign > 0
	} else {
		if byteAlign, ok := GetBoolVal(decodeParams.Get("EncodedByteAlign")); ok {
			ccittFaxEncoder.EncodedByteAlign = byteAlign
		}
	}
	if endOfLine, err := GetNumberAsInt64(decodeParams.Get("EndOfLine")); err == nil {
		ccittFaxEncoder.EndOfLine = endOfLine > 0
	} else {
		if endOfLine2, ok := GetBoolVal(decodeParams.Get("EndOfLine")); ok {
			ccittFaxEncoder.EndOfLine = endOfLine2
		}
	}
	if rows, err := GetNumberAsInt64(decodeParams.Get("Rows")); err == nil {
		ccittFaxEncoder.Rows = int(rows)
	}
	ccittFaxEncoder.EndOfBlock = true
	if endOfBlock, err := GetNumberAsInt64(decodeParams.Get("EndOfBlock")); err == nil {
		ccittFaxEncoder.EndOfBlock = endOfBlock > 0
	} else {
		if endOfBlock2, ok := GetBoolVal(decodeParams.Get("EndOfBlock")); ok {
			ccittFaxEncoder.EndOfBlock = endOfBlock2
		}
	}
	if damagedRowsBeforeError, err := GetNumberAsInt64(decodeParams.Get("DamagedRowsBeforeError")); err != nil {
		ccittFaxEncoder.DamagedRowsBeforeError = int(damagedRowsBeforeError)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", decodeParams.String())
	}
	return ccittFaxEncoder, nil
}

// GetFilterName returns the names of the underlying encoding filters,
// separated by spaces.
// Note: This is just a string, should not be used in /Filter dictionary entry. Use GetFilterArray for that.
// TODO(v4): Refactor to GetFilter() which can be used for /Filter (either Name or Array), this can be
// renamed to String() as a pretty string to use in debugging etc.
func (multi *MultiEncoder) GetFilterName() string {
	var name strings.Builder
	for i, encoder := range multi.encoders {
		name.WriteString(encoder.GetFilterName())
		if i < len(multi.encoders)-1 {
			name.WriteString(" ")
		}
	}
	return name.String()
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
// Has the Filter set.  Some other parameters are generated elsewhere.
func (dct *DCTEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(dct.GetFilterName()))
	return dict
}

// Direct traces the indirect object to the direct object it contains.
func (_cfbfbgd *PdfIndirectObject) Direct() PdfObject { return TraceToDirectObject(_cfbfbgd) }

func (crypt *PdfCrypt) cryptFilterDict(encryptDict *PdfObjectDictionary, filterName string) *PdfObjectDictionary {
	if filterName == "" {
		return nil
	}
	traceDirect := func(obj PdfObject) PdfObject {
		obj = TraceToDirectObject(obj)
		if ref, ok := obj.(*PdfObjectReference); ok && crypt.parser != nil {
			if byReference, err := crypt.parser.LookupByReference(*ref); err == nil {
				obj = TraceToDirectObject(byReference)
			}
		}
		return obj
	}
	cfDict, ok := traceDirect(encryptDict.Get("CF")).(*PdfObjectDictionary)
	if !ok {
		return nil
	}
	dict, _ := traceDirect(cfDict.Get(PdfObjectName(filterName))).(*PdfObjectDictionary)
	return dict
}

// ThumbnailHintTable corresponds to the optional thumbnail hint table
// (ISO 32000-2 Annex F.4.6). The reader retains the raw header so callers
// can use it; per-page entry decoding is best-effort.
type ThumbnailHintTable struct {
	FirstThumbnailObjNum        uint32
	FirstThumbnailOffset        uint32
	NumThumbnails               uint32
	NumSharedThumbnails         uint32
	BitsPerSharedRef            uint16
	BitsPerObjectCountDelta     uint16
	LeastObjectsInThumbnail     uint32
	BitsPerThumbnailLengthDelta uint16
	LeastThumbnailLength        uint32
}

// DecodeBytes decodes a multi-encoded slice of bytes by passing it through the
// DecodeBytes method of the underlying encoders.
func (multi *MultiEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	data := encoded
	var err error
	for _, encoder := range multi.encoders {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Multi Encoder Decode: Applying Filter: %v %T", encoder, encoder)
		}
		data, err = encoder.DecodeBytes(data)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func init() {
	lazystream.Take = func(owner, target any) ([]byte, bool) {
		crypt, ok := owner.(*PdfCrypt)
		stream, isStream := target.(*PdfObjectStream)
		if !ok || !isStream || crypt == nil {
			return nil, false
		}
		return crypt.takeLazyStreamData(stream)
	}
	lazystream.Drop = func(v any) {
		if crypt, ok := v.(*PdfCrypt); ok && crypt != nil {
			crypt.dropLazyStreamData()
		}
	}
}

// DecodeGlobals decodes 'encoded' byte stream and returns their Globally defined segments ('Globals').
func (jbig22 *JBIG2Encoder) DecodeGlobals(encoded []byte) (jbig2.Globals, error) {
	return jbig2.DecodeGlobals(encoded)
}

func (limits decodeLimits) streamLimit() int64 {
	if limits.maxStream <= 0 {
		return DefaultMaxDecodedStreamSize()
	}
	return limits.maxStream
}

// String returns a string representation of `name`.
func (name *PdfObjectName) String() string { return string(*name) }

// GetFilterName returns the name of the encoding filter.
func (enc *LZWEncoder) GetFilterName() string { return StreamEncodingFilterNameLZW }

// Decoded returns the PDFDocEncoding or UTF-16BE decoded string contents.
// UTF-16BE is applied when the first two bytes are 0xFE, 0XFF, otherwise decoding of
// PDFDocEncoding is performed.
func (str *PdfObjectString) Decoded() string {
	if str == nil {
		return ""
	}
	data := []byte(str.val)
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return strutils.UTF16ToString(data[2:])
	}
	return strutils.PDFDocEncodingToString(data)
}

// HintRange is a (file offset, length) pair from the linearization
// dictionary's /H array describing where the hint stream(s) live.
type HintRange struct {
	Offset int64
	Length int64
}

func init() { maxDecodedStreamSize.Store(defaultMaxDecodedStreamSize) }

// Bytes returns the PdfObjectString content as a []byte array.
func (str *PdfObjectString) Bytes() []byte { return []byte(str.val) }

func (parser *PdfParser) checkAndRepairXrefTable(trailer *PdfObjectDictionary) error {
	err := parser.xrefTableHealthCheck(trailer)
	if err == nil {
		return nil
	}
	common.Log.Debug("Xref table health check failed: %v, attempt repair", err)
	if err2 := parser.rebuildXrefTableTopDown(); err2 != nil {
		common.Log.Debug("Failed to rebuild xrefs: %v, file unrecoverable", err2)
		return err2
	}
	return nil
}

var ErrCertificateRequired = errors.New("document is encrypted with a public-key security handler; a recipient certificate and private key are required")

func (parser *PdfParser) rebuildXrefTable(visited map[int]struct{}) error {
	xrefs := XrefTable{}
	xrefs.ObjectMap = map[int]XrefObject{}
	m := map[int]int{}
	nums := make([]int, 0, len(parser.currentXrefs().ObjectMap))
	for i := range parser.currentXrefs().ObjectMap {
		nums = append(nums, i)
	}
	sort.Ints(nums)
	ok := true
	var delta int
	for i, oldNum := range nums {
		xrefObj := parser.currentXrefs().ObjectMap[oldNum]
		var (
			obj PdfObject
			err error
		)
		if _, ok2 := visited[oldNum]; ok2 {
			obj, _, err = parser.lookupByNumber(oldNum, false, visited)
		} else {
			obj, _, err = parser.lookupByNumberWrapperInternal(oldNum, false, visited)
		}
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("ERROR: Unable to look up object (%s)", err)
				common.Log.Debug("ERROR: Xref table completely broken - attempting to repair ")
			}
			return parser.rebuildXrefTableTopDown()
		}
		newNum, newGen, err := indirectObjectIDs(obj)
		if err != nil {
			return err
		}
		if _, ok2 := xrefs.ObjectMap[int(newNum)]; ok2 {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Xref renumbering collision: entries %d and %d both resolve to object %d - rebuilding top-down", m[int(newNum)], oldNum, newNum)
			}
			return parser.rebuildXrefTableTopDown()
		}
		if i == 0 {
			delta = int(newNum) - oldNum
		} else if int(newNum)-oldNum != delta {
			ok = false
		}
		xrefObj.ObjectNumber = int(newNum)
		xrefObj.Generation = int(newGen)
		xrefs.ObjectMap[int(newNum)] = xrefObj
		m[int(newNum)] = oldNum
	}
	if !ok {
		for _, num := range nums {
			if _, ok2 := xrefs.ObjectMap[num]; ok2 {
				continue
			}
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Xref renumbering lost object %d - rebuilding top-down", num)
			}
			return parser.rebuildXrefTableTopDown()
		}
	}
	parser.setXrefs(xrefs)
	parser.clearObjCache()
	common.Log.Debug("New xref table built")
	logXrefTable(*parser.currentXrefs())
	return nil
}

const maxTraceDepth = 10

func normalizePredictorParams(columns, bitsPerComponent, colors int) (rowBytes, bytesPerPixel, bitsPerComp int, err error) {
	if err2 := checkPredictorDimensions(colors, columns); err2 != nil {
		return 0, 0, 0, err2
	}
	if bitsPerComponent == 0 {
		bitsPerComponent = 8
	}
	switch bitsPerComponent {
	case 1, 2, 4, 8, 16:
	default:
		return 0, 0, 0, fmt.Errorf("%w: predictor BitsPerComponent %d", ErrRangeError, bitsPerComponent)
	}
	bits := int64(colors) * int64(columns) * int64(bitsPerComponent)
	if bits > math.MaxInt32 {
		return 0, 0, 0, fmt.Errorf("%w: predictor row of %d bits", ErrRangeError, bits)
	}
	return int((bits + 7) / 8), max(1, (columns*bitsPerComponent+7)/8), bitsPerComponent, nil
}

// LZWEncoder provides LZW encoding/decoding functionality.
type LZWEncoder struct {
	Predictor        int
	BitsPerComponent int

	// For predictors
	Columns int
	Colors  int

	// LZW algorithm setting.
	EarlyChange int
	limits      decodeLimits
}
type eofMarker struct{ markerOffset, endOffset int64 }

// DecodeBytes decodes a slice of LZW encoded bytes and returns the result.
func (enc *LZWEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	reader := bytes.NewReader(encoded)
	var rc io.ReadCloser
	if enc.EarlyChange == 1 {
		rc = tifflzw.NewReader(reader, tifflzw.MSB, 8)
	} else {
		rc = lzw.NewReader(reader, lzw.MSB, 8)
	}
	defer rc.Close()
	all, err := enc.limits.readAll(StreamEncodingFilterNameLZW, rc)
	if err != nil {
		if err != io.ErrUnexpectedEOF || len(all) == 0 {
			ReleaseBytes(all)
			return nil, err
		}
		common.Log.Debug("WARN: LZW decoding error: %v. Output may be incorrect.", err)
	}
	enc.limits.charge(int64(len(all)))
	return all, nil
}

func (parser *PdfParser) checkFirstXrefPrevChain(linInfo *LinearizationInfo) *LinearizationViolation {
	dict, err := parser.FirstXrefTrailer()
	if err != nil || dict == nil {
		return &LinearizationViolation{Code: LinErrPrevChainBroken, Severity: SeverityError, Message: "unable to read the first xref trailer to verify the /Prev chain"}
	}
	prev := dict.Get("Prev")
	ok := prev != nil
	var prev2 int64
	if ok {
		if prevOffset, err2 := GetNumberAsInt64(prev); err2 == nil {
			prev2 = prevOffset
		} else {
			ok = false
		}
	}
	return checkFirstXrefPrev(ok, prev2, linInfo.EndOfFirstPage, parser.fileSize)
}

// HintTables holds the decoded hint stream tables from a linearized PDF.
// The Page Offset and Shared Object tables are mandatory; the rest are
// optional and may be nil if the producer did not emit them.
type HintTables struct {
	PageOffset       *PageOffsetHintTable
	SharedObject     *SharedObjectHintTable
	Thumbnail        *ThumbnailHintTable
	OutlineHierarchy *GenericHintTable
	ArticleThread    *GenericHintTable
	NamedDestination *GenericHintTable
	InteractiveForm  *GenericHintTable
	InformationDict  *GenericHintTable
	LogicalStructure *GenericHintTable
	PageLabel        *GenericHintTable
	Rendition        *GenericHintTable
	EmbeddedFile     *GenericHintTable
}
type (
	indirectPair struct{ first, second *PdfIndirectObject }
	objectStream struct {
		N       int
		data    []byte
		offsets map[int]int64
	}
)

const maxLinearizationParamDictEnd = 1024

func checkPredictorDimensions(columns, colors int) error {
	if columns < 0 || colors < 0 || int64(columns)*int64(colors) > math.MaxInt32 {
		return fmt.Errorf("%w: predictor Columns %d Colors %d", ErrRangeError, columns, colors)
	}
	return nil
}

// Clear resets the array to an empty state.
func (arr *PdfObjectArray) Clear() { arr.elements = []PdfObject{} }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (jpxEncoder *JPXEncoder) MakeDecodeParams() PdfObject { return nil }

func (parser *PdfParser) parseHintStreamIfPresent() error {
	if parser.linearization == nil || len(parser.linearization.HintOffsets) == 0 {
		return nil
	}
	hintRange := parser.linearization.HintOffsets[0]
	if hintRange.Length <= 0 {
		return nil
	}
	hintStreamObject, err := parser.parseHintStreamObject(hintRange.Offset)
	if err != nil {
		return fmt.Errorf("primary hint stream: %w", err)
	}
	stream, err := DecodeStream(hintStreamObject)
	if err != nil {
		return fmt.Errorf("primary hint stream decode: %w", err)
	}
	if len(parser.linearization.HintOffsets) > 1 {
		secondRange := parser.linearization.HintOffsets[1]
		if secondRange.Length > 0 {
			secondStream, err2 := parser.parseHintStreamObject(secondRange.Offset)
			if err2 == nil {
				if data, err3 := DecodeStream(secondStream); err3 == nil {
					stream = append(stream, data...)
				} else {
					common.Log.Debug("WARN: secondary hint stream decode failed: %v", err3)
				}
			} else {
				common.Log.Debug("WARN: secondary hint stream parse failed: %v", err2)
			}
		}
	}
	hintTables, err := parseHintTables(stream, hintStreamObject.PdfObjectDictionary, parser.linearization.NumPages)
	if err != nil {
		return err
	}
	parser.linearization.HintTables = hintTables
	return nil
}

// PdfObjectFloat represents the primitive PDF floating point numerical object.
type PdfObjectFloat float64

var numberPattern = regexp.MustCompile("^[\\+-.]*([0-9.]+)")

// GetStream returns the *PdfObjectStream represented by the PdfObject. On type mismatch the found bool flag is
// false and a nil pointer is returned.
func GetStream(obj PdfObject) (stream *PdfObjectStream, ok bool) {
	obj = ResolveReference(obj)
	stream, ok = obj.(*PdfObjectStream)
	return stream, ok
}

var errEOFNotFound = errors.New("EOF not found")

// PdfCrypt provides PDF encryption/decryption support.
// The PDF standard supports encryption of strings and streams (Section 7.6).
type PdfCrypt struct {
	encrypt          encryptDict
	stdEncrypt       security.StdEncryptDict
	pubSec           *security.PubSecEncryptDict
	recipientFilter  string
	permissions      security.Permissions
	id0              string
	encryptionKey    []byte
	decryptedObjects map[PdfObject]bool
	encryptedObjects map[PdfObject]bool
	authenticated    bool
	cryptFilters     cryptFilters
	streamFilter     string
	stringFilter     string
	parser           *PdfParser
	encryptedObjNums map[int]struct{}
	lazyStreamData   map[*PdfObjectStream][]byte
}

// Write outputs the object as it is to be written to file.
func (name *PdfObjectName) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		if len(*name) > 127 {
			common.Log.Debug("ERROR: Name too long (%s)", *name)
		}
		buf.WriteString("/")
		for i := 0; i < len(*name); i++ {
			b := (*name)[i]
			if !IsPrintable(b) || b == '#' || IsDelimiter(b) {
				fmt.Fprintf(buf, "#%.2x", b)
			} else {
				buf.WriteByte(b)
			}
		}
	})
}

func (parser *PdfParser) loadXrefs() (trailer *PdfObjectDictionary, err error) {
	parser.setXrefs(XrefTable{ObjectMap: map[int]XrefObject{}})
	parser.objStreams = make(objectStreams)
	defer func() {
		if err == nil {
			parser.setXrefs(*parser.currentXrefs())
		}
	}()
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("fsize: %d", parser.fileSize)
	}
	cursor := parser.newCursor(0)
	err = parser.seekToEOFMarker(cursor, parser.fileSize)
	if err != nil {
		common.Log.Debug("Failed seek to eof marker: %v", err)
		return nil, err
	}
	fileOffset := cursor.GetFileOffset()
	var tailLen int64 = 64
	start := max(fileOffset-tailLen, 0)
	cursor.SetFileOffset(start)
	data := make([]byte, tailLen)
	_, err = cursor.Read(data)
	if err != nil {
		common.Log.Debug("Failed reading while looking for startxref: %v", err)
		return nil, err
	}
	stringSubmatch := startxrefRegexp.FindStringSubmatch(string(data))
	if len(stringSubmatch) < 2 {
		common.Log.Debug("Error: startxref not found!")
		return nil, errors.New("startxref not found")
	}
	if len(stringSubmatch) > 2 {
		common.Log.Debug("ERROR: Multiple startxref (%s)!", data)
		return nil, errors.New("multiple startxref entries?")
	}
	offset, _ := strconv.ParseInt(stringSubmatch[1], 10, 64)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("startxref at %d", offset)
	}
	parser.startxrefOffset = offset
	if offset > parser.fileSize {
		common.Log.Debug("ERROR: Xref offset outside of file")
		common.Log.Debug("Attempting repair")
		offset, err = parser.repairLocateXref(cursor)
		if err != nil {
			common.Log.Debug("ERROR: Repair attempt failed (%s)")
			return nil, err
		}
	}
	cursor.SetFileOffset(offset)
	xref, err := parser.parseXref(cursor, &parser.xrefOffset)
	if err != nil {
		return nil, err
	}
	nums := []int64{offset}
	xRefStm := xref.Get("XRefStm")
	if xRefStm != nil {
		num, ok := xRefStm.(*PdfObjectInteger)
		if !ok {
			return nil, errors.New("XRefStm != int")
		}
		_, err = parser.parseXrefStream(cursor, num)
		if err != nil && parser.Opts != nil && !parser.Opts.RelaxedMode {
			return nil, err
		}
		if err == nil {
			nums = append(nums, int64(*num))
		}
	}
	xRefStm = xref.Get("Prev")
	for xRefStm != nil {
		num, ok := xRefStm.(*PdfObjectInteger)
		if !ok {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Invalid Prev reference: Not a *PdfObjectInteger (%T)", xRefStm)
			}
			break
		}
		prevOffset := int64(*num)
		if slices.Contains(nums, prevOffset) || slices.Contains(parser.xrefOffsets, prevOffset) {
			common.Log.Debug("Preventing circular xref referencing")
			break
		}
		if prevOffset <= 0 || prevOffset >= parser.fileSize {
			common.Log.Debug("Prev xref offset outside of file, ignoring it")
			break
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Another Prev xref table object at %d", prevOffset)
		}
		cursor.SetFileOffset(prevOffset)
		dict, err2 := parser.parseXref(cursor, &parser.xrefOffset)
		if err2 != nil {
			common.Log.Debug("Warning: Error - Failed loading another (Prev) trailer")
			common.Log.Debug("Attempting to continue by ignoring it")
			break
		}
		parser.xrefOffsets = append(parser.xrefOffsets, prevOffset)
		xRefStm = dict.Get("Prev")
	}
	if parser.Opts != nil && parser.Opts.AutoRepairXrefTable {
		if parser.trailerHasEncrypt(xref) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Encrypted file: deferring xref table health check until authentication")
			}
			parser.pendingXrefRepair = true
		} else if err2 := parser.checkAndRepairXrefTable(xref); err2 != nil {
			return xref, err2
		}
	}
	return xref, nil
}

func newJBIG2EncoderFromStream(streamObj *PdfObjectStream, decodeParams *PdfObjectDictionary) (*JBIG2Encoder, error) {
	const procNewJBIG2DecoderFromStream = "newJBIG2DecoderFromStream"
	jbiG2Encoder := NewJBIG2Encoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return jbiG2Encoder, nil
	}
	if decodeParams == nil {
		decodeParms := pdfObjectDictionary.Get("DecodeParms")
		if decodeParms != nil {
			switch v := decodeParms.(type) {
			case *PdfObjectDictionary:
				decodeParams = v
			case *PdfObjectArray:
				if v.Len() == 1 {
					if dict, ok := GetDict(v.Get(0)); ok {
						decodeParams = dict
					}
				}
			default:
				common.Log.Error("DecodeParams not a dictionary %#v", decodeParms)
				return nil, jbig2errors.Errorf(procNewJBIG2DecoderFromStream, "invalid DecodeParms type: %T", v)
			}
		}
	}
	if decodeParams == nil {
		return jbiG2Encoder, nil
	}
	jbiG2Encoder.UpdateParams(decodeParams)
	stream, ok := GetStream(decodeParams.Get("JBIG2Globals"))
	if !ok {
		return jbiG2Encoder, nil
	}
	data := stream.Stream
	var err error
	if stream.PdfObjectDictionary != nil && !isJBIG2Stream(stream) {
		data, err = DecodeStream(stream)
		if err != nil {
			err = jbig2errors.Wrap(err, procNewJBIG2DecoderFromStream, "decoding jbig2 globals stream failed")
			common.Log.Debug("ERROR: %v", err)
			return nil, err
		}
	}
	jbiG2Encoder.Globals, err = jbig2.DecodeGlobals(data)
	if err != nil {
		err = jbig2errors.Wrap(err, procNewJBIG2DecoderFromStream, "corrupted jbig2 encoded data")
		common.Log.Debug("ERROR: %v", err)
		return nil, err
	}
	return jbiG2Encoder, nil
}

const minPoolBufSize = poolSize16K / 4

// GetEncryptObj returns the PdfIndirectObject which has information about the PDFs encryption details.
func (parser *PdfParser) GetEncryptObj() *PdfIndirectObject { return parser.encryptObj }

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

type xrefType int

func (parser *PdfParser) parseFirstXrefTrailer() (*PdfObjectDictionary, error) {
	if parser.firstXrefOffset == 0 {
		return nil, errors.New("first xref location not recorded; linearization dict not parsed")
	}
	firstParser := &PdfParser{reader: parser.reader, fileSize: parser.fileSize, Opts: parser.Opts, decodedTotal: parser.decodedTotal}
	xrefs := parser.currentXrefs()
	m := make(map[int]XrefObject, len(xrefs.ObjectMap))
	maps.Copy(m, xrefs.ObjectMap)
	firstParser.setXrefs(XrefTable{ObjectMap: m})
	firstParser.objStreams = make(objectStreams)
	if parser.xrefType != nil {
		xrefType := *parser.xrefType
		firstParser.xrefType = &xrefType
	}
	cursor := firstParser.newCursor(parser.firstXrefOffset)
	return firstParser.parseXref(cursor, nil)
}

// NewBitWriter returns an empty MSB-first bit writer.
func NewBitWriter() *BitWriter { return &BitWriter{} }

// StreamingDecoder is implemented by stream encoders that can decode their
// input incrementally, without materializing the full decoded output in
// memory. Decoders that do not implement this interface (image codecs such
// as DCT, JPX, CCITTFax, JBIG2) must be consumed via DecodeBytes.
//
// The reader returned by DecodeReader produces the same bytes that
// DecodeBytes would produce for the same valid input. Decoding errors are
// surfaced lazily via Read.
type StreamingDecoder interface {
	DecodeReader(r io.Reader) io.Reader
}

var zlibReaderPool = sync.Pool{New: func() any {
	reader, _ := zlib.NewReader(bytes.NewReader([]byte{0x78, 0x9c, 0x01, 0x00, 0x00, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01}))
	return reader
}}

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

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (asciiHex *ASCIIHexEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(asciiHex.GetFilterName()))
	return dict
}

// String returns a string describing `array`.
func (arr *PdfObjectArray) String() string {
	var out strings.Builder
	out.WriteString("[")
	for i, elem := range arr.Elements() {
		out.WriteString(elem.String())
		if i < (arr.Len() - 1) {
			out.WriteString(", ")
		}
	}
	out.WriteString("]")
	return out.String()
}

var xrefKeywordRegexp = regexp.MustCompile("\\s*xref\\s*")

// MakeName creates a PdfObjectName from a string.
func MakeName(s string) *PdfObjectName { name := PdfObjectName(s); return &name }

// Validate validates the page settings for the JBIG2 encoder.
func (jbiG2EncoderSettings JBIG2EncoderSettings) Validate() error {
	const procValidateEncoder = "validateEncoder"
	if jbiG2EncoderSettings.Threshold < 0 || jbiG2EncoderSettings.Threshold > 1.0 {
		return jbig2errors.Errorf(procValidateEncoder, "provided threshold value: '%v' must be in range [0.0, 1.0]", jbiG2EncoderSettings.Threshold)
	}
	if jbiG2EncoderSettings.ResolutionX < 0 {
		return jbig2errors.Errorf(procValidateEncoder, "provided x resolution: '%d' must be positive or zero value", jbiG2EncoderSettings.ResolutionX)
	}
	if jbiG2EncoderSettings.ResolutionY < 0 {
		return jbig2errors.Errorf(procValidateEncoder, "provided y resolution: '%d' must be positive or zero value", jbiG2EncoderSettings.ResolutionY)
	}
	if jbiG2EncoderSettings.DefaultPixelValue != 0 && jbiG2EncoderSettings.DefaultPixelValue != 1 {
		return jbig2errors.Errorf(procValidateEncoder, "default pixel value: '%d' must be a value for the bit: {0,1}", jbiG2EncoderSettings.DefaultPixelValue)
	}
	if jbiG2EncoderSettings.Compression != JB2Generic {
		return jbig2errors.Errorf(procValidateEncoder, "provided compression is not implemented yet")
	}
	return nil
}

// NewLZWEncoder makes a new LZW encoder with default parameters.
func NewLZWEncoder() *LZWEncoder {
	lzw := &LZWEncoder{}
	lzw.Predictor = 1
	lzw.BitsPerComponent = 8
	lzw.Colors = 1
	lzw.Columns = 1
	lzw.EarlyChange = 1
	return lzw
}

const (
	DefaultJPEGQuality = 75
)

func checkSliceBounds(length, low, high int) error {
	if low < 0 || low > length {
		return errors.New("slice index a out of bounds")
	}
	if high < low {
		return errors.New("invalid slice index b < a")
	}
	if high > length {
		return errors.New("slice index b out of bounds")
	}
	return nil
}

// GetFilterName returns the name of the encoding filter.
func (ccitt *CCITTFaxEncoder) GetFilterName() string { return StreamEncodingFilterNameCCITTFax }

// DecodeBytes decodes a byte slice from Run length encoding.
//
// 7.4.5 RunLengthDecode Filter
// The RunLengthDecode filter decodes data that has been encoded in a simple byte-oriented format based on run length.
// The encoded data shall be a sequence of runs, where each run shall consist of a length byte followed by 1 to 128
// bytes of data. If the length byte is in the range 0 to 127, the following length + 1 (1 to 128) bytes shall be
// copied literally during decompression. If length is in the range 129 to 255, the following single byte shall be
// copied 257 - length (2 to 128) times during decompression. A length value of 128 shall denote EOD.
func (runLength *RunLengthEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	reader := bytes.NewReader(encoded)
	var data []byte
	remaining := runLength.limits.remaining()
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b != 128 {
			count := int64(b) + 1
			if b > 128 {
				count = 257 - int64(b)
			}
			if int64(len(data))+count > remaining {
				return nil, runLength.limits.exceeded(StreamEncodingFilterNameRunLength, remaining)
			}
		}
		if b > 128 {
			c, err2 := reader.ReadByte()
			if err2 != nil {
				return nil, err2
			}
			for i := 0; i < 257-int(b); i++ {
				data = append(data, c)
			}
		} else if b < 128 {
			for i := 0; i < int(b)+1; i++ {
				c, err2 := reader.ReadByte()
				if err2 != nil {
					return nil, err2
				}
				data = append(data, c)
			}
		} else {
			break
		}
	}
	runLength.limits.charge(int64(len(data)))
	return data, nil
}

func releaseBuffer(buffer *bytes.Buffer) {
	if buffer.Cap() > poolSize16M {
		return
	}
	buffer.Reset()
	bufferPool.Put(buffer)
}

func (runLength *RunLengthEncoder) setDecodeLimits(limits decodeLimits) { runLength.limits = limits }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (asciiHex *ASCIIHexEncoder) MakeDecodeParams() PdfObject { return nil }

// DecodeBytes returns the passed in slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) DecodeBytes(encoded []byte) ([]byte, error) { return encoded, nil }

// NewParserAt creates a parser with an explicit document size. Use this when
// the source's byte length cannot be auto-detected from its type — e.g. a
// custom io.ReaderAt that doesn't implement Size/Seek/Stat.
func NewParserAt(r io.ReaderAt, size int64) (*PdfParser, error) {
	return newPdfParserAt(r, size, nil, new(atomic.Int64))
}

// EncodeImage encodes 'img' golang image.Image into jbig2 encoded bytes document using default encoder settings.
func (jbig2 *JBIG2Encoder) EncodeImage(img image.Image) ([]byte, error) {
	return jbig2.encodeImage(img)
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (str *PdfObjectString) Equals(other PdfObject) bool {
	if str == nil {
		return other == nil
	}
	return equalObjects(str, other, 0, nil)
}

// Close closes the wrapped reader when it is an io.Closer.
func (r *limitedDecodeReader) Close() error {
	if c, ok := r.src.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// GetFilterName returns the name of the encoding filter.
func (jpxEncoder *JPXEncoder) GetFilterName() string { return StreamEncodingFilterNameJPX }

func (br *bitReader) seek(pos int) error {
	if pos < 0 || pos > len(br.data) {
		return fmt.Errorf("hint stream: seek out of range (%d, len=%d)", pos, len(br.data))
	}
	br.pos = pos
	br.acc = 0
	br.nbits = 0
	return nil
}

func readSharedObjectHintTable(br *bitReader) (*SharedObjectHintTable, error) {
	shared := &SharedObjectHintTable{}
	sts := []struct {
		dest any
		bits uint
	}{{&shared.FirstSharedObjNum, 32}, {&shared.FirstSharedOffset, 32}, {&shared.NumSharedFirstPage, 32}, {&shared.NumSharedTotal, 32}, {&shared.BitsPerGroupIdentifier, 16}, {&shared.LeastSharedGroupLength, 32}, {&shared.BitsPerSharedGroupLengthDelta, 16}}
	for _, st := range sts {
		bits, err := br.readBits(st.bits)
		if err != nil {
			return nil, err
		}
		switch v := st.dest.(type) {
		case *uint32:
			*v = bits
		case *uint16:
			*v = uint16(bits)
		}
	}
	if shared.NumSharedFirstPage > shared.NumSharedTotal {
		return nil, fmt.Errorf("hint stream: NumSharedFirstPage %d exceeds NumSharedTotal %d", shared.NumSharedFirstPage, shared.NumSharedTotal)
	}
	if shared.NumSharedTotal == 0 {
		return shared, nil
	}
	if shared.NumSharedTotal > maxSharedObjectsTotal {
		return nil, fmt.Errorf("hint stream: NumSharedTotal %d exceeds limit %d", shared.NumSharedTotal, maxSharedObjectsTotal)
	}
	shared.Entries = make([]SharedObjectEntry, shared.NumSharedTotal)
	for i := uint32(0); i < shared.NumSharedTotal; i++ {
		shared.Entries[i].ObjectsInGroup = 1
	}
	for i := uint32(0); i < shared.NumSharedTotal; i++ {
		bits, err := br.readBits(uint(shared.BitsPerSharedGroupLengthDelta))
		if err != nil {
			return shared, fmt.Errorf("shared[%d] groupLenDelta: %w", i, err)
		}
		shared.Entries[i].GroupLengthDelta = bits
	}
	br.skipToNextByte()
	for i := uint32(0); i < shared.NumSharedTotal; i++ {
		bits, err := br.readBits(1)
		if err != nil {
			return shared, fmt.Errorf("shared[%d] signaturePresent: %w", i, err)
		}
		shared.Entries[i].SignaturePresent = bits != 0
	}
	br.skipToNextByte()
	for i := uint32(0); i < shared.NumSharedTotal; i++ {
		if !shared.Entries[i].SignaturePresent {
			continue
		}
		if err := br.skipBits(128); err != nil {
			return shared, fmt.Errorf("shared[%d] signature: %w", i, err)
		}
	}
	for i := uint32(0); i < shared.NumSharedTotal; i++ {
		bits, err := br.readBits(uint(shared.BitsPerGroupIdentifier))
		if err != nil {
			return shared, fmt.Errorf("shared[%d] nobjects-1: %w", i, err)
		}
		shared.Entries[i].ObjectsInGroup = bits + 1
	}
	br.skipToNextByte()
	return shared, nil
}

// LookupByReference looks up a PdfObject by a reference.
func (parser *PdfParser) LookupByReference(ref PdfObjectReference) (PdfObject, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Looking up reference %s", ref.String())
	}
	return parser.LookupByNumber(int(ref.ObjectNumber))
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (flate *FlateEncoder) MakeDecodeParams() PdfObject {
	if flate.Predictor > 1 {
		dict := MakeDict()
		dict.Set("Predictor", MakeInteger(int64(flate.Predictor)))
		if flate.BitsPerComponent != 8 {
			dict.Set("BitsPerComponent", MakeInteger(int64(flate.BitsPerComponent)))
		}
		if flate.Columns != 1 {
			dict.Set("Columns", MakeInteger(int64(flate.Columns)))
		}
		if flate.Colors != 1 {
			dict.Set("Colors", MakeInteger(int64(flate.Colors)))
		}
		return dict
	}
	return nil
}

func sortedXrefObjects(objectMap map[int]XrefObject) []XrefObject {
	count := 0
	for _, xrefObj := range objectMap {
		if xrefObj.Offset > 0 {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	xrefObjs := make([]XrefObject, 0, count)
	for _, xrefObj := range objectMap {
		if xrefObj.Offset > 0 {
			xrefObjs = append(xrefObjs, xrefObj)
		}
	}
	sort.Slice(xrefObjs, func(i, j int) bool { return xrefObjs[i].Offset < xrefObjs[j].Offset })
	return xrefObjs
}

func (parser *PdfParser) parseXref(cursor *readerAtCursor, xrefOffset *int64) (*PdfObjectDictionary, error) {
	parser.skipSpaces(cursor)
	const xrefProbeLen = 20
	data, _ := cursor.Peek(xrefProbeLen)
	for i := 0; i < 2; i++ {
		if xrefOffset != nil && *xrefOffset == 0 {
			*xrefOffset = cursor.GetFileOffset()
		}
		if objHeaderRegexp.Match(data) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("xref points to an object. Probably xref object")
			}
			common.Log.Debug("starting with \"%s\"", string(data))
			xrefStream, err := parser.parseXrefStream(cursor, nil)
			return parser.recordXrefSectionEnd(cursor, xrefStream, err)
		}
		if xrefKeywordRegexp.Match(data) {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Standard xref section table!")
			}
			xrefTable, err := parser.parseXrefTable(cursor)
			return parser.recordXrefSectionEnd(cursor, xrefTable, err)
		}
		fileOffset := cursor.GetFileOffset()
		if xrefOffset != nil && *xrefOffset == 0 {
			*xrefOffset = fileOffset
		}
		cursor.SetFileOffset(fileOffset - xrefProbeLen)
		defer cursor.SetFileOffset(fileOffset)
		peeked, _ := cursor.Peek(xrefProbeLen)
		data = append(peeked, data...)
	}
	common.Log.Debug("Warning: Unable to find xref table or stream. Repair attempted: Looking for earliest xref from bottom.")
	if err := parser.repairSeekXrefMarker(cursor); err != nil {
		common.Log.Debug("Repair failed - %v", err)
		return nil, err
	}
	xrefTable, err := parser.parseXrefTable(cursor)
	return parser.recordXrefSectionEnd(cursor, xrefTable, err)
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
		ObjectNumber: int64(index),
		PdfObject:    object,
		parser:       parser,
	}
	return &indirect, nil
}

func decodePredictor(data []byte, predictor, columns, bitsPerComponent, colors int) ([]byte, error) {
	rowBytes, bytesPerPixel, bitsPerComp, err := normalizePredictorParams(columns, bitsPerComponent, colors)
	if err != nil {
		return nil, err
	}
	switch {
	case predictor == 2:
		return decodeTIFFPredictor(data, columns, bitsPerComp, colors, rowBytes)
	case predictor >= 10 && predictor <= 15:
		return decodePNGPredictor(data, rowBytes, bytesPerPixel)
	}
	return nil, fmt.Errorf("unsupported predictor (%d)", predictor)
}

// DecodeBytes decodes a slice of JPX encoded bytes and returns the result.
func (jpxEncoder *JPXEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	common.Log.Debug("Error: Attempting to use unsupported encoding %s", jpxEncoder.GetFilterName())
	return encoded, ErrNoJPXDecode
}

func (parser *PdfParser) recordXrefSectionEnd(cursor *readerAtCursor, trailer *PdfObjectDictionary, err error) (*PdfObjectDictionary, error) {
	if err == nil {
		parser.xrefSectionEnds = append(parser.xrefSectionEnds, cursor.GetFileOffset())
	}
	return trailer, err
}

// PdfParser parses a PDF file and provides access to the object structure of the PDF.
type PdfParser struct {
	version              Version
	reader               io.ReaderAt
	fileSize             int64
	xrefs                atomic.Pointer[XrefTable]
	xrefOffset           int64
	xrefType             *xrefType
	objStreams           objectStreams
	trailer              *PdfObjectDictionary
	crypter              *PdfCrypt
	encryptObj           *PdfIndirectObject
	repairOnce           sync.Once
	repair               repairResult
	repairedXrefs        atomic.Pointer[XrefTable]
	pendingXrefRepair    bool
	objCache             sync.Map
	lookupGroup          singleflight.Group
	objStreamMu          sync.Mutex
	metadata             ParserMetadata
	compliance           bool
	xrefOffsets          []int64
	numRevisions         int
	startxrefOffset      int64
	xrefSectionEnds      []int64
	linearized           bool
	linearizedFileLength int64
	linearizationDict    *PdfObjectDictionary
	linearization        *LinearizationInfo
	linearizationObjNum  int
	firstTrailerOnce     sync.Once
	firstTrailer         *PdfObjectDictionary
	firstTrailerErr      error
	firstXrefOffset      int64
	revisionParsers      map[*PdfParser]*PdfParser
	revisionEndsOnce     sync.Once
	revisionEnds         []int64
	revisionEndsErr      error
	revisionEndsValid    bool
	revisions            []*PdfParser

	// Opts holds different parsing options.
	Opts         *ParserOpts
	decodedTotal *atomic.Int64
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (jbig2 *JBIG2Encoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(jbig2.GetFilterName()))
	return dict
}

// IsFloatDigit checks if a character can be a part of a float number string.
func IsFloatDigit(c byte) bool { return ('0' <= c && c <= '9') || c == '.' }

// EncodeBytes DCT encodes the passed in slice of bytes.
func (dct *DCTEncoder) EncodeBytes(data []byte) ([]byte, error) {
	var img image.Image
	if dct.ColorComponents == 1 && dct.BitsPerComponent == 8 {
		img = &image.Gray{Rect: image.Rect(0, 0, dct.Width, dct.Height), Pix: data, Stride: imageutil.BytesPerLine(dct.Width, dct.BitsPerComponent, dct.ColorComponents)}
	} else {
		var err error
		img, err = imageutil.NewImage(dct.Width, dct.Height, dct.BitsPerComponent, dct.ColorComponents, data, nil, nil)
		if err != nil {
			return nil, err
		}
	}
	options := jpeg.Options{}
	options.Quality = dct.Quality
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &options); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (br *bitReader) readBits(bits uint) (uint32, error) {
	if bits == 0 {
		return 0, nil
	}
	if bits > 32 {
		return 0, fmt.Errorf("hint stream: readBits n=%d exceeds 32", bits)
	}
	for br.nbits < bits {
		if br.pos >= len(br.data) {
			return 0, errHintShortRead
		}
		br.acc = (br.acc << 8) | uint64(br.data[br.pos])
		br.pos++
		br.nbits += 8
	}
	shift := br.nbits - bits
	mask := uint64(1)<<bits - 1
	val := uint32((br.acc >> shift) & mask)
	br.nbits -= bits
	br.acc &= (uint64(1) << br.nbits) - 1
	return val, nil
}

// DecodeBytes decodes a slice of JBIG2 encoded bytes and returns the results.
func (jbig22 *JBIG2Encoder) DecodeBytes(encoded []byte) ([]byte, error) {
	if size, ok := jbig2FirstPageBitmapSize(encoded, jbig22.Globals); ok {
		if err := jbig22.limits.check(StreamEncodingFilterNameJBIG2, size); err != nil {
			return nil, err
		}
	}
	bytes, err := jbig2.DecodeBytes(encoded, decoder.Parameters{}, jbig22.Globals)
	if err != nil {
		return nil, err
	}
	if err2 := jbig22.limits.check(StreamEncodingFilterNameJBIG2, int64(len(bytes))); err2 != nil {
		return nil, err2
	}
	jbig22.limits.charge(int64(len(bytes)))
	return bytes, nil
}

// XrefObject defines a cross reference entry which is a map between object number (with generation number) and the
// location of the actual object, either as a file offset (xref table entry), or as a location within an xref
// stream object (xref object stream).
type XrefObject struct {
	XType        xrefType
	ObjectNumber int
	Generation   int

	// For normal xrefs (defined by OFFSET)
	Offset int64

	// For xrefs to object streams.
	OsObjNumber int
	OsObjIndex  int
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

func (parser *PdfParser) validateLinearization() ([]LinearizationViolation, error) {
	if !parser.IsLinearized() {
		return []LinearizationViolation{{Code: LinErrNotLinearized, Severity: SeverityError, Message: "PDF is not linearized"}}, nil
	}
	var violations []LinearizationViolation
	linInfo := parser.LinearizationInfo()
	if violation := checkParamDictEnd(parser.firstXrefOffset); violation != nil {
		violations = append(violations, *violation)
	}
	if linInfo != nil && linInfo.FileLength != parser.fileSize {
		violations = append(violations, LinearizationViolation{Code: LinErrFileLengthMismatch, Severity: SeverityError, Message: fmt.Sprintf("/L (%d) does not match actual file size (%d)", linInfo.FileLength, parser.fileSize)})
	}
	if parser.linearizationDict != nil {
		if violation := checkLinearizedVersion(parser.linearizationDict); violation != nil {
			violations = append(violations, *violation)
		}
	}
	if linInfo != nil {
		if violation := checkFirstPageEndOffset(linInfo.EndOfFirstPage, parser.firstXrefOffset, linInfo.MainXrefOffset, parser.fileSize); violation != nil {
			violations = append(violations, *violation)
		}
		if violation := checkMainXrefOffset(linInfo.MainXrefOffset, linInfo.EndOfFirstPage, parser.fileSize); violation != nil {
			violations = append(violations, *violation)
		}
	}
	if linInfo != nil {
		violations = append(violations, parser.validateFirstPageAndCount(linInfo)...)
		violations = append(violations, parser.validateFirstPageSelfContained(linInfo)...)
	}
	if linInfo != nil {
		if len(linInfo.HintOffsets) > 0 && linInfo.HintTables == nil {
			_ = parser.parseHintStreamIfPresent()
		}
		violations = append(violations, checkHintStreams(linInfo, parser.fileSize)...)
	}
	if linInfo != nil {
		if violation := checkTrailingStartxref(parser.GetXrefOffset(), parser.firstXrefOffset, linInfo.EndOfFirstPage); violation != nil {
			violations = append(violations, *violation)
		}
		if violation := parser.checkFirstXrefPrevChain(linInfo); violation != nil {
			violations = append(violations, *violation)
		}
	}
	if violation := parser.checkLinearizedTrailerID(); violation != nil {
		violations = append(violations, *violation)
	}
	if parser.GetRevisionNumber() > 0 {
		violations = append(violations, LinearizationViolation{Code: LinErrUpdateBreaksLinearization, Severity: SeverityWarning, Message: "incremental update present after linearization; linearization is stale"})
	}
	return violations, nil
}

func (parser *PdfParser) objectFileOffset(objNum int) (int64, bool) {
	xrefObj, ok := parser.currentXrefs().ObjectMap[objNum]
	if !ok || xrefObj.XType != XrefTypeTableEntry {
		return 0, false
	}
	return xrefObj.Offset, true
}

// EncodeSharedObjectHintTable encodes the mandatory shared object hint table
// from header parameters and per-entry records.
func EncodeSharedObjectHintTable(bw *BitWriter, t *SharedObjectHintTable) error {
	if t == nil {
		return fmt.Errorf("hint encoder: nil shared object table")
	}
	bw.WriteBits(t.FirstSharedObjNum, 32)
	bw.WriteBits(t.FirstSharedOffset, 32)
	bw.WriteBits(t.NumSharedFirstPage, 32)
	bw.WriteBits(t.NumSharedTotal, 32)
	bw.WriteBits(uint32(t.BitsPerGroupIdentifier), 16)
	bw.WriteBits(t.LeastSharedGroupLength, 32)
	bw.WriteBits(uint32(t.BitsPerSharedGroupLengthDelta), 16)
	for _, entry := range t.Entries {
		bw.WriteBits(entry.GroupLengthDelta, uint(t.BitsPerSharedGroupLengthDelta))
	}
	bw.AlignByte()
	for _, entry := range t.Entries {
		var bit uint32
		if entry.SignaturePresent {
			bit = 1
		}
		bw.WriteBits(bit, 1)
	}
	bw.AlignByte()
	for _, entry := range t.Entries {
		if !entry.SignaturePresent {
			continue
		}
		for range 4 {
			bw.WriteBits(0, 32)
		}
	}
	for _, entry := range t.Entries {
		delta := uint32(0)
		if entry.ObjectsInGroup > 0 {
			delta = entry.ObjectsInGroup - 1
		}
		bw.WriteBits(delta, uint(t.BitsPerGroupIdentifier))
	}
	bw.AlignByte()
	return nil
}

func (parser *PdfParser) computeRevisionEndOffsets() ([]int64, bool, error) {
	eofMarkers, err := parser.scanEOFMarkers()
	if err != nil {
		return nil, false, err
	}
	if len(eofMarkers) == 0 {
		return nil, false, errEOFNotFound
	}
	want := parser.numRevisions + 1
	ok := false
	if markers, hasMarkers := parser.filterEOFMarkersByContext(eofMarkers); len(markers) >= want {
		eofMarkers = markers
		ok = hasMarkers
	}
	nums := make([]int64, len(eofMarkers))
	for i, marker := range eofMarkers {
		nums[i] = marker.endOffset
	}
	if len(nums) <= want {
		return nums, ok && len(nums) == want, nil
	}
	sorted := append([]int64(nil), parser.xrefOffsets...)
	slices.Sort(sorted)
	if len(sorted) > want-1 {
		sorted = sorted[len(sorted)-(want-1):]
	}
	ends := make([]int64, 0, want)
	idx := 0
	for _, off := range sorted {
		for idx < len(nums) && nums[idx] <= off {
			idx++
		}
		if idx >= len(nums)-1 {
			return nums[len(nums)-want:], false, nil
		}
		ends = append(ends, nums[idx])
		idx++
	}
	return append(ends, nums[len(nums)-1]), false, nil
}

// AddPageImage adds the page with the image 'img' to the encoder context in order to encode it jbig2 document.
// The 'settings' defines what encoding type should be used by the encoder.
func (jbig2 *JBIG2Encoder) AddPageImage(img *JBIG2Image, settings *JBIG2EncoderSettings) (err error) {
	const procJBIG2DocumentAddPageImage = "JBIG2Document.AddPageImage"
	if jbig2 == nil {
		return jbig2errors.Error(procJBIG2DocumentAddPageImage, "JBIG2Document is nil")
	}
	if settings == nil {
		settings = &jbig2.DefaultPageSettings
	}
	if jbig2.doc == nil {
		jbig2.doc = document.InitEncodeDocument(settings.FileMode)
	}
	if err = settings.Validate(); err != nil {
		return jbig2errors.Wrap(err, procJBIG2DocumentAddPageImage, "")
	}
	bm, err := img.toBitmap()
	if err != nil {
		return jbig2errors.Wrap(err, procJBIG2DocumentAddPageImage, "")
	}
	switch settings.Compression {
	case JB2Generic:
		if err = jbig2.doc.AddGenericPage(bm, settings.DuplicatedLinesRemoval); err != nil {
			return jbig2errors.Wrap(err, procJBIG2DocumentAddPageImage, "")
		}
	case JB2SymbolCorrelation:
		return jbig2errors.Error(procJBIG2DocumentAddPageImage, "symbol correlation encoding not implemented yet")
	case JB2SymbolRankHaus:
		return jbig2errors.Error(procJBIG2DocumentAddPageImage, "symbol rank haus encoding not implemented yet")
	default:
		return jbig2errors.Error(procJBIG2DocumentAddPageImage, "provided invalid compression")
	}
	return nil
}

// String returns a string describing `stream`.
func (stream *PdfObjectStream) String() string {
	return fmt.Sprintf("Object stream %d: %s", stream.ObjectNumber, stream.PdfObjectDictionary)
}

// PdfCryptNewDecrypt makes the document crypt handler based on the encryption dictionary
// and trailer dictionary. Returns an error on failure to process.
func PdfCryptNewDecrypt(parser *PdfParser, ed, trailer *PdfObjectDictionary) (*PdfCrypt, error) {
	crypt := &PdfCrypt{authenticated: false, decryptedObjects: make(map[PdfObject]bool), encryptedObjects: make(map[PdfObject]bool), encryptedObjNums: make(map[int]struct{}), parser: parser}
	filter, ok := ed.Get("Filter").(*PdfObjectName)
	if !ok {
		common.Log.Debug("ERROR Crypt dictionary missing required Filter field!")
		return crypt, errors.New("required crypt field Filter missing")
	}
	switch *filter {
	case "Standard", adobePubSecFilter:
	default:
		common.Log.Debug("ERROR Unsupported filter (%s)", *filter)
		return crypt, errors.New("unsupported Filter")
	}
	crypt.encrypt.Filter = string(*filter)
	ok2 := crypt.encrypt.Filter == adobePubSecFilter
	if subFilter, hasSubFilter := GetName(ed.Get("SubFilter")); hasSubFilter {
		crypt.encrypt.SubFilter = string(*subFilter)
	} else if str, ok3 := GetString(ed.Get("SubFilter")); ok3 {
		crypt.encrypt.SubFilter = str.Str()
	}
	if crypt.encrypt.SubFilter != "" {
		common.Log.Debug("Using subfilter %s", crypt.encrypt.SubFilter)
	}
	if L, isNum := ed.Get("Length").(*PdfObjectInteger); isNum {
		if (*L % 8) != 0 {
			common.Log.Debug("ERROR Invalid encryption length")
			return crypt, errors.New("invalid encryption length")
		}
		crypt.encrypt.Length = int(*L)
	} else {
		crypt.encrypt.Length = 40
	}
	crypt.encrypt.V = 0
	if vNum, isNum := ed.Get("V").(*PdfObjectInteger); isNum {
		V := int(*vNum)
		crypt.encrypt.V = V
		if V >= 1 && V <= 2 {
			crypt.cryptFilters = defaultCryptFilters(crypt.encrypt.Length)
		} else if V >= 4 && V <= 5 {
			if err := crypt.loadCryptFilters(ed); err != nil {
				return crypt, err
			}
		} else {
			common.Log.Debug("ERROR Unsupported encryption algo V = %d", V)
			return crypt, errors.New("unsupported algorithm")
		}
	}
	if ok2 {
		pubSecEncryptDict, err := parsePubSecEncryptDict(crypt, ed)
		if err != nil {
			return crypt, err
		}
		crypt.pubSec = pubSecEncryptDict
	} else {
		if err := parseStdEncryptDict(&crypt.stdEncrypt, ed); err != nil {
			return crypt, err
		}
	}
	s := ""
	if idArr, isArr := trailer.Get("ID").(*PdfObjectArray); isArr && idArr.Len() >= 1 {
		str, hasStr := GetString(idArr.Get(0))
		if !hasStr {
			return crypt, errors.New("invalid trailer ID")
		}
		s = str.Str()
	} else {
		common.Log.Debug("Trailer ID array missing or invalid!")
	}
	crypt.id0 = s
	return crypt, nil
}

// PdfObjectArray represents the primitive PDF array object.
type PdfObjectArray struct{ elements []PdfObject }

// DecodeReader returns a reader that Flate-decodes (zlib) from r. Reading
// past the decoded size limit fails with ErrDecodedSizeLimitExceeded. The
// predictor configured on enc is NOT applied here — DecodeReader parities
// DecodeBytes, which is also raw zlib decompression. Callers that need
// predictor application must continue to use DecodeStream.
//
// A fresh zlib reader is allocated per call rather than reusing
// zlibReaderPool: the returned reader's lifetime is unbounded and the pool
// requires explicit return-on-close that an io.Reader cannot express.
//
// An empty input stream yields an empty payload (zero bytes, io.EOF),
// matching DecodeBytes.
func (flate *FlateEncoder) DecodeReader(r io.Reader) io.Reader {
	if r == nil {
		return errorReader{err: errors.New("FlateDecode: nil input")}
	}
	reader := bufio.NewReader(r)
	if _, err := reader.Peek(1); err != nil {
		if err == io.EOF {
			return emptyReader{}
		}
		return errorReader{err: err}
	}
	rc, err := zlib.NewReader(reader)
	if err != nil {
		return errorReader{err: err}
	}
	return flate.limits.reader(StreamEncodingFilterNameFlate, rc)
}

// UpdateParams updates the parameter values of the encoder.
func (multi *MultiEncoder) UpdateParams(params *PdfObjectDictionary) {
	for _, encoder := range multi.encoders {
		encoder.UpdateParams(params)
	}
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

const (
	JB2Generic JBIG2CompressionType = iota
	JB2SymbolCorrelation
	JB2SymbolRankHaus
)

func (parser *PdfParser) seekToEOFMarker(cursor *readerAtCursor, size int64) error {
	var scanned int64
	var chunk int64 = 2048
	for scanned < size-4 {
		if size <= (chunk + scanned) {
			chunk = size - scanned
		}
		cursor.SetFileOffset(size - scanned - chunk)
		data := make([]byte, chunk)
		cursor.Read(data)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking for EOF marker: \"%s\"", string(data))
		}
		allStringIndex := eofRegexp.FindAllStringIndex(string(data), -1)
		if allStringIndex != nil {
			nums := allStringIndex[len(allStringIndex)-1]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Ind: % d", allStringIndex)
			}
			pos := size - scanned - chunk + int64(nums[0])
			cursor.SetFileOffset(pos)
			return nil
		}
		common.Log.Debug("Warning: EOF marker not found! - continue seeking")
		scanned += chunk - 4
	}
	common.Log.Debug("Error: EOF marker was not found.")
	return errEOFNotFound
}

func (parser *PdfParser) xrefNextObjectOffset(offset int64) int64 {
	xrefs := parser.currentXrefs()
	if len(xrefs.sorted) == 0 {
		return 0
	}
	idx := sort.Search(len(xrefs.sorted), func(i int) bool { return xrefs.sorted[i].Offset >= offset })
	offset2 := int64(0)
	if idx < len(xrefs.sorted) {
		offset2 = xrefs.sorted[idx].Offset
	}
	return offset2
}
func absInt(x int) int { mask := x >> (intBits - 1); return (x ^ mask) - mask }

var pdfVersionRegexp = regexp.MustCompile("%PDF-(\\d)\\.(\\d)")

// PageOffsetHintTable is the mandatory page offset hint table
// (ISO 32000-1 Annex F, Table F.3).
//
// The header carries baseline values, and PerPage[i] carries the per-page
// deltas (object count, byte length, shared-object usage, etc.) for page i.
// Page 0 corresponds to the first page in the document (which is laid out at
// the start of the file in a linearized PDF).
//
// Field order in the bit stream matches the spec items 1..13:
//
//  1. least objects in a page
//  2. first page's page object offset
//  3. bits per (max-min) object-count delta
//  4. least page length
//  5. bits per (max-min) page-length delta
//  6. least content-stream offset
//  7. bits per (max-min) content-stream-offset delta
//  8. least content-stream length
//  9. bits per (max-min) content-stream-length delta
//  10. bits per max shared object references count
//  11. bits per max shared object identifier
//  12. bits per fractional position numerator
//  13. shared denominator
//
// The first page's object NUMBER is not in this table; it is in /O of the
// linearization dictionary.
type PageOffsetHintTable struct {
	LeastObjectsInPage              uint32
	FirstPageOffset                 uint32
	BitsPerObjectCountDelta         uint16
	LeastPageLength                 uint32
	BitsPerPageLengthDelta          uint16
	LeastContentStreamOffset        uint32
	BitsPerContentStreamOffsetDelta uint16
	LeastContentStreamLength        uint32
	BitsPerContentStreamLengthDelta uint16

	// BitsPerNumSharedObjects is the bit width of each page's
	// NumSharedObjects count (spec item 10: "bits needed to represent the
	// greatest number of shared object references"). It is a count width, not
	// a delta.
	BitsPerNumSharedObjects uint16
	BitsPerSharedIdentifier uint16
	BitsPerSharedNumerator  uint16
	SharedDenominator       uint16
	PerPage                 []PageOffsetEntry
}

// JBIG2EncoderSettings contains the parameters and settings used by the JBIG2Encoder.
// Current version works only on JB2Generic compression.
type JBIG2EncoderSettings struct {
	// FileMode defines if the jbig2 encoder should return full jbig2 file instead of
	// shortened pdf mode. This adds the file header to the jbig2 definition.
	FileMode bool

	// Compression is the setting that defines the compression type used for encoding the page.
	Compression JBIG2CompressionType

	// DuplicatedLinesRemoval code generic region in a way such that if the lines are duplicated the encoder
	// doesn't store it twice.
	DuplicatedLinesRemoval bool

	// DefaultPixelValue is the bit value initial for every pixel in the page.
	DefaultPixelValue uint8

	// ResolutionX optional setting that defines the 'x' axis input image resolution - used for single page encoding.
	ResolutionX int

	// ResolutionY optional setting that defines the 'y' axis input image resolution - used for single page encoding.
	ResolutionY int

	// Threshold defines the threshold of the image correlation for
	// non Generic compression.
	// User only for JB2SymbolCorrelation and JB2SymbolRankHaus methods.
	// Best results in range [0.7 - 0.98] - the less the better the compression would be
	// but the more lossy.
	// Default value: 0.95
	Threshold float64
}

// Elements returns a slice of the PdfObject elements in the array.
func (arr *PdfObjectArray) Elements() []PdfObject {
	if arr == nil {
		return nil
	}
	return arr.elements
}

// MakeBool creates a PdfObjectBool from a bool value.
func MakeBool(val bool) *PdfObjectBool { boolObj := PdfObjectBool(val); return &boolObj }

// ValidateLinearizationTrailerID runs only the linearization /ID consistency
// check (first-xref trailer vs main trailer), without the full Annex F battery
// (no page-tree walk or hint decoding). Returns nil if the PDF is not
// linearized or the /ID values match. Intended for callers — such as the PDF/A
// verifier — that only need the /ID rule and want to avoid the O(pages) cost of
// the full validator.
func (parser *PdfParser) ValidateLinearizationTrailerID() *LinearizationViolation {
	if !parser.IsLinearized() {
		return nil
	}
	return parser.checkLinearizedTrailerID()
}

func getNonNegativeInt(dict *PdfObjectDictionary, key PdfObjectName) (int, bool) {
	obj := dict.Get(key)
	if obj == nil {
		return 0, false
	}
	n, err := GetNumberAsInt64(obj)
	if err != nil || n < 0 {
		return 0, false
	}
	return int(n), true
}

// MakeArrayFromIntegers64 creates an PdfObjectArray from a slice of int64s, where each array element
// is an PdfObjectInteger.
func MakeArrayFromIntegers64(vals []int64) *PdfObjectArray {
	array := MakeArray()
	for _, val := range vals {
		array.Append(MakeInteger(val))
	}
	return array
}

// GetFilterName returns the name of the encoding filter.
func (jbig2 *JBIG2Encoder) GetFilterName() string { return StreamEncodingFilterNameJBIG2 }

// IsNullObject returns true if `obj` is a PdfObjectNull.
func IsNullObject(obj PdfObject) bool {
	_, ok := TraceToDirectObject(obj).(*PdfObjectNull)
	return ok
}

// PdfCryptNewEncrypt makes the document crypt handler based on a specified crypt filter.
func PdfCryptNewEncrypt(cf crypt.Filter, userPass, ownerPass []byte, perm security.Permissions) (*PdfCrypt, *EncryptInfo, error) {
	perm = perm.Normalized()
	if len(ownerPass) == 0 && len(userPass) > 0 {
		common.Log.Warning("Empty owner password: the user password is used as the owner password and grants full permissions")
		ownerPass = userPass
	}
	crypt := &PdfCrypt{encryptedObjects: make(map[PdfObject]bool), cryptFilters: make(cryptFilters), stdEncrypt: security.StdEncryptDict{P: perm, EncryptMetadata: true}}
	var version Version
	if cf != nil {
		nums := cf.PDFVersion()
		version.Major, version.Minor = nums[0], nums[1]
		V, R := cf.HandlerVersion()
		crypt.encrypt.V = V
		crypt.stdEncrypt.R = R
		crypt.encrypt.Length = cf.KeyLength() * 8
	}
	const (
		cryptFilterName = stdCryptFilterName
	)
	crypt.cryptFilters[cryptFilterName] = cf
	if crypt.encrypt.V >= 4 {
		crypt.streamFilter = cryptFilterName
		crypt.stringFilter = cryptFilterName
	}
	encryptDict := crypt.newEncryptDict()
	id0, id1 := generateFileIDs()
	crypt.id0 = id0
	err := crypt.generateParams(userPass, ownerPass)
	if err != nil {
		return nil, nil, err
	}
	writeStdEncryptDict(&crypt.stdEncrypt, encryptDict)
	if crypt.encrypt.V >= 4 {
		if err2 := crypt.saveCryptFilters(encryptDict); err2 != nil {
			return nil, nil, err2
		}
	}
	return crypt, &EncryptInfo{Version: version, Encrypt: encryptDict, ID0: id0, ID1: id1}, nil
}

// JBIG2Encoder implements both jbig2 encoder and the decoder. The encoder allows to encode
// provided images (best used document scans) in multiple way. By default it uses single page generic
// encoder. It allows to store lossless data as a single segment.
// In order to store multiple image pages use the 'FileMode' which allows to store more pages within single jbig2 document.
// WIP: In order to obtain better compression results the encoder would allow to encode the input in a
// lossy or lossless way with a component (symbol) mode. It divides the image into components.
// Then checks if any component is 'similar' to the others and maps them together. The symbol classes are stored
// in the dictionary. Then the encoder creates text regions which uses the related symbol classes to fill it's space.
// The similarity is defined by the 'Threshold' variable (default: 0.95). The less the value is, the more components
// matches to single class, thus the compression is better, but the result might become lossy.
type JBIG2Encoder struct {
	// These values are required to be set for the 'EncodeBytes' method.
	// ColorComponents defines the number of color components for provided image.
	ColorComponents int

	// BitsPerComponent is the number of bits that stores per color component
	BitsPerComponent int

	// Width is the width of the image to encode
	Width int

	// Height is the height of the image to encode.
	Height int
	doc    *document.Document

	// Globals are the JBIG2 global segments.
	Globals jbig2.Globals

	// IsChocolateData defines if the data is encoded such that
	// binary data '1' means black and '0' white.
	// otherwise the data is called vanilla.
	// Naming convention taken from: 'https://en.wikipedia.org/wiki/Binary_image#Interpretation'
	IsChocolateData bool

	// DefaultPageSettings are the settings parameters used by the jbig2 encoder.
	DefaultPageSettings JBIG2EncoderSettings
	limits              decodeLimits
}

// HasInvalidSubsectionHeader implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidSubsectionHeader() bool { return meta.invalidSubsectionHeader }

func (r *runLengthReader) fillRun() error {
	b, err := r.src.ReadByte()
	if err != nil {
		return err
	}
	switch {
	case b == 128:
		return io.EOF
	case b > 128:
		c, err2 := r.src.ReadByte()
		if err2 != nil {
			return err2
		}
		count := 257 - int(b)
		for i := 0; i < count; i++ {
			r.run[i] = c
		}
		r.runEnd = count
		r.runPos = 0
		return nil
	default:
		count := int(b) + 1
		if _, err2 := io.ReadFull(r.src, r.run[:count]); err2 != nil {
			return err2
		}
		r.runEnd = count
		r.runPos = 0
		return nil
	}
}

// Size returns the effective byte length of the PDF document. Offsets used
// with ReadBytesAt are relative to the same coordinate space: when garbage
// bytes precede the %PDF header, the parser reads through an offset-adjusted
// source and Size reflects the effective extent rather than the raw source
// length. Note that a signature ByteRange written by an external tool against
// the raw (prefixed) bytes is shifted relative to this space — such
// signatures fail digest verification for the same reason.
func (parser *PdfParser) Size() int64 { return parser.fileSize }

type bitReader struct {
	data  []byte
	pos   int
	acc   uint64
	nbits uint
}

func (br *bitReader) skipBits(nbits uint) error {
	for nbits > 32 {
		if _, err := br.readBits(32); err != nil {
			return err
		}
		nbits -= 32
	}
	_, err := br.readBits(nbits)
	return err
}

func (crypt *PdfCrypt) encodeEncryptPubSec() (*PdfObjectDictionary, error) {
	pubSec := crypt.pubSec
	encryptDict := crypt.newEncryptDict()
	encryptDict.Set("Filter", MakeName(adobePubSecFilter))
	encryptDict.Set("SubFilter", MakeName(pubSec.SubFilter))
	if err := crypt.saveCryptFilters(encryptDict); err != nil {
		return nil, err
	}
	cfDict, ok := GetDict(encryptDict.Get("CF"))
	if !ok {
		return nil, errors.New("encryption dictionary has no CF entry")
	}
	dict, ok := GetDict(cfDict.Get(defaultCryptFilterKey))
	if !ok {
		return nil, fmt.Errorf("crypt filter %s missing from encryption dictionary", defaultCryptFilterKey)
	}
	dict.Set("Length", MakeInteger(int64(crypt.encrypt.Length)))
	array := MakeArray()
	for _, recipient := range pubSec.Recipients {
		array.Append(MakeHexString(string(recipient)))
	}
	dict.Set("Recipients", array)
	dict.Set("EncryptMetadata", MakeBool(pubSec.EncryptMetadata))
	return encryptDict, nil
}

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

// ToGoImage converts the JBIG2Image to the golang image.Image.
func (jbig2Img *JBIG2Image) ToGoImage() (image.Image, error) {
	const procJBIG2ImageToGoImage = "JBIG2Image.ToGoImage"
	if jbig2Img.Data == nil {
		return nil, jbig2errors.Error(procJBIG2ImageToGoImage, "image data not defined")
	}
	if jbig2Img.Width == 0 || jbig2Img.Height == 0 {
		return nil, jbig2errors.Error(procJBIG2ImageToGoImage, "image height or width not defined")
	}
	image, err := imageutil.NewImage(jbig2Img.Width, jbig2Img.Height, 1, 1, jbig2Img.Data, nil, nil)
	if err != nil {
		return nil, err
	}
	return image, nil
}

// Bytes returns the packed bytes, including any partial trailing byte zero-
// padded.
func (bw *BitWriter) Bytes() []byte {
	data := append([]byte(nil), bw.data...)
	if bw.nbits > 0 {
		data = append(data, byte(bw.acc<<(8-bw.nbits)))
	}
	return data
}

func parseStdEncryptDict(stdEncrypt *security.StdEncryptDict, ed *PdfObjectDictionary) error {
	R, ok := ed.Get("R").(*PdfObjectInteger)
	if !ok {
		return errors.New("encrypt dictionary missing R")
	}
	if *R < 2 || *R > 6 {
		return fmt.Errorf("invalid R (%d)", *R)
	}
	stdEncrypt.R = int(*R)
	O, ok := ed.GetString("O")
	if !ok {
		return errors.New("encrypt dictionary missing O")
	}
	if stdEncrypt.R == 5 || stdEncrypt.R == 6 {
		if len(O) < 48 {
			return fmt.Errorf("Length(O) < 48 (%d)", len(O))
		}
	} else if len(O) != 32 {
		return fmt.Errorf("Length(O) != 32 (%d)", len(O))
	}
	stdEncrypt.O = []byte(O)
	U, ok := ed.GetString("U")
	if !ok {
		return errors.New("encrypt dictionary missing U")
	}
	if stdEncrypt.R == 5 || stdEncrypt.R == 6 {
		if len(U) < 48 {
			return fmt.Errorf("Length(U) < 48 (%d)", len(U))
		}
	} else if len(U) != 32 {
		common.Log.Debug("Warning: Length(U) != 32 (%d)", len(U))
	}
	stdEncrypt.U = []byte(U)
	if stdEncrypt.R >= 5 {
		OE, ok2 := ed.GetString("OE")
		if !ok2 {
			return errors.New("encrypt dictionary missing OE")
		} else if len(OE) != 32 {
			return fmt.Errorf("Length(OE) != 32 (%d)", len(OE))
		}
		stdEncrypt.OE = []byte(OE)
		UE, ok2 := ed.GetString("UE")
		if !ok2 {
			return errors.New("encrypt dictionary missing UE")
		} else if len(UE) != 32 {
			return fmt.Errorf("Length(UE) != 32 (%d)", len(UE))
		}
		stdEncrypt.UE = []byte(UE)
	}
	P, ok := ed.Get("P").(*PdfObjectInteger)
	if !ok {
		return errors.New("encrypt dictionary missing permissions attr")
	}
	stdEncrypt.P = security.Permissions(*P)
	if stdEncrypt.R == 6 {
		Perms, ok2 := ed.GetString("Perms")
		if !ok2 {
			return errors.New("encrypt dictionary missing Perms")
		} else if len(Perms) != 16 {
			return fmt.Errorf("Length(Perms) != 16 (%d)", len(Perms))
		}
		stdEncrypt.Perms = []byte(Perms)
	}
	if encryptMetadata, isBoolObj := ed.Get("EncryptMetadata").(*PdfObjectBool); isBoolObj {
		stdEncrypt.EncryptMetadata = bool(*encryptMetadata)
	} else {
		stdEncrypt.EncryptMetadata = true
	}
	return nil
}

// EncodeJBIG2Image encodes 'img' into jbig2 encoded bytes stream, using default encoder settings.
func (jbig2 *JBIG2Encoder) EncodeJBIG2Image(img *JBIG2Image) ([]byte, error) {
	const procEncodeJBIG2Image = "core.EncodeJBIG2Image"
	if err := jbig2.AddPageImage(img, &jbig2.DefaultPageSettings); err != nil {
		return nil, jbig2errors.Wrap(err, procEncodeJBIG2Image, "")
	}
	return jbig2.Encode()
}

// ToInt64Slice returns a slice of all array elements as an int64 slice. An error is returned if the
// array non-integer objects. Each element can only be PdfObjectInteger.
func (arr *PdfObjectArray) ToInt64Slice() ([]int64, error) {
	var nums []int64
	for _, elem := range arr.Elements() {
		if num, ok := elem.(*PdfObjectInteger); ok {
			nums = append(nums, int64(*num))
		} else {
			return nil, ErrTypeError
		}
	}
	return nums, nil
}

func (parser *PdfParser) xrefTableHealthCheck(trailer *PdfObjectDictionary) error {
	if size, ok := trailer.Get("Size").(*PdfObjectInteger); ok {
		half := int64(*size) / 2
		if int64(len(parser.currentXrefs().ObjectMap)) < half {
			return errors.New("mapped object too low compared with total trailer size")
		}
	}
	root := trailer.Get("Root")
	if root == nil {
		return errors.New("root object is missing")
	}
	obj, err := parser.Resolve(root)
	if err != nil {
		return err
	}
	if _, ok := obj.(*PdfObjectDictionary); !ok {
		return errors.New("catalog object is not a dictionary")
	}
	pages := obj.(*PdfObjectDictionary).Get("Pages")
	if pages == nil {
		return errors.New("pages object not found in catalog")
	}
	pagesObj, err := parser.Resolve(pages)
	if err != nil {
		return err
	}
	if _, ok := pagesObj.(*PdfObjectDictionary); !ok {
		return errors.New("pages object is not a dictionary")
	}
	return nil
}

const adobePubSecFilter = "Adobe.PubSec"

// String returns a string describing `ref`.
func (ref *PdfObjectReference) String() string {
	return fmt.Sprintf("Ref(%d %d)", ref.ObjectNumber, ref.GenerationNumber)
}
func (num *PdfObjectInteger) String() string { return fmt.Sprintf("%d", *num) }
func (parser *PdfParser) repairLocateXref(cursor *readerAtCursor) (int64, error) {
	backLen := int64(1000)
	cursor.SetFileOffset(cursor.GetFileOffset() - backLen)
	fileOffset := cursor.GetFileOffset()
	data := make([]byte, backLen)
	cursor.Read(data)
	allStringIndex := xrefLineRegexp.FindAllStringIndex(string(data), -1)
	if len(allStringIndex) < 1 {
		common.Log.Debug("ERROR: Repair: xref not found!")
		return 0, errors.New("repair: xref not found")
	}
	idx := int64(allStringIndex[len(allStringIndex)-1][0])
	pos := fileOffset + idx
	return pos, nil
}

func (limits decodeLimits) readAll(filterName string, r io.Reader) ([]byte, error) {
	remaining := limits.remaining()
	data := AcquireBytes(poolSize16K)[:0]
	var err error
	for {
		if len(data) == cap(data) {
			if int64(len(data)) >= remaining {
				var ok bool
				if ok, err = readerHasData(r); ok {
					ReleaseBytes(data)
					return nil, limits.exceeded(filterName, remaining)
				}
				break
			}
			grown := AcquireBytes(int(min(2*int64(cap(data)), remaining, math.MaxInt)))[:len(data)]
			copy(grown, data)
			ReleaseBytes(data)
			data = grown
		}
		var read int
		read, err = r.Read(data[len(data):cap(data)])
		data = data[:len(data)+read]
		if int64(len(data)) > remaining {
			ReleaseBytes(data)
			return nil, limits.exceeded(filterName, remaining)
		}
		if err != nil {
			break
		}
	}
	if err == io.EOF {
		err = nil
	}
	if len(data) == 0 {
		ReleaseBytes(data)
		return nil, err
	}
	if len(data) < minPoolBufSize {
		dst := make([]byte, len(data))
		copy(dst, data)
		ReleaseBytes(data)
		return dst, err
	}
	if cap(data) > poolSize16M && cap(data)-len(data) > len(data)/2 {
		dst := make([]byte, len(data))
		copy(dst, data)
		data = dst
	}
	return data, err
}

// UpdateParams updates the parameter values of the encoder.
// Implements StreamEncoder interface.
func (jbig2 *JBIG2Encoder) UpdateParams(params *PdfObjectDictionary) {
	bitsPerComponent, err := GetNumberAsInt64(params.Get("BitsPerComponent"))
	if err == nil {
		jbig2.BitsPerComponent = int(bitsPerComponent)
	}
	width, err := GetNumberAsInt64(params.Get("Width"))
	if err == nil {
		jbig2.Width = int(width)
	}
	height, err := GetNumberAsInt64(params.Get("Height"))
	if err == nil {
		jbig2.Height = int(height)
	}
	colorComponents, err := GetNumberAsInt64(params.Get("ColorComponents"))
	if err == nil {
		jbig2.ColorComponents = int(colorComponents)
	}
}

// Clear resets the dictionary to an empty state.
func (dict *PdfObjectDictionary) Clear() {
	dict.mu.Lock()
	defer dict.mu.Unlock()
	dict.keys = []PdfObjectName{}
	dict.dict = map[PdfObjectName]PdfObject{}
}

// EncodeBytes implements support for LZW encoding.  Currently not supporting predictors (raw compressed data only).
// Only supports the Early change = 1 algorithm (compress/lzw) as the other implementation
// does not have a write method.
// TODO: Consider refactoring compress/lzw to allow both.
func (enc *LZWEncoder) EncodeBytes(data []byte) ([]byte, error) {
	if enc.Predictor != 1 {
		return nil, fmt.Errorf("LZW Predictor = 1 only supported yet")
	}
	if enc.EarlyChange == 1 {
		return nil, fmt.Errorf("LZW Early Change = 0 only supported yet")
	}
	var buf bytes.Buffer
	writer := lzw.NewWriter(&buf, lzw.MSB, 8)
	writer.Write(data)
	writer.Close()
	return buf.Bytes(), nil
}

func (parser *PdfParser) parseXrefTable(cursor *readerAtCursor) (*PdfObjectDictionary, error) {
	defer parser.repairedXrefs.Store(nil)
	var dict *PdfObjectDictionary
	textLine, err := parser.readTextLine(cursor)
	if err != nil {
		return nil, err
	}
	if parser.compliance && strings.Count(strings.TrimPrefix(textLine, "xref"), " ") > 0 {
		parser.metadata.invalidSeparationAfterXRef = true
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("xref first line: %s", textLine)
	}
	start := -1
	subsectionCount := 0
	ok := false
	s := ""
	for {
		parser.skipSpaces(cursor)
		_, err2 := cursor.Peek(1)
		if err2 != nil {
			return nil, err2
		}
		textLine, err2 = parser.readTextLine(cursor)
		if err2 != nil {
			return nil, err2
		}
		stringSubmatch := xrefSubsectionRegexp.FindStringSubmatch(textLine)
		if len(stringSubmatch) == 0 {
			ok2 := len(s) > 0
			s += textLine + "\n"
			if ok2 {
				stringSubmatch = xrefSubsectionRegexp.FindStringSubmatch(s)
			}
		}
		if len(stringSubmatch) == 3 {
			if parser.compliance && !parser.metadata.invalidSubsectionHeader {
				var (
					ok2    bool
					digits int
				)
				for _, r := range textLine {
					if unicode.IsDigit(r) {
						if ok2 {
							break
						}
						continue
					}
					if !ok2 {
						ok2 = true
					}
					digits++
				}
				if digits > 1 {
					parser.metadata.invalidSubsectionHeader = true
				}
			}
			firstObj, _ := strconv.Atoi(stringSubmatch[1])
			count, _ := strconv.Atoi(stringSubmatch[2])
			start = firstObj
			subsectionCount = count
			ok = true
			s = ""
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("xref subsection: first object: %d objects: %d", start, subsectionCount)
			}
			continue
		}
		strs := xrefEntryRegexp.FindStringSubmatch(textLine)
		if len(strs) == 4 {
			if !ok {
				common.Log.Debug("ERROR Xref invalid format!\n")
				return nil, errors.New("xref invalid format")
			}
			offset, _ := strconv.ParseInt(strs[1], 10, 64)
			gen, _ := strconv.Atoi(strs[2])
			kind := strs[3]
			s = ""
			if strings.ToLower(kind) == "n" && offset > 1 {
				xrefObj, ok2 := parser.currentXrefs().ObjectMap[start]
				if !ok2 || gen > xrefObj.Generation {
					entry := XrefObject{ObjectNumber: start, XType: XrefTypeTableEntry, Offset: offset, Generation: gen}
					parser.currentXrefs().ObjectMap[start] = entry
				}
			}
			start++
			continue
		}
		if (len(textLine) > 6) && (textLine[:7] == "trailer") {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Found trailer - %s", textLine)
			}
			if len(textLine) > 9 {
				fileOffset := cursor.GetFileOffset()
				cursor.SetFileOffset(fileOffset - int64(len(textLine)) + 7)
			}
			parser.skipSpaces(cursor)
			parser.skipComments(cursor)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Reading trailer dict!")
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("peek: \"%s\"", textLine)
			}
			dict, err2 = parser.parseDict(cursor, 0)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("EOF reading trailer dict!")
			}
			if err2 != nil {
				common.Log.Debug("Error parsing trailer dict (%s)", err2)
				return nil, err2
			}
			break
		}
		if textLine == "%%EOF" {
			common.Log.Debug("ERROR: end of file - trailer not found - error!")
			return nil, errors.New("end of file - trailer not found")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("xref more : %s", textLine)
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("EOF parsing xref table!")
	}
	if parser.xrefType == nil {
		xrefType := XrefTypeTableEntry
		parser.xrefType = &xrefType
	}
	return dict, nil
}

// RunLengthEncoder represents Run length encoding.
type RunLengthEncoder struct{ limits decodeLimits }

func scaleDecodeValue(v uint, dmin, dmax float64) float64 {
	f := precision.RoundFloat((float64(v) / 255.0), 6)
	delta := precision.RoundFloat((dmax-dmin)*f, 6)
	return precision.RoundFloat((dmin+delta)*255.0, 6)
}

// IsDecimalDigit checks if the character is a part of a decimal number string.
func IsDecimalDigit(c byte) bool { return '0' <= c && c <= '9' }

// FormatWriteDirectionLTR formats a string for left-to-right rendering, handling bidirectional runs.
func FormatWriteDirectionLTR(str string, isLTR bool) string {
	s := str
	if s == "" {
		return ""
	}
	paragraph := bidi.Paragraph{}
	_, err := paragraph.SetString(str)
	if err != nil {
		return s
	}
	ordering, err := paragraph.Order()
	if err != nil {
		return s
	}
	numRuns := ordering.NumRuns()
	strs := make([]string, numRuns)
	for i := 0; i < ordering.NumRuns(); i++ {
		run := ordering.Run(i)
		text := run.String()
		if run.Direction() == bidi.RightToLeft {
			text = bidi.ReverseString(text)
		}
		if isLTR {
			strs[i] = text
		} else {
			strs[numRuns-1] = text
		}
		numRuns--
	}
	if len(strs) != ordering.NumRuns() {
		return str
	}
	s = strings.Join(strs, "")
	return s
}

type byteReader interface {
	io.Reader
	io.ByteReader
}

// Resolve resolves the reference and returns the indirect or stream object.
// If the reference cannot be resolved, a *PdfObjectNull object is returned.
func (ref *PdfObjectReference) Resolve() PdfObject {
	if ref.parser == nil {
		return MakeNull()
	}
	reference, _, err := ref.parser.resolveReference(ref)
	if err != nil {
		common.Log.Debug("ERROR resolving reference: %v - returning null object", err)
		return MakeNull()
	}
	if reference == nil {
		common.Log.Debug("ERROR resolving reference: nil object - returning a null object")
		return MakeNull()
	}
	return reference
}

// UpdateParams updates the parameter values of the encoder.
func (rawEncoder *RawEncoder) UpdateParams(params *PdfObjectDictionary) {}

func (limits decodeLimits) check(filterName string, n int64) error {
	if remaining := limits.remaining(); n > remaining {
		return limits.exceeded(filterName, remaining)
	}
	return nil
}

// NewMultiEncoder returns a new instance of MultiEncoder.
func NewMultiEncoder() *MultiEncoder {
	multi := MultiEncoder{}
	multi.encoders = []StreamEncoder{}
	return &multi
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
func (flate *FlateEncoder) setDecodeLimits(limits decodeLimits) { flate.limits = limits }

// Direct returns the direct object this object represents. For leaf and container
// objects this is the object itself.
func (b *PdfObjectBool) Direct() PdfObject                            { return b }
func (asciiHex *ASCIIHexEncoder) setDecodeLimits(limits decodeLimits) { asciiHex.limits = limits }
func (parser *PdfParser) validateFirstPageSelfContained(linInfo *LinearizationInfo) []LinearizationViolation {
	var violations []LinearizationViolation
	byNumber, err := parser.LookupByNumber(int(linInfo.FirstPageObjNum))
	if err != nil {
		return nil
	}
	dict, ok := GetDict(byNumber)
	if !ok {
		return nil
	}
	for _, objNum := range collectReferencedObjNums(dict, firstPageSkipKeys...) {
		obj, err2 := parser.LookupByNumber(objNum)
		if err2 != nil || obj == nil || IsNullObject(obj) {
			violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectUnresolved, Severity: SeverityError, Message: fmt.Sprintf("first page dependency object %d cannot be resolved", objNum)})
			continue
		}
		offset, ok2 := parser.objectFileOffset(objNum)
		if !ok2 {
			continue
		}
		if linInfo.EndOfFirstPage > 0 && offset >= linInfo.EndOfFirstPage {
			violations = append(violations, LinearizationViolation{Code: LinErrFirstPageObjectOutsideSection, Severity: SeverityError, Message: fmt.Sprintf("first page dependency object %d at offset %d is outside the first-page section (/E %d)", objNum, offset, linInfo.EndOfFirstPage)})
		}
	}
	return violations
}

var firstPageSkipKeys = []string{"Parent", "Thumb", "B"}

// HasInvalidSeparationAfterXRef implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidSeparationAfterXRef() bool {
	return meta.invalidSeparationAfterXRef
}

// Get returns the i-th element of the array or nil if out of bounds (by index).
func (arr *PdfObjectArray) Get(i int) PdfObject {
	if arr == nil || i >= len(arr.elements) || i < 0 {
		return nil
	}
	return arr.elements[i]
}

// SharedObjectEntry is a per-shared-object record. The PDF spec also defines
// optional MD5 signatures and object-count fields; this v1 reader only
// retains the offset/length information needed for layout.
type SharedObjectEntry struct {
	GroupLengthDelta uint32
	SignaturePresent bool
	ObjectsInGroup   uint32
}

func (crypt *PdfCrypt) setLazyStreamData(stream *PdfObjectStream, data []byte) {
	if crypt.lazyStreamData == nil {
		crypt.lazyStreamData = map[*PdfObjectStream][]byte{}
	}
	crypt.lazyStreamData[stream] = data
}

// HeaderCommentBytes gets the header comment bytes.
func (meta ParserMetadata) HeaderCommentBytes() [4]byte { return meta.headerCommentBytes }

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

// GetNumberAsInt64 returns the contents of `obj` as an int64 if it is an integer or float, or an
// error if it isn't. This is for cases where expecting an integer, but some implementations
// actually store the number in a floating point format.
func GetNumberAsInt64(obj PdfObject) (int64, error) {
	switch v := obj.(type) {
	case *PdfObjectFloat:
		common.Log.Debug("Number expected as integer was stored as float (type casting used)")
		return int64(*v), nil
	case *PdfObjectInteger:
		return int64(*v), nil
	case *PdfObjectReference:
		toDirectObject := TraceToDirectObject(obj)
		return GetNumberAsInt64(toDirectObject)
	case *PdfIndirectObject:
		return GetNumberAsInt64(v.PdfObject)
	}
	return 0, ErrNotANumber
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (jpxEncoder *JPXEncoder) MakeStreamDict() *PdfObjectDictionary { return MakeDict() }

func resolveReferencesRecursive(pdfObject PdfObject, depth int) PdfObject {
	if depth > maxTraceDepth {
		common.Log.Error("Trace depth level beyond %d - error!", maxTraceDepth)
		return MakeNull()
	}
	switch v := pdfObject.(type) {
	case *PdfIndirectObject:
		pdfObject = resolveReferencesRecursive((*v).PdfObject, depth+1)
	case *PdfObjectArray:
		for i, elem := range (*v).elements {
			(*v).elements[i] = resolveReferencesRecursive(elem, depth+1)
		}
	case *PdfObjectDictionary:
		v.mu.RLock()
		m := make(map[PdfObjectName]PdfObject, len(v.dict))
		maps.Copy(m, v.dict)
		v.mu.RUnlock()
		for nameKey, obj := range m {
			m[nameKey] = resolveReferencesRecursive(obj, depth+1)
		}
		v.mu.Lock()
		maps.Copy(v.dict, m)
		slices.Sort(v.keys)
		v.mu.Unlock()
	}
	return pdfObject
}

func (r *asciiHexReader) nextDigit() (byte, bool) {
	for {
		b, err := r.src.ReadByte()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			r.err = err
			return 0, false
		}
		if b == '>' {
			r.err = io.EOF
			return 0, false
		}
		if IsWhiteSpace(b) {
			continue
		}
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') {
			return b, true
		}
		common.Log.Debug("ERROR: Invalid ascii hex character (%c)", b)
		r.err = fmt.Errorf("invalid ascii hex character (%c)", b)
		return 0, false
	}
}

// EncodeMandatoryHintStream encodes the two mandatory hint tables (Page
// Offset + Shared Object) and returns the concatenated bytes along with the
// byte offset at which the Shared Object table begins (the /S value of the
// hint stream's stream dictionary). The two tables are byte-aligned.
func EncodeMandatoryHintStream(po *PageOffsetHintTable, so *SharedObjectHintTable) ([]byte, int64, error) {
	bitWriter := NewBitWriter()
	if err := EncodePageOffsetHintTable(bitWriter, po); err != nil {
		return nil, 0, err
	}
	bitWriter.AlignByte()
	bitLen := int64(bitWriter.Len())
	if err := EncodeSharedObjectHintTable(bitWriter, so); err != nil {
		return nil, 0, err
	}
	bitWriter.AlignByte()
	return bitWriter.Bytes(), bitLen, nil
}

func (r *runLengthReader) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if r.runPos < r.runEnd {
			copied := copy(p[total:], r.run[r.runPos:r.runEnd])
			total += copied
			r.runPos += copied
			continue
		}
		if r.err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, r.err
		}
		r.err = r.fillRun()
	}
	return total, nil
}

// DecodeStream implements ASCII85 stream decoding.
func (ascii85 *ASCII85Encoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return ascii85.DecodeBytes(streamObj.Stream)
}

// ToFloat64Array returns a slice of all elements in the array as a float64 slice.  An error is
// returned if the array contains non-numeric objects (each element can be either PdfObjectInteger
// or PdfObjectFloat).
func (arr *PdfObjectArray) ToFloat64Array() ([]float64, error) {
	var floats []float64
	for _, elem := range arr.Elements() {
		switch v := elem.(type) {
		case *PdfObjectInteger:
			floats = append(floats, float64(*v))
		case *PdfObjectFloat:
			floats = append(floats, float64(*v))
		default:
			return nil, ErrTypeError
		}
	}
	return floats, nil
}

// GetFilterName returns the name of the encoding filter.
func (asciiHex *ASCIIHexEncoder) GetFilterName() string { return StreamEncodingFilterNameASCIIHex }

type multiFallbackReader struct {
	src       io.Reader
	fallbacks []StreamEncoder
	started   bool
	err       error
	data      []byte
	pos       int
}

// HasEOLAfterHeader gets information if there is a EOL after the version header.
func (meta ParserMetadata) HasEOLAfterHeader() bool { return meta.eolAfterHeader }

type lookupResult struct {
	obj       PdfObject
	decrypted bool
}

func decodePNGPredictor(data []byte, rowBytes, bpp int) ([]byte, error) {
	stride := rowBytes + 1
	if len(data)%stride != 0 {
		return nil, fmt.Errorf("invalid row length (%d/%d)", len(data), stride)
	}
	if stride > len(data) {
		return nil, errors.New("range check error")
	}
	outOff := 0
	zeroRow := make([]byte, rowBytes)
	prevRow := zeroRow
	for i := 0; i < len(data); i += stride {
		b, row := data[i], data[i+1:i+stride]
		switch b {
		case predictorNone:
		case predictorSub:
			for j := bpp; j < rowBytes; j++ {
				row[j] += row[j-bpp]
			}
		case predictorUp:
			for j := range row {
				row[j] += prevRow[j]
			}
		case predictorAverage:
			for j := range row {
				var leftInt int
				if j >= bpp {
					leftInt = int(row[j-bpp])
				}
				row[j] += byte((leftInt + int(prevRow[j])) / 2)
			}
		case predictorPaeth:
			for j := range row {
				var left, upperLeft byte
				if j >= bpp {
					left, upperLeft = row[j-bpp], prevRow[j-bpp]
				}
				row[j] += paethPredictor(left, prevRow[j], upperLeft)
			}
		default:
			return nil, fmt.Errorf("invalid filter byte (%d)", b)
		}
		copy(data[outOff:], row)
		prevRow = data[outOff : outOff+rowBytes]
		outOff += rowBytes
	}
	return data[:outOff], nil
}

// GetAsFloat64Slice returns the array as []float64 slice.
// Returns an error if not entirely numeric (only PdfObjectIntegers, PdfObjectFloats).
func (arr *PdfObjectArray) GetAsFloat64Slice() ([]float64, error) {
	var floats []float64
	for _, elem := range arr.Elements() {
		numberAsFloat, err := GetNumberAsFloat(TraceToDirectObject(elem))
		if err != nil {
			return nil, fmt.Errorf("array element not a number")
		}
		floats = append(floats, numberAsFloat)
	}
	return floats, nil
}

// EncodeBytes encodes slice of bytes into JBIG2 encoding format.
// The input 'data' must be an image. In order to Decode it a user is responsible to
// load the codec ('png', 'jpg').
// Returns jbig2 single page encoded document byte slice. The encoder uses DefaultPageSettings
// to encode given image.
func (jbig2 *JBIG2Encoder) EncodeBytes(data []byte) ([]byte, error) {
	const procJBIG2EncoderEncodeBytes = "JBIG2Encoder.EncodeBytes"
	if jbig2.ColorComponents != 1 || jbig2.BitsPerComponent != 1 {
		return nil, jbig2errors.Errorf(procJBIG2EncoderEncodeBytes, "provided invalid input image. JBIG2 Encoder requires binary images data")
	}
	var (
		bm  *bitmap.Bitmap
		err error
	)
	ok := (jbig2.Width * jbig2.Height) == len(data)
	if ok {
		bm, err = bitmap.NewWithUnpaddedData(jbig2.Width, jbig2.Height, data)
	} else {
		bm, err = bitmap.NewWithData(jbig2.Width, jbig2.Height, data)
	}
	if err != nil {
		return nil, err
	}
	defaultPageSettings := jbig2.DefaultPageSettings
	if err = defaultPageSettings.Validate(); err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2EncoderEncodeBytes, "")
	}
	if jbig2.doc == nil {
		jbig2.doc = document.InitEncodeDocument(defaultPageSettings.FileMode)
	}
	switch defaultPageSettings.Compression {
	case JB2Generic:
		if err = jbig2.doc.AddGenericPage(bm, defaultPageSettings.DuplicatedLinesRemoval); err != nil {
			return nil, jbig2errors.Wrap(err, procJBIG2EncoderEncodeBytes, "")
		}
	case JB2SymbolCorrelation:
		return nil, jbig2errors.Error(procJBIG2EncoderEncodeBytes, "symbol correlation encoding not implemented yet")
	case JB2SymbolRankHaus:
		return nil, jbig2errors.Error(procJBIG2EncoderEncodeBytes, "symbol rank haus encoding not implemented yet")
	default:
		return nil, jbig2errors.Error(procJBIG2EncoderEncodeBytes, "provided invalid compression")
	}
	return jbig2.Encode()
}

func (limits decodeLimits) reader(filterName string, r io.Reader) io.Reader {
	return &limitedDecodeReader{src: r, limits: limits, streamLimit: limits.streamLimit(), filterName: filterName}
}

func checkMainXrefOffset(mainXrefOffset, endOfFirstPage, fileSize int64) *LinearizationViolation {
	if mainXrefOffset <= 0 || mainXrefOffset >= fileSize || mainXrefOffset <= endOfFirstPage {
		return &LinearizationViolation{Code: LinErrXRefOffsetMismatch, Severity: SeverityError, Message: fmt.Sprintf("/T (%d) does not point past the first-page section (/E %d) within the file (%d)", mainXrefOffset, endOfFirstPage, fileSize)}
	}
	return nil
}

func equalObjects(pdfObjectDictionary, other PdfObject, depth int, seen map[indirectPair]bool) bool {
	if depth > maxTraceDepth {
		common.Log.Error("Trace depth level beyond %d - error!", maxTraceDepth)
		return false
	}
	if pdfObjectDictionary == nil && other == nil {
		return true
	} else if pdfObjectDictionary == nil || other == nil {
		return false
	}
	if reflect.TypeOf(pdfObjectDictionary) != reflect.TypeOf(other) {
		return false
	}
	switch v := pdfObjectDictionary.(type) {
	case *PdfObjectNull:
		return true
	case *PdfObjectReference:
		ref := other.(*PdfObjectReference)
		return v.ObjectNumber == ref.ObjectNumber && v.GenerationNumber == ref.GenerationNumber
	case *PdfObjectName:
		return *v == *other.(*PdfObjectName)
	case *PdfObjectString:
		return *v == *other.(*PdfObjectString)
	case *PdfObjectInteger:
		return *v == *other.(*PdfObjectInteger)
	case *PdfObjectBool:
		return *v == *other.(*PdfObjectBool)
	case *PdfObjectFloat:
		return *v == *other.(*PdfObjectFloat)
	case *PdfIndirectObject:
		if seen == nil {
			seen = map[indirectPair]bool{}
		}
		indirect := other.(*PdfIndirectObject)
		pair := indirectPair{v, indirect}
		if seen[pair] {
			return true
		}
		seen[pair] = true
		return equalObjects(TraceToDirectObject(pdfObjectDictionary), TraceToDirectObject(other), depth+1, seen)
	case *PdfObjectArray:
		arr := other.(*PdfObjectArray)
		if len((*v).elements) != len((*arr).elements) {
			return false
		}
		for i, elem := range (*v).elements {
			if !equalObjects(elem, (*arr).elements[i], depth+1, seen) {
				return false
			}
		}
		return true
	case *PdfObjectDictionary:
		dict := other.(*PdfObjectDictionary)
		names := v.Keys()
		otherNames := dict.Keys()
		if len(names) != len(otherNames) {
			return false
		}
		for _, name := range names {
			obj := v.Get(name)
			otherVal := dict.Get(name)
			if otherVal == nil || !equalObjects(obj, otherVal, depth+1, seen) {
				return false
			}
		}
		return true
	case *PdfObjectStream:
		stream := other.(*PdfObjectStream)
		if !bytes.Equal(v.Stream, stream.Stream) {
			return false
		}
		return equalObjects(v.PdfObjectDictionary, stream.PdfObjectDictionary, depth+1, seen)
	case *PdfObjectStreams:
		streams := other.(*PdfObjectStreams)
		if len(v.vec) != len(streams.vec) {
			return false
		}
		for i, obj := range v.vec {
			if !equalObjects(obj, streams.vec[i], depth+1, seen) {
				return false
			}
		}
		return true
	default:
		common.Log.Error("ERROR: Unknown type: %T - should never happen!", pdfObjectDictionary)
	}
	return false
}

// EncodeStream encodes the stream data using the encoded specified by the stream's dictionary.
func EncodeStream(streamObj *PdfObjectStream) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encode stream")
	}
	encoderFromStream, err := NewEncoderFromStream(streamObj)
	if err != nil {
		common.Log.Debug("Stream decoding failed: %v", err)
		return err
	}
	if lzw, ok := encoderFromStream.(*LZWEncoder); ok {
		lzw.EarlyChange = 0
		streamObj.Set("EarlyChange", MakeInteger(0))
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encoder: %+v\n", encoderFromStream)
	}
	bytes, err := encoderFromStream.EncodeBytes(streamObj.Stream)
	if err != nil {
		common.Log.Debug("Stream encoding failed: %v", err)
		return err
	}
	streamObj.Stream = bytes
	streamObj.Set("Length", MakeInteger(int64(len(bytes))))
	return nil
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (rawEncoder *RawEncoder) MakeStreamDict() *PdfObjectDictionary { return MakeDict() }

// HasDataAfterEOF checks if there is some data after EOF marker.
func (meta ParserMetadata) HasDataAfterEOF() bool { return meta.dataAfterEOF }

// GetIntVal returns the int value represented by the PdfObject directly or indirectly if contained within an
// indirect object. On type mismatch the found bool flag returned is false and a nil pointer is returned.
func GetIntVal(obj PdfObject) (val int, ok bool) {
	num, ok := TraceToDirectObject(obj).(*PdfObjectInteger)
	if ok && num != nil {
		return int(*num), true
	}
	return 0, false
}

// Len returns the number of complete bytes plus 1 for a partial byte.
func (bw *BitWriter) Len() int {
	if bw.nbits == 0 {
		return len(bw.data)
	}
	return len(bw.data) + 1
}

func checkHintStreams(linInfo *LinearizationInfo, fileSize int64) []LinearizationViolation {
	var violations []LinearizationViolation
	if len(linInfo.HintOffsets) == 0 {
		violations = append(violations, LinearizationViolation{Code: LinErrHintStreamMissing, Severity: SeverityWarning, Message: "/H is absent or malformed in the linearization parameter dictionary"})
		return violations
	}
	if linInfo.HintTables == nil {
		violations = append(violations, LinearizationViolation{Code: LinErrHintStreamMissing, Severity: SeverityWarning, Message: "primary hint stream could not be decoded"})
		return violations
	}
	if perPage := len(linInfo.HintTables.PageOffset.PerPage); int64(perPage) != int64(linInfo.NumPages) {
		violations = append(violations, LinearizationViolation{Code: LinErrHintPageCountMismatch, Severity: SeverityError, Message: fmt.Sprintf("page offset hint table has %d per-page entries but /N is %d", perPage, linInfo.NumPages)})
	}
	hintRange := linInfo.HintOffsets[0]
	if hintRange.Offset <= 0 || hintRange.Offset >= fileSize || (linInfo.EndOfFirstPage > 0 && hintRange.Offset >= linInfo.EndOfFirstPage) {
		violations = append(violations, LinearizationViolation{Code: LinErrHintStreamMisplaced, Severity: SeverityWarning, Message: fmt.Sprintf("primary hint stream offset (%d) is not within the first-page section (/E %d, size %d)", hintRange.Offset, linInfo.EndOfFirstPage, fileSize)})
	}
	if hintRange.Length <= 0 || hintRange.Offset < 0 || hintRange.Length > fileSize-hintRange.Offset {
		violations = append(violations, LinearizationViolation{Code: LinErrHintStreamLengthMismatch, Severity: SeverityWarning, Message: fmt.Sprintf("primary hint stream region (offset %d, length %d) exceeds the file size (%d)", hintRange.Offset, hintRange.Length, fileSize)})
	}
	if len(linInfo.HintOffsets) >= 2 {
		overflow := linInfo.HintOffsets[1]
		if overflow.Offset <= 0 || overflow.Length <= 0 || overflow.Offset >= fileSize || overflow.Length > fileSize-overflow.Offset {
			violations = append(violations, LinearizationViolation{Code: LinErrOverflowHintStreamInvalid, Severity: SeverityWarning, Message: fmt.Sprintf("overflow hint stream region (offset %d, length %d) is invalid for file size %d", overflow.Offset, overflow.Length, fileSize)})
		}
	}
	return violations
}

func poolIndexFor(size int) int {
	if size < minPoolBufSize {
		return -1
	}
	for i, bytePoolSize := range bytePoolSizes {
		if size <= bytePoolSize {
			return i
		}
	}
	return -1
}

// UpdateParams updates the parameter values of the encoder.
func (jpxEncoder *JPXEncoder) UpdateParams(params *PdfObjectDictionary) {}

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

func (parser *PdfParser) parseXrefStream(cursor *readerAtCursor, xrefStm *PdfObjectInteger) (*PdfObjectDictionary, error) {
	defer parser.repairedXrefs.Store(nil)
	if xrefStm != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("XRefStm xref table object at %d", xrefStm)
		}
		cursor.SetFileOffset(int64(*xrefStm))
	}
	fileOffset := cursor.GetFileOffset()
	indirectObject, err := parser.parseIndirectObject(cursor, map[int]struct{}{})
	if err != nil {
		common.Log.Debug("ERROR: Failed to read xref object")
		return nil, errors.New("failed to read xref object")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("XRefStm object: %s", indirectObject)
	}
	stream, ok := indirectObject.(*PdfObjectStream)
	if !ok {
		common.Log.Debug("ERROR: XRefStm pointing to non-stream object!")
		return nil, errors.New("XRefStm pointing to a non-stream object")
	}
	pdfObjectDictionary := stream.PdfObjectDictionary
	size, ok := stream.Get("Size").(*PdfObjectInteger)
	if !ok {
		common.Log.Debug("ERROR: Missing size from xref stm")
		return nil, errors.New("missing Size from xref stm")
	}
	if int64(*size) > 8388607 {
		common.Log.Debug("ERROR: xref Size exceeded limit, over 8388607 (%d)", *size)
		return nil, errors.New("range check error")
	}
	w := stream.Get("W")
	arr, ok := w.(*PdfObjectArray)
	if !ok {
		return nil, errors.New("invalid W in xref stream")
	}
	wLen := arr.Len()
	if wLen != 3 {
		common.Log.Debug("ERROR: Unsupported xref stm (len(W) != 3 - %d)", wLen)
		return nil, errors.New("unsupported xref stm len(W) != 3")
	}
	var nums []int64
	for i := 0; i < 3; i++ {
		num, hasNum := GetInt(arr.Get(i))
		if !hasNum {
			return nil, errors.New("invalid w object type")
		}
		nums = append(nums, int64(*num))
	}
	data, err := DecodeStream(stream)
	if err != nil {
		common.Log.Debug("ERROR: Unable to decode stream: %v", err)
		return nil, err
	}
	typeEnd := int(nums[0])
	field2End := int(nums[0] + nums[1])
	field3End := int(nums[0] + nums[1] + nums[2])
	entrySize := int(nums[0] + nums[1] + nums[2])
	if typeEnd < 0 || field2End < 0 || field3End < 0 {
		common.Log.Debug("Error s value < 0 (%d,%d,%d)", typeEnd, field2End, field3End)
		return nil, errors.New("range check error")
	}
	if entrySize == 0 {
		common.Log.Debug("No xref objects in stream (deltab == 0)")
		return pdfObjectDictionary, nil
	}
	numEntries := len(data) / entrySize
	numObjects := 0
	index := stream.Get("Index")
	var objNums []int
	if index != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Index: %b", index)
		}
		indexArr, isArr := index.(*PdfObjectArray)
		if !isArr {
			common.Log.Debug("Invalid Index object (should be an array)")
			return nil, errors.New("invalid Index object")
		}
		if indexArr.Len()%2 != 0 {
			common.Log.Debug("WARNING Failure loading xref stm index not multiple of 2.")
			return nil, errors.New("range check error")
		}
		numObjects = 0
		indexPairs, err2 := indexArr.ToIntegerArray()
		if err2 != nil {
			common.Log.Debug("Error getting index array as integers: %v", err2)
			return nil, err2
		}
		for i := 0; i < len(indexPairs); i += 2 {
			first := indexPairs[i]
			count := indexPairs[i+1]
			for j := 0; j < count; j++ {
				objNums = append(objNums, first+j)
			}
			numObjects += count
		}
	} else {
		for i := 0; i < int(*size); i++ {
			objNums = append(objNums, i)
		}
		numObjects = int(*size)
	}
	if numEntries == numObjects+1 {
		common.Log.Debug("Incompatibility: Index missing coverage of 1 object - appending one - May lead to problems")
		maxNum := numObjects - 1
		for _, num := range objNums {
			if num > maxNum {
				maxNum = num
			}
		}
		objNums = append(objNums, maxNum+1)
		numObjects++
	}
	if numEntries != len(objNums) {
		common.Log.Debug("ERROR: xref stm: num entries != len(indices) (%d != %d)", numEntries, len(objNums))
		return nil, errors.New("xref stm num entries != len(indices)")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Objects count %d", numObjects)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Indices: % d", objNums)
	}
	bytesToInt := func(b []byte) int64 {
		var result int64
		for i := 0; i < len(b); i++ {
			result += int64(b[i]) * (1 << uint(8*(len(b)-i-1)))
		}
		return result
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Decoded stream length: %d", len(data))
	}
	entryIdx := 0
	for i := 0; i < len(data); i += entrySize {
		err2 := checkSliceBounds(len(data), i, i+typeEnd)
		if err2 != nil {
			common.Log.Debug("Invalid slice range: %v", err2)
			return nil, err2
		}
		field1 := data[i : i+typeEnd]
		err2 = checkSliceBounds(len(data), i+typeEnd, i+field2End)
		if err2 != nil {
			common.Log.Debug("Invalid slice range: %v", err2)
			return nil, err2
		}
		field2 := data[i+typeEnd : i+field2End]
		err2 = checkSliceBounds(len(data), i+field2End, i+field3End)
		if err2 != nil {
			common.Log.Debug("Invalid slice range: %v", err2)
			return nil, err2
		}
		field3 := data[i+field2End : i+field3End]
		entryType := bytesToInt(field1)
		val2 := bytesToInt(field2)
		val3 := bytesToInt(field3)
		if nums[0] == 0 {
			entryType = 1
		}
		if entryIdx >= len(objNums) {
			common.Log.Debug("XRef stream - Trying to access index out of bounds - breaking")
			break
		}
		objNum := objNums[entryIdx]
		entryIdx++
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%d. p1: % x", objNum, field1)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%d. p2: % x", objNum, field2)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%d. p3: % x", objNum, field3)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%d. xref: %d %d %d", objNum, entryType, val2, val3)
		}
		switch entryType {
		case 0:
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("- Free object - can probably ignore")
			}
		case 1:
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("- In use - uncompressed via offset %b", field2)
			}
			if val2 == fileOffset {
				common.Log.Debug("Updating object number for XRef table %d -> %d", objNum, stream.ObjectNumber)
				objNum = int(stream.ObjectNumber)
			}
			if xrefObj, ok2 := parser.currentXrefs().ObjectMap[objNum]; !ok2 || int(val3) > xrefObj.Generation {
				entry := XrefObject{ObjectNumber: objNum, XType: XrefTypeTableEntry, Offset: val2, Generation: int(val3)}
				parser.currentXrefs().ObjectMap[objNum] = entry
			}
		case 2:
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("- In use - compressed object")
			}
			if _, ok2 := parser.currentXrefs().ObjectMap[objNum]; !ok2 {
				xrefObj := XrefObject{ObjectNumber: objNum, XType: XrefTypeObjectStream, OsObjNumber: int(val2), OsObjIndex: int(val3)}
				parser.currentXrefs().ObjectMap[objNum] = xrefObj
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("entry: %+v", xrefObj)
				}
			}
		default:
			common.Log.Debug("ERROR: --------INVALID TYPE XrefStm invalid?-------")
			continue
		}
	}
	if parser.xrefType == nil {
		xrefType := XrefTypeObjectStream
		parser.xrefType = &xrefType
	}
	return pdfObjectDictionary, nil
}

type objectStreams map[int]objectStream

// IsOctalDigit checks if a character can be part of an octal digit string.
func IsOctalDigit(c byte) bool { return '0' <= c && c <= '7' }

func (parser *PdfParser) checkPostEOFData() error {
	const eofMarkerText = "%%EOF"
	data := make([]byte, len([]byte(eofMarkerText))+1)
	start := max(parser.fileSize-int64(len(data)), 0)
	if _, err := parser.reader.ReadAt(data, start); err != nil && err != io.EOF {
		return err
	}
	if string(data) == eofMarkerText || string(data) == eofMarkerText+"\n" {
		parser.metadata.dataAfterEOF = true
	}
	return nil
}

// PdfObjectReference represents the primitive PDF reference object.
type PdfObjectReference struct {
	parser           *PdfParser
	ObjectNumber     int64
	GenerationNumber int64
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (multi *MultiEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", multi.GetFilterArray())
	for _, encoder := range multi.encoders {
		streamDict := encoder.MakeStreamDict()
		for _, key := range streamDict.Keys() {
			obj := streamDict.Get(key)
			if key != "Filter" && key != "DecodeParms" {
				dict.Set(key, obj)
			}
		}
	}
	decodeParams := multi.MakeDecodeParams()
	if decodeParams != nil {
		dict.Set("DecodeParms", decodeParams)
	}
	return dict
}
func (jbig2 *JBIG2Encoder) setDecodeLimits(limits decodeLimits) { jbig2.limits = limits }
func (r *ascii85Reader) fillGroup() error {
	var data [5]byte
	n := 0
	ok := false
	for n < 5 {
		b, err := r.src.ReadByte()
		if err != nil {
			if err == io.EOF {
				if n == 0 {
					return io.EOF
				}
				ok = true
				break
			}
			return err
		}
		if IsWhiteSpace(b) {
			continue
		}
		if b == '~' {
			next, err2 := r.src.ReadByte()
			if err2 != nil || next != '>' {
				return errors.New("invalid code encountered")
			}
			ok = true
			break
		}
		if b == 'z' && n == 0 {
			r.group = [4]byte{0, 0, 0, 0}
			r.groupLen = 4
			r.groupPos = 0
			return nil
		}
		if b >= '!' && b <= 'u' {
			data[n] = b - '!'
			n++
			continue
		}
		return errors.New("invalid code encountered")
	}
	outLen := 4
	if ok {
		outLen = max(n-1, 0)
	}
	for i := n; i < 5; i++ {
		data[i] = 84
	}
	word := uint32(data[0])*85*85*85*85 + uint32(data[1])*85*85*85 + uint32(data[2])*85*85 + uint32(data[3])*85 + uint32(data[4])
	r.group[0] = byte(word >> 24)
	r.group[1] = byte(word >> 16)
	r.group[2] = byte(word >> 8)
	r.group[3] = byte(word)
	r.groupLen = outLen
	r.groupPos = 0
	if ok {
		return io.EOF
	}
	return nil
}

// Write outputs the object as it is to be written to file.
func (null *PdfObjectNull) Write() []byte { return []byte("null") }

const (
	poolSize16K  = 16 << 10
	poolSize64K  = 64 << 10
	poolSize256K = 256 << 10
	poolSize1M   = 1 << 20
	poolSize4M   = 4 << 20
	poolSize16M  = 16 << 20
)

// CheckAccessRightsWithCertificate reports whether the certificate can open a
// public-key encrypted document and the permissions its recipient entry
// grants, without authenticating the parser. An unencrypted document grants
// full permissions; a document using the standard security handler returns
// ErrPasswordRequired.
func (parser *PdfParser) CheckAccessRightsWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, security.Permissions, error) {
	if parser.crypter == nil {
		return true, security.PermOwner, nil
	}
	return parser.crypter.checkAccessRightsCertificate(cert, key)
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (enc *LZWEncoder) MakeDecodeParams() PdfObject {
	if enc.Predictor > 1 {
		dict := MakeDict()
		dict.Set("Predictor", MakeInteger(int64(enc.Predictor)))
		if enc.BitsPerComponent != 8 {
			dict.Set("BitsPerComponent", MakeInteger(int64(enc.BitsPerComponent)))
		}
		if enc.Columns != 1 {
			dict.Set("Columns", MakeInteger(int64(enc.Columns)))
		}
		if enc.Colors != 1 {
			dict.Set("Colors", MakeInteger(int64(enc.Colors)))
		}
		return dict
	}
	return nil
}

// ResolveReferencesDeep traverses through object `o` and everything it references, looking up and replacing
// references with indirect objects.
// Optionally a map of already deep-resolved objects can be provided via `traversed`. The `traversed` map
// is updated while traversing the objects to avoid traversing same objects multiple times.
func ResolveReferencesDeep(o PdfObject, traversed map[PdfObject]struct{}) error {
	if traversed == nil {
		traversed = map[PdfObject]struct{}{}
	}
	return resolveReferencesIterative(o, traversed)
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on the current encoder settings.
func (jbig2 *JBIG2Encoder) MakeDecodeParams() PdfObject { return MakeDict() }

// IsPublicKey reports whether the document is encrypted with a public-key
// security handler (/Filter /Adobe.PubSec), which is opened with a recipient
// certificate and private key rather than a password.
func (crypt *PdfCrypt) IsPublicKey() bool { return crypt.isPubSec() }

var stringEscapes = map[byte][]byte{'\n': []byte("\\n"), '\r': []byte("\\r"), '\t': []byte("\\t"), '\b': []byte("\\b"), '\f': []byte("\\f"), '(': []byte("\\("), ')': []byte("\\)"), '\\': []byte("\\\\")}

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

// NewParser creates a new parser for a PDF source. The source's byte length
// is auto-detected via Size() / Seek() / Stat() — *os.File, *bytes.Reader,
// and *io.SectionReader all work. For exotic sources that can't be
// auto-sized, use NewParserAt.
func NewParser(r io.ReaderAt) (*PdfParser, error) {
	size, err := SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewParserAt(r, size)
}

// Append appends PdfObject(s) to the array.
func (arr *PdfObjectArray) Append(objects ...PdfObject) {
	if arr == nil {
		common.Log.Debug("Warn - Attempt to append to a nil array")
		return
	}
	arr.elements = append(arr.elements, objects...)
}

func newMultiEncoderFromStream(streamObj *PdfObjectStream) (*MultiEncoder, error) {
	multiEncoder := NewMultiEncoder()
	limitsFromStream := decodeLimitsFromStream(streamObj)
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return multiEncoder, nil
	}
	var dict *PdfObjectDictionary
	var objs []PdfObject
	decodeParms := pdfObjectDictionary.Get("DecodeParms")
	if decodeParms != nil {
		single, ok := decodeParms.(*PdfObjectDictionary)
		if ok {
			dict = single
		}
		arr, isArr := decodeParms.(*PdfObjectArray)
		if isArr {
			for _, elem := range arr.Elements() {
				elem = TraceToDirectObject(elem)
				if elemDict, isDict := elem.(*PdfObjectDictionary); isDict {
					objs = append(objs, elemDict)
				} else {
					objs = append(objs, MakeDict())
				}
			}
		}
	}
	decodeParms = pdfObjectDictionary.Get("Filter")
	if decodeParms == nil {
		return nil, fmt.Errorf("filter missing")
	}
	arr, ok := decodeParms.(*PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("multi filter can only be made from array")
	}
	for i, elem := range arr.Elements() {
		name, isName := elem.(*PdfObjectName)
		if !isName {
			return nil, fmt.Errorf("multi filter array element not a name")
		}
		var obj PdfObject
		if dict != nil {
			obj = dict
		} else {
			if len(objs) > 0 {
				if i >= len(objs) {
					return nil, fmt.Errorf("missing elements in decode params array")
				}
				obj = objs[i]
			}
		}
		var current *PdfObjectDictionary
		if d, isDict := obj.(*PdfObjectDictionary); isDict {
			current = d
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Next name: %s, dp: %v, dParams: %v", *name, obj, current)
		}
		switch *name {
		case StreamEncodingFilterNameFlate:
			flateEncoderFromStream, err := newFlateEncoderFromStream(streamObj, current)
			if err != nil {
				return nil, err
			}
			applyDecodeLimits(flateEncoderFromStream, limitsFromStream)
			multiEncoder.AddEncoder(flateEncoderFromStream)
		case StreamEncodingFilterNameLZW:
			lzwEncoderFromStream, err := newLZWEncoderFromStream(streamObj, current)
			if err != nil {
				return nil, err
			}
			applyDecodeLimits(lzwEncoderFromStream, limitsFromStream)
			multiEncoder.AddEncoder(lzwEncoderFromStream)
		case StreamEncodingFilterNameASCIIHex:
			asciiHexEncoder := NewASCIIHexEncoder()
			applyDecodeLimits(asciiHexEncoder, limitsFromStream)
			multiEncoder.AddEncoder(asciiHexEncoder)
		case StreamEncodingFilterNameASCII85:
			asciI85Encoder := NewASCII85Encoder()
			applyDecodeLimits(asciI85Encoder, limitsFromStream)
			multiEncoder.AddEncoder(asciI85Encoder)
		case StreamEncodingFilterNameDCT:
			dctEncoderFromStream, err := newDCTEncoderFromStream(streamObj, multiEncoder)
			if err != nil {
				return nil, err
			}
			applyDecodeLimits(dctEncoderFromStream, limitsFromStream)
			multiEncoder.AddEncoder(dctEncoderFromStream)
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Added DCT encoder...")
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Multi encoder: %#v", multiEncoder)
			}
		case StreamEncodingFilterNameCCITTFax:
			ccittFaxEncoderFromStream, err := newCCITTFaxEncoderFromStream(streamObj, current)
			if err != nil {
				return nil, err
			}
			applyDecodeLimits(ccittFaxEncoderFromStream, limitsFromStream)
			multiEncoder.AddEncoder(ccittFaxEncoderFromStream)
		default:
			common.Log.Error("Unsupported filter %s", *name)
			return nil, fmt.Errorf("invalid filter in multi filter array")
		}
	}
	return multiEncoder, nil
}

// MakeStream creates an PdfObjectStream with specified contents and encoding. If encoding is nil, then raw encoding
// will be used (i.e. no encoding applied).
func MakeStream(contents []byte, encoder StreamEncoder) (*PdfObjectStream, error) {
	stream := &PdfObjectStream{}
	if encoder == nil {
		encoder = NewRawEncoder()
	}
	stream.PdfObjectDictionary = encoder.MakeStreamDict()
	bytes, err := encoder.EncodeBytes(contents)
	if err != nil {
		return nil, err
	}
	stream.Set("Length", MakeInteger(int64(len(bytes))))
	stream.Stream = bytes
	return stream, nil
}

// ASCIIHexEncoder implements ASCII hex encoder/decoder.
type ASCIIHexEncoder struct{ limits decodeLimits }

// AcquireBytes returns a []byte with cap at least size, drawn from a pooled
// size-class bucket. The returned slice has len == size. If size exceeds the
// largest bucket, a plain make([]byte, size) is returned instead; that buffer
// is still safe to pass to ReleaseBytes (it is dropped because its cap does
// not match a bucket). If size <= 0, returns nil.
func AcquireBytes(size int) []byte {
	if size <= 0 {
		return nil
	}
	poolIdx := poolIndexFor(size)
	if poolIdx < 0 {
		return make([]byte, size)
	}
	data := bytePools[poolIdx].Get().(*[]byte)
	return (*data)[:size]
}

// Severity classifies a linearization violation as a hard error or a warning.
// The ISO 32000-2 Annex F checks differ in how strictly they bear on whether a
// PDF works in Fast Web View: some violations break it outright (Error), others
// only degrade or signal staleness (Warning).
type Severity int

// ShallowEqualObjects returns true if `obj1` and `obj2` are equal at the top level
// without recursing into nested objects. Indirect objects are compared by object number
// (identity), not by content. Dictionary and array values are compared by pointer equality.
func ShallowEqualObjects(obj1, obj2 PdfObject) bool {
	if obj1 == nil && obj2 == nil {
		return true
	} else if obj1 == nil || obj2 == nil {
		return false
	}
	if reflect.TypeOf(obj1) != reflect.TypeOf(obj2) {
		return false
	}
	switch v := obj1.(type) {
	case *PdfObjectNull:
		return true
	case *PdfObjectReference:
		ref := obj2.(*PdfObjectReference)
		return v.ObjectNumber == ref.ObjectNumber && v.GenerationNumber == ref.GenerationNumber
	case *PdfObjectName:
		return *v == *obj2.(*PdfObjectName)
	case *PdfObjectString:
		return *v == *obj2.(*PdfObjectString)
	case *PdfObjectInteger:
		return *v == *obj2.(*PdfObjectInteger)
	case *PdfObjectBool:
		return *v == *obj2.(*PdfObjectBool)
	case *PdfObjectFloat:
		return *v == *obj2.(*PdfObjectFloat)
	case *PdfIndirectObject:
		indirect := obj2.(*PdfIndirectObject)
		return v.ObjectNumber == indirect.ObjectNumber && v.GenerationNumber == indirect.GenerationNumber
	case *PdfObjectArray:
		arr := obj2.(*PdfObjectArray)
		if len(v.elements) != len(arr.elements) {
			return false
		}
		for i, elem := range v.elements {
			if elem != arr.elements[i] {
				return false
			}
		}
		return true
	case *PdfObjectDictionary:
		dict := obj2.(*PdfObjectDictionary)
		names := v.Keys()
		otherKeys := dict.Keys()
		if len(names) != len(otherKeys) {
			return false
		}
		for _, name := range names {
			obj := v.Get(name)
			otherVal := dict.Get(name)
			if obj != otherVal {
				return false
			}
		}
		return true
	case *PdfObjectStream:
		stream := obj2.(*PdfObjectStream)
		if v.PdfObjectDictionary != stream.PdfObjectDictionary {
			return false
		}
		return bytes.Equal(v.Stream, stream.Stream)
	default:
		common.Log.Error("ERROR: Unknown type: %T - should never happen!", obj1)
	}
	return false
}

// UpdateParams updates the parameter values of the encoder.
func (flate *FlateEncoder) UpdateParams(params *PdfObjectDictionary) {
	predictor, err := GetNumberAsInt64(params.Get("Predictor"))
	if err == nil {
		flate.Predictor = int(predictor)
	}
	bitsPerComponent, err := GetNumberAsInt64(params.Get("BitsPerComponent"))
	if err == nil {
		flate.BitsPerComponent = int(bitsPerComponent)
	}
	width, err := GetNumberAsInt64(params.Get("Width"))
	if err == nil {
		flate.Columns = int(width)
	}
	colorComponents, err := GetNumberAsInt64(params.Get("ColorComponents"))
	if err == nil {
		flate.Colors = int(colorComponents)
	}
}

func (parser *PdfParser) parseNumber(cursor *readerAtCursor) (PdfObject, error) {
	return ParseNumber(cursor.Underlying())
}

// DecodeReader returns a reader that LZW-decodes from r. EarlyChange selects
// which LZW variant (one-code-early vs postponed code-length increase),
// matching DecodeBytes. The predictor is NOT applied — see FlateEncoder's
// DecodeReader for rationale.
func (enc *LZWEncoder) DecodeReader(r io.Reader) io.Reader {
	if enc.EarlyChange == 1 {
		return enc.limits.reader(StreamEncodingFilterNameLZW, tifflzw.NewReader(r, tifflzw.MSB, 8))
	}
	return enc.limits.reader(StreamEncodingFilterNameLZW, lzw.NewReader(r, lzw.MSB, 8))
}

// IsLinearized reports whether the PDF is a linearized ("fast web view") file
// as defined in ISO 32000-2 Annex F.
func (parser *PdfParser) IsLinearized() bool { return parser.linearized }

func (crypt *PdfCrypt) pubSecKeyBits() int {
	if crypt.encrypt.V >= 4 {
		for _, name := range []string{crypt.recipientFilter, crypt.streamFilter, crypt.stringFilter} {
			if name == "" || name == "Identity" {
				continue
			}
			if filter, ok := crypt.cryptFilters[name]; ok && filter.KeyLength() > 0 {
				return filter.KeyLength() * 8
			}
		}
	}
	return crypt.encrypt.Length
}

// HasNonConformantStream implements core.ParserMetadata.
func (meta ParserMetadata) HasNonConformantStream() bool { return meta.nonConformantStream }

type limitedDecoder interface{ setDecodeLimits(decodeLimits) }

// DecodeStream decodes RunLengthEncoded stream object and give back decoded bytes.
func (runLength *RunLengthEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return runLength.DecodeBytes(streamObj.Stream)
}

// Read implements io.Reader.
func (cursor *readerAtCursor) Read(p []byte) (int, error) { return cursor.buffered.Read(p) }

type repairResult struct {
	xrefs *XrefTable
	err   error
}

func (parser *PdfParser) filterEOFMarkersByContext(markers []eofMarker) ([]eofMarker, bool) {
	m := make(map[int64]struct{}, len(parser.xrefOffsets)+1)
	if parser.startxrefOffset > 0 {
		m[parser.startxrefOffset] = struct{}{}
	}
	for _, xrefOffset := range parser.xrefOffsets {
		m[xrefOffset] = struct{}{}
	}
	nums := append([]int64(nil), parser.xrefSectionEnds...)
	slices.Sort(nums)
	var result []eofMarker
	seen := make(map[int64]struct{}, len(m))
	prevEnd := make(map[int]int64, len(nums))
	ok := true
	for _, marker := range markers {
		idx := sort.Search(len(nums), func(i int) bool { return nums[i] > marker.markerOffset }) - 1
		if idx < 0 {
			continue
		}
		if prevOff, ok2 := prevEnd[idx]; ok2 {
			if tailOffset, ok3 := parser.trailerTailOffset(prevOff, marker.markerOffset); ok3 {
				if _, ok4 := m[tailOffset]; ok4 {
					ok = false
				}
			}
			continue
		}
		tail, ok2 := parser.trailerTailOffset(nums[idx], marker.markerOffset)
		if !ok2 {
			continue
		}
		if _, ok3 := m[tail]; !ok3 {
			continue
		}
		prevEnd[idx] = marker.endOffset
		if _, ok3 := seen[tail]; ok3 {
			ok = false
		}
		seen[tail] = struct{}{}
		result = append(result, marker)
	}
	return result, ok
}

var objHeaderRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s+obj")

// NewRunLengthEncoder makes a new run length encoder
func NewRunLengthEncoder() *RunLengthEncoder { return &RunLengthEncoder{} }

func (r *asciiHexReader) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if r.err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, r.err
		}
		b, ok := r.nextDigit()
		if !ok {
			continue
		}
		lo, ok := r.nextDigit()
		if !ok {
			if r.err == io.EOF {
				p[total] = hexDigitValue(b) << 4
				total++
			}
			continue
		}
		p[total] = hexDigitValue(b)<<4 | hexDigitValue(lo)
		total++
	}
	return total, nil
}

func withPooledBuffer(fn func(*bytes.Buffer)) []byte {
	buffer := acquireBuffer()
	defer releaseBuffer(buffer)
	fn(buffer)
	data := buffer.Bytes()
	out := make([]byte, len(data))
	copy(out, data)
	return out
}

func parsePubSecEncryptDict(crypt *PdfCrypt, ed *PdfObjectDictionary) (*security.PubSecEncryptDict, error) {
	pubSecDict := &security.PubSecEncryptDict{SubFilter: crypt.encrypt.SubFilter}
	switch pubSecDict.SubFilter {
	case security.PubSecSubFilterS4, security.PubSecSubFilterS5:
	case "":
		return nil, errors.New("public-key encrypt dictionary missing SubFilter")
	default:
		return nil, fmt.Errorf("unsupported public-key SubFilter (%s)", pubSecDict.SubFilter)
	}
	switch crypt.encrypt.V {
	case 1, 2, 4, 5:
	default:
		return nil, fmt.Errorf("unsupported V (%d) for public-key encryption", crypt.encrypt.V)
	}
	dict := ed
	crypt.recipientFilter = ""
	if crypt.encrypt.V >= 4 {
		findRecipientFilter := func(filterNames []string) bool {
			for _, name := range filterNames {
				if name == "" || name == "Identity" {
					continue
				}
				if filterDict := crypt.cryptFilterDict(ed, name); filterDict != nil && filterDict.Get("Recipients") != nil {
					dict = filterDict
					crypt.recipientFilter = name
					return true
				}
			}
			return false
		}
		strs := []string{crypt.streamFilter, crypt.stringFilter}
		if eff, ok := GetName(ed.Get("EFF")); ok {
			strs = append(strs, string(*eff))
		}
		if !findRecipientFilter(strs) && ed.Get("Recipients") == nil {
			var sortedKeys []string
			for sKey := range crypt.cryptFilters {
				sortedKeys = append(sortedKeys, sKey)
			}
			sort.Strings(sortedKeys)
			findRecipientFilter(sortedKeys)
		}
	}
	var err error
	pubSecDict.Recipients, err = parsePubSecRecipients(dict)
	if err != nil {
		return nil, err
	}
	if crypt.encrypt.V >= 4 {
		recipientFilter := crypt.recipientFilter
		if recipientFilter == "" {
			recipientFilter = "top-level /Recipients"
		}
		for sKey := range crypt.cryptFilters {
			if sKey == "Identity" || sKey == crypt.recipientFilter {
				continue
			}
			filterDict := crypt.cryptFilterDict(ed, sKey)
			if filterDict == nil || filterDict.Get("Recipients") == nil {
				continue
			}
			pubSecRecipients, err2 := parsePubSecRecipients(filterDict)
			if err2 != nil {
				return nil, err2
			}
			if !slices.EqualFunc(pubSecDict.Recipients, pubSecRecipients, bytes.Equal) {
				return nil, fmt.Errorf("unsupported public-key encryption: %s and crypt filter %s have different recipient lists", recipientFilter, sKey)
			}
		}
	}
	pubSecDict.EncryptMetadata = true
	if encryptMetadata, ok := GetBool(dict.Get("EncryptMetadata")); ok {
		pubSecDict.EncryptMetadata = bool(*encryptMetadata)
	} else if boolObj, hasEncryptMetadata := GetBool(ed.Get("EncryptMetadata")); hasEncryptMetadata && dict != ed {
		pubSecDict.EncryptMetadata = bool(*boolObj)
	}
	return pubSecDict, nil
}

func (parser *PdfParser) clearObjCache() {
	parser.objCache.Clear()
	parser.objStreamMu.Lock()
	parser.objStreams = make(objectStreams)
	parser.objStreamMu.Unlock()
}

// EncodeBytes returns the passed in slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) EncodeBytes(data []byte) ([]byte, error) { return data, nil }

// UpdateParams updates the parameter values of the encoder.
func (dct *DCTEncoder) UpdateParams(params *PdfObjectDictionary) {
	colorComponents, err := GetNumberAsInt64(params.Get("ColorComponents"))
	if err == nil {
		dct.ColorComponents = int(colorComponents)
	}
	bitsPerComponent, err := GetNumberAsInt64(params.Get("BitsPerComponent"))
	if err == nil {
		dct.BitsPerComponent = int(bitsPerComponent)
	}
	width, err := GetNumberAsInt64(params.Get("Width"))
	if err == nil {
		dct.Width = int(width)
	}
	height, err := GetNumberAsInt64(params.Get("Height"))
	if err == nil {
		dct.Height = int(height)
	}
	quality, err := GetNumberAsInt64(params.Get("Quality"))
	if err == nil {
		dct.Quality = int(quality)
	}
	decode, ok := GetArray(params.Get("Decode"))
	if ok {
		dct.Decode, err = decode.ToFloat64Array()
		if err != nil {
			common.Log.Error("Failed converting decode object into arrays: %v", err)
		}
	}
}

// GetUpdatedObjects returns pdf objects which were updated from the specific version (from prevParser).
func (parser *PdfParser) GetUpdatedObjects(prevParser *PdfParser) (map[int64]PdfObject, error) {
	if prevParser == nil {
		return nil, errors.New("previous parser can't be null")
	}
	numbersOfUpdatedObjects, err := parser.getNumbersOfUpdatedObjects(prevParser)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]PdfObject)
	for _, num := range numbersOfUpdatedObjects {
		if byNumber, err2 := parser.LookupByNumber(num); err2 == nil {
			m[int64(num)] = byNumber
		} else {
			return nil, err2
		}
	}
	return m, nil
}

// ResolveReference resolves reference if `o` is a *PdfObjectReference and returns the object referenced to.
// Otherwise returns back `o`.
func ResolveReference(obj PdfObject) PdfObject {
	if ref, ok := obj.(*PdfObjectReference); ok {
		return ref.Resolve()
	}
	return obj
}

// Inspect analyzes the document object structure. Returns a map of object types (by name) with the instance count
// as value.
func (parser *PdfParser) Inspect() (map[string]int, error) { return parser.inspect() }

// GetNumbersAsFloat converts a list of pdf objects representing floats or integers to a slice of
// float64 values.
func GetNumbersAsFloat(objects []PdfObject) (floats []float64, err error) {
	for _, object := range objects {
		numberAsFloat, err2 := GetNumberAsFloat(object)
		if err2 != nil {
			return nil, err2
		}
		floats = append(floats, numberAsFloat)
	}
	return floats, nil
}

// WriteBits writes the low n bits of v into the buffer, MSB-first.
// Caller is responsible for ensuring v fits in n bits.
func (bw *BitWriter) WriteBits(v uint32, n uint) {
	if n == 0 {
		return
	}
	bw.acc = (bw.acc << n) | uint64(v&((1<<n)-1))
	bw.nbits += n
	for bw.nbits >= 8 {
		bw.nbits -= 8
		bw.data = append(bw.data, byte((bw.acc>>bw.nbits)&0xFF))
		bw.acc &= (1 << bw.nbits) - 1
	}
}

func readThumbnailHintTable(br *bitReader) (*ThumbnailHintTable, error) {
	thumb := &ThumbnailHintTable{}
	sts := []struct {
		dest any
		bits uint
	}{{&thumb.FirstThumbnailObjNum, 32}, {&thumb.FirstThumbnailOffset, 32}, {&thumb.NumThumbnails, 32}, {&thumb.NumSharedThumbnails, 32}, {&thumb.BitsPerSharedRef, 16}, {&thumb.BitsPerObjectCountDelta, 16}, {&thumb.LeastObjectsInThumbnail, 32}, {&thumb.BitsPerThumbnailLengthDelta, 16}, {&thumb.LeastThumbnailLength, 32}}
	for _, st := range sts {
		bits, err := br.readBits(st.bits)
		if err != nil {
			return nil, err
		}
		switch v := st.dest.(type) {
		case *uint32:
			*v = bits
		case *uint16:
			*v = uint16(bits)
		}
	}
	return thumb, nil
}

// DecodeReader returns a reader that ASCIIHex-decodes from r. Whitespace is
// skipped, pairs of hex digits become one output byte, and decoding stops
// at the '>' EOD marker. An odd trailing digit before '>' is padded with
// '0' to match DecodeBytes. An input that ends without '>' surfaces as
// io.ErrUnexpectedEOF, matching DecodeBytes' error return for that case.
func (asciiHex *ASCIIHexEncoder) DecodeReader(r io.Reader) io.Reader {
	return asciiHex.limits.reader(StreamEncodingFilterNameASCIIHex, &asciiHexReader{src: newByteReader(r)})
}

// EncodeBytes ASCII encodes the passed in slice of bytes.
func (asciiHex *ASCIIHexEncoder) EncodeBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	for _, b := range data {
		buf.WriteString(fmt.Sprintf("%.2X ", b))
	}
	buf.WriteByte('>')
	return buf.Bytes(), nil
}

const maxCCITTColumns = 65535

func (parser *PdfParser) buildLinearizationInfo(dict *PdfObjectDictionary) *LinearizationInfo {
	linInfo := &LinearizationInfo{FileLength: parser.linearizedFileLength}
	if oN, err := GetNumberAsInt64(dict.Get("O")); err == nil {
		linInfo.FirstPageObjNum = oN
	}
	if eN, err := GetNumberAsInt64(dict.Get("E")); err == nil {
		linInfo.EndOfFirstPage = eN
	}
	if nN, err := GetNumberAsInt64(dict.Get("N")); err == nil {
		if nN > 0 && nN <= maxLinearizationPages {
			linInfo.NumPages = int(nN)
		}
	}
	if tN, err := GetNumberAsInt64(dict.Get("T")); err == nil {
		linInfo.MainXrefOffset = tN
	}
	if pN, err := GetNumberAsInt64(dict.Get("P")); err == nil {
		linInfo.FirstPageNum = int(pN)
	}
	if hArr, ok := GetArray(dict.Get("H")); ok {
		linInfo.HintOffsets = parseHintRanges(hArr)
	}
	return linInfo
}

func (parser *PdfParser) getNumbersOfUpdatedObjects(prevParser *PdfParser) ([]int, error) {
	if prevParser == nil {
		return nil, errors.New("previous parser can't be null")
	}
	fileSize := prevParser.fileSize
	nums := make([]int, 0)
	m := make(map[int]any)
	offsets := make(map[int]int64)
	for i, xrefObj := range parser.currentXrefs().ObjectMap {
		if xrefObj.Offset == 0 {
			if xrefObj.OsObjNumber != 0 {
				if prev, ok := parser.currentXrefs().ObjectMap[xrefObj.OsObjNumber]; ok {
					m[xrefObj.OsObjNumber] = struct{}{}
					offsets[i] = prev.Offset
				} else {
					return nil, errors.New("undefined xref table")
				}
			}
		} else {
			offsets[i] = xrefObj.Offset
		}
	}
	for i, n := range offsets {
		if _, ok := m[i]; ok {
			continue
		}
		if n >= fileSize {
			nums = append(nums, i)
		}
	}
	return nums, nil
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

func (r *multiFallbackReader) Read(p []byte) (int, error) {
	if !r.started {
		r.started = true
		all, err := io.ReadAll(r.src)
		if err != nil {
			r.err = err
			return 0, err
		}
		for _, fallback := range r.fallbacks {
			all, err = fallback.DecodeBytes(all)
			if err != nil {
				r.err = err
				return 0, err
			}
		}
		r.data = all
	}
	if r.err != nil {
		return 0, r.err
	}
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// Write outputs the object as it is to be written to file.
func (ref *PdfObjectReference) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(ref.ObjectNumber, 10))
		buf.WriteString(" ")
		buf.WriteString(strconv.FormatInt(ref.GenerationNumber, 10))
		buf.WriteString(" R")
	})
}

const intBits = 32 << (^uint(0) >> 63)

// GetFilterName returns the name of the encoding filter.
func (runLength *RunLengthEncoder) GetFilterName() string { return StreamEncodingFilterNameRunLength }

func (reader *offsetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("offsetReaderAt.ReadAt: negative offset")
	}
	return reader.source.ReadAt(p, off+reader.offset)
}

var exponentialNumberPattern = regexp.MustCompile("^[\\+-.]*([0-9.]+)[eE][\\+-.]*([0-9.]+)")

// String returns the state of the bool as "true" or "false".
func (b *PdfObjectBool) String() string {
	if *b {
		return "true"
	}
	return "false"
}

// PdfObject is an interface which all primitive PDF objects must implement.
type PdfObject interface {
	// String outputs a string representation of the primitive (for debugging).
	String() string

	// Write outputs the PDF primitive as written to file as expected by the standard.
	Write() []byte

	// Direct returns the direct object this object represents, dereferencing
	// references and indirect objects. Leaf and container objects return themselves.
	// It is equivalent to TraceToDirectObject(obj).
	Direct() PdfObject

	// Equals reports whether other has the same contents as this object. It performs
	// the same deep comparison as EqualObjects(obj, other).
	Equals(obj PdfObject) bool
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (ind *PdfIndirectObject) Equals(other PdfObject) bool {
	if ind == nil {
		return other == nil
	}
	return equalObjects(ind, other, 0, nil)
}

// Write outputs the object as it is to be written to file.
func (ind *PdfIndirectObject) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(ind.ObjectNumber, 10))
		buf.WriteString(" 0 R")
	})
}

// IsWhiteSpace checks if byte represents a white space character.
func IsWhiteSpace(ch byte) bool {
	if (ch == 0x00) || (ch == 0x09) || (ch == 0x0A) || (ch == 0x0C) || (ch == 0x0D) || (ch == 0x20) {
		return true
	}
	return false
}

// LinearizationViolationCode identifies a specific linearization check failure.
type LinearizationViolationCode int

// Update updates multiple keys and returns the dictionary back so can be used in a chained fashion.
func (dict *PdfObjectDictionary) Update(objmap map[string]PdfObject) *PdfObjectDictionary {
	dict.mu.Lock()
	defer dict.mu.Unlock()
	for sKey, obj := range objmap {
		dict.setWithLock(PdfObjectName(sKey), obj, false)
	}
	return dict
}

type offsetReaderAt struct {
	source io.ReaderAt
	offset int64
}

var xrefEntryRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s+([nf])\\s*$")

func (parser *PdfParser) trailerHasEncrypt(trailer *PdfObjectDictionary) bool {
	if trailer == nil {
		return false
	}
	switch v := trailer.Get("Encrypt").(type) {
	case *PdfObjectDictionary:
		return true
	case *PdfObjectReference:
		byReference, err := parser.LookupByReference(*v)
		if err != nil {
			return false
		}
		indirect, ok := byReference.(*PdfIndirectObject)
		if !ok {
			return false
		}
		_, isDict := indirect.PdfObject.(*PdfObjectDictionary)
		return isDict
	}
	return false
}

// GetName returns the *PdfObjectName represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetName(obj PdfObject) (name *PdfObjectName, ok bool) {
	name, ok = TraceToDirectObject(obj).(*PdfObjectName)
	return name, ok
}

var errHintShortRead = errors.New("hint stream: short read")

func (parser *PdfParser) parseHintStreamObject(offset int64) (*PdfObjectStream, error) {
	indirectObjectAt, err := parser.ParseIndirectObjectAt(offset)
	if err != nil {
		return nil, err
	}
	if stream, ok := indirectObjectAt.(*PdfObjectStream); ok {
		return stream, nil
	}
	if indirect, ok := indirectObjectAt.(*PdfIndirectObject); ok {
		if stream, isStream := indirect.PdfObject.(*PdfObjectStream); isStream {
			return stream, nil
		}
	}
	return nil, fmt.Errorf("hint object at offset %d is not a stream (%T)", offset, indirectObjectAt)
}

// MakeArray creates an PdfObjectArray from a list of PdfObjects.
func MakeArray(objects ...PdfObject) *PdfObjectArray { return &PdfObjectArray{elements: objects} }

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

func checkPageCount(count, treeCount int64) *LinearizationViolation {
	if count > 0 && count != treeCount {
		return &LinearizationViolation{Code: LinErrPageCountTreeMismatch, Severity: SeverityWarning, Message: fmt.Sprintf("/Pages /Count (%d) does not match the walked leaf page count (%d)", count, treeCount)}
	}
	return nil
}

// GetXrefType returns the type of the first xref object (table or stream).
func (parser *PdfParser) GetXrefType() *xrefType { return parser.xrefType }

// Equals reports whether other has the same contents as this object (deep comparison).
func (stream *PdfObjectStream) Equals(other PdfObject) bool {
	if stream == nil {
		return other == nil
	}
	return equalObjects(stream, other, 0, nil)
}
func (limits decodeLimits) remaining() int64 { return limits.clampToTotal(limits.streamLimit()) }

// EncodeBytes encodes a bytes array and return the encoded value based on the encoder parameters.
func (runLength *RunLengthEncoder) EncodeBytes(data []byte) ([]byte, error) {
	reader := bytes.NewReader(data)
	var encoded []byte
	var literal []byte
	b, err := reader.ReadByte()
	if err == io.EOF {
		return []byte{}, nil
	} else if err != nil {
		return nil, err
	}
	runLen := 1
	for {
		next, err2 := reader.ReadByte()
		if err2 == io.EOF {
			break
		} else if err2 != nil {
			return nil, err2
		}
		if next == b {
			if len(literal) > 0 {
				literal = literal[:len(literal)-1]
				if len(literal) > 0 {
					encoded = append(encoded, byte(len(literal)-1))
					encoded = append(encoded, literal...)
				}
				runLen = 1
				literal = []byte{}
			}
			runLen++
			if runLen >= 127 {
				encoded = append(encoded, byte(257-runLen), b)
				runLen = 0
			}
		} else {
			if runLen > 0 {
				if runLen == 1 {
					literal = []byte{b}
				} else {
					encoded = append(encoded, byte(257-runLen), b)
				}
				runLen = 0
			}
			literal = append(literal, next)
			if len(literal) >= 127 {
				encoded = append(encoded, byte(len(literal)-1))
				encoded = append(encoded, literal...)
				literal = []byte{}
			}
		}
		b = next
	}
	if len(literal) > 0 {
		encoded = append(encoded, byte(len(literal)-1))
		encoded = append(encoded, literal...)
	} else if runLen > 0 {
		encoded = append(encoded, byte(257-runLen), b)
	}
	encoded = append(encoded, 128)
	return encoded, nil
}

// Write outputs the object as it is to be written to file.
func (arr *PdfObjectArray) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString("[")
		for i, elem := range arr.Elements() {
			buf.Write(elem.Write())
			if i < (arr.Len() - 1) {
				buf.WriteString(" ")
			}
		}
		buf.WriteString("]")
	})
}

func (ascii85 *ASCII85Encoder) base256Tobase85(val uint32) [5]byte {
	data := [5]byte{0, 0, 0, 0, 0}
	rem := val
	for i := 0; i < 5; i++ {
		div := uint32(1)
		for j := 0; j < 4-i; j++ {
			div *= 85
		}
		digit := rem / div
		rem = rem % div
		data[i] = byte(digit)
	}
	return data
}

func checkLinearizedVersion(linearizationDict *PdfObjectDictionary) *LinearizationViolation {
	linearized := linearizationDict.Get("Linearized")
	if linearized == nil {
		return nil
	}
	if _, err := GetNumberAsFloat(linearized); err != nil {
		return &LinearizationViolation{Code: LinErrInvalidVersion, Severity: SeverityWarning, Message: fmt.Sprintf("/Linearized value is not a number: %s", linearized.String())}
	}
	return nil
}

// GenericHintTable is the common shape of the optional hint tables that all
// share the same four-field header: outline (F.4.7), article thread (F.4.8),
// named destination (F.4.9), interactive form (F.4.10), information dict
// (F.4.11), logical structure (F.4.12), page label (F.4.13), rendition
// (F.4.14) and embedded file stream (F.4.15). The v1 reader records the
// location header for each; per-entry decoding is producer-specific and not
// retained. If one of these tables ever needs fields beyond the common four,
// promote it back to a dedicated type.
type GenericHintTable struct {
	FirstObjNum uint32
	FirstOffset uint32
	NumObjects  uint32
	GroupLength uint32
}

func newLZWEncoderFromStream(streamObj *PdfObjectStream, decodeParams *PdfObjectDictionary) (*LZWEncoder, error) {
	lzwEncoder := NewLZWEncoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return lzwEncoder, nil
	}
	if decodeParams == nil {
		toDirectObject := TraceToDirectObject(pdfObjectDictionary.Get("DecodeParms"))
		if toDirectObject != nil {
			if dict, ok := toDirectObject.(*PdfObjectDictionary); ok {
				decodeParams = dict
			} else if arr, isArr := toDirectObject.(*PdfObjectArray); isArr {
				if arr.Len() == 1 {
					if first, hasDict := GetDict(arr.Get(0)); hasDict {
						decodeParams = first
					}
				}
			}
			if decodeParams == nil {
				common.Log.Error("DecodeParms not a dictionary %#v", toDirectObject)
				return nil, fmt.Errorf("invalid DecodeParms")
			}
		}
	}
	earlyChange := pdfObjectDictionary.Get("EarlyChange")
	if earlyChange != nil {
		num, ok := earlyChange.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: EarlyChange specified but not numeric (%T)", earlyChange)
			return nil, fmt.Errorf("invalid EarlyChange")
		}
		if *num != 0 && *num != 1 {
			return nil, fmt.Errorf("invalid EarlyChange value (not 0 or 1)")
		}
		lzwEncoder.EarlyChange = int(*num)
	} else {
		lzwEncoder.EarlyChange = 1
	}
	if decodeParams == nil {
		return lzwEncoder, nil
	}
	if intVal, ok := GetIntVal(decodeParams.Get("EarlyChange")); ok {
		if intVal == 0 || intVal == 1 {
			lzwEncoder.EarlyChange = intVal
		} else {
			common.Log.Debug("WARN: invalid EarlyChange value: %d", intVal)
		}
	}
	earlyChange = decodeParams.Get("Predictor")
	if earlyChange != nil {
		num, ok := earlyChange.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: Predictor specified but not numeric (%T)", earlyChange)
			return nil, fmt.Errorf("invalid Predictor")
		}
		lzwEncoder.Predictor = int(*num)
	}
	earlyChange = decodeParams.Get("BitsPerComponent")
	if earlyChange != nil {
		num, ok := earlyChange.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("ERROR: Invalid BitsPerComponent")
			return nil, fmt.Errorf("invalid BitsPerComponent")
		}
		lzwEncoder.BitsPerComponent = int(*num)
	}
	if lzwEncoder.Predictor > 1 {
		lzwEncoder.Columns = 1
		earlyChange = decodeParams.Get("Columns")
		if earlyChange != nil {
			num, ok := earlyChange.(*PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor column invalid")
			}
			lzwEncoder.Columns = int(*num)
		}
		lzwEncoder.Colors = 1
		earlyChange = decodeParams.Get("Colors")
		if earlyChange != nil {
			num, ok := earlyChange.(*PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor colors not an integer")
			}
			lzwEncoder.Colors = int(*num)
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", decodeParams.String())
	}
	return lzwEncoder, nil
}
func (dict *PdfObjectDictionary) Direct() PdfObject { return dict }

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

// GetFilterArray returns the names of the underlying encoding filters in an array that
// can be used as /Filter entry.
func (multi *MultiEncoder) GetFilterArray() *PdfObjectArray {
	objs := make([]PdfObject, len(multi.encoders))
	for i, encoder := range multi.encoders {
		objs[i] = MakeName(encoder.GetFilterName())
	}
	return MakeArray(objs...)
}

func readerHasData(r io.Reader) (bool, error) {
	var data [1]byte
	for {
		nr, err := r.Read(data[:])
		if nr > 0 {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
}

// DecodeStream decodes a JPX encoded stream and returns the result as a
// slice of bytes.
func (jpxEncoder *JPXEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	common.Log.Debug("Error: Attempting to use unsupported encoding %s", jpxEncoder.GetFilterName())
	return streamObj.Stream, ErrNoJPXDecode
}

// GetBool returns the *PdfObjectBool object that is represented by a PdfObject directly or indirectly
// within an indirect object. The bool flag indicates whether a match was found.
func GetBool(obj PdfObject) (boolObj *PdfObjectBool, ok bool) {
	boolObj, ok = TraceToDirectObject(obj).(*PdfObjectBool)
	return boolObj, ok
}
func (stream *PdfObjectStream) Direct() PdfObject { return stream }

// PageOffsetEntry is a per-page record in the Page Offset Hint Table
// (ISO 32000-2 Annex F.4.4 item 12 onward). All values are deltas added to the
// "Least…" baselines from the table header.
type PageOffsetEntry struct {
	ObjectCountDelta         uint32
	PageLengthDelta          uint32
	NumSharedObjects         uint32
	SharedObjectIdentifiers  []uint32
	SharedObjectNumerators   []uint32
	ContentStreamOffsetDelta uint32
	ContentStreamLengthDelta uint32
}

// PdfObjectStreams represents the primitive PDF object streams.
// 7.5.7 Object Streams (page 45).
type PdfObjectStreams struct {
	PdfObjectReference
	vec []PdfObject
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (null *PdfObjectNull) Equals(other PdfObject) bool {
	if null == nil {
		return other == nil
	}
	return equalObjects(null, other, 0, nil)
}

func (parser *PdfParser) newCursor(offset int64) *readerAtCursor {
	return newReaderAtCursor(parser.reader, parser.fileSize, offset)
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (dict *PdfObjectDictionary) Equals(other PdfObject) bool {
	if dict == nil {
		return other == nil
	}
	return equalObjects(dict, other, 0, nil)
}

func (parser *PdfParser) newRevisionParser(section *io.SectionReader) (*PdfParser, error) {
	if parser.compliance {
		compliancePdfParser, err := newCompliancePdfParser(section, section.Size(), parser.decodedTotal)
		if err != nil {
			return nil, err
		}
		compliancePdfParser.Opts = parser.Opts
		return compliancePdfParser, nil
	}
	var opts *ParserOpts
	if parser.Opts != nil {
		optsCopy := *parser.Opts
		opts = &optsCopy
	}
	return newPdfParserAt(section, section.Size(), opts, parser.decodedTotal)
}

func (parser *PdfParser) initParser() error {
	pdfVersion, minor, err := parser.parsePdfVersion()
	if err != nil {
		common.Log.Error("Unable to parse version: %v", err)
		return err
	}
	parser.version.Major = pdfVersion
	parser.version.Minor = minor
	if parser.trailer, err = parser.loadXrefs(); err != nil {
		common.Log.Debug("ERROR: Failed to load xref table! %s", err)
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Trailer: %s", parser.trailer)
	}
	linearizedDictionary, err := parser.parseLinearizedDictionary()
	if err != nil {
		return err
	}
	var ok bool
	if linearizedDictionary != nil {
		parser.linearized, err = parser.checkLinearizedInformation(linearizedDictionary)
		if err != nil {
			return err
		}
		ok = parser.linearized
		if parser.linearized && parser.linearizationObjNum > 0 {
			if byNumber, err2 := parser.LookupByNumber(parser.linearizationObjNum); err2 == nil {
				if dict, hasDict := GetDict(byNumber); !hasDict || dict.Get("Linearized") == nil {
					parser.linearized = false
				}
			}
		}
		if parser.linearized {
			parser.linearizationDict = linearizedDictionary
			parser.linearization = parser.buildLinearizationInfo(linearizedDictionary)
			if err2 := parser.parseHintStreamIfPresent(); err2 != nil {
				common.Log.Debug("WARN: hint stream decode failed: %v (continuing without hint tables)", err2)
			}
		}
	}
	if len(parser.currentXrefs().ObjectMap) == 0 {
		return fmt.Errorf("empty XREF table - Invalid")
	}
	parser.numRevisions = len(parser.xrefOffsets)
	if ok && parser.numRevisions != 0 {
		parser.numRevisions--
	}
	parser.revisions = make([]*PdfParser, parser.numRevisions)
	return nil
}
func (num *PdfObjectFloat) String() string { return fmt.Sprintf("%f", *num) }

// NewASCIIHexEncoder makes a new ASCII hex encoder.
func NewASCIIHexEncoder() *ASCIIHexEncoder { asciiHex := &ASCIIHexEncoder{}; return asciiHex }

// ReadAtLeast reads at least n bytes into p.
func (cursor *readerAtCursor) ReadAtLeast(p []byte, n int) (int, error) {
	remaining := n
	total := 0
	for remaining > 0 {
		nr, err := cursor.Read(p[total:])
		if err != nil {
			return total, err
		}
		total += nr
		remaining -= nr
	}
	return total, nil
}

// PdfObjectStream represents the primitive PDF Object stream.
type PdfObjectStream struct {
	PdfObjectReference
	*PdfObjectDictionary
	Stream []byte

	// A lazy stream keeps its data in TempFile and leaves Stream nil.
	Lazy     bool
	TempFile string
}

func (limits decodeLimits) charge(n int64) {
	if limits.total != nil && n > 0 {
		limits.total.Add(n)
	}
}

// Discard skips the next n bytes.
func (cursor *readerAtCursor) Discard(n int) (int, error) { return cursor.buffered.Discard(n) }

func encodePNGPredictor(data []byte, colors, bitsPerComponent, columns int) ([]byte, error) {
	rowBytes, bpp, _, err := normalizePredictorParams(colors, bitsPerComponent, columns)
	if err != nil {
		return nil, err
	}
	if rowBytes < 1 || len(data)%rowBytes != 0 {
		return nil, errors.New("invalid row length")
	}
	out := make([]byte, 0, len(data)/rowBytes*(rowBytes+1))
	for i := 0; i < len(data); i += rowBytes {
		row := data[i : i+rowBytes]
		out = append(out, predictorSub)
		for j := range row {
			if j < bpp {
				out = append(out, row[j])
			} else {
				out = append(out, row[j]-row[j-bpp])
			}
		}
	}
	return out, nil
}

func newJBIG2ImageFromImage(img imageutil.Image) *JBIG2Image {
	base := img.Base()
	return &JBIG2Image{Data: base.Data, Width: base.Width, Height: base.Height, HasPadding: true}
}

var errObjectNestingTooDeep = errors.New("arrays and dictionaries nested too deeply")

// DecodeStream decodes a JBIG2 encoded stream and returns the result as a slice of bytes.
func (jbig2 *JBIG2Encoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return jbig2.DecodeBytes(streamObj.Stream)
}

// DecodeBytes decodes a slice of DCT encoded bytes and returns the result.
func (dct *DCTEncoder) DecodeBytes(encoded []byte) (decoded []byte, err error) {
	config, err := jpeg.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		common.Log.Debug("Error decoding image: %s", err)
		return nil, err
	}
	components := int64(dct.ColorComponents)
	switch config.ColorModel {
	case color.CMYKModel:
		components = max(components, 4)
	case color.YCbCrModel, color.RGBAModel:
		components = max(components, 3)
	}
	size := int64(config.Width) * int64(config.Height) * components * int64(max(dct.BitsPerComponent, 8)) / 8
	if err2 := dct.limits.check(StreamEncodingFilterNameDCT, size); err2 != nil {
		return nil, err2
	}
	reader := bytes.NewReader(encoded)
	img, err := jpeg.Decode(reader)
	if err != nil {
		common.Log.Debug("Error decoding image: %s", err)
		return nil, err
	}
	rectangle := img.Bounds()
	bytes := AcquireBytes(rectangle.Dx() * rectangle.Dy() * dct.ColorComponents * dct.BitsPerComponent / 8)
	defer func() {
		if err != nil {
			ReleaseBytes(bytes)
		}
	}()
	pos := 0
	switch dct.ColorComponents {
	case 1:
		floats := []float64{dct.Decode[0], dct.Decode[1]}
		for i := rectangle.Min.Y; i < rectangle.Max.Y; i++ {
			for j := rectangle.Min.X; j < rectangle.Max.X; j++ {
				c := img.At(j, i)
				if dct.BitsPerComponent == 16 {
					gray16, ok := c.(color.Gray16)
					if !ok {
						return nil, errors.New("color type error")
					}
					v8 := scaleDecodeValue(uint(gray16.Y>>8), floats[0], floats[1])
					v16 := scaleDecodeValue(uint(gray16.Y), floats[0], floats[1])
					bytes[pos] = byte(v8)
					pos++
					bytes[pos] = byte(v16)
					pos++
				} else {
					gray, ok := c.(color.Gray)
					if !ok {
						return nil, errors.New("color type error")
					}
					bytes[pos] = byte(scaleDecodeValue(uint(gray.Y), floats[0], floats[1]))
					pos++
				}
			}
		}
	case 3:
		floats := []float64{dct.Decode[0], dct.Decode[1]}
		decG := []float64{dct.Decode[2], dct.Decode[3]}
		decB := []float64{dct.Decode[4], dct.Decode[5]}
		for i := rectangle.Min.Y; i < rectangle.Max.Y; i++ {
			for j := rectangle.Min.X; j < rectangle.Max.X; j++ {
				c := img.At(j, i)
				if dct.BitsPerComponent == 16 {
					rgbA64, ok := c.(color.RGBA64)
					if !ok {
						return nil, errors.New("color type error")
					}
					r8 := scaleDecodeValue(uint(rgbA64.R>>8), floats[0], floats[1])
					r16 := scaleDecodeValue(uint(rgbA64.R), floats[0], floats[1])
					g8 := scaleDecodeValue(uint(rgbA64.G>>8), decG[0], decG[1])
					g16 := scaleDecodeValue(uint(rgbA64.G), decG[0], decG[1])
					b8 := scaleDecodeValue(uint(rgbA64.B>>8), decB[0], decB[1])
					b16 := scaleDecodeValue(uint(rgbA64.B), decB[0], decB[1])
					bytes[pos] = byte(r8)
					pos++
					bytes[pos] = byte(r16)
					pos++
					bytes[pos] = byte(g8)
					pos++
					bytes[pos] = byte(g16)
					pos++
					bytes[pos] = byte(b8)
					pos++
					bytes[pos] = byte(b16)
					pos++
				} else {
					rgba, ok := c.(color.RGBA)
					if ok {
						f := scaleDecodeValue(uint(rgba.R), floats[0], floats[1])
						g := scaleDecodeValue(uint(rgba.G), decG[0], decG[1])
						b := scaleDecodeValue(uint(rgba.B), decB[0], decB[1])
						bytes[pos] = byte(f)
						pos++
						bytes[pos] = byte(g)
						pos++
						bytes[pos] = byte(b)
						pos++
					} else {
						yCbCr, isYCbCr := c.(color.YCbCr)
						if !isYCbCr {
							return nil, errors.New("color type error")
						}
						yv, cbv, crv, _ := yCbCr.RGBA()
						y := scaleDecodeValue(uint(yv>>8), floats[0], floats[1])
						cb := scaleDecodeValue(uint(cbv>>8), decG[0], decG[1])
						cr := scaleDecodeValue(uint(crv>>8), decB[0], decB[1])
						bytes[pos] = byte(y)
						pos++
						bytes[pos] = byte(cb)
						pos++
						bytes[pos] = byte(cr)
						pos++
					}
				}
			}
		}
	case 4:
		floats := []float64{dct.Decode[0], dct.Decode[1]}
		decM := []float64{dct.Decode[2], dct.Decode[3]}
		decY := []float64{dct.Decode[4], dct.Decode[5]}
		decK := []float64{dct.Decode[6], dct.Decode[7]}
		for i := rectangle.Min.Y; i < rectangle.Max.Y; i++ {
			for j := rectangle.Min.X; j < rectangle.Max.X; j++ {
				c := img.At(j, i)
				cmyk, ok := c.(color.CMYK)
				if !ok {
					return nil, errors.New("color type error")
				}
				f := 255 - scaleDecodeValue(uint(cmyk.C), floats[0], floats[1])
				m := 255 - scaleDecodeValue(uint(cmyk.M), decM[0], decM[1])
				y := 255 - scaleDecodeValue(uint(cmyk.Y), decY[0], decY[1])
				k := 255 - scaleDecodeValue(uint(cmyk.K), decK[0], decK[1])
				bytes[pos] = byte(f)
				pos++
				bytes[pos] = byte(m)
				pos++
				bytes[pos] = byte(y)
				pos++
				bytes[pos] = byte(k)
				pos++
			}
		}
	}
	dct.limits.charge(int64(len(bytes)))
	return bytes, nil
}

func (parser *PdfParser) walkPageTree() (pageCount int, declaredCount int64, firstPageIndex int64, err error) {
	firstPageIndex = -1
	trailer := parser.GetTrailer()
	if trailer == nil {
		return 0, 0, -1, errors.New("linearization: missing trailer")
	}
	obj, err := parser.Resolve(trailer.Get("Root"))
	if err != nil {
		return 0, 0, -1, err
	}
	dict, ok := GetDict(obj)
	if !ok {
		return 0, 0, -1, errors.New("linearization: /Root is not a dictionary")
	}
	pages := dict.Get("Pages")
	pagesObj, err := parser.Resolve(pages)
	if err != nil {
		return 0, 0, -1, err
	}
	pagesDict, ok := GetDict(pagesObj)
	if !ok {
		return 0, 0, -1, errors.New("linearization: /Pages is not a dictionary")
	}
	if count, err2 := GetNumberAsInt64(pagesDict.Get("Count")); err2 == nil {
		declaredCount = count
	}
	m := map[int]struct{}{}
	var walk func(node PdfObject) error
	walk = func(node PdfObject) error {
		objNum := int64(-1)
		if ref, isRef := node.(*PdfObjectReference); isRef {
			objNum = ref.ObjectNumber
			if _, ok2 := m[int(objNum)]; ok2 {
				return nil
			}
			m[int(objNum)] = struct{}{}
		}
		resolved, err2 := parser.Resolve(node)
		if err2 != nil {
			return err2
		}
		nodeDict, hasDict := GetDict(resolved)
		if !hasDict {
			return errors.New("linearization: page tree node is not a dictionary")
		}
		if typeName, hasTypeName := GetName(nodeDict.Get("Type")); hasTypeName && string(*typeName) == "Page" {
			pageCount++
			if firstPageIndex < 0 {
				firstPageIndex = objNum
			}
			return nil
		}
		kids, hasKids := GetArray(nodeDict.Get("Kids"))
		if !hasKids {
			kidsObj, err3 := parser.Resolve(nodeDict.Get("Kids"))
			if err3 != nil {
				return err3
			}
			if kids, hasKids = GetArray(kidsObj); !hasKids {
				return errors.New("linearization: page tree node missing /Kids")
			}
		}
		for _, elem := range kids.Elements() {
			if err3 := walk(elem); err3 != nil {
				return err3
			}
		}
		return nil
	}
	if err2 := walk(pages); err2 != nil {
		return pageCount, declaredCount, firstPageIndex, err2
	}
	return pageCount, declaredCount, firstPageIndex, nil
}

// GetObjectStreams returns the *PdfObjectStreams represented by the PdfObject. On type mismatch the found bool flag is
// false and a nil pointer is returned.
func GetObjectStreams(obj PdfObject) (streams *PdfObjectStreams, ok bool) {
	streams, ok = obj.(*PdfObjectStreams)
	return streams, ok
}

var referencePattern = regexp.MustCompile("^\\s*[-]*(\\d+)\\s+(\\d+)\\s+R")

type emptyReader struct{}

// EqualObjects returns true if `obj1` and `obj2` have the same contents.
// It performs a deep recursive comparison, dereferencing indirect objects and
// comparing stream data. Circular references are detected and handled safely.
//
// NOTE: It is a good idea to flatten obj1 and obj2 with FlattenObject before calling this function
// so that contents, rather than references, can be compared.
func EqualObjects(obj1, obj2 PdfObject) bool { return equalObjects(obj1, obj2, 0, nil) }

func (parser *PdfParser) scanEOFMarkers() ([]eofMarker, error) {
	data := []byte("%%EOF")
	const eofScanChunkSize = int64(64 * 1024)
	extra := int64(len(data)) + 2
	var markers []eofMarker
	buf := make([]byte, eofScanChunkSize+extra)
	for i := int64(0); i < parser.fileSize; i += eofScanChunkSize {
		readLen := eofScanChunkSize + extra
		if i+readLen > parser.fileSize {
			readLen = parser.fileSize - i
		}
		chunk := buf[:readLen]
		if at, err := parser.reader.ReadAt(chunk, i); err != nil && (err != io.EOF || int64(at) != readLen) {
			return nil, err
		}
		for j := int64(0); ; {
			rel := int64(bytes.Index(chunk[j:], data))
			if rel < 0 {
				break
			}
			found := j + rel
			j = found + int64(len(data))
			if found >= eofScanChunkSize {
				continue
			}
			end := j
			if end < readLen && chunk[end] == '\r' {
				end++
			}
			if end < readLen && chunk[end] == '\n' {
				end++
			}
			markers = append(markers, eofMarker{markerOffset: i + found, endOffset: i + end})
		}
	}
	return markers, nil
}

func newByteReader(r io.Reader) byteReader {
	if br, ok := r.(byteReader); ok {
		return br
	}
	return bufio.NewReader(r)
}

// GetDict returns the *PdfObjectDictionary represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetDict(obj PdfObject) (dict *PdfObjectDictionary, ok bool) {
	dict, ok = TraceToDirectObject(obj).(*PdfObjectDictionary)
	return dict, ok
}

const maxObjectNestingDepth = 500

// PdfObjectNull represents the primitive PDF null object.
type PdfObjectNull struct{}

func (crypt *PdfCrypt) checkAccessRights(password []byte) (bool, security.Permissions, error) {
	if crypt.isPubSec() {
		return false, 0, ErrCertificateRequired
	}
	stdHandler := crypt.securityHandler()
	data, perms, err := stdHandler.Authenticate(&crypt.stdEncrypt, password)
	if err != nil {
		return false, 0, err
	} else if len(data) == 0 {
		return false, 0, nil
	}
	return true, perms, nil
}
func (enc *LZWEncoder) setDecodeLimits(limits decodeLimits) { enc.limits = limits }
func (crypt *PdfCrypt) authenticate(password []byte) (bool, error) {
	if crypt.isPubSec() {
		return false, ErrCertificateRequired
	}
	crypt.authenticated = false
	stdHandler := crypt.securityHandler()
	data, _, err := stdHandler.Authenticate(&crypt.stdEncrypt, password)
	if err != nil {
		return false, err
	} else if len(data) == 0 {
		return false, nil
	}
	crypt.authenticated = true
	crypt.encryptionKey = data
	return true, nil
}

func (crypt *PdfCrypt) newEncryptDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName("Standard"))
	dict.Set("V", MakeInteger(int64(crypt.encrypt.V)))
	dict.Set("Length", MakeInteger(int64(crypt.encrypt.Length)))
	return dict
}

// EncodePageOffsetHintTable encodes the mandatory page offset hint table
// from header parameters and per-page entries. It writes to bw without
// aligning at the end — caller aligns before encoding the next table.
//
// The encoding matches the read-side decoder in linearization_hint.go:
// items 1..13 of the header followed by per-page entries that use the
// header-declared bit widths.
func EncodePageOffsetHintTable(bw *BitWriter, t *PageOffsetHintTable) error {
	if t == nil {
		return fmt.Errorf("hint encoder: nil page offset table")
	}
	bw.WriteBits(t.LeastObjectsInPage, 32)
	bw.WriteBits(t.FirstPageOffset, 32)
	bw.WriteBits(uint32(t.BitsPerObjectCountDelta), 16)
	bw.WriteBits(t.LeastPageLength, 32)
	bw.WriteBits(uint32(t.BitsPerPageLengthDelta), 16)
	bw.WriteBits(t.LeastContentStreamOffset, 32)
	bw.WriteBits(uint32(t.BitsPerContentStreamOffsetDelta), 16)
	bw.WriteBits(t.LeastContentStreamLength, 32)
	bw.WriteBits(uint32(t.BitsPerContentStreamLengthDelta), 16)
	bw.WriteBits(uint32(t.BitsPerNumSharedObjects), 16)
	bw.WriteBits(uint32(t.BitsPerSharedIdentifier), 16)
	bw.WriteBits(uint32(t.BitsPerSharedNumerator), 16)
	bw.WriteBits(uint32(t.SharedDenominator), 16)
	for i, entry := range t.PerPage {
		if uint32(len(entry.SharedObjectIdentifiers)) != entry.NumSharedObjects {
			return fmt.Errorf("hint encoder: page %d has %d sharedIDs but NumSharedObjects=%d", i, len(entry.SharedObjectIdentifiers), entry.NumSharedObjects)
		}
		if uint32(len(entry.SharedObjectNumerators)) != entry.NumSharedObjects {
			return fmt.Errorf("hint encoder: page %d has %d sharedNums but NumSharedObjects=%d", i, len(entry.SharedObjectNumerators), entry.NumSharedObjects)
		}
	}
	for _, entry := range t.PerPage {
		bw.WriteBits(entry.ObjectCountDelta, uint(t.BitsPerObjectCountDelta))
	}
	bw.AlignByte()
	for _, entry := range t.PerPage {
		bw.WriteBits(entry.PageLengthDelta, uint(t.BitsPerPageLengthDelta))
	}
	bw.AlignByte()
	for _, entry := range t.PerPage {
		bw.WriteBits(entry.NumSharedObjects, uint(t.BitsPerNumSharedObjects))
	}
	bw.AlignByte()
	for _, entry := range t.PerPage {
		for _, sharedObjectIdentifier := range entry.SharedObjectIdentifiers {
			bw.WriteBits(sharedObjectIdentifier, uint(t.BitsPerSharedIdentifier))
		}
	}
	bw.AlignByte()
	for _, entry := range t.PerPage {
		for _, sharedObjectNumerator := range entry.SharedObjectNumerators {
			bw.WriteBits(sharedObjectNumerator, uint(t.BitsPerSharedNumerator))
		}
	}
	bw.AlignByte()
	for _, entry := range t.PerPage {
		bw.WriteBits(entry.ContentStreamOffsetDelta, uint(t.BitsPerContentStreamOffsetDelta))
	}
	bw.AlignByte()
	for _, entry := range t.PerPage {
		bw.WriteBits(entry.ContentStreamLengthDelta, uint(t.BitsPerContentStreamLengthDelta))
	}
	bw.AlignByte()
	return nil
}

// Peek returns the next n bytes without advancing the cursor.
func (cursor *readerAtCursor) Peek(n int) ([]byte, error) { return cursor.buffered.Peek(n) }

// String returns a string describing `null`.
func (null *PdfObjectNull) String() string { return "null" }

var xrefSubsectionRegexp = regexp.MustCompile("(\\d+)\\s+(\\d+)\\s*$")

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

// GetArray returns the *PdfObjectArray represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetArray(obj PdfObject) (arr *PdfObjectArray, ok bool) {
	arr, ok = TraceToDirectObject(obj).(*PdfObjectArray)
	return arr, ok
}

// JBIG2CompressionType defines the enum compression type used by the JBIG2Encoder.
type JBIG2CompressionType int

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (ccitt *CCITTFaxEncoder) MakeDecodeParams() PdfObject {
	dict := MakeDict()
	dict.Set("K", MakeInteger(int64(ccitt.K)))
	dict.Set("Columns", MakeInteger(int64(ccitt.Columns)))
	if ccitt.BlackIs1 {
		dict.Set("BlackIs1", MakeBool(ccitt.BlackIs1))
	}
	if ccitt.EncodedByteAlign {
		dict.Set("EncodedByteAlign", MakeBool(ccitt.EncodedByteAlign))
	}
	if ccitt.EndOfLine && ccitt.K >= 0 {
		dict.Set("EndOfLine", MakeBool(ccitt.EndOfLine))
	}
	if ccitt.Rows != 0 && !ccitt.EndOfBlock {
		dict.Set("Rows", MakeInteger(int64(ccitt.Rows)))
	}
	if !ccitt.EndOfBlock {
		dict.Set("EndOfBlock", MakeBool(ccitt.EndOfBlock))
	}
	if ccitt.DamagedRowsBeforeError != 0 {
		dict.Set("DamagedRowsBeforeError", MakeInteger(int64(ccitt.DamagedRowsBeforeError)))
	}
	return dict
}

// Encrypt an object with specified key. For numbered objects,
// the key argument is not used and a new one is generated based
// on the object and generation number.
// Traverses through all the subobjects (recursive).
//
// Does not look up references..  That should be done prior to calling.
func (crypt *PdfCrypt) Encrypt(obj PdfObject, parentObjNum, parentGenNum int64) error {
	if crypt.isEncrypted(obj) {
		return nil
	}
	switch v := obj.(type) {
	case *PdfIndirectObject:
		crypt.encryptedObjects[v] = true
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encrypting indirect %d %d obj!", v.ObjectNumber, v.GenerationNumber)
		}
		objectNumber := v.ObjectNumber
		generationNumber := v.GenerationNumber
		err := crypt.Encrypt(v.PdfObject, objectNumber, generationNumber)
		if err != nil {
			return err
		}
		return nil
	case *PdfObjectStream:
		crypt.encryptedObjects[v] = true
		pdfObjectDictionary := v.PdfObjectDictionary
		if typeName, ok := pdfObjectDictionary.Get("Type").(*PdfObjectName); ok && *typeName == "XRef" {
			return nil
		}
		objectNumber := v.ObjectNumber
		generationNumber := v.GenerationNumber
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encrypting stream %d %d !", objectNumber, generationNumber)
		}
		filterName := stdCryptFilterName
		if crypt.encrypt.V >= 4 {
			filterName = crypt.streamFilter
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("this.streamFilter = %s", crypt.streamFilter)
			}
			if filter, ok := pdfObjectDictionary.Get("Filter").(*PdfObjectArray); ok {
				if name, hasName := GetName(filter.Get(0)); hasName {
					if *name == "Crypt" {
						filterName = "Identity"
						if decodeParms, isDict := pdfObjectDictionary.Get("DecodeParms").(*PdfObjectDictionary); isDict {
							if filterName2, isName := decodeParms.Get("Name").(*PdfObjectName); isName {
								if _, ok2 := crypt.cryptFilters[string(*filterName2)]; ok2 {
									if common.Log.IsLogLevel(common.LogLevelTrace) {
										common.Log.Trace("Using stream filter %s", *filterName2)
									}
									filterName = string(*filterName2)
								}
							}
						}
					}
				}
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("with %s filter", filterName)
			}
			if filterName == "Identity" {
				if v.Lazy {
					crypt.setLazyStreamData(v, nil)
				}
				return nil
			}
		}
		err := crypt.Encrypt(v.PdfObjectDictionary, objectNumber, generationNumber)
		if err != nil {
			return err
		}
		key, err := crypt.makeKey(filterName, uint32(objectNumber), uint32(generationNumber), crypt.encryptionKey)
		if err != nil {
			return err
		}
		if v.Lazy {
			return crypt.encryptLazyStream(v, filterName, key)
		}
		v.Stream, err = crypt.encryptBytes(v.Stream, filterName, key)
		if err != nil {
			return err
		}
		pdfObjectDictionary.Set("Length", MakeInteger(int64(len(v.Stream))))
		return nil
	case *PdfObjectString:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encrypting string!")
		}
		filterName := stdCryptFilterName
		if crypt.encrypt.V >= 4 {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("with %s filter", crypt.stringFilter)
			}
			if crypt.stringFilter == "Identity" {
				return nil
			}
			filterName = crypt.stringFilter
		}
		key, err := crypt.makeKey(filterName, uint32(parentObjNum), uint32(parentGenNum), crypt.encryptionKey)
		if err != nil {
			return err
		}
		str := v.Str()
		data := make([]byte, len(str))
		for i := 0; i < len(str); i++ {
			data[i] = str[i]
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Encrypt string: %s : % x", data, data)
		}
		data, err = crypt.encryptBytes(data, filterName, key)
		if err != nil {
			return err
		}
		v.val = string(data)
		return nil
	case *PdfObjectArray:
		for _, elem := range v.Elements() {
			err := crypt.Encrypt(elem, parentObjNum, parentGenNum)
			if err != nil {
				return err
			}
		}
		return nil
	case *PdfObjectDictionary:
		ok := false
		if typeVal := v.Get("Type"); typeVal != nil {
			name, isName := typeVal.(*PdfObjectName)
			if isName && *name == "Sig" {
				ok = true
			}
		}
		for _, key := range v.Keys() {
			child := v.Get(key)
			if ok && string(key) == "Contents" {
				continue
			}
			if string(key) != "Parent" && string(key) != "Prev" && string(key) != "Last" {
				err := crypt.Encrypt(child, parentObjNum, parentGenNum)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
	return nil
}

// Write outputs the object as it is to be written to file.
func (b *PdfObjectBool) Write() []byte {
	if *b {
		return []byte("true")
	}
	return []byte("false")
}

type cryptFilters map[string]crypt.Filter

func logXrefTable(table XrefTable) {
	common.Log.Debug("=X=X=X=")
	common.Log.Debug("Xref table:")
	idx := 0
	for _, xrefObj := range table.ObjectMap {
		common.Log.Debug("i+1: %d (obj num: %d gen: %d) -> %d", idx+1, xrefObj.ObjectNumber, xrefObj.Generation, xrefObj.Offset)
		idx++
	}
}

// DecodeReader returns a reader that decodes the chain of sub-encoders in
// forward (decode) order: r → encoders[0].DecodeReader → ... → output.
//
// The first sub-encoder that does NOT implement StreamingDecoder (typically
// DCT, JPX, CCITTFax, JBIG2) terminates the streaming portion: the upstream
// reader is drained into memory and the remaining encoders run via their
// DecodeBytes. The end-to-end output is therefore identical to DecodeBytes,
// but only the portion of the chain up to (and not including) the first
// non-streaming encoder runs without full materialization.
func (multi *MultiEncoder) DecodeReader(r io.Reader) io.Reader {
	cur := r
	for i, encoder := range multi.encoders {
		streamingDecoder, ok := encoder.(StreamingDecoder)
		if !ok {
			return &multiFallbackReader{src: cur, fallbacks: multi.encoders[i:]}
		}
		cur = streamingDecoder.DecodeReader(cur)
	}
	return cur
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (arr *PdfObjectArray) Equals(other PdfObject) bool {
	if arr == nil {
		return other == nil
	}
	return equalObjects(arr, other, 0, nil)
}

// Write outputs the object as it is to be written to file.
func (str *PdfObjectString) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		if str.isHex {
			val := str.val
			if !str.hexEncoded {
				val = hex.EncodeToString(str.Bytes())
			}
			buf.Grow(len(val) + 2)
			buf.WriteString("<")
			buf.WriteString(val)
			buf.WriteString(">")
			return
		}
		buf.Grow(len(str.val) + len(str.val)/5 + 2)
		buf.WriteString("(")
		for i := 0; i < len(str.val); i++ {
			b := str.val[i]
			if data, ok := stringEscapes[b]; ok {
				buf.Write(data)
			} else {
				buf.WriteByte(b)
			}
		}
		buf.WriteString(")")
	})
}

const (
	SeverityError Severity = iota
	SeverityWarning
)

// ValidateLinearization checks whether r contains a correctly linearized PDF as
// defined in ISO 32000-2 Annex F. It returns a slice of violations; an empty
// slice means the PDF passes all checks. If the PDF is not linearized it returns
// exactly [LinErrNotLinearized].
//
// The returned error is non-nil only when the file cannot be parsed at all and
// validation could not run; spec violations are reported through the slice.
func ValidateLinearization(r io.ReaderAt, size int64) ([]LinearizationViolation, error) {
	parserAt, err := NewParserAt(r, size)
	if err != nil {
		return nil, err
	}
	return parserAt.validateLinearization()
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

// ASCII85Encoder implements ASCII85 encoder/decoder.
type ASCII85Encoder struct{ limits decodeLimits }

// RegisterCustomStreamEncoder register a custom encoder handler for certain filter.
func RegisterCustomStreamEncoder(filterName string, customStreamEncoder StreamEncoder) {
	customStreamEncoders.Store(filterName, customStreamEncoder)
}

func readPageOffsetHintTable(br *bitReader, numPages int) (*PageOffsetHintTable, error) {
	pageHints := &PageOffsetHintTable{}
	sts := []struct {
		dest any
		bits uint
	}{{&pageHints.LeastObjectsInPage, 32}, {&pageHints.FirstPageOffset, 32}, {&pageHints.BitsPerObjectCountDelta, 16}, {&pageHints.LeastPageLength, 32}, {&pageHints.BitsPerPageLengthDelta, 16}, {&pageHints.LeastContentStreamOffset, 32}, {&pageHints.BitsPerContentStreamOffsetDelta, 16}, {&pageHints.LeastContentStreamLength, 32}, {&pageHints.BitsPerContentStreamLengthDelta, 16}, {&pageHints.BitsPerNumSharedObjects, 16}, {&pageHints.BitsPerSharedIdentifier, 16}, {&pageHints.BitsPerSharedNumerator, 16}, {&pageHints.SharedDenominator, 16}}
	for _, st := range sts {
		bits, err := br.readBits(st.bits)
		if err != nil {
			return nil, err
		}
		switch v := st.dest.(type) {
		case *uint32:
			*v = bits
		case *uint16:
			*v = uint16(bits)
		}
	}
	if numPages <= 0 || numPages > maxLinearizationPages {
		return pageHints, nil
	}
	pageHints.PerPage = make([]PageOffsetEntry, numPages)
	readField := func(fieldName string, nbits uint, set func(page int, v uint32)) error {
		for i := 0; i < numPages; i++ {
			bits, err := br.readBits(nbits)
			if err != nil {
				return fmt.Errorf("page %d %s: %w", i, fieldName, err)
			}
			set(i, bits)
		}
		br.skipToNextByte()
		return nil
	}
	if err := readField("objCountDelta", uint(pageHints.BitsPerObjectCountDelta), func(page int, v uint32) { pageHints.PerPage[page].ObjectCountDelta = v }); err != nil {
		return pageHints, err
	}
	if err := readField("pageLengthDelta", uint(pageHints.BitsPerPageLengthDelta), func(page int, v uint32) { pageHints.PerPage[page].PageLengthDelta = v }); err != nil {
		return pageHints, err
	}
	if err := readField("numSharedObjects", uint(pageHints.BitsPerNumSharedObjects), func(page int, v uint32) { pageHints.PerPage[page].NumSharedObjects = v }); err != nil {
		return pageHints, err
	}
	for i := 0; i < numPages; i++ {
		if pageHints.PerPage[i].NumSharedObjects > maxSharedObjectsPerPage {
			return nil, fmt.Errorf("hint stream: page %d NumSharedObjects %d exceeds limit %d", i, pageHints.PerPage[i].NumSharedObjects, maxSharedObjectsPerPage)
		}
	}
	for i := 0; i < numPages; i++ {
		numSharedObjects := pageHints.PerPage[i].NumSharedObjects
		if numSharedObjects == 0 {
			continue
		}
		pageHints.PerPage[i].SharedObjectIdentifiers = make([]uint32, numSharedObjects)
		for j := uint32(0); j < numSharedObjects; j++ {
			bits, err := br.readBits(uint(pageHints.BitsPerSharedIdentifier))
			if err != nil {
				return pageHints, fmt.Errorf("page %d sharedID[%d]: %w", i, j, err)
			}
			pageHints.PerPage[i].SharedObjectIdentifiers[j] = bits
		}
	}
	br.skipToNextByte()
	for i := 0; i < numPages; i++ {
		numSharedObjects := pageHints.PerPage[i].NumSharedObjects
		if numSharedObjects == 0 {
			continue
		}
		pageHints.PerPage[i].SharedObjectNumerators = make([]uint32, numSharedObjects)
		for j := uint32(0); j < numSharedObjects; j++ {
			bits, err := br.readBits(uint(pageHints.BitsPerSharedNumerator))
			if err != nil {
				return pageHints, fmt.Errorf("page %d sharedNum[%d]: %w", i, j, err)
			}
			pageHints.PerPage[i].SharedObjectNumerators[j] = bits
		}
	}
	br.skipToNextByte()
	if err := readField("csOffsetDelta", uint(pageHints.BitsPerContentStreamOffsetDelta), func(page int, v uint32) { pageHints.PerPage[page].ContentStreamOffsetDelta = v }); err != nil {
		return pageHints, err
	}
	if err := readField("csLengthDelta", uint(pageHints.BitsPerContentStreamLengthDelta), func(page int, v uint32) { pageHints.PerPage[page].ContentStreamLengthDelta = v }); err != nil {
		return pageHints, err
	}
	return pageHints, nil
}

// NewCCITTFaxEncoder makes a new CCITTFax encoder.
func NewCCITTFaxEncoder() *CCITTFaxEncoder { return &CCITTFaxEncoder{Columns: 1728, EndOfBlock: true} }

// AddEncoder adds the passed in encoder to the underlying encoder slice.
func (multi *MultiEncoder) AddEncoder(encoder StreamEncoder) {
	multi.encoders = append(multi.encoders, encoder)
}

func (crypt *PdfCrypt) unwrapRecipient(cert *x509.Certificate, privKey crypto.PrivateKey) ([]byte, [4]byte, int, error) {
	if !crypt.isPubSec() {
		return nil, [4]byte{}, -1, ErrPasswordRequired
	}
	return security.PubSecUnwrap(crypt.pubSec.Recipients, cert, privKey)
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
func (ascii85 *ASCII85Encoder) setDecodeLimits(limits decodeLimits) { ascii85.limits = limits }

// Write outputs the object as it is to be written to file.
func (num *PdfObjectInteger) Write() []byte { return []byte(strconv.FormatInt(int64(*num), 10)) }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (multi *MultiEncoder) MakeDecodeParams() PdfObject {
	if len(multi.encoders) == 0 {
		return nil
	}
	if len(multi.encoders) == 1 {
		return multi.encoders[0].MakeDecodeParams()
	}
	array := MakeArray()
	ok := true
	for _, encoder := range multi.encoders {
		decodeParams := encoder.MakeDecodeParams()
		if decodeParams == nil {
			array.Append(MakeNull())
		} else {
			ok = false
			array.Append(decodeParams)
		}
	}
	if ok {
		return nil
	}
	return array
}

// UpdateParams updates the parameter values of the encoder.
func (ccitt *CCITTFaxEncoder) UpdateParams(params *PdfObjectDictionary) {
	if kN, err := GetNumberAsInt64(params.Get("K")); err == nil {
		ccitt.K = int(kN)
	}
	if columns, err := GetNumberAsInt64(params.Get("Columns")); err == nil {
		ccitt.Columns = int(columns)
	} else if columns, err = GetNumberAsInt64(params.Get("Width")); err == nil {
		ccitt.Columns = int(columns)
	}
	if blackIs1, err := GetNumberAsInt64(params.Get("BlackIs1")); err == nil {
		ccitt.BlackIs1 = blackIs1 > 0
	} else {
		if blackIs12, ok := GetBoolVal(params.Get("BlackIs1")); ok {
			ccitt.BlackIs1 = blackIs12
		} else {
			if decode, hasDecode := GetArray(params.Get("Decode")); hasDecode {
				nums, err2 := decode.ToIntegerArray()
				if err2 == nil {
					ccitt.BlackIs1 = nums[0] == 1 && nums[1] == 0
				}
			}
		}
	}
	if encodedByteAlign, err := GetNumberAsInt64(params.Get("EncodedByteAlign")); err == nil {
		ccitt.EncodedByteAlign = encodedByteAlign > 0
	} else {
		if byteAlign, ok := GetBoolVal(params.Get("EncodedByteAlign")); ok {
			ccitt.EncodedByteAlign = byteAlign
		}
	}
	if endOfLine, err := GetNumberAsInt64(params.Get("EndOfLine")); err == nil {
		ccitt.EndOfLine = endOfLine > 0
	} else {
		if endOfLine2, ok := GetBoolVal(params.Get("EndOfLine")); ok {
			ccitt.EndOfLine = endOfLine2
		}
	}
	if rows, err := GetNumberAsInt64(params.Get("Rows")); err == nil {
		ccitt.Rows = int(rows)
	} else if rows, err = GetNumberAsInt64(params.Get("Height")); err == nil {
		ccitt.Rows = int(rows)
	}
	if endOfBlock, err := GetNumberAsInt64(params.Get("EndOfBlock")); err == nil {
		ccitt.EndOfBlock = endOfBlock > 0
	} else {
		if endOfBlock2, ok := GetBoolVal(params.Get("EndOfBlock")); ok {
			ccitt.EndOfBlock = endOfBlock2
		}
	}
	if damagedRowsBeforeError, err := GetNumberAsInt64(params.Get("DamagedRowsBeforeError")); err != nil {
		ccitt.DamagedRowsBeforeError = int(damagedRowsBeforeError)
	}
}

// MakeHexStringFromHexNumber creates an PdfObjectString from a numeric value intended for output as a hexadecimal string.
// The value is formatted as a hex string with padding to match the specified byte width (byteWidth * 2 digits).
func MakeHexStringFromHexNumber(val uint32, byteWidth int) *PdfObjectString {
	hex := fmt.Sprintf("%0*x", byteWidth*2, val)
	str := PdfObjectString{val: hex, isHex: true, hexEncoded: true}
	return &str
}

func (crypt *PdfCrypt) takeLazyStreamData(stream *PdfObjectStream) ([]byte, bool) {
	data, ok := crypt.lazyStreamData[stream]
	if ok {
		delete(crypt.lazyStreamData, stream)
	}
	return data, ok
}

func checkFirstPageEndOffset(endOfFirstPage, firstXrefOffset, mainXrefOffset, fileSize int64) *LinearizationViolation {
	if endOfFirstPage <= firstXrefOffset || endOfFirstPage >= fileSize || (mainXrefOffset > 0 && endOfFirstPage >= mainXrefOffset) {
		return &LinearizationViolation{Code: LinErrFirstPageEndOffset, Severity: SeverityError, Message: fmt.Sprintf("/E (%d) is inconsistent with the file layout (lin dict ends %d, /T %d, size %d)", endOfFirstPage, firstXrefOffset, mainXrefOffset, fileSize)}
	}
	return nil
}

func hexDigitValue(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c <= 'F':
		return c - 'A' + 10
	default:
		return c - 'a' + 10
	}
}

type ascii85Reader struct {
	src      byteReader
	err      error
	group    [4]byte
	groupLen int
	groupPos int
}

// ValidateLinearization runs the ISO 32000-2 Annex F checks against this
// already-parsed PDF. Callers that already hold a parser (e.g. a PdfReader)
// should use this rather than the package-level ValidateLinearization, which
// re-parses from scratch. Returns [LinErrNotLinearized] if the PDF is not
// linearized.
func (parser *PdfParser) ValidateLinearization() ([]LinearizationViolation, error) {
	return parser.validateLinearization()
}

func collectReferencedObjNums(dict *PdfObjectDictionary, keys ...string) []int {
	m := map[string]struct{}{}
	for _, key := range keys {
		m[key] = struct{}{}
	}
	seen := map[int]struct{}{}
	var nums []int
	var visit func(obj PdfObject)
	visit = func(obj PdfObject) {
		switch v := obj.(type) {
		case *PdfObjectReference:
			objNum := int(v.ObjectNumber)
			if _, ok := seen[objNum]; !ok {
				seen[objNum] = struct{}{}
				nums = append(nums, objNum)
			}
		case *PdfObjectArray:
			for _, elem := range v.Elements() {
				visit(elem)
			}
		case *PdfObjectDictionary:
			for _, key := range v.Keys() {
				visit(v.Get(key))
			}
		}
	}
	for _, key := range dict.Keys() {
		if _, ok := m[string(key)]; ok {
			continue
		}
		visit(dict.Get(key))
	}
	return nums
}

type readerAtCursor struct {
	source        io.ReaderAt
	size          int64
	sectionOffset int64
	section       *io.SectionReader
	buffered      *bufio.Reader
}

// GetNameVal returns the string value represented by the PdfObject directly or indirectly if
// contained within an indirect object. On type mismatch the found bool flag returned is false and
// an empty string is returned.
func GetNameVal(obj PdfObject) (val string, ok bool) {
	name, ok := TraceToDirectObject(obj).(*PdfObjectName)
	if ok {
		return string(*name), true
	}
	return
}

// FlattenObject returns the contents of `obj`. In other words, `obj` with indirect objects replaced
// by their values.
// The replacements are made recursively to a depth of traceMaxDepth.
// NOTE: Dicts are sorted to make objects with same contents have the same PDF object strings.
func FlattenObject(obj PdfObject) PdfObject { return resolveReferencesRecursive(obj, 0) }

// AlignByte pads any partial byte out with zero bits so subsequent writes
// start on a byte boundary.
func (bw *BitWriter) AlignByte() {
	if bw.nbits == 0 {
		return
	}
	bw.WriteBits(0, 8-bw.nbits)
}

func (crypt *PdfCrypt) authenticateCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, error) {
	data, perms, _, err := crypt.unwrapRecipient(cert, key)
	if err != nil {
		return false, err
	}
	pubSec := crypt.pubSec
	crypt.encryptionKey = security.PubSecFileKey(data, pubSec.Recipients, crypt.encrypt.V, crypt.pubSecKeyBits(), pubSec.EncryptMetadata)
	crypt.permissions = security.PubSecPermissions(perms)
	crypt.authenticated = true
	return true, nil
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (runLength *RunLengthEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(runLength.GetFilterName()))
	return dict
}

// ReadBytes reads until the first occurrence of delim.
func (cursor *readerAtCursor) ReadBytes(delim byte) ([]byte, error) {
	return cursor.buffered.ReadBytes(delim)
}

var bufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func (dict *PdfObjectDictionary) setWithLock(key PdfObjectName, val PdfObject, lock bool) {
	if lock {
		dict.mu.Lock()
		defer dict.mu.Unlock()
	}
	_, ok := dict.dict[key]
	if !ok {
		dict.keys = append(dict.keys, key)
	}
	dict.dict[key] = val
}

// RGBToGrayscale converts RGB color values to grayscale using the standard luminance formula.
//
// Parameters r, g, b are expected to be integers in the range [0, 255].
//
// The coefficient used in the formula to convert RGB values to grayscale.
// This formula is based on the luminance perception of the human eye,
// which is more sensitive to green light and less sensitive to blue light.
// The full formula is:  [ gray = 0.299 * R + 0.587 * G + 0.114 * B ]
//
// This implementation uses integer arithmetic for better performance by using
// scaled coefficients: 299, 587, 114 (scaled by 1000).
func RGBToGrayscale(r, g, b int) int { gray := 299*r + 587*g + 114*b; return gray / 1000 }

func (parser *PdfParser) parseLinearizedDictionary() (*PdfObjectDictionary, error) {
	fileSize := parser.fileSize
	cursor := parser.newCursor(0)
	var pos int64
	var window int64 = 2048
	for pos < fileSize-4 {
		if fileSize <= (window + pos) {
			window = fileSize - pos
		}
		cursor.SetFileOffset(pos)
		data := make([]byte, window)
		_, err := cursor.Read(data)
		if err != nil {
			return nil, err
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking for first indirect object: \"%s\"", string(data))
		}
		allStringIndex := objHeaderRegexp.FindAllStringIndex(string(data), -1)
		if allStringIndex != nil {
			nums := allStringIndex[0]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Ind: % d", allStringIndex)
			}
			cursor.SetFileOffset(pos + int64(nums[0]))
			indirectObject, err2 := parser.parseIndirectObject(cursor, map[int]struct{}{})
			if err2 != nil {
				return nil, nil
			}
			if indirect, ok := GetIndirect(indirectObject); ok {
				if dict, hasDict := GetDict(indirect.PdfObject); hasDict {
					if linearized := dict.Get("Linearized"); linearized != nil {
						parser.firstXrefOffset = cursor.GetFileOffset()
						parser.linearizationObjNum = int(indirect.ObjectNumber)
						return dict, nil
					}
					return nil, nil
				}
			}
			return nil, nil
		}
		pos += window - 4
	}
	return nil, errors.New("the first object not found")
}

// ReadAt exposes the underlying io.ReaderAt so callers that don't need cursor
// state (e.g. ReadBytesAt) can read directly. Does not advance the cursor.
func (cursor *readerAtCursor) ReadAt(p []byte, off int64) (int, error) {
	return cursor.source.ReadAt(p, off)
}

// DecodeStream decodes the stream data and returns the decoded data.
// An error is returned upon failure.
func DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Decode stream")
	}
	encoderFromStream, err := NewEncoderFromStream(streamObj)
	if err != nil {
		common.Log.Debug("ERROR: Stream decoding failed: %v", err)
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encoder: %#v\n", encoderFromStream)
	}
	stream, err := encoderFromStream.DecodeStream(streamObj)
	if err != nil {
		common.Log.Debug("ERROR: Stream decoding failed: %v", err)
		return nil, err
	}
	return stream, nil
}

// MakeStringFromBytes creates an PdfObjectString from a byte array.
// This is more natural than MakeString as `data` is usually not utf-8 encoded.
func MakeStringFromBytes(data []byte) *PdfObjectString { return MakeString(string(data)) }

// Equals reports whether other has the same contents as this object (deep comparison).
func (streams *PdfObjectStreams) Equals(other PdfObject) bool {
	if streams == nil {
		return other == nil
	}
	return equalObjects(streams, other, 0, nil)
}

// HasInvalidHexRunes implements core.ParserMetadata interface.
func (meta ParserMetadata) HasInvalidHexRunes() bool { return meta.invalidHexRunes }

// DecodeStream decodes the stream containing CCITTFax encoded image data.
func (ccitt *CCITTFaxEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return ccitt.DecodeBytes(streamObj.Stream)
}

// PdfObjectBool represents the primitive PDF boolean object.
type PdfObjectBool bool

// RawEncoder implements Raw encoder/decoder (no encoding, pass through)
type RawEncoder struct{}

// Resolve resolves a PdfObject to direct object, looking up and resolving references as needed (unlike TraceToDirect).
func (parser *PdfParser) Resolve(obj PdfObject) (PdfObject, error) {
	return parser.resolveInternal(obj, map[int]struct{}{})
}

func (parser *PdfParser) checkLinearizedInformation(dict *PdfObjectDictionary) (bool, error) {
	var err error
	parser.linearizedFileLength, err = GetNumberAsInt64(dict.Get("L"))
	if err != nil {
		return false, err
	}
	cursor := parser.newCursor(0)
	err = parser.seekToEOFMarker(cursor, parser.linearizedFileLength)
	switch err {
	case nil:
		return true, nil
	case errEOFNotFound:
		return false, nil
	default:
		return false, err
	}
}

func (r *ascii85Reader) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if r.groupPos < r.groupLen {
			copied := copy(p[total:], r.group[r.groupPos:r.groupLen])
			total += copied
			r.groupPos += copied
			continue
		}
		if r.err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, r.err
		}
		r.err = r.fillGroup()
	}
	return total, nil
}

// SetImage sets the image base for given flate encoder.
func (flate *FlateEncoder) SetImage(img *imageutil.ImageBase) { flate.image = img }

// Decrypt attempts to decrypt the PDF file with a specified password.  Also tries to
// decrypt with an empty password.  Returns true if successful, false otherwise.
// An error is returned when there is a problem with decrypting.
//
// Decrypt must not run concurrently with object lookups: on success it
// clears the object cache, and a lookup in flight during authentication
// could re-populate it with a still-encrypted object.
//
// With ParserOpts.AutoRepairXrefTable, the deferred xref table health check
// runs on the first successful authentication. If the table then cannot be
// rebuilt, the password is reported as accepted and an error is returned.
func (parser *PdfParser) Decrypt(password []byte) (bool, error) {
	if parser.crypter == nil {
		return false, errors.New("check encryption first")
	}
	ok, err := parser.crypter.authenticate(password)
	if err != nil {
		return false, err
	}
	if !ok {
		ok, err = parser.crypter.authenticate([]byte(""))
	}
	if ok {
		if err2 := parser.afterAuthentication(); err2 != nil {
			return true, err2
		}
	}
	return ok, err
}

func (parser *PdfParser) repairSeekXrefMarker(cursor *readerAtCursor) error {
	fileSize := parser.fileSize
	re := regexp.MustCompile("\\sxref\\s*")
	var pos int64
	var window int64 = 1000
	for pos < fileSize {
		if fileSize <= (window + pos) {
			window = fileSize - pos
		}
		cursor.SetFileOffset(fileSize - pos - window)
		data := make([]byte, window)
		cursor.Read(data)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking for xref : \"%s\"", string(data))
		}
		allStringIndex := re.FindAllStringIndex(string(data), -1)
		if allStringIndex != nil {
			nums := allStringIndex[len(allStringIndex)-1]
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Ind: % d", allStringIndex)
			}
			cursor.SetFileOffset(fileSize - pos - window + int64(nums[0]))
			for {
				peeked, err := cursor.Peek(1)
				if err != nil {
					return err
				}
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("B: %d %c", peeked[0], peeked[0])
				}
				if !IsWhiteSpace(peeked[0]) {
					break
				}
				cursor.Discard(1)
			}
			return nil
		}
		common.Log.Debug("Warning: EOF marker not found! - continue seeking")
		pos += window
	}
	common.Log.Debug("Error: Xref table marker was not found.")
	return errors.New("xref not found ")
}

// PdfObjectInteger represents the primitive PDF integer numerical object.
type PdfObjectInteger int64

func (crypt *PdfCrypt) encryptLazyStream(stream *PdfObjectStream, filterName string, key []byte) error {
	if stream.TempFile == "" {
		return fmt.Errorf("lazy stream %d: already written (lazy streams are consumed by the first write)", stream.ObjectNumber)
	}
	file, err := os.ReadFile(filepath.Clean(stream.TempFile))
	if err != nil {
		return fmt.Errorf("lazy stream %d: reading temp file: %w", stream.ObjectNumber, err)
	}
	data, err := crypt.encryptBytes(file, filterName, key)
	if err != nil {
		return err
	}
	crypt.setLazyStreamData(stream, data)
	stream.Set("Length", MakeInteger(int64(len(data))))
	return nil
}

// SizeOfReaderAt auto-detects the byte length of an io.ReaderAt source by
// type-asserting for the common ways a source carries its size:
//   - interface{ Size() int64 } — *bytes.Reader, *io.SectionReader
//   - interface{ Stat() (os.FileInfo, error) } — *os.File
//   - io.Seeker — fallback for other seekable sources
//
// Stat is checked before Seek because *os.File satisfies both, and Stat is
// side-effect-free whereas Seek mutates the file position.
//
// Returns an error if the source supports none of these. Callers with such
// sources should use the explicit-size constructor (e.g. model.NewPdfReaderAt)
// and pass the size from their own knowledge.
func SizeOfReaderAt(r io.ReaderAt) (int64, error) {
	if r == nil {
		return 0, errors.New("core.SizeOfReaderAt: nil source")
	}
	if iface, ok := r.(interface{ Size() int64 }); ok {
		return iface.Size(), nil
	}
	if iface, ok := r.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, err := iface.Stat()
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	if seeker, ok := r.(io.Seeker); ok {
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, err
		}
		if _, err2 := seeker.Seek(0, io.SeekStart); err2 != nil {
			return 0, err2
		}
		return size, nil
	}
	return 0, errors.New("core.SizeOfReaderAt: source does not support Size(), Stat(), or Seek(); use the explicit-size constructor")
}

func (limits decodeLimits) clampToTotal(n int64) int64 {
	if limits.maxTotal > 0 && limits.total != nil {
		if remaining := limits.maxTotal - limits.total.Load(); remaining < n {
			n = remaining
		}
	}
	return max(n, 0)
}

// EncodeBytes encodes the passed in slice of bytes by passing it through the
// EncodeBytes method of the underlying encoders.
func (multi *MultiEncoder) EncodeBytes(data []byte) ([]byte, error) {
	encoded := data
	var err error
	for _, encoder := range slices.Backward(multi.encoders) {

		encoded, err = encoder.EncodeBytes(encoded)
		if err != nil {
			return nil, err
		}
	}
	return encoded, nil
}

// LookupByNumber looks up a PdfObject by object number. Returns an error on failure.
// Safe for concurrent use; cache hits are lock-free, cache misses are deduped
// via singleflight (one goroutine parses, others wait and share the result).
func (parser *PdfParser) LookupByNumber(objNumber int) (PdfObject, error) {
	byNumberWrapperInternal, _, err := parser.lookupByNumberWrapperInternal(objNumber, true, map[int]struct{}{})
	return byNumberWrapperInternal, err
}

// GetNumberAsFloat returns the contents of `obj` as a float if it is an integer or float, or an
// error if it isn't.
func GetNumberAsFloat(obj PdfObject) (float64, error) {
	switch v := obj.(type) {
	case *PdfObjectFloat:
		return float64(*v), nil
	case *PdfObjectInteger:
		return float64(*v), nil
	case *PdfObjectReference:
		toDirectObject := TraceToDirectObject(obj)
		return GetNumberAsFloat(toDirectObject)
	case *PdfIndirectObject:
		return GetNumberAsFloat(v.PdfObject)
	}
	return 0, ErrNotANumber
}

const (
	maxLinearizationPages   = 1 << 20
	maxSharedObjectsTotal   = 1 << 20
	maxSharedObjectsPerPage = 1 << 16
)

// EncodeBytes encodes the image data using either Group3 or Group4 CCITT facsimile (fax) encoding.
// `data` is expected to be 1 color component, 1 bit per component. It is also valid to provide 8 BPC, 1 CC image like
// a standard go image Gray data.
func (ccitt *CCITTFaxEncoder) EncodeBytes(data []byte) ([]byte, error) {
	var gray imageutil.Gray
	switch len(data) {
	case ccitt.Rows * ccitt.Columns:
		image, err := imageutil.NewImage(ccitt.Columns, ccitt.Rows, 8, 1, data, nil, nil)
		if err != nil {
			return nil, err
		}
		gray = image.(imageutil.Gray)
	case (ccitt.Columns * ccitt.Rows) + 7>>3:
		image, err := imageutil.NewImage(ccitt.Columns, ccitt.Rows, 1, 1, data, nil, nil)
		if err != nil {
			return nil, err
		}
		monochrome := image.(*imageutil.Monochrome)
		if err = monochrome.AddPadding(); err != nil {
			return nil, err
		}
		gray = monochrome
	default:
		if len(data) < imageutil.BytesPerLine(ccitt.Columns, 1, 1)*ccitt.Rows {
			return nil, errors.New("provided invalid input data")
		}
		image, err := imageutil.NewImage(ccitt.Columns, ccitt.Rows, 1, 1, data, nil, nil)
		if err != nil {
			return nil, err
		}
		monochrome := image.(*imageutil.Monochrome)
		gray = monochrome
	}
	datas := make([][]byte, ccitt.Rows)
	for i := 0; i < ccitt.Rows; i++ {
		row := make([]byte, ccitt.Columns)
		for j := 0; j < ccitt.Columns; j++ {
			px := gray.GrayAt(j, i)
			row[j] = px.Y >> 7
		}
		datas[i] = row
	}
	encoder := &ccittfax.Encoder{K: ccitt.K, Columns: ccitt.Columns, EndOfLine: ccitt.EndOfLine, EndOfBlock: ccitt.EndOfBlock, BlackIs1: ccitt.BlackIs1, DamagedRowsBeforeError: ccitt.DamagedRowsBeforeError, Rows: ccitt.Rows, EncodedByteAlign: ccitt.EncodedByteAlign}
	return encoder.Encode(datas), nil
}

// CCITTFaxEncoder implements Group3 and Group4 facsimile (fax) encoder/decoder.
type CCITTFaxEncoder struct {
	K                      int
	EndOfLine              bool
	EncodedByteAlign       bool
	Columns                int
	Rows                   int
	EndOfBlock             bool
	BlackIs1               bool
	DamagedRowsBeforeError int
	limits                 decodeLimits
}

const (
	XrefTypeTableEntry   xrefType = iota
	XrefTypeObjectStream xrefType = iota
)

func acquireBuffer() *bytes.Buffer { return bufferPool.Get().(*bytes.Buffer) }

// Equals reports whether other has the same contents as this object (deep comparison).
func (num *PdfObjectFloat) Equals(other PdfObject) bool {
	if num == nil {
		return other == nil
	}
	return equalObjects(num, other, 0, nil)
}

// Set sets the dictionary's key -> val mapping entry. Overwrites if key already set.
func (dict *PdfObjectDictionary) Set(key PdfObjectName, val PdfObject) {
	dict.setWithLock(key, val, true)
}

func decodeTIFFPredictor(data []byte, columns, bitsPerComp, colors, rowBytes int) ([]byte, error) {
	if rowBytes < 1 {
		return data[:0], nil
	}
	if len(data)%rowBytes != 0 {
		return nil, fmt.Errorf("invalid row length (%d/%d)", len(data), rowBytes)
	}
	if rowBytes > len(data) {
		return nil, errors.New("range check error")
	}
	for i := 0; i < len(data); i += rowBytes {
		row := data[i : i+rowBytes]
		switch bitsPerComp {
		case 8:
			for j := columns; j < rowBytes; j++ {
				row[j] += row[j-columns]
			}
		case 16:
			for j := 2 * columns; j+1 < rowBytes; j += 2 {
				sum := binary.BigEndian.Uint16(row[j:]) + binary.BigEndian.Uint16(row[j-2*columns:])
				binary.BigEndian.PutUint16(row[j:], sum)
			}
		default:
			b := byte(1)<<bitsPerComp - 1
			bitPos := func(col int) (int, int) { bit := col * bitsPerComp; return bit / 8, 8 - bitsPerComp - bit%8 }
			for j := columns; j < colors*columns; j++ {
				byteIdx, shift := bitPos(j)
				bytePrev, shiftPrev := bitPos(j - columns)
				sum := (row[byteIdx]>>shift + row[bytePrev]>>shiftPrev) & b
				row[byteIdx] = row[byteIdx]&^(b<<shift) | sum<<shift
			}
		}
	}
	return data, nil
}

// GetStringVal returns the string value represented by the PdfObject directly or indirectly if
// contained within an indirect object. On type mismatch the found bool flag returned is false and
// an empty string is returned.
func GetStringVal(obj PdfObject) (val string, ok bool) {
	str, ok := TraceToDirectObject(obj).(*PdfObjectString)
	if ok {
		return str.Str(), true
	}
	return
}

// GetRevision returns PdfParser for the specific version of the Pdf document.
func (parser *PdfParser) GetRevision(revisionNumber int) (*PdfParser, error) {
	numRevisions := parser.numRevisions
	if numRevisions == revisionNumber {
		return parser, nil
	}
	if revisionNumber < 0 || numRevisions < revisionNumber {
		return nil, errors.New("undefined revisionNumber version")
	}
	if parser.revisions[revisionNumber] != nil {
		return parser.revisions[revisionNumber], nil
	}
	cur := parser
	for ; numRevisions > revisionNumber; numRevisions-- {
		previousRevisionParser, err := cur.GetPreviousRevisionParser()
		if err != nil {
			return nil, err
		}
		parser.revisions[numRevisions-1] = previousRevisionParser
		parser.revisionParsers[cur] = previousRevisionParser
		cur = previousRevisionParser
	}
	return cur, nil
}

// NewASCII85Encoder makes a new ASCII85 encoder.
func NewASCII85Encoder() *ASCII85Encoder { ascii85 := &ASCII85Encoder{}; return ascii85 }

// DecryptWithCertificate authenticates a document encrypted with a public-key
// security handler (/Filter /Adobe.PubSec) using a recipient's certificate and
// private key, and builds the document key. The key must be an
// *rsa.PrivateKey: RSA key transport is the only recipient type supported.
// It returns security.ErrNotRecipient when the certificate is not among the
// recipients, security.ErrRecipientNotOpened when the recipient's entry does
// not open with the key, and ErrPasswordRequired when the document uses the
// standard security handler. The deferred xref health check behaves as for
// Decrypt.
func (parser *PdfParser) DecryptWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, error) {
	if parser.crypter == nil {
		return false, errors.New("check encryption first")
	}
	ok, err := parser.crypter.authenticateCertificate(cert, key)
	if err != nil {
		return false, err
	}
	if ok {
		if err2 := parser.afterAuthentication(); err2 != nil {
			return true, err2
		}
	}
	return ok, nil
}

// Len returns the number of elements in the streams.
func (streams *PdfObjectStreams) Len() int {
	if streams == nil {
		return 0
	}
	return len(streams.vec)
}

func (parser *PdfParser) sectioningRevisionEnds() ([]int64, error) {
	if parser.numRevisions == 0 {
		return nil, errors.New("this is first revision")
	}
	nums, ok, err := parser.revisionEndOffsets()
	if err != nil {
		if errors.Is(err, errEOFNotFound) {
			return nil, fmt.Errorf("%w: no %%%%EOF markers located", ErrRevisionBoundaryNotFound)
		}
		return nil, err
	}
	want := parser.numRevisions + 1
	if len(nums) != want {
		return nil, fmt.Errorf("%w: located %d of %d revision end markers", ErrRevisionBoundaryNotFound, len(nums), want)
	}
	if !ok {
		return nil, fmt.Errorf("%w: %%%%EOF markers cannot be reconciled with %d revisions", ErrRevisionBoundaryNotFound, want)
	}
	return nums, nil
}

var bytePools = [len(bytePoolSizes)]sync.Pool{{New: func() any { data := make([]byte, poolSize16K); return &data }}, {New: func() any { data := make([]byte, poolSize64K); return &data }}, {New: func() any { data := make([]byte, poolSize256K); return &data }}, {New: func() any {
	data := make([]byte, poolSize1M)
	return &data
}}, {New: func() any { data := make([]byte, poolSize4M); return &data }}, {New: func() any { data := make([]byte, poolSize16M); return &data }}}

func imageBaseFromDict(dict *PdfObjectDictionary) (img *imageutil.ImageBase) {
	var (
		num *PdfObjectInteger
		ok  bool
	)
	if num, ok = dict.Get("Width").(*PdfObjectInteger); ok {
		img = &imageutil.ImageBase{Width: int(*num)}
	} else {
		return nil
	}
	if num, ok = dict.Get("Height").(*PdfObjectInteger); ok {
		img.Height = int(*num)
	}
	if num, ok = dict.Get("BitsPerComponent").(*PdfObjectInteger); ok {
		img.BitsPerComponent = int(*num)
	}
	if num, ok = dict.Get("ColorComponents").(*PdfObjectInteger); ok {
		img.ColorComponents = int(*num)
	}
	return img
}

// MakeNull creates an PdfObjectNull.
func MakeNull() *PdfObjectNull { null := PdfObjectNull{}; return &null }

// String returns a string describing `streams`.
func (streams *PdfObjectStreams) String() string {
	return fmt.Sprintf("Object stream %d", streams.ObjectNumber)
}

// GetFileOffset returns the file offset of the next byte to be consumed.
// It is derived from the section reader's position (bytes pulled into the
// buffer) minus the bytes still buffered, so it stays accurate regardless of
// whether bytes were consumed through the cursor methods or directly via the
// buffer returned by Underlying().
func (cursor *readerAtCursor) GetFileOffset() int64 {
	sectionPosition, _ := cursor.section.Seek(0, io.SeekCurrent)
	return cursor.sectionOffset + sectionPosition - int64(cursor.buffered.Buffered())
}

func isJBIG2Stream(stream *PdfObjectStream) bool {
	switch v := TraceToDirectObject(stream.Get("Filter")).(type) {
	case *PdfObjectName:
		return v.String() == StreamEncodingFilterNameJBIG2
	case *PdfObjectArray:
		for _, elem := range v.Elements() {
			if name, ok := GetName(elem); ok && name.String() == StreamEncodingFilterNameJBIG2 {
				return true
			}
		}
	}
	return false
}

func parseHintTables(data []byte, dict *PdfObjectDictionary, numPages int) (*HintTables, error) {
	if dict == nil {
		return nil, errors.New("hint stream missing dictionary")
	}
	hints := &HintTables{}
	bitReader := newBitReader(data)
	pageOffsetHintTable, err := readPageOffsetHintTable(bitReader, numPages)
	if err != nil {
		return nil, fmt.Errorf("page offset hint table: %w", err)
	}
	hints.PageOffset = pageOffsetHintTable
	nonNegativeInt, ok := getNonNegativeInt(dict, "S")
	if !ok {
		return hints, errors.New("hint stream dictionary missing required /S offset")
	}
	if err2 := bitReader.seek(nonNegativeInt); err2 != nil {
		return hints, fmt.Errorf("seek to shared object table: %w", err2)
	}
	sharedObjectHintTable, err := readSharedObjectHintTable(bitReader)
	if err != nil {
		return hints, fmt.Errorf("shared object hint table: %w", err)
	}
	hints.SharedObject = sharedObjectHintTable
	sts := []struct {
		name   string
		assign func(*HintTables, *GenericHintTable)
	}{{"O", func(tables *HintTables, generic *GenericHintTable) { tables.OutlineHierarchy = generic }}, {"A", func(tables *HintTables, generic *GenericHintTable) { tables.ArticleThread = generic }}, {"E", func(tables *HintTables, generic *GenericHintTable) { tables.NamedDestination = generic }}, {"V", func(tables *HintTables, generic *GenericHintTable) { tables.InteractiveForm = generic }}, {"I", func(tables *HintTables, generic *GenericHintTable) { tables.InformationDict = generic }}, {"C", func(tables *HintTables, generic *GenericHintTable) { tables.LogicalStructure = generic }}, {"L", func(tables *HintTables, generic *GenericHintTable) { tables.PageLabel = generic }}, {"R", func(tables *HintTables, generic *GenericHintTable) { tables.Rendition = generic }}, {"B", func(tables *HintTables, generic *GenericHintTable) { tables.EmbeddedFile = generic }}}
	for _, st := range sts {
		offset, hasNonNegativeInt := getNonNegativeInt(dict, PdfObjectName(st.name))
		if !hasNonNegativeInt {
			continue
		}
		if err2 := bitReader.seek(offset); err2 != nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("WARN: optional hint table /%s seek failed: %v", st.name, err2)
			}
			continue
		}
		firstObjNum, err2 := bitReader.readBits(32)
		firstOffset, err3 := bitReader.readBits(32)
		numObjects, err4 := bitReader.readBits(32)
		groupLength, err5 := bitReader.readBits(32)
		if err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("WARN: optional hint table /%s header truncated", st.name)
			}
			continue
		}
		st.assign(hints, &GenericHintTable{FirstObjNum: firstObjNum, FirstOffset: firstOffset, NumObjects: numObjects, GroupLength: groupLength})
	}
	if offset, hasNonNegativeInt := getNonNegativeInt(dict, "T"); hasNonNegativeInt {
		if err2 := bitReader.seek(offset); err2 == nil {
			if thumbnailHintTable, err3 := readThumbnailHintTable(bitReader); err3 == nil {
				hints.Thumbnail = thumbnailHintTable
			}
		}
	}
	return hints, nil
}

// PdfObjectDictionary represents the primitive PDF dictionary/map object.
type PdfObjectDictionary struct {
	dict   map[PdfObjectName]PdfObject
	keys   []PdfObjectName
	mu     sync.RWMutex
	parser *PdfParser
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
// Has the Filter set and the DecodeParms.
func (flate *FlateEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(flate.GetFilterName()))
	decodeParams := flate.MakeDecodeParams()
	if decodeParams != nil {
		dict.Set("DecodeParms", decodeParams)
	}
	return dict
}
func (null *PdfObjectNull) Direct() PdfObject { return null }

// PdfObjectName represents the primitive PDF name object.
type PdfObjectName string

func (crypt *PdfCrypt) isEncrypted(obj PdfObject) bool {
	_, ok := crypt.encryptedObjects[obj]
	if ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Already encrypted")
		}
		return true
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Not encrypted yet")
	}
	return false
}

func equalIDObjects(a, b PdfObject) bool {
	a = TraceToDirectObject(a)
	b = TraceToDirectObject(b)
	switch v := a.(type) {
	case *PdfObjectString:
		str, ok := b.(*PdfObjectString)
		return ok && v.Str() == str.Str()
	case *PdfObjectArray:
		arr, ok := b.(*PdfObjectArray)
		if !ok || v.Len() != arr.Len() {
			return false
		}
		for i := 0; i < v.Len(); i++ {
			if !equalIDObjects(v.Get(i), arr.Get(i)) {
				return false
			}
		}
		return true
	default:
		if a == nil || b == nil {
			return a == b
		}
		return string(a.Write()) == string(b.Write())
	}
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

// Set sets the PdfObject at index i of the array. An error is returned if the index is outside bounds.
func (arr *PdfObjectArray) Set(i int, obj PdfObject) error {
	if i < 0 || i >= len(arr.elements) {
		return errors.New("outside bounds")
	}
	arr.elements[i] = obj
	return nil
}

// DecodeStream returns the passed in stream as a slice of bytes.
// The purpose of the method is to satisfy the StreamEncoder interface.
func (rawEncoder *RawEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return streamObj.Stream, nil
}

// Equals compares reference identity (ObjectNumber and GenerationNumber), not the
// contents of the referenced object. This is consistent with EqualObjects for
// references. For content equality, use ref.Direct().Equals(other.Direct()) instead.
func (ref *PdfObjectReference) Equals(other PdfObject) bool {
	if ref == nil {
		return other == nil
	}
	return equalObjects(ref, other, 0, nil)
}

func checkTrailingStartxref(startxref, endOfFirstPage, fileSize int64) *LinearizationViolation {
	if startxref <= endOfFirstPage || (fileSize > 0 && startxref >= fileSize) {
		return &LinearizationViolation{Code: LinErrTrailingStartxrefMismatch, Severity: SeverityError, Message: fmt.Sprintf("trailing startxref (%d) does not point to the first-page xref (after lin dict %d, before /E %d)", startxref, endOfFirstPage, fileSize)}
	}
	return nil
}

func (crypt *PdfCrypt) saveCryptFilters(dict *PdfObjectDictionary) error {
	if crypt.encrypt.V < 4 {
		return errors.New("can only be used with V>=4")
	}
	cfDict := MakeDict()
	dict.Set("CF", cfDict)
	for sKey, cryptFilter := range crypt.cryptFilters {
		if sKey == "Identity" {
			continue
		}
		cryptFilterDict := makeCryptFilterDict(cryptFilter, "")
		cfDict.Set(PdfObjectName(sKey), cryptFilterDict)
	}
	dict.Set("StrF", MakeName(crypt.stringFilter))
	dict.Set("StmF", MakeName(crypt.streamFilter))
	return nil
}

func jbig2FirstPageBitmapSize(encoded []byte, globals jbig2.Globals) (int64, bool) {
	doc, err := document.DecodeDocument(bitwise.NewReader(encoded), globals.ToDocumentGlobals())
	if err != nil || len(doc.Pages) == 0 {
		return 0, false
	}
	first := -1
	for i := range doc.Pages {
		if first < 0 || i < first {
			first = i
		}
	}
	for _, segment := range doc.Pages[first].Segments {
		if segment.Type != segments.TPageInformation {
			continue
		}
		segmentData, err2 := segment.GetSegmentData()
		if err2 != nil {
			return 0, false
		}
		pageInformationSegment, ok := segmentData.(*segments.PageInformationSegment)
		if !ok || pageInformationSegment.PageBMWidth <= 0 || pageInformationSegment.PageBMHeight <= 0 {
			return 0, false
		}
		return (int64(pageInformationSegment.PageBMWidth) + 7) / 8 * int64(pageInformationSegment.PageBMHeight), true
	}
	return 0, false
}

// NewDCTEncoder makes a new DCT encoder with default parameters.
func NewDCTEncoder() *DCTEncoder {
	dct := &DCTEncoder{}
	dct.ColorComponents = 3
	dct.BitsPerComponent = 8
	dct.Quality = DefaultJPEGQuality
	dct.Decode = []float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0}
	return dct
}

// Append appends PdfObject(s) to the streams.
func (streams *PdfObjectStreams) Append(objects ...PdfObject) {
	if streams == nil {
		common.Log.Debug("Warn - Attempt to append to a nil streams")
		return
	}
	streams.vec = append(streams.vec, objects...)
}

// IsHexadecimal checks if the PdfObjectString contains Hexadecimal data.
func (str *PdfObjectString) IsHexadecimal() bool { return str.isHex }

func (crypt *PdfCrypt) securityHandler() security.StdHandler {
	if crypt.stdEncrypt.R >= 5 {
		return security.NewHandlerR6()
	}
	return security.NewHandlerR4(crypt.id0, crypt.encrypt.Length)
}

// MakeHexString creates an PdfObjectString from a string intended for output as a hexadecimal string.
func MakeHexString(s string) *PdfObjectString {
	str := PdfObjectString{val: s, isHex: true}
	return &str
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (name *PdfObjectName) Equals(other PdfObject) bool {
	if name == nil {
		return other == nil
	}
	return equalObjects(name, other, 0, nil)
}

// String returns a string describing `d`.
func (dict *PdfObjectDictionary) String() string {
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	var builder strings.Builder
	builder.WriteString("Dict(")
	for _, key := range dict.keys {
		obj := dict.dict[key]
		builder.WriteString("\"" + key.String() + "\": ")
		builder.WriteString(obj.String())
		builder.WriteString(", ")
	}
	builder.WriteString(")")
	return builder.String()
}

// PdfObjectString represents the primitive PDF string object.
type PdfObjectString struct {
	val        string
	isHex      bool
	hexEncoded bool
}
type limitedDecodeReader struct {
	src         io.Reader
	limits      decodeLimits
	streamLimit int64
	read        int64
	filterName  string
	err         error
}

// PdfIndirectObject represents the primitive PDF indirect object.
type PdfIndirectObject struct {
	PdfObjectReference
	PdfObject
}

// BitWriter is an MSB-first bit packer. PDF hint tables specify all fields
// as variable-width unsigned integers packed with no padding between fields;
// alignment to a byte boundary is only required between top-level tables.
//
// Exported so the model package can use it for hint stream encoding.
type BitWriter struct {
	data  []byte
	acc   uint64
	nbits uint
}
type errorReader struct{ err error }

// StreamEncoder represents the interface for all PDF stream encoders.
type StreamEncoder interface {
	GetFilterName() string
	MakeDecodeParams() PdfObject
	MakeStreamDict() *PdfObjectDictionary
	UpdateParams(dict *PdfObjectDictionary)
	EncodeBytes(data []byte) ([]byte, error)
	DecodeBytes(data []byte) ([]byte, error)
	DecodeStream(stream *PdfObjectStream) ([]byte, error)
}

// EncryptInfo contains an information generated by the document encrypter.
type EncryptInfo struct {
	Version

	// Encrypt is an encryption dictionary that contains all necessary parameters.
	// It should be stored in all copies of the document trailer.
	Encrypt *PdfObjectDictionary

	// ID0 and ID1 are IDs used in the trailer. Older algorithms such as RC4 uses them for encryption.
	ID0, ID1 string
}

// PdfCryptNewEncryptPubSec makes the document crypt handler for the public-key
// security handler (/Filter /Adobe.PubSec), encrypting with cf for the given
// recipient certificates, which all receive perm and must carry RSA public
// keys. cf must be a crypt filter (AESV2 for V4, AESV3 for V5); the output
// uses SubFilter adbe.pkcs7.s5, the layout that carries /Recipients in the
// crypt filter dictionary.
func PdfCryptNewEncryptPubSec(cf crypt.Filter, recipients []*x509.Certificate, perm security.Permissions) (*PdfCrypt, *EncryptInfo, error) {
	if cf == nil {
		return nil, nil, errors.New("crypt filter is required")
	}
	V, _ := cf.HandlerVersion()
	if V < 4 {
		return nil, nil, fmt.Errorf("public-key encryption requires a crypt filter (AESV2 or AESV3), not %s", cf.Name())
	}
	crypt := &PdfCrypt{encryptedObjects: make(map[PdfObject]bool), cryptFilters: cryptFilters{defaultCryptFilterKey: cf}, streamFilter: defaultCryptFilterKey, stringFilter: defaultCryptFilterKey, recipientFilter: defaultCryptFilterKey}
	var version Version
	nums := cf.PDFVersion()
	version.Major, version.Minor = nums[0], nums[1]
	crypt.encrypt.Filter = adobePubSecFilter
	crypt.encrypt.V = V
	crypt.encrypt.Length = cf.KeyLength() * 8
	data := make([]byte, security.PubSecSeedLength)
	if _, err := rand.Read(data); err != nil {
		return nil, nil, fmt.Errorf("generating seed: %w", err)
	}
	permBytes := security.PubSecPermissionBytes(perm)
	datas, err := security.PubSecWrap(data, permBytes, recipients)
	if err != nil {
		return nil, nil, err
	}
	pubSecDict := &security.PubSecEncryptDict{SubFilter: security.PubSecSubFilterS5, Recipients: datas, EncryptMetadata: true}
	crypt.pubSec = pubSecDict
	crypt.encryptionKey = security.PubSecFileKey(data, datas, V, crypt.encrypt.Length, pubSecDict.EncryptMetadata)
	crypt.permissions = security.PubSecPermissions(permBytes)
	crypt.authenticated = true
	id0, id1 := generateFileIDs()
	crypt.id0 = id0
	encryptPubSec, err := crypt.encodeEncryptPubSec()
	if err != nil {
		return nil, nil, err
	}
	return crypt, &EncryptInfo{Version: version, Encrypt: encryptPubSec, ID0: id0, ID1: id1}, nil
}

const defaultCryptFilterKey = "DefaultCryptFilter"

// GetObjectNums returns a sorted list of object numbers of the PDF objects in the file.
func (parser *PdfParser) GetObjectNums() []int {
	var nums []int
	for _, xrefObj := range parser.currentXrefs().ObjectMap {
		nums = append(nums, xrefObj.ObjectNumber)
	}
	sort.Ints(nums)
	return nums
}

// EncodeBytes JPX encodes the passed in slice of bytes.
func (jpxEncoder *JPXEncoder) EncodeBytes(data []byte) ([]byte, error) {
	common.Log.Debug("Error: Attempting to use unsupported encoding %s", jpxEncoder.GetFilterName())
	return data, ErrNoJPXDecode
}

// GetFloatVal returns the float64 value represented by the PdfObject directly or indirectly if contained within an
// indirect object. On type mismatch the found bool flag returned is false and a nil pointer is returned.
func GetFloatVal(obj PdfObject) (val float64, ok bool) {
	num, ok := TraceToDirectObject(obj).(*PdfObjectFloat)
	if ok {
		return float64(*num), true
	}
	return 0, false
}

// ReadByte reads a single byte.
func (cursor *readerAtCursor) ReadByte() (byte, error) { return cursor.buffered.ReadByte() }

// GetString returns the *PdfObjectString represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetString(obj PdfObject) (str *PdfObjectString, ok bool) {
	str, ok = TraceToDirectObject(obj).(*PdfObjectString)
	return str, ok
}
func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

// Underlying returns the inner *bufio.Reader so helpers that take a raw
// *bufio.Reader (ParseNumber) keep working without copying bytes. Bytes
// consumed through it are accounted for by GetFileOffset.
func (cursor *readerAtCursor) Underlying() *bufio.Reader { return cursor.buffered }

// String returns the PDF version as a string. Implements interface fmt.Stringer.
func (version Version) String() string { return fmt.Sprintf("%0d.%0d", version.Major, version.Minor) }

// DecodeBytes decodes the CCITTFax encoded image data.
func (ccitt *CCITTFaxEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	if ccitt.Columns < 1 || ccitt.Columns > maxCCITTColumns {
		return nil, fmt.Errorf("%w: CCITTFax Columns %d outside [1, %d]", ErrRangeError, ccitt.Columns, maxCCITTColumns)
	}
	if ccitt.Rows < 0 {
		return nil, fmt.Errorf("%w: CCITTFax Rows %d is negative", ErrRangeError, ccitt.Rows)
	}
	decoder, err := ccittfax.NewDecoder(encoded, ccittfax.DecodeOptions{Columns: ccitt.Columns, Rows: ccitt.Rows, K: ccitt.K, EncodedByteAligned: ccitt.EncodedByteAlign, BlackIsOne: ccitt.BlackIs1, EndOfBlock: ccitt.EndOfBlock, EndOfLine: ccitt.EndOfLine, DamagedRowsBeforeError: ccitt.DamagedRowsBeforeError})
	if err != nil {
		return nil, err
	}
	all, err := ccitt.limits.readAll(StreamEncodingFilterNameCCITTFax, decoder)
	if err != nil {
		ReleaseBytes(all)
		return nil, err
	}
	ccitt.limits.charge(int64(len(all)))
	return all, nil
}

// MakeEncodedString creates a PdfObjectString with encoded content, which can be either
// UTF-16BE or PDFDocEncoding depending on whether `utf16BE` is true or false respectively.
func MakeEncodedString(s string, utf16BE bool) *PdfObjectString {
	if utf16BE {
		var buf bytes.Buffer
		buf.Write([]byte{0xFE, 0xFF})
		buf.WriteString(strutils.StringToUTF16(s))
		return &PdfObjectString{val: buf.String(), isHex: true}
	}
	return &PdfObjectString{val: string(strutils.StringToPDFDocEncoding(s)), isHex: false}
}

func (crypt *PdfCrypt) isDecrypted(obj PdfObject) bool {
	_, ok := crypt.decryptedObjects[obj]
	if ok {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Already decrypted")
		}
		return true
	}
	switch v := obj.(type) {
	case *PdfObjectStream:
		if crypt.stdEncrypt.R != 5 {
			if typeName, isName := v.Get("Type").(*PdfObjectName); isName && *typeName == "XRef" {
				return true
			}
		}
	case *PdfIndirectObject:
		if _, ok = crypt.encryptedObjNums[int(v.ObjectNumber)]; ok {
			return true
		}
		switch v2 := v.PdfObject.(type) {
		case *PdfObjectDictionary:
			if filter, isName := v2.Get("Filter").(*PdfObjectName); isName && *filter == adobePubSecFilter {
				return true
			}
			ok2 := true
			for _, encryptDictKey := range encryptDictKeys {
				if v2.Get(encryptDictKey) == nil {
					ok2 = false
					break
				}
			}
			if ok2 {
				return true
			}
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Not decrypted yet")
	}
	return false
}
func (crypt *PdfCrypt) dropLazyStreamData() { crypt.lazyStreamData = nil }
func generateFileIDs() (id0, id1 string) {
	data := md5.Sum([]byte(time.Now().Format(time.RFC850)))
	id0 = string(data[:])
	buf := make([]byte, 100)
	rand.Read(buf)
	data = md5.Sum(buf)
	id1 = string(data[:])
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Random b: % x", buf)
		common.Log.Trace("Gen Id 0: % x", id0)
	}
	return id0, id1
}

// NewRawEncoder returns a new instace of RawEncoder.
func NewRawEncoder() *RawEncoder { return &RawEncoder{} }

// NewJPXEncoder returns a new instance of JPXEncoder.
func NewJPXEncoder() *JPXEncoder { return &JPXEncoder{} }

// FlateEncoder represents Flate encoding.
type FlateEncoder struct {
	Predictor        int
	BitsPerComponent int

	// For predictors
	Columns int
	Rows    int
	Colors  int
	image   *imageutil.ImageBase
	limits  decodeLimits
}

// NewParserWithOptsAt creates a parser with explicit size and options.
func NewParserWithOptsAt(r io.ReaderAt, size int64, opts ParserOpts) (*PdfParser, error) {
	return newPdfParserAt(r, size, &opts, new(atomic.Int64))
}

// DecodeReader returns a reader that ASCII85-decodes from r. Handles the
// PDF/Adobe variant: 5-char groups, 'z' shortcut for four zero bytes, '~>'
// EOD marker, whitespace skipped. The stdlib's encoding/ascii85 does not
// recognize '~>' (it treats '~' as a corrupt byte), so this decoder is
// hand-rolled to match DecodeBytes.
func (ascii85 *ASCII85Encoder) DecodeReader(r io.Reader) io.Reader {
	return ascii85.limits.reader(StreamEncodingFilterNameASCII85, &ascii85Reader{src: newByteReader(r)})
}

// MakeIndirectObject creates an PdfIndirectObject with a specified direct object PdfObject.
func MakeIndirectObject(obj PdfObject) *PdfIndirectObject {
	indirect := &PdfIndirectObject{}
	indirect.PdfObject = obj
	return indirect
}

// UpdateParams updates the parameter values of the encoder.
func (ascii85 *ASCII85Encoder) UpdateParams(params *PdfObjectDictionary) {}

type asciiHexReader struct {
	src byteReader
	err error
}
type runLengthReader struct {
	src    byteReader
	err    error
	run    [128]byte
	runEnd int
	runPos int
}

func (parser *PdfParser) revisionEndOffsets() ([]int64, bool, error) {
	parser.revisionEndsOnce.Do(func() {
		parser.revisionEnds, parser.revisionEndsValid, parser.revisionEndsErr = parser.computeRevisionEndOffsets()
	})
	if parser.revisionEndsErr != nil {
		return nil, false, parser.revisionEndsErr
	}
	return parser.revisionEnds, parser.revisionEndsValid, nil
}

var ErrPasswordRequired = errors.New("document is encrypted with the standard security handler; a password is required")

// Merge merges in key/values from another dictionary. Overwriting if has same keys.
// The mutated dictionary (d) is returned in order to allow method chaining.
func (dict *PdfObjectDictionary) Merge(another *PdfObjectDictionary) *PdfObjectDictionary {
	if another != nil {
		for _, key := range another.Keys() {
			obj := another.Get(key)
			dict.Set(key, obj)
		}
	}
	return dict
}

// AcquireBytesZeroed is AcquireBytes with a guarantee that buf[:size] is all
// zeroes. Capacity beyond len is not cleared, so a reslice or append past len
// can still expose bytes from a previous user of the buffer. It is for callers
// that read the buffer before writing every byte of it (e.g. accumulating into
// an alpha mask); callers that overwrite the whole buffer should use
// AcquireBytes and skip the clear. Only pooled buffers are cleared: the make()
// fallback for sizes outside the buckets is already zeroed by the runtime, and
// clearing it would touch every page for nothing.
func AcquireBytesZeroed(size int) []byte {
	bytes := AcquireBytes(size)
	if poolIndexFor(size) >= 0 {
		clear(bytes)
	}
	return bytes
}
func (arr *PdfObjectArray) Direct() PdfObject { return arr }

var hexDigits = []byte("0123456789abcdefABCDEF")

// JBIG2Image is the image structure used by the jbig2 encoder. Its Data must be in a
// 1 bit per component and 1 component per pixel (1bpp). In order to create binary image
// use GoImageToJBIG2 function. If the image data contains the row bytes padding set the HasPadding to true.
type JBIG2Image struct {
	// Width and Height defines the image boundaries.
	Width, Height int

	// Data is the byte slice data for the input image
	Data []byte

	// HasPadding is the attribute that defines if the last byte of the data in the row contains
	// 0 bits padding.
	HasPadding bool
}

// BuildHintStreamObject wraps the encoded hint table bytes in a stream
// indirect object suitable for emission. The stream is Flate-encoded and
// carries the required /S key in its dictionary so the reader can locate
// the Shared Object Hint Table.
func BuildHintStreamObject(objNum int64, payload []byte, sOffset int64) ([]byte, error) {
	stream, err := MakeStream(payload, NewFlateEncoder())
	if err != nil {
		return nil, fmt.Errorf("hint stream encode: %w", err)
	}
	stream.ObjectNumber = objNum
	stream.Set("S", MakeInteger(sOffset))
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d 0 obj\n", objNum)
	buf.Write(stream.PdfObjectDictionary.Write())
	buf.WriteString("\nstream\n")
	buf.Write(stream.Stream)
	buf.WriteString("\nendstream\nendobj\n")
	return buf.Bytes(), nil
}

// ToIntegerArray returns a slice of all array elements as an int slice. An error is returned if the
// array non-integer objects. Each element can only be PdfObjectInteger.
func (arr *PdfObjectArray) ToIntegerArray() ([]int, error) {
	var nums []int
	for _, elem := range arr.Elements() {
		if num, ok := elem.(*PdfObjectInteger); ok {
			nums = append(nums, int(*num))
		} else {
			return nil, ErrTypeError
		}
	}
	return nums, nil
}

// NewParserFromString is used for testing purposes.
func NewParserFromString(txt string) *PdfParser {
	reader := bytes.NewReader([]byte(txt))
	parser := &PdfParser{decodedTotal: new(atomic.Int64), reader: reader, fileSize: int64(len(txt)), revisionParsers: make(map[*PdfParser]*PdfParser)}
	parser.setXrefs(XrefTable{ObjectMap: map[int]XrefObject{}})
	return parser
}

// UpdateParams updates the parameter values of the encoder.
func (enc *LZWEncoder) UpdateParams(params *PdfObjectDictionary) {
	predictor, err := GetNumberAsInt64(params.Get("Predictor"))
	if err == nil {
		enc.Predictor = int(predictor)
	}
	bitsPerComponent, err := GetNumberAsInt64(params.Get("BitsPerComponent"))
	if err == nil {
		enc.BitsPerComponent = int(bitsPerComponent)
	}
	width, err := GetNumberAsInt64(params.Get("Width"))
	if err == nil {
		enc.Columns = int(width)
	}
	colorComponents, err := GetNumberAsInt64(params.Get("ColorComponents"))
	if err == nil {
		enc.Colors = int(colorComponents)
	}
	earlyChange, err := GetNumberAsInt64(params.Get("EarlyChange"))
	if err == nil {
		enc.EarlyChange = int(earlyChange)
	}
}

type decodeLimits struct {
	maxStream int64
	maxTotal  int64
	total     *atomic.Int64
}

const (
	predictorNone    = 0
	predictorSub     = 1
	predictorUp      = 2
	predictorAverage = 3
	predictorPaeth   = 4
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

// SharedObjectHintTable is the mandatory shared object hint table
// (ISO 32000-2 Annex F.4.5).
//
// A "shared object" is an object referenced from more than one page, plus all
// objects from the first page that are not first-page-only.
type SharedObjectHintTable struct {
	// Header fields (Table F.4).
	FirstSharedObjNum             uint32
	FirstSharedOffset             uint32
	NumSharedFirstPage            uint32
	NumSharedTotal                uint32
	BitsPerGroupIdentifier        uint16
	LeastSharedGroupLength        uint32
	BitsPerSharedGroupLengthDelta uint16
	Entries                       []SharedObjectEntry
}

// UnreadByte unreads the last byte read.
func (cursor *readerAtCursor) UnreadByte() error { return cursor.buffered.UnreadByte() }

// ShallowCopy returns a new PdfObjectDictionary with the same key-value mappings.
// The new dictionary has its own mutex and independent keys slice, but values
// are shared (not deep-copied).
func (dict *PdfObjectDictionary) ShallowCopy() *PdfObjectDictionary {
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	clone := &PdfObjectDictionary{}
	clone.dict = make(map[PdfObjectName]PdfObject, len(dict.dict))
	clone.keys = make([]PdfObjectName, len(dict.keys))
	copy(clone.keys, dict.keys)
	maps.Copy(clone.dict, dict.dict)
	clone.parser = dict.parser
	return clone
}

// GetRevisionNumber returns the current version of the Pdf document.
func (parser *PdfParser) GetRevisionNumber() int { return parser.numRevisions }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (rawEncoder *RawEncoder) MakeDecodeParams() PdfObject { return nil }

// MakeArrayFromFloats creates an PdfObjectArray from a slice of float64s, where each array element is an
// PdfObjectFloat.
func MakeArrayFromFloats(vals []float64) *PdfObjectArray {
	array := MakeArray()
	for _, val := range vals {
		array.Append(MakeFloat(val))
	}
	return array
}

// DecodeImages decodes the page images from the jbig2 'encoded' data input.
// The jbig2 document may contain multiple pages, thus the function can return multiple
// images. The images order corresponds to the page number.
func (jbig2 *JBIG2Encoder) DecodeImages(encoded []byte) ([]image.Image, error) {
	const procJBIG2EncoderDecodeImages = "JBIG2Encoder.DecodeImages"
	dec, err := decoder.Decode(encoded, decoder.Parameters{}, jbig2.Globals.ToDocumentGlobals())
	if err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2EncoderDecodeImages, "")
	}
	numPages, err := dec.PageNumber()
	if err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2EncoderDecodeImages, "")
	}
	imgs := []image.Image{}
	var img image.Image
	for i := 1; i <= numPages; i++ {
		img, err = dec.DecodePageImage(i)
		if err != nil {
			return nil, jbig2errors.Wrapf(err, procJBIG2EncoderDecodeImages, "page: '%d'", i)
		}
		imgs = append(imgs, img)
	}
	return imgs, nil
}

func (parser *PdfParser) doRepairRebuildXrefsTopDown() (*XrefTable, error) {
	cursor := parser.newCursor(0)
	winLen := 20
	data := make([]byte, winLen)
	xrefs := XrefTable{}
	xrefs.ObjectMap = make(map[int]XrefObject)
	for {
		b, err := cursor.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return nil, err
			}
		}
		if b == 'j' && data[winLen-1] == 'b' && data[winLen-2] == 'o' && IsWhiteSpace(data[winLen-3]) {
			pos := winLen - 4
			for IsWhiteSpace(data[pos]) && pos > 0 {
				pos--
			}
			if pos == 0 || !IsDecimalDigit(data[pos]) {
				continue
			}
			for IsDecimalDigit(data[pos]) && pos > 0 {
				pos--
			}
			if pos == 0 || !IsWhiteSpace(data[pos]) {
				continue
			}
			for IsWhiteSpace(data[pos]) && pos > 0 {
				pos--
			}
			if pos == 0 || !IsDecimalDigit(data[pos]) {
				continue
			}
			for IsDecimalDigit(data[pos]) && pos > 0 {
				pos--
			}
			if pos == 0 {
				continue
			}
			objOffset := cursor.GetFileOffset() - int64(winLen-pos)
			candidate := append(data[pos+1:], b)
			objSignature, genNum, err2 := parseObjSignature(string(candidate))
			if err2 != nil {
				common.Log.Debug("Unable to parse object number: %v", err2)
				return nil, err2
			}
			if xrefObj, ok := xrefs.ObjectMap[objSignature]; !ok || xrefObj.Generation <= genNum {
				entry := XrefObject{}
				entry.XType = XrefTypeTableEntry
				entry.ObjectNumber = objSignature
				entry.Generation = genNum
				entry.Offset = objOffset
				xrefs.ObjectMap[objSignature] = entry
			}
		}
		data = append(data[1:winLen], b)
	}
	return &xrefs, nil
}

// Set sets the PdfObject at index i of the streams. An error is returned if the index is outside bounds.
func (streams *PdfObjectStreams) Set(i int, obj PdfObject) error {
	if i < 0 || i >= len(streams.vec) {
		return errors.New("outside bounds")
	}
	streams.vec[i] = obj
	return nil
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (num *PdfObjectInteger) Equals(other PdfObject) bool {
	if num == nil {
		return other == nil
	}
	return equalObjects(num, other, 0, nil)
}

// StoreCachedObject stores obj in the parser's object cache under objNumber.
func (parser *PdfParser) StoreCachedObject(objNumber int, obj PdfObject) {
	parser.objCache.Store(objNumber, obj)
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
func libraryVersion() string { return common.Version }

// MakeDictMap creates a PdfObjectDictionary initialized from a map of keys to values.
func MakeDictMap(objmap map[string]PdfObject) *PdfObjectDictionary {
	dict := MakeDict()
	return dict.Update(objmap)
}

type encryptDict struct {
	Filter    string
	V         int
	SubFilter string
	Length    int
	StmF      string
	StrF      string
	EFF       string
	CF        map[string]crypt.FilterDict
}

// PdfVersion returns version of the PDF file.
func (parser *PdfParser) PdfVersion() Version { return parser.version }

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (ascii85 *ASCII85Encoder) MakeDecodeParams() PdfObject { return nil }

// String returns a descriptive information string about the encryption method used.
func (crypt *PdfCrypt) String() string {
	if crypt == nil {
		return ""
	}
	var out strings.Builder
	out.WriteString(crypt.encrypt.Filter + " - ")
	if crypt.isPubSec() {
		out.WriteString(fmt.Sprintf("%s (%d recipients) - ", crypt.pubSec.SubFilter, len(crypt.pubSec.Recipients)))
	}
	if crypt.encrypt.V == 0 {
		out.WriteString("Undocumented algorithm")
	} else if crypt.encrypt.V == 1 {
		out.WriteString("RC4: 40 bits")
	} else if crypt.encrypt.V == 2 {
		out.WriteString(fmt.Sprintf("RC4: %d bits", crypt.encrypt.Length))
	} else if crypt.encrypt.V == 3 {
		out.WriteString("Unpublished algorithm")
	} else if crypt.encrypt.V >= 4 {
		out.WriteString(fmt.Sprintf("Stream filter: %s - String filter: %s", crypt.streamFilter, crypt.stringFilter))
		out.WriteString("; Crypt filters:")
		for sKey, cryptFilter := range crypt.cryptFilters {
			out.WriteString(fmt.Sprintf(" - %s: %s (%d)", sKey, cryptFilter.Name(), cryptFilter.KeyLength()))
		}
	}
	accessPermissions := crypt.GetAccessPermissions()
	out.WriteString(fmt.Sprintf(" - %#v", accessPermissions))
	return out.String()
}

// SetDefaultMaxDecodedStreamSize sets the maximum number of bytes a single
// stream, or a single filter stage of a filter chain, may decode to when no
// ParserOpts.MaxDecodedStreamSize applies. That covers streams created in
// memory and streams read by a parser whose options leave the limit at 0.
// A value <= 0 restores the built-in default of 256 MiB.
// Decoding past the limit fails with ErrDecodedSizeLimitExceeded.
func SetDefaultMaxDecodedStreamSize(n int64) {
	if n <= 0 {
		n = defaultMaxDecodedStreamSize
	}
	maxDecodedStreamSize.Store(n)
}

// DrawableImage is same as golang image/draw's Image interface that allow drawing images.
type DrawableImage interface {
	ColorModel() color.Model
	Bounds() image.Rectangle
	At(x, y int) color.Color
	Set(x, y int, c color.Color)
}

func (c *PdfCrypt) loadCryptFilters(ed *PdfObjectDictionary) error {
	c.cryptFilters = cryptFilters{}
	cf := ed.Get("CF")
	cf = TraceToDirectObject(cf)
	if ref, ok := cf.(*PdfObjectReference); ok {
		byReference, err := c.parser.LookupByReference(*ref)
		if err != nil {
			common.Log.Debug("Error looking up CF reference")
			return err
		}
		cf = TraceToDirectObject(byReference)
	}
	dict, ok := cf.(*PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Invalid CF, type: %T", cf)
		return errors.New("invalid CF")
	}
	for _, key := range dict.Keys() {
		obj := dict.Get(key)
		if ref, isRef := obj.(*PdfObjectReference); isRef {
			byReference, err := c.parser.LookupByReference(*ref)
			if err != nil {
				common.Log.Debug("Error lookup up dictionary reference")
				return err
			}
			obj = TraceToDirectObject(byReference)
		}
		cfDict, isDict := obj.(*PdfObjectDictionary)
		if !isDict {
			return fmt.Errorf("invalid dict in CF (name %s) - not a dictionary but %T", key, obj)
		}
		if key == "Identity" {
			common.Log.Debug("ERROR - Cannot overwrite the identity filter - Trying next")
			continue
		}
		var filterDict crypt.FilterDict
		if err := parseCryptFilterDict(&filterDict, cfDict); err != nil {
			return err
		}
		filter, err := crypt.NewFilter(filterDict)
		if err != nil {
			return err
		}
		c.cryptFilters[string(key)] = filter
	}
	c.cryptFilters["Identity"] = crypt.NewIdentity()
	c.stringFilter = "Identity"
	if strF, isName := ed.Get("StrF").(*PdfObjectName); isName {
		if _, ok2 := c.cryptFilters[string(*strF)]; !ok2 {
			return fmt.Errorf("crypt filter for StrF not specified in CF dictionary (%s)", *strF)
		}
		c.stringFilter = string(*strF)
	}
	c.streamFilter = "Identity"
	if stmF, isName := ed.Get("StmF").(*PdfObjectName); isName {
		if _, ok2 := c.cryptFilters[string(*stmF)]; !ok2 {
			return fmt.Errorf("crypt filter for StmF not specified in CF dictionary (%s)", *stmF)
		}
		c.streamFilter = string(*stmF)
	}
	return nil
}

// Buffered returns the number of bytes currently in the buffer.
func (cursor *readerAtCursor) Buffered() int { return cursor.buffered.Buffered() }

// TraceToDirectObject traces a PdfObject to a direct object.  For example direct objects contained
// in indirect objects (can be double referenced even).
func TraceToDirectObject(obj PdfObject) PdfObject {
	if ref, ok := obj.(*PdfObjectReference); ok {
		obj = ref.Resolve()
	}
	indirect, ok := obj.(*PdfIndirectObject)
	depth := 0
	for ok {
		obj = indirect.PdfObject
		indirect, ok = GetIndirect(obj)
		depth++
		if depth > maxTraceDepth {
			common.Log.Error("ERROR: Trace depth level beyond %d - not going deeper!", maxTraceDepth)
			return nil
		}
	}
	return obj
}

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (runLength *RunLengthEncoder) MakeDecodeParams() PdfObject { return nil }

// DCTEncoder provides a DCT (JPG) encoding/decoding functionality for images.
type DCTEncoder struct {
	ColorComponents  int
	BitsPerComponent int
	Width            int
	Height           int
	Quality          int
	Decode           []float64
	limits           decodeLimits
}

func paethPredictor(a, b, c uint8) uint8 {
	c2 := int(c)
	pa := int(b) - c2
	pb := int(a) - c2
	c2 = absInt(pa + pb)
	pa = absInt(pa)
	pb = absInt(pb)
	if pa <= pb && pa <= c2 {
		return a
	} else if pb <= c2 {
		return b
	}
	return c
}

// Equals reports whether other has the same contents as this object (deep comparison).
func (b *PdfObjectBool) Equals(other PdfObject) bool {
	if b == nil {
		return other == nil
	}
	return equalObjects(b, other, 0, nil)
}

func (crypt *PdfCrypt) generateParams(userPass, ownerPass []byte) error {
	stdHandler := crypt.securityHandler()
	data, err := stdHandler.GenerateParams(&crypt.stdEncrypt, ownerPass, userPass)
	if err != nil {
		return err
	}
	crypt.encryptionKey = data
	return nil
}

func (crypt *PdfCrypt) checkAccessRightsCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, security.Permissions, error) {
	_, data, _, err := crypt.unwrapRecipient(cert, key)
	if err != nil {
		return false, 0, err
	}
	return true, security.PubSecPermissions(data), nil
}
func (str *PdfObjectString) Direct() PdfObject { return str }

// UpdateParams updates the parameter values of the encoder.
func (asciiHex *ASCIIHexEncoder) UpdateParams(params *PdfObjectDictionary) {}

func writeStdEncryptDict(stdDict *security.StdEncryptDict, dict *PdfObjectDictionary) {
	dict.Set("R", MakeInteger(int64(stdDict.R)))
	dict.Set("P", MakeInteger(int64(int32(stdDict.P))))
	dict.Set("O", MakeStringFromBytes(stdDict.O))
	dict.Set("U", MakeStringFromBytes(stdDict.U))
	if stdDict.R >= 5 {
		dict.Set("OE", MakeStringFromBytes(stdDict.OE))
		dict.Set("UE", MakeStringFromBytes(stdDict.UE))
		dict.Set("EncryptMetadata", MakeBool(stdDict.EncryptMetadata))
		if stdDict.R > 5 {
			dict.Set("Perms", MakeStringFromBytes(stdDict.Perms))
		}
	}
}

const JB2ImageAutoThreshold = -1.0
const (
	StreamEncodingFilterNameFlate     = "FlateDecode"
	StreamEncodingFilterNameLZW       = "LZWDecode"
	StreamEncodingFilterNameDCT       = "DCTDecode"
	StreamEncodingFilterNameRunLength = "RunLengthDecode"
	StreamEncodingFilterNameASCIIHex  = "ASCIIHexDecode"
	StreamEncodingFilterNameASCII85   = "ASCII85Decode"
	StreamEncodingFilterNameCCITTFax  = "CCITTFaxDecode"
	StreamEncodingFilterNameJBIG2     = "JBIG2Decode"
	StreamEncodingFilterNameJPX       = "JPXDecode"
	StreamEncodingFilterNameRaw       = "Raw"
)

// Write outputs the object as it is to be written to file.
func (dict *PdfObjectDictionary) Write() []byte {
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	var buf bytes.Buffer
	buf.WriteString("<<")
	for _, key := range dict.keys {
		obj := dict.dict[key]
		buf.Write(key.Write())
		buf.WriteString(" ")
		buf.Write(obj.Write())
	}
	buf.WriteString(">>")
	return buf.Bytes()
}

func makeCryptFilterDict(filter crypt.Filter, event security.AuthEvent) *PdfObjectDictionary {
	if event == "" {
		event = security.EventDocOpen
	}
	dict := MakeDict()
	dict.Set("Type", MakeName("CryptFilter"))
	dict.Set("AuthEvent", MakeName(string(event)))
	dict.Set("CFM", MakeName(filter.Name()))
	dict.Set("Length", MakeInteger(int64(filter.KeyLength())))
	return dict
}

// DecodeReader returns a reader that RunLength-decodes from r. A length byte
// in [0,127] introduces length+1 literal bytes; a length byte in [129,255]
// repeats the next byte 257-length times; a length byte of 128 marks EOD.
func (runLength *RunLengthEncoder) DecodeReader(r io.Reader) io.Reader {
	return runLength.limits.reader(StreamEncodingFilterNameRunLength, &runLengthReader{src: newByteReader(r)})
}
func (num *PdfObjectFloat) Direct() PdfObject { return num }
func (br *bitReader) skipToNextByte() {
	br.nbits -= br.nbits % 8
	if br.nbits == 0 {
		br.acc = 0
	}
}

// HeaderPosition gets the file header position.
func (meta ParserMetadata) HeaderPosition() int { return meta.headerPosition }

// DecodeStream implements ASCII hex decoding.
func (asciiHex *ASCIIHexEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return asciiHex.DecodeBytes(streamObj.Stream)
}

// ReadBytesAt reads length bytes at the given offset within the PDF. It
// returns an error on a short read (fewer than length bytes available), so
// callers never receive silently zero-padded data.
func (parser *PdfParser) ReadBytesAt(offset, length int64) ([]byte, error) {
	data := make([]byte, length)
	at, err := parser.reader.ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if int64(at) != length {
		return nil, fmt.Errorf("core.ReadBytesAt: short read at offset %d: got %d of %d bytes", offset, at, length)
	}
	return data, nil
}

// LinearizationInfo returns the populated LinearizationInfo for a linearized
// PDF, or nil for a non-linearized one. The returned value is shared state —
// callers should not mutate it.
func (parser *PdfParser) LinearizationInfo() *LinearizationInfo { return parser.linearization }
func (num *PdfObjectInteger) Direct() PdfObject                 { return num }

// Encode encodes previously prepare jbig2 document and stores it as the byte slice.
func (jbig2 *JBIG2Encoder) Encode() (data []byte, err error) {
	const procJBIG2DocumentEncode = "JBIG2Document.Encode"
	if jbig2.doc == nil {
		return nil, jbig2errors.Errorf(procJBIG2DocumentEncode, "document input data not defined")
	}
	jbig2.doc.FullHeaders = jbig2.DefaultPageSettings.FileMode
	data, err = jbig2.doc.Encode()
	if err != nil {
		return nil, jbig2errors.Wrap(err, procJBIG2DocumentEncode, "")
	}
	return data, nil
}

var encryptDictKeys = []PdfObjectName{"V", "R", "O", "U", "P"}

func numberToFloatPtr(obj PdfObject) (*float64, error) {
	switch v := obj.(type) {
	case *PdfObjectFloat:
		f := float64(*v)
		return &f, nil
	case *PdfObjectInteger:
		f := float64(*v)
		return &f, nil
	case *PdfObjectNull:
		return nil, nil
	}
	return nil, ErrNotANumber
}

// MakeArrayFromIntegers creates an PdfObjectArray from a slice of ints, where each array element is
// an PdfObjectInteger.
func MakeArrayFromIntegers(vals []int) *PdfObjectArray {
	array := MakeArray()
	for _, val := range vals {
		array.Append(MakeInteger(int64(val)))
	}
	return array
}

func decodeLimitsFromStream(streamObj *PdfObjectStream) decodeLimits {
	if streamObj == nil {
		return decodeLimits{}
	}
	parser := streamObj.GetParser()
	if parser == nil {
		return decodeLimits{}
	}
	limits := decodeLimits{total: parser.decodedTotal}
	if parser.Opts != nil {
		limits.maxStream = parser.Opts.MaxDecodedStreamSize
		limits.maxTotal = parser.Opts.MaxDecodedTotalSize
	}
	return limits
}

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

// MakeInteger creates a PdfObjectInteger from an int64.
func MakeInteger(val int64) *PdfObjectInteger { num := PdfObjectInteger(val); return &num }

func (flate *FlateEncoder) postDecodePredict(data []byte) ([]byte, error) {
	if flate.Predictor <= 1 {
		return data, nil
	}
	predictor, err := decodePredictor(data, flate.Predictor, flate.Colors, flate.BitsPerComponent, flate.Columns)
	if err != nil {
		ReleaseBytes(data)
		return nil, err
	}
	return predictor, nil
}

var startxrefRegexp = regexp.MustCompile("startx?ref\\s*(\\d+)")

func isPoolSize(n int) bool {
	for _, bytePoolSize := range bytePoolSizes {
		if n == bytePoolSize {
			return true
		}
	}
	return false
}

// ParserMetadata gets the pdf parser metadata.
func (parser *PdfParser) ParserMetadata() (ParserMetadata, error) {
	if !parser.compliance {
		return ParserMetadata{}, fmt.Errorf("parser was not marked for getting detailed metadata")
	}
	return parser.metadata, nil
}

// GetAccessPermissions returns the PDF access permissions as an AccessPermissions object.
// For a public-key encrypted document they are the permissions granted to the
// authenticated recipient, and zero before authentication.
func (crypt *PdfCrypt) GetAccessPermissions() security.Permissions {
	if crypt.isPubSec() {
		return crypt.permissions
	}
	return crypt.stdEncrypt.P
}

// GetFilterName returns the name of the encoding filter.
func (rawEncoder *RawEncoder) GetFilterName() string { return StreamEncodingFilterNameRaw }

// GetPreviousRevision returns an io.SectionReader covering the previous
// revision of the document, ending at the boundary RevisionEndOffsets reports
// for it (past its %%EOF marker and the end-of-line sequence following it).
// The concrete *io.SectionReader satisfies both io.ReadSeeker and io.ReaderAt
// so it can be fed directly into NewParser / NewPdfReader.
// If the revision boundaries cannot be located, an error wrapping
// ErrRevisionBoundaryNotFound is returned.
func (parser *PdfParser) GetPreviousRevision() (*io.SectionReader, error) {
	nums, err := parser.sectioningRevisionEnds()
	if err != nil {
		return nil, err
	}
	return io.NewSectionReader(parser.reader, 0, nums[parser.numRevisions-1]), nil
}

// MakeObjectStreams creates an PdfObjectStreams from a list of PdfObjects.
func MakeObjectStreams(objects ...PdfObject) *PdfObjectStreams {
	return &PdfObjectStreams{vec: objects}
}

// GetBoolVal returns the bool value within a *PdObjectBool represented by an PdfObject interface directly or indirectly.
// If the PdfObject does not represent a bool value, a default value of false is returned (found = false also).
func GetBoolVal(obj PdfObject) (val bool, ok bool) {
	boolObj, ok := TraceToDirectObject(obj).(*PdfObjectBool)
	if ok {
		return bool(*boolObj), true
	}
	return false, false
}

// NewCompliancePdfParser creates a PdfParser that extracts additional metadata
// for standards compliance checks (PDF/A, PDF/X). The source's byte length is
// auto-detected. For exotic io.ReaderAt sources that can't be auto-sized, use
// NewCompliancePdfParserAt.
func NewCompliancePdfParser(r io.ReaderAt) (*PdfParser, error) {
	size, err := SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewCompliancePdfParserAt(r, size)
}

// IsDelimiter checks if a character represents a delimiter.
func IsDelimiter(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}

// IsTextWriteDirectionLTR returns `true` if string write direction Left To Right.
// detect string write direction using golang.org/x/text/unicode/bidi library.
func IsTextWriteDirectionLTR(str string) bool {
	isNewline := func(r rune) bool { return r == '\u000A' }
	trimmed := strings.TrimFunc(str, isNewline)
	paragraph := bidi.Paragraph{}
	_, err := paragraph.SetString(trimmed)
	if err != nil {
		return true
	}
	ordering, err := paragraph.Order()
	if err != nil {
		return true
	}
	if ordering.NumRuns() < 1 {
		return true
	}
	return paragraph.IsLeftToRight()
}

// Remove removes an element specified by key.
func (dict *PdfObjectDictionary) Remove(key PdfObjectName) {
	dict.mu.Lock()
	defer dict.mu.Unlock()
	idx := -1
	for i, name := range dict.keys {
		if name == key {
			idx = i
			break
		}
	}
	if idx >= 0 {
		dict.keys = append(dict.keys[:idx], dict.keys[idx+1:]...)
		delete(dict.dict, key)
	}
}

func (parser *PdfParser) parseIndirectObject(cursor *readerAtCursor, visited map[int]struct{}) (PdfObject, error) {
	indirect := PdfIndirectObject{
		parser: parser,
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
						ObjectNumber:        indirect.ObjectNumber,
						GenerationNumber:    indirect.GenerationNumber,
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
func (streams *PdfObjectStreams) Direct() PdfObject { return streams }
func (parser *PdfParser) currentXrefs() *XrefTable {
	if xrefs := parser.xrefs.Load(); xrefs != nil {
		return xrefs
	}
	xrefs := &XrefTable{ObjectMap: map[int]XrefObject{}}
	if parser.xrefs.CompareAndSwap(nil, xrefs) {
		return xrefs
	}
	return parser.xrefs.Load()
}

// RangeCachedObjects iterates over every cached object, invoking fn for each
// (object number, object) pair. Iteration stops early if fn returns false.
// Iteration order is unspecified.
func (parser *PdfParser) RangeCachedObjects(fn func(objNum int, obj PdfObject) bool) {
	parser.objCache.Range(func(k, v any) bool { return fn(k.(int), v.(PdfObject)) })
}

func parseHintRanges(arr *PdfObjectArray) []HintRange {
	objs := arr.Elements()
	if len(objs) != 2 && len(objs) != 4 {
		return nil
	}
	nums := make([]int64, len(objs))
	for i, obj := range objs {
		n, err := GetNumberAsInt64(obj)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	hintRanges := []HintRange{{Offset: nums[0], Length: nums[1]}}
	if len(nums) == 4 {
		hintRanges = append(hintRanges, HintRange{Offset: nums[2], Length: nums[3]})
	}
	return hintRanges
}

func (parser *PdfParser) parseNull(cursor *readerAtCursor) (PdfObjectNull, error) {
	_, err := cursor.Discard(4)
	return PdfObjectNull{}, err
}

// String returns a string representation of the *PdfObjectString.
func (str *PdfObjectString) String() string { return str.val }

// ParseDict reads and parses a PDF dictionary object enclosed with '<<' and
// '>>' at the start of the source (offset 0). Convenience wrapper for
// ParseDictAt(0), intended for parsers built from a buffer where the dict
// is the entire content (e.g. NewParserFromString-based tests). Callers that
// need to parse at a different offset must use ParseDictAt(offset); the
// parser no longer carries an internal cursor for SetFileOffset-then-ParseDict
// patterns to work against.
func (parser *PdfParser) ParseDict() (*PdfObjectDictionary, error) { return parser.ParseDictAt(0) }

// GetInt returns the *PdfObjectBool object that is represented by a PdfObject either directly or indirectly
// within an indirect object. The bool flag indicates whether a match was found.
func GetInt(obj PdfObject) (num *PdfObjectInteger, ok bool) {
	num, ok = TraceToDirectObject(obj).(*PdfObjectInteger)
	return num, ok
}

const (
	_ LinearizationViolationCode = iota
	LinErrNotLinearized
	LinErrParamDictNotFirstObject
	LinErrFileLengthMismatch
	LinErrFirstPageObjectInvalid
	LinErrFirstPageEndOffset
	LinErrPageCountMismatch
	LinErrHintStreamMissing
	LinErrXRefOffsetMismatch
	LinErrIDMismatch
	LinErrUpdateBreaksLinearization
	LinErrTrailingStartxrefMismatch
	LinErrHintPageCountMismatch
	LinErrFirstPageObjectNotFirstPage
	LinErrFirstPageObjectOutsideSection
	LinErrPageCountTreeMismatch
	LinErrHintStreamLengthMismatch
	LinErrHintStreamMisplaced
	LinErrOverflowHintStreamInvalid
	LinErrPrevChainBroken
	LinErrInvalidVersion
	LinErrFirstPageObjectUnresolved
	LinErrTrailerReadFailure
)

// MakeFloat creates an PdfObjectFloat from a float64.
func MakeFloat(val float64) *PdfObjectFloat {
	val = precision.RoundDefault(val)
	num := PdfObjectFloat(val)
	return &num
}

// GetIndirect returns the *PdfIndirectObject represented by the PdfObject. On type mismatch the found bool flag is
// false and a nil pointer is returned.
func GetIndirect(obj PdfObject) (indirect *PdfIndirectObject, ok bool) {
	obj = ResolveReference(obj)
	indirect, ok = obj.(*PdfIndirectObject)
	return indirect, ok
}

func resolveReferencesIterative(obj PdfObject, visited map[PdfObject]struct{}) error {
	objs := []PdfObject{obj}
	for len(objs) > 0 {
		last := objs[len(objs)-1]
		objs = objs[:len(objs)-1]
		if _, ok := visited[last]; ok {
			continue
		}
		visited[last] = struct{}{}
		switch v := last.(type) {
		case *PdfObjectReference:
			common.Log.Debug("ERROR: Tracing a reference!")
			return errors.New("error tracing a reference")
		case *PdfIndirectObject:
			objs = append(objs, v.PdfObject)
		case *PdfObjectStream:
			objs = append(objs, v.PdfObjectDictionary)
		case *PdfObjectDictionary:
			names := v.Keys()
			for _, name := range slices.Backward(names) {
				child := v.Get(name)
				if ref, ok := child.(*PdfObjectReference); ok {
					child = ref.Resolve()
					v.Set(name, child)
				}
				objs = append(objs, child)
			}
		case *PdfObjectArray:
			elems := v.Elements()
			for i, elem := range slices.Backward(elems) {

				if ref, ok := elem.(*PdfObjectReference); ok {
					elem = ref.Resolve()
					v.Set(i, elem)
				}
				objs = append(objs, elem)
			}
		}
	}
	return nil
}

// Keys returns the list of keys in the dictionary.
// If `d` is nil returns a nil slice.
func (dict *PdfObjectDictionary) Keys() []PdfObjectName {
	if dict == nil {
		return nil
	}
	dict.mu.RLock()
	defer dict.mu.RUnlock()
	names := make([]PdfObjectName, len(dict.keys))
	copy(names, dict.keys)
	return names
}

// UnpooledBytes returns buf, or a copy of it when ReleaseBytes would take
// buf's capacity into the pool. ReleaseBytes never pools the result, so it can
// be shared by values whose owners may each release it.
func UnpooledBytes(buf []byte) []byte {
	if !isPoolSize(cap(buf)) {
		return buf
	}
	size := len(buf)
	for isPoolSize(size) {
		size++
	}
	data := make([]byte, len(buf), size)
	copy(data, buf)
	return data
}

func newReaderAtCursor(source io.ReaderAt, size, offset int64) *readerAtCursor {
	if offset < 0 {
		offset = 0
	}
	cursor := &readerAtCursor{source: source, size: size}
	cursor.reset(offset)
	return cursor
}

var maxDecodedStreamSize atomic.Int64

func (name *PdfObjectName) Direct() PdfObject { return name }
func (limits decodeLimits) exceeded(name string, limit int64) error {
	return fmt.Errorf("%w: %s output over %d bytes", ErrDecodedSizeLimitExceeded, name, limit)
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
func (ccitt *CCITTFaxEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(ccitt.GetFilterName()))
	dict.SetIfNotNil("DecodeParms", ccitt.MakeDecodeParams())
	return dict
}

// Direct resolves the reference and returns the direct object it points to.
func (ref *PdfObjectReference) Direct() PdfObject { return TraceToDirectObject(ref) }

// NewFlateEncoder makes a new flate encoder with default parameters, predictor 1 and bits per component 8.
func NewFlateEncoder() *FlateEncoder {
	flate := &FlateEncoder{}
	flate.Predictor = 1
	flate.BitsPerComponent = 8
	flate.Colors = 1
	flate.Columns = 1
	return flate
}

func (parser *PdfParser) repairRebuildXrefsTopDown() (*XrefTable, error) {
	parser.repairOnce.Do(func() { parser.repair.xrefs, parser.repair.err = parser.doRepairRebuildXrefsTopDown() })
	return parser.repair.xrefs, parser.repair.err
}

// RevisionEndOffsets returns the exclusive end offset of each revision of the
// document, in ascending order: element i is the offset just past revision
// i's %%EOF marker plus at most one immediately following end-of-line
// sequence. The last element is the end of the final revision, which equals
// Size() unless trailing bytes follow the final %%EOF marker.
//
// The list is best-effort on damaged files: revision ends are located by
// scanning for physical %%EOF markers, so when the markers cannot be fully
// reconciled with the xref-derived revision count (see GetRevisionNumber) the
// result may hold fewer entries than there are revisions. The scan runs once
// per parser and is cached.
func (parser *PdfParser) RevisionEndOffsets() ([]int64, error) {
	nums, _, err := parser.revisionEndOffsets()
	if err != nil {
		return nil, err
	}
	ends := make([]int64, len(nums))
	copy(ends, nums)
	return ends, nil
}

// GetParser returns the parser for lazy-loading or compare references.
func (ref *PdfObjectReference) GetParser() *PdfParser { return ref.parser }

// GetPreviousRevisionParser returns PdfParser for the previous version of the Pdf document.
func (parser *PdfParser) GetPreviousRevisionParser() (*PdfParser, error) {
	if parser.numRevisions == 0 {
		return nil, errors.New("this is first revision")
	}
	if prev, ok := parser.revisionParsers[parser]; ok {
		return prev, nil
	}
	nums, err := parser.sectioningRevisionEnds()
	if err != nil {
		return nil, err
	}
	sectionReader := io.NewSectionReader(parser.reader, 0, nums[parser.numRevisions-1])
	revisionParser, err := parser.newRevisionParser(sectionReader)
	if err != nil {
		return nil, err
	}
	if revisionParser.numRevisions == parser.numRevisions-1 {
		revisionParser.revisionEndsOnce.Do(func() {
			ends := make([]int64, parser.numRevisions)
			copy(ends, nums[:parser.numRevisions])
			revisionParser.revisionEnds = ends
			revisionParser.revisionEndsValid = true
		})
	}
	revisionParser.revisionParsers = parser.revisionParsers
	parser.revisionParsers[parser] = revisionParser
	return revisionParser, nil
}

func (parser *PdfParser) setXrefs(table XrefTable) {
	table.sorted = sortedXrefObjects(table.ObjectMap)
	parser.xrefs.Store(&table)
}

// DecodeBytes decodes a slice of ASCII encoded bytes and returns the result.
func (asciiHex *ASCIIHexEncoder) DecodeBytes(encoded []byte) ([]byte, error) {
	reader := bytes.NewReader(encoded)
	var data []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == '>' {
			break
		}
		if IsWhiteSpace(b) {
			continue
		}
		if (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') || (b >= '0' && b <= '9') {
			data = append(data, b)
		} else {
			common.Log.Debug("ERROR: Invalid ascii hex character (%c)", b)
			return nil, fmt.Errorf("invalid ascii hex character (%c)", b)
		}
	}
	if len(data)%2 == 1 {
		data = append(data, '0')
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Inbound %s", data)
	}
	decodedLen := hex.DecodedLen(len(data))
	if err := asciiHex.limits.check(StreamEncodingFilterNameASCIIHex, int64(decodedLen)); err != nil {
		return nil, err
	}
	decoded := make([]byte, decodedLen)
	_, err := hex.Decode(decoded, data)
	if err != nil {
		return nil, err
	}
	asciiHex.limits.charge(int64(decodedLen))
	return decoded, nil
}

var customStreamEncoders sync.Map

// MakeDecodeParams makes a new instance of an encoding dictionary based on
// the current encoder settings.
func (dct *DCTEncoder) MakeDecodeParams() PdfObject { return nil }

// MakeLazy moves the stream data into a temporary file to reduce memory usage,
// e.g. when creating a PDF with many images.
//
// A lazy stream is single-use: the temporary file is removed once a Write
// that serializes it succeeds, after which writing the stream again returns
// an error. A failed write leaves the stream intact so the document can be
// written again, with the same PdfWriter unless that write copied the
// objects (optimizer, append mode, linearization) or encrypted them, in
// which case the writer requires a new PdfWriter. When writing encrypted,
// the ciphertext is held in memory until the stream is written.
func (stream *PdfObjectStream) MakeLazy() error {
	if stream.Lazy {
		return nil
	}
	temp, err := os.CreateTemp("", "xobject")
	if err != nil {
		return err
	}
	defer temp.Close()
	_, err = temp.Write(stream.Stream)
	if err != nil {
		return err
	}
	stream.Lazy = true
	stream.Stream = nil
	stream.TempFile = temp.Name()
	return nil
}

// CheckAccessRights checks access rights and permissions for a specified password. If either user/owner password is
// specified, full rights are granted, otherwise the access rights are specified by the Permissions flag.
//
// The bool flag indicates that the user can access and view the file.
// The AccessPermissions shows what access the user has for editing etc.
// An error is returned if there was a problem performing the authentication.
func (parser *PdfParser) CheckAccessRights(password []byte) (bool, security.Permissions, error) {
	if parser.crypter == nil {
		return true, security.PermOwner, nil
	}
	return parser.crypter.checkAccessRights(password)
}

func (jbig2 *JBIG2Encoder) encodeImage(img image.Image) ([]byte, error) {
	const procEncodeImage = "encodeImage"
	jbig2Img, err := GoImageToJBIG2(img, JB2ImageAutoThreshold)
	if err != nil {
		return nil, jbig2errors.Wrap(err, procEncodeImage, "convert input image to jbig2 img")
	}
	if err = jbig2.AddPageImage(jbig2Img, &jbig2.DefaultPageSettings); err != nil {
		return nil, jbig2errors.Wrap(err, procEncodeImage, "")
	}
	return jbig2.Encode()
}

// ReleaseBytes returns buf to the pool bucket whose capacity matches cap(buf).
// Buffers whose cap does not match any bucket (including nil and empty
// slices) are dropped silently, so this is safe to call with any []byte.
func ReleaseBytes(buf []byte) {
	c := cap(buf)
	if c == 0 {
		return
	}
	for i, bytePoolSize := range bytePoolSizes {
		if c == bytePoolSize {
			data := buf[:c]
			bytePools[i].Put(&data)
			return
		}
	}
}
func (ccitt *CCITTFaxEncoder) setDecodeLimits(limits decodeLimits) { ccitt.limits = limits }

// NewCompliancePdfParserAt is the explicit-size form of NewCompliancePdfParser.
func NewCompliancePdfParserAt(r io.ReaderAt, size int64) (parser *PdfParser, err error) {
	return newCompliancePdfParser(r, size, new(atomic.Int64))
}

// XrefTable represents the cross references in a PDF, i.e. the table of
// objects and information where to access within the PDF file. Instances are
// treated as immutable snapshots once published via parser.setXrefs — repair
// paths build a new XrefTable rather than mutating an existing one.
type XrefTable struct {
	ObjectMap map[int]XrefObject
	sorted    []XrefObject
}

// EncodeBytes encodes a bytes array and return the encoded value based on the encoder parameters.
func (flate *FlateEncoder) EncodeBytes(data []byte) ([]byte, error) {
	if flate.Predictor != 1 && flate.Predictor != 11 {
		common.Log.Debug("Encoding error: FlateEncoder Predictor = 1, 11 only supported")
		return nil, ErrUnsupportedEncodingParameters
	}
	if flate.Predictor == 11 {
		pngPredictor, err := encodePNGPredictor(data, flate.Colors, flate.BitsPerComponent, flate.Columns)
		if err != nil {
			common.Log.Debug("ERROR: PNG predictor: %v", err)
			return nil, err
		}
		data = pngPredictor
	}
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	writer.Write(data)
	writer.Close()
	return buf.Bytes(), nil
}

func (parser *PdfParser) inspect() (map[string]int, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("--------INSPECT ----------")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Xref table:")
	}
	m := map[string]int{}
	count := 0
	failed := 0
	var nums []int
	for i := range parser.currentXrefs().ObjectMap {
		nums = append(nums, i)
	}
	sort.Ints(nums)
	total := 0
	for _, num := range nums {
		xrefObj := parser.currentXrefs().ObjectMap[num]
		if xrefObj.ObjectNumber == 0 {
			continue
		}
		count++
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("==========")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking up object number: %d", xrefObj.ObjectNumber)
		}
		byNumber, err := parser.LookupByNumber(xrefObj.ObjectNumber)
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("ERROR: Fail to lookup obj %d (%s)", xrefObj.ObjectNumber, err)
			}
			failed++
			continue
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("obj: %s", byNumber)
		}
		indirect, ok := byNumber.(*PdfIndirectObject)
		if ok {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("IND OOBJ %d: %s", xrefObj.ObjectNumber, indirect)
			}
			dict, isDict := indirect.PdfObject.(*PdfObjectDictionary)
			if isDict {
				if typeName, isName := dict.Get("Type").(*PdfObjectName); isName {
					typeStr := string(*typeName)
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("---> Obj type: %s", typeStr)
					}
					_, ok2 := m[typeStr]
					if ok2 {
						m[typeStr]++
					} else {
						m[typeStr] = 1
					}
				} else if subtype, ok2 := dict.Get("Subtype").(*PdfObjectName); ok2 {
					subtypeStr := string(*subtype)
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("---> Obj subtype: %s", subtypeStr)
					}
					_, ok3 := m[subtypeStr]
					if ok3 {
						m[subtypeStr]++
					} else {
						m[subtypeStr] = 1
					}
				}
				if sName, isName := dict.Get("S").(*PdfObjectName); isName && *sName == "JavaScript" {
					_, ok2 := m["JavaScript"]
					if ok2 {
						m["JavaScript"]++
					} else {
						m["JavaScript"] = 1
					}
				}
			}
		} else if stream, isStream := byNumber.(*PdfObjectStream); isStream {
			if typeName, isName := stream.Get("Type").(*PdfObjectName); isName {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("--> Stream object type: %s", *typeName)
				}
				typeStr := string(*typeName)
				m[typeStr]++
			}
		} else {
			dict, isDict := byNumber.(*PdfObjectDictionary)
			if isDict {
				typeName, isName := dict.Get("Type").(*PdfObjectName)
				if isName {
					typeStr := string(*typeName)
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("--- obj type %s", typeStr)
					}
					m[typeStr]++
				}
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("DIRECT OBJ %d: %s", xrefObj.ObjectNumber, byNumber)
			}
		}
		total++
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("--------EOF INSPECT ----------")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("=======")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Object count: %d", count)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Failed lookup: %d", failed)
	}
	for sKey, count2 := range m {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%s: %d", sKey, count2)
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("=======")
	}
	if len(parser.currentXrefs().ObjectMap) < 1 {
		common.Log.Debug("ERROR: This document is invalid (xref table missing!)")
		return nil, fmt.Errorf("invalid document (xref table missing)")
	}
	fontCount, ok := m["Font"]
	if !ok || fontCount < 2 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("This document is probably scanned!")
		}
	} else {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("This document is valid for extraction!")
		}
	}
	return m, nil
}

func (r *limitedDecodeReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if limit := r.limits.clampToTotal(r.streamLimit - r.read); limit < math.MaxInt64 && int64(len(p)) > limit+1 {
		p = p[:limit+1]
	}
	n, err := r.src.Read(p)
	allowed := r.limits.clampToTotal(r.streamLimit - r.read)
	if int64(n) > allowed {
		n = int(allowed)
		r.err = r.limits.exceeded(r.filterName, r.read+allowed)
		err = r.err
	}
	r.read += int64(n)
	r.limits.charge(int64(n))
	return n, err
}

// MakeStreamDict make a new instance of an encoding dictionary for a stream object.
func (ascii85 *ASCII85Encoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(ascii85.GetFilterName()))
	return dict
}

func newPdfParserAt(r io.ReaderAt, size int64, opts *ParserOpts, decodedTotal *atomic.Int64) (*PdfParser, error) {
	parser := &PdfParser{decodedTotal: decodedTotal, reader: r, fileSize: size, xrefOffsets: make([]int64, 0), revisionParsers: make(map[*PdfParser]*PdfParser), Opts: opts}
	if err := parser.initParser(); err != nil {
		return nil, err
	}
	return parser, nil
}

func (parser *PdfParser) checkLinearizedTrailerID() *LinearizationViolation {
	dict, err := parser.FirstXrefTrailer()
	if err != nil || dict == nil {
		return &LinearizationViolation{Code: LinErrTrailerReadFailure, Severity: SeverityError, Message: "unable to read the first xref trailer for /ID comparison"}
	}
	trailer := parser.GetTrailer()
	if trailer == nil {
		return &LinearizationViolation{Code: LinErrTrailerReadFailure, Severity: SeverityError, Message: "missing main trailer for /ID comparison"}
	}
	id := dict.Get("ID")
	obj := trailer.Get("ID")
	if id == nil || obj == nil {
		return &LinearizationViolation{Code: LinErrIDMismatch, Severity: SeverityError, Message: "/ID must be present in both the first xref trailer and the main trailer"}
	}
	if !equalIDObjects(id, obj) {
		return &LinearizationViolation{Code: LinErrIDMismatch, Severity: SeverityError, Message: "/ID in the first xref trailer differs from /ID in the main trailer"}
	}
	return nil
}

// Write outputs the object as it is to be written to file.
func (stream *PdfObjectStream) Write() []byte {
	return withPooledBuffer(func(buf *bytes.Buffer) {
		buf.WriteString(strconv.FormatInt(stream.ObjectNumber, 10))
		buf.WriteString(" 0 R")
	})
}

// DecodeReader returns r unchanged.
func (rawEncoder *RawEncoder) DecodeReader(r io.Reader) io.Reader { return r }

func newFlateEncoderFromStream(streamObj *PdfObjectStream, decodeParams *PdfObjectDictionary) (*FlateEncoder, error) {
	flateEncoder := NewFlateEncoder()
	pdfObjectDictionary := streamObj.PdfObjectDictionary
	if pdfObjectDictionary == nil {
		return flateEncoder, nil
	}
	flateEncoder.image = imageBaseFromDict(pdfObjectDictionary)
	if decodeParams == nil {
		toDirectObject := TraceToDirectObject(pdfObjectDictionary.Get("DecodeParms"))
		switch v := toDirectObject.(type) {
		case *PdfObjectArray:
			if v.Len() != 1 {
				common.Log.Debug("Error: DecodeParms array length != 1 (%d)", v.Len())
				return nil, errors.New("range check error")
			}
			if dict, ok := GetDict(v.Get(0)); ok {
				decodeParams = dict
			}
		case *PdfObjectDictionary:
			decodeParams = v
		case *PdfObjectNull, nil:
		default:
			common.Log.Debug("Error: DecodeParms not a dictionary (%T)", toDirectObject)
			return nil, fmt.Errorf("invalid DecodeParms")
		}
	}
	if decodeParams == nil {
		return flateEncoder, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("decode params: %s", decodeParams.String())
	}
	predictor := decodeParams.Get("Predictor")
	if predictor == nil {
		common.Log.Debug("Error: Predictor missing from DecodeParms - Continue with default (1)")
	} else {
		num, ok := predictor.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("Error: Predictor specified but not numeric (%T)", predictor)
			return nil, fmt.Errorf("invalid Predictor")
		}
		flateEncoder.Predictor = int(*num)
	}
	predictor = decodeParams.Get("BitsPerComponent")
	if predictor != nil {
		num, ok := predictor.(*PdfObjectInteger)
		if !ok {
			common.Log.Debug("ERROR: Invalid BitsPerComponent")
			return nil, fmt.Errorf("invalid BitsPerComponent")
		}
		flateEncoder.BitsPerComponent = int(*num)
	}
	if flateEncoder.Predictor > 1 {
		flateEncoder.Columns = 1
		predictor = decodeParams.Get("Columns")
		if predictor != nil {
			num, ok := predictor.(*PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor column invalid")
			}
			flateEncoder.Columns = int(*num)
		}
		flateEncoder.Colors = 1
		predictor = decodeParams.Get("Colors")
		if predictor != nil {
			num, ok := predictor.(*PdfObjectInteger)
			if !ok {
				return nil, fmt.Errorf("predictor colors not an integer")
			}
			flateEncoder.Colors = int(*num)
		}
	}
	return flateEncoder, nil
}

// MakeStreamDict makes a new instance of an encoding dictionary for a stream object.
// Has the Filter set and the DecodeParms.
func (enc *LZWEncoder) MakeStreamDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName(enc.GetFilterName()))
	decodeParams := enc.MakeDecodeParams()
	if decodeParams != nil {
		dict.Set("DecodeParms", decodeParams)
	}
	dict.Set("EarlyChange", MakeInteger(int64(enc.EarlyChange)))
	return dict
}

func newRunLengthEncoderFromStream(_ *PdfObjectStream, _ *PdfObjectDictionary) (*RunLengthEncoder, error) {
	return NewRunLengthEncoder(), nil
}

// DecodeStream decodes a multi-encoded stream by passing it through the
// DecodeStream method of the underlying encoders.
func (multi *MultiEncoder) DecodeStream(streamObj *PdfObjectStream) ([]byte, error) {
	return multi.DecodeBytes(streamObj.Stream)
}

// Write outputs the object as it is to be written to file.
func (num *PdfObjectFloat) Write() []byte {
	return []byte(strconv.FormatFloat(float64(*num), 'f', -1, 64))
}

// LinearizationViolation describes a single violation of the PDF linearization spec
// (ISO 32000-2 Annex F).
type LinearizationViolation struct {
	Code     LinearizationViolationCode
	Severity Severity
	Message  string
}

// SetFileOffset repositions the cursor and reinitialises the buffer.
func (cursor *readerAtCursor) SetFileOffset(offset int64) {
	if offset < 0 {
		offset = 0
	}
	cursor.reset(offset)
}

// SetIfNotNil sets the dictionary's key -> val mapping entry -IF- val is not nil.
// Note that we take care to perform a type switch.  Otherwise if we would supply a nil value
// of another type, e.g. (PdfObjectArray*)(nil), then it would not be a PdfObject(nil) and thus
// would get set.
func (dict *PdfObjectDictionary) SetIfNotNil(key PdfObjectName, val PdfObject) {
	if val != nil {
		switch v := val.(type) {
		case *PdfObjectName:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectDictionary:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectStream:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectString:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectNull:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectInteger:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectArray:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectBool:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectFloat:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfObjectReference:
			if v != nil {
				dict.Set(key, val)
			}
		case *PdfIndirectObject:
			if v != nil {
				dict.Set(key, val)
			}
		default:
			common.Log.Error("ERROR: Unknown type: %T - should never happen!", val)
		}
	}
}

// String returns a string describing `ind`.
func (ind *PdfIndirectObject) String() string {
	return fmt.Sprintf("IObject:%d", (*ind).ObjectNumber)
}

// GetFilterName returns the name of the encoding filter.
func (ascii85 *ASCII85Encoder) GetFilterName() string { return StreamEncodingFilterNameASCII85 }

func newOffsetReaderAt(source io.ReaderAt, offset int64) *offsetReaderAt {
	return &offsetReaderAt{source: source, offset: offset}
}

// DefaultMaxDecodedStreamSize returns the per-stream decoded size limit set by
// SetDefaultMaxDecodedStreamSize.
func DefaultMaxDecodedStreamSize() int64 { return maxDecodedStreamSize.Load() }

// MakeString creates an PdfObjectString from a string.
// NOTE: PDF does not use utf-8 string encoding like Go so `s` will often not be a utf-8 encoded
// string.
func MakeString(s string) *PdfObjectString { str := PdfObjectString{val: s}; return &str }

func (cursor *readerAtCursor) reset(offset int64) {
	cursor.sectionOffset = offset
	cursor.section = io.NewSectionReader(cursor.source, offset, cursor.size-offset)
	cursor.buffered = bufio.NewReader(cursor.section)
}

// GetFloat returns the *PdfObjectFloat represented by the PdfObject directly or indirectly within an indirect
// object. On type mismatch the found bool flag is false and a nil pointer is returned.
func GetFloat(obj PdfObject) (num *PdfObjectFloat, ok bool) {
	num, ok = TraceToDirectObject(obj).(*PdfObjectFloat)
	return num, ok
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

// GetFilterName returns the name of the encoding filter.
func (dct *DCTEncoder) GetFilterName() string { return StreamEncodingFilterNameDCT }

// FirstXrefTrailer returns the trailer dictionary of the first xref table
// in a linearized file (the one immediately after the linearization
// parameter dictionary). Returns nil if the file is not linearized, and an
// error if the first xref cannot be located or parsed.
//
// This is needed by PDF/A validators that must compare the /ID entry of
// the first xref's trailer against the main xref's trailer (ISO 19005-1
// rule 6.1.3-4).
func (parser *PdfParser) FirstXrefTrailer() (*PdfObjectDictionary, error) {
	if !parser.linearized {
		return nil, nil
	}
	parser.firstTrailerOnce.Do(func() { parser.firstTrailer, parser.firstTrailerErr = parser.parseFirstXrefTrailer() })
	return parser.firstTrailer, parser.firstTrailerErr
}

// NewEncoderFromStream creates a StreamEncoder based on the stream's dictionary.
func NewEncoderFromStream(streamObj *PdfObjectStream) (StreamEncoder, error) {
	toDirectObject := TraceToDirectObject(streamObj.Get("Filter"))
	if toDirectObject == nil {
		return NewRawEncoder(), nil
	}
	if _, ok := toDirectObject.(*PdfObjectNull); ok {
		return NewRawEncoder(), nil
	}
	name, ok := toDirectObject.(*PdfObjectName)
	if !ok {
		arr, isArr := toDirectObject.(*PdfObjectArray)
		if !isArr {
			return nil, fmt.Errorf("filter not a Name or Array object")
		}
		if arr.Len() == 0 {
			return NewRawEncoder(), nil
		}
		if arr.Len() != 1 {
			multiEncoderFromStream, err := newMultiEncoderFromStream(streamObj)
			if err != nil {
				common.Log.Error("Failed creating multi encoder: %v", err)
				return nil, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Multi enc: %s\n", multiEncoderFromStream)
			}
			return multiEncoderFromStream, nil
		}
		toDirectObject = arr.Get(0)
		name, isArr = toDirectObject.(*PdfObjectName)
		if !isArr {
			return nil, fmt.Errorf("filter array member not a Name object")
		}
	}
	if encoder, ok2 := customStreamEncoders.Load(name.String()); ok2 {
		return encoder.(StreamEncoder), nil
	}
	var encoder StreamEncoder
	var err error
	switch *name {
	case StreamEncodingFilterNameFlate:
		encoder, err = newFlateEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameLZW:
		encoder, err = newLZWEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameDCT:
		encoder, err = newDCTEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameRunLength:
		encoder, err = newRunLengthEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameASCIIHex:
		encoder = NewASCIIHexEncoder()
	case StreamEncodingFilterNameASCII85, "A85":
		encoder = NewASCII85Encoder()
	case StreamEncodingFilterNameCCITTFax:
		encoder, err = newCCITTFaxEncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameJBIG2:
		encoder, err = newJBIG2EncoderFromStream(streamObj, nil)
	case StreamEncodingFilterNameJPX:
		encoder = NewJPXEncoder()
	default:
		common.Log.Debug("ERROR: Unsupported encoding method!")
		return nil, fmt.Errorf("unsupported encoding method (%s)", *name)
	}
	if err != nil {
		return nil, err
	}
	applyDecodeLimits(encoder, decodeLimitsFromStream(streamObj))
	return encoder, nil
}

func (crypt *PdfCrypt) encryptBytes(data []byte, filterName string, key []byte) ([]byte, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encrypt bytes")
	}
	filter, ok := crypt.cryptFilters[filterName]
	if !ok {
		return nil, fmt.Errorf("unknown crypt filter (%s)", filterName)
	}
	return filter.EncryptBytes(data, key)
}

const defaultMaxDecodedStreamSize = 256 << 20

// IsPrintable checks if a character is printable.
// Regular characters that are outside the range EXCLAMATION MARK(21h)
// (!) to TILDE (7Eh) (~) should be written using the hexadecimal notation.
func IsPrintable(c byte) bool { return 0x21 <= c && c <= 0x7E }
