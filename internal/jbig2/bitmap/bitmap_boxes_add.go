// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"image"
	"testing"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

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
