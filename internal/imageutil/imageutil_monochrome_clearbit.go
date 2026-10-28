// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
)

func (m *Monochrome) clearBit(idx, x int) { m.Data[idx] &= ^(0x80 >> uint(x&7)) }
func copyGrayToRGBA(src Gray, dst RGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			g := src.GrayAt(x, y)
			dst.SetRGBA(x, y, grayToRGBA(g))
		}
	}
}

func cmykToNRGBA(c color.CMYK) color.NRGBA {
	r, g, b := color.CMYKToRGB(c.C, c.M, c.Y, c.K)
	return color.NRGBA{R: r, G: g, B: b, A: 0xff}
}

func (r32 *RGBA32) RGBAAt(x, y int) color.RGBA {
	c, _ := ColorAtRGBA32(x, y, r32.Width, r32.Data, r32.Alpha, r32.Decode)
	return c
}

func copyGenericWithAlpha(src image.Image, dst Image, bounds image.Rectangle) {
	if masker, ok := src.(SMasker); ok && masker.HasAlpha() {
		dst.(SMasker).MakeAlpha()
	}
	copyGeneric(src, dst, bounds)
}
func (n64 *NRGBA64) Base() *ImageBase { return &n64.ImageBase }
func copyNRGBAToCMYK(src NRGBA, dst CMYK, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.NRGBAAt(x, y)
			dst.SetCMYK(x, y, nrgbaToCMYK(c))
		}
	}
}

type Gray16 struct{ ImageBase }

func (m *Monochrome) ColorModel() color.Model { return MonochromeModel(m.ModelThreshold) }
func ConverterFunc(converterFunc func(img image.Image) (Image, error)) ColorConverter {
	return colorConverter{convert: converterFunc}
}

func copyCMYKToCMYK(src, dst CMYK, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			dst.SetCMYK(x, y, src.CMYKAt(x, y))
		}
	}
}
func (m *Monochrome) IsUnpadded() bool { return (m.Width * m.Height) == len(m.Data) }
func FromGoImage(i image.Image) (Image, error) {
	switch img := i.(type) {
	case Image:
		return img.Copy(), nil
	case Gray:
		return GrayConverter.Convert(i)
	case *image.Gray16:
		return Gray16Converter.Convert(i)
	case CMYK:
		return CMYKConverter.Convert(i)
	case *image.NRGBA64:
		return NRGBA64Converter.Convert(i)
	default:
		return NRGBAConverter.Convert(i)
	}
}

func NextPowerOf2(n uint) uint {
	if IsPowerOf2(n) {
		return n
	}
	return 1 << (log2Floor(n) + 1)
}

func (m *Monochrome) RasterOperation(dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	return rasterOperation(m, dx, dy, dw, dh, op, src, sx, sy)
}

var _ Gray = &Monochrome{}

func newMonochrome(width, height int) *Monochrome {
	return &Monochrome{ImageBase: NewImageBase(width, height, 1, 1, nil, nil, nil), ModelThreshold: 0x0f}
}

func (conv *monochromeThresholdConverter) Convert(img image.Image) (Image, error) {
	if existing, ok := img.(*Monochrome); ok {
		return existing.Copy(), nil
	}
	bounds := img.Bounds()
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 1, 1, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	dst.(*Monochrome).ModelThreshold = conv.Threshold
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := img.At(x, y)
			dst.Set(x, y, c)
		}
	}
	return dst, nil
}

func makeExpand4Table() (table [256]uint32) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			table[i] |= 0xf
		}
		if i&0x02 != 0 {
			table[i] |= 0xf0
		}
		if i&0x04 != 0 {
			table[i] |= 0xf00
		}
		if i&0x08 != 0 {
			table[i] |= 0xf000
		}
		if i&0x10 != 0 {
			table[i] |= 0xf0000
		}
		if i&0x20 != 0 {
			table[i] |= 0xf00000
		}
		if i&0x40 != 0 {
			table[i] |= 0xf000000
		}
		if i&0x80 != 0 {
			table[i] |= 0xf0000000
		}
	}
	return table
}
func (g2 *Gray2) Base() *ImageBase { return &g2.ImageBase }
func (n16 *NRGBA16) NRGBAAt(x, y int) color.NRGBA {
	c, _ := ColorAtNRGBA16(x, y, n16.Width, n16.BytesPerLine, n16.Data, n16.Alpha, n16.Decode)
	return c
}

func reduceRankBinaryLevel2(src, dst *Monochrome, tab []byte, width int) (err error) {
	var (
		srcRow, dstRow, y, dstY, x, dstX, k, idx int
		top, bottom, both, either                uint32
		b0, b1                                   byte
		word                                     uint16
	)
	rowTop := make([]byte, 4)
	rowBottom := make([]byte, 4)
	for y = 0; y < src.Height-1; y, dstY = y+2, dstY+1 {
		srcRow = y * src.BytesPerLine
		dstRow = dstY * dst.BytesPerLine
		for x, dstX = 0, 0; x < width; x, dstX = x+4, dstX+1 {
			for k = 0; k < 4; k++ {
				idx = srcRow + x + k
				if idx <= len(src.Data)-1 && idx < srcRow+src.BytesPerLine {
					rowTop[k] = src.Data[idx]
				} else {
					rowTop[k] = 0x00
				}
				idx = srcRow + src.BytesPerLine + x + k
				if idx <= len(src.Data)-1 && idx < srcRow+(2*src.BytesPerLine) {
					rowBottom[k] = src.Data[idx]
				} else {
					rowBottom[k] = 0x00
				}
			}
			top = binary.BigEndian.Uint32(rowTop)
			bottom = binary.BigEndian.Uint32(rowBottom)
			both = top & bottom
			both |= both << 1
			either = top | bottom
			either &= either << 1
			bottom = both | either
			bottom &= 0xaaaaaaaa
			top = bottom | (bottom << 7)
			b0 = byte(top >> 24)
			b1 = byte((top >> 8) & 0xff)
			idx = dstRow + dstX
			if idx+1 == len(dst.Data)-1 || idx+1 >= dstRow+dst.BytesPerLine {
				if err = dst.setByte(idx, tab[b0]); err != nil {
					return fmt.Errorf("index: %d", idx)
				}
			} else {
				word = (uint16(tab[b0]) << 8) | uint16(tab[b1])
				if err = dst.setTwoBytes(idx, word); err != nil {
					return fmt.Errorf("setting two bytes failed, index: %d", idx)
				}
				dstX++
			}
		}
	}
	return nil
}

func boundsAndAlphaBuffer(img image.Image, bytesPerPixel int) (image.Rectangle, bool, []byte) {
	bounds := img.Bounds()
	var (
		hasAlpha bool
		alpha    []byte
	)
	switch v := img.(type) {
	case SMasker:
		hasAlpha = v.HasAlpha()
	case NRGBA, RGBA, *image.RGBA64, nrgba64, *image.NYCbCrA:
		alpha = make([]byte, bounds.Max.X*bounds.Max.Y*bytesPerPixel)
	case *image.Paletted:
		if !v.Opaque() {
			alpha = make([]byte, bounds.Max.X*bounds.Max.Y*bytesPerPixel)
		}
	}
	return bounds, hasAlpha, alpha
}

func (n64 *NRGBA64) SetNRGBA64(x, y int, c color.NRGBA64) {
	alphaIdx := (y*n64.Width + x) * 2
	idx := alphaIdx * 3
	if idx+5 >= len(n64.Data) {
		return
	}
	n64.setNRGBA64(idx, c, alphaIdx)
}
func (conv colorConverter) Convert(src image.Image) (Image, error) { return conv.convert(src) }
func (g16 *Gray16) ColorAt(x, y int) (color.Color, error) {
	return ColorAtGray16BPC(x, y, g16.BytesPerLine, g16.Data, g16.Decode)
}

type NRGBA16 struct{ ImageBase }

func (b *ImageBase) setEightBytes(idx int, value uint64) error {
	remaining := b.BytesPerLine - (idx % b.BytesPerLine)
	if b.BytesPerLine != b.Width>>3 {
		remaining--
	}
	if remaining >= 8 {
		return b.setEightFullBytes(idx, value)
	}
	return b.setEightPartlyBytes(idx, remaining, value)
}

var _ Image = &CMYK32{}

func MonochromeModel(threshold uint8) color.Model { return monochromeModel(threshold) }
func (b *ImageBase) setTwoBytes(idx int, value uint16) error {
	if idx+1 > len(b.Data)-1 {
		return errors.New("index out of range")
	}
	b.Data[idx] = byte((value & 0xff00) >> 8)
	b.Data[idx+1] = byte(value & 0xff)
	return nil
}

func (m *Monochrome) getBit(rowStart, x int) uint8 {
	return m.Data[rowStart+(x>>3)] >> uint(7-(x&7)) & 1
}
func (r32 *RGBA32) Base() *ImageBase { return &r32.ImageBase }
func (g16 *Gray16) Set(x, y int, c color.Color) {
	idx := (y*g16.BytesPerLine/2 + x) * 2
	if idx+1 >= len(g16.Data) {
		return
	}
	g := color.Gray16Model.Convert(c).(color.Gray16)
	g16.Data[idx], g16.Data[idx+1] = uint8(g.Y>>8), uint8(g.Y&0xff)
}

var (
	leadingBitMask  = []byte{0x00, 0x80, 0xC0, 0xE0, 0xF0, 0xF8, 0xFC, 0xFE, 0xFF}
	trailingBitMask = []byte{0x00, 0x01, 0x03, 0x07, 0x0F, 0x1F, 0x3F, 0x7F, 0xFF}
)

func ImgToGray(i image.Image) *image.Gray {
	if g, ok := i.(*image.Gray); ok {
		return g
	}
	bounds := i.Bounds()
	dst := image.NewGray(bounds)
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := i.At(x, y)
			dst.Set(x, y, c)
		}
	}
	return dst
}
func (n32 *NRGBA32) ColorModel() color.Model { return color.NRGBAModel }
func (m *Monochrome) ReduceBinary(factor float64) (*Monochrome, error) {
	n := log2Floor(uint(factor))
	if !IsPowerOf2(uint(factor)) {
		n++
	}
	levels := make([]int, n)
	for i := range levels {
		levels[i] = 4
	}
	result, err := reduceRankBinaryCascade(m, levels...)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (g8 *Gray8) SetGray(x, y int, g color.Gray) {
	idx := y*g8.BytesPerLine + x
	if idx > len(g8.Data)-1 {
		return
	}
	g8.Data[idx] = g.Y
}

func (r32 *RGBA32) Set(x, y int, c color.Color) {
	pixIdx := y*r32.Width + x
	idx := 3 * pixIdx
	if idx+2 >= len(r32.Data) {
		return
	}
	nc := color.RGBAModel.Convert(c).(color.RGBA)
	r32.setRGBA(pixIdx, nc)
}

var _ NRGBA = &NRGBA32{}

func convertToCMYK32(img image.Image) (Image, error) {
	if existing, ok := img.(*CMYK32); ok {
		return existing.Copy(), nil
	}
	bounds := img.Bounds()
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 8, 4, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	switch v := img.(type) {
	case CMYK:
		copyCMYKToCMYK(v, dst.(CMYK), bounds)
	case Gray:
		copyGrayToCMYK(v, dst.(CMYK), bounds)
	case NRGBA:
		copyNRGBAToCMYK(v, dst.(CMYK), bounds)
	case RGBA:
		copyRGBAToCMYK(v, dst.(CMYK), bounds)
	default:
		copyGeneric(img, dst, bounds)
	}
	return dst, nil
}

func (n64 *NRGBA64) Set(x, y int, c color.Color) {
	alphaIdx := (y*n64.Width + x) * 2
	idx := alphaIdx * 3
	if idx+5 >= len(n64.Data) {
		return
	}
	nc := color.NRGBA64Model.Convert(c).(color.NRGBA64)
	n64.setNRGBA64(idx, nc, alphaIdx)
}

func (n32 *NRGBA32) setRGBA(pixIdx int, c color.NRGBA) {
	idx := 3 * pixIdx
	n32.Data[idx] = c.R
	n32.Data[idx+1] = c.G
	n32.Data[idx+2] = c.B
	if pixIdx < len(n32.Alpha) {
		n32.Alpha[pixIdx] = c.A
	}
}

func (n16 *NRGBA16) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: n16.Width, Y: n16.Height}}
}

func makeExpand8Table() (table [256]uint64) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			table[i] |= 0xff
		}
		if i&0x02 != 0 {
			table[i] |= 0xff00
		}
		if i&0x04 != 0 {
			table[i] |= 0xff0000
		}
		if i&0x08 != 0 {
			table[i] |= 0xff000000
		}
		if i&0x10 != 0 {
			table[i] |= 0xff00000000
		}
		if i&0x20 != 0 {
			table[i] |= 0xff0000000000
		}
		if i&0x40 != 0 {
			table[i] |= 0xff000000000000
		}
		if i&0x80 != 0 {
			table[i] |= 0xff00000000000000
		}
	}
	return table
}
func (n64 *NRGBA64) Copy() Image { return &NRGBA64{ImageBase: n64.copy()} }
func (g8 *Gray8) ColorAt(x, y int) (color.Color, error) {
	return ColorAtGray8BPC(x, y, g8.BytesPerLine, g8.Data, g8.Decode)
}

var _ Image = &NRGBA32{}

func (g4 *Gray4) Set(x, y int, c color.Color) {
	if x >= g4.Width || y >= g4.Height {
		return
	}
	g := Gray4Model.Convert(c).(color.Gray)
	g4.setGray(x, y, g)
}
func (g2 *Gray2) Copy() Image { return &Gray2{ImageBase: g2.copy()} }
func (r32 *RGBA32) setRGBA(pixIdx int, c color.RGBA) {
	idx := 3 * pixIdx
	r32.Data[idx] = c.R
	r32.Data[idx+1] = c.G
	r32.Data[idx+2] = c.B
	if pixIdx < len(r32.Alpha) {
		r32.Alpha[pixIdx] = c.A
	}
}

func (m monochromeModel) Convert(c color.Color) color.Color {
	g := color.GrayModel.Convert(c).(color.Gray)
	return applyMonochromeThreshold(g, m)
}
