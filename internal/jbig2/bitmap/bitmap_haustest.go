// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"image"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
