// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
)

func (g2 *Gray2) Histogram() (hist [256]int) {
	for x := 0; x < g2.Width; x++ {
		for y := 0; y < g2.Height; y++ {
			hist[g2.GrayAt(x, y).Y]++
		}
	}
	return hist
}

func makeRankReduceTable() (table []byte) {
	table = make([]byte, 256)
	for i := 0; i < 256; i++ {
		b := byte(i)
		table[b] = (b & 0x01) | ((b & 0x04) >> 1) | ((b & 0x10) >> 2) | ((b & 0x40) >> 3) | ((b & 0x02) << 3) | ((b & 0x08) << 2) | ((b & 0x20) << 1) | (b & 0x80)
	}
	return table
}
func (m *Monochrome) Base() *ImageBase { return &m.ImageBase }
func (g4 *Gray4) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: g4.Width, Y: g4.Height}}
}

type Monochrome struct {
	ImageBase
	ModelThreshold uint8
}

func (g4 *Gray4) Copy() Image { return &Gray4{ImageBase: g4.copy()} }

type Image interface {
	draw.Image
	Base() *ImageBase
	Copy() Image
	Pix() []byte
	ColorAt(x, y int) (color.Color, error)
	Validate() error
}

func copyNYCbCrAToNRGBA(src *image.NYCbCrA, dst NRGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.NYCbCrAAt(x, y)
			dst.SetNRGBA(x, y, nycbcraToNRGBA(c))
		}
	}
}

type nrgba64 interface {
	NRGBA64At(x, y int) color.NRGBA64
	SetNRGBA64(x, y int, c color.NRGBA64)
}

func (g8 *Gray8) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: g8.Width, Y: g8.Height}}
}

func copyNRGBA64ToGray(dst Gray, src nrgba64, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			g := nrgba64ToGray(src.NRGBA64At(x, y))
			dst.SetGray(x, y, g)
		}
	}
}

func rgbaToNRGBA(c color.RGBA) color.NRGBA {
	switch c.A {
	case 0xff:
		return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff}
	case 0x00:
		return color.NRGBA{}
	default:
		r, g, b, a := c.RGBA()
		r = (r * 0xffff) / a
		g = (g * 0xffff) / a
		b = (b * 0xffff) / a
		return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
	}
}

var _ Image = &Gray16{}

func grayToRGBA(g color.Gray) color.RGBA {
	return color.RGBA{R: g.Y, G: g.Y, B: g.Y, A: 0xff}
}
func (b *ImageBase) Pix() []byte { return b.Data }
func (g16 *Gray16) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: g16.Width, Y: g16.Height}}
}

func expandBinaryFactor4(dst, src *Monochrome) (err error) {
	srcBytesPerLine := src.BytesPerLine
	dstBytesPerLine := dst.BytesPerLine
	overhang := src.BytesPerLine*4 - dst.BytesPerLine
	var (
		srcByte, b                             byte
		expanded                               uint32
		srcRow, dstRow, y, col, k, dstIdx, idx int
	)
	for y = 0; y < src.Height; y++ {
		srcRow = y * srcBytesPerLine
		dstRow = 4 * y * dstBytesPerLine
		for col = 0; col < srcBytesPerLine; col++ {
			srcByte = src.Data[srcRow+col]
			expanded = expand4Table[srcByte]
			dstIdx = dstRow + col*4
			if overhang != 0 && (col+1)*4 > dst.BytesPerLine {
				for k = overhang; k > 0; k-- {
					b = byte((expanded >> uint(k*8)) & 0xff)
					idx = dstIdx + (overhang - k)
					if err = dst.setByte(idx, b); err != nil {
						return err
					}
				}
			} else if err = dst.setFourBytes(dstIdx, expanded); err != nil {
				return err
			}
			if err = dst.setFourBytes(dstRow+col*4, expand4Table[src.Data[srcRow+col]]); err != nil {
				return err
			}
		}
		for k = 1; k < 4; k++ {
			for col = 0; col < dstBytesPerLine; col++ {
				if err = dst.setByte(dstRow+k*dstBytesPerLine+col, dst.Data[dstRow+col]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (g2 *Gray2) SetGray(x, y int, gray color.Gray) {
	q := quantizeGray2(gray)
	rowStart := y * g2.BytesPerLine
	idx := rowStart + (x >> 2)
	if idx >= len(g2.Data) {
		return
	}
	v := q.Y >> 6
	g2.Data[idx] = (g2.Data[idx] & (^(0xc0 >> uint(2*(x&3))))) | (v << uint(6-2*(x&3)))
}

func (m *Monochrome) copy() *Monochrome {
	cp := newMonochrome(m.Width, m.Height)
	cp.ModelThreshold = m.ModelThreshold
	cp.Data = make([]byte, len(m.Data))
	copy(cp.Data, m.Data)
	if len(m.Decode) != 0 {
		cp.Decode = make([]float64, len(m.Decode))
		copy(cp.Decode, m.Decode)
	}
	if len(m.Alpha) != 0 {
		cp.Alpha = make([]byte, len(m.Alpha))
		copy(cp.Alpha, m.Alpha)
	}
	return cp
}

func (g8 *Gray8) Validate() error {
	if len(g8.Data) != g8.Height*g8.BytesPerLine {
		return ErrInvalidImage
	}
	return nil
}

func rgbaToCMYK(c color.RGBA) color.CMYK {
	cyan, magenta, yellow, black := color.RGBToCMYK(c.R, c.G, c.B)
	return color.CMYK{C: cyan, M: magenta, Y: yellow, K: black}
}

func ColorAtGray16BPC(x, y, bytesPerLine int, data []byte, decode []float64) (color.Gray16, error) {
	idx := (y*bytesPerLine/2 + x) * 2
	if idx+1 >= len(data) {
		return color.Gray16{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	v := uint16(data[idx])<<8 | uint16(data[idx+1])
	if len(decode) == 2 {
		v = uint16(math.Round(DecodeSample(float64(v), 65535, decode[0], decode[1]) * 65535))
	}
	return color.Gray16{Y: v}, nil
}

func RasterOperation(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	return rasterOperation(dest, dx, dy, dw, dh, op, src, sx, sy)
}

var (
	_ image.Image = &Gray16{}
	_ RGBA        = &RGBA32{}
)

func (n64 *NRGBA64) ColorAt(x, y int) (color.Color, error) {
	return ColorAtNRGBA64(x, y, n64.Width, n64.Data, n64.Alpha, n64.Decode)
}
func (b *ImageBase) GetAlpha() []byte       { return b.Alpha }
func (c32 *CMYK32) At(x, y int) color.Color { c, _ := c32.ColorAt(x, y); return c }
func copyGrayToGray(src, dst Gray, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			dst.SetGray(x, y, src.GrayAt(x, y))
		}
	}
}
func (r32 *RGBA32) At(x, y int) color.Color { c, _ := r32.ColorAt(x, y); return c }
func ColorAtRGBA32(x, y, width int, data, alpha []byte, decode []float64) (color.RGBA, error) {
	pixIdx := y*width + x
	idx := 3 * pixIdx
	if idx+2 >= len(data) {
		return color.RGBA{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	a := uint8(0xff)
	if alpha != nil && len(alpha) > pixIdx {
		a = alpha[pixIdx]
	}
	r, g, b := data[idx], data[idx+1], data[idx+2]
	if len(decode) == 6 {
		r = uint8(uint32(LinearInterpolate(float64(r), 0, 255, decode[0], decode[1])) & 0xff)
		g = uint8(uint32(LinearInterpolate(float64(g), 0, 255, decode[2], decode[3])) & 0xff)
		b = uint8(uint32(LinearInterpolate(float64(b), 0, 255, decode[4], decode[5])) & 0xff)
	}
	return color.RGBA{R: r, G: g, B: b, A: a}, nil
}

func (m *Monochrome) Copy() Image {
	return &Monochrome{ImageBase: m.ImageBase.copy(), ModelThreshold: m.ModelThreshold}
}

type Gray2 struct{ ImageBase }

func (n32 *NRGBA32) Validate() error {
	if len(n32.Data) != 3*n32.Width*n32.Height {
		return errors.New("invalid image data size for provided dimensions")
	}
	return nil
}

func isBlackOrWhite(v uint8) bool {
	if v == 0 || v == 255 {
		return true
	}
	return false
}

func quantizeNRGBA16(c color.NRGBA) color.NRGBA {
	c.R = c.R>>4 | (c.R>>4)<<4
	c.G = c.G>>4 | (c.G>>4)<<4
	c.B = c.B>>4 | (c.B>>4)<<4
	return c
}

func ColorAtGray4BPC(x, y, bytesPerLine int, data []byte, decode []float64) (color.Gray, error) {
	idx := y*bytesPerLine + x>>1
	if idx >= len(data) {
		return color.Gray{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	v := data[idx] >> uint(4-(x&1)*4) & 0xf
	if len(decode) == 2 {
		return color.Gray{Y: decodeSampleToGray8(float64(v), 15, decode)}, nil
	}
	return color.Gray{Y: v * 17 & 0xff}, nil
}

func copyToGray(src image.Image, dst Image, bounds image.Rectangle) {
	switch img := src.(type) {
	case Gray:
		copyGrayToGray(img, dst.(Gray), bounds)
	case NRGBA:
		copyNRGBAToGray(img, dst.(Gray), bounds)
	case CMYK:
		copyCMYKToGray(img, dst.(Gray), bounds)
	case RGBA:
		copyRGBAToGray(img, dst.(Gray), bounds)
	default:
		copyGeneric(src, dst, bounds)
	}
}

func rasterOpUniformUnaligned(dest *Monochrome, dx, dy int, dw, dh int, op RasterOperator) {
	var (
		singleByte   bool
		hasMiddle    bool
		middleBytes  int
		dstMiddle    int
		trailingBits int
		dstTrailing  int
		hasTrailing  bool
		trailingMask byte
	)
	leadingBits := 8 - (dx & 7)
	leadingMask := trailingBitMask[leadingBits]
	dstIdx := dest.BytesPerLine*dy + (dx >> 3)
	if dw < leadingBits {
		singleByte = true
		leadingMask &= leadingBitMask[8-leadingBits+dw]
	}
	if !singleByte {
		middleBytes = (dw - leadingBits) >> 3
		if middleBytes != 0 {
			hasMiddle = true
			dstMiddle = dstIdx + 1
		}
	}
	trailingBits = (dx + dw) & 7
	if !(singleByte || trailingBits == 0) {
		hasTrailing = true
		trailingMask = leadingBitMask[trailingBits]
		dstTrailing = dstIdx + 1 + middleBytes
	}
	var y, i int
	switch op {
	case PixClr:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], 0x0, leadingMask)
			dstIdx += dest.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = 0x0
				}
				dstMiddle += dest.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], 0x0, trailingMask)
				dstTrailing += dest.BytesPerLine
			}
		}
	case PixSet:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], 0xff, leadingMask)
			dstIdx += dest.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = 0xff
				}
				dstMiddle += dest.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], 0xff, trailingMask)
				dstTrailing += dest.BytesPerLine
			}
		}
	case PixNotDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^dest.Data[dstMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
			}
		}
	}
}

var _ image.Image = &NRGBA16{}

type (
	NRGBA64 struct{ ImageBase }
	RGBA32  struct{ ImageBase }
)

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func coordinatesOutOfRange(x int, y int) error {
	return fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
}
