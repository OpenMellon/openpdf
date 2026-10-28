package imagerender

import (
	"fmt"
	"image"
	draw2 "image/draw"
	"math"
	"strings"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"golang.org/x/image/draw"
)

func blendPixel(dst, backdrop []uint8, mode context.BlendMode, sr, sg, sb, alpha float64) {
	if alpha <= 0 {
		return
	}
	dstA8 := dst[3]
	var da, dr, dg, db float64
	if dstA8 != 0 {
		da = float64(dstA8) / 255
		dr = float64(dst[0]) / 255 / da
		dg = float64(dst[1]) / 255 / da
		db = float64(dst[2]) / 255 / da
	}
	ba, br, bg, bb := da, dr, dg, db
	if backdrop != nil && da < 1 {
		if backA8 := backdrop[3]; backA8 != 0 {
			backA := float64(backA8) / 255
			backR := float64(backdrop[0]) / 255 / backA
			backG := float64(backdrop[1]) / 255 / backA
			backB := float64(backdrop[2]) / 255 / backA
			if extra := backA * (1 - da); extra > 0 {
				ba = da + extra
				br = (dr*da + backR*extra) / ba
				bg = (dg*da + backG*extra) / ba
				bb = (db*da + backB*extra) / ba
			}
		}
	}
	var blR, blG, blB float64
	switch {
	case mode == context.BlendModeNormal:
		blR, blG, blB = sr, sg, sb
	case mode >= context.BlendModeHue:
		nonsep := blendNonSeparable(mode, [3]float64{br, bg, bb}, [3]float64{sr, sg, sb})
		blR, blG, blB = nonsep[0], nonsep[1], nonsep[2]
	default:
		blR = blendSeparable(mode, br, sr)
		blG = blendSeparable(mode, bg, sg)
		blB = blendSeparable(mode, bb, sb)
	}
	mixR := (1-ba)*sr + ba*blR
	mixG := (1-ba)*sg + ba*blG
	mixB := (1-ba)*sb + ba*blB
	outA := alpha + da - alpha*da
	if outA <= 0 {
		return
	}
	wSrc := alpha / outA
	wDst := 1 - wSrc
	outR := wDst*dr + wSrc*mixR
	outG := wDst*dg + wSrc*mixG
	outB := wDst*db + wSrc*mixB
	dst[0] = toByte(outR * outA)
	dst[1] = toByte(outG * outA)
	dst[2] = toByte(outB * outA)
	dst[3] = toByte(outA)
}

const alpha16Max = 1<<16 - 1

func setLum(c [3]float64, l float64) [3]float64 {
	d := l - lum(c)
	c[0] += d
	c[1] += d
	c[2] += d
	return clipColor(c)
}

func softLight(cb, cs float64) float64 {
	if cs <= 0.5 {
		return cb - (1-2*cs)*cb*(1-cb)
	}
	var d float64
	if cb <= 0.25 {
		d = ((16*cb-12)*cb + 4) * cb
	} else {
		d = math.Sqrt(cb)
	}
	return cb + (2*cs-1)*(d-cb)
}

func paintAlpha16(pattern context.Pattern, alpha float64, colorLocked bool) uint32 {
	if _, isSolid := pattern.(*solidPattern); isSolid && !colorLocked {
		return alpha16Max
	}
	return uint32(alpha*alpha16Max + 0.5)
}

func sat(c [3]float64) float64 {
	return math.Max(c[0], math.Max(c[1], c[2])) - math.Min(c[0], math.Min(c[1], c[2]))
}

func newAlpha(w, h int) *image.Alpha {
	pix := core.AcquireBytesZeroed(w * h)
	return &image.Alpha{Pix: pix, Stride: w, Rect: image.Rect(0, 0, w, h)}
}

func blendNonSeparable(mode context.BlendMode, cb, cs [3]float64) [3]float64 {
	switch mode {
	case context.BlendModeHue:
		return setLum(setSat(cs, sat(cb)), lum(cb))
	case context.BlendModeSaturation:
		return setLum(setSat(cb, sat(cs)), lum(cb))
	case context.BlendModeColor:
		return setLum(cs, lum(cb))
	case context.BlendModeLuminosity:
		return setLum(cb, lum(cs))
	default:
		return cs
	}
}

func blendSeparable(mode context.BlendMode, cb, cs float64) float64 {
	switch mode {
	case context.BlendModeMultiply:
		return cb * cs
	case context.BlendModeScreen:
		return cb + cs - cb*cs
	case context.BlendModeOverlay:
		return hardLight(cs, cb)
	case context.BlendModeDarken:
		return math.Min(cb, cs)
	case context.BlendModeLighten:
		return math.Max(cb, cs)
	case context.BlendModeColorDodge:
		if cb == 0 {
			return 0
		}
		if cs >= 1 {
			return 1
		}
		return math.Min(1, cb/(1-cs))
	case context.BlendModeColorBurn:
		if cb >= 1 {
			return 1
		}
		if cs <= 0 {
			return 0
		}
		return 1 - math.Min(1, (1-cb)/cs)
	case context.BlendModeHardLight:
		return hardLight(cb, cs)
	case context.BlendModeSoftLight:
		return softLight(cb, cs)
	case context.BlendModeDifference:
		return math.Abs(cb - cs)
	case context.BlendModeExclusion:
		return cb + cs - 2*cb*cs
	default:
		return cs
	}
}

func compositeBlend(dst, src, backdrop *image.RGBA, mode context.BlendMode, alpha float64, mask *image.Alpha, knockout bool) {
	alpha = clamp01(alpha)
	if alpha == 0 {
		return
	}
	area := src.Bounds().Intersect(dst.Bounds())
	if area.Empty() {
		return
	}
	if backdrop != nil && !area.In(backdrop.Bounds()) {
		backdrop = nil
	}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		si := src.PixOffset(area.Min.X, y)
		di := dst.PixOffset(area.Min.X, y)
		bi := 0
		if backdrop != nil {
			bi = backdrop.PixOffset(area.Min.X, y)
		}
		for x := area.Min.X; x < area.Max.X; x++ {
			a := alpha
			if mask != nil {
				a *= float64(mask.AlphaAt(x, y).A) / 255
			}
			if a == 0 {
				si += 4
				di += 4
				bi += 4
				continue
			}
			if srcA8 := src.Pix[si+3]; srcA8 != 0 {
				srcA := float64(srcA8) / 255
				sr := float64(src.Pix[si+0]) / 255 / srcA
				sg := float64(src.Pix[si+1]) / 255 / srcA
				sb := float64(src.Pix[si+2]) / 255 / srcA
				var bd []uint8
				if backdrop != nil {
					bd = backdrop.Pix[bi : bi+4]
				}
				if knockout {
					var tmp [4]uint8
					blendPixel(tmp[:], bd, mode, sr, sg, sb, a)
					lerpPixel(dst.Pix[di:di+4], tmp[:], uint32(srcA8), 255)
				} else {
					blendPixel(dst.Pix[di:di+4], bd, mode, sr, sg, sb, srcA*a)
				}
			}
			si += 4
			di += 4
			bi += 4
		}
	}
}

func toRGBA(im image.Image) *image.RGBA {
	bounds := im.Bounds()
	dst := image.NewRGBA(bounds)
	draw2.Draw(dst, bounds, im, bounds.Min, draw2.Src)
	return dst
}

func toByte(v float64) uint8 {
	v = v*255 + 0.5
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

func parseHexColor(s string) (r, g, b, a int) {
	s = strings.TrimPrefix(s, "#")
	a = 255
	if len(s) == 3 {
		format := "%1x%1x%1x"
		fmt.Sscanf(s, format, &r, &g, &b)
		r |= r << 4
		g |= g << 4
		b |= b << 4
	}
	if len(s) == 6 {
		format := "%02x%02x%02x"
		fmt.Sscanf(s, format, &r, &g, &b)
	}
	if len(s) == 8 {
		format := "%02x%02x%02x%02x"
		fmt.Sscanf(s, format, &r, &g, &b, &a)
	}
	return
}

func copyToAlpha(src image.Image, pt image.Point, r image.Rectangle) *image.Alpha {
	w, h := r.Dx(), r.Dy()
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	if srcAlpha, ok := src.(*image.Alpha); ok {
		for y := 0; y < h; y++ {
			so := srcAlpha.PixOffset(pt.X, pt.Y+y)
			do := y * dst.Stride
			copy(dst.Pix[do:do+w], srcAlpha.Pix[so:so+w])
		}
		return dst
	}
	draw.Draw(dst, dst.Rect, src, pt, draw.Src)
	return dst
}

func setSat(c [3]float64, s float64) [3]float64 {
	imin, imid, imax := 0, 1, 2
	if c[imin] > c[imid] {
		imin, imid = imid, imin
	}
	if c[imid] > c[imax] {
		imid, imax = imax, imid
	}
	if c[imin] > c[imid] {
		imin, imid = imid, imin
	}
	if c[imax] > c[imin] {
		c[imid] = (c[imid] - c[imin]) * s / (c[imax] - c[imin])
		c[imax] = s
	} else {
		c[imid] = 0
		c[imax] = 0
	}
	c[imin] = 0
	return c
}

func clipColor(c [3]float64) [3]float64 {
	l := lum(c)
	lo := math.Min(c[0], math.Min(c[1], c[2]))
	hi := math.Max(c[0], math.Max(c[1], c[2]))
	if lo < 0 {
		if d := l - lo; d != 0 {
			for i := range c {
				c[i] = l + (c[i]-l)*l/d
			}
		}
	}
	if hi > 1 {
		if d2 := hi - l; d2 != 0 {
			for i2 := range c {
				c[i2] = l + (c[i2]-l)*(1-l)/d2
			}
		}
	}
	return c
}

func releaseAlpha(a *image.Alpha) {
	if a == nil {
		return
	}
	core.ReleaseBytes(a.Pix)
	a.Pix = nil
}

func lum(c [3]float64) float64 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }

func lerpPixel(dst []uint8, src []uint8, w, maxW uint32) {
	inv := maxW - w
	for i := 0; i < 4; i++ {
		dst[i] = uint8((uint32(dst[i])*inv + uint32(src[i])*w + maxW/2) / maxW)
	}
}

func hardLight(cb, cs float64) float64 {
	if cs <= 0.5 {
		return cb * (2 * cs)
	}
	return cb + (2*cs - 1) - cb*(2*cs-1)
}

func unpremultiply(c, a uint8) uint8 {
	v := (uint32(c)*255 + uint32(a)/2) / uint32(a)
	if v > 255 {
		return 255
	}
	return uint8(v)
}
