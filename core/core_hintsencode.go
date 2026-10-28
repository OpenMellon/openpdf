package core

import (
	"bytes"
	"fmt"
)

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
