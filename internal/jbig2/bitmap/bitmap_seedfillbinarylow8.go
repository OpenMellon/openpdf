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
