// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"image"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
