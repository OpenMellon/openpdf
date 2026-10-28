// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package ccittfax

import (
	"errors"
	"io"
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
