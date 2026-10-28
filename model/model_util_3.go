package model

import (
	"encoding/asn1"
	"fmt"
	_ "image/gif"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func fillSignatureCoverage(parser *core.PdfParser, nums []int64, contents *core.PdfObjectString, result *SignatureValidationResult) {
	result.byteRangeChecked = true
	n := nums[len(nums)-2] + nums[len(nums)-1]
	result.IsCoveringEntireFile = n == parser.Size()
	result.ContentsGapOnly = isContentsGapOnly(parser, nums, contents)
	revisionEndOffsets, err := parser.RevisionEndOffsets()
	if err != nil || len(revisionEndOffsets) == 0 {
		result.IsCoveringEntireRevision = result.IsCoveringEntireFile
		return
	}
	search := sort.Search(len(revisionEndOffsets), func(count int) bool { return revisionEndOffsets[count] >= n })
	if search == len(revisionEndOffsets) {
		result.IsCoveringEntireRevision = result.IsCoveringEntireFile || isWhitespaceGap(parser, revisionEndOffsets[len(revisionEndOffsets)-1], n)
		return
	}
	if n == revisionEndOffsets[search] {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - 1 - search
		return
	}
	if isWhitespaceGap(parser, n, revisionEndOffsets[search]) {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - 1 - search
		return
	}
	if search > 0 && isWhitespaceGap(parser, revisionEndOffsets[search-1], n) {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - search
	}
}

func libraryVersion() string { return common.Version }

func inheritReaderOpts(reader *PdfReader, opts *ReaderOpts) *ReaderOpts {
	if reader.parser == nil || reader.parser.Opts == nil {
		return opts
	}
	readerOpts := NewReaderOpts()
	if opts != nil {
		*readerOpts = *opts
	}
	if readerOpts.MaxDecodedStreamSize == 0 {
		readerOpts.MaxDecodedStreamSize = reader.parser.Opts.MaxDecodedStreamSize
	}
	if readerOpts.MaxDecodedTotalSize == 0 {
		readerOpts.MaxDecodedTotalSize = reader.parser.Opts.MaxDecodedTotalSize
	}
	return readerOpts
}

func iccProfileNumComponents(data []byte) int {
	if len(data) < 128 {
		return 0
	}
	switch string(data[16:20]) {
	case "GRAY":
		return 1
	case "RGB ":
		return 3
	case "CMYK":
		return 4
	default:
		return 0
	}
}

var _ pdfFont = (*pdfFontSimple)(nil)

var _ pdfFont = (*pdfFontType0)(nil)

var _ pdfFont = (*pdfFontType3)(nil)

var _ pdfFont = (*pdfCIDFontType0)(nil)

var _ pdfFont = (*pdfCIDFontType2)(nil)

const maxGapBytes = 2

const maxBitsPerComponent = 16

const maxCRLAge = 24 * time.Hour

const maxLinearizationIterations = 16

const (
	maxFunctionInputs = 8
	maxSampledInputs  = 8
)

func matchesAuthorityCertIssuer(data, rawIssuer []byte) bool {
	var rawValues []asn1.RawValue
	if data2, err := asn1.Unmarshal(data, &rawValues); err != nil || len(data2) > 0 {
		return false
	}
	for _, rawValue := range rawValues {
		if rawValue.Class == asn1.ClassContextSpecific && rawValue.Tag == 4 && equalDistinguishedNames(rawValue.Bytes, rawIssuer) {
			return true
		}
	}
	return false
}

func mergeFontDescriptor(fontDescriptor, std14Descriptor *PdfFontDescriptor) {
	if fontDescriptor == nil || std14Descriptor == nil {
		return
	}
	for _, st := range []struct{ dst, src *core.PdfObject }{{&fontDescriptor.FontName, &std14Descriptor.FontName}, {&fontDescriptor.FontFamily, &std14Descriptor.FontFamily}, {&fontDescriptor.FontWeight, &std14Descriptor.FontWeight}, {&fontDescriptor.Flags, &std14Descriptor.Flags}, {&fontDescriptor.FontBBox, &std14Descriptor.FontBBox}, {&fontDescriptor.ItalicAngle, &std14Descriptor.ItalicAngle}, {&fontDescriptor.Ascent, &std14Descriptor.Ascent}, {&fontDescriptor.Descent, &std14Descriptor.Descent}, {&fontDescriptor.CapHeight, &std14Descriptor.CapHeight}, {&fontDescriptor.XHeight, &std14Descriptor.XHeight}, {&fontDescriptor.StemV, &std14Descriptor.StemV}, {&fontDescriptor.StemH, &std14Descriptor.StemH}} {
		if *st.dst == nil {
			*st.dst = *st.src
		}
	}
}

func measureXrefTable(writer *PdfWriter, nums []int64, n int64, count int64, ok bool, found bool) int {
	xrefTable, _ := buildXrefTable(writer, nums, map[int64]int64{}, n, count, ok, 0, found)
	return len(xrefTable)
}

var (
	metadataMu      sync.Mutex
	pdfAuthor       = ""
	pdfCreationDate time.Time
	pdfCreator      = ""
	pdfKeywords     = ""
	pdfModifiedDate time.Time
	pdfProducer     = ""
	pdfSubject      = ""
	pdfTitle        = ""
)

func padBytes(data []byte, n int) []byte {
	if len(data) >= n {
		return data
	}
	data2 := make([]byte, n)
	count := copy(data2, data)
	for i := count; i < n; i++ {
		data2[i] = ' '
	}
	return data2
}

var newlineRegexp = regexp.MustCompile("[\\n\\r]+")

var (
	oidAuthorityKeyIdentifier = asn1.ObjectIdentifier{2, 5, 29, 35}
	oidCRLNumber              = asn1.ObjectIdentifier{2, 5, 29, 20}
	oidCRLReason              = asn1.ObjectIdentifier{2, 5, 29, 21}
	oidInvalidityDate         = asn1.ObjectIdentifier{2, 5, 29, 24}
)

var (
	oidDeltaCRLIndicator        = asn1.ObjectIdentifier{2, 5, 29, 27}
	oidIssuingDistributionPoint = asn1.ObjectIdentifier{2, 5, 29, 28}
	oidCertificateIssuer        = asn1.ObjectIdentifier{2, 5, 29, 29}
)

const noTrustRootsMessage = "no trust roots configured"

func pdfObjectToString(obj core.PdfObject) (string, error) {
	obj = core.TraceToDirectObject(obj)
	switch v := obj.(type) {
	case *core.PdfObjectString:
		return v.Str(), nil
	case *core.PdfObjectStream:
		stream, err := core.DecodeStream(v)
		if err != nil {
			return "", err
		}
		return string(stream), nil
	}
	return "", fmt.Errorf("invalid content stream object holder (%T)", obj)
}

func partitionObjectNumbers(writer *PdfWriter, layout *linearizationLayout, objByNum map[int64]core.PdfObject) (nums, nums2 []int64) {
	for _, mainSection := range layout.mainSection {
		obj := objByNum[mainSection]
		if obj == nil {
			nums2 = append(nums2, mainSection)
			continue
		}
		if canStoreInObjectStream(obj, writer) {
			nums = append(nums, mainSection)
		} else {
			nums2 = append(nums2, mainSection)
		}
	}
	return nums, nums2
}
