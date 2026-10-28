// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"github.com/OpenMellon/openpdf/common"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

func rasterOpShiftedLow(dest *Bitmap, dx, dy, dw, dh int, op RasterOperator, src *Bitmap, sx, sy int) error {
	var (
		leadingPartial  bool
		inOneByte       bool
		firstMask       byte
		firstBits       int
		dAlign          int
		dFirst          int
		sFirst          int
		hasMiddle       bool
		midBytes        int
		dMid            int
		sMid            int
		trailingPartial bool
		lastMask        byte
		lastBits        int
		dLast           int
		sLast           int
		val             byte
		sFirstBits      int
		sAlign          int
		leftShift       uint
		rightShift      uint
		shiftMask       byte
		shiftDir        shift
		useNextFirst    bool
		useNextLast     bool
		i, j            int
	)
	if sx&7 != 0 {
		sAlign = 8 - (sx & 7)
	}
	if dx&7 != 0 {
		dAlign = 8 - (dx & 7)
	}
	if sAlign == 0 && dAlign == 0 {
		shiftMask = rightMask8[0]
	} else {
		if dAlign > sAlign {
			leftShift = uint(dAlign - sAlign)
		} else {
			leftShift = uint(8 - (sAlign - dAlign))
		}
		rightShift = 8 - leftShift
		shiftMask = rightMask8[leftShift]
	}
	if (dx & 7) != 0 {
		leadingPartial = true
		firstBits = 8 - (dx & 7)
		firstMask = rightMask8[firstBits]
		dFirst = dest.RowStride*dy + (dx >> 3)
		sFirst = src.RowStride*sy + (sx >> 3)
		sFirstBits = 8 - (sx & 7)
		if firstBits > sFirstBits {
			shiftDir = shiftLeft
			if dw >= sAlign {
				useNextFirst = true
			}
		} else {
			shiftDir = shiftRight
		}
	}
	if dw < firstBits {
		inOneByte = true
		firstMask &= leftMask8[8-firstBits+dw]
	}
	if !inOneByte {
		midBytes = (dw - firstBits) >> 3
		if midBytes != 0 {
			hasMiddle = true
			dMid = dest.RowStride*dy + ((dx + dAlign) >> 3)
			sMid = src.RowStride*sy + ((sx + dAlign) >> 3)
		}
	}
	lastBits = (dx + dw) & 7
	if !inOneByte && lastBits != 0 {
		trailingPartial = true
		lastMask = leftMask8[lastBits]
		dLast = dest.RowStride*dy + ((dx + dAlign) >> 3) + midBytes
		sLast = src.RowStride*sy + ((sx + dAlign) >> 3) + midBytes
		if lastBits > int(rightShift) {
			useNextLast = true
		}
	}
	switch op {
	case PixSrc:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], val, firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] = val
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], val, lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotSrc:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^val, firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] = ^val
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^val, lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcOrDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], val|dest.Data[dFirst], firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] |= val
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], val|dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcAndDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], val&dest.Data[dFirst], firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] &= val
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], val&dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcXorDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], val^dest.Data[dFirst], firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] ^= val
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], val^dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotSrcOrDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^val|dest.Data[dFirst], firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] |= ^val
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^val|dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotSrcAndDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^val&dest.Data[dFirst], firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] &= ^val
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^val&dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcOrNotDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], val|^dest.Data[dFirst], firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] = val | ^dest.Data[dMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], val|^dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixSrcAndNotDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], val&^dest.Data[dFirst], firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] = val &^ dest.Data[dMid+j]
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], val&^dest.Data[dLast], lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotPixSrcOrDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^(val | dest.Data[dFirst]), firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] = ^(val | dest.Data[dMid+j])
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^(val | dest.Data[dLast]), lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotPixSrcAndDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^(val & dest.Data[dFirst]), firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] = ^(val & dest.Data[dMid+j])
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^(val & dest.Data[dLast]), lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	case PixNotPixSrcXorDst:
		if leadingPartial {
			for i = 0; i < dh; i++ {
				if shiftDir == shiftLeft {
					val = src.Data[sFirst] << leftShift
					if useNextFirst {
						val = mergeBytes(val, src.Data[sFirst+1]>>rightShift, shiftMask)
					}
				} else {
					val = src.Data[sFirst] >> rightShift
				}
				dest.Data[dFirst] = mergeBytes(dest.Data[dFirst], ^(val ^ dest.Data[dFirst]), firstMask)
				dFirst += dest.RowStride
				sFirst += src.RowStride
			}
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					val = mergeBytes(src.Data[sMid+j]<<leftShift, src.Data[sMid+j+1]>>rightShift, shiftMask)
					dest.Data[dMid+j] = ^(val ^ dest.Data[dMid+j])
				}
				dMid += dest.RowStride
				sMid += src.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				val = src.Data[sLast] << leftShift
				if useNextLast {
					val = mergeBytes(val, src.Data[sLast+1]>>rightShift, shiftMask)
				}
				dest.Data[dLast] = mergeBytes(dest.Data[dLast], ^(val ^ dest.Data[dLast]), lastMask)
				dLast += dest.RowStride
				sLast += src.RowStride
			}
		}
	default:
		common.Log.Debug("Operation: '%d' not permitted", op)
		return jbig2errors.Error("rasterOpGeneralLow", "raster operation not permitted")
	}
	return nil
}
