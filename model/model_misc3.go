package model

import (
	"bytes"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	_ "image/gif"
	"io"
	"math"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/model/internal/fonts"
	"github.com/OpenMellon/typesetting/font/opentype"
)

// CompliancePdfReader is a wrapper over PdfReader that is used for verifying if the input Pdf document matches the
// compliance rules of standards like PDF/A.
// NOTE: This implementation is in experimental development state.
//
//	Keep in mind that it might change in the subsequent minor versions.
type CompliancePdfReader struct {
	*PdfReader
	parserMetadata core.ParserMetadata
}

// NewCompliancePdfReader creates a PdfReader that scans the file for the
// extra metadata needed by PDF/A or PDF/X compliance checks. The source's
// byte length is auto-detected; for exotic io.ReaderAt sources use
// NewCompliancePdfReaderAt.
// NOTE: This implementation is in experimental development state and may
// change in subsequent minor versions.
func NewCompliancePdfReader(r io.ReaderAt) (*CompliancePdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewCompliancePdfReaderAt(r, sizeOfReaderAt)
}

func parseByteRange(arr *core.PdfObjectArray, n int64) ([]int64, error) {
	arrLen := arr.Len()
	if arrLen < 2 || arrLen%2 != 0 {
		return nil, fmt.Errorf("invalid ByteRange length %d", arrLen)
	}
	nums := make([]int64, 0, arrLen)
	for i := 0; i < arrLen; i++ {
		n, err := core.GetNumberAsInt64(arr.Get(i))
		if err != nil {
			return nil, fmt.Errorf("invalid ByteRange entry %d: %v", i, err)
		}
		if n < 0 {
			return nil, fmt.Errorf("negative ByteRange entry %d", i)
		}
		nums = append(nums, n)
	}
	for i := 0; i < arrLen; i += 2 {
		if nums[i] > n || nums[i+1] > n-nums[i] {
			return nil, fmt.Errorf("ByteRange pair (%d, %d) outside of file", nums[i], nums[i+1])
		}
	}
	return nums, nil
}

func (c certRevocation) settled() bool {
	return c.status == RevocationStatusGood || c.status == RevocationStatusRevoked && !c.onHold
}

func parseCertificates(cert []*core.PdfObjectStream) []*x509.Certificate {
	var certs []*x509.Certificate
	for _, stream := range decodeStreams(cert) {
		certificate, err := x509.ParseCertificate(stream)
		if err != nil {
			common.Log.Debug("ERROR: skipping unparsable DSS certificate: %v", err)
			continue
		}
		certs = append(certs, certificate)
	}
	return certs
}

func buildXrefStream(plan *xrefStreamPlan) ([]byte, error) {
	var buf bytes.Buffer
	array := core.MakeArray()
	binary.Write(&buf, binary.BigEndian, byte(0))
	binary.Write(&buf, binary.BigEndian, uint32(0))
	binary.Write(&buf, binary.BigEndian, uint16(0xFFFF))
	array.Append(core.MakeInteger(0), core.MakeInteger(1))
	entries := append([]xrefEntry(nil), plan.entries...)
	for i := 1; i < len(entries); i++ {
		entry := entries[i]
		n := i - 1
		for n >= 0 && entries[n].objNum > entry.objNum {
			entries[n+1] = entries[n]
			n--
		}
		entries[n+1] = entry
	}
	for i := 0; i < len(entries); {
		objNum := entries[i].objNum
		n := i + 1
		for n < len(entries) && entries[n].objNum == entries[n-1].objNum+1 {
			n++
		}
		array.Append(core.MakeInteger(objNum), core.MakeInteger(int64(n-i)))
		for j := i; j < n; j++ {
			entry := entries[j]
			binary.Write(&buf, binary.BigEndian, entry.entryType)
			binary.Write(&buf, binary.BigEndian, entry.offset)
			binary.Write(&buf, binary.BigEndian, entry.genOrIndex)
		}
		i = n
	}
	stream, err := core.MakeStream(buf.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return nil, fmt.Errorf("xref stream encode: %w", err)
	}
	stream.ObjectNumber = plan.objNum
	dict := stream.PdfObjectDictionary
	dict.Set("Type", core.MakeName("XRef"))
	dict.Set("W", core.MakeArray(core.MakeInteger(1), core.MakeInteger(4), core.MakeInteger(2)))
	dict.Set("Index", array)
	dict.Set("Size", core.MakeInteger(plan.size))
	if plan.root != nil {
		dict.Set("Root", plan.root)
	}
	if plan.info != nil {
		dict.Set("Info", plan.info)
	}
	if plan.encrypt != nil {
		dict.Set("Encrypt", plan.encrypt)
	}
	if plan.id != nil {
		dict.Set("ID", plan.id)
	}
	if plan.hasPrev {
		dict.Set("Prev", core.MakeInteger(plan.prev))
	}
	var buf2 bytes.Buffer
	fmt.Fprintf(&buf2, "%d 0 obj\n", plan.objNum)
	buf2.Write(dict.Write())
	buf2.WriteString("\nstream\n")
	buf2.Write(stream.Stream)
	buf2.WriteString("\nendstream\nendobj\n")
	return buf2.Bytes(), nil
}

// CharMetrics represents width and height metrics of a glyph.
type CharMetrics = fonts.CharMetrics

type chainRevocation struct {
	status      RevocationStatus
	revokedCert *x509.Certificate
	revokedAt   time.Time
	leaf        certRevocation
	warnings    []string
}

// ParserMetadata gets the parser  metadata.
func (r *CompliancePdfReader) ParserMetadata() core.ParserMetadata {
	if r.parserMetadata == (core.ParserMetadata{}) {
		r.parserMetadata, _ = r.parser.ParserMetadata()
	}
	return r.parserMetadata
}

func cffTable(loader *opentype.Loader) ([]byte, error) {
	mustNewTag := opentype.MustNewTag("CFF ")
	rawTable, err := loader.RawTable(mustNewTag)
	if err != nil {
		return nil, fmt.Errorf("CFF table not found: %w", err)
	}
	return rawTable, nil
}

func buildXrefTable(writer *PdfWriter, nums []int64, table map[int64]int64, n int64, count int64, ok bool, num int64, found bool) ([]byte, int64) {
	var buf bytes.Buffer
	buf.WriteString("xref\r\n")
	size := int64(-1)
	if found {
		buf.WriteString("0 1\r\n")
		size = int64(buf.Len())
		buf.WriteString("0000000000 65535 f\r\n")
	}
	for i := 0; i < len(nums); {
		n := nums[i]
		length := i + 1
		for length < len(nums) && nums[length] == nums[length-1]+1 {
			length++
		}
		length2 := length - i
		fmt.Fprintf(&buf, "%d %d\r\n", n, length2)
		for j := i; j < length; j++ {
			if size < 0 {
				size = int64(buf.Len())
			}
			fmt.Fprintf(&buf, "%010d 00000 n\r\n", table[nums[j]])
		}
		i = length
	}
	if size < 0 {
		size = 0
	}
	buf.WriteString("trailer\r\n")
	writeTrailerDict(&buf, writer, n+1, count, ok)
	fmt.Fprintf(&buf, "\r\nstartxref\r\n%010d\r\n%%%%EOF\r\n", num)
	return buf.Bytes(), size
}

func canStoreInObjectStream(obj core.PdfObject, writer *PdfWriter) bool {
	indirect, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		return false
	}
	if obj == writer.encryptObj {
		return false
	}
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return false
	}
	if _, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
		return false
	}
	if name, _ := dict.Get("Type").(*core.PdfObjectName); name != nil {
		switch string(*name) {
		case "Page", "Pages", "Catalog", "XRef", "ObjStm":
			return false
		}
	}
	return true
}

func clampToRange(floats, floats2 []float64) []float64 {
	if len(floats2) != 2*len(floats) {
		return floats
	}
	for i := range floats {
		floats[i] = math.Min(math.Max(floats[i], floats2[2*i]), floats2[2*i+1])
	}
	return floats
}

// ButtonType represents the subtype of a button field, can be one of:
// - Checkbox (ButtonTypeCheckbox)
// - PushButton (ButtonTypePushButton)
// - RadioButton (ButtonTypeRadioButton)
type ButtonType int

type certRevocation struct {
	status    RevocationStatus
	revokedAt time.Time
	onHold    bool
	ocspFound bool
	crlFound  bool
}

func cmapFromGlyphMap(chars map[rune]fonts.GID) *cmap.CMap {
	if len(chars) == 0 {
		return nil
	}
	textByCode := make(map[cmap.CharCode]string, len(chars))
	for n, char := range chars {
		code := cmap.CharCode(char)
		s := string(n)
		if s2, ok := textByCode[code]; !ok || (ok && len(s2) > 0 && s2 > s) {
			textByCode[code] = s
		}
	}
	return cmap.NewToUnicodeCMap(textByCode)
}

// NewPdfColorspaceFromPdfObject loads a PdfColorspace from a PdfObject.  Returns an error if there is
// a failure in loading.
func NewPdfColorspaceFromPdfObject(obj core.PdfObject) (PdfColorspace, error) {
	if obj == nil {
		return nil, nil
	}
	var indirect *core.PdfIndirectObject
	var name *core.PdfObjectName
	var arr *core.PdfObjectArray
	if indirect2, ok := obj.(*core.PdfIndirectObject); ok {
		indirect = indirect2
	}
	obj = core.TraceToDirectObject(obj)
	switch v := obj.(type) {
	case *core.PdfObjectArray:
		arr = v
	case *core.PdfObjectName:
		name = v
	}
	if name != nil {
		switch *name {
		case "DeviceGray":
			return NewPdfColorspaceDeviceGray(), nil
		case "DeviceRGB":
			return NewPdfColorspaceDeviceRGB(), nil
		case "DeviceCMYK":
			return NewPdfColorspaceDeviceCMYK(), nil
		case "Pattern":
			return NewPdfColorspaceSpecialPattern(), nil
		default:
			common.Log.Debug("ERROR: Unknown colorspace %s", *name)
			return nil, errRangeCheck
		}
	}
	if arr != nil && arr.Len() > 0 {
		var obj core.PdfObject = indirect
		if indirect == nil {
			obj = arr
		}
		if name, ok := core.GetName(arr.Get(0)); ok {
			switch name.String() {
			case "DeviceGray":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceGray(), nil
				}
			case "DeviceRGB":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceRGB(), nil
				}
			case "DeviceCMYK":
				if arr.Len() == 1 {
					return NewPdfColorspaceDeviceCMYK(), nil
				}
			case "CalGray":
				return newPdfColorspaceCalGrayFromPdfObject(obj)
			case "CalRGB":
				return newPdfColorspaceCalRGBFromPdfObject(obj)
			case "Lab":
				return newPdfColorspaceLabFromPdfObject(obj)
			case "ICCBased":
				return newPdfColorspaceICCBasedFromPdfObject(obj)
			case "Pattern":
				return newPdfColorspaceSpecialPatternFromPdfObject(obj)
			case "Indexed":
				return newPdfColorspaceSpecialIndexedFromPdfObject(obj)
			case "Separation":
				return newPdfColorspaceSpecialSeparationFromPdfObject(obj)
			case "DeviceN":
				return newPdfColorspaceDeviceNFromPdfObject(obj)
			default:
				common.Log.Debug("Array with invalid name: %s", *name)
			}
		}
	}
	common.Log.Debug("PDF File Error: Colorspace type error: %s", obj.String())
	return nil, ErrTypeCheck
}

// NewCompliancePdfReaderAt is the explicit-size form of NewCompliancePdfReader.
func NewCompliancePdfReaderAt(r io.ReaderAt, size int64) (*CompliancePdfReader, error) {
	const trackNameNewCompliancePdfReader = "model:NewCompliancePdfReader"
	readerTracked, err := newPdfReaderTracked(r, size, &ReaderOpts{ComplianceMode: true}, false, false, trackNameNewCompliancePdfReader)
	if err != nil {
		return nil, err
	}
	return &CompliancePdfReader{PdfReader: readerTracked}, nil
}

func clampToDomain(xVec, domain []float64) []float64 {
	if len(domain) != 2*len(xVec) {
		return xVec
	}
	floats := make([]float64, len(xVec))
	for i := range xVec {
		floats[i] = math.Min(math.Max(xVec[i], domain[2*i]), domain[2*i+1])
	}
	return floats
}

const (
	ButtonTypeCheckbox ButtonType = iota
	ButtonTypePush     ButtonType = iota
	ButtonTypeRadio    ButtonType = iota
)

// PdfColorspace interface defines the common methods of a PDF colorspace.
// The colorspace defines the data storage format for each color and color representation.
//
// Device based colorspace, specified by name
// - /DeviceGray
// - /DeviceRGB
// - /DeviceCMYK
//
// CIE based colorspace specified by [name, dictionary]
// - [/CalGray dict]
// - [/CalRGB dict]
// - [/Lab dict]
// - [/ICCBased dict]
//
// Special colorspaces
// - /Pattern
// - /Indexed
// - /Separation
// - /DeviceN
//
// Work is in progress to support all colorspaces. At the moment ICCBased color spaces fall back to the alternate
// colorspace which works OK in most cases. For full color support, will need fully featured ICC support.
type PdfColorspace interface {
	// String returns the PdfColorspace's name.
	String() string

	// ImageToRGB converts an Image in a given PdfColorspace to an RGB image.
	ImageToRGB(Image) (Image, error)

	// ColorToRGB converts a single color in a given PdfColorspace to an RGB color.
	ColorToRGB(col PdfColor) (PdfColor, error)

	// GetNumComponents returns the number of components in the PdfColorspace.
	GetNumComponents() int

	// ToPdfObject returns a PdfObject representation of the PdfColorspace.
	ToPdfObject() core.PdfObject

	// ColorFromPdfObjects returns a PdfColor in the given PdfColorspace from an array of PdfObject where each
	// PdfObject represents a numeric value.
	ColorFromPdfObjects(objs []core.PdfObject) (PdfColor, error)

	// ColorFromFloats returns a new PdfColor based on input color components for a given PdfColorspace.
	ColorFromFloats(floats []float64) (PdfColor, error)

	// DecodeArray returns the Decode array for the PdfColorSpace, i.e. the range of each component.
	DecodeArray() []float64
}
