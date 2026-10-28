package core

import (
	"fmt"
)

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
