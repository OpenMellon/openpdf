// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package bitmap

import (
	"encoding/binary"
	"image"
	"testing"

	"github.com/OpenMellon/openpdf/internal/bitwise"
	jbig2errors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"github.com/stretchr/testify/require"
)

func reduceRankBinary2Level2(src, dest *Bitmap, _ int, tab []byte, size int) (err error) {
	const processName = "reduceRankBinary2Level2"
	var (
		srcOff, destOff, y, destY, x, destX, k, idx int
		bits1, bits2, andBits, orBits               uint32
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
			andBits = bits1 & bits2
			andBits |= andBits << 1
			orBits = bits1 | bits2
			orBits &= orBits << 1
			bits2 = andBits | orBits
			bits2 &= 0xaaaaaaaa
			bits1 = bits2 | (bits2 << 7)
			hiByte = byte(bits1 >> 24)
			loByte = byte((bits1 >> 8) & 0xff)
			idx = destOff + destX
			if idx+1 == len(dest.Data)-1 || idx+1 >= destOff+dest.RowStride {
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

func (bm *Bitmap) Zero() bool {
	fullBytes := bm.Width / 8
	remBits := bm.Width & 7
	var lastMask byte
	if remBits != 0 {
		lastMask = byte(0xff << uint(8-remBits))
	}
	var idx, y, x int
	for y = 0; y < bm.Height; y++ {
		idx = bm.RowStride * y
		for x = 0; x < fullBytes; x, idx = x+1, idx+1 {
			if bm.Data[idx] != 0 {
				return false
			}
		}
		if remBits > 0 {
			if bm.Data[idx]&lastMask != 0 {
				return false
			}
		}
	}
	return true
}

func (s *byHeight) Less(i, j int) bool                             { return s.Values[i].Height < s.Values[j].Height }
func Dilate(d *Bitmap, s *Bitmap, sel *Selection) (*Bitmap, error) { return dilate(d, s, sel) }
func (bm *Bitmap) ClipRectangle(box *image.Rectangle) (result *Bitmap, clipped *image.Rectangle, err error) {
	const processName = "ClipRectangle"
	if box == nil {
		return nil, nil, jbig2errors.Error(processName, "box is not defined")
	}
	w, h := bm.Width, bm.Height
	bounds := image.Rect(0, 0, w, h)
	if !box.Overlaps(bounds) {
		return nil, nil, jbig2errors.Error(processName, "box doesn't overlap b")
	}
	inter := box.Intersect(bounds)
	x, y := inter.Min.X, inter.Min.Y
	cw, ch := inter.Dx(), inter.Dy()
	result, err = New(cw, ch)
	if err != nil {
		return nil, nil, jbig2errors.Wrap(err, processName, "")
	}
	result.Text = bm.Text
	if err = result.RasterOperation(0, 0, cw, ch, PixSrc, bm, x, y); err != nil {
		return nil, nil, jbig2errors.Wrap(err, processName, "PixSrc to clipped")
	}
	clipped = &inter
	return result, clipped, nil
}

type Point struct{ X, Y float32 }

func (bm *Bitmap) setAll() error {
	err := rasterOperation(bm, 0, 0, bm.Width, bm.Height, PixSet, nil, 0, 0)
	if err != nil {
		return jbig2errors.Wrap(err, "setAll", "")
	}
	return nil
}

func (bm *Bitmap) AddBorder(borderSize, val int) (*Bitmap, error) {
	if borderSize == 0 {
		return bm.Copy(), nil
	}
	result, err := bm.addBorderGeneral(borderSize, borderSize, borderSize, borderSize, val)
	if err != nil {
		return nil, jbig2errors.Wrap(err, "AddBorder", "")
	}
	return result, nil
}

func (bm *Bitmap) addPadBits() (err error) {
	const processName = "bitmap.addPadBits"
	remBits := bm.Width % 8
	if remBits == 0 {
		return nil
	}
	fullBytes := bm.Width / 8
	r := bitwise.NewReader(bm.Data)
	data := make([]byte, bm.Height*bm.RowStride)
	w := bitwise.NewWriterMSB(data)
	buf := make([]byte, fullBytes)
	var (
		y    int
		bits uint64
	)
	for y = 0; y < bm.Height; y++ {
		if _, err = r.Read(buf); err != nil {
			return jbig2errors.Wrap(err, processName, "full byte")
		}
		if _, err = w.Write(buf); err != nil {
			return jbig2errors.Wrap(err, processName, "full bytes")
		}
		if bits, err = r.ReadBits(byte(remBits)); err != nil {
			return jbig2errors.Wrap(err, processName, "skipping bits")
		}
		if err = w.WriteByte(byte(bits) << uint(8-remBits)); err != nil {
			return jbig2errors.Wrap(err, processName, "last byte")
		}
	}
	bm.Data = w.Data()
	return nil
}

func erode(dest, src *Bitmap, sel *Selection) (*Bitmap, error) {
	const processName = "erode"
	var (
		err  error
		temp *Bitmap
	)
	dest, err = processMorphArgs1(dest, src, sel, &temp)
	if err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	if err = dest.setAll(); err != nil {
		return nil, jbig2errors.Wrap(err, processName, "")
	}
	var selVal SelectionValue
	for i := 0; i < sel.Height; i++ {
		for j := 0; j < sel.Width; j++ {
			selVal = sel.Data[i][j]
			if selVal == SelHit {
				err = rasterOperation(dest, sel.Cx-j, sel.Cy-i, src.Width, src.Height, PixSrcAndDst, temp, 0, 0)
				if err != nil {
					return nil, jbig2errors.Wrap(err, processName, "")
				}
			}
		}
	}
	if morphBoundaryCondition == symmetricMorphBC {
		return dest, nil
	}
	xp, yp, xn, yn := sel.findMaxTranslations()
	if xp > 0 {
		if err = dest.RasterOperation(0, 0, xp, src.Height, PixClr, nil, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "xp > 0")
		}
	}
	if xn > 0 {
		if err = dest.RasterOperation(src.Width-xn, 0, xn, src.Height, PixClr, nil, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "xn > 0")
		}
	}
	if yp > 0 {
		if err = dest.RasterOperation(0, 0, src.Width, yp, PixClr, nil, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "yp > 0")
		}
	}
	if yn > 0 {
		if err = dest.RasterOperation(0, src.Height-yn, src.Width, yn, PixClr, nil, 0, 0); err != nil {
			return nil, jbig2errors.Wrap(err, processName, "yn > 0")
		}
	}
	return dest, nil
}
func (cp *ClassedPoints) YAtIndex(i int) float32 { return (*cp.Points)[cp.IntSlice[i]].Y }
func (bm *Bitmap) setTwoBytes(idx int, val uint16) error {
	if idx+1 > len(bm.Data)-1 {
		return jbig2errors.Errorf("setTwoBytes", "index: '%d' out of range", idx)
	}
	bm.Data[idx] = byte((val & 0xff00) >> 8)
	bm.Data[idx+1] = byte(val & 0xff)
	return nil
}

func ClipBoxToRectangle(box *image.Rectangle, wi, hi int) (result *image.Rectangle, err error) {
	const processName = "ClipBoxToRectangle"
	if box == nil {
		return nil, jbig2errors.Error(processName, "'box' not defined")
	}
	if box.Min.X >= wi || box.Min.Y >= hi || box.Max.X <= 0 || box.Max.Y <= 0 {
		return nil, jbig2errors.Error(processName, "'box' outside rectangle")
	}
	r := *box
	result = &r
	if result.Min.X < 0 {
		result.Max.X += result.Min.X
		result.Min.X = 0
	}
	if result.Min.Y < 0 {
		result.Max.Y += result.Min.Y
		result.Min.Y = 0
	}
	if result.Max.X > wi {
		result.Max.X = wi
	}
	if result.Max.Y > hi {
		result.Max.Y = hi
	}
	return result, nil
}

func makeExpandTab2x() (tab [256]uint16) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			tab[i] |= 0x3
		}
		if i&0x02 != 0 {
			tab[i] |= 0xc
		}
		if i&0x04 != 0 {
			tab[i] |= 0x30
		}
		if i&0x08 != 0 {
			tab[i] |= 0xc0
		}
		if i&0x10 != 0 {
			tab[i] |= 0x300
		}
		if i&0x20 != 0 {
			tab[i] |= 0xc00
		}
		if i&0x40 != 0 {
			tab[i] |= 0x3000
		}
		if i&0x80 != 0 {
			tab[i] |= 0xc000
		}
	}
	return tab
}

const (
	ComponentConn Component = iota
	ComponentCharacters
	ComponentWords
)

func TstRSymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(4, 5, []byte{0xF0, 0x90, 0xF0, 0xA0, 0x90})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func TstISymbol(t *testing.T, scale ...int) *Bitmap {
	bm, err := NewWithData(1, 5, []byte{0x80, 0x80, 0x80, 0x80, 0x80})
	require.NoError(t, err)
	return TstGetScaledSymbol(t, bm, scale...)
}

func expandBinaryFactor4(dest, src *Bitmap) (err error) {
	const processName = "expandBinaryFactor4"
	srcStride := src.RowStride
	destStride := dest.RowStride
	overhang := src.RowStride*4 - dest.RowStride
	var (
		b, outByte                             byte
		expanded                               uint32
		srcOff, destOff, y, x, k, destIdx, idx int
	)
	for y = 0; y < src.Height; y++ {
		srcOff = y * srcStride
		destOff = 4 * y * destStride
		for x = 0; x < srcStride; x++ {
			b = src.Data[srcOff+x]
			expanded = expandTab4x[b]
			destIdx = destOff + x*4
			if overhang != 0 && (x+1)*4 > dest.RowStride {
				for k = overhang; k > 0; k-- {
					outByte = byte((expanded >> uint(k*8)) & 0xff)
					idx = destIdx + (overhang - k)
					if err = dest.SetByte(idx, outByte); err != nil {
						return jbig2errors.Wrapf(err, processName, "Different rowstrides. K: %d", k)
					}
				}
			} else if err = dest.setFourBytes(destIdx, expanded); err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
			if err = dest.setFourBytes(destOff+x*4, expandTab4x[src.Data[srcOff+x]]); err != nil {
				return jbig2errors.Wrap(err, processName, "")
			}
		}
		for k = 1; k < 4; k++ {
			for x = 0; x < destStride; x++ {
				if err = dest.SetByte(destOff+k*destStride+x, dest.Data[destOff+x]); err != nil {
					return jbig2errors.Wrapf(err, processName, "copy 'quadrable' line: '%d', byte: '%d'", k, x)
				}
			}
		}
	}
	return nil
}

func (bm *Bitmap) GetByte(index int) (byte, error) {
	if index > len(bm.Data)-1 || index < 0 {
		return 0, jbig2errors.Errorf("GetByte", "index: %d out of range", index)
	}
	return bm.Data[index], nil
}

func (bms *Bitmaps) GetBitmap(i int) (*Bitmap, error) {
	const processName = "GetBitmap"
	if bms == nil {
		return nil, jbig2errors.Error(processName, "provided nil Bitmaps")
	}
	if i > len(bms.Values)-1 {
		return nil, jbig2errors.Errorf(processName, "index: '%d' out of range", i)
	}
	return bms.Values[i], nil
}

func (bm *Bitmap) Equals(s *Bitmap) bool {
	if len(bm.Data) != len(s.Data) || bm.Width != s.Width || bm.Height != s.Height {
		return false
	}
	for y := 0; y < bm.Height; y++ {
		off := y * bm.RowStride
		for x := 0; x < bm.RowStride; x++ {
			if bm.Data[off+x] != s.Data[off+x] {
				return false
			}
		}
	}
	return true
}

func (bm *Bitmap) SetPixel(x, y int, pixel byte) error {
	idx := bm.GetByteIndex(x, y)
	if idx > len(bm.Data)-1 {
		return jbig2errors.Errorf("SetPixel", "index out of range: %d", idx)
	}
	bitOff := bm.GetBitOffset(x)
	shift := uint(7 - bitOff)
	b := bm.Data[idx]
	var newByte byte
	if pixel == 1 {
		newByte = b | (pixel & 0x01 << shift)
	} else {
		newByte = b &^ (1 << shift)
	}
	bm.Data[idx] = newByte
	return nil
}
