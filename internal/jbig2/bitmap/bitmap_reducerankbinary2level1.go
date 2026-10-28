// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"encoding/binary"
	"image"
	"math"
	"sort"
	"testing"

	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

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
