// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package ccittfax

import (
	"errors"
)

func (n *treeNode) walk(bit bool) *treeNode {
	if bit {
		return n.one
	}
	return n.zero
}

type code struct {
	Code        uint16
	BitsWritten int
}
type DecodeOptions struct {
	Columns                int
	Rows                   int
	K                      int
	EncodedByteAligned     bool
	BlackIsOne             bool
	EndOfBlock             bool
	EndOfLine              bool
	DamagedRowsBeforeError int
}

func (d *Decoder) getNextChangingElement(col int, white bool) int {
	offset := 0
	if !white {
		offset = 1
	}
	idx := int(uint32(d.lastChangeIdx)&0xFFFFFFFE) + offset
	if idx > 2 {
		idx -= 2
	}
	if col == 0 {
		return idx
	}
	for i := idx; i < d.refCount; i += 2 {
		if col < d.refChanges[i] {
			d.lastChangeIdx = i
			return i
		}
	}
	return -1
}

func (t *tree) fill(length, bits, value int) error {
	cur := t.root
	for i := 0; i < length; i++ {
		shift := length - 1 - i
		bit := ((bits >> uint(shift)) & 1) != 0
		next := cur.walk(bit)
		if next != nil {
			if next.isLeaf {
				return errors.New("node is leaf, no other following")
			}
			cur = next
			continue
		}
		next = &treeNode{}
		if i == length-1 {
			next.value = value
			next.isLeaf = true
		}
		if bits == 0 {
			next.isZeroCode = true
		}
		cur.set(bit, next)
		cur = next
	}
	return nil
}

func rtcG31D(bitPos int) ([]byte, int) {
	var out []byte
	for i := 0; i < 6; i++ {
		out, bitPos = appendBits(out, bitPos, eolCode)
	}
	return out, bitPos % 8
}
