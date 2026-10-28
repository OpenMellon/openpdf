// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"image"
	"testing"

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
