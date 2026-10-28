package core

import (
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
)

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

// HintRange is a (file offset, length) pair from the linearization
// dictionary's /H array describing where the hint stream(s) live.
type HintRange struct {
	Offset int64
	Length int64
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
func absInt(x int) int { mask := x >> (intBits - 1); return (x ^ mask) - mask }

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

// SharedObjectEntry is a per-shared-object record. The PDF spec also defines
// optional MD5 signatures and object-count fields; this v1 reader only
// retains the offset/length information needed for layout.
type SharedObjectEntry struct {
	GroupLengthDelta uint32
	SignaturePresent bool
	ObjectsInGroup   uint32
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
