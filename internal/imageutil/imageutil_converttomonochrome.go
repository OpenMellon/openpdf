// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

func convertToMonochrome(img image.Image) (Image, error) {
	if existing, ok := img.(*Monochrome); ok {
		return existing, nil
	}
	bounds := img.Bounds()
	var g Gray
	switch v := img.(type) {
	case Gray:
		g = v
	case NRGBA:
		g = &Gray8{ImageBase: NewImageBase(bounds.Max.X, bounds.Max.Y, 8, 1, nil, nil, nil)}
		copyNRGBAToGrayZeroWhite(g, v, bounds)
	case nrgba64:
		g = &Gray8{ImageBase: NewImageBase(bounds.Max.X, bounds.Max.Y, 8, 1, nil, nil, nil)}
		copyNRGBA64ToGray(g, v, bounds)
	default:
		conv, err := GrayConverter.Convert(img)
		if err != nil {
			return nil, err
		}
		g = conv.(Gray)
	}
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 1, 1, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	mono := dst.(*Monochrome)
	threshold := AutoThresholdTriangle(GrayHistogram(g))
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			bw := applyMonochromeThreshold(g.GrayAt(x, y), monochromeModel(threshold))
			mono.SetGray(x, y, bw)
		}
	}
	return dst, nil
}

func ColorAtNRGBA32(x, y, width int, data, alpha []byte, decode []float64) (color.NRGBA, error) {
	pixIdx := y*width + x
	idx := 3 * pixIdx
	if idx+2 >= len(data) {
		return color.NRGBA{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	a := uint8(0xff)
	if alpha != nil && len(alpha) > pixIdx {
		a = alpha[pixIdx]
	}
	r, g, b := data[idx], data[idx+1], data[idx+2]
	if len(decode) == 6 {
		rf := LinearInterpolate(float64(r), 0, 255.0, decode[0], decode[1])
		gf := LinearInterpolate(float64(g), 0, 255.0, decode[2], decode[3])
		bf := LinearInterpolate(float64(b), 0, 255.0, decode[4], decode[5])
		if rf <= 1.0 && gf <= 1.0 && bf <= 1.0 {
			rf *= 255.0
			gf *= 255.0
			bf *= 255.0
		}
		r = uint8(rf) & 0xff
		g = uint8(gf) & 0xff
		b = uint8(bf) & 0xff
	}
	return color.NRGBA{R: r, G: g, B: b, A: a}, nil
}

func (m *Monochrome) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: m.Width, Y: m.Height}}
}

func (n64 *NRGBA64) Validate() error {
	if len(n64.Data) != 3*2*n64.Width*n64.Height {
		return errors.New("invalid image data size for provided dimensions")
	}
	return nil
}

type Gray interface {
	GrayAt(x, y int) color.Gray
	SetGray(x, y int, g color.Gray)
}

func reduceRankBinaryLevel4(src, dst *Monochrome, tab []byte, width int) (err error) {
	var (
		srcRow, dstRow, y, dstY, x, dstX, k, idx int
		top, bottom                              uint32
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
			bottom &= top
			bottom &= bottom << 1
			bottom &= 0xaaaaaaaa
			top = bottom | (bottom << 7)
			b0 = byte(top >> 24)
			b1 = byte((top >> 8) & 0xff)
			idx = dstRow + dstX
			if idx+1 == len(dst.Data)-1 || idx+1 >= dstRow+dst.BytesPerLine {
				dst.Data[idx] = tab[b0]
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
func (n16 *NRGBA16) At(x, y int) color.Color { c, _ := n16.ColorAt(x, y); return c }
func (m *Monochrome) getBitAt(x, y int) bool {
	idx := y*m.BytesPerLine + (x >> 3)
	bit := x & 0x07
	shift := uint(7 - bit)
	if idx > len(m.Data)-1 {
		return false
	}
	if (m.Data[idx]>>shift)&0x01 >= 1 {
		return true
	}
	return false
}

func (n32 *NRGBA32) ColorAt(x, y int) (color.Color, error) {
	return ColorAtNRGBA32(x, y, n32.Width, n32.Data, n32.Alpha, n32.Decode)
}

func (m *Monochrome) InverseData() error {
	return m.RasterOperation(0, 0, m.Width, m.Height, PixNotDst, nil, 0, 0)
}

func nrgbaToRGBA(c color.NRGBA) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func isBlackAndWhite(img *image.Gray) bool {
	for i := 0; i < len(img.Pix); i++ {
		if !isBlackOrWhite(img.Pix[i]) {
			return false
		}
	}
	return true
}

type ImageBase struct {
	Width, Height                     int
	BitsPerComponent, ColorComponents int
	Data, Alpha                       []byte
	Decode                            []float64
	BytesPerLine                      int
}

func copyGrayToCMYK(src Gray, dst CMYK, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			g := src.GrayAt(x, y)
			dst.SetCMYK(x, y, grayToCMYK(g))
		}
	}
}

func (b *ImageBase) setFourBytes(idx int, value uint32) error {
	if idx+3 > len(b.Data)-1 {
		return fmt.Errorf("index: '%d' out of range", idx)
	}
	b.Data[idx] = byte((value & 0xff000000) >> 24)
	b.Data[idx+1] = byte((value & 0xff0000) >> 16)
	b.Data[idx+2] = byte((value & 0xff00) >> 8)
	b.Data[idx+3] = byte(value & 0xff)
	return nil
}

var _ Image = &RGBA32{}

func copyRGBAToCMYK(src RGBA, dst CMYK, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.RGBAAt(x, y)
			dst.SetCMYK(x, y, rgbaToCMYK(c))
		}
	}
}
func (g16 *Gray16) Copy() Image { return &Gray16{ImageBase: g16.copy()} }
func (m *Monochrome) Scale(scale float64) (*Monochrome, error) {
	var reduce bool
	factor := scale
	if scale < 1 {
		factor = 1 / scale
		reduce = true
	}
	pow2 := NextPowerOf2(uint(factor))
	if InDelta(float64(pow2), factor, 0.001) {
		if reduce {
			return m.ReduceBinary(factor)
		}
		return m.ExpandBinary(int(pow2))
	}
	width := int(math.RoundToEven(float64(m.Width) * scale))
	height := int(math.RoundToEven(float64(m.Height) * scale))
	return m.ScaleLow(width, height)
}

func decodeSampleToGray8(sample, maxVal float64, decode []float64) uint8 {
	return uint8(math.Round(DecodeSample(sample, maxVal, decode[0], decode[1]) * 255))
}

func convertToNRGBA16(img image.Image) (Image, error) {
	if existing, ok := img.(*NRGBA16); ok {
		return existing.Copy(), nil
	}
	bounds := img.Bounds()
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 4, 3, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	copyToNRGBA(img, dst, bounds)
	return dst, nil
}

var _ Gray = &Gray16{}

func (n32 *NRGBA32) SetNRGBA(x, y int, c color.NRGBA) {
	pixIdx := y*n32.Width + x
	idx := 3 * pixIdx
	if idx+2 >= len(n32.Data) {
		return
	}
	n32.setRGBA(pixIdx, c)
}

var _ Gray = &Gray8{}

func (g2 *Gray2) At(x, y int) color.Color { c, _ := g2.ColorAt(x, y); return c }
func convertToGray8(img image.Image) (Image, error) {
	if existing, ok := img.(*Gray8); ok {
		return existing.Copy(), nil
	}
	bounds := img.Bounds()
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 8, 1, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	copyToGray(img, dst, bounds)
	return dst, nil
}

type monochromeThresholdConverter struct {
	Threshold uint8
}

func thresholdGray(img *image.Gray, threshold uint8) *image.Gray {
	bounds := img.Bounds()
	dst := image.NewGray(bounds)
	for x := 0; x < bounds.Dx(); x++ {
		for y := 0; y < bounds.Dy(); y++ {
			g := img.GrayAt(x, y)
			dst.SetGray(x, y, color.Gray{Y: thresholdByte(g.Y, threshold)})
		}
	}
	return dst
}

func (n64 *NRGBA64) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: n64.Width, Y: n64.Height}}
}
func (r32 *RGBA32) Copy() Image { return &RGBA32{ImageBase: r32.copy()} }

var _ NRGBA = &NRGBA16{}

func convertToGray2(img image.Image) (Image, error) {
	if existing, ok := img.(*Gray2); ok {
		return existing.Copy(), nil
	}
	bounds := img.Bounds()
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 2, 1, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	copyToGray(img, dst, bounds)
	return dst, nil
}

const (
	shiftLeft shift = iota
	shiftRight
)

func grayToCMYK(g color.Gray) color.CMYK    { return color.CMYK{K: 0xff - g.Y} }
func (c32 *CMYK32) ColorModel() color.Model { return color.CMYKModel }

type ColorConverter interface {
	Convert(img image.Image) (Image, error)
}

func (n16 *NRGBA16) setNRGBA(x, y, idx int, c color.NRGBA) {
	if x*3%2 == 0 {
		n16.Data[idx] = (c.R>>4)<<4 | (c.G >> 4)
		n16.Data[idx+1] = (c.B>>4)<<4 | (n16.Data[idx+1] & 0xf)
	} else {
		n16.Data[idx] = (n16.Data[idx] & 0xf0) | (c.R >> 4)
		n16.Data[idx+1] = (c.G>>4)<<4 | (c.B >> 4)
	}
	if n16.Alpha != nil {
		alphaIdx := y * BytesPerLine(n16.Width, 4, 1)
		if alphaIdx < len(n16.Alpha) {
			if x%2 == 0 {
				n16.Alpha[alphaIdx] = (c.A>>uint(4))<<uint(4) | (n16.Alpha[idx] & 0xf)
			} else {
				n16.Alpha[alphaIdx] = (n16.Alpha[alphaIdx] & 0xf0) | (c.A >> uint(4))
			}
		}
	}
}
func (g2 *Gray2) ColorModel() color.Model { return Gray2Model }
func (g16 *Gray16) Validate() error {
	if len(g16.Data) != g16.Height*g16.BytesPerLine {
		return ErrInvalidImage
	}
	return nil
}

func (n32 *NRGBA32) Set(x, y int, c color.Color) {
	pixIdx := y*n32.Width + x
	idx := 3 * pixIdx
	if idx+2 >= len(n32.Data) {
		return
	}
	nc := color.NRGBAModel.Convert(c).(color.NRGBA)
	n32.setRGBA(pixIdx, nc)
}

var _ Image = &NRGBA64{}

func (m *Monochrome) setBit(rowStart, x int) {
	m.Data[rowStart+(x>>3)] |= 0x80 >> uint(x&7)
}
func (g4 *Gray4) ColorModel() color.Model { return Gray4Model }
