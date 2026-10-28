// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"testing"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

func (bm *Bitmap) setEightBytes(byteIdx int, val uint64) error {
	remaining := bm.RowStride - (byteIdx % bm.RowStride)
	if bm.RowStride != bm.Width>>3 {
		remaining--
	}
	if remaining >= 8 {
		return bm.setEightFullBytes(byteIdx, val)
	}
	return bm.setEightPartlyBytes(byteIdx, remaining, val)
}

func (bm *Bitmap) RemoveBorder(borderSize int) (*Bitmap, error) {
	if borderSize == 0 {
		return bm.Copy(), nil
	}
	result, err := bm.removeBorderGeneral(borderSize, borderSize, borderSize, borderSize)
	if err != nil {
		return nil, jbig2errors.Wrap(err, "RemoveBorder", "")
	}
	return result, nil
}

func rasterOpNoSrcUnaligned(bm *Bitmap, dx, dy int, dw, dh int, op RasterOperator) {
	var (
		inOneByte       bool
		hasMiddle       bool
		midBytes        int
		dMid            int
		lastBits        int
		dLast           int
		trailingPartial bool
		lastMask        byte
	)
	firstBits := 8 - (dx & 7)
	firstMask := rightMask8[firstBits]
	dFirst := bm.RowStride*dy + (dx >> 3)
	if dw < firstBits {
		inOneByte = true
		firstMask &= leftMask8[8-firstBits+dw]
	}
	if !inOneByte {
		midBytes = (dw - firstBits) >> 3
		if midBytes != 0 {
			hasMiddle = true
			dMid = dFirst + 1
		}
	}
	lastBits = (dx + dw) & 7
	if !inOneByte && lastBits != 0 {
		trailingPartial = true
		lastMask = leftMask8[lastBits]
		dLast = dFirst + 1 + midBytes
	}
	var i, j int
	switch op {
	case PixClr:
		for i = 0; i < dh; i++ {
			bm.Data[dFirst] = mergeBytes(bm.Data[dFirst], 0x0, firstMask)
			dFirst += bm.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					bm.Data[dMid+j] = 0x0
				}
				dMid += bm.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				bm.Data[dLast] = mergeBytes(bm.Data[dLast], 0x0, lastMask)
				dLast += bm.RowStride
			}
		}
	case PixSet:
		for i = 0; i < dh; i++ {
			bm.Data[dFirst] = mergeBytes(bm.Data[dFirst], 0xff, firstMask)
			dFirst += bm.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					bm.Data[dMid+j] = 0xff
				}
				dMid += bm.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				bm.Data[dLast] = mergeBytes(bm.Data[dLast], 0xff, lastMask)
				dLast += bm.RowStride
			}
		}
	case PixNotDst:
		for i = 0; i < dh; i++ {
			bm.Data[dFirst] = mergeBytes(bm.Data[dFirst], ^bm.Data[dFirst], firstMask)
			dFirst += bm.RowStride
		}
		if hasMiddle {
			for i = 0; i < dh; i++ {
				for j = 0; j < midBytes; j++ {
					bm.Data[dMid+j] = ^bm.Data[dMid+j]
				}
				dMid += bm.RowStride
			}
		}
		if trailingPartial {
			for i = 0; i < dh; i++ {
				bm.Data[dLast] = mergeBytes(bm.Data[dLast], ^bm.Data[dLast], lastMask)
				dLast += bm.RowStride
			}
		}
	}
}

func rasterOpNoSrcAligned(bm *Bitmap, dx, dy int, dw, dh int, op RasterOperator) {
	var (
		dFirst   int
		lastMask byte
		i, j     int
		idx      int
	)
	fullBytes := dw >> 3
	remBits := dw & 7
	if remBits > 0 {
		lastMask = leftMask8[remBits]
	}
	dFirst = bm.RowStride*dy + (dx >> 3)
	switch op {
	case PixClr:
		for i = 0; i < dh; i++ {
			idx = dFirst + i*bm.RowStride
			for j = 0; j < fullBytes; j++ {
				bm.Data[idx] = 0x0
				idx++
			}
			if remBits > 0 {
				bm.Data[idx] = mergeBytes(bm.Data[idx], 0x0, lastMask)
			}
		}
	case PixSet:
		for i = 0; i < dh; i++ {
			idx = dFirst + i*bm.RowStride
			for j = 0; j < fullBytes; j++ {
				bm.Data[idx] = 0xff
				idx++
			}
			if remBits > 0 {
				bm.Data[idx] = mergeBytes(bm.Data[idx], 0xff, lastMask)
			}
		}
	case PixNotDst:
		for i = 0; i < dh; i++ {
			idx = dFirst + i*bm.RowStride
			for j = 0; j < fullBytes; j++ {
				bm.Data[idx] = ^bm.Data[idx]
				idx++
			}
			if remBits > 0 {
				bm.Data[idx] = mergeBytes(bm.Data[idx], ^bm.Data[idx], lastMask)
			}
		}
	}
}

type SelectionValue int

func (pts Points) YSorter() func(i, j int) bool {
	return func(i, j int) bool { return pts[i].Y < pts[j].Y }
}

func (bm *Bitmap) GetUnpaddedData() ([]byte, error) {
	remBits := uint(bm.Width & 0x07)
	if remBits == 0 {
		return bm.Data, nil
	}
	size := bm.Width * bm.Height
	if size%8 != 0 {
		size >>= 3
		size++
	} else {
		size >>= 3
	}
	data := make([]byte, size)
	w := bitwise.NewWriterMSB(data)
	const processName = "GetUnpaddedData"
	for y := 0; y < bm.Height; y++ {
		for x := 0; x < bm.RowStride; x++ {
			b := bm.Data[y*bm.RowStride+x]
			if x != bm.RowStride-1 {
				err := w.WriteByte(b)
				if err != nil {
					return nil, jbig2errors.Wrap(err, processName, "")
				}
				continue
			}
			for bit := uint(0); bit < remBits; bit++ {
				err := w.WriteBit(int(b >> (7 - bit) & 0x01))
				if err != nil {
					return nil, jbig2errors.Wrap(err, processName, "")
				}
			}
		}
	}
	return data, nil
}

const (
	_ LocationFilter = iota
	LocSelectWidth
	LocSelectHeight
	LocSelectXVal
	LocSelectYVal
	LocSelectIfEither
	LocSelectIfBoth
)

type byWidth Bitmaps

func (bms *Bitmaps) makeSizeIndicator(width, height int, filter LocationFilter, relation SizeComparison) (indicator *basic.NumSlice, err error) {
	const processName = "Bitmaps.makeSizeIndicator"
	if bms == nil {
		return nil, jbig2errors.Error(processName, "bitmaps 'b' not defined")
	}
	switch filter {
	case LocSelectWidth, LocSelectHeight, LocSelectIfEither, LocSelectIfBoth:
	default:
		return nil, jbig2errors.Errorf(processName, "provided invalid location filter type: %d", filter)
	}
	switch relation {
	case SizeSelectIfLT, SizeSelectIfGT, SizeSelectIfLTE, SizeSelectIfGTE, SizeSelectIfEQ:
	default:
		return nil, jbig2errors.Errorf(processName, "invalid relation: '%d'", relation)
	}
	indicator = &basic.NumSlice{}
	var (
		flag, w, h int
		bm         *Bitmap
	)
	for _, bm = range bms.Values {
		flag = 0
		w, h = bm.Width, bm.Height
		switch filter {
		case LocSelectWidth:
			if (relation == SizeSelectIfLT && w < width) || (relation == SizeSelectIfGT && w > width) || (relation == SizeSelectIfLTE && w <= width) || (relation == SizeSelectIfGTE && w >= width) || (relation == SizeSelectIfEQ && w == width) {
				flag = 1
			}
		case LocSelectHeight:
			if (relation == SizeSelectIfLT && h < height) || (relation == SizeSelectIfGT && h > height) || (relation == SizeSelectIfLTE && h <= height) || (relation == SizeSelectIfGTE && h >= height) || (relation == SizeSelectIfEQ && h == height) {
				flag = 1
			}
		case LocSelectIfEither:
			if (relation == SizeSelectIfLT && (w < width || h < height)) || (relation == SizeSelectIfGT && (w > width || h > height)) || (relation == SizeSelectIfLTE && (w <= width || h <= height)) || (relation == SizeSelectIfGTE && (w >= width || h >= height)) || (relation == SizeSelectIfEQ && (w == width || h == height)) {
				flag = 1
			}
		case LocSelectIfBoth:
			if (relation == SizeSelectIfLT && (w < width && h < height)) || (relation == SizeSelectIfGT && (w > width && h > height)) || (relation == SizeSelectIfLTE && (w <= width && h <= height)) || (relation == SizeSelectIfGTE && (w >= width && h >= height)) || (relation == SizeSelectIfEQ && (w == width && h == height)) {
				flag = 1
			}
		}
		indicator.AddInt(flag)
	}
	return indicator, nil
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

type Selection struct {
	Height, Width int
	Cx, Cy        int
	Name          string
	Data          [][]SelectionValue
}

func (bm *Bitmap) And(s *Bitmap) (result *Bitmap, err error) {
	const processName = "Bitmap.And"
	if bm == nil {
		return nil, jbig2errors.Error(processName, "'bitmap 'b' is nil")
	}
	if s == nil {
		return nil, jbig2errors.Error(processName, "bitmap 's' is nil")
	}
	if !bm.SizesEqual(s) {
		common.Log.Debug("%s - Bitmap 's' is not equal size with 'b'", processName)
	}
	if result, err = copyBitmap(result, bm); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "can't create 'd' bitmap")
	}
	if err = result.RasterOperation(0, 0, result.Width, result.Height, PixSrcAndDst, s, 0, 0); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return result, nil
}

func TstTSymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(5, 5, []byte{0xF8, 0x20, 0x20, 0x20, 0x20})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}
func clearLowBits(n uint, b byte) byte { return b >> n << n }
func (cp *ClassedPoints) Swap(i, j int) {
	cp.IntSlice[i], cp.IntSlice[j] = cp.IntSlice[j], cp.IntSlice[i]
}

func NewClassedPoints(points *Points, classes basic.IntSlice) (*ClassedPoints, error) {
	const processName = "NewClassedPoints"
	if points == nil {
		return nil, jbig2errors.Error(processName, "provided nil points")
	}
	if classes == nil {
		return nil, jbig2errors.Error(processName, "provided nil classes")
	}
	cp := &ClassedPoints{Points: points, IntSlice: classes}
	if err := cp.validateIntSlice(); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return cp, nil
}
func MakePixelSumTab8() []int { return makePixelSumTab8() }
func (pts Points) Get(i int) (Point, error) {
	if i > len(pts)-1 {
		return Point{}, jbig2errors.Errorf("Points.Get", "index: '%d' out of range", i)
	}
	return pts[i], nil
}

var (
	frameBitmap *Bitmap
	imageBitmap *Bitmap
)

func xor(dest, a, b *Bitmap) (*Bitmap, error) {
	const processName = "bitmap.xor"
	if a == nil {
		return nil, jbig2errors.Error(processName, "'b1' is nil")
	}
	if b == nil {
		return nil, jbig2errors.Error(processName, "'b2' is nil")
	}
	if dest == b {
		return nil, jbig2errors.Error(processName, "'d' == 'b2'")
	}
	if !a.SizesEqual(b) {
		common.Log.Debug("%s - Bitmap 'b1' is not equal size with 'b2'", processName)
	}
	var err error
	if dest, err = copyBitmap(dest, a); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "can't create 'd'")
	}
	if err = dest.RasterOperation(0, 0, dest.Width, dest.Height, PixSrcXorDst, b, 0, 0); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return dest, nil
}
