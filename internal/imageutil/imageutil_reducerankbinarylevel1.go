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

func reduceRankBinaryLevel1(src, dst *Monochrome, tab []byte, width int) (err error) {
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
			bottom |= top
			bottom |= bottom << 1
			bottom &= 0xaaaaaaaa
			top = bottom | (bottom << 7)
			b0 = byte(top >> 24)
			b1 = byte((top >> 8) & 0xff)
			idx = dstRow + dstX
			if idx+1 == len(dst.Data)-1 || idx+1 >= dstRow+dst.BytesPerLine {
				dst.Data[idx] = tab[b0]
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
func (g4 *Gray4) At(x, y int) color.Color { c, _ := g4.ColorAt(x, y); return c }

var _ Image = &Gray4{}

func (c32 *CMYK32) Set(x, y int, c color.Color) {
	idx := 4 * (y*c32.Width + x)
	if idx+3 >= len(c32.Data) {
		return
	}
	cmyk := color.CMYKModel.Convert(c).(color.CMYK)
	c32.Data[idx] = cmyk.C
	c32.Data[idx+1] = cmyk.M
	c32.Data[idx+2] = cmyk.Y
	c32.Data[idx+3] = cmyk.K
}

func gray2ModelConvert(c color.Color) color.Color {
	g := color.GrayModel.Convert(c).(color.Gray)
	return quantizeGray2(g)
}

func convertToNRGBA32(img image.Image) (Image, error) {
	if existing, ok := img.(*NRGBA32); ok {
		return existing.Copy(), nil
	}
	bounds, hasAlpha, alpha := boundsAndAlphaBuffer(img, 1)
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 8, 3, nil, alpha, nil)
	if err != nil {
		return nil, err
	}
	copyToNRGBA(img, dst, bounds)
	if len(alpha) != 0 && !hasAlpha {
		if err := dropOpaqueAlpha(alpha, dst); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func copyNRGBA64ToNRGBA(src nrgba64, dst NRGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.NRGBA64At(x, y)
			dst.SetNRGBA(x, y, nrgba64ToNRGBA(c))
		}
	}
}

func nrgbaToCMYK(c color.NRGBA) color.CMYK {
	r, g, b, _ := c.RGBA()
	cyan, magenta, yellow, black := color.RGBToCMYK(uint8(r>>8), uint8(g>>8), uint8(b>>8))
	return color.CMYK{C: cyan, M: magenta, Y: yellow, K: black}
}

type CMYK32 struct{ ImageBase }

func DecodeSample(sample, maxVal, dmin, dmax float64) float64 {
	v := LinearInterpolate(sample, 0, maxVal, dmin, dmax)
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
func (n16 *NRGBA16) Copy() Image { return &NRGBA16{ImageBase: n16.copy()} }
func copyNRGBAToGray(src NRGBA, dst Gray, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			g := nrgbaToGray(src.NRGBAAt(x, y))
			dst.SetGray(x, y, g)
		}
	}
}
func (n16 *NRGBA16) Base() *ImageBase      { return &n16.ImageBase }
func mergeMasked(dst, src, mask byte) byte { return (dst &^ mask) | (src & mask) }
func (b *ImageBase) getByte(idx int) (byte, error) {
	if idx > len(b.Data)-1 || idx < 0 {
		return 0, fmt.Errorf("index: %d out of range", idx)
	}
	return b.Data[idx], nil
}

func (m *Monochrome) Validate() error {
	if len(m.Data) != m.Height*m.BytesPerLine {
		return ErrInvalidImage
	}
	return nil
}

func copyRGBAToNRGBA(src RGBA, dst NRGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.RGBAAt(x, y)
			dst.SetNRGBA(x, y, rgbaToNRGBA(c))
		}
	}
}

func (n64 *NRGBA64) At(x, y int) color.Color {
	c, _ := n64.ColorAt(x, y)
	return c
}

func thresholdByte(v, threshold uint8) uint8 {
	if v < threshold {
		return 255
	}
	return 0
}

func copyNRGBAToGrayZeroWhite(dst Gray, src NRGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			g := nrgbaToGrayZeroWhite(src.NRGBAAt(x, y))
			dst.SetGray(x, y, g)
		}
	}
}

func thresholdGray16(img *image.Gray16, threshold uint8) *image.Gray {
	bounds := img.Bounds()
	dst := image.NewGray(bounds)
	for x := 0; x < bounds.Dx(); x++ {
		for y := 0; y < bounds.Dy(); y++ {
			c := img.Gray16At(x, y)
			dst.SetGray(x, y, color.Gray{Y: thresholdByte(uint8(c.Y/256), threshold)})
		}
	}
	return dst
}

func (g16 *Gray16) Histogram() (hist [256]int) {
	for x := 0; x < g16.Width; x++ {
		for y := 0; y < g16.Height; y++ {
			hist[g16.GrayAt(x, y).Y]++
		}
	}
	return hist
}

func copyNRGBAToNRGBA(src, dst NRGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			dst.SetNRGBA(x, y, src.NRGBAAt(x, y))
		}
	}
}

var _ image.Image = &NRGBA32{}

func MonochromeThresholdConverter(threshold uint8) ColorConverter {
	return &monochromeThresholdConverter{Threshold: threshold}
}

type colorConverter struct {
	convert func(img image.Image) (Image, error)
}

var _ Gray = &Gray2{}

func (n32 *NRGBA32) At(x, y int) color.Color {
	c, _ := n32.ColorAt(x, y)
	return c
}

func (b *ImageBase) copy() ImageBase {
	cp := *b
	cp.Data = make([]byte, len(b.Data))
	copy(cp.Data, b.Data)
	return cp
}

func (g8 *Gray8) GrayAt(x, y int) color.Gray {
	g, _ := ColorAtGray8BPC(x, y, g8.BytesPerLine, g8.Data, g8.Decode)
	return g
}
func quantizeGray4(g color.Gray) color.Gray { g.Y >>= 4; g.Y |= g.Y << 4; return g }
func (g4 *Gray4) Base() *ImageBase          { return &g4.ImageBase }
func (n64 *NRGBA64) NRGBA64At(x, y int) color.NRGBA64 {
	c, _ := ColorAtNRGBA64(x, y, n64.Width, n64.Data, n64.Alpha, n64.Decode)
	return c
}

func convertToGray4(img image.Image) (Image, error) {
	if existing, ok := img.(*Gray4); ok {
		return existing.Copy(), nil
	}
	bounds := img.Bounds()
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 4, 1, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	copyToGray(img, dst, bounds)
	return dst, nil
}

func LinearInterpolate(x, xmin, xmax, ymin, ymax float64) float64 {
	if math.Abs(xmax-xmin) < 0.000001 {
		return ymin
	}
	y := ymin + (x-xmin)*(ymax-ymin)/(xmax-xmin)
	return y
}

func GetConverter(bitsPerComponent, colorComponents int) (ColorConverter, error) {
	switch colorComponents {
	case 1:
		switch bitsPerComponent {
		case 1:
			return MonochromeConverter, nil
		case 2:
			return Gray2Converter, nil
		case 4:
			return Gray4Converter, nil
		case 8:
			return GrayConverter, nil
		case 16:
			return Gray16Converter, nil
		}
	case 3:
		switch bitsPerComponent {
		case 4:
			return NRGBA16Converter, nil
		case 8:
			return NRGBAConverter, nil
		case 16:
			return NRGBA64Converter, nil
		}
	case 4:
		return CMYKConverter, nil
	}
	return nil, fmt.Errorf("provided invalid colorConverter parameters. BitsPerComponent: %d, ColorComponents: %d", bitsPerComponent, colorComponents)
}

func (g4 *Gray4) SetGray(x, y int, g color.Gray) {
	if x >= g4.Width || y >= g4.Height {
		return
	}
	g = quantizeGray4(g)
	g4.setGray(x, y, g)
}

func (c32 *CMYK32) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: c32.Width, Y: c32.Height}}
}

var ErrInvalidImage = errors.New("invalid image data size for provided dimensions")

func (m *Monochrome) Set(x, y int, c color.Color) {
	idx := y*m.BytesPerLine + x>>3
	if idx > len(m.Data)-1 {
		return
	}
	g := m.ColorModel().Convert(c).(color.Gray)
	m.setGray(x, g, idx)
}

func (c32 *CMYK32) ColorAt(x, y int) (color.Color, error) {
	return ColorAtCMYK(x, y, c32.Width, c32.Data, c32.Decode)
}

func (c32 *CMYK32) CMYKAt(x, y int) color.CMYK {
	c, _ := ColorAtCMYK(x, y, c32.Width, c32.Data, c32.Decode)
	return c
}

func copyCMYKToRGBA(src CMYK, dst RGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.CMYKAt(x, y)
			dst.SetRGBA(x, y, cmykToRGBA(c))
		}
	}
}

func copyNRGBA64ToRGBA(src nrgba64, dst RGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.NRGBA64At(x, y)
			dst.SetRGBA(x, y, nrgba64ToRGBA(c))
		}
	}
}

func expandBinaryFactors(src *Monochrome, factor int, steps []uint) (*Monochrome, error) {
	width := factor * src.Width
	height := factor * src.Height
	dst := newMonochrome(width, height)
	for i, step := range steps {
		var err error
		switch step {
		case 2:
			err = expandBinaryFactor2(dst, src)
		case 4:
			err = expandBinaryFactor4(dst, src)
		case 8:
			err = expandBinaryFactor8(dst, src)
		}
		if err != nil {
			return nil, err
		}
		if i != len(steps)-1 {
			src = dst.copy()
		}
	}
	return dst, nil
}
func IsGrayImgBlackAndWhite(i *image.Gray) bool { return isBlackAndWhite(i) }
func (g16 *Gray16) At(x, y int) color.Color     { c, _ := g16.ColorAt(x, y); return c }
func nycbcraToRGBA(c color.NYCbCrA) color.RGBA {
	r, g, b, a := nycbcraToNRGBA(c).RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
func (g8 *Gray8) At(x, y int) color.Color { c, _ := g8.ColorAt(x, y); return c }
func (g8 *Gray8) Copy() Image             { return &Gray8{ImageBase: g8.copy()} }
func (c32 *CMYK32) Base() *ImageBase      { return &c32.ImageBase }
func nrgba64ToRGBA(c color.NRGBA64) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func initBitCountTable() {
	for i := 0; i < 256; i++ {
		bitCountTable[i] = uint8(i&0x1) + (uint8(i>>1) & 0x1) + (uint8(i>>2) & 0x1) + (uint8(i>>3) & 0x1) + (uint8(i>>4) & 0x1) + (uint8(i>>5) & 0x1) + (uint8(i>>6) & 0x1) + (uint8(i>>7) & 0x1)
	}
}

type shift int

var (
	expand2Table = makeExpand2Table()
	expand4Table = makeExpand4Table()
	expand8Table = makeExpand8Table()
)

func copyCMYKToNRGBA(src CMYK, dst NRGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.CMYKAt(x, y)
			dst.SetNRGBA(x, y, cmykToNRGBA(c))
		}
	}
}
func (n64 *NRGBA64) ColorModel() color.Model { return color.NRGBA64Model }
func InDelta(expected, current, delta float64) bool {
	diff := expected - current
	if diff <= -delta || diff >= delta {
		return false
	}
	return true
}
