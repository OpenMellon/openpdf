// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"image"
	"image/color"
	"image/draw"
	"math"
)

func (m *Monochrome) ResolveDecode() error {
	if len(m.Decode) != 2 {
		return nil
	}
	if m.Decode[0] == 1 && m.Decode[1] == 0 {
		if err := m.InverseData(); err != nil {
			return err
		}
		m.Decode = nil
	}
	return nil
}
func (m *Monochrome) setIndexedBit(bitIdx int) { m.Data[(bitIdx >> 3)] |= 0x80 >> uint(bitIdx&7) }
func BytesPerLine(width, bitsPerComponent, colorComponents int) int {
	return ((width*bitsPerComponent)*colorComponents + 7) >> 3
}
func NewImageBase(width int, height int, bitsPerComponent int, colorComponents int, data []byte, alpha []byte, decode []float64) ImageBase {
	base := ImageBase{Width: width, Height: height, BitsPerComponent: bitsPerComponent, ColorComponents: colorComponents, Data: data, Alpha: alpha, Decode: decode, BytesPerLine: BytesPerLine(width, bitsPerComponent, colorComponents)}
	if data == nil {
		base.Data = make([]byte, height*base.BytesPerLine)
	}
	return base
}
func AutoThresholdTriangle(histogram [256]int) uint8 {
	var minBin, peakCount, peakBin, maxBin int
	for i := 0; i < len(histogram); i++ {
		if histogram[i] > 0 {
			minBin = i
			break
		}
	}
	if minBin > 0 {
		minBin--
	}
	for i := 255; i > 0; i-- {
		if histogram[i] > 0 {
			maxBin = i
			break
		}
	}
	if maxBin < 255 {
		maxBin++
	}
	for i := 0; i < 256; i++ {
		if histogram[i] > peakCount {
			peakBin = i
			peakCount = histogram[i]
		}
	}
	var inverted bool
	if (peakBin - minBin) < (maxBin - peakBin) {
		inverted = true
		var left int
		right := 255
		for left < right {
			tmp := histogram[left]
			histogram[left] = histogram[right]
			histogram[right] = tmp
			left++
			right--
		}
		minBin = 255 - maxBin
		peakBin = 255 - peakBin
	}
	if minBin == peakBin {
		return uint8(minBin)
	}
	nx := float64(histogram[peakBin])
	ny := float64(minBin - peakBin)
	d := math.Sqrt(nx*nx + ny*ny)
	nx /= d
	ny /= d
	d = nx*float64(minBin) + ny*float64(histogram[minBin])
	split := minBin
	var splitDistance float64
	for i := minBin + 1; i <= peakBin; i++ {
		dist := nx*float64(i) + ny*float64(histogram[i]) - d
		if dist > splitDistance {
			split = i
			splitDistance = dist
		}
	}
	split--
	if inverted {
		var left int
		right := 255
		for left < right {
			tmp := histogram[left]
			histogram[left] = histogram[right]
			histogram[right] = tmp
			left++
			right--
		}
		return uint8(255 - split)
	}
	return uint8(split)
}
func (n16 *NRGBA16) ColorAt(x, y int) (color.Color, error) {
	return ColorAtNRGBA16(x, y, n16.Width, n16.BytesPerLine, n16.Data, n16.Alpha, n16.Decode)
}
func (g4 *Gray4) ColorAt(x, y int) (color.Color, error) {
	return ColorAtGray4BPC(x, y, g4.BytesPerLine, g4.Data, g4.Decode)
}
func expandBinaryFactor8(dst, src *Monochrome) (err error) {
	srcBytesPerLine := src.BytesPerLine
	dstBytesPerLine := dst.BytesPerLine
	var srcRow, dstRow, y, col, k int
	for y = 0; y < src.Height; y++ {
		srcRow = y * srcBytesPerLine
		dstRow = 8 * y * dstBytesPerLine
		for col = 0; col < srcBytesPerLine; col++ {
			if err = dst.setEightBytes(dstRow+col*8, expand8Table[src.Data[srcRow+col]]); err != nil {
				return err
			}
		}
		for k = 1; k < 8; k++ {
			for col = 0; col < dstBytesPerLine; col++ {
				if err = dst.setByte(dstRow+k*dstBytesPerLine+col, dst.Data[dstRow+col]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (b *ImageBase) setEightFullBytes(idx int, value uint64) error {
	if idx+7 > len(b.Data)-1 {
		return errors.New("index out of range")
	}
	b.Data[idx] = byte((value & 0xff00000000000000) >> 56)
	b.Data[idx+1] = byte((value & 0xff000000000000) >> 48)
	b.Data[idx+2] = byte((value & 0xff0000000000) >> 40)
	b.Data[idx+3] = byte((value & 0xff00000000) >> 32)
	b.Data[idx+4] = byte((value & 0xff000000) >> 24)
	b.Data[idx+5] = byte((value & 0xff0000) >> 16)
	b.Data[idx+6] = byte((value & 0xff00) >> 8)
	b.Data[idx+7] = byte(value & 0xff)
	return nil
}
func (m *Monochrome) SetGray(x, y int, g color.Gray) {
	idx := y*m.BytesPerLine + x>>3
	if idx > len(m.Data)-1 {
		return
	}
	g = applyMonochromeThreshold(g, monochromeModel(m.ModelThreshold))
	m.setGray(x, g, idx)
}
func makeExpand2Table() (table [256]uint16) {
	for i := 0; i < 256; i++ {
		if i&0x01 != 0 {
			table[i] |= 0x3
		}
		if i&0x02 != 0 {
			table[i] |= 0xc
		}
		if i&0x04 != 0 {
			table[i] |= 0x30
		}
		if i&0x08 != 0 {
			table[i] |= 0xc0
		}
		if i&0x10 != 0 {
			table[i] |= 0x300
		}
		if i&0x20 != 0 {
			table[i] |= 0xc00
		}
		if i&0x40 != 0 {
			table[i] |= 0x3000
		}
		if i&0x80 != 0 {
			table[i] |= 0xc000
		}
	}
	return table
}
func copyNRGBAToRGBA(src NRGBA, dst RGBA, bounds image.Rectangle) {
	for x := 0; x < bounds.Max.X; x++ {
		for y := 0; y < bounds.Max.Y; y++ {
			c := src.NRGBAAt(x, y)
			dst.SetRGBA(x, y, nrgbaToRGBA(c))
		}
	}
}
func (m *Monochrome) setGray(x int, g color.Gray, idx int) {
	if g.Y == 0 {
		m.clearBit(idx, x)
	} else {
		m.setGrayBit(idx, x)
	}
}
func (c32 *CMYK32) Validate() error {
	if len(c32.Data) != 4*c32.Width*c32.Height {
		return errors.New("invalid image data size for provided dimensions")
	}
	return nil
}
func (g2 *Gray2) Validate() error {
	if len(g2.Data) != g2.Height*g2.BytesPerLine {
		return ErrInvalidImage
	}
	return nil
}
func reduceRankBinaryLevel3(src, dst *Monochrome, tab []byte, width int) (err error) {
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
			bottom = both & either
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
func (m *Monochrome) ColorAt(x, y int) (color.Color, error) {
	return ColorAtGray1BPC(x, y, m.BytesPerLine, m.Data, m.Decode)
}

var bitCountTable [256]uint8

func nrgba16ModelConvert(c color.Color) color.Color {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return quantizeNRGBA16(n)
}
func (g16 *Gray16) GrayAt(x, y int) color.Gray {
	c, _ := g16.ColorAt(x, y)
	return color.Gray{Y: uint8(c.(color.Gray16).Y >> 8)}
}
func ColorAtGrayscale(x, y, bitsPerColor, bytesPerLine int, data []byte, decode []float64) (color.Color, error) {
	switch bitsPerColor {
	case 1:
		return ColorAtGray1BPC(x, y, bytesPerLine, data, decode)
	case 2:
		return ColorAtGray2BPC(x, y, bytesPerLine, data, decode)
	case 4:
		return ColorAtGray4BPC(x, y, bytesPerLine, data, decode)
	case 8:
		return ColorAtGray8BPC(x, y, bytesPerLine, data, decode)
	case 16:
		return ColorAtGray16BPC(x, y, bytesPerLine, data, decode)
	default:
		return nil, fmt.Errorf("unsupported gray scale bits per color amount: '%d'", bitsPerColor)
	}
}
func ColorAtNRGBA16(x, y, width, bytesPerLine int, data, alpha []byte, decode []float64) (color.NRGBA, error) {
	idx := y*bytesPerLine + x*3/2
	if idx+1 >= len(data) {
		return color.NRGBA{}, coordinatesOutOfRange(x, y)
	}
	const (
		nibbleMask = 0xf
		opaque     = uint8(0xff)
	)
	a := opaque
	if alpha != nil {
		alphaIdx := y * BytesPerLine(width, 4, 1)
		if alphaIdx < len(alpha) {
			if x%2 == 0 {
				a = (alpha[alphaIdx] >> uint(4)) & nibbleMask
			} else {
				a = alpha[alphaIdx] & nibbleMask
			}
			a |= a << 4
		}
	}
	var r, g, b uint8
	if x*3%2 == 0 {
		r = (data[idx] >> uint(4)) & nibbleMask
		g = data[idx] & nibbleMask
		b = (data[idx+1] >> uint(4)) & nibbleMask
	} else {
		r = data[idx] & nibbleMask
		g = (data[idx+1] >> uint(4)) & nibbleMask
		b = data[idx+1] & nibbleMask
	}
	if len(decode) == 6 {
		r = uint8(uint32(LinearInterpolate(float64(r), 0, 15, decode[0], decode[1])) & 0xf)
		g = uint8(uint32(LinearInterpolate(float64(g), 0, 15, decode[2], decode[3])) & 0xf)
		b = uint8(uint32(LinearInterpolate(float64(b), 0, 15, decode[4], decode[5])) & 0xf)
	}
	return color.NRGBA{R: (r << 4) | (r & 0xf), G: (g << 4) | (g & 0xf), B: (b << 4) | (b & 0xf), A: a}, nil
}
func (g16 *Gray16) Base() *ImageBase { return &g16.ImageBase }
func (b *ImageBase) HasAlpha() bool {
	if b.Alpha == nil {
		return false
	}
	for i := range b.Alpha {
		if b.Alpha[i] != 0xff {
			return true
		}
	}
	return false
}

var _ Gray = &Gray4{}

func expandBinaryXY(src *Monochrome, xFactor, yFactor int) (*Monochrome, error) {
	if src == nil {
		return nil, errors.New("source not defined")
	}
	if xFactor <= 0 || yFactor <= 0 {
		return nil, errors.New("invalid scale factor: <= 0")
	}
	if xFactor == yFactor {
		if xFactor == 1 {
			return src.copy(), nil
		}
		if xFactor == 2 || xFactor == 4 || xFactor == 8 {
			expanded, err := expandBinary(src, xFactor)
			if err != nil {
				return nil, err
			}
			return expanded, nil
		}
	}
	newWidth := xFactor * src.Width
	newHeight := yFactor * src.Height
	dst := newMonochrome(newWidth, newHeight)
	dstBytesPerLine := dst.BytesPerLine
	var (
		dstRow, y, x, k, xOffset int
		b                        byte
		err                      error
	)
	for y = 0; y < src.Height; y++ {
		dstRow = yFactor * y * dstBytesPerLine
		for x = 0; x < src.Width; x++ {
			if set := src.getBitAt(x, y); set {
				xOffset = xFactor * x
				for k = 0; k < xFactor; k++ {
					dst.setIndexedBit(dstRow*8 + xOffset + k)
				}
			}
		}
		for k = 1; k < yFactor; k++ {
			rowStart := dstRow + k*dstBytesPerLine
			for i := 0; i < dstBytesPerLine; i++ {
				if b, err = dst.getByte(dstRow + i); err != nil {
					return nil, err
				}
				if err = dst.setByte(rowStart+i, b); err != nil {
					return nil, err
				}
			}
		}
	}
	return dst, nil
}
func (r32 *RGBA32) SetRGBA(x, y int, c color.RGBA) {
	pixIdx := y*r32.Width + x
	idx := 3 * pixIdx
	if idx+2 >= len(r32.Data) {
		return
	}
	r32.setRGBA(pixIdx, c)
}
func (g4 *Gray4) Validate() error {
	if len(g4.Data) != g4.Height*g4.BytesPerLine {
		return ErrInvalidImage
	}
	return nil
}
func (m *Monochrome) At(x, y int) color.Color { c, _ := m.ColorAt(x, y); return c }
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
	g2.Data[idx] = (g2.Data[idx] & (^(0xc0 >> uint(2*((x)&3))))) | (v << uint(6-2*(x&3)))
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

var _ image.Image = &Gray16{}
var _ RGBA = &RGBA32{}

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
					dest.Data[dstMiddle+i] = ^(dest.Data[dstMiddle+i])
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

type NRGBA64 struct{ ImageBase }
type RGBA32 struct{ ImageBase }

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
func coordinatesOutOfRange(x int, y int) error {
	return fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
}
func (m *Monochrome) AddPadding() (err error) {
	if minLen := ((m.Width * m.Height) + 7) >> 3; len(m.Data) < minLen {
		return fmt.Errorf("invalid data length: '%d'. The data should contain at least: '%d' bytes", len(m.Data), minLen)
	}
	padBits := m.Width % 8
	if padBits == 0 {
		return nil
	}
	rowBytes := m.Width / 8
	r := bitwise.NewReader(m.Data)
	out := make([]byte, m.Height*m.BytesPerLine)
	w := bitwise.NewWriterMSB(out)
	rowBuf := make([]byte, rowBytes)
	var (
		y    int
		bits uint64
	)
	for y = 0; y < m.Height; y++ {
		if _, err = r.Read(rowBuf); err != nil {
			return err
		}
		if _, err = w.Write(rowBuf); err != nil {
			return err
		}
		if bits, err = r.ReadBits(byte(padBits)); err != nil {
			return err
		}
		if err = w.WriteByte(byte(bits) << uint(8-padBits)); err != nil {
			return err
		}
	}
	m.Data = w.Data()
	return nil
}
func (r32 *RGBA32) Bounds() image.Rectangle {
	return image.Rectangle{Max: image.Point{X: r32.Width, Y: r32.Height}}
}
func (m *Monochrome) GrayAt(x, y int) color.Gray {
	g, _ := ColorAtGray1BPC(x, y, m.BytesPerLine, m.Data, m.Decode)
	return g
}
func rasterOperation(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	if dest == nil {
		return errors.New("nil 'dest' Bitmap")
	}
	if op == PixDst {
		return nil
	}
	switch op {
	case PixClr, PixSet, PixNotDst:
		rasterOpUniform(dest, dx, dy, dw, dh, op)
		return nil
	}
	if src == nil {
		common.Log.Debug("RasterOperation source bitmap is not defined")
		return errors.New("nil 'src' bitmap")
	}
	if err := rasterOperationClipped(dest, dx, dy, dw, dh, op, src, sx, sy); err != nil {
		return err
	}
	return nil
}
func cmykToRGBA(c color.CMYK) color.RGBA {
	r, g, b := color.CMYKToRGB(c.C, c.M, c.Y, c.K)
	return color.RGBA{R: r, G: g, B: b, A: 0xff}
}
func (g2 *Gray2) Set(x, y int, c color.Color) {
	if x >= g2.Width || y >= g2.Height {
		return
	}
	g := Gray2Model.Convert(c).(color.Gray)
	rowStart := y * g2.BytesPerLine
	idx := rowStart + (x >> 2)
	v := g.Y >> 6
	g2.Data[idx] = (g2.Data[idx] & (^(0xc0 >> uint(2*((x)&3))))) | (v << uint(6-2*(x&3)))
}

var (
	Gray2Model   = color.ModelFunc(gray2ModelConvert)
	Gray4Model   = color.ModelFunc(gray4ModelConvert)
	NRGBA16Model = color.ModelFunc(nrgba16ModelConvert)
)

func (n32 *NRGBA32) Base() *ImageBase { return &n32.ImageBase }

type SMasker interface {
	HasAlpha() bool
	GetAlpha() []byte
	MakeAlpha()
}

func expandBinaryFactor2(dst, src *Monochrome) (err error) {
	srcBytesPerLine := src.BytesPerLine
	dstBytesPerLine := dst.BytesPerLine
	var (
		b                           byte
		expanded                    uint16
		srcRow, dstRow, y, col, idx int
	)
	for y = 0; y < src.Height; y++ {
		srcRow = y * srcBytesPerLine
		dstRow = 2 * y * dstBytesPerLine
		for col = 0; col < srcBytesPerLine; col++ {
			b = src.Data[srcRow+col]
			expanded = expand2Table[b]
			idx = dstRow + col*2
			if dst.BytesPerLine != src.BytesPerLine*2 && (col+1)*2 > dst.BytesPerLine {
				err = dst.setByte(idx, byte(expanded>>8))
			} else {
				err = dst.setTwoBytes(idx, expanded)
			}
			if err != nil {
				return err
			}
		}
		for col = 0; col < dstBytesPerLine; col++ {
			idx = dstRow + dstBytesPerLine + col
			b = dst.Data[dstRow+col]
			if err = dst.setByte(idx, b); err != nil {
				return err
			}
		}
	}
	return nil
}
func (n32 *NRGBA32) NRGBAAt(x, y int) color.NRGBA {
	c, _ := ColorAtNRGBA32(x, y, n32.Width, n32.Data, n32.Alpha, n32.Decode)
	return c
}

type CMYK interface {
	CMYKAt(x, y int) color.CMYK
	SetCMYK(x, y int, c color.CMYK)
}

func nycbcraToNRGBA(c color.NYCbCrA) color.NRGBA {
	y := int32(c.Y) * 0x10101
	cb := int32(c.Cb) - 128
	cr := int32(c.Cr) - 128
	r := y + 91881*cr
	if uint32(r)&0xff000000 == 0 {
		r >>= 8
	} else {
		r = ^(r >> 31) & 0xffff
	}
	g := y - 22554*cb - 46802*cr
	if uint32(g)&0xff000000 == 0 {
		g >>= 8
	} else {
		g = ^(g >> 31) & 0xffff
	}
	b := y + 116130*cb
	if uint32(b)&0xff000000 == 0 {
		b >>= 8
	} else {
		b = ^(b >> 31) & 0xffff
	}
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: c.A}
}
func NewImage(width, height, bitsPerComponent, colorComponents int, data, alpha []byte, decode []float64) (Image, error) {
	base := NewImageBase(width, height, bitsPerComponent, colorComponents, data, alpha, decode)
	var img Image
	switch colorComponents {
	case 1:
		switch bitsPerComponent {
		case 1:
			img = &Monochrome{ImageBase: base, ModelThreshold: 0x0f}
		case 2:
			img = &Gray2{ImageBase: base}
		case 4:
			img = &Gray4{ImageBase: base}
		case 8:
			img = &Gray8{ImageBase: base}
		case 16:
			img = &Gray16{ImageBase: base}
		}
	case 3:
		switch bitsPerComponent {
		case 4:
			img = &NRGBA16{ImageBase: base}
		case 8:
			img = &NRGBA32{ImageBase: base}
		case 16:
			img = &NRGBA64{ImageBase: base}
		}
	case 4:
		img = &CMYK32{ImageBase: base}
	}
	if img == nil {
		return nil, ErrInvalidImage
	}
	return img, nil
}
func (g4 *Gray4) Histogram() (hist [256]int) {
	for x := 0; x < g4.Width; x++ {
		for y := 0; y < g4.Height; y++ {
			hist[g4.GrayAt(x, y).Y]++
		}
	}
	return hist
}
func (c32 *CMYK32) SetCMYK(x, y int, c color.CMYK) {
	idx := 4 * (y*c32.Width + x)
	if idx+3 >= len(c32.Data) {
		return
	}
	c32.Data[idx] = c.C
	c32.Data[idx+1] = c.M
	c32.Data[idx+2] = c.Y
	c32.Data[idx+3] = c.K
}

var _ image.Image = &Gray8{}

func reduceRankBinaryCascade(src *Monochrome, levels ...int) (result *Monochrome, err error) {
	if src == nil {
		return nil, errors.New("source bitmap not defined")
	}
	if len(levels) == 0 {
		return nil, errors.New("there must be at least one level of reduction")
	}
	tab := makeRankReduceTable()
	result = src
	for _, level := range levels {
		if level <= 0 {
			break
		}
		result, err = reduceRankBinary(result, level, tab)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
func (m *Monochrome) ExpandBinary(factor int) (*Monochrome, error) {
	if !IsPowerOf2(uint(factor)) {
		return nil, fmt.Errorf("invalid expand binary factor: %d", factor)
	}
	return expandBinary(m, factor)
}

type RasterOperator int

func rasterOperationClipped(dest *Monochrome, dx, dy int, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	var destOverflowX, srcOverflowX, destOverflowY, srcOverflowY int
	if dx < 0 {
		sx -= dx
		dw += dx
		dx = 0
	}
	if sx < 0 {
		dx -= sx
		dw += sx
		sx = 0
	}
	destOverflowX = dx + dw - dest.Width
	if destOverflowX > 0 {
		dw -= destOverflowX
	}
	srcOverflowX = sx + dw - src.Width
	if srcOverflowX > 0 {
		dw -= srcOverflowX
	}
	if dy < 0 {
		sy -= dy
		dh += dy
		dy = 0
	}
	if sy < 0 {
		dy -= sy
		dh += sy
		sy = 0
	}
	destOverflowY = dy + dh - dest.Height
	if destOverflowY > 0 {
		dh -= destOverflowY
	}
	srcOverflowY = sy + dh - src.Height
	if srcOverflowY > 0 {
		dh -= srcOverflowY
	}
	if dw <= 0 || dh <= 0 {
		return nil
	}
	var err error
	switch {
	case dx&7 == 0 && sx&7 == 0:
		err = rasterOpByteAligned(dest, dx, dy, dw, dh, op, src, sx, sy)
	case dx&7 == sx&7:
		err = rasterOpSameShift(dest, dx, dy, dw, dh, op, src, sx, sy)
	default:
		err = rasterOpShifted(dest, dx, dy, dw, dh, op, src, sx, sy)
	}
	if err != nil {
		return err
	}
	return nil
}
func (g4 *Gray4) GrayAt(x, y int) color.Gray {
	g, _ := ColorAtGray4BPC(x, y, g4.BytesPerLine, g4.Data, g4.Decode)
	return g
}
func ColorAtNRGBA64(x, y, width int, data, alpha []byte, decode []float64) (color.NRGBA64, error) {
	alphaIdx := (y*width + x) * 2
	idx := alphaIdx * 3
	if idx+5 >= len(data) {
		return color.NRGBA64{}, fmt.Errorf("image coordinates out of range (%d, %d)", x, y)
	}
	const maxVal = 0xffff
	a := uint16(maxVal)
	if alpha != nil && len(alpha) > alphaIdx+1 {
		a = uint16(alpha[alphaIdx])<<8 | uint16(alpha[alphaIdx+1])
	}
	r := uint16(data[idx])<<8 | uint16(data[idx+1])
	g := uint16(data[idx+2])<<8 | uint16(data[idx+3])
	b := uint16(data[idx+4])<<8 | uint16(data[idx+5])
	if len(decode) == 6 {
		r = uint16(uint64(LinearInterpolate(float64(r), 0, 65535, decode[0], decode[1])) & maxVal)
		g = uint16(uint64(LinearInterpolate(float64(g), 0, 65535, decode[2], decode[3])) & maxVal)
		b = uint16(uint64(LinearInterpolate(float64(b), 0, 65535, decode[4], decode[5])) & maxVal)
	}
	return color.NRGBA64{R: r, G: g, B: b, A: a}, nil
}
func rasterOpByteAligned(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	var (
		lastMask       byte
		srcStart       int
		dstStart       int
		srcIdx, dstIdx int
		y, i           int
	)
	fullBytes := dw >> 3
	trailingBits := dw & 7
	if trailingBits > 0 {
		lastMask = leadingBitMask[trailingBits]
	}
	srcStart = src.BytesPerLine*sy + (sx >> 3)
	dstStart = dest.BytesPerLine*dy + (dx >> 3)
	switch op {
	case PixSrc:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx], lastMask)
			}
		}
	case PixNotSrc:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = ^(src.Data[srcIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^src.Data[srcIdx], lastMask)
			}
		}
	case PixSrcOrDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] |= src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]|dest.Data[dstIdx], lastMask)
			}
		}
	case PixSrcAndDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] &= src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]&dest.Data[dstIdx], lastMask)
			}
		}
	case PixSrcXorDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] ^= src.Data[srcIdx]
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]^dest.Data[dstIdx], lastMask)
			}
		}
	case PixNotSrcOrDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] |= ^(src.Data[srcIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx])|dest.Data[dstIdx], lastMask)
			}
		}
	case PixNotSrcAndDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] &= ^(src.Data[srcIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx])&dest.Data[dstIdx], lastMask)
			}
		}
	case PixSrcOrNotDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = src.Data[srcIdx] | ^(dest.Data[dstIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]|^(dest.Data[dstIdx]), lastMask)
			}
		}
	case PixSrcAndNotDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = src.Data[srcIdx] &^ (dest.Data[dstIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]&^(dest.Data[dstIdx]), lastMask)
			}
		}
	case PixNotPixSrcOrDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = ^(src.Data[srcIdx] | dest.Data[dstIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] | dest.Data[dstIdx]), lastMask)
			}
		}
	case PixNotPixSrcAndDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = ^(src.Data[srcIdx] & dest.Data[dstIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] & dest.Data[dstIdx]), lastMask)
			}
		}
	case PixNotPixSrcXorDst:
		for y = 0; y < dh; y++ {
			srcIdx = srcStart + y*src.BytesPerLine
			dstIdx = dstStart + y*dest.BytesPerLine
			for i = 0; i < fullBytes; i++ {
				dest.Data[dstIdx] = ^(src.Data[srcIdx] ^ dest.Data[dstIdx])
				dstIdx++
				srcIdx++
			}
			if trailingBits > 0 {
				dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] ^ dest.Data[dstIdx]), lastMask)
			}
		}
	default:
		common.Log.Debug("Provided invalid raster operator: %v", op)
		return errors.New("invalid raster operator")
	}
	return nil
}
func (b *ImageBase) setByte(idx int, v byte) error {
	if idx > len(b.Data)-1 {
		return errors.New("index out of range")
	}
	b.Data[idx] = v
	return nil
}
func rasterOpShifted(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	var (
		hasLeading        bool
		singleByte        bool
		leadingMask       byte
		leadingBits       int
		dstOffset         int
		dstLeading        int
		srcLeading        int
		hasMiddle         bool
		middleBytes       int
		dstMiddle         int
		srcMiddle         int
		hasTrailing       bool
		trailingMask      byte
		trailingBits      int
		dstTrailing       int
		srcTrailing       int
		shifted           byte
		srcLeadingBits    int
		srcOffset         int
		shiftLeftBits     uint
		shiftRightBits    uint
		shiftMask         byte
		firstShift        shift
		needNextByte      bool
		trailingNeedsNext bool
		y, i              int
	)
	if sx&7 != 0 {
		srcOffset = 8 - (sx & 7)
	}
	if dx&7 != 0 {
		dstOffset = 8 - (dx & 7)
	}
	if srcOffset == 0 && dstOffset == 0 {
		shiftMask = trailingBitMask[0]
	} else {
		if dstOffset > srcOffset {
			shiftLeftBits = uint(dstOffset - srcOffset)
		} else {
			shiftLeftBits = uint(8 - (srcOffset - dstOffset))
		}
		shiftRightBits = 8 - shiftLeftBits
		shiftMask = trailingBitMask[shiftLeftBits]
	}
	if (dx & 7) != 0 {
		hasLeading = true
		leadingBits = 8 - (dx & 7)
		leadingMask = trailingBitMask[leadingBits]
		dstLeading = dest.BytesPerLine*dy + (dx >> 3)
		srcLeading = src.BytesPerLine*sy + (sx >> 3)
		srcLeadingBits = 8 - (sx & 7)
		if leadingBits > srcLeadingBits {
			firstShift = shiftLeft
			if dw >= srcOffset {
				needNextByte = true
			}
		} else {
			firstShift = shiftRight
		}
	}
	if dw < leadingBits {
		singleByte = true
		leadingMask &= leadingBitMask[8-leadingBits+dw]
	}
	if !singleByte {
		middleBytes = (dw - leadingBits) >> 3
		if middleBytes != 0 {
			hasMiddle = true
			dstMiddle = dest.BytesPerLine*dy + ((dx + dstOffset) >> 3)
			srcMiddle = src.BytesPerLine*sy + ((sx + dstOffset) >> 3)
		}
	}
	trailingBits = (dx + dw) & 7
	if !(singleByte || trailingBits == 0) {
		hasTrailing = true
		trailingMask = leadingBitMask[trailingBits]
		dstTrailing = dest.BytesPerLine*dy + ((dx + dstOffset) >> 3) + middleBytes
		srcTrailing = src.BytesPerLine*sy + ((sx + dstOffset) >> 3) + middleBytes
		if trailingBits > int(shiftRightBits) {
			trailingNeedsNext = true
		}
	}
	switch op {
	case PixSrc:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted, leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted, trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrc:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^shifted, leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = ^shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^shifted, trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcOrDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted|dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] |= shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted|dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcAndDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted&dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] &= shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted&dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcXorDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted^dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] ^= shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrcOrDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^shifted|dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] |= ^shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^shifted|dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrcAndDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^shifted&dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] &= ^shifted
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^shifted&dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcOrNotDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted|^dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = shifted | ^dest.Data[dstMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted|^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcAndNotDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], shifted&^dest.Data[dstLeading], leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = shifted &^ dest.Data[dstMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], shifted&^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcOrDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^(shifted | dest.Data[dstLeading]), leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = ^(shifted | dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(shifted | dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcAndDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^(shifted & dest.Data[dstLeading]), leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = ^(shifted & dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(shifted & dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcXorDst:
		if hasLeading {
			for y = 0; y < dh; y++ {
				if firstShift == shiftLeft {
					shifted = src.Data[srcLeading] << shiftLeftBits
					if needNextByte {
						shifted = mergeMasked(shifted, src.Data[srcLeading+1]>>shiftRightBits, shiftMask)
					}
				} else {
					shifted = src.Data[srcLeading] >> shiftRightBits
				}
				dest.Data[dstLeading] = mergeMasked(dest.Data[dstLeading], ^(shifted ^ dest.Data[dstLeading]), leadingMask)
				dstLeading += dest.BytesPerLine
				srcLeading += src.BytesPerLine
			}
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					shifted = mergeMasked(src.Data[srcMiddle+i]<<shiftLeftBits, src.Data[srcMiddle+i+1]>>shiftRightBits, shiftMask)
					dest.Data[dstMiddle+i] = ^(shifted ^ dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				shifted = src.Data[srcTrailing] << shiftLeftBits
				if trailingNeedsNext {
					shifted = mergeMasked(shifted, src.Data[srcTrailing+1]>>shiftRightBits, shiftMask)
				}
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(shifted ^ dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	default:
		common.Log.Debug("Operation: '%d' not permitted", op)
		return errors.New("raster operation not permitted")
	}
	return nil
}

const (
	PixSrc             RasterOperator = 0xc
	PixDst             RasterOperator = 0xa
	PixNotSrc          RasterOperator = 0x3
	PixNotDst          RasterOperator = 0x5
	PixClr             RasterOperator = 0x0
	PixSet             RasterOperator = 0xf
	PixSrcOrDst        RasterOperator = 0xe
	PixSrcAndDst       RasterOperator = 0x8
	PixSrcXorDst       RasterOperator = 0x6
	PixNotSrcOrDst     RasterOperator = 0xb
	PixNotSrcAndDst    RasterOperator = 0x2
	PixSrcOrNotDst     RasterOperator = 0xd
	PixSrcAndNotDst    RasterOperator = 0x4
	PixNotPixSrcOrDst  RasterOperator = 0x1
	PixNotPixSrcAndDst RasterOperator = 0x7
	PixNotPixSrcXorDst RasterOperator = 0x9
	PixPaint                          = PixSrcOrDst
	PixSubtract                       = PixNotSrcAndDst
	PixMask                           = PixSrcAndDst
)

func rasterOpSameShift(dest *Monochrome, dx, dy, dw, dh int, op RasterOperator, src *Monochrome, sx, sy int) error {
	var (
		singleByte   bool
		hasMiddle    bool
		middleBytes  int
		dstMiddle    int
		srcMiddle    int
		hasTrailing  bool
		trailingMask byte
		trailingBits int
		dstTrailing  int
		srcTrailing  int
		y, i         int
	)
	leadingBits := 8 - (dx & 7)
	leadingMask := trailingBitMask[leadingBits]
	dstIdx := dest.BytesPerLine*dy + (dx >> 3)
	srcIdx := src.BytesPerLine*sy + (sx >> 3)
	if dw < leadingBits {
		singleByte = true
		leadingMask &= leadingBitMask[8-leadingBits+dw]
	}
	if !singleByte {
		middleBytes = (dw - leadingBits) >> 3
		if middleBytes > 0 {
			hasMiddle = true
			dstMiddle = dstIdx + 1
			srcMiddle = srcIdx + 1
		}
	}
	trailingBits = (dx + dw) & 7
	if !(singleByte || trailingBits == 0) {
		hasTrailing = true
		trailingMask = leadingBitMask[trailingBits]
		dstTrailing = dstIdx + 1 + middleBytes
		srcTrailing = srcIdx + 1 + middleBytes
	}
	switch op {
	case PixSrc:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrc:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^src.Data[srcIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^src.Data[srcTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcOrDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]|dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] |= src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]|dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcAndDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]&dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] &= src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]&dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcXorDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]^dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] ^= src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]^dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrcOrDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx])|dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] |= ^(src.Data[srcMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(src.Data[srcTrailing])|dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotSrcAndDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx])&dest.Data[dstIdx], leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] &= ^src.Data[srcMiddle+i]
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(src.Data[srcTrailing])&dest.Data[dstTrailing], trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcOrNotDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]|^(dest.Data[dstIdx]), leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = src.Data[srcMiddle+i] | ^(dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]|^(dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixSrcAndNotDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], src.Data[srcIdx]&^(dest.Data[dstIdx]), leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = src.Data[srcMiddle+i] &^ (dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], src.Data[srcTrailing]&^(dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcOrDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] | dest.Data[dstIdx]), leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^(src.Data[srcMiddle+i] | dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(src.Data[srcTrailing] | dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcAndDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] & dest.Data[dstIdx]), leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^(src.Data[srcMiddle+i] & dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(src.Data[srcTrailing] & dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	case PixNotPixSrcXorDst:
		for y = 0; y < dh; y++ {
			dest.Data[dstIdx] = mergeMasked(dest.Data[dstIdx], ^(src.Data[srcIdx] ^ dest.Data[dstIdx]), leadingMask)
			dstIdx += dest.BytesPerLine
			srcIdx += src.BytesPerLine
		}
		if hasMiddle {
			for y = 0; y < dh; y++ {
				for i = 0; i < middleBytes; i++ {
					dest.Data[dstMiddle+i] = ^(src.Data[srcMiddle+i] ^ dest.Data[dstMiddle+i])
				}
				dstMiddle += dest.BytesPerLine
				srcMiddle += src.BytesPerLine
			}
		}
		if hasTrailing {
			for y = 0; y < dh; y++ {
				dest.Data[dstTrailing] = mergeMasked(dest.Data[dstTrailing], ^(src.Data[srcTrailing] ^ dest.Data[dstTrailing]), trailingMask)
				dstTrailing += dest.BytesPerLine
				srcTrailing += src.BytesPerLine
			}
		}
	default:
		common.Log.Debug("Invalid raster operator: %d", op)
		return errors.New("invalid raster operator")
	}
	return nil
}
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
func mergeMasked(dst, src, mask byte) byte { return (dst &^ (mask)) | (src & mask) }
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
