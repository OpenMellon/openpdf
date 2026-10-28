// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/OpenMellon/openpdf/internal/bitwise"
)

func gray4ModelConvert(c color.Color) color.Color {
	g := color.GrayModel.Convert(c).(color.Gray)
	return quantizeGray4(g)
}

func copyGeneric(src image.Image, dst Image, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.At(x, y)
			dst.Set(x, y, c)
		}
	}
}

func (g2 *Gray2) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: g2.Width, Y: g2.Height}}
}
func (n16 *NRGBA16) ColorModel() color.Model { return NRGBA16Model }
func (g2 *Gray2) GrayAt(x, y int) color.Gray {
	g, _ := ColorAtGray2BPC(x, y, g2.BytesPerLine, g2.Data, g2.Decode)
	return g
}

var _ Image = &NRGBA16{}

func ColorAt(x, y, width, bitsPerColor, colorComponents, bytesPerLine int, data, alpha []byte, decode []float64) (color.Color, error) {
	switch colorComponents {
	case 1:
		return ColorAtGrayscale(x, y, bitsPerColor, bytesPerLine, data, decode)
	case 3:
		return ColorAtNRGBA(x, y, width, bytesPerLine, bitsPerColor, data, alpha, decode)
	case 4:
		return ColorAtCMYK(x, y, width, data, decode)
	default:
		return nil, fmt.Errorf("provided invalid color component for the image: %d", colorComponents)
	}
}

func ColorAtGray2BPC(x, y, bytesPerLine int, data []byte, decode []float64) (color.Gray, error) {
	idx := y*bytesPerLine + x>>2
	if idx >= len(data) {
		return color.Gray{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	v := data[idx] >> uint(6-(x&3)*2) & 3
	if len(decode) == 2 {
		return color.Gray{Y: decodeSampleToGray8(float64(v), 3, decode)}, nil
	}
	return color.Gray{Y: v * 85}, nil
}

func cmykToGray(c color.CMYK) color.Gray {
	r, g, b := color.CMYKToRGB(c.C, c.M, c.Y, c.K)
	gray := (19595*uint32(r) + 38470*uint32(g) + 7471*uint32(b) + 1<<7) >> 16
	return color.Gray{Y: uint8(gray)}
}

func log2Floor(n uint) uint {
	var bits uint
	for n != 0 {
		n >>= 1
		bits++
	}
	return bits - 1
}

func ColorAtGray1BPC(x, y, bytesPerLine int, data []byte, decode []float64) (color.Gray, error) {
	idx := y*bytesPerLine + x>>3
	if idx >= len(data) {
		return color.Gray{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	v := data[idx] >> uint(7-(x&7)) & 1
	if len(decode) == 2 {
		return color.Gray{Y: decodeSampleToGray8(float64(v), 1, decode)}, nil
	}
	return color.Gray{Y: v * 255}, nil
}

func copyRGBAToRGBA(src, dst RGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			dst.SetRGBA(x, y, src.RGBAAt(x, y))
		}
	}
}

type RGBA interface {
	RGBAAt(x, y int) color.RGBA
	SetRGBA(x, y int, c color.RGBA)
}

func copyToNRGBA(src image.Image, dst Image, bounds image.Rectangle) {
	if masker, ok := src.(SMasker); ok && masker.HasAlpha() {
		dst.(SMasker).MakeAlpha()
	}
	switch img := src.(type) {
	case Gray:
		copyGrayToNRGBA(img, dst.(NRGBA), bounds)
	case NRGBA:
		copyNRGBAToNRGBA(img, dst.(NRGBA), bounds)
	case *image.NYCbCrA:
		copyNYCbCrAToNRGBA(img, dst.(NRGBA), bounds)
	case CMYK:
		copyCMYKToNRGBA(img, dst.(NRGBA), bounds)
	case RGBA:
		copyRGBAToNRGBA(img, dst.(NRGBA), bounds)
	case nrgba64:
		copyNRGBA64ToNRGBA(img, dst.(NRGBA), bounds)
	default:
		copyGeneric(src, dst, bounds)
	}
}
func (g8 *Gray8) ColorModel() color.Model { return color.GrayModel }
func (m *Monochrome) Histogram() (hist [256]int) {
	for _, b := range m.Data {
		hist[0xff] += int(bitCountTable[m.Data[b]])
	}
	return hist
}

func dropOpaqueAlpha(alpha []byte, img Image) error {
	opaque := true
	for i := 0; i < len(alpha); i++ {
		if alpha[i] != 0xff {
			opaque = false
			break
		}
	}
	if opaque {
		switch n := img.(type) {
		case *NRGBA32:
			n.Alpha = nil
		case *NRGBA64:
			n.Alpha = nil
		default:
			return fmt.Errorf("internal error - image should be of type *NRGBA32 or *NRGBA64 but is: %T", img)
		}
	}
	return nil
}

func (r32 *RGBA32) ColorAt(x, y int) (color.Color, error) {
	return ColorAtRGBA32(x, y, r32.Width, r32.Data, r32.Alpha, r32.Decode)
}

func (n16 *NRGBA16) SetNRGBA(x, y int, c color.NRGBA) {
	idx := y*n16.BytesPerLine + x*3/2
	if idx+1 >= len(n16.Data) {
		return
	}
	c = quantizeNRGBA16(c)
	n16.setNRGBA(x, y, idx, c)
}

type Gray8 struct{ ImageBase }

func ScaleAlphaToMonochrome(data []byte, width, height int) ([]byte, error) {
	bytesPerLine := BytesPerLine(width, 8, 1)
	if len(data) < bytesPerLine*height {
		return nil, nil
	}
	gray := &Gray8{NewImageBase(width, height, 8, 1, data, nil, nil)}
	mono, err := MonochromeConverter.Convert(gray)
	if err != nil {
		return nil, err
	}
	return mono.Base().Data, nil
}

func nrgbaToGray(c color.NRGBA) color.Gray {
	r, g, b, _ := c.RGBA()
	gray := (19595*r + 38470*g + 7471*b + 1<<15) >> 24
	return color.Gray{Y: uint8(gray)}
}

var _ image.Image = &Gray4{}

func (b *ImageBase) MakeAlpha() { b.newAlpha() }
func (g8 *Gray8) Histogram() (hist [256]int) {
	for i := 0; i < len(g8.Data); i++ {
		hist[g8.Data[i]]++
	}
	return hist
}

func newMonochromeWithData(width, height int, data []byte) *Monochrome {
	m := newMonochrome(width, height)
	m.Data = data
	return m
}

var _ Image = &Gray2{}

func copyCMYKToGray(src CMYK, dst Gray, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			g := cmykToGray(src.CMYKAt(x, y))
			dst.SetGray(x, y, g)
		}
	}
}
func grayToNRGBA(g color.Gray) color.NRGBA  { return color.NRGBA{R: g.Y, G: g.Y, B: g.Y, A: 0xff} }
func (m *Monochrome) setGrayBit(idx, x int) { m.Data[idx] |= 0x80 >> uint(x&7) }
func thresholdImage(img image.Image, threshold uint8) *image.Gray {
	bounds := img.Bounds()
	dst := image.NewGray(bounds)
	var (
		c color.Color
		g color.Gray
	)
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c = img.At(x, y)
			dst.Set(x, y, c)
			g = dst.GrayAt(x, y)
			dst.SetGray(x, y, color.Gray{Y: thresholdByte(g.Y, threshold)})
		}
	}
	return dst
}

func ColorAtGray8BPC(x, y, bytesPerLine int, data []byte, decode []float64) (color.Gray, error) {
	idx := y*bytesPerLine + x
	if idx >= len(data) {
		return color.Gray{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	v := data[idx]
	if len(decode) == 2 {
		v = decodeSampleToGray8(float64(v), 255, decode)
	}
	return color.Gray{Y: v}, nil
}

func rgbaToGray(c color.RGBA) color.Gray {
	gray := (19595*uint32(c.R) + 38470*uint32(c.G) + 7471*uint32(c.B) + 1<<7) >> 16
	return color.Gray{Y: uint8(gray)}
}

func nrgba64ToGray(c color.NRGBA64) color.Gray {
	var zero color.NRGBA64
	if c == zero {
		return color.Gray{Y: 0xff}
	}
	r, g, b, _ := c.RGBA()
	gray := (19595*r + 38470*g + 7471*b + 1<<15) >> 24
	return color.Gray{Y: uint8(gray)}
}

func rasterOpUniformAligned(dest *Monochrome, dx, dy int, dw, dh int, op RasterOperator) {
	var (
		dstStart int
		lastMask byte
		y, i     int
		idx      int
	)
	fullBytes := dw >> 3
	trailingBits := dw & 7
	if trailingBits > 0 {
		lastMask = leadingBitMask[trailingBits]
	}
	dstStart = dest.BytesPerLine*dy + (dx >> 3)
	switch op {
	case PixClr:
		for y = 0; y < dh; y++ {
			idx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[idx] = 0x0
				idx++
			}
			if trailingBits > 0 {
				dest.Data[idx] = mergeMasked(dest.Data[idx], 0x0, lastMask)
			}
		}
	case PixSet:
		for y = 0; y < dh; y++ {
			idx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[idx] = 0xff
				idx++
			}
			if trailingBits > 0 {
				dest.Data[idx] = mergeMasked(dest.Data[idx], 0xff, lastMask)
			}
		}
	case PixNotDst:
		for y = 0; y < dh; y++ {
			idx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[idx] = ^dest.Data[idx]
				idx++
			}
			if trailingBits > 0 {
				dest.Data[idx] = mergeMasked(dest.Data[idx], ^dest.Data[idx], lastMask)
			}
		}
	}
}

func convertToNRGBA64(img image.Image) (Image, error) {
	if n, ok := img.(*NRGBA64); ok {
		return n.Copy(), nil
	}
	bounds, hasAlpha, alpha := boundsAndAlphaBuffer(img, 2)
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 16, 3, nil, alpha, nil)
	if err != nil {
		return nil, err
	}
	copyGenericWithAlpha(img, dst, bounds)
	if len(alpha) != 0 && !hasAlpha {
		if err := dropOpaqueAlpha(alpha, dst); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func AddDataPadding(width, height, bitsPerComponent, colorComponents int, data []byte) ([]byte, error) {
	bytesPerLineOut := BytesPerLine(width, bitsPerComponent, colorComponents)
	if bytesPerLineOut == width*colorComponents*bitsPerComponent/8 {
		return data, nil
	}
	bitsPerRow := width * colorComponents * bitsPerComponent
	paddedBits := bytesPerLineOut * 8
	padBits := 8 - (paddedBits - bitsPerRow)
	r := bitwise.NewReader(data)
	rowBytes := bytesPerLineOut - 1
	rowBuf := make([]byte, rowBytes)
	out := make([]byte, height*bytesPerLineOut)
	w := bitwise.NewWriterMSB(out)
	var bits uint64
	var err error
	for y := 0; y < height; y++ {
		_, err = r.Read(rowBuf)
		if err != nil {
			return nil, err
		}
		_, err = w.Write(rowBuf)
		if err != nil {
			return nil, err
		}
		bits, err = r.ReadBits(byte(padBits))
		if err != nil {
			return nil, err
		}
		_, err = w.WriteBits(bits, padBits)
		if err != nil {
			return nil, err
		}
		w.FinishByte()
	}
	return out, nil
}

type monochromeModel uint8

func expandBinary(src *Monochrome, factor int) (*Monochrome, error) {
	if src == nil {
		return nil, errors.New("source not defined")
	}
	if factor == 1 {
		return src.copy(), nil
	}
	if !IsPowerOf2(uint(factor)) {
		return nil, fmt.Errorf("provided invalid expand factor: %d", factor)
	}
	steps := expandFactorSteps(factor)
	return expandBinaryFactors(src, factor, steps)
}

func copyNYCbCrAToRGBA(src *image.NYCbCrA, dst RGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.NYCbCrAAt(x, y)
			dst.SetRGBA(x, y, nycbcraToRGBA(c))
		}
	}
}
func init() { initBitCountTable() }
func (n64 *NRGBA64) setNRGBA64(idx int, c color.NRGBA64, alphaIdx int) {
	n64.Data[idx] = uint8(c.R >> 8)
	n64.Data[idx+1] = uint8(c.R & 0xff)
	n64.Data[idx+2] = uint8(c.G >> 8)
	n64.Data[idx+3] = uint8(c.G & 0xff)
	n64.Data[idx+4] = uint8(c.B >> 8)
	n64.Data[idx+5] = uint8(c.B & 0xff)
	if alphaIdx+1 < len(n64.Alpha) {
		n64.Alpha[alphaIdx] = uint8(c.A >> 8)
		n64.Alpha[alphaIdx+1] = uint8(c.A & 0xff)
	}
}

func nrgba64ToNRGBA(c color.NRGBA64) color.NRGBA {
	return color.NRGBA{R: uint8(c.R >> 8), G: uint8(c.G >> 8), B: uint8(c.B >> 8), A: uint8(c.A >> 8)}
}

func ColorAtNRGBA(x, y, width, bytesPerLine, bitsPerColor int, data, alpha []byte, decode []float64) (color.Color, error) {
	switch bitsPerColor {
	case 4:
		return ColorAtNRGBA16(x, y, width, bytesPerLine, data, alpha, decode)
	case 8:
		return ColorAtNRGBA32(x, y, width, data, alpha, decode)
	case 16:
		return ColorAtNRGBA64(x, y, width, data, alpha, decode)
	default:
		return nil, fmt.Errorf("unsupported rgb bits per color amount: '%d'", bitsPerColor)
	}
}

func (r32 *RGBA32) Validate() error {
	if len(r32.Data) != 3*r32.Width*r32.Height {
		return errors.New("invalid image data size for provided dimensions")
	}
	return nil
}
