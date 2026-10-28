// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"math"
	"testing"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

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
