// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"image"
	"sort"
	"testing"

	"github.com/OpenMellon/openpdf/common"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

func blitShifted(src, dst *Bitmap, startLine, endLine, dstIdx, srcStart, srcEnd, padding, xOffset, shift int, op CombinationOperator) error {
	var line int
	advance := func() {
		line++
		dstIdx += dst.RowStride
		srcStart += src.RowStride
		srcEnd += src.RowStride
	}
	for line = startLine; line < endLine; advance() {
		var acc uint16
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
			acc = (acc | uint16(srcByte)) << uint(shift)
			srcByte = byte(acc >> 8)
			if sIdx == srcEnd {
				srcByte = clearLowBits(uint(padding), srcByte)
			}
			if err = dst.SetByte(dIdx, combineBytes(dstByte, srcByte, op)); err != nil {
				return err
			}
			dIdx++
			acc <<= uint(xOffset)
		}
	}
	return nil
}
func (bms *Bitmaps) SortByWidth()            { sorter := (*byWidth)(bms); sort.Sort(sorter) }
func (cp *ClassedPoints) Less(i, j int) bool { return cp.lessFunc(i, j) }
func Extract(roi image.Rectangle, src *Bitmap) (*Bitmap, error) {
	result, err := New(roi.Dx(), roi.Dy())
	if err != nil {
		return nil, err
	}
	lshift := roi.Min.X & 0x07
	rshift := 8 - lshift
	trailBits := uint(8 - result.Width&0x07)
	firstIdx := src.GetByteIndex(roi.Min.X, roi.Min.Y)
	lastIdx := src.GetByteIndex(roi.Max.X-1, roi.Min.Y)
	fullRow := result.RowStride == lastIdx+1-firstIdx
	var dstOff int
	for y := roi.Min.Y; y < roi.Max.Y; y++ {
		srcIdx := firstIdx
		dstIdx := dstOff
		switch {
		case firstIdx == lastIdx:
			b, err2 := src.GetByte(srcIdx)
			if err2 != nil {
				return nil, err2
			}
			b <<= uint(lshift)
			err2 = result.SetByte(dstIdx, clearLowBits(trailBits, b))
			if err2 != nil {
				return nil, err2
			}
		case lshift == 0:
			for x := firstIdx; x <= lastIdx; x++ {
				b, err2 := src.GetByte(srcIdx)
				if err2 != nil {
					return nil, err2
				}
				srcIdx++
				if x == lastIdx && fullRow {
					b = clearLowBits(trailBits, b)
				}
				err2 = result.SetByte(dstIdx, b)
				if err2 != nil {
					return nil, err2
				}
				dstIdx++
			}
		default:
			err2 := extractShifted(src, result, uint(lshift), uint(rshift), trailBits, firstIdx, lastIdx, fullRow, srcIdx, dstIdx)
			if err2 != nil {
				return nil, err2
			}
		}
		firstIdx += src.RowStride
		lastIdx += src.RowStride
		dstOff += result.RowStride
	}
	return result, nil
}
func (bm *Bitmap) CreateTemplate() *Bitmap { return bm.createTemplate() }
func (bms *Bitmaps) Size() int             { return len(bms.Values) }
func rasterOperation(dest *Bitmap, dx, dy, dw, dh int, op RasterOperator, src *Bitmap, sx, sy int) error {
	const processName = "rasterOperation"
	if dest == nil {
		return jbig2errors.Error(processName, "nil 'dest' Bitmap")
	}
	if op == PixDst {
		return nil
	}
	switch op {
	case PixClr, PixSet, PixNotDst:
		rasterOpNoSrc(dest, dx, dy, dw, dh, op)
		return nil
	}
	if src == nil {
		common.Log.Debug("RasterOperation source bitmap is not defined")
		return jbig2errors.Error(processName, "nil 'src' bitmap")
	}
	if err := rasterOperationLow(dest, dx, dy, dw, dh, op, src, sx, sy); err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	return nil
}

func NewWithData(width, height int, data []byte) (*Bitmap, error) {
	const processName = "NewWithData"
	bm := newBitmapNoData(width, height)
	bm.Data = data
	if len(data) < height*bm.RowStride {
		return nil, jbig2errors.Errorf(processName, "invalid data length: %d - should be: %d", len(data), height*bm.RowStride)
	}
	return bm, nil
}

func NewWithUnpaddedData(width, height int, data []byte) (*Bitmap, error) {
	const processName = "NewWithUnpaddedData"
	bm := newBitmapNoData(width, height)
	bm.Data = data
	if minLen := ((width * height) + 7) >> 3; len(data) < minLen {
		return nil, jbig2errors.Errorf(processName, "invalid data length: '%d'. The data should contain at least: '%d' bytes", len(data), minLen)
	}
	if err := bm.addPadBits(); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return bm, nil
}

func CorrelationScore(bm1, bm2 *Bitmap, area1, area2 int, delX, delY float32, maxDiffW, maxDiffH int, tab []int) (score float64, err error) {
	const processName = "correlationScore"
	if bm1 == nil || bm2 == nil {
		return 0, jbig2errors.Error(processName, "provided nil bitmaps")
	}
	if tab == nil {
		return 0, jbig2errors.Error(processName, "'tab' not defined")
	}
	if area1 <= 0 || area2 <= 0 {
		return 0, jbig2errors.Error(processName, "areas must be greater than 0")
	}
	w1, h1 := bm1.Width, bm1.Height
	w2, h2 := bm2.Width, bm2.Height
	dw := absInt(w1 - w2)
	if dw > maxDiffW {
		return 0, nil
	}
	dh := absInt(h1 - h2)
	if dh > maxDiffH {
		return 0, nil
	}
	var dx, dy int
	if delX >= 0 {
		dx = int(delX + 0.5)
	} else {
		dx = int(delX - 0.5)
	}
	if delY >= 0 {
		dy = int(delY + 0.5)
	} else {
		dy = int(delY - 0.5)
	}
	yStart := maxInt(dy, 0)
	yEnd := minInt(h2+dy, h1)
	idx1 := bm1.RowStride * yStart
	idx2 := bm2.RowStride * (yStart - dy)
	xStart := maxInt(dx, 0)
	xEnd := minInt(w2+dx, w1)
	stride2 := bm2.RowStride
	var byteOffset1, byteOffset2 int
	if dx >= 8 {
		byteOffset1 = dx >> 3
		idx1 += byteOffset1
		xStart -= byteOffset1 << 3
		xEnd -= byteOffset1 << 3
		dx &= 7
	} else if dx <= -8 {
		byteOffset2 = -((dx + 7) >> 3)
		idx2 += byteOffset2
		stride2 -= byteOffset2
		dx += byteOffset2 << 3
	}
	if xStart >= xEnd || yStart >= yEnd {
		return 0, nil
	}
	wBytes := (xEnd + 7) >> 3
	var (
		byte1, byte2, anded byte
		count, x, y         int
	)
	switch {
	case dx == 0:
		for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
			for x = 0; x < wBytes; x++ {
				anded = bm1.Data[idx1+x] & bm2.Data[idx2+x]
				count += tab[anded]
			}
		}
	case dx > 0:
		if stride2 < wBytes {
			for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
				byte1, byte2 = bm1.Data[idx1], bm2.Data[idx2]>>uint(dx)
				anded = byte1 & byte2
				count += tab[anded]
				for x = 1; x < stride2; x++ {
					byte1, byte2 = bm1.Data[idx1+x], (bm2.Data[idx2+x]>>uint(dx))|(bm2.Data[idx2+x-1]<<uint(8-dx))
					anded = byte1 & byte2
					count += tab[anded]
				}
				byte1 = bm1.Data[idx1+x]
				byte2 = bm2.Data[idx2+x-1] << uint(8-dx)
				anded = byte1 & byte2
				count += tab[anded]
			}
		} else {
			for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
				byte1, byte2 = bm1.Data[idx1], bm2.Data[idx2]>>uint(dx)
				anded = byte1 & byte2
				count += tab[anded]
				for x = 1; x < wBytes; x++ {
					byte1 = bm1.Data[idx1+x]
					byte2 = (bm2.Data[idx2+x] >> uint(dx)) | (bm2.Data[idx2+x-1] << uint(8-dx))
					anded = byte1 & byte2
					count += tab[anded]
				}
			}
		}
	default:
		if wBytes < stride2 {
			for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
				for x = 0; x < wBytes; x++ {
					byte1 = bm1.Data[idx1+x]
					byte2 = bm2.Data[idx2+x] << uint(-dx)
					byte2 |= bm2.Data[idx2+x+1] >> uint(8+dx)
					anded = byte1 & byte2
					count += tab[anded]
				}
			}
		} else {
			for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
				for x = 0; x < wBytes-1; x++ {
					byte1 = bm1.Data[idx1+x]
					byte2 = bm2.Data[idx2+x] << uint(-dx)
					byte2 |= bm2.Data[idx2+x+1] >> uint(8+dx)
					anded = byte1 & byte2
					count += tab[anded]
				}
				byte1 = bm1.Data[idx1+x]
				byte2 = bm2.Data[idx2+x] << uint(-dx)
				anded = byte1 & byte2
				count += tab[anded]
			}
		}
	}
	score = float64(count) * float64(count) / (float64(area1) * float64(area2))
	return score, nil
}

func TstWordBitmapWithSpaces(t *testing.T, scale ...int) *Bitmap {
	scaleFactor := 1
	if len(scale) > 0 {
		scaleFactor = scale[0]
	}
	space := 3
	width := 9 + 7 + 15 + 2*space + 2*space
	height := 5 + space + 5 + 2*space
	bm, err := New(width*scaleFactor, height*scaleFactor)
	require.NoError(t, err)
	bms := &Bitmaps{}
	var xPtr *int
	space *= scaleFactor
	x := space
	xPtr = &x
	y := space
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
	*xPtr = space
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

func expandBinaryPower2(src *Bitmap, factor int) (*Bitmap, error) {
	const processName = "expandBinaryPower2"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source not defined")
	}
	if factor == 1 {
		return copyBitmap(nil, src)
	}
	if factor != 2 && factor != 4 && factor != 8 {
		return nil, jbig2errors.Error(processName, "factor must be in {2,4,8} range")
	}
	w := factor * src.Width
	h := factor * src.Height
	dest, err := New(w, h)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	switch factor {
	case 2:
		err = expandBinaryFactor2(dest, src)
	case 4:
		err = expandBinaryFactor4(dest, src)
	case 8:
		err = expandBinaryFactor8(dest, src)
	}
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return dest, nil
}

type RasterOperator int
