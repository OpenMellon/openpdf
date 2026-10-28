// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

func (bm *Bitmap) nextOnPixel(x, y int) (pt image.Point, found bool, err error) {
	const processName = "nextOnPixel"
	pt, found, err = bm.nextOnPixelLow(bm.Width, bm.Height, bm.RowStride, x, y)
	if err != nil {
		return pt, false, jbig2errors.Wrap(err, processName, "")
	}
	return pt, found, nil
}

func (boxes *Boxes) selectWithIndicator(indicator *basic.NumSlice) (result *Boxes, err error) {
	const processName = "Boxes.selectWithIndicator"
	if boxes == nil {
		return nil, jbig2errors.Error(processName, "boxes 'b' not defined")
	}
	if indicator == nil {
		return nil, jbig2errors.Error(processName, "'na' not defined")
	}
	if len(*indicator) != len(*boxes) {
		return nil, jbig2errors.Error(processName, "boxes 'b' has different size than 'na'")
	}
	var val, count int
	for i := 0; i < len(*indicator); i++ {
		if val, err = indicator.GetInt(i); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "checking count")
		}
		if val == 1 {
			count++
		}
	}
	if count == len(*boxes) {
		return boxes, nil
	}
	selected := Boxes{}
	for i := 0; i < len(*indicator); i++ {
		val = int((*indicator)[i])
		if val == 0 {
			continue
		}
		selected = append(selected, (*boxes)[i])
	}
	result = &selected
	return result, nil
}

func reduceRankBinary2(src *Bitmap, level int, tab []byte) (dest *Bitmap, err error) {
	const processName = "reduceRankBinary2"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source bitmap not defined")
	}
	if level < 1 || level > 4 {
		return nil, jbig2errors.Error(processName, "level must be in set {1,2,3,4}")
	}
	if src.Height <= 1 {
		return nil, jbig2errors.Errorf(processName, "source height must be at least '2' - is: '%d'", src.Height)
	}
	dest, err = New(src.Width/2, src.Height/2)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if tab == nil {
		tab = makeSubsampleTab2x()
	}
	size := minInt(src.RowStride, 2*dest.RowStride)
	switch level {
	case 1:
		err = reduceRankBinary2Level1(src, dest, level, tab, size)
	case 2:
		err = reduceRankBinary2Level2(src, dest, level, tab, size)
	case 3:
		err = reduceRankBinary2Level3(src, dest, level, tab, size)
	case 4:
		err = reduceRankBinary2Level4(src, dest, level, tab, size)
	}
	if err != nil {
		return nil, err
	}
	return dest, nil
}

func (cp *ClassedPoints) ySortFunction() func(i int, j int) bool {
	return func(i, j int) bool { return cp.YAtIndex(i) < cp.YAtIndex(j) }
}
func (ba *BitmapsArray) AddBox(box *image.Rectangle) { ba.Boxes = append(ba.Boxes, box) }
func (bm *Bitmap) Equivalent(s *Bitmap) bool         { return bm.equivalent(s) }
func seedFillStackBB4(s *Bitmap, stack *basic.Stack, x, y int) (bb *image.Rectangle, err error) {
	const processName = "seedFillStackBB"
	if s == nil {
		return nil, jbig2errors.Error(processName, "provided nil 's' Bitmap")
	}
	if stack == nil {
		return nil, jbig2errors.Error(processName, "provided nil 'stack'")
	}
	w, h := s.Width, s.Height
	xMax := w - 1
	yMax := h - 1
	if x < 0 || x > xMax || y < 0 || y > yMax || !s.GetPixel(x, y) {
		return nil, nil
	}
	box := image.Rect(100000, 100000, 0, 0)
	if err = pushFillSegmentBoundingBox(stack, x, x, y, 1, yMax, &box); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "initial push")
	}
	if err = pushFillSegmentBoundingBox(stack, x, x, y+1, -1, yMax, &box); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "2nd initial push")
	}
	box.Min.X, box.Max.X = x, x
	box.Min.Y, box.Max.Y = y, y
	var (
		seg      *fillSegment
		runStart int
	)
	for stack.Len() > 0 {
		if seg, err = popFillSegment(stack); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		y = seg.y
		for x = seg.x1 - 1; x >= 0 && s.GetPixel(x, y); x-- {
			if err = s.SetPixel(x, y, 0); err != nil {
				return nil, jbig2errors.Wrap(err, processName, "1st set")
			}
		}
		if x >= seg.x1-1 {
			for {
				for x++; x <= seg.x2+1 && x <= xMax && !s.GetPixel(x, y); x++ {
				}
				runStart = x
				if x > seg.x2+1 || x > xMax {
					break
				}
				for ; x <= xMax && s.GetPixel(x, y); x++ {
					if err = s.SetPixel(x, y, 0); err != nil {
						return nil, jbig2errors.Wrap(err, processName, "2nd set")
					}
				}
				if err = pushFillSegmentBoundingBox(stack, runStart, x-1, seg.y, seg.dy, yMax, &box); err != nil {
					return nil, jbig2errors.Wrap(err, processName, "normal push")
				}
				if x > seg.x2 {
					if err = pushFillSegmentBoundingBox(stack, seg.x2+1, x-1, seg.y, -seg.dy, yMax, &box); err != nil {
						return nil, jbig2errors.Wrap(err, processName, "leak on right side")
					}
				}
			}
			continue
		}
		runStart = x + 1
		if runStart < seg.x1 {
			if err = pushFillSegmentBoundingBox(stack, runStart, seg.x1-1, seg.y, -seg.dy, yMax, &box); err != nil {
				return nil, jbig2errors.Wrap(err, processName, "leak on left side")
			}
		}
		x = seg.x1
		for {
			for ; x <= xMax && s.GetPixel(x, y); x++ {
				if err = s.SetPixel(x, y, 0); err != nil {
					return nil, jbig2errors.Wrap(err, processName, "2nd set")
				}
			}
			if err = pushFillSegmentBoundingBox(stack, runStart, x-1, seg.y, seg.dy, yMax, &box); err != nil {
				return nil, jbig2errors.Wrap(err, processName, "normal push")
			}
			if x > seg.x2 {
				if err = pushFillSegmentBoundingBox(stack, seg.x2+1, x-1, seg.y, -seg.dy, yMax, &box); err != nil {
					return nil, jbig2errors.Wrap(err, processName, "leak on right side")
				}
			}
			for x++; x <= seg.x2+1 && x <= xMax && !s.GetPixel(x, y); x++ {
			}
			runStart = x
			if x > seg.x2+1 || x > xMax {
				break
			}
		}
	}
	box.Max.X++
	box.Max.Y++
	return &box, nil
}

func makeSubsampleTab2x() (tab []byte) {
	tab = make([]byte, 256)
	for i := 0; i < 256; i++ {
		b := byte(i)
		tab[b] = (b & 0x01) | ((b & 0x04) >> 1) | ((b & 0x10) >> 2) | ((b & 0x40) >> 3) | ((b & 0x02) << 3) | ((b & 0x08) << 2) | ((b & 0x20) << 1) | (b & 0x80)
	}
	return tab
}

type Bitmap struct {
	Width, Height            int
	BitmapNumber             int
	RowStride                int
	Data                     []byte
	Color                    Color
	Special                  int
	Text                     string
	XResolution, YResolution int
}

func verifyMorphProcesses(procs ...MorphProcess) (err error) {
	const processName = "verifyMorphProcesses"
	var netReduction, borderSize int
	for i, proc := range procs {
		if err = proc.verify(i, &netReduction, &borderSize); err != nil {
			return jbig2errors.Wrap(err, processName, "")
		}
	}
	if borderSize != 0 && netReduction != 0 {
		return jbig2errors.Error(processName, "Morph sequence - border added but net reduction not 0")
	}
	return nil
}
func mergeBytes(dst, src, mask byte) byte { return (dst &^ mask) | (src & mask) }
func TstImageBitmap() *Bitmap             { return imageBitmap.Copy() }
func dilate(dest *Bitmap, src *Bitmap, sel *Selection) (*Bitmap, error) {
	var (
		temp *Bitmap
		err  error
	)
	dest, err = processMorphArgs1(dest, src, sel, &temp)
	if err != nil {
		return nil, err
	}
	if err = dest.clearAll(); err != nil {
		return nil, err
	}
	var selVal SelectionValue
	for i := 0; i < sel.Height; i++ {
		for j := 0; j < sel.Width; j++ {
			selVal = sel.Data[i][j]
			if selVal == SelHit {
				if err = dest.RasterOperation(j-sel.Cx, i-sel.Cy, src.Width, src.Height, PixSrcOrDst, temp, 0, 0); err != nil {
					return nil, err
				}
			}
		}
	}
	return dest, nil
}

func TstDSymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(4, 5, []byte{0xf0, 0x90, 0x90, 0x90, 0xE0})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

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

func (bms *Bitmaps) HeightSorter() func(i, j int) bool {
	return func(i, j int) bool {
		less := bms.Values[i].Height < bms.Values[j].Height
		common.Log.Debug("Height: %v < %v = %v", bms.Values[i].Height, bms.Values[j].Height, less)
		return less
	}
}

func seedfillBinaryLow4(seed, mask *Bitmap, h, wpl int) (err error) {
	const processName = "seedfillBinaryLow4"
	var (
		i, j, seedLine, maskLine                                                int
		seedByte, maskByte, aboveByte, leftByte, belowByte, rightByte, prevByte byte
	)
	for i = 0; i < h; i++ {
		seedLine = i * seed.RowStride
		maskLine = i * mask.RowStride
		for j = 0; j < wpl; j++ {
			seedByte, err = seed.GetByte(seedLine + j)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "first get")
			}
			maskByte, err = mask.GetByte(maskLine + j)
			if err != nil {
				return jbig2errors.Wrap(err, processName, "second get")
			}
			if i > 0 {
				aboveByte, err = seed.GetByte(seedLine - seed.RowStride + j)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "i > 0")
				}
				seedByte |= aboveByte
			}
			if j > 0 {
				leftByte, err = seed.GetByte(seedLine + j - 1)
				if err != nil {
					return jbig2errors.Wrap(err, processName, "j > 0")
				}
				seedByte |= leftByte << 7
			}
			seedByte &= maskByte
			if seedByte == 0 || (^seedByte) == 0 {
				if err = seed.SetByte(seedLine+j, seedByte); err != nil {
					return jbig2errors.Wrap(err, processName, "bt == 0 || (^bt) == 0")
				}
				continue
			}
			for {
				prevByte = seedByte
				seedByte = (seedByte | (seedByte >> 1) | (seedByte << 1)) & maskByte
				if (seedByte ^ prevByte) == 0 {
					if err = seed.SetByte(seedLine+j, seedByte); err != nil {
						return jbig2errors.Wrap(err, processName, "setting prev byte")
					}
					break
				}
			}
		}
	}
	for i = h - 1; i >= 0; i-- {
		seedLine = i * seed.RowStride
		maskLine = i * mask.RowStride
		for j = wpl - 1; j >= 0; j-- {
			if seedByte, err = seed.GetByte(seedLine + j); err != nil {
				return jbig2errors.Wrap(err, processName, "reverse first get")
			}
			if maskByte, err = mask.GetByte(maskLine + j); err != nil {
				return jbig2errors.Wrap(err, processName, "reverse get mask byte")
			}
			if i < h-1 {
				if belowByte, err = seed.GetByte(seedLine + seed.RowStride + j); err != nil {
					return jbig2errors.Wrap(err, processName, "reverse i < h -1")
				}
				seedByte |= belowByte
			}
			if j < wpl-1 {
				if rightByte, err = seed.GetByte(seedLine + j + 1); err != nil {
					return jbig2errors.Wrap(err, processName, "reverse j < wpl - 1")
				}
				seedByte |= rightByte >> 7
			}
			seedByte &= maskByte
			if seedByte == 0 || (^seedByte) == 0 {
				if err = seed.SetByte(seedLine+j, seedByte); err != nil {
					return jbig2errors.Wrap(err, processName, "setting masked byte failed")
				}
				continue
			}
			for {
				prevByte = seedByte
				seedByte = (seedByte | (seedByte >> 1) | (seedByte << 1)) & maskByte
				if (seedByte ^ prevByte) == 0 {
					if err = seed.SetByte(seedLine+j, seedByte); err != nil {
						return jbig2errors.Wrap(err, processName, "reverse setting prev byte")
					}
					break
				}
			}
		}
	}
	return nil
}

func init() {
	for i := 0; i < 256; i++ {
		popCountTab[i] = uint8(i&0x1) + (uint8(i>>1) & 0x1) + (uint8(i>>2) & 0x1) + (uint8(i>>3) & 0x1) + (uint8(i>>4) & 0x1) + (uint8(i>>5) & 0x1) + (uint8(i>>6) & 0x1) + (uint8(i>>7) & 0x1)
	}
}

func Centroid(bm *Bitmap, centTab, sumTab []int) (Point, error) { return bm.centroid(centTab, sumTab) }

func (bm *Bitmap) setBit(bitIdx int) { bm.Data[(bitIdx >> 3)] |= 0x80 >> uint(bitIdx&7) }

func (bm *Bitmap) GetComponents(components Component, maxWidth, maxHeight int) (bitmaps *Bitmaps, boxes *Boxes, err error) {
	const processName = "Bitmap.GetComponents"
	if bm == nil {
		return nil, nil, jbig2errors.Error(processName, "source Bitmap not defined.")
	}
	switch components {
	case ComponentConn, ComponentCharacters, ComponentWords:
	default:
		return nil, nil, jbig2errors.Error(processName, "invalid components parameter")
	}
	if bm.Zero() {
		boxes = &Boxes{}
		bitmaps = &Bitmaps{}
		return bitmaps, boxes, nil
	}
	switch components {
	case ComponentConn:
		bitmaps = &Bitmaps{}
		if boxes, err = bm.ConnComponents(bitmaps, 8); err != nil {
			return nil, nil, jbig2errors.Wrap(err, processName, "no preprocessing")
		}
	case ComponentCharacters:
		closed, err2 := MorphSequence(bm, MorphProcess{Operation: MopClosing, Arguments: []int{1, 6}})
		if err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "characters preprocessing")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("ComponentCharacters bitmap after closing: %s", closed.String())
		}
		connBitmaps := &Bitmaps{}
		boxes, err2 = closed.ConnComponents(connBitmaps, 8)
		if err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "characters preprocessing")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("ComponentCharacters bitmap after connectivity: %s", connBitmaps.String())
		}
		if bitmaps, err2 = connBitmaps.ClipToBitmap(bm); err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "characters preprocessing")
		}
	case ComponentWords:
		factor := 1
		var reduced *Bitmap
		switch {
		case bm.XResolution <= 200:
			reduced = bm
		case bm.XResolution <= 400:
			factor = 2
			reduced, err = reduceRankBinaryCascade(bm, 1, 0, 0, 0)
			if err != nil {
				return nil, nil, jbig2errors.Wrap(err, processName, "word preprocess - xres<=400")
			}
		default:
			factor = 4
			reduced, err = reduceRankBinaryCascade(bm, 1, 1, 0, 0)
			if err != nil {
				return nil, nil, jbig2errors.Wrap(err, processName, "word preprocess - xres > 400")
			}
		}
		wordMask, _, err2 := wordMaskByDilation(reduced)
		if err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "word preprocess")
		}
		expanded, err2 := expandReplicate(wordMask, factor)
		if err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "word preprocess")
		}
		wordBitmaps := &Bitmaps{}
		if boxes, err2 = expanded.ConnComponents(wordBitmaps, 4); err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "word preprocess, connect expanded")
		}
		if bitmaps, err2 = wordBitmaps.ClipToBitmap(bm); err2 != nil {
			return nil, nil, jbig2errors.Wrap(err2, processName, "word preprocess")
		}
	}
	bitmaps, err = bitmaps.SelectBySize(maxWidth, maxHeight, LocSelectIfBoth, SizeSelectIfLTE)
	if err != nil {
		return nil, nil, jbig2errors.Wrap(err, processName, "")
	}
	boxes, err = boxes.SelectBySize(maxWidth, maxHeight, LocSelectIfBoth, SizeSelectIfLTE)
	if err != nil {
		return nil, nil, jbig2errors.Wrap(err, processName, "")
	}
	return bitmaps, boxes, nil
}

func (boxes *Boxes) SelectBySize(width, height int, tp LocationFilter, relation SizeComparison) (result *Boxes, err error) {
	const processName = "Boxes.SelectBySize"
	if boxes == nil {
		return nil, jbig2errors.Error(processName, "boxes 'b' not defined")
	}
	if len(*boxes) == 0 {
		return boxes, nil
	}
	switch tp {
	case LocSelectWidth, LocSelectHeight, LocSelectIfEither, LocSelectIfBoth:
	default:
		return nil, jbig2errors.Errorf(processName, "invalid filter type: %d", tp)
	}
	switch relation {
	case SizeSelectIfLT, SizeSelectIfGT, SizeSelectIfLTE, SizeSelectIfGTE:
	default:
		return nil, jbig2errors.Errorf(processName, "invalid relation type: '%d'", tp)
	}
	indicator := boxes.makeSizeIndicator(width, height, tp, relation)
	selected, err := boxes.selectWithIndicator(indicator)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return selected, nil
}

func makeExpandTab4x() (tab [256]uint32) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			tab[i] |= 0xf
		}
		if i&0x02 != 0 {
			tab[i] |= 0xf0
		}
		if i&0x04 != 0 {
			tab[i] |= 0xf00
		}
		if i&0x08 != 0 {
			tab[i] |= 0xf000
		}
		if i&0x10 != 0 {
			tab[i] |= 0xf0000
		}
		if i&0x20 != 0 {
			tab[i] |= 0xf00000
		}
		if i&0x40 != 0 {
			tab[i] |= 0xf000000
		}
		if i&0x80 != 0 {
			tab[i] |= 0xf0000000
		}
	}
	return tab
}

func (cp *ClassedPoints) GroupByY() ([]*ClassedPoints, error) {
	const processName = "ClassedPoints.GroupByY"
	if err := cp.validateIntSlice(); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if cp.IntSlice.Size() == 0 {
		return nil, jbig2errors.Error(processName, "No classes provided")
	}
	cp.SortByY()
	var (
		groups []*ClassedPoints
		y      int
	)
	prevY := -1
	var group *ClassedPoints
	for i := 0; i < len(cp.IntSlice); i++ {
		y = int(cp.YAtIndex(i))
		if y != prevY {
			group = &ClassedPoints{Points: cp.Points}
			prevY = y
			groups = append(groups, group)
		}
		group.IntSlice = append(group.IntSlice, cp.IntSlice[i])
	}
	for _, g := range groups {
		g.SortByX()
	}
	return groups, nil
}

const (
	shiftLeft shift = iota
	shiftRight
)

func (boxes *Boxes) makeSizeIndicator(width, height int, filter LocationFilter, relation SizeComparison) *basic.NumSlice {
	indicator := &basic.NumSlice{}
	var flag, w, h int
	for _, box := range *boxes {
		flag = 0
		w, h = box.Dx(), box.Dy()
		switch filter {
		case LocSelectWidth:
			if (relation == SizeSelectIfLT && w < width) || (relation == SizeSelectIfGT && w > width) || (relation == SizeSelectIfLTE && w <= width) || (relation == SizeSelectIfGTE && w >= width) {
				flag = 1
			}
		case LocSelectHeight:
			if (relation == SizeSelectIfLT && h < height) || (relation == SizeSelectIfGT && h > height) || (relation == SizeSelectIfLTE && h <= height) || (relation == SizeSelectIfGTE && h >= height) {
				flag = 1
			}
		case LocSelectIfEither:
			if (relation == SizeSelectIfLT && (h < height || w < width)) || (relation == SizeSelectIfGT && (h > height || w > width)) || (relation == SizeSelectIfLTE && (h <= height || w <= width)) || (relation == SizeSelectIfGTE && (h >= height || w >= width)) {
				flag = 1
			}
		case LocSelectIfBoth:
			if (relation == SizeSelectIfLT && (h < height && w < width)) || (relation == SizeSelectIfGT && (h > height && w > width)) || (relation == SizeSelectIfLTE && (h <= height && w <= width)) || (relation == SizeSelectIfGTE && (h >= height && w >= width)) {
				flag = 1
			}
		}
		indicator.AddInt(flag)
	}
	return indicator
}

const (
	asymmetricMorphBC BoundaryCondition = iota
	symmetricMorphBC
)

func makeExpandTab8x() (tab [256]uint64) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			tab[i] |= 0xff
		}
		if i&0x02 != 0 {
			tab[i] |= 0xff00
		}
		if i&0x04 != 0 {
			tab[i] |= 0xff0000
		}
		if i&0x08 != 0 {
			tab[i] |= 0xff000000
		}
		if i&0x10 != 0 {
			tab[i] |= 0xff00000000
		}
		if i&0x20 != 0 {
			tab[i] |= 0xff0000000000
		}
		if i&0x40 != 0 {
			tab[i] |= 0xff000000000000
		}
		if i&0x80 != 0 {
			tab[i] |= 0xff00000000000000
		}
	}
	return tab
}
func (cp *ClassedPoints) SortByY() { cp.lessFunc = cp.ySortFunction(); sort.Sort(cp) }
func TstCSymbol(t *testing.T) *Bitmap {
	t.Helper()
	bm, err := New(6, 6)
	require.NoError(t, err)
	require.NoError(t, bm.SetPixel(1, 0, 1))
	require.NoError(t, bm.SetPixel(2, 0, 1))
	require.NoError(t, bm.SetPixel(3, 0, 1))
	require.NoError(t, bm.SetPixel(4, 0, 1))
	require.NoError(t, bm.SetPixel(0, 1, 1))
	require.NoError(t, bm.SetPixel(5, 1, 1))
	require.NoError(t, bm.SetPixel(0, 2, 1))
	require.NoError(t, bm.SetPixel(0, 3, 1))
	require.NoError(t, bm.SetPixel(0, 4, 1))
	require.NoError(t, bm.SetPixel(5, 4, 1))
	require.NoError(t, bm.SetPixel(1, 5, 1))
	require.NoError(t, bm.SetPixel(2, 5, 1))
	require.NoError(t, bm.SetPixel(3, 5, 1))
	require.NoError(t, bm.SetPixel(4, 5, 1))
	return bm
}

func (pts Points) XSorter() func(i, j int) bool {
	return func(i, j int) bool { return pts[i].X < pts[j].X }
}

func New(width, height int) (*Bitmap, error) {
	if width < 0 || height < 0 {
		return nil, fmt.Errorf("JBIG2: invalid bitmap dimensions %d x %d", width, height)
	}
	bm := newBitmapNoData(width, height)
	if bm.RowStride < 0 {
		return nil, fmt.Errorf("JBIG2: invalid bitmap row stride for width %d", width)
	}
	dataSize := int64(height) * int64(bm.RowStride)
	if dataSize > maxBitmapBytes {
		return nil, fmt.Errorf("JBIG2: bitmap allocation %d x %d (%d bytes) exceeds limit of %d bytes", width, height, dataSize, maxBitmapBytes)
	}
	bm.Data = make([]byte, dataSize)
	return bm, nil
}

func makePixelCentroidTab8() []int {
	tab := make([]int, 256)
	tab[0] = 0
	tab[1] = 7
	var i int
	for i = 2; i < 4; i++ {
		tab[i] = tab[i-2] + 6
	}
	for i = 4; i < 8; i++ {
		tab[i] = tab[i-4] + 5
	}
	for i = 8; i < 16; i++ {
		tab[i] = tab[i-8] + 4
	}
	for i = 16; i < 32; i++ {
		tab[i] = tab[i-16] + 3
	}
	for i = 32; i < 64; i++ {
		tab[i] = tab[i-32] + 2
	}
	for i = 64; i < 128; i++ {
		tab[i] = tab[i-64] + 1
	}
	for i = 128; i < 256; i++ {
		tab[i] = tab[i-128]
	}
	return tab
}

func expandReplicate(src *Bitmap, factor int) (*Bitmap, error) {
	const processName = "expandReplicate"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source not defined")
	}
	if factor <= 0 {
		return nil, jbig2errors.Error(processName, "invalid factor - <= 0")
	}
	if factor == 1 {
		copied, err := copyBitmap(nil, src)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "factor = 1")
		}
		return copied, nil
	}
	result, err := expandBinaryReplicate(src, factor, factor)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return result, nil
}

type SizeComparison int

func newSelection(height, width int, name string) *Selection {
	sel := &Selection{Height: height, Width: width, Name: name}
	sel.Data = make([][]SelectionValue, height)
	for i := 0; i < height; i++ {
		sel.Data[i] = make([]SelectionValue, width)
	}
	return sel
}

func MorphSequence(src *Bitmap, sequence ...MorphProcess) (*Bitmap, error) {
	return morphSequence(src, sequence...)
}

const (
	CmbOpOr CombinationOperator = iota
	CmbOpAnd
	CmbOpXor
	CmbOpXNor
	CmbOpReplace
	CmbOpNot
)

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type Component int

func Copy(d, s *Bitmap) (*Bitmap, error) { return copyBitmap(d, s) }

type (
	MorphOperation int
	shift          int
)

func RankHausTest(p1, p2, p3, p4 *Bitmap, delX, delY float32, maxDiffW, maxDiffH, area1, area3 int, rank float32, tab8 []int) (exceeds bool, err error) {
	const processName = "RankHausTest"
	w1, h1 := p1.Width, p1.Height
	w3, h3 := p3.Width, p3.Height
	if basic.Abs(w1-w3) > maxDiffW {
		return false, nil
	}
	if basic.Abs(h1-h3) > maxDiffH {
		return false, nil
	}
	thresh1 := int(float32(area1)*(1.0-rank) + 0.5)
	thresh3 := int(float32(area3)*(1.0-rank) + 0.5)
	var shiftX, shiftY int
	if delX >= 0 {
		shiftX = int(delX + 0.5)
	} else {
		shiftX = int(delX - 0.5)
	}
	if delY >= 0 {
		shiftY = int(delY + 0.5)
	} else {
		shiftY = int(delY - 0.5)
	}
	temp := p1.CreateTemplate()
	if err = temp.RasterOperation(0, 0, w1, h1, PixSrc, p1, 0, 0); err != nil {
		return false, jbig2errors.Wrap(err, processName, "p1 -SRC-> t")
	}
	if err = temp.RasterOperation(shiftX, shiftY, w1, h1, PixNotSrcAndDst, p4, 0, 0); err != nil {
		return false, jbig2errors.Wrap(err, processName, "t & !p4")
	}
	exceeds, err = temp.ThresholdPixelSum(thresh1, tab8)
	if err != nil {
		return false, jbig2errors.Wrap(err, processName, "t->thresh1")
	}
	if exceeds {
		return false, nil
	}
	if err = temp.RasterOperation(shiftX, shiftY, w3, h3, PixSrc, p3, 0, 0); err != nil {
		return false, jbig2errors.Wrap(err, processName, "p3 -SRC-> t")
	}
	if err = temp.RasterOperation(0, 0, w3, h3, PixNotSrcAndDst, p2, 0, 0); err != nil {
		return false, jbig2errors.Wrap(err, processName, "t & !p2")
	}
	exceeds, err = temp.ThresholdPixelSum(thresh3, tab8)
	if err != nil {
		return false, jbig2errors.Wrap(err, processName, "t->thresh3")
	}
	return !exceeds, nil
}

func (mp MorphProcess) verify(index int, netReduction, borderSize *int) error {
	const processName = "MorphProcess.verify"
	switch mp.Operation {
	case MopDilation, MopErosion, MopOpening, MopClosing:
		if len(mp.Arguments) != 2 {
			return jbig2errors.Error(processName, "Operation: 'd', 'e', 'o', 'c' requires at least 2 arguments")
		}
		w, h := mp.getWidthHeight()
		if w <= 0 || h <= 0 {
			return jbig2errors.Error(processName, "Operation: 'd', 'e', 'o', 'c'  requires both width and height to be >= 0")
		}
	case MopRankBinaryReduction:
		nArgs := len(mp.Arguments)
		*netReduction += nArgs
		if nArgs < 1 || nArgs > 4 {
			return jbig2errors.Error(processName, "Operation: 'r' requires at least 1 and at most 4 arguments")
		}
		for i := 0; i < nArgs; i++ {
			if mp.Arguments[i] < 1 || mp.Arguments[i] > 4 {
				return jbig2errors.Error(processName, "RankBinaryReduction level must be in range (0, 4>")
			}
		}
	case MopReplicativeBinaryExpansion:
		if len(mp.Arguments) == 0 {
			return jbig2errors.Error(processName, "ReplicativeBinaryExpansion requires one argument")
		}
		factor := mp.Arguments[0]
		if factor != 2 && factor != 4 && factor != 8 {
			return jbig2errors.Error(processName, "ReplicativeBinaryExpansion must be of factor in range {2,4,8}")
		}
		*netReduction -= reductionTab[factor/4]
	case MopAddBorder:
		if len(mp.Arguments) == 0 {
			return jbig2errors.Error(processName, "AddBorder requires one argument")
		}
		size := mp.Arguments[0]
		if index > 0 {
			return jbig2errors.Error(processName, "AddBorder must be a first morph process")
		}
		if size < 1 {
			return jbig2errors.Error(processName, "AddBorder value lower than 0")
		}
		*borderSize = size
	}
	return nil
}

func openMorph(dest, src *Bitmap, sel *Selection) (*Bitmap, error) {
	const processName = "open"
	var err error
	dest, err = processMorphArgs2(dest, src, sel)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	temp, err := erode(nil, src, sel)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	_, err = dilate(dest, temp, sel)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return dest, nil
}

func rasterOpNoSrc(bm *Bitmap, dx, dy, dw, dh int, op RasterOperator) {
	if dx < 0 {
		dw += dx
		dx = 0
	}
	overW := dx + dw - bm.Width
	if overW > 0 {
		dw -= overW
	}
	if dy < 0 {
		dh += dy
		dy = 0
	}
	overH := dy + dh - bm.Height
	if overH > 0 {
		dh -= overH
	}
	if dw <= 0 || dh <= 0 {
		return
	}
	if (dx & 7) == 0 {
		rasterOpNoSrcAligned(bm, dx, dy, dw, dh, op)
	} else {
		rasterOpNoSrcUnaligned(bm, dx, dy, dw, dh, op)
	}
}

func (pts Points) GetGeometry(i int) (x, y float32, err error) {
	if i > len(pts)-1 {
		return 0, 0, jbig2errors.Errorf("Points.Get", "index: '%d' out of range", i)
	}
	pt := pts[i]
	return pt.X, pt.Y, nil
}

func CorrelationScoreThresholded(bm1, bm2 *Bitmap, area1, area2 int, delX, delY float32, maxDiffW, maxDiffH int, tab, downcount []int, scoreThreshold float32) (bool, error) {
	const processName = "CorrelationScoreThresholded"
	if bm1 == nil {
		return false, jbig2errors.Error(processName, "correlationScoreThresholded bm1 is nil")
	}
	if bm2 == nil {
		return false, jbig2errors.Error(processName, "correlationScoreThresholded bm2 is nil")
	}
	if area1 <= 0 || area2 <= 0 {
		return false, jbig2errors.Error(processName, "correlationScoreThresholded - areas must be > 0")
	}
	if downcount == nil {
		return false, jbig2errors.Error(processName, "provided no 'downcount'")
	}
	if tab == nil {
		return false, jbig2errors.Error(processName, "provided nil 'sumtab'")
	}
	w1, h1 := bm1.Width, bm1.Height
	w2, h2 := bm2.Width, bm2.Height
	if basic.Abs(w1-w2) > maxDiffW {
		return false, nil
	}
	if basic.Abs(h1-h2) > maxDiffH {
		return false, nil
	}
	dx := int(delX + basic.Sign(delX)*0.5)
	dy := int(delY + basic.Sign(delY)*0.5)
	countThreshold := int(math.Ceil(math.Sqrt(float64(scoreThreshold) * float64(area1) * float64(area2))))
	stride2 := bm2.RowStride
	yStart := maxInt(dy, 0)
	yEnd := minInt(h2+dy, h1)
	idx1 := bm1.RowStride * yStart
	idx2 := bm2.RowStride * (yStart - dy)
	var downcountEnd int
	if yEnd <= h1 {
		downcountEnd = downcount[yEnd-1]
	}
	xStart := maxInt(dx, 0)
	xEnd := minInt(w2+dx, w1)
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
	var (
		count, y, x         int
		anded, byte1, byte2 byte
	)
	if xStart >= xEnd || yStart >= yEnd {
		return false, nil
	}
	wBytes := (xEnd + 7) >> 3
	switch {
	case dx == 0:
		for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
			for x = 0; x < wBytes; x++ {
				anded = bm1.Data[idx1+x] & bm2.Data[idx2+x]
				count += tab[anded]
			}
			if count >= countThreshold {
				return true, nil
			}
			if upperBound := count + downcount[y] - downcountEnd; upperBound < countThreshold {
				return false, nil
			}
		}
	case dx > 0 && stride2 < wBytes:
		for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
			byte1 = bm1.Data[idx1]
			byte2 = bm2.Data[idx2] >> uint(dx)
			anded = byte1 & byte2
			count += tab[anded]
			for x = 1; x < stride2; x++ {
				byte1 = bm1.Data[idx1+x]
				byte2 = bm2.Data[idx2+x]>>uint(dx) | bm2.Data[idx2+x-1]<<uint(8-dx)
				anded = byte1 & byte2
				count += tab[anded]
			}
			byte1 = bm1.Data[idx1+x]
			byte2 = bm2.Data[idx2+x-1] << uint(8-dx)
			anded = byte1 & byte2
			count += tab[anded]
			if count >= countThreshold {
				return true, nil
			} else if count+downcount[y]-downcountEnd < countThreshold {
				return false, nil
			}
		}
	case dx > 0 && stride2 >= wBytes:
		for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
			byte1 = bm1.Data[idx1]
			byte2 = bm2.Data[idx2] >> uint(dx)
			anded = byte1 & byte2
			count += tab[anded]
			for x = 1; x < wBytes; x++ {
				byte1 = bm1.Data[idx1+x]
				byte2 = bm2.Data[idx2+x] >> uint(dx)
				byte2 |= bm2.Data[idx2+x-1] << uint(8-dx)
				anded = byte1 & byte2
				count += tab[anded]
			}
			if count >= countThreshold {
				return true, nil
			} else if count+downcount[y]-downcountEnd < countThreshold {
				return false, nil
			}
		}
	case wBytes < stride2:
		for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
			for x = 0; x < wBytes; x++ {
				byte1 = bm1.Data[idx1+x]
				byte2 = bm2.Data[idx2+x] << uint(-dx)
				byte2 |= bm2.Data[idx2+x+1] >> uint(8+dx)
				anded = byte1 & byte2
				count += tab[anded]
			}
			if count >= countThreshold {
				return true, nil
			} else if upperBound := count + downcount[y] - downcountEnd; upperBound < countThreshold {
				return false, nil
			}
		}
	case stride2 >= wBytes:
		for y = yStart; y < yEnd; y, idx1, idx2 = y+1, idx1+bm1.RowStride, idx2+bm2.RowStride {
			for x = 0; x < wBytes; x++ {
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
			if count >= countThreshold {
				return true, nil
			} else if count+downcount[y]-downcountEnd < countThreshold {
				return false, nil
			}
		}
	}
	score := float32(count) * float32(count) / (float32(area1) * float32(area2))
	if score >= scoreThreshold {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("count: %d < threshold %d but score %f >= scoreThreshold %f", count, countThreshold, score, scoreThreshold)
		}
	}
	return false, nil
}

func combineBytes(src, dst byte, op CombinationOperator) byte {
	switch op {
	case CmbOpOr:
		return dst | src
	case CmbOpAnd:
		return dst & src
	case CmbOpXor:
		return dst ^ src
	case CmbOpXNor:
		return ^(dst ^ src)
	case CmbOpNot:
		return ^dst
	default:
		return dst
	}
}

func closeSafeBrick(dest, src *Bitmap, hSize, vSize int) (*Bitmap, error) {
	const processName = "closeSafeBrick"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source is nil")
	}
	if hSize < 1 || vSize < 1 {
		return nil, jbig2errors.Error(processName, "hsize and vsize not >= 1")
	}
	if hSize == 1 && vSize == 1 {
		return copyBitmap(dest, src)
	}
	if morphBoundaryCondition == symmetricMorphBC {
		result, err := closeBrick(dest, src, hSize, vSize)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "symmetricMorphBC")
		}
		return result, nil
	}
	maxSize := maxInt(hSize/2, vSize/2)
	borderSize := 8 * ((maxSize + 7) / 8)
	bordered, err := src.AddBorder(borderSize, 0)
	if err != nil {
		return nil, jbig2errors.Wrapf(err, processName, "BorderSize: %d", borderSize)
	}
	var closed, unbordered *Bitmap
	if hSize == 1 || vSize == 1 {
		sel := SelCreateBrick(vSize, hSize, vSize/2, hSize/2, SelHit)
		closed, err = closeMorph(nil, bordered, sel)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "hSize == 1 || vSize == 1")
		}
	} else {
		selH := SelCreateBrick(1, hSize, 0, hSize/2, SelHit)
		temp, err2 := dilate(nil, bordered, selH)
		if err2 != nil {
			return nil, jbig2errors.Wrap(err2, processName, "regular - first dilate")
		}
		selV := SelCreateBrick(vSize, 1, vSize/2, 0, SelHit)
		closed, err2 = dilate(nil, temp, selV)
		if err2 != nil {
			return nil, jbig2errors.Wrap(err2, processName, "regular - second dilate")
		}
		if _, err2 = erode(temp, closed, selH); err2 != nil {
			return nil, jbig2errors.Wrap(err2, processName, "regular - first erode")
		}
		if _, err2 = erode(closed, temp, selV); err2 != nil {
			return nil, jbig2errors.Wrap(err2, processName, "regular - second erode")
		}
	}
	if unbordered, err = closed.RemoveBorder(borderSize); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "regular")
	}
	if dest == nil {
		return unbordered, nil
	}
	if _, err = copyBitmap(dest, unbordered); err != nil {
		return nil, err
	}
	return dest, nil
}

func wordMaskByDilation(src *Bitmap) (mask *Bitmap, size int, err error) {
	const processName = "Bitmap.wordMaskByDilation"
	if src == nil {
		return nil, 0, jbig2errors.Errorf(processName, "'s' bitmap not defined")
	}
	var prev, dilated *Bitmap
	if prev, err = copyBitmap(nil, src); err != nil {
		return nil, 0, jbig2errors.Wrap(err, processName, "copy 's'")
	}
	var (
		counts              [13]int
		initialCount, delta int
	)
	maxIter := 12
	countSlice := basic.NewNumSlice(maxIter + 1)
	deltaSlice := basic.NewNumSlice(maxIter + 1)
	var ccBoxes *Boxes
	for i := 0; i <= maxIter; i++ {
		if i == 0 {
			if dilated, err = copyBitmap(nil, prev); err != nil {
				return nil, 0, jbig2errors.Wrap(err, processName, "first bm2")
			}
		} else {
			if dilated, err = morphSequence(prev, MorphProcess{Operation: MopDilation, Arguments: []int{2, 1}}); err != nil {
				return nil, 0, jbig2errors.Wrap(err, processName, "dilation bm2")
			}
		}
		if ccBoxes, err = dilated.connComponentsBB(4); err != nil {
			return nil, 0, jbig2errors.Wrap(err, processName, "")
		}
		counts[i] = len(*ccBoxes)
		countSlice.AddInt(counts[i])
		switch i {
		case 0:
			initialCount = counts[0]
		default:
			delta = counts[i-1] - counts[i]
			deltaSlice.AddInt(delta)
		}
		prev = dilated
	}
	first := true
	sizeCand := 2
	var nc, maxDelta int
	for i := 1; i < len(*deltaSlice); i++ {
		if nc, err = countSlice.GetInt(i); err != nil {
			return nil, 0, jbig2errors.Wrap(err, processName, "Checking best dilation")
		}
		if first && nc < int(0.3*float32(initialCount)) {
			sizeCand = i + 1
			first = false
		}
		if delta, err = deltaSlice.GetInt(i); err != nil {
			return nil, 0, jbig2errors.Wrap(err, processName, "getting naDiff")
		}
		if delta > maxDelta {
			maxDelta = delta
		}
	}
	xRes := src.XResolution
	if xRes == 0 {
		xRes = 150
	}
	if xRes > 110 {
		sizeCand++
	}
	if sizeCand < 2 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("JBIG2 setting iBest to minimum allowable")
		}
		sizeCand = 2
	}
	size = sizeCand + 1
	if mask, err = closeBrick(nil, src, sizeCand+1, 1); err != nil {
		return nil, 0, jbig2errors.Wrap(err, processName, "getting mask failed")
	}
	return mask, size, nil
}

func TstNSymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(4, 5, []byte{0x90, 0xD0, 0xB0, 0x90, 0x90})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func openBrick(dest, src *Bitmap, hSize, vSize int) (*Bitmap, error) {
	const processName = "openBrick"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source bitmap not defined")
	}
	if hSize < 1 && vSize < 1 {
		return nil, jbig2errors.Error(processName, "hSize < 1 && vSize < 1")
	}
	if hSize == 1 && vSize == 1 {
		return src.Copy(), nil
	}
	if hSize == 1 || vSize == 1 {
		var err error
		sel := SelCreateBrick(vSize, hSize, vSize/2, hSize/2, SelHit)
		dest, err = openMorph(dest, src, sel)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "hSize == 1 || vSize == 1")
		}
		return dest, nil
	}
	selH := SelCreateBrick(1, hSize, 0, hSize/2, SelHit)
	selV := SelCreateBrick(vSize, 1, vSize/2, 0, SelHit)
	temp, err := erode(nil, src, selH)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "1st erode")
	}
	dest, err = erode(dest, temp, selV)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "2nd erode")
	}
	_, err = dilate(temp, dest, selH)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "1st dilate")
	}
	_, err = dilate(dest, temp, selV)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "2nd dilate")
	}
	return dest, nil
}

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

func morphSequence(src *Bitmap, sequence ...MorphProcess) (result *Bitmap, err error) {
	const processName = "morphSequence"
	if src == nil {
		return nil, jbig2errors.Error(processName, "morphSequence source bitmap not defined")
	}
	if len(sequence) == 0 {
		return nil, jbig2errors.Error(processName, "morphSequence, sequence not defined")
	}
	if err = verifyMorphProcesses(sequence...); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	var w, h, borderSize int
	result = src.Copy()
	for _, proc := range sequence {
		switch proc.Operation {
		case MopDilation:
			w, h = proc.getWidthHeight()
			result, err = DilateBrick(nil, result, w, h)
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "")
			}
		case MopErosion:
			w, h = proc.getWidthHeight()
			result, err = erodeBrick(nil, result, w, h)
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "")
			}
		case MopOpening:
			w, h = proc.getWidthHeight()
			result, err = openBrick(nil, result, w, h)
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "")
			}
		case MopClosing:
			w, h = proc.getWidthHeight()
			result, err = closeSafeBrick(nil, result, w, h)
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "")
			}
		case MopRankBinaryReduction:
			result, err = reduceRankBinaryCascade(result, proc.Arguments...)
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "")
			}
		case MopReplicativeBinaryExpansion:
			result, err = expandReplicate(result, proc.Arguments[0])
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "")
			}
		case MopAddBorder:
			borderSize = proc.Arguments[0]
			result, err = result.AddBorder(borderSize, 0)
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "")
			}
		default:
			return nil, jbig2errors.Error(processName, "invalid morphOperation provided to the sequence")
		}
	}
	if borderSize > 0 {
		result, err = result.RemoveBorder(borderSize)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "border > 0")
		}
	}
	return result, nil
}

func DilateBrick(d, s *Bitmap, hSize, vSize int) (*Bitmap, error) {
	return dilateBrick(d, s, hSize, vSize)
}

func (bm *Bitmap) GetVanillaData() []byte {
	if bm.Color == Chocolate {
		bm.inverseData()
	}
	return bm.Data
}

const (
	Vanilla Color = iota
	Chocolate
)

func (bm *Bitmap) thresholdPixelSum(thresh int) bool {
	var (
		sum      int
		lastMask uint8
		b        byte
		x        int
	)
	fullBytes := bm.RowStride
	remBits := uint(bm.Width & 0x07)
	if remBits != 0 {
		lastMask = uint8((0xff << (8 - remBits)) & 0xff)
		fullBytes--
	}
	for y := 0; y < bm.Height; y++ {
		for x = 0; x < fullBytes; x++ {
			b = bm.Data[y*bm.RowStride+x]
			sum += int(popCountTab[b])
		}
		if remBits != 0 {
			b = bm.Data[y*bm.RowStride+x] & lastMask
			sum += int(popCountTab[b])
		}
		if sum > thresh {
			return true
		}
	}
	return false
}

func combineBitmaps(a, b *Bitmap, op CombinationOperator) (*Bitmap, error) {
	result, err := New(a.Width, a.Height)
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(result.Data); i++ {
		result.Data[i] = combineBytes(a.Data[i], b.Data[i], op)
	}
	return result, nil
}

func makePixelSumTab8() []int {
	tab := make([]int, 256)
	for i := 0; i <= 0xff; i++ {
		b := byte(i)
		tab[b] = int(b&0x1) + (int(b>>1) & 0x1) + (int(b>>2) & 0x1) + (int(b>>3) & 0x1) + (int(b>>4) & 0x1) + (int(b>>5) & 0x1) + (int(b>>6) & 0x1) + (int(b>>7) & 0x1)
	}
	return tab
}

func seedfillBinaryLow(seed *Bitmap, mask *Bitmap, connectivity int) (err error) {
	const processName = "seedfillBinaryLow"
	h := minInt(seed.Height, mask.Height)
	wpl := minInt(seed.RowStride, mask.RowStride)
	switch connectivity {
	case 4:
		err = seedfillBinaryLow4(seed, mask, h, wpl)
	case 8:
		err = seedfillBinaryLow8(seed, mask, h, wpl)
	default:
		return jbig2errors.Errorf(processName, "connectivity must be 4 or 8 - is: '%d'", connectivity)
	}
	if err != nil {
		return jbig2errors.Wrap(err, processName, "")
	}
	return nil
}
func (bms *Bitmaps) AddBitmap(bm *Bitmap) { bms.Values = append(bms.Values, bm) }
func (bm *Bitmap) GetBitOffset(x int) int { return x & 0x07 }
func (bms *Bitmaps) GroupByHeight() (*BitmapsArray, error) {
	const processName = "GroupByHeight"
	if len(bms.Values) == 0 {
		return nil, jbig2errors.Error(processName, "no values provided")
	}
	result := &BitmapsArray{}
	bms.SortByHeight()
	curHeight := -1
	groupIdx := -1
	for i := 0; i < len(bms.Values); i++ {
		h := bms.Values[i].Height
		if h > curHeight {
			curHeight = h
			groupIdx++
			result.Values = append(result.Values, &Bitmaps{})
		}
		result.Values[groupIdx].AddBitmap(bms.Values[i])
	}
	return result, nil
}

func expandBinaryReplicate(src *Bitmap, xFact, yFact int) (*Bitmap, error) {
	const processName = "expandBinaryReplicate"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source not defined")
	}
	if xFact <= 0 || yFact <= 0 {
		return nil, jbig2errors.Error(processName, "invalid scale factor: <= 0")
	}
	if xFact == yFact {
		if xFact == 1 {
			copied, err := copyBitmap(nil, src)
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "xFact == yFact")
			}
			return copied, nil
		}
		if xFact == 2 || xFact == 4 || xFact == 8 {
			expanded, err := expandBinaryPower2(src, xFact)
			if err != nil {
				return nil, jbig2errors.Wrap(err, processName, "xFact in {2,4,8}")
			}
			return expanded, nil
		}
	}
	w := xFact * src.Width
	h := yFact * src.Height
	dest, err := New(w, h)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	destStride := dest.RowStride
	var (
		rowOff, y, x, k, bitOff int
		b                       byte
	)
	for y = 0; y < src.Height; y++ {
		rowOff = yFact * y * destStride
		for x = 0; x < src.Width; x++ {
			if set := src.GetPixel(x, y); set {
				bitOff = xFact * x
				for k = 0; k < xFact; k++ {
					dest.setBit(rowOff*8 + bitOff + k)
				}
			}
		}
		for k = 1; k < yFact; k++ {
			dstOff := rowOff + k*destStride
			for j := 0; j < destStride; j++ {
				if b, err = dest.GetByte(rowOff + j); err != nil {
					return nil, jbig2errors.Wrapf(err, processName, "replicating line: '%d'", k)
				}
				if err = dest.SetByte(dstOff+j, b); err != nil {
					return nil, jbig2errors.Wrap(err, processName, "Setting byte failed")
				}
			}
		}
	}
	return dest, nil
}

func (bm *Bitmap) String() string {
	var s strings.Builder
	s.WriteString("\n")
	for y := 0; y < bm.Height; y++ {
		var line strings.Builder
		for x := 0; x < bm.Width; x++ {
			isSet := bm.GetPixel(x, y)
			if isSet {
				line.WriteString("1")
			} else {
				line.WriteString("0")
			}
		}
		s.WriteString(line.String() + "\n")
	}
	return s.String()
}

func expandBinaryFactor8(dest, src *Bitmap) (err error) {
	const processName = "expandBinaryFactor8"
	srcStride := src.RowStride
	destStride := dest.RowStride
	var srcOff, destOff, y, x, k int
	for y = 0; y < src.Height; y++ {
		srcOff = y * srcStride
		destOff = 8 * y * destStride
		for x = 0; x < srcStride; x++ {
			if err = dest.setEightBytes(destOff+x*8, expandTab8x[src.Data[srcOff+x]]); err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
		}
		for k = 1; k < 8; k++ {
			for x = 0; x < destStride; x++ {
				if err = dest.SetByte(destOff+k*destStride+x, dest.Data[destOff+x]); err != nil {
					return jbig2errors.Wrap(err, processName, "")
				}
			}
		}
	}
	return nil
}

func (cp *ClassedPoints) validateIntSlice() error {
	const processName = "validateIntSlice"
	for _, class := range cp.IntSlice {
		if class >= cp.Points.Size() {
			return jbig2errors.Errorf(processName, "class id: '%d' is not a valid index in the points of size: %d", class, cp.Points.Size())
		}
	}
	return nil
}

type ClassedPoints struct {
	*Points
	basic.IntSlice
	lessFunc func(i, j int) bool
}
type CombinationOperator int

func (bm *Bitmap) SizesEqual(s *Bitmap) bool {
	if bm == s {
		return true
	}
	if bm.Width != s.Width || bm.Height != s.Height {
		return false
	}
	return true
}

func (cp *ClassedPoints) GetIntYByClass(i int) (int, error) {
	const processName = "ClassedPoints.GetIntYByClass"
	if i >= cp.IntSlice.Size() {
		return 0, jbig2errors.Errorf(processName, "i: '%d' is out of the range of the IntSlice", i)
	}
	return int(cp.YAtIndex(i)), nil
}

func seedfillBinaryLow8(seed, mask *Bitmap, h, wpl int) (err error) {
	const processName = "seedfillBinaryLow8"
	var (
		i, j, seedLine, maskLine                                                          int
		seedByte, maskByte, aboveByte, leftByte, belowByte, rightByte, prevByte, diagByte byte
	)
	for i = 0; i < h; i++ {
		seedLine = i * seed.RowStride
		maskLine = i * mask.RowStride
		for j = 0; j < wpl; j++ {
			if seedByte, err = seed.GetByte(seedLine + j); err != nil {
				return jbig2errors.Wrap(err, processName, "get source byte")
			}
			if maskByte, err = mask.GetByte(maskLine + j); err != nil {
				return jbig2errors.Wrap(err, processName, "get mask byte")
			}
			if i > 0 {
				if aboveByte, err = seed.GetByte(seedLine - seed.RowStride + j); err != nil {
					return jbig2errors.Wrap(err, processName, "i > 0 byte")
				}
				seedByte |= aboveByte | (aboveByte << 1) | (aboveByte >> 1)
				if j > 0 {
					if diagByte, err = seed.GetByte(seedLine - seed.RowStride + j - 1); err != nil {
						return jbig2errors.Wrap(err, processName, "i > 0 && j > 0 byte")
					}
					seedByte |= diagByte << 7
				}
				if j < wpl-1 {
					if diagByte, err = seed.GetByte(seedLine - seed.RowStride + j + 1); err != nil {
						return jbig2errors.Wrap(err, processName, "j < wpl - 1 byte")
					}
					seedByte |= diagByte >> 7
				}
			}
			if j > 0 {
				if leftByte, err = seed.GetByte(seedLine + j - 1); err != nil {
					return jbig2errors.Wrap(err, processName, "j > 0")
				}
				seedByte |= leftByte << 7
			}
			seedByte &= maskByte
			if seedByte == 0 || ^seedByte == 0 {
				if err = seed.SetByte(seedLine+j, seedByte); err != nil {
					return jbig2errors.Wrap(err, processName, "setting empty byte")
				}
			}
			for {
				prevByte = seedByte
				seedByte = (seedByte | (seedByte >> 1) | (seedByte << 1)) & maskByte
				if (seedByte ^ prevByte) == 0 {
					if err = seed.SetByte(seedLine+j, seedByte); err != nil {
						return jbig2errors.Wrap(err, processName, "setting prev byte")
					}
					break
				}
			}
		}
	}
	for i = h - 1; i >= 0; i-- {
		seedLine = i * seed.RowStride
		maskLine = i * mask.RowStride
		for j = wpl - 1; j >= 0; j-- {
			if seedByte, err = seed.GetByte(seedLine + j); err != nil {
				return jbig2errors.Wrap(err, processName, "reverse get source byte")
			}
			if maskByte, err = mask.GetByte(maskLine + j); err != nil {
				return jbig2errors.Wrap(err, processName, "reverse get mask byte")
			}
			if i < h-1 {
				if belowByte, err = seed.GetByte(seedLine + seed.RowStride + j); err != nil {
					return jbig2errors.Wrap(err, processName, "i < h - 1 -> get source byte")
				}
				seedByte |= belowByte | (belowByte << 1) | belowByte>>1
				if j > 0 {
					if diagByte, err = seed.GetByte(seedLine + seed.RowStride + j - 1); err != nil {
						return jbig2errors.Wrap(err, processName, "i < h-1 & j > 0 -> get source byte")
					}
					seedByte |= diagByte << 7
				}
				if j < wpl-1 {
					if diagByte, err = seed.GetByte(seedLine + seed.RowStride + j + 1); err != nil {
						return jbig2errors.Wrap(err, processName, "i < h-1 && j <wpl-1 -> get source byte")
					}
					seedByte |= diagByte >> 7
				}
			}
			if j < wpl-1 {
				if rightByte, err = seed.GetByte(seedLine + j + 1); err != nil {
					return jbig2errors.Wrap(err, processName, "j < wpl -1 -> get source byte")
				}
				seedByte |= rightByte >> 7
			}
			seedByte &= maskByte
			if seedByte == 0 || (^seedByte) == 0 {
				if err = seed.SetByte(seedLine+j, seedByte); err != nil {
					return jbig2errors.Wrap(err, processName, "set masked byte")
				}
			}
			for {
				prevByte = seedByte
				seedByte = (seedByte | (seedByte >> 1) | (seedByte << 1)) & maskByte
				if (seedByte ^ prevByte) == 0 {
					if err = seed.SetByte(seedLine+j, seedByte); err != nil {
						return jbig2errors.Wrap(err, processName, "reverse set prev byte")
					}
					break
				}
			}
		}
	}
	return nil
}

var morphBoundaryCondition BoundaryCondition

func processMorphArgs1(dest *Bitmap, src *Bitmap, sel *Selection, ptemp **Bitmap) (*Bitmap, error) {
	const processName = "processMorphArgs1"
	if src == nil {
		return nil, jbig2errors.Error(processName, "MorphArgs1 's' not defined")
	}
	if sel == nil {
		return nil, jbig2errors.Error(processName, "MorhpArgs1 'sel' not defined")
	}
	sh, sw := sel.Height, sel.Width
	if sh == 0 || sw == 0 {
		return nil, jbig2errors.Error(processName, "selection of size 0")
	}
	if dest == nil {
		dest = src.createTemplate()
		*ptemp = src
		return dest, nil
	}
	dest.Width = src.Width
	dest.Height = src.Height
	dest.RowStride = src.RowStride
	dest.Color = src.Color
	dest.Data = make([]byte, src.RowStride*src.Height)
	if dest == src {
		*ptemp = src.Copy()
	} else {
		*ptemp = src
	}
	return dest, nil
}

func (ba *BitmapsArray) GetBox(i int) (*image.Rectangle, error) {
	const processName = "BitmapsArray.GetBox"
	if ba == nil {
		return nil, jbig2errors.Error(processName, "provided nil 'BitmapsArray'")
	}
	if i > len(ba.Boxes)-1 {
		return nil, jbig2errors.Errorf(processName, "index: '%d' out of range", i)
	}
	return ba.Boxes[i], nil
}

func (bms *Bitmaps) WidthSorter() func(i, j int) bool {
	return func(i, j int) bool { return bms.Values[i].Width < bms.Values[j].Width }
}
func (cp *ClassedPoints) XAtIndex(i int) float32 { return (*cp.Points)[cp.IntSlice[i]].X }

type Boxes []*image.Rectangle

func TstFrameBitmapData() []byte { return frameBitmap.Data }
func (bm *Bitmap) connComponentsBB(connectivity int) (boxes *Boxes, err error) {
	const processName = "Bitmap.connComponentsBB"
	if connectivity != 4 && connectivity != 8 {
		return nil, jbig2errors.Error(processName, "connectivity must be a '4' or '8'")
	}
	if bm.Zero() {
		return &Boxes{}, nil
	}
	bm.setPadBits(0)
	work, err := copyBitmap(nil, bm)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "bm1")
	}
	stack := &basic.Stack{}
	stack.Aux = &basic.Stack{}
	boxes = &Boxes{}
	var (
		y, x  int
		pt    image.Point
		found bool
		box   *image.Rectangle
	)
	for {
		if pt, found, err = work.nextOnPixel(x, y); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if !found {
			break
		}
		if box, err = seedFillStackBB(work, stack, pt.X, pt.Y, connectivity); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if err = boxes.Add(box); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		x = pt.X
		y = pt.Y
	}
	return boxes, nil
}

func TstASymbol(t *testing.T) *Bitmap {
	t.Helper()
	bm, err := New(6, 6)
	require.NoError(t, err)
	require.NoError(t, bm.SetPixel(1, 0, 1))
	require.NoError(t, bm.SetPixel(2, 0, 1))
	require.NoError(t, bm.SetPixel(3, 0, 1))
	require.NoError(t, bm.SetPixel(4, 0, 1))
	require.NoError(t, bm.SetPixel(5, 1, 1))
	require.NoError(t, bm.SetPixel(1, 2, 1))
	require.NoError(t, bm.SetPixel(2, 2, 1))
	require.NoError(t, bm.SetPixel(3, 2, 1))
	require.NoError(t, bm.SetPixel(4, 2, 1))
	require.NoError(t, bm.SetPixel(5, 2, 1))
	require.NoError(t, bm.SetPixel(0, 3, 1))
	require.NoError(t, bm.SetPixel(5, 3, 1))
	require.NoError(t, bm.SetPixel(0, 4, 1))
	require.NoError(t, bm.SetPixel(5, 4, 1))
	require.NoError(t, bm.SetPixel(1, 5, 1))
	require.NoError(t, bm.SetPixel(2, 5, 1))
	require.NoError(t, bm.SetPixel(3, 5, 1))
	require.NoError(t, bm.SetPixel(4, 5, 1))
	require.NoError(t, bm.SetPixel(5, 5, 1))
	return bm
}

func (bm *Bitmap) setPadBits(val int) {
	padBits := 8 - bm.Width%8
	if padBits == 8 {
		return
	}
	lastCol := bm.Width / 8
	mask := rightMask8[padBits]
	if val == 0 {
		mask ^= mask
	}
	var idx int
	for y := 0; y < bm.Height; y++ {
		idx = y*bm.RowStride + lastCol
		if val == 0 {
			bm.Data[idx] &= mask
		} else {
			bm.Data[idx] |= mask
		}
	}
}

func (bm *Bitmap) GetChocolateData() []byte {
	if bm.Color == Vanilla {
		bm.inverseData()
	}
	return bm.Data
}

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

func (bm *Bitmap) resizeImageData(src *Bitmap) error {
	if src == nil {
		return jbig2errors.Error("resizeImageData", "src is not defined")
	}
	if bm.SizesEqual(src) {
		return nil
	}
	bm.Data = make([]byte, len(src.Data))
	bm.Width = src.Width
	bm.Height = src.Height
	bm.RowStride = src.RowStride
	return nil
}

func (sel *Selection) findMaxTranslations() (xp, yp, xn, yn int) {
	for i := 0; i < sel.Height; i++ {
		for j := 0; j < sel.Width; j++ {
			if sel.Data[i][j] == SelHit {
				xp = maxInt(xp, sel.Cx-j)
				yp = maxInt(yp, sel.Cy-i)
				xn = maxInt(xn, j-sel.Cx)
				yn = maxInt(yn, i-sel.Cy)
			}
		}
	}
	return xp, yp, xn, yn
}

func processMorphArgs2(dest, src *Bitmap, sel *Selection) (*Bitmap, error) {
	const processName = "processMorphArgs2"
	var sw, sh int
	if src == nil {
		return nil, jbig2errors.Error(processName, "source bitmap is nil")
	}
	if sel == nil {
		return nil, jbig2errors.Error(processName, "sel not defined")
	}
	sw = sel.Width
	sh = sel.Height
	if sw == 0 || sh == 0 {
		return nil, jbig2errors.Error(processName, "sel of size 0")
	}
	if dest == nil {
		return src.createTemplate(), nil
	}
	if err := dest.resizeImageData(src); err != nil {
		return nil, err
	}
	return dest, nil
}
func (s *byHeight) Len() int { return len(s.Values) }
func (bms *Bitmaps) CountPixels() *basic.NumSlice {
	counts := &basic.NumSlice{}
	for _, bm := range bms.Values {
		counts.AddInt(bm.CountPixels())
	}
	return counts
}

type Bitmaps struct {
	Values []*Bitmap
	Boxes  []*image.Rectangle
}

func seedFillStackBB(s *Bitmap, stack *basic.Stack, x, y, connectivity int) (bb *image.Rectangle, err error) {
	const processName = "seedFillStackBB"
	if s == nil {
		return nil, jbig2errors.Error(processName, "provided nil 's' Bitmap")
	}
	if stack == nil {
		return nil, jbig2errors.Error(processName, "provided nil 'stack'")
	}
	switch connectivity {
	case 4:
		if bb, err = seedFillStackBB8(s, stack, x, y); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		return bb, nil
	case 8:
		if bb, err = seedFillStackBB4(s, stack, x, y); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		return bb, nil
	default:
		return nil, jbig2errors.Errorf(processName, "connectivity is not 4 or 8: '%d'", connectivity)
	}
}

var _ sort.Interface = &ClassedPoints{}

func (pts Points) Size() int { return len(pts) }
func (bms *Bitmaps) SelectBySize(width, height int, tp LocationFilter, relation SizeComparison) (result *Bitmaps, err error) {
	const processName = "Bitmaps.SelectBySize"
	if bms == nil {
		return nil, jbig2errors.Error(processName, "'b' Bitmaps not defined")
	}
	switch tp {
	case LocSelectWidth, LocSelectHeight, LocSelectIfEither, LocSelectIfBoth:
	default:
		return nil, jbig2errors.Errorf(processName, "provided invalid location filter type: %d", tp)
	}
	switch relation {
	case SizeSelectIfLT, SizeSelectIfGT, SizeSelectIfLTE, SizeSelectIfGTE, SizeSelectIfEQ:
	default:
		return nil, jbig2errors.Errorf(processName, "invalid relation: '%d'", relation)
	}
	indicator, err := bms.makeSizeIndicator(width, height, tp, relation)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	result, err = bms.selectByIndicator(indicator)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return result, nil
}
func (ba *BitmapsArray) AddBitmaps(bm *Bitmaps) { ba.Values = append(ba.Values, bm) }

var (
	leftMask8  = []byte{0x00, 0x80, 0xC0, 0xE0, 0xF0, 0xF8, 0xFC, 0xFE, 0xFF}
	rightMask8 = []byte{0x00, 0x01, 0x03, 0x07, 0x0F, 0x1F, 0x3F, 0x7F, 0xFF}
)

func (bm *Bitmap) addBorderGeneral(left, right, top, bot int, val int) (*Bitmap, error) {
	const processName = "addBorderGeneral"
	if left < 0 || right < 0 || top < 0 || bot < 0 {
		return nil, jbig2errors.Error(processName, "negative border added")
	}
	w, h := bm.Width, bm.Height
	newW := w + left + right
	newH := h + top + bot
	result, err := New(newW, newH)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	result.Color = bm.Color
	op := PixClr
	if val > 0 {
		op = PixSet
	}
	err = result.RasterOperation(0, 0, left, newH, op, nil, 0, 0)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "left")
	}
	err = result.RasterOperation(newW-right, 0, right, newH, op, nil, 0, 0)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "right")
	}
	err = result.RasterOperation(0, 0, newW, top, op, nil, 0, 0)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "top")
	}
	err = result.RasterOperation(0, newH-bot, newW, bot, op, nil, 0, 0)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "bottom")
	}
	err = result.RasterOperation(left, top, w, h, PixSrc, bm, 0, 0)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "copy")
	}
	return result, nil
}

func (bms *Bitmaps) selectByIndicator(indicator *basic.NumSlice) (result *Bitmaps, err error) {
	const processName = "Bitmaps.selectByIndicator"
	if bms == nil {
		return nil, jbig2errors.Error(processName, "'b' bitmaps not defined")
	}
	if indicator == nil {
		return nil, jbig2errors.Error(processName, "'na' indicators not defined")
	}
	if len(bms.Values) == 0 {
		return bms, nil
	}
	if len(*indicator) != len(bms.Values) {
		return nil, jbig2errors.Errorf(processName, "na length: %d, is different than bitmaps: %d", len(*indicator), len(bms.Values))
	}
	var val, i, count int
	for i = 0; i < len(*indicator); i++ {
		if val, err = indicator.GetInt(i); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "first check")
		}
		if val == 1 {
			count++
		}
	}
	if count == len(bms.Values) {
		return bms, nil
	}
	result = &Bitmaps{}
	hasBoxes := len(bms.Values) == len(bms.Boxes)
	for i = 0; i < len(*indicator); i++ {
		if val = int((*indicator)[i]); val == 0 {
			continue
		}
		result.Values = append(result.Values, bms.Values[i])
		if hasBoxes {
			result.Boxes = append(result.Boxes, bms.Boxes[i])
		}
	}
	return result, nil
}

func reduceRankBinary2Level4(src, dest *Bitmap, level int, tab []byte, size int) (err error) {
	const processName = "reduceRankBinary2Level4"
	var (
		srcOff, destOff, y, destY, x, destX, k, idx int
		bits1, bits2                                uint32
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
			bits2 &= bits1
			bits2 &= bits2 << 1
			bits2 &= 0xaaaaaaaa
			bits1 = bits2 | (bits2 << 7)
			hiByte = byte(bits1 >> 24)
			loByte = byte((bits1 >> 8) & 0xff)
			idx = destOff + destX
			if idx+1 == len(dest.Data)-1 || idx+1 >= destOff+dest.RowStride {
				dest.Data[idx] = tab[hiByte]
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TstPSymbol(t *testing.T) *Bitmap {
	t.Helper()
	bm, err := New(5, 8)
	require.NoError(t, err)
	require.NoError(t, bm.SetPixel(0, 0, 1))
	require.NoError(t, bm.SetPixel(1, 0, 1))
	require.NoError(t, bm.SetPixel(2, 0, 1))
	require.NoError(t, bm.SetPixel(3, 0, 1))
	require.NoError(t, bm.SetPixel(4, 1, 1))
	require.NoError(t, bm.SetPixel(0, 1, 1))
	require.NoError(t, bm.SetPixel(4, 2, 1))
	require.NoError(t, bm.SetPixel(0, 2, 1))
	require.NoError(t, bm.SetPixel(4, 3, 1))
	require.NoError(t, bm.SetPixel(0, 3, 1))
	require.NoError(t, bm.SetPixel(0, 4, 1))
	require.NoError(t, bm.SetPixel(1, 4, 1))
	require.NoError(t, bm.SetPixel(2, 4, 1))
	require.NoError(t, bm.SetPixel(3, 4, 1))
	require.NoError(t, bm.SetPixel(0, 5, 1))
	require.NoError(t, bm.SetPixel(0, 6, 1))
	require.NoError(t, bm.SetPixel(0, 7, 1))
	return bm
}

func (bm *Bitmap) clipRectangle(box, outBox *image.Rectangle) (result *Bitmap, err error) {
	const processName = "clipRectangle"
	if box == nil {
		return nil, jbig2errors.Error(processName, "provided nil 'box'")
	}
	w, h := bm.Width, bm.Height
	clipped, err := ClipBoxToRectangle(box, w, h)
	if err != nil {
		common.Log.Warning("'box' doesn't overlap bitmap 'b': %v", err)
		return nil, nil
	}
	x, y := clipped.Min.X, clipped.Min.Y
	cw, ch := clipped.Max.X-clipped.Min.X, clipped.Max.Y-clipped.Min.Y
	result, err = New(cw, ch)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	result.Text = bm.Text
	if err = result.RasterOperation(0, 0, cw, ch, PixSrc, bm, x, y); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if outBox != nil {
		*outBox = *clipped
	}
	return result, nil
}

const (
	_ SizeComparison = iota
	SizeSelectIfLT
	SizeSelectIfGT
	SizeSelectIfLTE
	SizeSelectIfGTE
	SizeSelectIfEQ
)

func (bms *Bitmaps) AddBox(box *image.Rectangle) { bms.Boxes = append(bms.Boxes, box) }
func TstESymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(4, 5, []byte{0xF0, 0x80, 0xE0, 0x80, 0xF0})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func (bm *Bitmap) setEightFullBytes(idx int, val uint64) error {
	if idx+7 > len(bm.Data)-1 {
		return jbig2errors.Error("setEightBytes", "index out of range")
	}
	bm.Data[idx] = byte((val & 0xff00000000000000) >> 56)
	bm.Data[idx+1] = byte((val & 0xff000000000000) >> 48)
	bm.Data[idx+2] = byte((val & 0xff0000000000) >> 40)
	bm.Data[idx+3] = byte((val & 0xff00000000) >> 32)
	bm.Data[idx+4] = byte((val & 0xff000000) >> 24)
	bm.Data[idx+5] = byte((val & 0xff0000) >> 16)
	bm.Data[idx+6] = byte((val & 0xff00) >> 8)
	bm.Data[idx+7] = byte(val & 0xff)
	return nil
}

func (bm *Bitmap) RemoveBorderGeneral(left, right, top, bot int) (*Bitmap, error) {
	return bm.removeBorderGeneral(left, right, top, bot)
}

func (op CombinationOperator) String() string {
	var s string
	switch op {
	case CmbOpOr:
		s = "OR"
	case CmbOpAnd:
		s = "AND"
	case CmbOpXor:
		s = "XOR"
	case CmbOpXNor:
		s = "XNOR"
	case CmbOpReplace:
		s = "REPLACE"
	case CmbOpNot:
		s = "NOT"
	}
	return s
}

const maxBitmapBytes = 256 * 1024 * 1024

type SizeSelection int

func TstImageBitmapInverseData() []byte {
	bm := imageBitmap.Copy()
	bm.InverseData()
	return bm.Data
}

type Points []Point

func (boxes *Boxes) Add(box *image.Rectangle) error {
	if boxes == nil {
		return jbig2errors.Error("Boxes.Add", "'Boxes' not defined")
	}
	*boxes = append(*boxes, box)
	return nil
}
func (bm *Bitmap) InverseData() { bm.inverseData() }
func (bm *Bitmap) AddBorderGeneral(left, right, top, bot int, val int) (*Bitmap, error) {
	return bm.addBorderGeneral(left, right, top, bot, val)
}

func (bm *Bitmap) ToImage() image.Image {
	img, err := imageutil.NewImage(bm.Width, bm.Height, 1, 1, bm.Data, nil, nil)
	if err != nil {
		common.Log.Error("Converting jbig2.Bitmap to imageutil.Image failed: %v", err)
	}
	return img
}

func (bm *Bitmap) GetPixel(x, y int) bool {
	idx := bm.GetByteIndex(x, y)
	bitOff := bm.GetBitOffset(x)
	shift := uint(7 - bitOff)
	if idx > len(bm.Data)-1 {
		common.Log.Debug("Trying to get pixel out of the data range. x: '%d', y:'%d', bm: '%s'", x, y, bm)
		return false
	}
	if (bm.Data[idx]>>shift)&0x01 >= 1 {
		return true
	}
	return false
}
func (cp *ClassedPoints) Len() int { return cp.IntSlice.Size() }

type BoundaryCondition int

func closeMorph(dest, src *Bitmap, sel *Selection) (*Bitmap, error) {
	const processName = "closeBitmap"
	var err error
	if dest, err = processMorphArgs2(dest, src, sel); err != nil {
		return nil, err
	}
	temp, err := dilate(nil, src, sel)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if _, err = erode(dest, temp, sel); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return dest, nil
}

func (bms *Bitmaps) SelectByIndexes(idx []int) (*Bitmaps, error) {
	const processName = "Bitmaps.SortIndexesByHeight"
	result, err := bms.selectByIndexes(idx)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	return result, nil
}

func TstOSymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(4, 5, []byte{0xF0, 0x90, 0x90, 0x90, 0xF0})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func (cp *ClassedPoints) xSortFunction() func(i int, j int) bool {
	return func(i, j int) bool { return cp.XAtIndex(i) < cp.XAtIndex(j) }
}

const (
	_ SizeSelection = iota
	SizeSelectByWidth
	SizeSelectByHeight
	SizeSelectByMaxDimension
	SizeSelectByArea
	SizeSelectByPerimeter
)

func (s *byHeight) Swap(i, j int) {
	s.Values[i], s.Values[j] = s.Values[j], s.Values[i]
	if s.Boxes != nil {
		s.Boxes[i], s.Boxes[j] = s.Boxes[j], s.Boxes[i]
	}
}

func (bms *Bitmaps) ClipToBitmap(s *Bitmap) (*Bitmaps, error) {
	const processName = "Bitmaps.ClipToBitmap"
	if bms == nil {
		return nil, jbig2errors.Error(processName, "Bitmaps not defined")
	}
	if s == nil {
		return nil, jbig2errors.Error(processName, "source bitmap not defined")
	}
	n := len(bms.Values)
	result := &Bitmaps{Values: make([]*Bitmap, n), Boxes: make([]*image.Rectangle, n)}
	var (
		bm, clipped *Bitmap
		box         *image.Rectangle
		err         error
	)
	for i := 0; i < n; i++ {
		if bm, err = bms.GetBitmap(i); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if box, err = bms.GetBox(i); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if clipped, err = s.clipRectangle(box, nil); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if clipped, err = clipped.And(bm); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		result.Values[i] = clipped
		result.Boxes[i] = box
	}
	return result, nil
}

func seedFillStackBB8(s *Bitmap, stack *basic.Stack, x, y int) (bb *image.Rectangle, err error) {
	const processName = "seedFillStackBB"
	if s == nil {
		return nil, jbig2errors.Error(processName, "provided nil 's' Bitmap")
	}
	if stack == nil {
		return nil, jbig2errors.Error(processName, "provided nil 'stack'")
	}
	w, h := s.Width, s.Height
	xMax := w - 1
	yMax := h - 1
	if x < 0 || x > xMax || y < 0 || y > yMax || !s.GetPixel(x, y) {
		return nil, nil
	}
	var box *image.Rectangle
	box, err = Rect(100000, 100000, 0, 0)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if err = pushFillSegmentBoundingBox(stack, x, x, y, 1, yMax, box); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "initial push")
	}
	if err = pushFillSegmentBoundingBox(stack, x, x, y+1, -1, yMax, box); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "2nd initial push")
	}
	box.Min.X, box.Max.X = x, x
	box.Min.Y, box.Max.Y = y, y
	var (
		seg      *fillSegment
		runStart int
	)
	for stack.Len() > 0 {
		if seg, err = popFillSegment(stack); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		y = seg.y
		for x = seg.x1; x >= 0 && s.GetPixel(x, y); x-- {
			if err = s.SetPixel(x, y, 0); err != nil {
				return nil, jbig2errors.Wrap(err, processName, "")
			}
		}
		if x >= seg.x1 {
			for x++; x <= seg.x2 && x <= xMax && !s.GetPixel(x, y); x++ {
			}
			runStart = x
			if x > seg.x2 || x > xMax {
				continue
			}
		} else {
			runStart = x + 1
			if runStart < seg.x1-1 {
				if err = pushFillSegmentBoundingBox(stack, runStart, seg.x1-1, seg.y, -seg.dy, yMax, box); err != nil {
					return nil, jbig2errors.Wrap(err, processName, "leak on left side")
				}
			}
			x = seg.x1 + 1
		}
		for {
			for ; x <= xMax && s.GetPixel(x, y); x++ {
				if err = s.SetPixel(x, y, 0); err != nil {
					return nil, jbig2errors.Wrap(err, processName, "2nd set")
				}
			}
			if err = pushFillSegmentBoundingBox(stack, runStart, x-1, seg.y, seg.dy, yMax, box); err != nil {
				return nil, jbig2errors.Wrap(err, processName, "normal push")
			}
			if x > seg.x2+1 {
				if err = pushFillSegmentBoundingBox(stack, seg.x2+1, x-1, seg.y, -seg.dy, yMax, box); err != nil {
					return nil, jbig2errors.Wrap(err, processName, "leak on right side")
				}
			}
			for x++; x <= seg.x2 && x <= xMax && !s.GetPixel(x, y); x++ {
			}
			runStart = x
			if x > seg.x2 || x > xMax {
				break
			}
		}
	}
	box.Max.X++
	box.Max.Y++
	return box, nil
}
func (s *byWidth) Len() int { return len(s.Values) }
func blitShiftedWithTail(src, dst *Bitmap, startLine, endLine, dstIdx, srcStart, srcEnd, padding, xOffset, shift int, op CombinationOperator, srcTrailBits int) error {
	var line int
	advance := func() { line++; dstIdx += dst.RowStride; srcStart += src.RowStride; srcEnd += src.RowStride }
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
			acc = (acc | (uint16(srcByte) & 0xff)) << uint(shift)
			srcByte = byte(acc >> 8)
			if err = dst.SetByte(dIdx, combineBytes(dstByte, srcByte, op)); err != nil {
				return err
			}
			dIdx++
			acc <<= uint(xOffset)
			if sIdx == srcEnd {
				srcByte = byte(acc >> (8 - uint8(shift)))
				if srcTrailBits != 0 {
					srcByte = clearLowBits(uint(8+padding), srcByte)
				}
				dstByte, err = dst.GetByte(dIdx)
				if err != nil {
					return err
				}
				if err = dst.SetByte(dIdx, combineBytes(dstByte, srcByte, op)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func dilateBrick(dest, src *Bitmap, hSize, vSize int) (*Bitmap, error) {
	const processName = "dilateBrick"
	if src == nil {
		common.Log.Debug("dilateBrick source not defined")
		return nil, jbig2errors.Error(processName, "source bitmap not defined")
	}
	if hSize < 1 || vSize < 1 {
		return nil, jbig2errors.Error(processName, "hSzie and vSize are no greater equal to 1")
	}
	if hSize == 1 && vSize == 1 {
		copied, err := copyBitmap(dest, src)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "hSize == 1 && vSize == 1")
		}
		return copied, nil
	}
	if hSize == 1 || vSize == 1 {
		sel := SelCreateBrick(vSize, hSize, vSize/2, hSize/2, SelHit)
		result, err := dilate(dest, src, sel)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "hsize == 1 || vSize == 1")
		}
		return result, nil
	}
	selH := SelCreateBrick(1, hSize, 0, hSize/2, SelHit)
	selV := SelCreateBrick(vSize, 1, vSize/2, 0, SelHit)
	temp, err := dilate(nil, src, selH)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "1st dilate")
	}
	dest, err = dilate(dest, temp, selV)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "2nd dilate")
	}
	return dest, nil
}

func (bm *Bitmap) connComponentsBitmapsBB(bms *Bitmaps, connectivity int) (boxes *Boxes, err error) {
	const processName = "connComponentsBitmapsBB"
	if connectivity != 4 && connectivity != 8 {
		return nil, jbig2errors.Error(processName, "connectivity must be a '4' or '8'")
	}
	if bms == nil {
		return nil, jbig2errors.Error(processName, "provided nil Bitmaps")
	}
	if len(bms.Values) > 0 {
		return nil, jbig2errors.Error(processName, "provided non-empty Bitmaps")
	}
	if bm.Zero() {
		return &Boxes{}, nil
	}
	var work, rest, component, region *Bitmap
	bm.setPadBits(0)
	if work, err = copyBitmap(nil, bm); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "bm1")
	}
	if rest, err = copyBitmap(nil, bm); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "bm2")
	}
	stack := &basic.Stack{}
	stack.Aux = &basic.Stack{}
	boxes = &Boxes{}
	var (
		x, y  int
		pt    image.Point
		found bool
		box   *image.Rectangle
	)
	for {
		if pt, found, err = work.nextOnPixel(x, y); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if !found {
			break
		}
		if box, err = seedFillStackBB(work, stack, pt.X, pt.Y, connectivity); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if err = boxes.Add(box); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if component, err = work.clipRectangle(box, nil); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "bm3")
		}
		if region, err = rest.clipRectangle(box, nil); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "bm4")
		}
		if _, err = xor(component, component, region); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "bm3 ^ bm4")
		}
		if err = rest.RasterOperation(box.Min.X, box.Min.Y, box.Dx(), box.Dy(), PixSrcXorDst, component, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "bm2 -XOR-> bm3")
		}
		bms.AddBitmap(component)
		x = pt.X
		y = pt.Y
	}
	bms.Boxes = *boxes
	return boxes, nil
}

func (bms *Bitmaps) selectByIndexes(idx []int) (*Bitmaps, error) {
	result := &Bitmaps{}
	for _, i := range idx {
		bm, err := bms.GetBitmap(i)
		if err != nil {
			return nil, jbig2errors.Wrap(err, "selectByIndexes", "")
		}
		result.AddBitmap(bm)
	}
	return result, nil
}

func (pts Points) GetIntY(i int) (int, error) {
	if i >= len(pts) {
		return 0, jbig2errors.Errorf("Points.GetIntY", "index: '%d' out of range", i)
	}
	return int(pts[i].Y), nil
}

const maxFillIterations = 5000

func reduceRankBinaryCascade(src *Bitmap, levels ...int) (dest *Bitmap, err error) {
	const processName = "reduceRankBinaryCascade"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source bitmap not defined")
	}
	if len(levels) == 0 || len(levels) > 4 {
		return nil, jbig2errors.Error(processName, "there must be at least one and at most 4 levels")
	}
	if levels[0] <= 0 {
		common.Log.Debug("level1 <= 0 - no reduction")
		dest, err = copyBitmap(nil, src)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "level1 <= 0")
		}
		return dest, nil
	}
	tab := makeSubsampleTab2x()
	dest = src
	for i, level := range levels {
		if level <= 0 {
			break
		}
		dest, err = reduceRankBinary2(dest, level, tab)
		if err != nil {
			return nil, jbig2errors.Wrapf(err, processName, "level%d reduction", i)
		}
	}
	return dest, nil
}

type fillSegment struct {
	x1 int
	x2 int
	y  int
	dy int
}

func (pts *Points) AddPoint(x, y float32) { *pts = append(*pts, Point{x, y}) }
func Rect(x, y, w, h int) (*image.Rectangle, error) {
	const processName = "bitmap.Rect"
	if x < 0 {
		w += x
		x = 0
		if w <= 0 {
			return nil, jbig2errors.Errorf(processName, "x:'%d' < 0 and w: '%d' <= 0", x, w)
		}
	}
	if y < 0 {
		h += y
		y = 0
		if h <= 0 {
			return nil, jbig2errors.Error(processName, "y < 0 and box off +quad")
		}
	}
	r := image.Rect(x, y, x+w, y+h)
	return &r, nil
}

const (
	MopDilation MorphOperation = iota
	MopErosion
	MopOpening
	MopClosing
	MopRankBinaryReduction
	MopReplicativeBinaryExpansion
	MopAddBorder
)

type LocationFilter int

func (cp *ClassedPoints) GetIntXByClass(i int) (int, error) {
	const processName = "ClassedPoints.GetIntYByClass"
	if i >= cp.IntSlice.Size() {
		return 0, jbig2errors.Errorf(processName, "i: '%d' is out of the range of the IntSlice", i)
	}
	return int(cp.XAtIndex(i)), nil
}

func pushFillSegmentBoundingBox(stack *basic.Stack, x1, x2, y, dy, maxY int, bb *image.Rectangle) (err error) {
	const processName = "pushFillSegmentBoundingBox"
	if stack == nil {
		return jbig2errors.Error(processName, "nil stack provided")
	}
	if bb == nil {
		return jbig2errors.Error(processName, "provided nil image.Rectangle")
	}
	bb.Min.X = basic.Min(bb.Min.X, x1)
	bb.Max.X = basic.Max(bb.Max.X, x2)
	bb.Min.Y = basic.Min(bb.Min.Y, y)
	bb.Max.Y = basic.Max(bb.Max.Y, y)
	if y+dy < 0 || y+dy > maxY {
		return nil
	}
	if stack.Aux == nil {
		return jbig2errors.Error(processName, "auxStack not defined")
	}
	var seg *fillSegment
	item, ok := stack.Aux.Pop()
	if ok {
		if seg, ok = item.(*fillSegment); !ok {
			return jbig2errors.Error(processName, "auxStack data is not a *fillSegment")
		}
	} else {
		seg = &fillSegment{}
	}
	seg.x1 = x1
	seg.x2 = x2
	seg.y = y
	seg.dy = dy
	stack.Push(seg)
	return nil
}

type Getter interface{ GetBitmap() *Bitmap }

func (pts Points) GetIntX(i int) (int, error) {
	if i >= len(pts) {
		return 0, jbig2errors.Errorf("Points.GetIntX", "index: '%d' out of range", i)
	}
	return int(pts[i].X), nil
}

func Blit(src *Bitmap, dst *Bitmap, x, y int, op CombinationOperator) error {
	var startLine, srcStart int
	srcEnd := src.RowStride - 1
	if x < 0 {
		srcStart = -x
		x = 0
	} else if x+src.Width > dst.Width {
		srcEnd -= src.Width + x - dst.Width
	}
	if y < 0 {
		startLine = -y
		y = 0
		srcStart += src.RowStride
		srcEnd += src.RowStride
	} else if y+src.Height > dst.Height {
		startLine = src.Height + y - dst.Height
	}
	var (
		endLine int
		err     error
	)
	xOffset := x & 0x07
	shift := 8 - xOffset
	srcTrailBits := src.Width & 0x07
	padding := shift - srcTrailBits
	unaligned := shift&0x07 != 0
	fitsInRows := src.Width <= ((srcEnd-srcStart)<<3)+shift
	dstIdx := dst.GetByteIndex(x, y)
	maxLine := startLine + dst.Height
	endLine = min(src.Height, maxLine)
	switch {
	case !unaligned:
		err = blitAligned(src, dst, startLine, endLine, dstIdx, srcStart, srcEnd, op)
	case fitsInRows:
		err = blitShifted(src, dst, startLine, endLine, dstIdx, srcStart, srcEnd, padding, xOffset, shift, op)
	default:
		err = blitShiftedWithTail(src, dst, startLine, endLine, dstIdx, srcStart, srcEnd, padding, xOffset, shift, op, srcTrailBits)
	}
	return err
}

func CombineBytes(oldByte, newByte byte, op CombinationOperator) byte {
	return combineBytes(oldByte, newByte, op)
}

func (bm *Bitmap) inverseData() {
	if err := bm.RasterOperation(0, 0, bm.Width, bm.Height, PixNotDst, nil, 0, 0); err != nil {
		common.Log.Debug("Inverse data failed: '%v'", err)
	}
	if bm.Color == Chocolate {
		bm.Color = Vanilla
	} else {
		bm.Color = Chocolate
	}
}

func closeBrick(dest, src *Bitmap, hSize, vSize int) (*Bitmap, error) {
	const processName = "closeBrick"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source not defined")
	}
	if hSize < 1 || vSize < 1 {
		return nil, jbig2errors.Error(processName, "hSize and vSize not >= 1")
	}
	if hSize == 1 && vSize == 1 {
		return src.Copy(), nil
	}
	if hSize == 1 || vSize == 1 {
		sel := SelCreateBrick(vSize, hSize, vSize/2, hSize/2, SelHit)
		var err error
		dest, err = closeMorph(dest, src, sel)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "hSize == 1 || vSize == 1")
		}
		return dest, nil
	}
	selH := SelCreateBrick(1, hSize, 0, hSize/2, SelHit)
	selV := SelCreateBrick(vSize, 1, vSize/2, 0, SelHit)
	temp, err := dilate(nil, src, selH)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "1st dilate")
	}
	if dest, err = dilate(dest, temp, selV); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "2nd dilate")
	}
	if _, err = erode(temp, dest, selH); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "1st erode")
	}
	if _, err = erode(dest, temp, selV); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "2nd erode")
	}
	return dest, nil
}

func (pts *Points) Add(pt *Points) error {
	const processName = "Points.Add"
	if pts == nil {
		return jbig2errors.Error(processName, "points not defined")
	}
	if pt == nil {
		return jbig2errors.Error(processName, "argument points not defined")
	}
	*pts = append(*pts, *pt...)
	return nil
}

var imageBitmapData = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x3E, 0x78, 0x27, 0xC2, 0x27, 0x91, 0x00, 0x22, 0x48, 0x21, 0x03, 0x24, 0x91, 0x00, 0x22, 0x48, 0x21, 0x02, 0xA4, 0x95, 0x00, 0x22, 0x48, 0x21, 0x02, 0x64, 0x9B, 0x00, 0x3C, 0x78, 0x21, 0x02, 0x27, 0x91, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7F, 0xF8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7F, 0xF8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x63, 0x18, 0x00, 0x00, 0x00, 0x00, 0x00, 0x63, 0x18, 0x00, 0x00, 0x00, 0x00, 0x00, 0x63, 0x18, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7F, 0xF8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x15, 0x50, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

func erodeBrick(dest, src *Bitmap, hSize, vSize int) (*Bitmap, error) {
	const processName = "erodeBrick"
	if src == nil {
		return nil, jbig2errors.Error(processName, "source not defined")
	}
	if hSize < 1 || vSize < 1 {
		return nil, jbig2errors.Error(processName, "hsize and vsize are not greater than or equal to 1")
	}
	if hSize == 1 && vSize == 1 {
		copied, err := copyBitmap(dest, src)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "hSize == 1 && vSize == 1")
		}
		return copied, nil
	}
	if hSize == 1 || vSize == 1 {
		sel := SelCreateBrick(vSize, hSize, vSize/2, hSize/2, SelHit)
		result, err := erode(dest, src, sel)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "hSize == 1 || vSize == 1")
		}
		return result, nil
	}
	selH := SelCreateBrick(1, hSize, 0, hSize/2, SelHit)
	selV := SelCreateBrick(vSize, 1, vSize/2, 0, SelHit)
	temp, err := erode(nil, src, selH)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "1st erode")
	}
	dest, err = erode(dest, temp, selV)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "2nd erode")
	}
	return dest, nil
}
func TstImageBitmapData() []byte { return imageBitmap.Data }
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

func reduceRankBinary2Level2(src, dest *Bitmap, _ int, tab []byte, size int) (err error) {
	const processName = "reduceRankBinary2Level2"
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
			bits2 = andBits | orBits
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

func (bm *Bitmap) Zero() bool {
	fullBytes := bm.Width / 8
	remBits := bm.Width & 7
	var lastMask byte
	if remBits != 0 {
		lastMask = byte(0xff << uint(8-remBits))
	}
	var idx, y, x int
	for y = 0; y < bm.Height; y++ {
		idx = bm.RowStride * y
		for x = 0; x < fullBytes; x, idx = x+1, idx+1 {
			if bm.Data[idx] != 0 {
				return false
			}
		}
		if remBits > 0 {
			if bm.Data[idx]&lastMask != 0 {
				return false
			}
		}
	}
	return true
}

func (s *byHeight) Less(i, j int) bool                             { return s.Values[i].Height < s.Values[j].Height }
func Dilate(d *Bitmap, s *Bitmap, sel *Selection) (*Bitmap, error) { return dilate(d, s, sel) }
func (bm *Bitmap) ClipRectangle(box *image.Rectangle) (result *Bitmap, clipped *image.Rectangle, err error) {
	const processName = "ClipRectangle"
	if box == nil {
		return nil, nil, jbig2errors.Error(processName, "box is not defined")
	}
	w, h := bm.Width, bm.Height
	bounds := image.Rect(0, 0, w, h)
	if !box.Overlaps(bounds) {
		return nil, nil, jbig2errors.Error(processName, "box doesn't overlap b")
	}
	inter := box.Intersect(bounds)
	x, y := inter.Min.X, inter.Min.Y
	cw, ch := inter.Dx(), inter.Dy()
	result, err = New(cw, ch)
	if err != nil {
		return nil, nil, jbig2errors.Wrap(err, processName, "")
	}
	result.Text = bm.Text
	if err = result.RasterOperation(0, 0, cw, ch, PixSrc, bm, x, y); err != nil {
		return nil, nil, jbig2errors.Wrap(err, processName, "PixSrc to clipped")
	}
	clipped = &inter
	return result, clipped, nil
}

type Point struct{ X, Y float32 }

func (bm *Bitmap) setAll() error {
	err := rasterOperation(bm, 0, 0, bm.Width, bm.Height, PixSet, nil, 0, 0)
	if err != nil {
		return jbig2errors.Wrap(err, "setAll", "")
	}
	return nil
}

func (bm *Bitmap) AddBorder(borderSize, val int) (*Bitmap, error) {
	if borderSize == 0 {
		return bm.Copy(), nil
	}
	result, err := bm.addBorderGeneral(borderSize, borderSize, borderSize, borderSize, val)
	if err != nil {
		return nil, jbig2errors.Wrap(err, "AddBorder", "")
	}
	return result, nil
}

func (bm *Bitmap) addPadBits() (err error) {
	const processName = "bitmap.addPadBits"
	remBits := bm.Width % 8
	if remBits == 0 {
		return nil
	}
	fullBytes := bm.Width / 8
	r := bitwise.NewReader(bm.Data)
	data := make([]byte, bm.Height*bm.RowStride)
	w := bitwise.NewWriterMSB(data)
	buf := make([]byte, fullBytes)
	var (
		y    int
		bits uint64
	)
	for y = 0; y < bm.Height; y++ {
		if _, err = r.Read(buf); err != nil {
			return jbig2errors.Wrap(err, processName, "full byte")
		}
		if _, err = w.Write(buf); err != nil {
			return jbig2errors.Wrap(err, processName, "full bytes")
		}
		if bits, err = r.ReadBits(byte(remBits)); err != nil {
			return jbig2errors.Wrap(err, processName, "skipping bits")
		}
		if err = w.WriteByte(byte(bits) << uint(8-remBits)); err != nil {
			return jbig2errors.Wrap(err, processName, "last byte")
		}
	}
	bm.Data = w.Data()
	return nil
}

func erode(dest, src *Bitmap, sel *Selection) (*Bitmap, error) {
	const processName = "erode"
	var (
		err  error
		temp *Bitmap
	)
	dest, err = processMorphArgs1(dest, src, sel, &temp)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if err = dest.setAll(); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	var selVal SelectionValue
	for i := 0; i < sel.Height; i++ {
		for j := 0; j < sel.Width; j++ {
			selVal = sel.Data[i][j]
			if selVal == SelHit {
				err = rasterOperation(dest, sel.Cx-j, sel.Cy-i, src.Width, src.Height, PixSrcAndDst, temp, 0, 0)
				if err != nil {
					return nil, jbig2errors.Wrap(err, processName, "")
				}
			}
		}
	}
	if morphBoundaryCondition == symmetricMorphBC {
		return dest, nil
	}
	xp, yp, xn, yn := sel.findMaxTranslations()
	if xp > 0 {
		if err = dest.RasterOperation(0, 0, xp, src.Height, PixClr, nil, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "xp > 0")
		}
	}
	if xn > 0 {
		if err = dest.RasterOperation(src.Width-xn, 0, xn, src.Height, PixClr, nil, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "xn > 0")
		}
	}
	if yp > 0 {
		if err = dest.RasterOperation(0, 0, src.Width, yp, PixClr, nil, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "yp > 0")
		}
	}
	if yn > 0 {
		if err = dest.RasterOperation(0, src.Height-yn, src.Width, yn, PixClr, nil, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "yn > 0")
		}
	}
	return dest, nil
}
func (cp *ClassedPoints) YAtIndex(i int) float32 { return (*cp.Points)[cp.IntSlice[i]].Y }
func (bm *Bitmap) setTwoBytes(idx int, val uint16) error {
	if idx+1 > len(bm.Data)-1 {
		return jbig2errors.Errorf("setTwoBytes", "index: '%d' out of range", idx)
	}
	bm.Data[idx] = byte((val & 0xff00) >> 8)
	bm.Data[idx+1] = byte(val & 0xff)
	return nil
}

func ClipBoxToRectangle(box *image.Rectangle, wi, hi int) (result *image.Rectangle, err error) {
	const processName = "ClipBoxToRectangle"
	if box == nil {
		return nil, jbig2errors.Error(processName, "'box' not defined")
	}
	if box.Min.X >= wi || box.Min.Y >= hi || box.Max.X <= 0 || box.Max.Y <= 0 {
		return nil, jbig2errors.Error(processName, "'box' outside rectangle")
	}
	r := *box
	result = &r
	if result.Min.X < 0 {
		result.Max.X += result.Min.X
		result.Min.X = 0
	}
	if result.Min.Y < 0 {
		result.Max.Y += result.Min.Y
		result.Min.Y = 0
	}
	if result.Max.X > wi {
		result.Max.X = wi
	}
	if result.Max.Y > hi {
		result.Max.Y = hi
	}
	return result, nil
}

func makeExpandTab2x() (tab [256]uint16) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			tab[i] |= 0x3
		}
		if i&0x02 != 0 {
			tab[i] |= 0xc
		}
		if i&0x04 != 0 {
			tab[i] |= 0x30
		}
		if i&0x08 != 0 {
			tab[i] |= 0xc0
		}
		if i&0x10 != 0 {
			tab[i] |= 0x300
		}
		if i&0x20 != 0 {
			tab[i] |= 0xc00
		}
		if i&0x40 != 0 {
			tab[i] |= 0x3000
		}
		if i&0x80 != 0 {
			tab[i] |= 0xc000
		}
	}
	return tab
}

const (
	ComponentConn Component = iota
	ComponentCharacters
	ComponentWords
)

func TstRSymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(4, 5, []byte{0xF0, 0x90, 0xF0, 0xA0, 0x90})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func TstISymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(1, 5, []byte{0x80, 0x80, 0x80, 0x80, 0x80})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func expandBinaryFactor4(dest, src *Bitmap) (err error) {
	const processName = "expandBinaryFactor4"
	srcStride := src.RowStride
	destStride := dest.RowStride
	overhang := src.RowStride*4 - dest.RowStride
	var (
		b, outByte                             byte
		expanded                               uint32
		srcOff, destOff, y, x, k, destIdx, idx int
	)
	for y = 0; y < src.Height; y++ {
		srcOff = y * srcStride
		destOff = 4 * y * destStride
		for x = 0; x < srcStride; x++ {
			b = src.Data[srcOff+x]
			expanded = expandTab4x[b]
			destIdx = destOff + x*4
			if overhang != 0 && (x+1)*4 > dest.RowStride {
				for k = overhang; k > 0; k-- {
					outByte = byte((expanded >> uint(k*8)) & 0xff)
					idx = destIdx + (overhang - k)
					if err = dest.SetByte(idx, outByte); err != nil {
						return jbig2errors.Wrapf(err, processName, "Different rowstrides. K: %d", k)
					}
				}
			} else if err = dest.setFourBytes(destIdx, expanded); err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			if err = dest.setFourBytes(destOff+x*4, expandTab4x[src.Data[srcOff+x]]); err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
		}
		for k = 1; k < 4; k++ {
			for x = 0; x < destStride; x++ {
				if err = dest.SetByte(destOff+k*destStride+x, dest.Data[destOff+x]); err != nil {
					return jbig2errors.Wrapf(err, processName, "copy 'quadrable' line: '%d', byte: '%d'", k, x)
				}
			}
		}
	}
	return nil
}

func (bm *Bitmap) GetByte(index int) (byte, error) {
	if index > len(bm.Data)-1 || index < 0 {
		return 0, jbig2errors.Errorf("GetByte", "index: %d out of range", index)
	}
	return bm.Data[index], nil
}

func (bms *Bitmaps) GetBitmap(i int) (*Bitmap, error) {
	const processName = "GetBitmap"
	if bms == nil {
		return nil, jbig2errors.Error(processName, "provided nil Bitmaps")
	}
	if i > len(bms.Values)-1 {
		return nil, jbig2errors.Errorf(processName, "index: '%d' out of range", i)
	}
	return bms.Values[i], nil
}

func (bm *Bitmap) Equals(s *Bitmap) bool {
	if len(bm.Data) != len(s.Data) || bm.Width != s.Width || bm.Height != s.Height {
		return false
	}
	for y := 0; y < bm.Height; y++ {
		off := y * bm.RowStride
		for x := 0; x < bm.RowStride; x++ {
			if bm.Data[off+x] != s.Data[off+x] {
				return false
			}
		}
	}
	return true
}

func (bm *Bitmap) SetPixel(x, y int, pixel byte) error {
	idx := bm.GetByteIndex(x, y)
	if idx > len(bm.Data)-1 {
		return jbig2errors.Errorf("SetPixel", "index out of range: %d", idx)
	}
	bitOff := bm.GetBitOffset(x)
	shift := uint(7 - bitOff)
	b := bm.Data[idx]
	var newByte byte
	if pixel == 1 {
		newByte = b | (pixel & 0x01 << shift)
	} else {
		newByte = b &^ (1 << shift)
	}
	bm.Data[idx] = newByte
	return nil
}

func reduceRankBinary2Level1(src, dest *Bitmap, _ int, tab []byte, size int) (err error) {
	const processName = "reduceRankBinary2Level1"
	var (
		srcOff, destOff, y, destY, x, destX, k, idx int
		bits1, bits2                                uint32
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
			bits2 |= bits1
			bits2 |= bits2 << 1
			bits2 &= 0xaaaaaaaa
			bits1 = bits2 | (bits2 << 7)
			hiByte = byte(bits1 >> 24)
			loByte = byte((bits1 >> 8) & 0xff)
			idx = destOff + destX
			if idx+1 == len(dest.Data)-1 || idx+1 >= destOff+dest.RowStride {
				dest.Data[idx] = tab[hiByte]
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

func TstGetScaledSymbol(t *testing.T, sm *Bitmap, scale ...int) *Bitmap {
	if len(scale) == 0 {
		return sm
	}
	if scale[0] == 1 {
		return sm
	}
	scaled, err := MorphSequence(sm, MorphProcess{Operation: MopReplicativeBinaryExpansion, Arguments: scale})
	require.NoError(t, err)
	return scaled
}

type byHeight Bitmaps

func subtract(dest, a, b *Bitmap) (*Bitmap, error) {
	const processName = "subtract"
	if a == nil {
		return nil, jbig2errors.Error(processName, "'s1' is nil")
	}
	if b == nil {
		return nil, jbig2errors.Error(processName, "'s2' is nil")
	}
	var err error
	switch dest {
	case a:
		if err = dest.RasterOperation(0, 0, a.Width, a.Height, PixNotSrcAndDst, b, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "d == s1")
		}
	case b:
		if err = dest.RasterOperation(0, 0, a.Width, a.Height, PixNotSrcAndDst, a, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "d == s2")
		}
	default:
		dest, err = copyBitmap(dest, a)
		if err != nil {
			return nil, jbig2errors.Wrap(err, processName, "")
		}
		if err = dest.RasterOperation(0, 0, a.Width, a.Height, PixNotSrcAndDst, b, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "default")
		}
	}
	return dest, nil
}

var (
	expandTab2x = makeExpandTab2x()
	expandTab4x = makeExpandTab4x()
	expandTab8x = makeExpandTab8x()
)

func (mp MorphProcess) getWidthHeight() (w, h int) {
	return mp.Arguments[0], mp.Arguments[1]
}
func (cp *ClassedPoints) SortByX() { cp.lessFunc = cp.xSortFunction(); sort.Sort(cp) }
func (bms *Bitmaps) GetBox(i int) (*image.Rectangle, error) {
	const processName = "GetBox"
	if bms == nil {
		return nil, jbig2errors.Error(processName, "provided nil 'Bitmaps'")
	}
	if i > len(bms.Boxes)-1 {
		return nil, jbig2errors.Errorf(processName, "index: '%d' out of range", i)
	}
	return bms.Boxes[i], nil
}

func (bm *Bitmap) countPixels() int {
	var (
		sum      int
		lastMask uint8
		b        byte
		x        int
	)
	fullBytes := bm.RowStride
	remBits := uint(bm.Width & 0x07)
	if remBits != 0 {
		lastMask = uint8((0xff << (8 - remBits)) & 0xff)
		fullBytes--
	}
	for y := 0; y < bm.Height; y++ {
		for x = 0; x < fullBytes; x++ {
			b = bm.Data[y*bm.RowStride+x]
			sum += int(popCountTab[b])
		}
		if remBits != 0 {
			sum += int(popCountTab[bm.Data[y*bm.RowStride+x]&lastMask])
		}
	}
	return sum
}

func (bm *Bitmap) RasterOperation(dx, dy, dw, dh int, op RasterOperator, src *Bitmap, sx, sy int) error {
	return rasterOperation(bm, dx, dy, dw, dh, op, src, sx, sy)
}
func (bm *Bitmap) CountPixels() int { return bm.countPixels() }
func newBitmapNoData(width, height int) *Bitmap {
	return &Bitmap{Width: width, Height: height, RowStride: (width + 7) >> 3}
}

func CorrelationScoreSimple(bm1, bm2 *Bitmap, area1, area2 int, delX, delY float32, maxDiffW, maxDiffH int, tab []int) (score float64, err error) {
	const processName = "CorrelationScoreSimple"
	if bm1 == nil || bm2 == nil {
		return score, jbig2errors.Error(processName, "nil bitmaps provided")
	}
	if tab == nil {
		return score, jbig2errors.Error(processName, "tab undefined")
	}
	if area1 == 0 || area2 == 0 {
		return score, jbig2errors.Error(processName, "provided areas must be > 0")
	}
	w1, h1 := bm1.Width, bm1.Height
	w2, h2 := bm2.Width, bm2.Height
	if absInt(w1-w2) > maxDiffW {
		return 0, nil
	}
	if absInt(h1-h2) > maxDiffH {
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
	temp := bm1.createTemplate()
	if err = temp.RasterOperation(dx, dy, w2, h2, PixSrc, bm2, 0, 0); err != nil {
		return score, jbig2errors.Wrap(err, processName, "bm2 to Template")
	}
	if err = temp.RasterOperation(0, 0, w1, h1, PixSrcAndDst, bm1, 0, 0); err != nil {
		return score, jbig2errors.Wrap(err, processName, "bm1 and bmT")
	}
	count := temp.countPixels()
	score = float64(count) * float64(count) / (float64(area1) * float64(area2))
	return score, nil
}

func RasterOperation(dest *Bitmap, dx, dy, dw, dh int, op RasterOperator, src *Bitmap, sx, sy int) error {
	return rasterOperation(dest, dx, dy, dw, dh, op, src, sx, sy)
}

func TstAddSymbol(t *testing.T, bms *Bitmaps, sym *Bitmap, x *int, y int, space int) {
	bms.AddBitmap(sym)
	box := image.Rect(*x, y, *x+sym.Width, y+sym.Height)
	bms.AddBox(&box)
	*x += sym.Width + space
}

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

func (bm *Bitmap) equivalent(other *Bitmap) bool {
	if bm == other {
		return true
	}
	if !bm.SizesEqual(other) {
		return false
	}
	xored, err := combineBitmaps(bm, other, CmbOpXor)
	if err != nil {
		return false
	}
	count := bm.countPixels()
	thresh := int(0.25 * float32(count))
	if xored.thresholdPixelSum(thresh) {
		return false
	}
	var (
		cellCounts [9][9]int
		hHalves    [18][9]int
		vHalves    [9][18]int
		xShift     int
		yShift     int
	)
	gridSize := 9
	cellH := bm.Height / gridSize
	cellW := bm.Width / gridSize
	a, b := cellH/2, cellW/2
	if cellH < cellW {
		a = cellW / 2
		b = cellH / 2
	}
	ellipseArea := float64(a) * float64(b) * math.Pi
	vHalfThresh := int(float64(cellH*cellW/2) * 0.9)
	hHalfThresh := int(float64(cellW*cellH/2) * 0.9)
	for gx := 0; gx < gridSize; gx++ {
		x0 := cellW*gx + xShift
		var x1 int
		if gx == gridSize-1 {
			xShift = 0
			x1 = bm.Width
		} else {
			x1 = x0 + cellW
			if ((bm.Width - xShift) % gridSize) > 0 {
				xShift++
				x1++
			}
		}
		for gy := 0; gy < gridSize; gy++ {
			y0 := cellH*gy + yShift
			var y1 int
			if gy == gridSize-1 {
				yShift = 0
				y1 = bm.Height
			} else {
				y1 = y0 + cellH
				if (bm.Height-yShift)%gridSize > 0 {
					yShift++
					y1++
				}
			}
			var left, right, bottom, top int
			midX := (x0 + x1) / 2
			midY := (y0 + y1) / 2
			for x := x0; x < x1; x++ {
				for y := y0; y < y1; y++ {
					if xored.GetPixel(x, y) {
						if x < midX {
							left++
						} else {
							right++
						}
						if y < midY {
							top++
						} else {
							bottom++
						}
					}
				}
			}
			cellCounts[gx][gy] = left + right
			hHalves[gx*2][gy] = left
			hHalves[gx*2+1][gy] = right
			vHalves[gx][gy*2] = top
			vHalves[gx][gy*2+1] = bottom
		}
	}
	for i := 0; i < gridSize*2-1; i++ {
		for j := 0; j < (gridSize - 1); j++ {
			var sum int
			for di := 0; di < 2; di++ {
				for dj := 0; dj < 2; dj++ {
					sum += hHalves[i+di][j+dj]
				}
			}
			if sum > hHalfThresh {
				return false
			}
		}
	}
	for i := 0; i < (gridSize - 1); i++ {
		for j := 0; j < ((gridSize * 2) - 1); j++ {
			var sum int
			for di := 0; di < 2; di++ {
				for dj := 0; dj < 2; dj++ {
					sum += vHalves[i+di][j+dj]
				}
			}
			if sum > vHalfThresh {
				return false
			}
		}
	}
	for i := 0; i < (gridSize - 2); i++ {
		for j := 0; j < (gridSize - 2); j++ {
			var diagSum, antiDiagSum int
			for di := 0; di < 3; di++ {
				for dj := 0; dj < 3; dj++ {
					if di == dj {
						diagSum += cellCounts[i+di][j+dj]
					}
					if (2 - di) == dj {
						antiDiagSum += cellCounts[i+di][j+dj]
					}
				}
			}
			if diagSum > hHalfThresh || antiDiagSum > hHalfThresh {
				return false
			}
		}
	}
	for i := 0; i < (gridSize - 1); i++ {
		for j := 0; j < (gridSize - 1); j++ {
			var sum int
			for di := 0; di < 2; di++ {
				for dj := 0; dj < 2; dj++ {
					sum += cellCounts[i+di][j+dj]
				}
			}
			if float64(sum) > ellipseArea {
				return false
			}
		}
	}
	return true
}

func copyBitmap(dest, src *Bitmap) (*Bitmap, error) {
	if src == nil {
		return nil, jbig2errors.Error("copyBitmap", "source not defined")
	}
	if src == dest {
		return dest, nil
	}
	if dest == nil {
		dest = src.createTemplate()
		copy(dest.Data, src.Data)
		return dest, nil
	}
	err := dest.resizeImageData(src)
	if err != nil {
		return nil, jbig2errors.Wrap(err, "copyBitmap", "")
	}
	dest.Text = src.Text
	copy(dest.Data, src.Data)
	return dest, nil
}

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

func HausTest(p1, p2, p3, p4 *Bitmap, delX, delY float32, maxDiffW, maxDiffH int) (bool, error) {
	const processName = "HausTest"
	w1, h1 := p1.Width, p1.Height
	w3, h3 := p3.Width, p3.Height
	if basic.Abs(w1-w3) > maxDiffW {
		return false, nil
	}
	if basic.Abs(h1-h3) > maxDiffH {
		return false, nil
	}
	dx := int(delX + basic.Sign(delX)*0.5)
	dy := int(delY + basic.Sign(delY)*0.5)
	var err error
	temp := p1.CreateTemplate()
	if err = temp.RasterOperation(0, 0, w1, h1, PixSrc, p1, 0, 0); err != nil {
		return false, jbig2errors.Wrap(err, processName, "p1 -SRC-> t")
	}
	if err = temp.RasterOperation(dx, dy, w1, h1, PixNotSrcAndDst, p4, 0, 0); err != nil {
		return false, jbig2errors.Wrap(err, processName, "!p4 & t")
	}
	if temp.Zero() {
		return false, nil
	}
	if err = temp.RasterOperation(dx, dy, w3, h3, PixSrc, p3, 0, 0); err != nil {
		return false, jbig2errors.Wrap(err, processName, "p3 -SRC-> t")
	}
	if err = temp.RasterOperation(0, 0, w3, h3, PixNotSrcAndDst, p2, 0, 0); err != nil {
		return false, jbig2errors.Wrap(err, processName, "!p2 & t")
	}
	return temp.Zero(), nil
}
func MakePixelCentroidTab8() []int { return makePixelCentroidTab8() }

var reductionTab = [5]int{1, 2, 3, 0, 4}

func (bms *Bitmaps) String() string {
	sb := strings.Builder{}
	for _, bm := range bms.Values {
		sb.WriteString(bm.String())
		sb.WriteRune('\n')
	}
	return sb.String()
}

type BitmapsArray struct {
	Values []*Bitmaps
	Boxes  []*image.Rectangle
}

func (bms *Bitmaps) SortByHeight() { sorter := (*byHeight)(bms); sort.Sort(sorter) }
func (boxes *Boxes) Get(i int) (*image.Rectangle, error) {
	const processName = "Boxes.Get"
	if boxes == nil {
		return nil, jbig2errors.Error(processName, "'Boxes' not defined")
	}
	if i > len(*boxes)-1 {
		return nil, jbig2errors.Errorf(processName, "index: '%d' out of range", i)
	}
	return (*boxes)[i], nil
}
func (sel *Selection) setOrigin(cy, cx int) { sel.Cy, sel.Cx = cy, cx }
func SelCreateBrick(h, w int, cy, cx int, tp SelectionValue) *Selection {
	sel := newSelection(h, w, "")
	sel.setOrigin(cy, cx)
	var i, j int
	for i = 0; i < h; i++ {
		for j = 0; j < w; j++ {
			sel.Data[i][j] = tp
		}
	}
	return sel
}

func (bm *Bitmap) setEightPartlyBytes(idx, n int, val uint64) (err error) {
	var (
		b     byte
		shift int
	)
	const processName = "setEightPartlyBytes"
	for i := 1; i <= n; i++ {
		shift = 64 - i*8
		b = byte(val >> uint(shift) & 0xff)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("temp: %08b, index: %d, idx: %d, fullBytesNumber: %d, shift: %d", b, idx, idx+i-1, n, shift)
		}
		if err = bm.SetByte(idx+i-1, b); err != nil {
			return jbig2errors.Wrap(err, processName, "fullByte")
		}
	}
	padBits := bm.RowStride*8 - bm.Width
	if padBits == 0 {
		return nil
	}
	shift -= 8
	b = byte(val>>uint(shift)&0xff) << uint(padBits)
	if err = bm.SetByte(idx+n, b); err != nil {
		return jbig2errors.Wrap(err, processName, "padded")
	}
	return nil
}

func (bm *Bitmap) ThresholdPixelSum(thresh int, tab8 []int) (exceeds bool, err error) {
	const processName = "Bitmap.ThresholdPixelSum"
	if tab8 == nil {
		tab8 = makePixelSumTab8()
	}
	fullBytes := bm.Width >> 3
	remBits := bm.Width & 7
	lastMask := byte(0xff << uint(8-remBits))
	var (
		y, x, off, sum int
		b              byte
	)
	for y = 0; y < bm.Height; y++ {
		off = bm.RowStride * y
		for x = 0; x < fullBytes; x++ {
			b, err = bm.GetByte(off + x)
			if err != nil {
				return false, jbig2errors.Wrap(err, processName, "fullByte")
			}
			sum += tab8[b]
		}
		if remBits != 0 {
			b, err = bm.GetByte(off + x)
			if err != nil {
				return false, jbig2errors.Wrap(err, processName, "partialByte")
			}
			b &= lastMask
			sum += tab8[b]
		}
		if sum > thresh {
			return true, nil
		}
	}
	return exceeds, nil
}
