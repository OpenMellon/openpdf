// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package arithmetic

import (
	"bytes"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/bitmap"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

var qeTable = []state{{0x5601, 1, 1, 1}, {0x3401, 2, 6, 0}, {0x1801, 3, 9, 0}, {0x0AC1, 4, 12, 0}, {0x0521, 5, 29, 0}, {0x0221, 38, 33, 0}, {0x5601, 7, 6, 1}, {0x5401, 8, 14, 0}, {0x4801, 9, 14, 0}, {0x3801, 10, 14, 0}, {0x3001, 11, 17, 0}, {0x2401, 12, 18, 0}, {0x1C01, 13, 20, 0}, {0x1601, 29, 21, 0}, {0x5601, 15, 14, 1}, {0x5401, 16, 14, 0}, {0x5101, 17, 15, 0}, {0x4801, 18, 16, 0}, {0x3801, 19, 17, 0}, {0x3401, 20, 18, 0}, {0x3001, 21, 19, 0}, {0x2801, 22, 19, 0}, {0x2401, 23, 20, 0}, {0x2201, 24, 21, 0}, {0x1C01, 25, 22, 0}, {0x1801, 26, 23, 0}, {0x1601, 27, 24, 0}, {0x1401, 28, 25, 0}, {0x1201, 29, 26, 0}, {0x1101, 30, 27, 0}, {0x0AC1, 31, 28, 0}, {0x09C1, 32, 29, 0}, {0x08A1, 33, 30, 0}, {0x0521, 34, 31, 0}, {0x0441, 35, 32, 0}, {0x02A1, 36, 33, 0}, {0x0221, 37, 34, 0}, {0x0141, 38, 35, 0}, {0x0111, 39, 36, 0}, {0x0085, 40, 37, 0}, {0x0049, 41, 38, 0}, {0x0025, 42, 39, 0}, {0x0015, 43, 40, 0}, {0x0009, 44, 41, 0}, {0x0005, 45, 42, 0}, {0x0001, 45, 43, 0}, {0x5601, 46, 46, 0}}

func (e *Encoder) dataSize() int { return chunkSize*len(e.chunks) + e.chunkLen }
func (e *Encoder) encodeOOB(class Class) error {
	ctx := e.intCtx[class]
	err := e.encodeBit(ctx, 1, 1)
	if err != nil {
		return err
	}
	err = e.encodeBit(ctx, 3, 0)
	if err != nil {
		return err
	}
	err = e.encodeBit(ctx, 6, 0)
	if err != nil {
		return err
	}
	err = e.encodeBit(ctx, 12, 0)
	if err != nil {
		return err
	}
	return nil
}

func (e *Encoder) rBlock() {
	if e.bytePos >= 0 {
		e.emit()
	}
	e.bytePos++
	e.outByte = uint8(e.creg >> 20)
	e.creg &= 0xfffff
	e.ct = 7
}

func (e *Encoder) codeLPS(ctx *codingContext, idx uint32, qe uint16, stateIdx byte) {
	e.areg -= qe
	if e.areg < qe {
		e.creg += uint32(qe)
	} else {
		e.areg = qe
	}
	if qeTable[stateIdx].switchMPS == 1 {
		ctx.flipMps(idx)
	}
	ctx.states[idx] = qeTable[stateIdx].nextLPS
	e.renormalize()
}
func (e *Encoder) DataSize() int { return e.dataSize() }

type (
	Class         int
	codingContext struct {
		states  []byte
		mpsBits []byte
	}
)

const (
	IAAI Class = iota
	IADH
	IADS
	IADT
	IADW
	IAEX
	IAFS
	IAIT
	IARDH
	IARDW
	IARDX
	IARDY
	IARI
)

func (e *Encoder) EncodeOOB(proc Class) (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encode OOB with Class: '%s'", proc)
	}
	if err = e.encodeOOB(proc); err != nil {
		return jbig2errors.Wrap(err, "EncodeOOB", "")
	}
	return nil
}

var _ io.WriterTo = &Encoder{}

func (c Class) String() string {
	switch c {
	case IAAI:
		return "IAAI"
	case IADH:
		return "IADH"
	case IADS:
		return "IADS"
	case IADT:
		return "IADT"
	case IADW:
		return "IADW"
	case IAEX:
		return "IAEX"
	case IAFS:
		return "IAFS"
	case IAIT:
		return "IAIT"
	case IARDH:
		return "IARDH"
	case IARDW:
		return "IARDW"
	case IARDX:
		return "IARDX"
	case IARDY:
		return "IARDY"
	case IARI:
		return "IARI"
	default:
		return "UNKNOWN"
	}
}

func (e *Encoder) code0(ctx *codingContext, idx uint32, qe uint16, stateIdx byte) {
	if ctx.mps(idx) == 0 {
		e.codeMPS(ctx, idx, qe, stateIdx)
	} else {
		e.codeLPS(ctx, idx, qe, stateIdx)
	}
}

type state struct {
	qe               uint16
	nextMPS, nextLPS uint8
	switchMPS        uint8
}

func (e *Encoder) Reset() {
	e.areg = 0x8000
	e.creg = 0
	e.ct = 12
	e.bytePos = -1
	e.outByte = 0
	e.iaidCtx = nil
	e.genericCtx = newCodingContext(contextSize)
}

func (e *Encoder) EncodeBitmap(bm *bitmap.Bitmap, duplicateLineRemoval bool) error {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encode Bitmap [%dx%d], %s", bm.Width, bm.Height, bm)
	}
	var (
		ltp, sltp                   uint8
		row2Bits, row1Bits, curBits uint16
		row2Byte, row1Byte, curByte byte
		x, curOff, prevOff          int
		curRow, prevRow             []byte
	)
	for y := 0; y < bm.Height; y++ {
		row2Byte, row1Byte = 0, 0
		if y >= 2 {
			row2Byte = bm.Data[(y-2)*bm.RowStride]
		}
		if y >= 1 {
			row1Byte = bm.Data[(y-1)*bm.RowStride]
			if duplicateLineRemoval {
				curOff = y * bm.RowStride
				curRow = bm.Data[curOff : curOff+bm.RowStride]
				prevOff = (y - 1) * bm.RowStride
				prevRow = bm.Data[prevOff : prevOff+bm.RowStride]
				if bytes.Equal(curRow, prevRow) {
					sltp = ltp ^ 1
					ltp = 1
				} else {
					sltp = ltp
					ltp = 0
				}
			}
		}
		if duplicateLineRemoval {
			if err := e.encodeBit(e.genericCtx, tpgdContext, sltp); err != nil {
				return err
			}
			if ltp != 0 {
				continue
			}
		}
		curByte = bm.Data[y*bm.RowStride]
		row2Bits = uint16(row2Byte >> 5)
		row1Bits = uint16(row1Byte >> 4)
		row2Byte <<= 3
		row1Byte <<= 4
		curBits = 0
		for x = 0; x < bm.Width; x++ {
			ctx := uint32(row2Bits<<11 | row1Bits<<4 | curBits)
			bit := (curByte & 0x80) >> 7
			err := e.encodeBit(e.genericCtx, ctx, bit)
			if err != nil {
				return err
			}
			row2Bits <<= 1
			row1Bits <<= 1
			curBits <<= 1
			row2Bits |= uint16((row2Byte & 0x80) >> 7)
			row1Bits |= uint16((row1Byte & 0x80) >> 7)
			curBits |= uint16(bit)
			bitPos := x % 8
			byteIdx := x/8 + 1
			if bitPos == 4 && y >= 2 {
				row2Byte = 0
				if byteIdx < bm.RowStride {
					row2Byte = bm.Data[(y-2)*bm.RowStride+byteIdx]
				}
			} else {
				row2Byte <<= 1
			}
			if bitPos == 3 && y >= 1 {
				row1Byte = 0
				if byteIdx < bm.RowStride {
					row1Byte = bm.Data[(y-1)*bm.RowStride+byteIdx]
				}
			} else {
				row1Byte <<= 1
			}
			if bitPos == 7 {
				curByte = 0
				if byteIdx < bm.RowStride {
					curByte = bm.Data[y*bm.RowStride+byteIdx]
				}
			} else {
				curByte <<= 1
			}
			row2Bits &= 31
			row1Bits &= 127
			curBits &= 15
		}
	}
	return nil
}

func (e *Encoder) code1(ctx *codingContext, idx uint32, qe uint16, stateIdx byte) {
	if ctx.mps(idx) == 1 {
		e.codeMPS(ctx, idx, qe, stateIdx)
	} else {
		e.codeLPS(ctx, idx, qe, stateIdx)
	}
}

func (e *Encoder) renormalize() {
	for {
		e.areg <<= 1
		e.creg <<= 1
		e.ct--
		if e.ct == 0 {
			e.byteOut()
		}
		if (e.areg & 0x8000) != 0 {
			break
		}
	}
}

func (e *Encoder) flush() {
	e.setBits()
	e.creg <<= e.ct
	e.byteOut()
	e.creg <<= e.ct
	e.byteOut()
	e.emit()
	if e.outByte != 0xff {
		e.bytePos++
		e.outByte = 0xff
		e.emit()
	}
	e.bytePos++
	e.outByte = 0xac
	e.bytePos++
	e.emit()
}
func (e *Encoder) Final() { e.flush() }

const (
	contextSize = 65536
	chunkSize   = 20 * 1024
)

func (e *Encoder) codeMPS(ctx *codingContext, idx uint32, qe uint16, stateIdx byte) {
	e.areg -= qe
	if e.areg&0x8000 != 0 {
		e.creg += uint32(qe)
		return
	}
	if e.areg < qe {
		e.areg = qe
	} else {
		e.creg += uint32(qe)
	}
	ctx.states[idx] = qeTable[stateIdx].nextMPS
	e.renormalize()
}

func (e *Encoder) setBits() {
	tempC := e.creg + uint32(e.areg)
	e.creg |= 0xffff
	if e.creg >= tempC {
		e.creg -= 0x8000
	}
}
func (c *codingContext) flipMps(idx uint32) { c.mpsBits[idx] = 1 - c.mpsBits[idx] }
func (e *Encoder) encodeBit(ctx *codingContext, idx uint32, bit uint8) error {
	const funcName = "Encoder.encodeBit"
	e.encodeCount++
	if idx >= uint32(len(ctx.states)) {
		return jbig2errors.Errorf(funcName, "arithmetic encoder - invalid ctx number: '%d'", idx)
	}
	stateIdx := ctx.states[idx]
	mps := ctx.mps(idx)
	qe := qeTable[stateIdx].qe
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("EC: %d\t D: %d\t I: %d\t MPS: %d\t QE: %04X\t  A: %04X\t C: %08X\t CT: %d\t B: %02X\t BP: %d", e.encodeCount, bit, stateIdx, mps, qe, e.areg, e.creg, e.ct, e.outByte, e.bytePos)
	}
	if bit == 0 {
		e.code0(ctx, idx, qe, stateIdx)
	} else {
		e.code1(ctx, idx, qe, stateIdx)
	}
	return nil
}

func (e *Encoder) encodeInteger(class Class, value int) error {
	const funcName = "Encoder.encodeInteger"
	if value > 2000000000 || value < -2000000000 {
		return jbig2errors.Errorf(funcName, "arithmetic encoder - invalid integer value: '%d'", value)
	}
	ctx := e.intCtx[class]
	prev := uint32(1)
	var rangeIdx int
	for ; ; rangeIdx++ {
		if intEncRanges[rangeIdx].low <= value && intEncRanges[rangeIdx].high >= value {
			break
		}
	}
	if value < 0 {
		value = -value
	}
	value -= int(intEncRanges[rangeIdx].offset)
	prefix := intEncRanges[rangeIdx].prefix
	for i := uint8(0); i < intEncRanges[rangeIdx].prefixBits; i++ {
		bit := prefix & 1
		if err := e.encodeBit(ctx, prev, bit); err != nil {
			return jbig2errors.Wrap(err, funcName, "")
		}
		prefix >>= 1
		if prev&0x100 > 0 {
			prev = (((prev << 1) | uint32(bit)) & 0x1ff) | 0x100
		} else {
			prev = (prev << 1) | uint32(bit)
		}
	}
	value <<= 32 - intEncRanges[rangeIdx].valueBits
	for i := uint8(0); i < intEncRanges[rangeIdx].valueBits; i++ {
		bit := uint8((uint32(value) & 0x80000000) >> 31)
		if err := e.encodeBit(ctx, prev, bit); err != nil {
			return jbig2errors.Wrap(err, funcName, "move data to the top of word")
		}
		value <<= 1
		if prev&0x100 != 0 {
			prev = (((prev << 1) | uint32(bit)) & 0x1ff) | 0x100
		} else {
			prev = (prev << 1) | uint32(bit)
		}
	}
	return nil
}

var intEncRanges = []intEncRangeS{{0, 3, 0, 2, 0, 2}, {-1, -1, 9, 4, 0, 0}, {-3, -2, 5, 3, 2, 1}, {4, 19, 2, 3, 4, 4}, {-19, -4, 3, 3, 4, 4}, {20, 83, 6, 4, 20, 6}, {-83, -20, 7, 4, 20, 6}, {84, 339, 14, 5, 84, 8}, {-339, -84, 15, 5, 84, 8}, {340, 4435, 30, 6, 340, 12}, {-4435, -340, 31, 6, 340, 12}, {4436, 2000000000, 62, 6, 4436, 32}, {-2000000000, -4436, 63, 6, 4436, 32}}

func (e *Encoder) Flush()                   { e.chunkLen = 0; e.chunks = nil; e.bytePos = -1 }
func New() *Encoder                         { e := &Encoder{}; e.Init(); return e }
func (c *codingContext) mps(idx uint32) int { return int(c.mpsBits[idx]) }

type Encoder struct {
	creg        uint32
	areg        uint16
	ct, outByte uint8
	bytePos     int
	encodeCount int
	chunks      [][]byte
	chunk       []byte
	chunkLen    int
	genericCtx  *codingContext
	intCtx      [13]*codingContext
	iaidCtx     *codingContext
}

func (e *Encoder) encodeIAID(codeLen, value int) error {
	if e.iaidCtx == nil {
		e.iaidCtx = newCodingContext(1 << uint(codeLen))
	}
	mask := uint32(1<<uint32(codeLen+1)) - 1
	value <<= uint(32 - codeLen)
	prev := uint32(1)
	for i := 0; i < codeLen; i++ {
		ctxIdx := prev & mask
		bit := uint8((uint32(value) & 0x80000000) >> 31)
		if err := e.encodeBit(e.iaidCtx, ctxIdx, bit); err != nil {
			return err
		}
		prev = (prev << 1) | uint32(bit)
		value <<= 1
	}
	return nil
}

func newCodingContext(size int) *codingContext {
	return &codingContext{states: make([]byte, size), mpsBits: make([]byte, size)}
}

func (e *Encoder) Refine(iTemp, iTarget *bitmap.Bitmap, ox, oy int) error {
	for y := 0; y < iTarget.Height; y++ {
		var x int
		ty := y + oy
		var (
			tmplAbove, tmplMid, tmplBelow, tgtAbove, prevBit                 uint16
			tmplAboveByte, tmplMidByte, tmplBelowByte, tgtAboveByte, tgtByte byte
		)
		if ty >= 1 && (ty-1) < iTemp.Height {
			tmplAboveByte = iTemp.Data[(ty-1)*iTemp.RowStride]
		}
		if ty >= 0 && ty < iTemp.Height {
			tmplMidByte = iTemp.Data[ty*iTemp.RowStride]
		}
		if ty >= -1 && ty+1 < iTemp.Height {
			tmplBelowByte = iTemp.Data[(ty+1)*iTemp.RowStride]
		}
		if y >= 1 {
			tgtAboveByte = iTarget.Data[(y-1)*iTarget.RowStride]
		}
		tgtByte = iTarget.Data[y*iTarget.RowStride]
		shiftR := uint(6 + ox)
		tmplAbove = uint16(tmplAboveByte >> shiftR)
		tmplMid = uint16(tmplMidByte >> shiftR)
		tmplBelow = uint16(tmplBelowByte >> shiftR)
		tgtAbove = uint16(tgtAboveByte >> 6)
		shiftL := uint(2 - ox)
		tmplAboveByte <<= shiftL
		tmplMidByte <<= shiftL
		tmplBelowByte <<= shiftL
		tgtAboveByte <<= 2
		for x = 0; x < iTarget.Width; x++ {
			ctx := (tmplAbove << 10) | (tmplMid << 7) | (tmplBelow << 4) | (tgtAbove << 1) | prevBit
			bit := tgtByte >> 7
			err := e.encodeBit(e.genericCtx, uint32(ctx), bit)
			if err != nil {
				return err
			}
			tmplAbove <<= 1
			tmplMid <<= 1
			tmplBelow <<= 1
			tgtAbove <<= 1
			tmplAbove |= uint16(tmplAboveByte >> 7)
			tmplMid |= uint16(tmplMidByte >> 7)
			tmplBelow |= uint16(tmplBelowByte >> 7)
			tgtAbove |= uint16(tgtAboveByte >> 7)
			prevBit = uint16(bit)
			bitPos := x % 8
			byteIdx := x/8 + 1
			if bitPos == 5+ox {
				tmplAboveByte, tmplMidByte, tmplBelowByte = 0, 0, 0
				if byteIdx < iTemp.RowStride && ty >= 1 && (ty-1) < iTemp.Height {
					tmplAboveByte = iTemp.Data[(ty-1)*iTemp.RowStride+byteIdx]
				}
				if byteIdx < iTemp.RowStride && ty >= 0 && ty < iTemp.Height {
					tmplMidByte = iTemp.Data[ty*iTemp.RowStride+byteIdx]
				}
				if byteIdx < iTemp.RowStride && ty >= -1 && (ty+1) < iTemp.Height {
					tmplBelowByte = iTemp.Data[(ty+1)*iTemp.RowStride+byteIdx]
				}
			} else {
				tmplAboveByte <<= 1
				tmplMidByte <<= 1
				tmplBelowByte <<= 1
			}
			if bitPos == 5 && y >= 1 {
				tgtAboveByte = 0
				if byteIdx < iTarget.RowStride {
					tgtAboveByte = iTarget.Data[(y-1)*iTarget.RowStride+byteIdx]
				}
			} else {
				tgtAboveByte <<= 1
			}
			if bitPos == 7 {
				tgtByte = 0
				if byteIdx < iTarget.RowStride {
					tgtByte = iTarget.Data[y*iTarget.RowStride+byteIdx]
				}
			} else {
				tgtByte <<= 1
			}
			tmplAbove &= 7
			tmplMid &= 7
			tmplBelow &= 7
			tgtAbove &= 7
		}
	}
	return nil
}

func (e *Encoder) EncodeIAID(symbolCodeLength, value int) (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encode IAID. SymbolCodeLength: '%d', Value: '%d'", symbolCodeLength, value)
	}
	if err = e.encodeIAID(symbolCodeLength, value); err != nil {
		return jbig2errors.Wrap(err, "EncodeIAID", "")
	}
	return nil
}

func (e *Encoder) WriteTo(w io.Writer) (int64, error) {
	const funcName = "Encoder.WriteTo"
	var total int64
	for i, chunk := range e.chunks {
		n, err := w.Write(chunk)
		if err != nil {
			return 0, jbig2errors.Wrapf(err, funcName, "failed at i'th: '%d' chunk", i)
		}
		total += int64(n)
	}
	e.chunk = e.chunk[:e.chunkLen]
	n, err := w.Write(e.chunk)
	if err != nil {
		return 0, jbig2errors.Wrap(err, funcName, "buffered chunks")
	}
	total += int64(n)
	return total, nil
}

const tpgdContext = 0x9b25

func (e *Encoder) byteOut() {
	if e.outByte == 0xff {
		e.rBlock()
		return
	}
	if e.creg < 0x8000000 {
		e.lBlock()
		return
	}
	e.outByte++
	if e.outByte != 0xff {
		e.lBlock()
		return
	}
	e.creg &= 0x7ffffff
	e.rBlock()
}

func (e *Encoder) emit() {
	if e.chunkLen == chunkSize {
		e.chunks = append(e.chunks, e.chunk)
		e.chunk = make([]byte, chunkSize)
		e.chunkLen = 0
	}
	e.chunk[e.chunkLen] = e.outByte
	e.chunkLen++
}

func (e *Encoder) EncodeInteger(proc Class, value int) (err error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Encode Integer:'%d' with Class: '%s'", value, proc)
	}
	if err = e.encodeInteger(proc, value); err != nil {
		return jbig2errors.Wrap(err, "EncodeInteger", "")
	}
	return nil
}

type intEncRangeS struct {
	low, high          int
	prefix, prefixBits uint8
	offset             uint16
	valueBits          uint8
}

func (e *Encoder) Init() {
	e.genericCtx = newCodingContext(contextSize)
	e.areg = 0x8000
	e.creg = 0
	e.ct = 12
	e.bytePos = -1
	e.outByte = 0
	e.chunkLen = 0
	e.chunk = make([]byte, chunkSize)
	for i := 0; i < len(e.intCtx); i++ {
		e.intCtx[i] = newCodingContext(512)
	}
	e.iaidCtx = nil
}

func (e *Encoder) lBlock() {
	if e.bytePos >= 0 {
		e.emit()
	}
	e.bytePos++
	e.outByte = uint8(e.creg >> 19)
	e.creg &= 0x7ffff
	e.ct = 8
}
