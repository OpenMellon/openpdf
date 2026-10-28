// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imageutil

import (
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/bitwise"
)

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
	g2.Data[idx] = (g2.Data[idx] & (^(0xc0 >> uint(2*(x&3))))) | (v << uint(6-2*(x&3)))
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
