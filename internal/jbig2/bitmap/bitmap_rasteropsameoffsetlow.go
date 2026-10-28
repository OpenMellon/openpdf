// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"encoding/binary"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func rasterOpSameOffsetLow(dest *Bitmap, dx, dy, dw, dh int, op RasterOperator, src *Bitmap, sx, sy int) error {
	var (
		inOneByte       bool
		hasMiddle       bool
		midBytes        int
		dMid            int
		sMid            int
		trailingPartial bool
		lastMask        byte
		lastBits        int
		dLast           int
		sLast           int
		i, j            int
	)
	firstBits := 8 - (dx & 7)
	firstMask := rightMask8[firstBits]
	dFirst := dest.RowStride*dy + (dx >> 3)
	sFirst := src.RowStride*sy + (sx >> 3)
	if dw < firstBits {
		inOneByte = true
		firstMask &= leftMask8[8-firstBits+dw]
	}
	if !inOneByte {
		midBytes = (dw - firstBits) >> 3
		if midBytes > 0 {
			hasMiddle = true
			dMid = dFirst + 1
			sMid = sFirst + 1
		}
	}
	lastBits = (dx + dw) & 7
	if !inOneByte && lastBits != 0 {
		trailingPartial = true
		lastMask = leftMask8[lastBits]
		dLast = dFirst + 1 + midBytes
		sLast = sFirst + 1 + midBytes
	}
	switch op {
	case PixSrc:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], src.Data[sFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] = src.Data[sMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], src.Data[sLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotSrc:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^src.Data[sFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] = ^src.Data[sMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^src.Data[sLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcOrDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], src.Data[sFirst]|dest.Data[dFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] |= src.Data[sMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], src.Data[sLast]|dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcAndDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], src.Data[sFirst]&dest.Data[dFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] &= src.Data[sMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], src.Data[sLast]&dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcXorDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], src.Data[sFirst]^dest.Data[dFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] ^= src.Data[sMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], src.Data[sLast]^dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotSrcOrDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^src.Data[sFirst]|dest.Data[dFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] |= ^src.Data[sMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^src.Data[sLast]|dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotSrcAndDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^src.Data[sFirst]&dest.Data[dFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] &= ^src.Data[sMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^src.Data[sLast]&dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcOrNotDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], src.Data[sFirst]|^dest.Data[dFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] = src.Data[sMid+j] | ^dest.Data[dMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], src.Data[sLast]|^dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcAndNotDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], src.Data[sFirst]&^dest.Data[dFirst], firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] = src.Data[sMid+j] &^ dest.Data[dMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], src.Data[sLast]&^dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotPixSrcOrDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^(src.Data[sFirst] | dest.Data[dFirst]), firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] = ^(src.Data[sMid+j] | dest.Data[dMid+j])
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^(src.Data[sLast] | dest.Data[dLast]), lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotPixSrcAndDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^(src.Data[sFirst] & dest.Data[dFirst]), firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] = ^(src.Data[sMid+j] & dest.Data[dMid+j])
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^(src.Data[sLast] & dest.Data[dLast]), lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotPixSrcXorDst:
		for i = 0; i < dh; i++ {
			dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^(src.Data[sFirst] ^ dest.Data[dFirst]), firstMask)
			dFirst += dest.RowStride
			sFirst += src.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					dest.Data[dMid+j] = ^(src.Data[sMid+j] ^ dest.Data[dMid+j])
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^(src.Data[sLast] ^ dest.Data[dLast]), lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	default:
		common.Log.Debug("Invalid raster operator: %d", op)
		return jbig2errors.Error("rasterOpVAlignedLow", "invalid raster operator")
	}
	return nil
}

func popFillSegment(stack *basic.Stack) (seg *fillSegment, err error) {
	const processName = "popFillSegment"
	if stack == nil {
		return nil, jbig2errors.Error(processName, "nil stack provided")
	}
	if stack.Aux == nil {
		return nil, jbig2errors.Error(processName, "auxStack not defined")
	}
	item, ok := stack.Pop()
	if !ok {
		return nil, nil
	}
	popped, ok := item.(*fillSegment)
	if !ok {
		return nil, jbig2errors.Error(processName, "stack doesn't contain *fillSegment")
	}
	seg = &fillSegment{popped.x1, popped.x2, popped.y + popped.dy, popped.dy}
	stack.Aux.Push(popped)
	return seg, nil
}
func (s *byWidth) Less(i, j int) bool { return s.Values[i].Width < s.Values[j].Width }
func reduceRankBinary2Level3(src, dest *Bitmap, level int, tab []byte, size int) (err error) {
	const processName = "reduceRankBinary2Level3"
	var (
		srcOff, destOff, y, destY, x, destX, k, idx int
		bits1, bits2, andBits, orBits               uint32
		hiByte, loByte                              byte
		pair                                        uint16
	)
	buf1 := make([]byte, 4)
	buf2 := make([]byte, 4)
	for y = 0; y < src.Height-1; y, destY = y+2, destY+1 {
		srcOff = y * src.RowStride
		destOff = destY * dest.RowStride
		for x, destX = 0, 0; x < size; x, destX = x+4, destX+1 {
			for k = 0; k < 4; k++ {
				idx = srcOff + x + k
				if idx <= len(src.Data)-1 && idx < srcOff+src.RowStride {
					buf1[k] = src.Data[idx]
				} else {
					buf1[k] = 0x00
				}
				idx = srcOff + src.RowStride + x + k
				if idx <= len(src.Data)-1 && idx < srcOff+(2*src.RowStride) {
					buf2[k] = src.Data[idx]
				} else {
					buf2[k] = 0x00
				}
			}
			bits1 = binary.BigEndian.Uint32(buf1)
			bits2 = binary.BigEndian.Uint32(buf2)
			andBits = bits1 & bits2
			andBits |= andBits << 1
			orBits = bits1 | bits2
			orBits &= orBits << 1
			bits2 = andBits & orBits
			bits2 &= 0xaaaaaaaa
			bits1 = bits2 | (bits2 << 7)
			hiByte = byte(bits1 >> 24)
			loByte = byte((bits1 >> 8) & 0xff)
			idx = destOff + destX
			if idx+1 == len(dest.Data)-1 || idx+1 >= destOff+dest.RowStride {
				if err = dest.SetByte(idx, tab[hiByte]); err != nil {
					return jbig2errors.Wrapf(err, processName, "index: %d", idx)
				}
			} else {
				pair = (uint16(tab[hiByte]) << 8) | uint16(tab[loByte])
				if err = dest.setTwoBytes(idx, pair); err != nil {
					return jbig2errors.Wrapf(err, processName, "setting two bytes failed, index: %d", idx)
				}
				destX++
			}
		}
	}
	return nil
}
