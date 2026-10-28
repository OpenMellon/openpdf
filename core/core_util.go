package core

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/jbig2"
	"github.com/OpenMellon/openpdf/internal/jbig2/document"
	"github.com/OpenMellon/openpdf/internal/jbig2/document/segments"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/OpenMellon/openpdf/internal/lazystream"
	"github.com/OpenMellon/openpdf/internal/precision"
	"golang.org/x/text/unicode/bidi"
	"golang.org/x/xerrors"
)

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

func init() { maxDecodedStreamSize.Store(defaultMaxDecodedStreamSize) }

const intBits = 32 << (^uint(0) >> 63)

const maxCCITTColumns = 65535

var maxDecodedStreamSize atomic.Int64

const maxLinearizationParamDictEnd = 1024

const (
	maxLinearizationPages   = 1 << 20
	maxSharedObjectsTotal   = 1 << 20
	maxSharedObjectsPerPage = 1 << 16
)

const maxTraceDepth = 10

const maxObjectNestingDepth = 500

var ErrPasswordRequired = errors.New("document is encrypted with the standard security handler; a password is required")

var ErrCertificateRequired = errors.New("document is encrypted with a public-key security handler; a recipient certificate and private key are required")

var errHintShortRead = errors.New("hint stream: short read")

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

var errObjectNestingTooDeep = errors.New("arrays and dictionaries nested too deeply")

var errEOFNotFound = errors.New("EOF not found")

const stdCryptFilterName = "StdCF"

const defaultCryptFilterKey = "DefaultCryptFilter"

const (
	DefaultJPEGQuality = 75
)

// DefaultMaxDecodedStreamSize returns the per-stream decoded size limit set by
// SetDefaultMaxDecodedStreamSize.
func DefaultMaxDecodedStreamSize() int64 { return maxDecodedStreamSize.Load() }

const defaultMaxDecodedStreamSize = 256 << 20

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

const adobePubSecFilter = "Adobe.PubSec"

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

// RegisterCustomStreamEncoder register a custom encoder handler for certain filter.
func RegisterCustomStreamEncoder(filterName string, customStreamEncoder StreamEncoder) {
	customStreamEncoders.Store(filterName, customStreamEncoder)
}

var customStreamEncoders sync.Map

func acquireZlibReader(r io.Reader) (io.ReadCloser, error) {
	rc := zlibReaderPool.Get().(io.ReadCloser)
	if err := rc.(zlib.Resetter).Reset(r, nil); err != nil {
		zlibReaderPool.Put(rc)
		return nil, err
	}
	return rc, nil
}

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

func acquireBuffer() *bytes.Buffer { return bufferPool.Get().(*bytes.Buffer) }

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

var zlibReaderPool = sync.Pool{New: func() any {
	reader, _ := zlib.NewReader(bytes.NewReader([]byte{0x78, 0x9c, 0x01, 0x00, 0x00, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01}))
	return reader
}}

const (
	predictorNone    = 0
	predictorSub     = 1
	predictorUp      = 2
	predictorAverage = 3
	predictorPaeth   = 4
)

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

func absInt(x int) int { mask := x >> (intBits - 1); return (x ^ mask) - mask }

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

func scaleDecodeValue(v uint, dmin, dmax float64) float64 {
	f := precision.RoundFloat((float64(v) / 255.0), 6)
	delta := precision.RoundFloat((dmax-dmin)*f, 6)
	return precision.RoundFloat((dmin+delta)*255.0, 6)
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

// IsPrintable checks if a character is printable.
// Regular characters that are outside the range EXCLAMATION MARK(21h)
// (!) to TILDE (7Eh) (~) should be written using the hexadecimal notation.
func IsPrintable(c byte) bool { return 0x21 <= c && c <= 0x7E }

// IsNullObject returns true if `obj` is a PdfObjectNull.
func IsNullObject(obj PdfObject) bool {
	_, ok := TraceToDirectObject(obj).(*PdfObjectNull)
	return ok
}

// IsFloatDigit checks if a character can be a part of a float number string.
func IsFloatDigit(c byte) bool { return ('0' <= c && c <= '9') || c == '.' }

// IsDecimalDigit checks if the character is a part of a decimal number string.
func IsDecimalDigit(c byte) bool { return '0' <= c && c <= '9' }

// IsOctalDigit checks if a character can be part of an octal digit string.
func IsOctalDigit(c byte) bool { return '0' <= c && c <= '7' }

// IsWhiteSpace checks if byte represents a white space character.
func IsWhiteSpace(ch byte) bool {
	if (ch == 0x00) || (ch == 0x09) || (ch == 0x0A) || (ch == 0x0C) || (ch == 0x0D) || (ch == 0x20) {
		return true
	}
	return false
}

// IsDelimiter checks if a character represents a delimiter.
func IsDelimiter(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}

func isPoolSize(n int) bool {
	for _, bytePoolSize := range bytePoolSizes {
		if n == bytePoolSize {
			return true
		}
	}
	return false
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

const JB2ImageAutoThreshold = -1.0

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

var firstPageSkipKeys = []string{"Parent", "Thumb", "B"}
