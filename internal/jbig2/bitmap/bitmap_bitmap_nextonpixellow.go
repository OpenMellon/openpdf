// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"image"
	"testing"

	"github.com/OpenMellon/openpdf/common"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

func (bm *Bitmap) nextOnPixelLow(w, h, stride, x, y int) (pt image.Point, found bool, err error) {
	const processName = "Bitmap.nextOnPixelLow"
	var (
		i int
		b byte
	)
	lineOff := y * stride
	idx := lineOff + (x / 8)
	if b, err = bm.GetByte(idx); err != nil {
		return pt, false, jbig2errors.Wrap(err, processName, "xStart and yStart out of range")
	}
	if b != 0 {
		lastX := x - (x % 8) + 7
		for i = x; i <= lastX && i < w; i++ {
			if bm.GetPixel(i, y) {
				pt.X = i
				pt.Y = y
				return pt, true, nil
			}
		}
	}
	byteIdx := (x / 8) + 1
	i = 8 * byteIdx
	var k int
	for idx = lineOff + byteIdx; i < w; idx, i = idx+1, i+8 {
		if b, err = bm.GetByte(idx); err != nil {
			return pt, false, jbig2errors.Wrap(err, processName, "rest of the line byte")
		}
		if b == 0 {
			continue
		}
		for k = 0; k < 8 && i < w; k, i = k+1, i+1 {
			if bm.GetPixel(i, y) {
				pt.X = i
				pt.Y = y
				return pt, true, nil
			}
		}
	}
	for row := y + 1; row < h; row++ {
		lineOff = row * stride
		for idx, i = lineOff, 0; i < w; idx, i = idx+1, i+8 {
			if b, err = bm.GetByte(idx); err != nil {
				return pt, false, jbig2errors.Wrap(err, processName, "following lines")
			}
			if b == 0 {
				continue
			}
			for k = 0; k < 8 && i < w; k, i = k+1, i+1 {
				if bm.GetPixel(i, row) {
					pt.X = i
					pt.Y = row
					return pt, true, nil
				}
			}
		}
	}
	return pt, false, nil
}

func (bm *Bitmap) centroid(centTab, sumTab []int) (Point, error) {
	pt := Point{}
	bm.setPadBits(0)
	if len(centTab) == 0 {
		centTab = makePixelCentroidTab8()
	}
	if len(sumTab) == 0 {
		sumTab = makePixelSumTab8()
	}
	var xSum, ySum, total, rowCount, y, x int
	var b byte
	for y = 0; y < bm.Height; y++ {
		off := bm.RowStride * y
		rowCount = 0
		for x = 0; x < bm.RowStride; x++ {
			b = bm.Data[off+x]
			if b != 0 {
				rowCount += sumTab[b]
				xSum += centTab[b] + x*8*sumTab[b]
			}
		}
		total += rowCount
		ySum += rowCount * y
	}
	if total != 0 {
		pt.X = float32(xSum) / float32(total)
		pt.Y = float32(ySum) / float32(total)
	}
	return pt, nil
}

func extractShifted(src, dest *Bitmap, lshift, rshift, trailBits uint, firstIdx, lastIdx int, fullRow bool, srcIdx, dstIdx int) error {
	for x := firstIdx; x < lastIdx; x++ {
		if srcIdx+1 < len(src.Data) {
			isLast := x+1 == lastIdx
			hi, err := src.GetByte(srcIdx)
			if err != nil {
				return err
			}
			srcIdx++
			hi <<= lshift
			lo, err := src.GetByte(srcIdx)
			if err != nil {
				return err
			}
			lo >>= rshift
			merged := hi | lo
			if isLast && !fullRow {
				merged = clearLowBits(trailBits, merged)
			}
			err = dest.SetByte(dstIdx, merged)
			if err != nil {
				return err
			}
			dstIdx++
			if isLast && fullRow {
				b, err2 := src.GetByte(srcIdx)
				if err2 != nil {
					return err2
				}
				b <<= lshift
				merged = clearLowBits(trailBits, b)
				if err2 = dest.SetByte(dstIdx, merged); err2 != nil {
					return err2
				}
			}
			continue
		}
		b, err := src.GetByte(srcIdx)
		if err != nil {
			common.Log.Debug("Getting the value at: %d failed: %s", srcIdx, err)
			return err
		}
		b <<= lshift
		srcIdx++
		err = dest.SetByte(dstIdx, b)
		if err != nil {
			return err
		}
		dstIdx++
	}
	return nil
}
func TstFrameBitmap() *Bitmap { return frameBitmap.Copy() }
func (bm *Bitmap) setFourBytes(idx int, val uint32) error {
	if idx+3 > len(bm.Data)-1 {
		return jbig2errors.Errorf("setFourBytes", "index: '%d' out of range", idx)
	}
	bm.Data[idx] = byte((val & 0xff000000) >> 24)
	bm.Data[idx+1] = byte((val & 0xff0000) >> 16)
	bm.Data[idx+2] = byte((val & 0xff00) >> 8)
	bm.Data[idx+3] = byte(val & 0xff)
	return nil
}

func (bm *Bitmap) ConnComponents(bms *Bitmaps, connectivity int) (boxes *Boxes, err error) {
	const processName = "Bitmap.ConnComponents"
	if bm == nil {
		return nil, jbig2errors.Error(processName, "provided empty 'b' bitmap")
	}
	if connectivity != 4 && connectivity != 8 {
		return nil, jbig2errors.Error(processName, "connectivity not 4 or 8")
	}
	if bms == nil {
		if boxes, err = bm.connComponentsBB(connectivity); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
	} else {
		if boxes, err = bm.connComponentsBitmapsBB(bms, connectivity); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
	}
	return boxes, nil
}

func Centroids(bms []*Bitmap) (*Points, error) {
	pts := make([]Point, len(bms))
	centTab := makePixelCentroidTab8()
	sumTab := makePixelSumTab8()
	var err error
	for i, bm := range bms {
		pts[i], err = bm.centroid(centTab, sumTab)
		if err != nil {
			return nil, err
		}
	}
	result := Points(pts)
	return &result, nil
}

const (
	SelDontCare SelectionValue = iota
	SelHit
	SelMiss
)

func init() {
	const processName = "bitmaps.initialization"
	var err error
	frameBitmap, err = New(50, 40)
	if err != nil {
		panic(jbig2errors.Wrap(err, processName, "frameBitmap"))
	}
	frameBitmap, err = frameBitmap.AddBorder(2, 1)
	if err != nil {
		panic(jbig2errors.Wrap(err, processName, "frameBitmap"))
	}
	imageBitmap, err = NewWithData(50, 22, imageBitmapData)
	if err != nil {
		panic(jbig2errors.Wrap(err, processName, "imageBitmap"))
	}
}

func expandBinaryPower2Low(dest *Bitmap, src *Bitmap, factor int) (err error) {
	const processName = "expandBinaryPower2Low"
	switch factor {
	case 2:
		err = expandBinaryFactor2(dest, src)
	case 4:
		err = expandBinaryFactor4(dest, src)
	case 8:
		err = expandBinaryFactor8(dest, src)
	default:
		return jbig2errors.Error(processName, "expansion factor not in {2,4,8} range")
	}
	if err != nil {
		err = jbig2errors.Wrap(err, processName, "")
	}
	return err
}

type MorphProcess struct {
	Operation MorphOperation
	Arguments []int
}

func (bm *Bitmap) SetByte(index int, v byte) error {
	if index > len(bm.Data)-1 || index < 0 {
		return jbig2errors.Errorf("SetByte", "index out of range: %d", index)
	}
	bm.Data[index] = v
	return nil
}

func TstWriteSymbols(t *testing.T, bms *Bitmaps, src *Bitmap) {
	for i := 0; i < bms.Size(); i++ {
		sym := bms.Values[i]
		box := bms.Boxes[i]
		err := src.RasterOperation(box.Min.X, box.Min.Y, sym.Width, sym.Height, PixSrc, sym, 0, 0)
		require.NoError(t, err)
	}
}

func expandBinaryFactor2(dest, src *Bitmap) (err error) {
	const processName = "expandBinaryFactor2"
	srcStride := src.RowStride
	destStride := dest.RowStride
	var (
		b                              byte
		expanded                       uint16
		srcOff, destOff, y, x, destIdx int
	)
	for y = 0; y < src.Height; y++ {
		srcOff = y * srcStride
		destOff = 2 * y * destStride
		for x = 0; x < srcStride; x++ {
			b = src.Data[srcOff+x]
			expanded = expandTab2x[b]
			destIdx = destOff + x*2
			if dest.RowStride != src.RowStride*2 && (x+1)*2 > dest.RowStride {
				err = dest.SetByte(destIdx, byte(expanded>>8))
			} else {
				err = dest.setTwoBytes(destIdx, expanded)
			}
			if err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
		}
		for x = 0; x < destStride; x++ {
			destIdx = destOff + destStride + x
			b = dest.Data[destOff+x]
			if err = dest.SetByte(destIdx, b); err != nil {
				return jbig2errors.Wrapf(err, processName, "copy doubled line: '%d', Byte: '%d'", destOff+x, destOff+destStride+x)
			}
		}
	}
	return nil
}

func seedFillBinary(dest, seed, mask *Bitmap, connectivity int) (*Bitmap, error) {
	const processName = "seedFillBinary"
	if seed == nil {
		return nil, jbig2errors.Error(processName, "source bitmap is nil")
	}
	if mask == nil {
		return nil, jbig2errors.Error(processName, "'mask' bitmap is nil")
	}
	if connectivity != 4 && connectivity != 8 {
		return nil, jbig2errors.Error(processName, "connectivity not in range {4,8}")
	}
	var err error
	dest, err = copyBitmap(dest, seed)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "copy source to 'd'")
	}
	prev := seed.createTemplate()
	mask.setPadBits(0)
	for i := 0; i < maxFillIterations; i++ {
		prev, err = copyBitmap(prev, dest)
		if err != nil {
			return nil, jbig2errors.Wrapf(err, processName, "iteration: %d", i)
		}
		if err = seedfillBinaryLow(dest, mask, connectivity); err != nil {
			return nil, jbig2errors.Wrapf(err, processName, "iteration: %d", i)
		}
		if prev.Equals(dest) {
			break
		}
	}
	return dest, nil
}

func (ba *BitmapsArray) GetBitmaps(i int) (*Bitmaps, error) {
	const processName = "BitmapsArray.GetBitmaps"
	if ba == nil {
		return nil, jbig2errors.Error(processName, "provided nil 'BitmapsArray'")
	}
	if i > len(ba.Values)-1 {
		return nil, jbig2errors.Errorf(processName, "index: '%d' out of range", i)
	}
	return ba.Values[i], nil
}

func TstWSymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(5, 5, []byte{0x88, 0x88, 0xA8, 0xD8, 0x88})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func (bm *Bitmap) clearAll() error {
	return bm.RasterOperation(0, 0, bm.Width, bm.Height, PixClr, nil, 0, 0)
}
func (bm *Bitmap) SetPadBits(value int) { bm.setPadBits(value) }
func rasterOperationLow(dest *Bitmap, dx, dy int, dw, dh int, op RasterOperator, src *Bitmap, sx, sy int) error {
	var overW, srcOverW, overH, srcOverH int
	if dx < 0 {
		sx -= dx
		dw += dx
		dx = 0
	}
	if sx < 0 {
		dx -= sx
		dw += sx
		sx = 0
	}
	overW = dx + dw - dest.Width
	if overW > 0 {
		dw -= overW
	}
	srcOverW = sx + dw - src.Width
	if srcOverW > 0 {
		dw -= srcOverW
	}
	if dy < 0 {
		sy -= dy
		dh += dy
		dy = 0
	}
	if sy < 0 {
		dy -= sy
		dh += sy
		sy = 0
	}
	overH = dy + dh - dest.Height
	if overH > 0 {
		dh -= overH
	}
	srcOverH = sy + dh - src.Height
	if srcOverH > 0 {
		dh -= srcOverH
	}
	if dw <= 0 || dh <= 0 {
		return nil
	}
	var err error
	switch {
	case dx&7 == 0 && sx&7 == 0:
		err = rasterOpByteAlignedLow(dest, dx, dy, dw, dh, op, src, sx, sy)
	case dx&7 == sx&7:
		err = rasterOpSameOffsetLow(dest, dx, dy, dw, dh, op, src, sx, sy)
	default:
		err = rasterOpShiftedLow(dest, dx, dy, dw, dh, op, src, sx, sy)
	}
	if err != nil {
		return jbig2errors.Wrap(err, "rasterOpLow", "")
	}
	return nil
}
