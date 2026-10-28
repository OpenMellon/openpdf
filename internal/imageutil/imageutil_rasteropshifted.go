// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
)

func rasterOpShifted(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	var (
		hasLeading        bool
		singleByte        bool
		leadingMask       byte
		leadingBits       int
		dstOffset         int
		dstLeading        int
		srcLeading        int
		hasMiddle         bool
		middleBytes       int
		dstMiddle         int
		srcMiddle         int
		hasTrailing       bool
		trailingMask      byte
		trailingBits      int
		dstTrailing       int
		srcTrailing       int
		shifted           byte
		srcLeadingBits    int
		srcOffset         int
		shiftLeftBits     uint
		shiftRightBits    uint
		shiftMask         byte
		firstShift        shift
		needNextByte      bool
		trailingNeedsNext bool
		y, i              int
	)
	if sx&7 != 0 {
		srcOffset = 8 - (sx & 7)
	}
	if dx&7 != 0 {
		dstOffset = 8 - (dx & 7)
	}
	if srcOffset == 0 && dstOffset == 0 {
		shiftMask = trailingBitMask[0]
	} else {
		if dstOffset > srcOffset {
			shiftLeftBits = uint(dstOffset - srcOffset)
		} else {
			shiftLeftBits = uint(8 - (srcOffset - dstOffset))
		}
		shiftRightBits = 8 - shiftLeftBits
		shiftMask = trailingBitMask[shiftLeftBits]
	}
	if (dx & 7) != 0 {
		hasLeading = true
		leadingBits = 8 - (dx & 7)
		leadingMask = trailingBitMask[leadingBits]
		dstLeading = dest.BytesPerLine*dy + (dx >> 3)
		srcLeading = src.BytesPerLine*sy + (sx >> 3)
		srcLeadingBits = 8 - (sx & 7)
		if leadingBits > srcLeadingBits {
			firstShift = shiftLeft
			if dw >= srcOffset {
				needNextByte = true
			}
		} else {
			firstShift = shiftRight
		}
	}
	if dw < leadingBits {
		singleByte = true
		leadingMask &= leadingBitMask[8-leadingBits+dw]
	}
	if !singleByte {
		middleBytes = (dw - leadingBits) >> 3
		if middleBytes != 0 {
			hasMiddle = true
			dstMiddle = dest.BytesPerLine*dy + ((dx + dstOffset) >> 3)
			srcMiddle = src.BytesPerLine*sy + ((sx + dstOffset) >> 3)
		}
	}
	trailingBits = (dx + dw) & 7
	if !(singleByte || trailingBits == 0) {
		hasTrailing = true
		trailingMask = leadingBitMask[trailingBits]
		dstTrailing = dest.BytesPerLine*dy + ((dx + dstOffset) >> 3) + middleBytes
		srcTrailing = src.BytesPerLine*sy + ((sx + dstOffset) >> 3) + middleBytes
		if trailingBits > int(shiftRightBits) {
			trailingNeedsNext = true
		}
	}
	switch op {
	case PixSrc:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted, leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted, trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrc:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^shifted, leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = ^shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^shifted, trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcOrDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted|dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] |= shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted|dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcAndDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted&dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] &= shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted&dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcXorDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted^dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] ^= shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrcOrDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^shifted|dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] |= ^shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^shifted|dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrcAndDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^shifted&dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] &= ^shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^shifted&dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcOrNotDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted|^dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = shifted | ^dest.Data[dstMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted|^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcAndNotDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted&^dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = shifted &^ dest.Data[dstMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted&^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcOrDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^(shifted | dest.Data[dstLeading]), leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = ^(shifted | dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(shifted | dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcAndDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^(shifted & dest.Data[dstLeading]), leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = ^(shifted & dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(shifted & dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcXorDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^(shifted ^ dest.Data[dstLeading]), leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = ^(shifted ^ dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(shifted ^ dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	default:
		common.Log.Debug("Operation: '%d' not permitted", op)
		return errors.New("raster operation not permitted")
	}
	return nil
}
