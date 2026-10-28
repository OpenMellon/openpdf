package core

import (
	"fmt"
)

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

// SharedObjectEntry is a per-shared-object record. The PDF spec also defines
// optional MD5 signatures and object-count fields; this v1 reader only
// retains the offset/length information needed for layout.
type SharedObjectEntry struct {
	GroupLengthDelta uint32
	SignaturePresent bool
	ObjectsInGroup   uint32
}
