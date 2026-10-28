// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
)

const (
	PixSrc             RasterOperator = 0xc
	PixDst             RasterOperator = 0xa
	PixNotSrc          RasterOperator = 0x3
	PixNotDst          RasterOperator = 0x5
	PixClr             RasterOperator = 0x0
	PixSet             RasterOperator = 0xf
	PixSrcOrDst        RasterOperator = 0xe
	PixSrcAndDst       RasterOperator = 0x8
	PixSrcXorDst       RasterOperator = 0x6
	PixNotSrcOrDst     RasterOperator = 0xb
	PixNotSrcAndDst    RasterOperator = 0x2
	PixSrcOrNotDst     RasterOperator = 0xd
	PixSrcAndNotDst    RasterOperator = 0x4
	PixNotPixSrcOrDst  RasterOperator = 0x1
	PixNotPixSrcAndDst RasterOperator = 0x7
	PixNotPixSrcXorDst RasterOperator = 0x9
	PixPaint                          = PixSrcOrDst
	PixSubtract                       = PixNotSrcAndDst
	PixMask                           = PixSrcAndDst
)

func rasterOpSameShift(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	var (
		singleByte   bool
		hasMiddle    bool
		middleBytes  int
		dstMiddle    int
		srcMiddle    int
		hasTrailing  bool
		trailingMask byte
		trailingBits int
		dstTrailing  int
		srcTrailing  int
		y, i         int
	)
	leadingBits := 8 - (dx & 7)
	leadingMask := trailingBitMask[leadingBits]
	dstIdx := dest.BytesPerLine*dy + (dx >> 3)
	srcIdx := src.BytesPerLine*sy + (sx >> 3)
	if dw < leadingBits {
		singleByte = true
		leadingMask &= leadingBitMask[8-leadingBits+dw]
	}
	if !singleByte {
		middleBytes = (dw - leadingBits) >> 3
		if middleBytes > 0 {
			hasMiddle = true
			dstMiddle = dstIdx + 1
			srcMiddle = srcIdx + 1
		}
	}
	trailingBits = (dx + dw) & 7
	if !(singleByte || trailingBits == 0) {
		hasTrailing = true
		trailingMask = leadingBitMask[trailingBits]
		dstTrailing = dstIdx + 1 + middleBytes
		srcTrailing = srcIdx + 1 + middleBytes
	}
	switch op {
	case PixSrc:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrc:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^src.Data[srcIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^src.Data[srcTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcOrDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]|dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] |= src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]|dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcAndDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]&dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] &= src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]&dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcXorDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]^dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] ^= src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrcOrDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^src.Data[srcIdx]|dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] |= ^src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^src.Data[srcTrailing]|dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrcAndDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^src.Data[srcIdx]&dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] &= ^src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^src.Data[srcTrailing]&dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcOrNotDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]|^dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = src.Data[srcMiddle+i] | ^dest.Data[dstMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]|^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcAndNotDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]&^dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = src.Data[srcMiddle+i] &^ dest.Data[dstMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]&^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcOrDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] | dest.Data[dstIdx]), leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^(src.Data[srcMiddle+i] | dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(src.Data[srcTrailing] | dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcAndDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] & dest.Data[dstIdx]), leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^(src.Data[srcMiddle+i] & dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(src.Data[srcTrailing] & dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcXorDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] ^ dest.Data[dstIdx]), leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^(src.Data[srcMiddle+i] ^ dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(src.Data[srcTrailing] ^ dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	default:
		common.Log.Debug("Invalid raster operator: %d", op)
		return errors.New("invalid raster operator")
	}
	return nil
}
