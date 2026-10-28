// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"strings"

	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
)

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
