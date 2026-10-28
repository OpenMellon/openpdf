// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

func rasterOpUniform(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator) {
	if dx < 0 {
		dw += dx
		dx = 0
	}
	overflowX := dx + dw - dest.Width
	if overflowX > 0 {
		dw -= overflowX
	}
	if dy < 0 {
		dh += dy
		dy = 0
	}
	overflowY := dy + dh - dest.Height
	if overflowY > 0 {
		dh -= overflowY
	}
	if dw <= 0 || dh <= 0 {
		return
	}
	if (dx & 7) == 0 {
		rasterOpUniformAligned(dest, dx, dy, dw, dh, op)
	} else {
		rasterOpUniformUnaligned(dest, dx, dy, dw, dh, op)
	}
}

func applyMonochromeThreshold(g color.Gray, model monochromeModel) color.Gray {
	if g.Y > uint8(model) {
		return color.Gray{Y: math.MaxUint8}
	}
	return color.Gray{}
}

func convertToGray16(img image.Image) (Image, error) {
	if existing, ok := img.(*Gray16); ok {
		return existing.Copy(), nil
	}
	bounds := img.Bounds()
	dst, err := NewImage(bounds.Max.X, bounds.Max.Y, 16, 1, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	copyToGray(img, dst, bounds)
	return dst, nil
}

func ColorAtCMYK(x, y, width int, data []byte, decode []float64) (color.CMYK, error) {
	idx := 4 * (y*width + x)
	if idx+3 >= len(data) {
		return color.CMYK{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	C := data[idx] & 0xff
	M := data[idx+1] & 0xff
	Y := data[idx+2] & 0xff
	K := data[idx+3] & 0xff
	if len(decode) == 8 {
		C = uint8(uint32(LinearInterpolate(float64(C), 0, 255, decode[0], decode[1])) & 0xff)
		M = uint8(uint32(LinearInterpolate(float64(M), 0, 255, decode[2], decode[3])) & 0xff)
		Y = uint8(uint32(LinearInterpolate(float64(Y), 0, 255, decode[4], decode[5])) & 0xff)
		K = uint8(uint32(LinearInterpolate(float64(K), 0, 255, decode[6], decode[7])) & 0xff)
	}
	return color.CMYK{C: C, M: M, Y: Y, K: K}, nil
}

func GrayHistogram(g Gray) (hist [256]int) {
	switch v := g.(type) {
	case Histogramer:
		return v.Histogram()
	case image.Image:
		bounds := v.Bounds()
		for x := 0; x < bounds.Max.X; x++ {
			for y := 0; y < bounds.Max.Y; y++ {
				hist[g.GrayAt(x, y).Y]++
			}
		}
		return hist
	default:
		return [256]int{}
	}
}
func (g8 *Gray8) Base() *ImageBase { return &g8.ImageBase }
func copyRGBAToGray(src RGBA, dst Gray, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			g := rgbaToGray(src.RGBAAt(x, y))
			dst.SetGray(x, y, g)
		}
	}
}

func copyGrayToNRGBA(src Gray, dst NRGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			g := src.GrayAt(x, y)
			dst.SetNRGBA(x, y, grayToNRGBA(g))
		}
	}
}

func (b *ImageBase) setEightPartlyBytes(idx, n int, value uint64) (err error) {
	var (
		v     byte
		shift int
	)
	for i := 1; i <= n; i++ {
		shift = 64 - i*8
		v = byte(value >> uint(shift) & 0xff)
		if err = b.setByte(idx+i-1, v); err != nil {
			return err
		}
	}
	padBits := b.BytesPerLine*8 - b.Width
	if padBits == 0 {
		return nil
	}
	shift -= 8
	v = byte(value>>uint(shift)&0xff) << uint(padBits)
	if err = b.setByte(idx+n, v); err != nil {
		return err
	}
	return nil
}

func (g16 *Gray16) SetGray(x, y int, g color.Gray) {
	idx := (y*g16.BytesPerLine/2 + x) * 2
	if idx+1 >= len(g16.Data) {
		return
	}
	g16.Data[idx] = g.Y
	g16.Data[idx+1] = g.Y
}

func nrgbaToGrayZeroWhite(c color.NRGBA) color.Gray {
	var zero color.NRGBA
	if c == zero {
		return color.Gray{Y: 0xff}
	}
	r, g, b, _ := c.RGBA()
	gray := (19595*r + 38470*g + 7471*b + 1<<15) >> 24
	return color.Gray{Y: uint8(gray)}
}

func (n16 *NRGBA16) Set(x, y int, c color.Color) {
	idx := y*n16.BytesPerLine + x*3/2
	if idx+1 >= len(n16.Data) {
		return
	}
	nc := NRGBA16Model.Convert(c).(color.NRGBA)
	n16.setNRGBA(x, y, idx, nc)
}
func (c32 *CMYK32) Copy() Image { return &CMYK32{ImageBase: c32.copy()} }
func NormalizeIndexedDecode(decode []float64, bitsPerComponent int) ([]float64, bool) {
	if len(decode) != 2 || bitsPerComponent <= 0 || bitsPerComponent > 16 {
		return nil, false
	}
	maxVal := float64(uint(1)<<uint(bitsPerComponent) - 1)
	return []float64{decode[0] / maxVal, decode[1] / maxVal}, true
}
func (g16 *Gray16) ColorModel() color.Model { return color.Gray16Model }

var _ image.Image = &Gray2{}

func (n16 *NRGBA16) Validate() error {
	if len(n16.Data) != 3*n16.Width*n16.Height/2 {
		return errors.New("invalid image data size for provided dimensions")
	}
	return nil
}

var _ image.Image = &Monochrome{}

func (g8 *Gray8) Set(x, y int, c color.Color) {
	idx := y*g8.BytesPerLine + x
	if idx > len(g8.Data)-1 {
		return
	}
	g := color.GrayModel.Convert(c)
	g8.Data[idx] = g.(color.Gray).Y
}

type Histogramer interface{ Histogram() [256]int }

func expandFactorSteps(factor int) []uint {
	var steps []uint
	remaining := factor
	eights := remaining / 8
	if eights != 0 {
		for i := 0; i < eights; i++ {
			steps = append(steps, 8)
		}
		rem := remaining % 8
		remaining = 0
		if rem != 0 {
			remaining = rem
		}
	}
	fours := remaining / 4
	if fours != 0 {
		for i := 0; i < fours; i++ {
			steps = append(steps, 4)
		}
		rem := remaining % 4
		remaining = 0
		if rem != 0 {
			remaining = rem
		}
	}
	twos := remaining / 2
	if twos != 0 {
		for i := 0; i < twos; i++ {
			steps = append(steps, 2)
		}
	}
	return steps
}

func (n32 *NRGBA32) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: n32.Width, Y: n32.Height}}
}

func (b *ImageBase) newAlpha() {
	bytesPerLine := BytesPerLine(b.Width, b.BitsPerComponent, 1)
	b.Alpha = make([]byte, b.Height*bytesPerLine)
}

var _ image.Image = &RGBA32{}

func convertToRGBA32(img image.Image) (Image, error) {
	if existing, ok := img.(*RGBA32); ok {
		return existing.Copy(), nil
	}
	bounds, hasAlpha, alpha := boundsAndAlphaBuffer(img, 1)
	dst := &RGBA32{ImageBase: NewImageBase(bounds.Max.X, bounds.Max.Y, 8, 3, nil, alpha, nil)}
	copyToRGBA(img, dst, bounds)
	if len(alpha) != 0 && !hasAlpha {
		if err := dropOpaqueAlpha(alpha, dst); err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func reduceRankBinary(src *Monochrome, level int, tab []byte) (dst *Monochrome, err error) {
	const funcName = "reduceRankBinary"
	if src == nil {
		return nil, errors.New("source bitmap not defined")
	}
	if level < 1 || level > 4 {
		return nil, errors.New("level must be in set {1,2,3,4}")
	}
	if src.Height <= 1 {
		return nil, errors.New("source height must be at least '2'")
	}
	dst = newMonochrome(src.Width/2, src.Height/2)
	if tab == nil {
		tab = makeRankReduceTable()
	}
	width := minInt(src.BytesPerLine, 2*dst.BytesPerLine)
	switch level {
	case 1:
		err = reduceRankBinaryLevel1(src, dst, tab, width)
	case 2:
		err = reduceRankBinaryLevel2(src, dst, tab, width)
	case 3:
		err = reduceRankBinaryLevel3(src, dst, tab, width)
	case 4:
		err = reduceRankBinaryLevel4(src, dst, tab, width)
	}
	if err != nil {
		return nil, err
	}
	return dst, nil
}

var (
	MonochromeConverter = ConverterFunc(convertToMonochrome)
	Gray2Converter      = ConverterFunc(convertToGray2)
	Gray4Converter      = ConverterFunc(convertToGray4)
	GrayConverter       = ConverterFunc(convertToGray8)
	Gray16Converter     = ConverterFunc(convertToGray16)
	NRGBA16Converter    = ConverterFunc(convertToNRGBA16)
	NRGBAConverter      = ConverterFunc(convertToNRGBA32)
	NRGBA64Converter    = ConverterFunc(convertToNRGBA64)
	RGBAConverter       = ConverterFunc(convertToRGBA32)
	CMYKConverter       = ConverterFunc(convertToCMYK32)
)

func quantizeGray2(g color.Gray) color.Gray {
	v := g.Y >> 6
	v |= v << 2
	g.Y = v | v<<4
	return g
}

func ImgToBinary(i image.Image, threshold uint8) *image.Gray {
	switch v := i.(type) {
	case *image.Gray:
		if isBlackAndWhite(v) {
			return v
		}
		return thresholdGray(v, threshold)
	case *image.Gray16:
		return thresholdGray16(v, threshold)
	default:
		return thresholdImage(v, threshold)
	}
}

func (m *Monochrome) ScaleLow(width, height int) (*Monochrome, error) {
	if width < 0 || height < 0 {
		return nil, errors.New("provided invalid width and height")
	}
	dst := newMonochrome(width, height)
	rowMap := make([]int, height)
	colMap := make([]int, width)
	xRatio := float64(m.Width) / float64(width)
	yRatio := float64(m.Height) / float64(height)
	for y := 0; y < height; y++ {
		rowMap[y] = int(math.Min(yRatio*float64(y)+0.5, float64(m.Height-1)))
	}
	for x := 0; x < width; x++ {
		colMap[x] = int(math.Min(xRatio*float64(x)+0.5, float64(m.Width-1)))
	}
	prevCol := -1
	bit := byte(0)
	for y := 0; y < height; y++ {
		srcRow := rowMap[y] * m.BytesPerLine
		dstRow := y * dst.BytesPerLine
		for x := 0; x < width; x++ {
			srcX := colMap[x]
			if srcX != prevCol {
				bit = m.getBit(srcRow, srcX)
				if bit != 0 {
					dst.setBit(dstRow, x)
				}
				prevCol = srcX
			} else {
				if bit != 0 {
					dst.setBit(dstRow, x)
				}
			}
		}
	}
	return dst, nil
}

var _ Image = &Gray8{}

type Gray4 struct{ ImageBase }

var _ Image = &Monochrome{}

func copyToRGBA(src image.Image, dst Image, bounds image.Rectangle) {
	if masker, ok := src.(SMasker); ok && masker.HasAlpha() {
		dst.(SMasker).MakeAlpha()
	}
	switch img := src.(type) {
	case Gray:
		copyGrayToRGBA(img, dst.(RGBA), bounds)
	case NRGBA:
		copyNRGBAToRGBA(img, dst.(RGBA), bounds)
	case *image.NYCbCrA:
		copyNYCbCrAToRGBA(img, dst.(RGBA), bounds)
	case CMYK:
		copyCMYKToRGBA(img, dst.(RGBA), bounds)
	case RGBA:
		copyRGBAToRGBA(img, dst.(RGBA), bounds)
	case nrgba64:
		copyNRGBA64ToRGBA(img, dst.(RGBA), bounds)
	default:
		copyGeneric(src, dst, bounds)
	}
}

func (g4 *Gray4) setGray(x int, y int, g color.Gray) {
	rowStart := y * g4.BytesPerLine
	idx := rowStart + (x >> 1)
	if idx >= len(g4.Data) {
		return
	}
	v := g.Y >> 4
	g4.Data[idx] = (g4.Data[idx] & (^(0xf0 >> uint(4*(x&1))))) | (v << uint(4-4*(x&1)))
}

type NRGBA interface {
	NRGBAAt(x, y int) color.NRGBA
	SetNRGBA(x, y int, c color.NRGBA)
}
type NRGBA32 struct{ ImageBase }

func (g2 *Gray2) ColorAt(x, y int) (color.Color, error) {
	return ColorAtGray2BPC(x, y, g2.BytesPerLine, g2.Data, g2.Decode)
}
func (r32 *RGBA32) ColorModel() color.Model { return color.NRGBAModel }
func (n32 *NRGBA32) Copy() Image            { return &NRGBA32{ImageBase: n32.copy()} }
func IsPowerOf2(n uint) bool                { return n > 0 && (n&(n-1)) == 0 }

var _ image.Image = &NRGBA64{}
