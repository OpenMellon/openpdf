// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"testing"

	"github.com/OpenMellon/openpdf/common"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

func rasterOpByteAlignedLow(dest *Bitmap, dx, dy, dw, dh int, op RasterOperator, src *Bitmap, sx, sy int) error {
	var (
		lastMask   byte
		sFirst     int
		dFirst     int
		sIdx, dIdx int
		i, j       int
	)
	fullBytes := dw >> 3
	remBits := dw & 7
	if remBits > 0 {
		lastMask = leftMask8[remBits]
	}
	sFirst = src.RowStride*sy + (sx >> 3)
	dFirst = dest.RowStride*dy + (dx >> 3)
	switch op {
	case PixSrc:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] = src.Data[sIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], src.Data[sIdx], lastMask)
			}
		}
	case PixNotSrc:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] = ^src.Data[sIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], ^src.Data[sIdx], lastMask)
			}
		}
	case PixSrcOrDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] |= src.Data[sIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], src.Data[sIdx]|dest.Data[dIdx], lastMask)
			}
		}
	case PixSrcAndDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] &= src.Data[sIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], src.Data[sIdx]&dest.Data[dIdx], lastMask)
			}
		}
	case PixSrcXorDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] ^= src.Data[sIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], src.Data[sIdx]^dest.Data[dIdx], lastMask)
			}
		}
	case PixNotSrcOrDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] |= ^src.Data[sIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], ^src.Data[sIdx]|dest.Data[dIdx], lastMask)
			}
		}
	case PixNotSrcAndDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] &= ^src.Data[sIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], ^src.Data[sIdx]&dest.Data[dIdx], lastMask)
			}
		}
	case PixSrcOrNotDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] = src.Data[sIdx] | ^dest.Data[dIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], src.Data[sIdx]|^dest.Data[dIdx], lastMask)
			}
		}
	case PixSrcAndNotDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] = src.Data[sIdx] &^ dest.Data[dIdx]
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], src.Data[sIdx]&^dest.Data[dIdx], lastMask)
			}
		}
	case PixNotPixSrcOrDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] = ^(src.Data[sIdx] | dest.Data[dIdx])
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], ^(src.Data[sIdx] | dest.Data[dIdx]), lastMask)
			}
		}
	case PixNotPixSrcAndDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] = ^(src.Data[sIdx] & dest.Data[dIdx])
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], ^(src.Data[sIdx] & dest.Data[dIdx]), lastMask)
			}
		}
	case PixNotPixSrcXorDst:
		for i = 0; i < dh; i++ {
			sIdx = sFirst + i*src.RowStride
			dIdx = dFirst + i*dest.RowStride
			for j = 0; j < fullBytes; j++ {
				dest.Data[dIdx] = ^(src.Data[sIdx] ^ dest.Data[dIdx])
				dIdx++
				sIdx++
			}
			if remBits > 0 {
				dest.Data[dIdx] = mergeBytes(dest.Data[dIdx], ^(src.Data[sIdx] ^ dest.Data[dIdx]), lastMask)
			}
		}
	default:
		common.Log.Debug("Provided invalid raster operator: %v", op)
		return jbig2errors.Error("rasterOpByteAlignedLow", "invalid raster operator")
	}
	return nil
}

func (bm *Bitmap) SetDefaultPixel() {
	for i := range bm.Data {
		bm.Data[i] = byte(0xff)
	}
}
func (bm *Bitmap) GetByteIndex(x, y int) int { return y*bm.RowStride + (x >> 3) }
func (bm *Bitmap) createTemplate() *Bitmap {
	return &Bitmap{Width: bm.Width, Height: bm.Height, RowStride: bm.RowStride, Color: bm.Color, Text: bm.Text, BitmapNumber: bm.BitmapNumber, Special: bm.Special, Data: make([]byte, len(bm.Data))}
}

func TstVSymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(5, 5, []byte{0x88, 0x88, 0x88, 0x50, 0x20})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func (s *byWidth) Swap(i, j int) {
	s.Values[i], s.Values[j] = s.Values[j], s.Values[i]
	if s.Boxes != nil {
		s.Boxes[i], s.Boxes[j] = s.Boxes[j], s.Boxes[i]
	}
}

var popCountTab [256]uint8

func (bms *Bitmaps) GroupByWidth() (*BitmapsArray, error) {
	const processName = "GroupByWidth"
	if len(bms.Values) == 0 {
		return nil, jbig2errors.Error(processName, "no values provided")
	}
	result := &BitmapsArray{}
	bms.SortByWidth()
	curWidth := -1
	groupIdx := -1
	for i := 0; i < len(bms.Values); i++ {
		w := bms.Values[i].Width
		if w > curWidth {
			curWidth = w
			groupIdx++
			result.Values = append(result.Values, &Bitmaps{})
		}
		result.Values[groupIdx].AddBitmap(bms.Values[i])
	}
	return result, nil
}

func blitAligned(src, dst *Bitmap, startLine, endLine, dstIdx, srcStart, srcEnd int, op CombinationOperator) error {
	var line int
	advance := func() { line++; dstIdx += dst.RowStride; srcStart += src.RowStride; srcEnd += src.RowStride }
	for line = startLine; line < endLine; advance() {
		dIdx := dstIdx
		for sIdx := srcStart; sIdx <= srcEnd; sIdx++ {
			dstByte, err := dst.GetByte(dIdx)
			if err != nil {
				return err
			}
			srcByte, err := src.GetByte(sIdx)
			if err != nil {
				return err
			}
			if err = dst.SetByte(dIdx, combineBytes(dstByte, srcByte, op)); err != nil {
				return err
			}
			dIdx++
		}
	}
	return nil
}

type Color int

func (bm *Bitmap) removeBorderGeneral(left, right, top, bot int) (*Bitmap, error) {
	const processName = "removeBorderGeneral"
	if left < 0 || right < 0 || top < 0 || bot < 0 {
		return nil, jbig2errors.Error(processName, "negative broder remove values")
	}
	w, h := bm.Width, bm.Height
	newW := w - left - right
	newH := h - top - bot
	if newW <= 0 {
		return nil, jbig2errors.Errorf(processName, "width: %d must be > 0", newW)
	}
	if newH <= 0 {
		return nil, jbig2errors.Errorf(processName, "height: %d must be > 0", newH)
	}
	result, err := New(newW, newH)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	result.Color = bm.Color
	err = result.RasterOperation(0, 0, newW, newH, PixSrc, bm, left, top)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return result, nil
}

func (bm *Bitmap) Copy() *Bitmap {
	data := make([]byte, len(bm.Data))
	copy(data, bm.Data)
	return &Bitmap{Width: bm.Width, Height: bm.Height, RowStride: bm.RowStride, Data: data, Color: bm.Color, Text: bm.Text, BitmapNumber: bm.BitmapNumber, Special: bm.Special}
}

func TstWordBitmap(t *testing.T, scale ...int) *Bitmap {
	scaleFactor := 1
	if len(scale) > 0 {
		scaleFactor = scale[0]
	}
	space := 3
	width := 9 + 7 + 15 + 2*space
	height := 5 + space + 5
	bm, err := New(width*scaleFactor, height*scaleFactor)
	require.NoError(t, err)
	bms := &Bitmaps{}
	var xPtr *int
	space *= scaleFactor
	x := 0
	xPtr = &x
	y := 0
	sym := TstDSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstOSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, space)
	sym = TstISymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstTSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, space)
	sym = TstNSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstOSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstWSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 0)
	*xPtr = 0
	y = 5*scaleFactor + space
	sym = TstOSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstRSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, space)
	sym = TstNSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstESymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstVSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstESymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 1*scaleFactor)
	sym = TstRSymbol(t, scale...)
	TstAddSymbol(t, bms, sym, xPtr, y, 0)
	TstWriteSymbols(t, bms, bm)
	return bm
}
