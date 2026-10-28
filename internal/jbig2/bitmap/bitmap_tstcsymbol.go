// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"fmt"
	"testing"

	"github.com/OpenMellon/openpdf/internal/jbig2/basic"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

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
