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
