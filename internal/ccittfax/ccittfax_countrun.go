// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package ccittfax

import (
	"errors"
	"io"
	"math"

	"github.com/OpenMellon/openpdf/internal/bitwise"
)

func countRun(row []byte, white bool, pos int) (int, int) {
	n := 0
	for pos < len(row) {
		if white {
			if row[pos] != whitePixel {
				break
			}
		} else {
			if row[pos] != blackPixel {
				break
			}
		}
		n++
		pos++
	}
	return n, pos
}

var whiteCodeBits = [...][]uint16{{0x7, 0x8, 0xb, 0xc, 0xe, 0xf}, {0x12, 0x13, 0x14, 0x1b, 0x7, 0x8}, {0x17, 0x18, 0x2a, 0x2b, 0x3, 0x34, 0x35, 0x7, 0x8}, {0x13, 0x17, 0x18, 0x24, 0x27, 0x28, 0x2b, 0x3, 0x37, 0x4, 0x8, 0xc}, {0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x1a, 0x1b, 0x2, 0x24, 0x25, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x3, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x4, 0x4a, 0x4b, 0x5, 0x52, 0x53, 0x54, 0x55, 0x58, 0x59, 0x5a, 0x5b, 0x64, 0x65, 0x67, 0x68, 0xa, 0xb}, {0x98, 0x99, 0x9a, 0x9b, 0xcc, 0xcd, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xdb}, {}, {0x8, 0xc, 0xd}, {0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x1c, 0x1d, 0x1e, 0x1f}}

func (d *Decoder) decodeRow() (err error) {
	if !d.endOfBlock && d.rows > 0 && d.rows == d.decodedRows {
		return io.EOF
	}
	switch d.decoderType {
	case tiffTypeModifiedHuffmanRle:
		err = d.decodeRowType2()
	case tiffTypeT4:
		err = d.decodeRowType4()
	case tiffTypeT6:
		err = d.decodeRowType6()
	}
	if err != nil {
		return err
	}
	col := 0
	white := true
	d.lastChangeIdx = 0
	for i := 0; i < d.curCount; i++ {
		runEnd := d.columns
		if i != d.curCount {
			runEnd = d.curChanges[i]
		}
		if runEnd > d.columns {
			runEnd = d.columns
		}
		byteIdx := col / 8
		for col%8 != 0 && runEnd-col > 0 {
			var bit byte
			if !white {
				bit = 1 << uint(7-(col%8))
			}
			d.rowBuf[byteIdx] |= bit
			col++
		}
		if col%8 == 0 {
			byteIdx = col / 8
			var fill byte
			if !white {
				fill = 0xff
			}
			for runEnd-col > 7 {
				d.rowBuf[byteIdx] = fill
				col += 8
				byteIdx++
			}
		}
		for runEnd-col > 0 {
			if col%8 == 0 {
				d.rowBuf[byteIdx] = 0
			}
			var bit byte
			if !white {
				bit = 1 << uint(7-(col%8))
			}
			d.rowBuf[byteIdx] |= bit
			col++
		}
		white = !white
	}
	if col != d.columns {
		return errors.New("sum of run-lengths does not equal scan line width")
	}
	d.rowBytes = (col + 7) / 8
	d.decodedRows++
	return nil
}

func (e *Encoder) encodeG32D(rows [][]byte) []byte {
	var out []byte
	var bitPos int
	for i := 0; i < len(rows); i += e.K {
		if e.Rows > 0 && !e.EndOfBlock && i == e.Rows {
			break
		}
		line1D, endBit := encodeRow1D(rows[i], bitPos, eolTag2DCode)
		out = e.appendEncodedRow(out, line1D, bitPos)
		if e.EncodedByteAlign {
			endBit = 0
		}
		bitPos = endBit
		for j := i + 1; j < (i+e.K) && j < len(rows); j++ {
			if e.Rows > 0 && !e.EndOfBlock && j == e.Rows {
				break
			}
			rowBuf, rowBit := appendBits(nil, bitPos, eolTag1DCode)
			var a1, b1, b2 int
			a0 := -1
			for a0 < len(rows[j]) {
				a1 = nextChange(rows[j], a0)
				b1 = findB1(rows[j], rows[j-1], a0)
				b2 = nextChange(rows[j-1], b1)
				if b2 < a1 {
					rowBuf, rowBit = appendPassCode(rowBuf, rowBit)
					a0 = b2
				} else {
					if math.Abs(float64(b1-a1)) > 3 {
						rowBuf, rowBit, a0 = appendHorizontalMode(rows[j], rowBuf, rowBit, a0, a1)
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
	}
	if e.EndOfBlock {
		eob, _ := rtcG32D(bitPos)
		out = e.appendEncodedRow(out, eob, bitPos)
	}
	return out
}

func (d *Decoder) decoderRowType41D() error {
	if d.byteAligned {
		d.reader.Align()
	}
	d.reader.Mark()
	var (
		foundEOL bool
		err      error
	)
	if d.endOfLine {
		foundEOL, err = d.tryFetchEOL()
		if err != nil {
			return err
		}
		if !foundEOL {
			return errEOLNotFound
		}
	} else {
		foundEOL, err = d.looseFetchEOL()
		if err != nil {
			return err
		}
	}
	if !foundEOL {
		d.reader.Reset()
	}
	if foundEOL && d.endOfBlock {
		d.reader.Mark()
		for i := 0; i < 5; i++ {
			foundEOL, err = d.tryFetchEOL()
			if err != nil {
				if errors.Is(err, io.EOF) {
					if i == 0 {
						break
					}
					return errCorruptedRTC
				}
			}
			if foundEOL {
				continue
			}
			if i > 0 {
				return errCorruptedRTC
			}
			break
		}
		if foundEOL {
			return io.EOF
		}
		d.reader.Reset()
	}
	if err = d.decode1D(); err != nil {
		return err
	}
	return nil
}

func appendPassCode(buf []byte, bitPos int) ([]byte, int) { return appendBits(buf, bitPos, passCode) }

func findB1(cur, ref []byte, a0 int) int {
	b1 := nextChange(ref, a0)
	if b1 < len(ref) && (a0 == -1 && ref[b1] == whitePixel || a0 >= 0 && a0 < len(cur) && cur[a0] == ref[b1] || a0 >= len(cur) && cur[a0-1] != ref[b1]) {
		b1 = nextChange(ref, b1)
	}
	return b1
}

func (d *Decoder) tryFetchEOL() (bool, error) {
	bits, err := d.reader.ReadBits(12)
	if err != nil {
		return false, err
	}
	return bits == 0x1, nil
}

var (
	eolNode             *treeNode
	fillNode            *treeNode
	blackTree           *tree
	whiteTree           *tree
	eolTree             *tree
	modeTree            *tree
	eolValue            = -2000
	fillValue           = -1000
	passModeValue       = -3000
	horizontalModeValue = -4000
)

func (t *tree) fillWithNode(length, bits int, node *treeNode) error {
	cur := t.root
	for i := 0; i < length; i++ {
		shift := uint(length - 1 - i)
		bit := ((bits >> shift) & 1) != 0
		next := cur.walk(bit)
		if next != nil {
			if next.isLeaf {
				return errors.New("node is leaf, no other following")
			}
			cur = next
			continue
		}
		if i == length-1 {
			next = node
		} else {
			next = &treeNode{}
		}
		if bits == 0 {
			next.isZeroCode = true
		}
		cur.set(bit, next)
		cur = next
	}
	return nil
}

type tiffType int

func (e *Encoder) Encode(pixels [][]byte) []byte {
	if e.BlackIs1 {
		whitePixel = 0
		blackPixel = 1
	} else {
		whitePixel = 1
		blackPixel = 0
	}
	if e.K == 0 {
		return e.encodeG31D(pixels)
	}
	if e.K > 0 {
		return e.encodeG32D(pixels)
	}
	if e.K < 0 {
		return e.encodeG4(pixels)
	}
	return nil
}

func (d *Decoder) decodeRun(t *tree) (int, error) {
	var total int
	node := t.root
	for {
		bit, err := d.reader.ReadBool()
		if err != nil {
			return 0, err
		}
		node = node.walk(bit)
		if node == nil {
			return 0, errors.New("unknown code in Huffman RLE stream")
		}
		if node.isLeaf {
			total += node.value
			switch {
			case node.value >= 64:
				node = t.root
			case node.value >= 0:
				return total, nil
			default:
				return d.columns, nil
			}
		}
	}
}

func appendVerticalCode(buf []byte, bitPos, a1, b1 int) ([]byte, int) {
	c := verticalModeCode(a1, b1)
	buf, bitPos = appendBits(buf, bitPos, c)
	return buf, bitPos
}

func (d *Decoder) decodeRowType2() error {
	if d.byteAligned {
		d.reader.Align()
	}
	if err := d.decode1D(); err != nil {
		return err
	}
	return nil
}

func nextChange(row []byte, pos int) int {
	if pos >= len(row) {
		return pos
	}
	if pos < -1 {
		pos = -1
	}
	var color byte
	if pos > -1 {
		color = row[pos]
	} else {
		color = whitePixel
	}
	i := pos + 1
	for i < len(row) {
		if row[i] != color {
			break
		}
		i++
	}
	return i
}

func NewDecoder(data []byte, options DecodeOptions) (*Decoder, error) {
	d := &Decoder{reader: bitwise.NewReader(data), columns: options.Columns, rows: options.Rows, damagedRowsBeforeError: options.DamagedRowsBeforeError, rowBuf: make([]byte, (options.Columns+7)/8), refChanges: make([]int, options.Columns+2), curChanges: make([]int, options.Columns+2), byteAligned: options.EncodedByteAligned, blackIsOne: options.BlackIsOne, endOfLine: options.EndOfLine, endOfBlock: options.EndOfBlock}
	switch {
	case options.K == 0:
		d.decoderType = tiffTypeT4
		if len(data) < 20 {
			return nil, errors.New("too short ccittfax stream")
		}
		head := data[:20]
		if head[0] != 0 || (head[1]>>4 != 1 && head[1] != 1) {
			d.decoderType = tiffTypeModifiedHuffmanRle
			window := (uint16(head[0])<<8 + uint16(head[1]&0xff)) >> 4
			for i := 12; i < 160; i++ {
				window = (window << 1) + uint16((head[i/8]>>uint16(7-(i%8)))&0x01)
				if window&0xfff == 1 {
					d.decoderType = tiffTypeT4
					break
				}
			}
		}
	case options.K < 0:
		d.decoderType = tiffTypeT6
	case options.K > 0:
		d.decoderType = tiffTypeT4
		d.mixed2D = true
	}
	switch d.decoderType {
	case tiffTypeModifiedHuffmanRle, tiffTypeT4, tiffTypeT6:
	default:
		return nil, errors.New("unknown ccittfax.Decoder type")
	}
	return d, nil
}

var whiteRunLengths = [...][]uint16{{2, 3, 4, 5, 6, 7}, {128, 8, 9, 64, 10, 11}, {192, 1664, 16, 17, 13, 14, 15, 1, 12}, {26, 21, 28, 27, 18, 24, 25, 22, 256, 23, 20, 19}, {33, 34, 35, 36, 37, 38, 31, 32, 29, 53, 54, 39, 40, 41, 42, 43, 44, 30, 61, 62, 63, 0, 320, 384, 45, 59, 60, 46, 49, 50, 51, 52, 55, 56, 57, 58, 448, 512, 640, 576, 47, 48}, {1472, 1536, 1600, 1728, 704, 768, 832, 896, 960, 1024, 1088, 1152, 1216, 1280, 1344, 1408}, {}, {1792, 1856, 1920}, {1984, 2048, 2112, 2176, 2240, 2304, 2368, 2432, 2496, 2560}}

func (e *Encoder) encodeG31D(rows [][]byte) []byte {
	var out []byte
	bitPos := 0
	for i := range rows {
		if e.Rows > 0 && !e.EndOfBlock && i == e.Rows {
			break
		}
		line, endBit := encodeRow1D(rows[i], bitPos, eolCode)
		out = e.appendEncodedRow(out, line, bitPos)
		if e.EncodedByteAlign {
			endBit = 0
		}
		bitPos = endBit
	}
	if e.EndOfBlock {
		eob, _ := rtcG31D(bitPos)
		out = e.appendEncodedRow(out, eob, bitPos)
	}
	return out
}

var (
	whitePixel byte = 1
	blackPixel byte = 0
)

type Decoder struct {
	columns                int
	rows                   int
	decodedRows            int
	rowBuf                 []byte
	damagedRowsBeforeError int
	mixed2D                bool
	unusedFlag1            bool
	unusedFlag2            bool
	blackIsOne             bool
	endOfLine              bool
	endOfBlock             bool
	byteAligned            bool
	rowBytes               int
	readPos                int
	refChanges             []int
	curChanges             []int
	refCount               int
	curCount               int
	damagedRows            int
	lastChangeIdx          int
	reader                 *bitwise.Reader
	decoderType            tiffType
	err                    error
}

var (
	blackTermCodes    map[int]code
	whiteTermCodes    map[int]code
	blackMakeupCodes  map[int]code
	whiteMakeupCodes  map[int]code
	commonMakeupCodes map[int]code
	bitMasks          map[int]byte
	eolCode           = code{Code: 1 << 4, BitsWritten: 12}
	eolTag2DCode      = code{Code: 3 << 3, BitsWritten: 13}
	eolTag1DCode      = code{Code: 2 << 3, BitsWritten: 13}
	passCode          = code{Code: 1 << 12, BitsWritten: 4}
	horizontalCode    = code{Code: 1 << 13, BitsWritten: 3}
	vertical0Code     = code{Code: 1 << 15, BitsWritten: 1}
	vertRight1Code    = code{Code: 3 << 13, BitsWritten: 3}
	vertRight2Code    = code{Code: 3 << 10, BitsWritten: 6}
	vertRight3Code    = code{Code: 3 << 9, BitsWritten: 7}
	vertLeft1Code     = code{Code: 2 << 13, BitsWritten: 3}
	vertLeft2Code     = code{Code: 2 << 10, BitsWritten: 6}
	vertLeft3Code     = code{Code: 2 << 9, BitsWritten: 7}
)
