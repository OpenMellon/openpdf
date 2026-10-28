// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
)

func rasterOpByteAligned(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	var (
		lastMask       byte
		srcStart       int
		dstStart       int
		srcIdx, dstIdx int
		y, i           int
	)
	fullBytes := dw >> 3
	trailingBits := dw & 7
	if trailingBits > 0 {
		lastMask = leadingBitMask[trailingBits]
	}
	srcStart = src.BytesPerLine*sy + (sx >> 3)
	dstStart = dest.BytesPerLine*dy + (dx >> 3)
	switch op {
	case PixSrc:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx], lastMask)
			}
		}
	case PixNotSrc:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = ^src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^src.Data[srcIdx], lastMask)
			}
		}
	case PixSrcOrDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] |= src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]|dest.Data[dstIdx], lastMask)
			}
		}
	case PixSrcAndDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] &= src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]&dest.Data[dstIdx], lastMask)
			}
		}
	case PixSrcXorDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] ^= src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]^dest.Data[dstIdx], lastMask)
			}
		}
	case PixNotSrcOrDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] |= ^src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^src.Data[srcIdx]|dest.Data[dstIdx], lastMask)
			}
		}
	case PixNotSrcAndDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] &= ^src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^src.Data[srcIdx]&dest.Data[dstIdx], lastMask)
			}
		}
	case PixSrcOrNotDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = src.Data[srcIdx] | ^dest.Data[dstIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]|^dest.Data[dstIdx], lastMask)
			}
		}
	case PixSrcAndNotDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = src.Data[srcIdx] &^ dest.Data[dstIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]&^dest.Data[dstIdx], lastMask)
			}
		}
	case PixNotPixSrcOrDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = ^(src.Data[srcIdx] | dest.Data[dstIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] | dest.Data[dstIdx]), lastMask)
			}
		}
	case PixNotPixSrcAndDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = ^(src.Data[srcIdx] & dest.Data[dstIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] & dest.Data[dstIdx]), lastMask)
			}
		}
	case PixNotPixSrcXorDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = ^(src.Data[srcIdx] ^ dest.Data[dstIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] ^ dest.Data[dstIdx]), lastMask)
			}
		}
	default:
		common.Log.Debug("Provided invalid raster operator: %v", op)
		return errors.New("invalid raster operator")
	}
	return nil
}

func (b *ImageBase) setByte(idx int, v byte) error {
	if idx > len(b.Data)-1 {
		return errors.New("index out of range")
	}
	b.Data[idx] = v
	return nil
}
