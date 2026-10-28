// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package ccittfax

import (
	"errors"
	"io"
	"math"

	"github.com/OpenMellon/openpdf/internal/bitwise"
)

func (e *Encoder) appendEncodedRow(dst, row []byte, bitPos int) []byte {
	if len(dst) > 0 && bitPos != 0 && !e.EncodedByteAlign {
		dst[len(dst)-1] = dst[len(dst)-1] | row[0]
		dst = append(dst, row[1:]...)
	} else {
		dst = append(dst, row...)
	}
	return dst
}

func init() {
	eolNode = &treeNode{isLeaf: true, value: eolValue}
	fillNode = &treeNode{value: fillValue, one: eolNode}
	fillNode.zero = fillNode
	eolTree = &tree{root: &treeNode{}}
	if err := eolTree.fillWithNode(12, 0, fillNode); err != nil {
		panic(err.Error())
	}
	if err := eolTree.fillWithNode(12, 1, eolNode); err != nil {
		panic(err.Error())
	}
	blackTree = &tree{root: &treeNode{}}
	for i := 0; i < len(blackCodeBits); i++ {
		for j := 0; j < len(blackCodeBits[i]); j++ {
			if err := blackTree.fill(i+2, int(blackCodeBits[i][j]), int(blackRunLengths[i][j])); err != nil {
				panic(err.Error())
			}
		}
	}
	if err := blackTree.fillWithNode(12, 0, fillNode); err != nil {
		panic(err.Error())
	}
	if err := blackTree.fillWithNode(12, 1, eolNode); err != nil {
		panic(err.Error())
	}
	whiteTree = &tree{root: &treeNode{}}
	for i := 0; i < len(whiteCodeBits); i++ {
		for j := 0; j < len(whiteCodeBits[i]); j++ {
			if err := whiteTree.fill(i+4, int(whiteCodeBits[i][j]), int(whiteRunLengths[i][j])); err != nil {
				panic(err.Error())
			}
		}
	}
	if err := whiteTree.fillWithNode(12, 0, fillNode); err != nil {
		panic(err.Error())
	}
	if err := whiteTree.fillWithNode(12, 1, eolNode); err != nil {
		panic(err.Error())
	}
	modeTree = &tree{root: &treeNode{}}
	if err := modeTree.fill(4, 1, passModeValue); err != nil {
		panic(err.Error())
	}
	if err := modeTree.fill(3, 1, horizontalModeValue); err != nil {
		panic(err.Error())
	}
	if err := modeTree.fill(1, 1, 0); err != nil {
		panic(err.Error())
	}
	if err := modeTree.fill(3, 3, 1); err != nil {
		panic(err.Error())
	}
	if err := modeTree.fill(6, 3, 2); err != nil {
		panic(err.Error())
	}
	if err := modeTree.fill(7, 3, 3); err != nil {
		panic(err.Error())
	}
	if err := modeTree.fill(3, 2, -1); err != nil {
		panic(err.Error())
	}
	if err := modeTree.fill(6, 2, -2); err != nil {
		panic(err.Error())
	}
	if err := modeTree.fill(7, 2, -3); err != nil {
		panic(err.Error())
	}
}

func (d *Decoder) decodeRowType4() error {
	if !d.mixed2D {
		return d.decoderRowType41D()
	}
	if d.byteAligned {
		d.reader.Align()
	}
	d.reader.Mark()
	foundEOL, err := d.tryFetchEOL()
	if err != nil {
		return err
	}
	if !foundEOL && d.endOfLine {
		d.damagedRows++
		if d.damagedRows > d.damagedRowsBeforeError {
			return errEOLNotFound
		}
		d.reader.Reset()
	}
	if !foundEOL {
		d.reader.Reset()
	}
	is1D, err := d.reader.ReadBool()
	if err != nil {
		return err
	}
	if is1D {
		if foundEOL && d.endOfBlock {
			if err = d.tryFetchRTC2D(); err != nil {
				return err
			}
		}
		err = d.decode1D()
	} else {
		err = d.decode2D()
	}
	if err != nil {
		return err
	}
	return nil
}

var (
	errCorruptedRTC = errors.New("ccittfax corrupted RTC")
	errEOLNotFound  = errors.New("ccittfax EOL not found")
)

const (
	_ tiffType = iota
	tiffTypeModifiedHuffmanRle
	tiffTypeT4
	tiffTypeT6
)

func (d *Decoder) decode1D() error {
	var (
		col int
		err error
	)
	white := true
	d.curCount = 0
	for {
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
		white = !white
		if col >= d.columns {
			break
		}
	}
	return nil
}

func (d *Decoder) tryFetchRTC2D() (err error) {
	d.reader.Mark()
	var foundEOL bool
	for i := 0; i < 5; i++ {
		foundEOL, err = d.tryFetchEOL1()
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
	return err
}

func (d *Decoder) looseFetchEOL() (bool, error) {
	bits, err := d.reader.ReadBits(12)
	if err != nil {
		return false, err
	}
	switch bits {
	case 0x1:
		return true, nil
	case 0x0:
		for {
			bit, readErr := d.reader.ReadBool()
			if readErr != nil {
				return false, readErr
			}
			if bit {
				return true, nil
			}
		}
	default:
		return false, nil
	}
}

func init() {
	blackTermCodes = make(map[int]code)
	blackTermCodes[0] = code{Code: 13<<8 | 3<<6, BitsWritten: 10}
	blackTermCodes[1] = code{Code: 2 << (5 + 8), BitsWritten: 3}
	blackTermCodes[2] = code{Code: 3 << (6 + 8), BitsWritten: 2}
	blackTermCodes[3] = code{Code: 2 << (6 + 8), BitsWritten: 2}
	blackTermCodes[4] = code{Code: 3 << (5 + 8), BitsWritten: 3}
	blackTermCodes[5] = code{Code: 3 << (4 + 8), BitsWritten: 4}
	blackTermCodes[6] = code{Code: 2 << (4 + 8), BitsWritten: 4}
	blackTermCodes[7] = code{Code: 3 << (3 + 8), BitsWritten: 5}
	blackTermCodes[8] = code{Code: 5 << (2 + 8), BitsWritten: 6}
	blackTermCodes[9] = code{Code: 4 << (2 + 8), BitsWritten: 6}
	blackTermCodes[10] = code{Code: 4 << (1 + 8), BitsWritten: 7}
	blackTermCodes[11] = code{Code: 5 << (1 + 8), BitsWritten: 7}
	blackTermCodes[12] = code{Code: 7 << (1 + 8), BitsWritten: 7}
	blackTermCodes[13] = code{Code: 4 << 8, BitsWritten: 8}
	blackTermCodes[14] = code{Code: 7 << 8, BitsWritten: 8}
	blackTermCodes[15] = code{Code: 12 << 8, BitsWritten: 9}
	blackTermCodes[16] = code{Code: 5<<8 | 3<<6, BitsWritten: 10}
	blackTermCodes[17] = code{Code: 6 << 8, BitsWritten: 10}
	blackTermCodes[18] = code{Code: 2 << 8, BitsWritten: 10}
	blackTermCodes[19] = code{Code: 12<<8 | 7<<5, BitsWritten: 11}
	blackTermCodes[20] = code{Code: 13 << 8, BitsWritten: 11}
	blackTermCodes[21] = code{Code: 13<<8 | 4<<5, BitsWritten: 11}
	blackTermCodes[22] = code{Code: 6<<8 | 7<<5, BitsWritten: 11}
	blackTermCodes[23] = code{Code: 5 << 8, BitsWritten: 11}
	blackTermCodes[24] = code{Code: 2<<8 | 7<<5, BitsWritten: 11}
	blackTermCodes[25] = code{Code: 3 << 8, BitsWritten: 11}
	blackTermCodes[26] = code{Code: 12<<8 | 10<<4, BitsWritten: 12}
	blackTermCodes[27] = code{Code: 12<<8 | 11<<4, BitsWritten: 12}
	blackTermCodes[28] = code{Code: 12<<8 | 12<<4, BitsWritten: 12}
	blackTermCodes[29] = code{Code: 12<<8 | 13<<4, BitsWritten: 12}
	blackTermCodes[30] = code{Code: 6<<8 | 8<<4, BitsWritten: 12}
	blackTermCodes[31] = code{Code: 6<<8 | 9<<4, BitsWritten: 12}
	blackTermCodes[32] = code{Code: 6<<8 | 10<<4, BitsWritten: 12}
	blackTermCodes[33] = code{Code: 6<<8 | 11<<4, BitsWritten: 12}
	blackTermCodes[34] = code{Code: 13<<8 | 2<<4, BitsWritten: 12}
	blackTermCodes[35] = code{Code: 13<<8 | 3<<4, BitsWritten: 12}
	blackTermCodes[36] = code{Code: 13<<8 | 4<<4, BitsWritten: 12}
	blackTermCodes[37] = code{Code: 13<<8 | 5<<4, BitsWritten: 12}
	blackTermCodes[38] = code{Code: 13<<8 | 6<<4, BitsWritten: 12}
	blackTermCodes[39] = code{Code: 13<<8 | 7<<4, BitsWritten: 12}
	blackTermCodes[40] = code{Code: 6<<8 | 12<<4, BitsWritten: 12}
	blackTermCodes[41] = code{Code: 6<<8 | 13<<4, BitsWritten: 12}
	blackTermCodes[42] = code{Code: 13<<8 | 10<<4, BitsWritten: 12}
	blackTermCodes[43] = code{Code: 13<<8 | 11<<4, BitsWritten: 12}
	blackTermCodes[44] = code{Code: 5<<8 | 4<<4, BitsWritten: 12}
	blackTermCodes[45] = code{Code: 5<<8 | 5<<4, BitsWritten: 12}
	blackTermCodes[46] = code{Code: 5<<8 | 6<<4, BitsWritten: 12}
	blackTermCodes[47] = code{Code: 5<<8 | 7<<4, BitsWritten: 12}
	blackTermCodes[48] = code{Code: 6<<8 | 4<<4, BitsWritten: 12}
	blackTermCodes[49] = code{Code: 6<<8 | 5<<4, BitsWritten: 12}
	blackTermCodes[50] = code{Code: 5<<8 | 2<<4, BitsWritten: 12}
	blackTermCodes[51] = code{Code: 5<<8 | 3<<4, BitsWritten: 12}
	blackTermCodes[52] = code{Code: 2<<8 | 4<<4, BitsWritten: 12}
	blackTermCodes[53] = code{Code: 3<<8 | 7<<4, BitsWritten: 12}
	blackTermCodes[54] = code{Code: 3<<8 | 8<<4, BitsWritten: 12}
	blackTermCodes[55] = code{Code: 2<<8 | 7<<4, BitsWritten: 12}
	blackTermCodes[56] = code{Code: 2<<8 | 8<<4, BitsWritten: 12}
	blackTermCodes[57] = code{Code: 5<<8 | 8<<4, BitsWritten: 12}
	blackTermCodes[58] = code{Code: 5<<8 | 9<<4, BitsWritten: 12}
	blackTermCodes[59] = code{Code: 2<<8 | 11<<4, BitsWritten: 12}
	blackTermCodes[60] = code{Code: 2<<8 | 12<<4, BitsWritten: 12}
	blackTermCodes[61] = code{Code: 5<<8 | 10<<4, BitsWritten: 12}
	blackTermCodes[62] = code{Code: 6<<8 | 6<<4, BitsWritten: 12}
	blackTermCodes[63] = code{Code: 6<<8 | 7<<4, BitsWritten: 12}
	whiteTermCodes = make(map[int]code)
	whiteTermCodes[0] = code{Code: 53 << 8, BitsWritten: 8}
	whiteTermCodes[1] = code{Code: 7 << (2 + 8), BitsWritten: 6}
	whiteTermCodes[2] = code{Code: 7 << (4 + 8), BitsWritten: 4}
	whiteTermCodes[3] = code{Code: 8 << (4 + 8), BitsWritten: 4}
	whiteTermCodes[4] = code{Code: 11 << (4 + 8), BitsWritten: 4}
	whiteTermCodes[5] = code{Code: 12 << (4 + 8), BitsWritten: 4}
	whiteTermCodes[6] = code{Code: 14 << (4 + 8), BitsWritten: 4}
	whiteTermCodes[7] = code{Code: 15 << (4 + 8), BitsWritten: 4}
	whiteTermCodes[8] = code{Code: 19 << (3 + 8), BitsWritten: 5}
	whiteTermCodes[9] = code{Code: 20 << (3 + 8), BitsWritten: 5}
	whiteTermCodes[10] = code{Code: 7 << (3 + 8), BitsWritten: 5}
	whiteTermCodes[11] = code{Code: 8 << (3 + 8), BitsWritten: 5}
	whiteTermCodes[12] = code{Code: 8 << (2 + 8), BitsWritten: 6}
	whiteTermCodes[13] = code{Code: 3 << (2 + 8), BitsWritten: 6}
	whiteTermCodes[14] = code{Code: 52 << (2 + 8), BitsWritten: 6}
	whiteTermCodes[15] = code{Code: 53 << (2 + 8), BitsWritten: 6}
	whiteTermCodes[16] = code{Code: 42 << (2 + 8), BitsWritten: 6}
	whiteTermCodes[17] = code{Code: 43 << (2 + 8), BitsWritten: 6}
	whiteTermCodes[18] = code{Code: 39 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[19] = code{Code: 12 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[20] = code{Code: 8 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[21] = code{Code: 23 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[22] = code{Code: 3 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[23] = code{Code: 4 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[24] = code{Code: 40 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[25] = code{Code: 43 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[26] = code{Code: 19 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[27] = code{Code: 36 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[28] = code{Code: 24 << (1 + 8), BitsWritten: 7}
	whiteTermCodes[29] = code{Code: 2 << 8, BitsWritten: 8}
	whiteTermCodes[30] = code{Code: 3 << 8, BitsWritten: 8}
	whiteTermCodes[31] = code{Code: 26 << 8, BitsWritten: 8}
	whiteTermCodes[32] = code{Code: 27 << 8, BitsWritten: 8}
	whiteTermCodes[33] = code{Code: 18 << 8, BitsWritten: 8}
	whiteTermCodes[34] = code{Code: 19 << 8, BitsWritten: 8}
	whiteTermCodes[35] = code{Code: 20 << 8, BitsWritten: 8}
	whiteTermCodes[36] = code{Code: 21 << 8, BitsWritten: 8}
	whiteTermCodes[37] = code{Code: 22 << 8, BitsWritten: 8}
	whiteTermCodes[38] = code{Code: 23 << 8, BitsWritten: 8}
	whiteTermCodes[39] = code{Code: 40 << 8, BitsWritten: 8}
	whiteTermCodes[40] = code{Code: 41 << 8, BitsWritten: 8}
	whiteTermCodes[41] = code{Code: 42 << 8, BitsWritten: 8}
	whiteTermCodes[42] = code{Code: 43 << 8, BitsWritten: 8}
	whiteTermCodes[43] = code{Code: 44 << 8, BitsWritten: 8}
	whiteTermCodes[44] = code{Code: 45 << 8, BitsWritten: 8}
	whiteTermCodes[45] = code{Code: 4 << 8, BitsWritten: 8}
	whiteTermCodes[46] = code{Code: 5 << 8, BitsWritten: 8}
	whiteTermCodes[47] = code{Code: 10 << 8, BitsWritten: 8}
	whiteTermCodes[48] = code{Code: 11 << 8, BitsWritten: 8}
	whiteTermCodes[49] = code{Code: 82 << 8, BitsWritten: 8}
	whiteTermCodes[50] = code{Code: 83 << 8, BitsWritten: 8}
	whiteTermCodes[51] = code{Code: 84 << 8, BitsWritten: 8}
	whiteTermCodes[52] = code{Code: 85 << 8, BitsWritten: 8}
	whiteTermCodes[53] = code{Code: 36 << 8, BitsWritten: 8}
	whiteTermCodes[54] = code{Code: 37 << 8, BitsWritten: 8}
	whiteTermCodes[55] = code{Code: 88 << 8, BitsWritten: 8}
	whiteTermCodes[56] = code{Code: 89 << 8, BitsWritten: 8}
	whiteTermCodes[57] = code{Code: 90 << 8, BitsWritten: 8}
	whiteTermCodes[58] = code{Code: 91 << 8, BitsWritten: 8}
	whiteTermCodes[59] = code{Code: 74 << 8, BitsWritten: 8}
	whiteTermCodes[60] = code{Code: 75 << 8, BitsWritten: 8}
	whiteTermCodes[61] = code{Code: 50 << 8, BitsWritten: 8}
	whiteTermCodes[62] = code{Code: 51 << 8, BitsWritten: 8}
	whiteTermCodes[63] = code{Code: 52 << 8, BitsWritten: 8}
	blackMakeupCodes = make(map[int]code)
	blackMakeupCodes[64] = code{Code: 3<<8 | 3<<6, BitsWritten: 10}
	blackMakeupCodes[128] = code{Code: 12<<8 | 8<<4, BitsWritten: 12}
	blackMakeupCodes[192] = code{Code: 12<<8 | 9<<4, BitsWritten: 12}
	blackMakeupCodes[256] = code{Code: 5<<8 | 11<<4, BitsWritten: 12}
	blackMakeupCodes[320] = code{Code: 3<<8 | 3<<4, BitsWritten: 12}
	blackMakeupCodes[384] = code{Code: 3<<8 | 4<<4, BitsWritten: 12}
	blackMakeupCodes[448] = code{Code: 3<<8 | 5<<4, BitsWritten: 12}
	blackMakeupCodes[512] = code{Code: 3<<8 | 12<<3, BitsWritten: 13}
	blackMakeupCodes[576] = code{Code: 3<<8 | 13<<3, BitsWritten: 13}
	blackMakeupCodes[640] = code{Code: 2<<8 | 10<<3, BitsWritten: 13}
	blackMakeupCodes[704] = code{Code: 2<<8 | 11<<3, BitsWritten: 13}
	blackMakeupCodes[768] = code{Code: 2<<8 | 12<<3, BitsWritten: 13}
	blackMakeupCodes[832] = code{Code: 2<<8 | 13<<3, BitsWritten: 13}
	blackMakeupCodes[896] = code{Code: 3<<8 | 18<<3, BitsWritten: 13}
	blackMakeupCodes[960] = code{Code: 3<<8 | 19<<3, BitsWritten: 13}
	blackMakeupCodes[1024] = code{Code: 3<<8 | 20<<3, BitsWritten: 13}
	blackMakeupCodes[1088] = code{Code: 3<<8 | 21<<3, BitsWritten: 13}
	blackMakeupCodes[1152] = code{Code: 3<<8 | 22<<3, BitsWritten: 13}
	blackMakeupCodes[1216] = code{Code: 119 << 3, BitsWritten: 13}
	blackMakeupCodes[1280] = code{Code: 2<<8 | 18<<3, BitsWritten: 13}
	blackMakeupCodes[1344] = code{Code: 2<<8 | 19<<3, BitsWritten: 13}
	blackMakeupCodes[1408] = code{Code: 2<<8 | 20<<3, BitsWritten: 13}
	blackMakeupCodes[1472] = code{Code: 2<<8 | 21<<3, BitsWritten: 13}
	blackMakeupCodes[1536] = code{Code: 2<<8 | 26<<3, BitsWritten: 13}
	blackMakeupCodes[1600] = code{Code: 2<<8 | 27<<3, BitsWritten: 13}
	blackMakeupCodes[1664] = code{Code: 3<<8 | 4<<3, BitsWritten: 13}
	blackMakeupCodes[1728] = code{Code: 3<<8 | 5<<3, BitsWritten: 13}
	whiteMakeupCodes = make(map[int]code)
	whiteMakeupCodes[64] = code{Code: 27 << (3 + 8), BitsWritten: 5}
	whiteMakeupCodes[128] = code{Code: 18 << (3 + 8), BitsWritten: 5}
	whiteMakeupCodes[192] = code{Code: 23 << (2 + 8), BitsWritten: 6}
	whiteMakeupCodes[256] = code{Code: 55 << (1 + 8), BitsWritten: 7}
	whiteMakeupCodes[320] = code{Code: 54 << 8, BitsWritten: 8}
	whiteMakeupCodes[384] = code{Code: 55 << 8, BitsWritten: 8}
	whiteMakeupCodes[448] = code{Code: 100 << 8, BitsWritten: 8}
	whiteMakeupCodes[512] = code{Code: 101 << 8, BitsWritten: 8}
	whiteMakeupCodes[576] = code{Code: 104 << 8, BitsWritten: 8}
	whiteMakeupCodes[640] = code{Code: 103 << 8, BitsWritten: 8}
	whiteMakeupCodes[704] = code{Code: 102 << 8, BitsWritten: 9}
	whiteMakeupCodes[768] = code{Code: 102<<8 | 1<<7, BitsWritten: 9}
	whiteMakeupCodes[832] = code{Code: 105 << 8, BitsWritten: 9}
	whiteMakeupCodes[896] = code{Code: 105<<8 | 1<<7, BitsWritten: 9}
	whiteMakeupCodes[960] = code{Code: 106 << 8, BitsWritten: 9}
	whiteMakeupCodes[1024] = code{Code: 106<<8 | 1<<7, BitsWritten: 9}
	whiteMakeupCodes[1088] = code{Code: 107 << 8, BitsWritten: 9}
	whiteMakeupCodes[1152] = code{Code: 107<<8 | 1<<7, BitsWritten: 9}
	whiteMakeupCodes[1216] = code{Code: 108 << 8, BitsWritten: 9}
	whiteMakeupCodes[1280] = code{Code: 108<<8 | 1<<7, BitsWritten: 9}
	whiteMakeupCodes[1344] = code{Code: 109 << 8, BitsWritten: 9}
	whiteMakeupCodes[1408] = code{Code: 109<<8 | 1<<7, BitsWritten: 9}
	whiteMakeupCodes[1472] = code{Code: 76 << 8, BitsWritten: 9}
	whiteMakeupCodes[1536] = code{Code: 76<<8 | 1<<7, BitsWritten: 9}
	whiteMakeupCodes[1600] = code{Code: 77 << 8, BitsWritten: 9}
	whiteMakeupCodes[1664] = code{Code: 24 << (2 + 8), BitsWritten: 6}
	whiteMakeupCodes[1728] = code{Code: 77<<8 | 1<<7, BitsWritten: 9}
	commonMakeupCodes = make(map[int]code)
	commonMakeupCodes[1792] = code{Code: 1 << 8, BitsWritten: 11}
	commonMakeupCodes[1856] = code{Code: 1<<8 | 4<<5, BitsWritten: 11}
	commonMakeupCodes[1920] = code{Code: 1<<8 | 5<<5, BitsWritten: 11}
	commonMakeupCodes[1984] = code{Code: 1<<8 | 2<<4, BitsWritten: 12}
	commonMakeupCodes[2048] = code{Code: 1<<8 | 3<<4, BitsWritten: 12}
	commonMakeupCodes[2112] = code{Code: 1<<8 | 4<<4, BitsWritten: 12}
	commonMakeupCodes[2176] = code{Code: 1<<8 | 5<<4, BitsWritten: 12}
	commonMakeupCodes[2240] = code{Code: 1<<8 | 6<<4, BitsWritten: 12}
	commonMakeupCodes[2304] = code{Code: 1<<8 | 7<<4, BitsWritten: 12}
	commonMakeupCodes[2368] = code{Code: 1<<8 | 12<<4, BitsWritten: 12}
	commonMakeupCodes[2432] = code{Code: 1<<8 | 13<<4, BitsWritten: 12}
	commonMakeupCodes[2496] = code{Code: 1<<8 | 14<<4, BitsWritten: 12}
	commonMakeupCodes[2560] = code{Code: 1<<8 | 15<<4, BitsWritten: 12}
	bitMasks = make(map[int]byte)
	bitMasks[0] = 0xFF
	bitMasks[1] = 0xFE
	bitMasks[2] = 0xFC
	bitMasks[3] = 0xF8
	bitMasks[4] = 0xF0
	bitMasks[5] = 0xE0
	bitMasks[6] = 0xC0
	bitMasks[7] = 0x80
	bitMasks[8] = 0x00
}

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
