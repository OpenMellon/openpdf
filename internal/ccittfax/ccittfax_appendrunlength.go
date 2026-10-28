// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package ccittfax

import (
	"errors"
	"io"
	"math"
)

func appendRunLength(buf []byte, bitPos int, length int, white bool) ([]byte, int) {
	var (
		c    code
		done bool
	)
	for !done {
		c, length, done = runCode(length, white)
		buf, bitPos = appendBits(buf, bitPos, c)
	}
	return buf, bitPos
}

var blackCodeBits = [...][]uint16{{0x2, 0x3}, {0x2, 0x3}, {0x2, 0x3}, {0x3}, {0x4, 0x5}, {0x4, 0x5, 0x7}, {0x4, 0x7}, {0x18}, {0x17, 0x18, 0x37, 0x8, 0xf}, {0x17, 0x18, 0x28, 0x37, 0x67, 0x68, 0x6c, 0x8, 0xc, 0xd}, {0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x1c, 0x1d, 0x1e, 0x1f, 0x24, 0x27, 0x28, 0x2b, 0x2c, 0x33, 0x34, 0x35, 0x37, 0x38, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0xc8, 0xc9, 0xca, 0xcb, 0xcc, 0xcd, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xda, 0xdb}, {0x4a, 0x4b, 0x4c, 0x4d, 0x52, 0x53, 0x54, 0x55, 0x5a, 0x5b, 0x64, 0x65, 0x6c, 0x6d, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77}}

func (d *Decoder) decodeG32D() error {
	d.refCount = d.curCount
	d.curChanges, d.refChanges = d.refChanges, d.curChanges
	white := true
	var (
		bit bool
		col int
		err error
	)
	d.curCount = 0
nextCode:
	for col < d.columns {
		node := modeTree.root
		for {
			bit, err = d.reader.ReadBool()
			if err != nil {
				return err
			}
			node = node.walk(bit)
			if node == nil {
				continue nextCode
			}
			if !node.isLeaf {
				continue
			}
			switch node.value {
			case horizontalModeValue:
				var runLen int
				if white {
					runLen, err = d.decodeRun(whiteTree)
				} else {
					runLen, err = d.decodeRun(blackTree)
				}
				if err != nil {
					return err
				}
				col += runLen
				d.curChanges[d.curCount] = col
				d.curCount++
				if white {
					runLen, err = d.decodeRun(blackTree)
				} else {
					runLen, err = d.decodeRun(whiteTree)
				}
				if err != nil {
					return err
				}
				col += runLen
				d.curChanges[d.curCount] = col
				d.curCount++
			case passModeValue:
				idx := d.getNextChangingElement(col, white) + 1
				if idx >= d.refCount {
					col = d.columns
				} else {
					col = d.refChanges[idx]
				}
			default:
				idx := d.getNextChangingElement(col, white)
				if idx >= d.refCount || idx == -1 {
					col = d.columns + node.value
				} else {
					col = d.refChanges[idx] + node.value
				}
				d.curChanges[d.curCount] = col
				d.curCount++
				white = !white
			}
			continue nextCode
		}
	}
	return nil
}

type (
	tree    struct{ root *treeNode }
	Encoder struct {
		K                      int
		EndOfLine              bool
		EncodedByteAlign       bool
		Columns                int
		Rows                   int
		EndOfBlock             bool
		BlackIs1               bool
		DamagedRowsBeforeError int
	}
)

func (e *Encoder) encodeG4(pixels [][]byte) []byte {
	rows := make([][]byte, len(pixels))
	copy(rows, pixels)
	rows = prependWhiteRow(rows)
	var out []byte
	var bitPos int
	for i := 1; i < len(rows); i++ {
		if e.Rows > 0 && !e.EndOfBlock && i == (e.Rows+1) {
			break
		}
		var rowBuf []byte
		var a1, b1, b2 int
		rowBit := bitPos
		a0 := -1
		for a0 < len(rows[i]) {
			a1 = nextChange(rows[i], a0)
			b1 = findB1(rows[i], rows[i-1], a0)
			b2 = nextChange(rows[i-1], b1)
			if b2 < a1 {
				rowBuf, rowBit = appendBits(rowBuf, rowBit, passCode)
				a0 = b2
			} else {
				if math.Abs(float64(b1-a1)) > 3 {
					rowBuf, rowBit, a0 = appendHorizontalMode(rows[i], rowBuf, rowBit, a0, a1)
				} else {
					rowBuf, rowBit = appendVerticalCode(rowBuf, rowBit, a1, b1)
					a0 = a1
				}
			}
		}
		out = e.appendEncodedRow(out, rowBuf, bitPos)
		if e.EncodedByteAlign {
			rowBit = 0
		}
		bitPos = rowBit % 8
	}
	if e.EndOfBlock {
		eofb, _ := eofbG4(bitPos)
		out = e.appendEncodedRow(out, eofb, bitPos)
	}
	return out
}

func findB1Colored(cur, ref []byte, a0 int, white bool) int {
	b1 := nextChange(ref, a0)
	if b1 < len(ref) && (a0 == -1 && ref[b1] == whitePixel || a0 >= 0 && a0 < len(cur) && cur[a0] == ref[b1] || a0 >= len(cur) && white && ref[b1] == whitePixel || a0 >= len(cur) && !white && ref[b1] == blackPixel) {
		b1 = nextChange(ref, b1)
	}
	return b1
}

func runCode(length int, white bool) (code, int, bool) {
	if length < 64 {
		if white {
			return whiteTermCodes[length], 0, true
		}
		return blackTermCodes[length], 0, true
	}
	n := length / 64
	if n > 40 {
		return commonMakeupCodes[2560], length - 2560, false
	}
	if n > 27 {
		return commonMakeupCodes[n*64], length - n*64, false
	}
	if white {
		return whiteMakeupCodes[n*64], length - n*64, false
	}
	return blackMakeupCodes[n*64], length - n*64, false
}

func prependWhiteRow(rows [][]byte) [][]byte {
	blank := make([]byte, len(rows[0]))
	for i := range blank {
		blank[i] = whitePixel
	}
	rows = append(rows, []byte{})
	for i := len(rows) - 1; i > 0; i-- {
		rows[i] = rows[i-1]
	}
	rows[0] = blank
	return rows
}

func (d *Decoder) Read(in []byte) (int, error) {
	if d.err != nil {
		return 0, d.err
	}
	remaining := len(in)
	var (
		n      int
		outPos int
	)
	for remaining != 0 {
		if d.readPos >= d.rowBytes {
			if err := d.fetch(); err != nil {
				d.err = err
				return 0, err
			}
		}
		if d.rowBytes == -1 {
			return n, io.EOF
		}
		switch {
		case remaining <= d.rowBytes-d.readPos:
			chunk := d.rowBuf[d.readPos : d.readPos+remaining]
			for _, b := range chunk {
				if !d.blackIsOne {
					b = ^b
				}
				in[outPos] = b
				outPos++
			}
			n += len(chunk)
			d.readPos += len(chunk)
			return n, nil
		default:
			rest := d.rowBuf[d.readPos:]
			for _, b := range rest {
				if !d.blackIsOne {
					b = ^b
				}
				in[outPos] = b
				outPos++
			}
			n += len(rest)
			d.readPos += len(rest)
			remaining -= len(rest)
		}
	}
	return n, nil
}

func (d *Decoder) decodeRowType6() error {
	if d.byteAligned {
		d.reader.Align()
	}
	if d.endOfBlock {
		d.reader.Mark()
		foundEOL, err := d.tryFetchEOL()
		if err != nil {
			return err
		}
		if foundEOL {
			foundEOL, err = d.tryFetchEOL()
			if err != nil {
				return err
			}
			if foundEOL {
				return io.EOF
			}
		}
		d.reader.Reset()
	}
	return d.decode2D()
}

func verticalModeCode(a1, b1 int) code {
	var c code
	switch b1 - a1 {
	case -1:
		c = vertRight1Code
	case -2:
		c = vertRight2Code
	case -3:
		c = vertRight3Code
	case 0:
		c = vertical0Code
	case 1:
		c = vertLeft1Code
	case 2:
		c = vertLeft2Code
	case 3:
		c = vertLeft3Code
	}
	return c
}

func (t tiffType) String() string {
	switch t {
	case tiffTypeModifiedHuffmanRle:
		return "tiffTypeModifiedHuffmanRle"
	case tiffTypeT4:
		return "tiffTypeT4"
	case tiffTypeT6:
		return "tiffTypeT6"
	default:
		return "undefined"
	}
}

func encodeRow1D(row []byte, bitPos int, tag code) ([]byte, int) {
	white := true
	var out []byte
	out, bitPos = appendBits(nil, bitPos, tag)
	pos := 0
	var run int
	for pos < len(row) {
		run, pos = countRun(row, white, pos)
		out, bitPos = appendRunLength(out, bitPos, run, white)
		white = !white
	}
	return out, bitPos % 8
}

func eofbG4(bitPos int) ([]byte, int) {
	var out []byte
	for i := 0; i < 2; i++ {
		out, bitPos = appendBits(out, bitPos, eolCode)
	}
	return out, bitPos % 8
}

func (d *Decoder) fetch() error {
	if d.rowBytes == -1 {
		return nil
	}
	if d.readPos < d.rowBytes {
		return nil
	}
	d.rowBytes = 0
	err := d.decodeRow()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return err
		}
		if d.rowBytes != 0 {
			return err
		}
		d.rowBytes = -1
	}
	d.readPos = 0
	return nil
}

func (d *Decoder) decode2D() error {
	d.refCount = d.curCount
	d.curChanges, d.refChanges = d.refChanges, d.curChanges
	white := true
	var (
		bit bool
		col int
		err error
	)
	d.curCount = 0
nextCode:
	for col < d.columns {
		node := modeTree.root
		for {
			bit, err = d.reader.ReadBool()
			if err != nil {
				return err
			}
			node = node.walk(bit)
			if node == nil {
				continue nextCode
			}
			if !node.isLeaf {
				continue
			}
			switch node.value {
			case horizontalModeValue:
				var runLen int
				if white {
					runLen, err = d.decodeRun(whiteTree)
				} else {
					runLen, err = d.decodeRun(blackTree)
				}
				if err != nil {
					return err
				}
				col += runLen
				d.curChanges[d.curCount] = col
				d.curCount++
				if white {
					runLen, err = d.decodeRun(blackTree)
				} else {
					runLen, err = d.decodeRun(whiteTree)
				}
				if err != nil {
					return err
				}
				col += runLen
				d.curChanges[d.curCount] = col
				d.curCount++
			case passModeValue:
				idx := d.getNextChangingElement(col, white) + 1
				if idx >= d.refCount {
					col = d.columns
				} else {
					col = d.refChanges[idx]
				}
			default:
				idx := d.getNextChangingElement(col, white)
				if idx >= d.refCount || idx == -1 {
					col = d.columns + node.value
				} else {
					col = d.refChanges[idx] + node.value
				}
				d.curChanges[d.curCount] = col
				d.curCount++
				white = !white
			}
			continue nextCode
		}
	}
	return nil
}

type treeNode struct {
	zero       *treeNode
	one        *treeNode
	value      int
	isZeroCode bool
	isLeaf     bool
}

func rtcG32D(bitPos int) ([]byte, int) {
	var out []byte
	for i := 0; i < 6; i++ {
		out, bitPos = appendBits(out, bitPos, eolTag2DCode)
	}
	return out, bitPos % 8
}

func appendBits(buf []byte, bitPos int, c code) ([]byte, int) {
	written := 0
	for written < c.BitsWritten {
		byteIdx := bitPos / 8
		bitOff := bitPos % 8
		if byteIdx >= len(buf) {
			buf = append(buf, 0)
		}
		n := 8 - bitOff
		remaining := c.BitsWritten - written
		if n > remaining {
			n = remaining
		}
		if written < 8 {
			buf[byteIdx] = buf[byteIdx] | byte(c.Code>>uint(8+bitOff-written))&bitMasks[8-n-bitOff]
		} else {
			buf[byteIdx] = buf[byteIdx] | (byte(c.Code<<uint(written-8))&bitMasks[8-n])>>uint(bitOff)
		}
		bitPos += n
		written += n
	}
	return buf, bitPos
}

func (n *treeNode) set(bit bool, child *treeNode) {
	if !bit {
		n.zero = child
	} else {
		n.one = child
	}
}

var blackRunLengths = [...][]uint16{{3, 2}, {1, 4}, {6, 5}, {7}, {9, 8}, {10, 11, 12}, {13, 14}, {15}, {16, 17, 0, 18, 64}, {24, 25, 23, 22, 19, 20, 21, 1792, 1856, 1920}, {1984, 2048, 2112, 2176, 2240, 2304, 2368, 2432, 2496, 2560, 52, 55, 56, 59, 60, 320, 384, 448, 53, 54, 50, 51, 44, 45, 46, 47, 57, 58, 61, 256, 48, 49, 62, 63, 30, 31, 32, 33, 40, 41, 128, 192, 26, 27, 28, 29, 34, 35, 36, 37, 38, 39, 42, 43}, {640, 704, 768, 832, 1280, 1344, 1408, 1472, 1536, 1600, 1664, 1728, 512, 576, 896, 960, 1024, 1088, 1152, 1216}}

func (d *Decoder) tryFetchEOL1() (bool, error) {
	bits, err := d.reader.ReadBits(13)
	if err != nil {
		return false, err
	}
	return bits == 0x3, nil
}

func appendHorizontalMode(cur, buf []byte, bitPos, a0, a1 int) ([]byte, int, int) {
	a2 := nextChange(cur, a1)
	white := a0 >= 0 && cur[a0] == whitePixel || a0 == -1
	buf, bitPos = appendBits(buf, bitPos, horizontalCode)
	var run0 int
	if a0 > -1 {
		run0 = a1 - a0
	} else {
		run0 = a1 - a0 - 1
	}
	buf, bitPos = appendRunLength(buf, bitPos, run0, white)
	white = !white
	run1 := a2 - a1
	buf, bitPos = appendRunLength(buf, bitPos, run1, white)
	a0 = a2
	return buf, bitPos, a0
}
