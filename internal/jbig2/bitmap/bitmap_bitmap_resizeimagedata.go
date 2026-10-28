// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"encoding/binary"
	"image"
	"sort"
	"testing"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

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
