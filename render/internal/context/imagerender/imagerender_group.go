package imagerender

import (
	"errors"
	"image"
	color2 "image/color"
	"math"

	"github.com/OpenMellon/freetype/raster"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

func (dc *Context) BeginTransparencyGroup(group context.TransparencyGroup) {
	saved := &groupState{img: dc.img, mask: dc.mask, ownsMask: dc.ownsMask, clipBounds: dc.clipBounds, clipBoundsValid: dc.clipBoundsValid, blendMode: dc.blendMode, fillAlpha: dc.fillAlpha, strokeAlpha: dc.strokeAlpha, color: dc.color, fillPattern: dc.fillPattern, strokePattern: dc.strokePattern, softMask: dc.softMask, convert: group.Convert, backdrop: dc.backdrop, knockout: dc.knockout}
	dc.groupStack = append(dc.groupStack, saved)
	switch {
	case group.Isolated:
		dc.backdrop = nil
	case dc.knockout:
	case dc.img == dc.baseImg:
		dc.backdrop = nil
	default:
		dc.backdrop = dc.img
	}
	dc.knockout = group.Knockout
	dc.img = image.NewRGBA(dc.img.Bounds())
	dc.ownsMask = false
	dc.blendMode = context.BlendModeNormal
	dc.fillAlpha = 1
	dc.strokeAlpha = 1
	dc.softMask = nil
	col := color2.NRGBAModel.Convert(dc.color).(color2.NRGBA)
	col.A = 255
	dc.color = col
	dc.fillPattern = newSolidPattern(col)
	dc.strokePattern = newSolidPattern(col)
}

func (dc *Context) ClipPreserve() {
	if dc.clipScratch == nil || dc.clipScratch.Bounds().Size() != image.Pt(dc.width, dc.height) {
		if dc.clipScratch != nil {
			releaseAlpha(dc.clipScratch)
		}
		dc.clipScratch = newAlpha(dc.width, dc.height)
	} else {
		clear(dc.clipScratch.Pix)
	}
	painter := raster.NewAlphaOverPainter(dc.clipScratch)
	dc.fill(painter)
	if dc.mask == nil {
		mask := newAlpha(dc.width, dc.height)
		copy(mask.Pix, dc.clipScratch.Pix)
		dc.mask = mask
		dc.ownsMask = true
	} else {
		if !dc.ownsMask {
			owned := newAlpha(dc.width, dc.height)
			copy(owned.Pix, dc.mask.Pix)
			dc.mask = owned
			dc.ownsMask = true
		}
		maskPix := dc.mask.Pix
		clipPix := dc.clipScratch.Pix
		for i, cov := range clipPix {
			if cov == 0 {
				maskPix[i] = 0
			} else if cov != 255 {
				v := int(maskPix[i]) * int(cov)
				maskPix[i] = uint8(v / 255)
			}
		}
	}
	dc.clipBoundsValid = false
}

func (dc *Context) AsMask() *image.Alpha {
	mask := image.NewAlpha(dc.img.Bounds())
	draw.Draw(mask, dc.img.Bounds(), dc.img, image.Point{}, draw.Src)
	return mask
}
func (dc *Context) Clip() { dc.ClipPreserve(); dc.ClearPath() }

func (dc *Context) ReleaseBuffers() {
	if dc.ownsMask {
		releaseAlpha(dc.mask)
	}
	dc.mask = nil
	dc.ownsMask = false
	if dc.clipScratch != nil {
		releaseAlpha(dc.clipScratch)
		dc.clipScratch = nil
	}
}

func (dc *Context) InvertMask() {
	if dc.mask == nil {
		dc.mask = newAlpha(dc.img.Bounds().Dx(), dc.img.Bounds().Dy())
		dc.ownsMask = true
	} else {
		if !dc.ownsMask {
			owned := newAlpha(dc.mask.Bounds().Dx(), dc.mask.Bounds().Dy())
			copy(owned.Pix, dc.mask.Pix)
			dc.mask = owned
			dc.ownsMask = true
		}
		for i, v := range dc.mask.Pix {
			dc.mask.Pix[i] = 255 - v
		}
	}
	dc.clipBounds = dc.img.Bounds()
	dc.clipBoundsValid = true
}

func (dc *Context) SetMask(mask *image.Alpha) error {
	if mask.Bounds().Size() != dc.img.Bounds().Size() {
		return errors.New("mask size must match context size")
	}
	if dc.ownsMask && mask != dc.mask {
		releaseAlpha(dc.mask)
	}
	dc.mask = mask
	dc.ownsMask = false
	dc.clipBoundsValid = false
	return nil
}

func transformedBounds(aff f64.Aff3, r image.Rectangle) image.Rectangle {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range [4][2]float64{{float64(r.Min.X), float64(r.Min.Y)}, {float64(r.Max.X), float64(r.Min.Y)}, {float64(r.Min.X), float64(r.Max.Y)}, {float64(r.Max.X), float64(r.Max.Y)}} {
		x := aff[0]*corner[0] + aff[1]*corner[1] + aff[2]
		y := aff[3]*corner[0] + aff[4]*corner[1] + aff[5]
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY))).Inset(-2)
}

type groupState struct {
	img             *image.RGBA
	mask            *image.Alpha
	ownsMask        bool
	clipBounds      image.Rectangle
	clipBoundsValid bool
	blendMode       context.BlendMode
	fillAlpha       float64
	strokeAlpha     float64
	color           color2.Color
	fillPattern     context.Pattern
	strokePattern   context.Pattern
	softMask        *image.Alpha
	convert         context.ColorConverter
	backdrop        *image.RGBA
	knockout        bool
}

func (dc *Context) SetSoftMask(mask *image.Alpha) {
	if mask != nil && mask.Bounds().Size() != dc.img.Bounds().Size() {
		return
	}
	dc.softMask = mask
}

func (dc *Context) effectiveMask() *image.Alpha {
	if dc.softMask == nil {
		return dc.mask
	}
	if dc.mask == nil {
		return dc.softMask
	}
	bounds := dc.mask.Bounds()
	if dc.combinedMask == nil || dc.combinedMask.Rect != bounds {
		dc.combinedMask = image.NewAlpha(bounds)
	}
	out := dc.combinedMask
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		mi := dc.mask.PixOffset(bounds.Min.X, y)
		si := dc.softMask.PixOffset(bounds.Min.X, y)
		oi := out.PixOffset(bounds.Min.X, y)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			out.Pix[oi] = uint8(uint32(dc.mask.Pix[mi]) * uint32(dc.softMask.Pix[si]) / 255)
			mi++
			si++
			oi++
		}
	}
	return out
}

func (dc *Context) ClipBounds() image.Rectangle {
	if dc.mask == nil {
		return dc.img.Bounds()
	}
	if !dc.clipBoundsValid {
		dc.clipBounds = alphaBounds(dc.mask)
		dc.clipBoundsValid = true
	}
	return dc.clipBounds
}

func (dc *Context) ResetClip() {
	if dc.ownsMask {
		releaseAlpha(dc.mask)
	}
	dc.mask = nil
	dc.ownsMask = false
}

func (dc *Context) EndTransparencyGroup() {
	if len(dc.groupStack) == 0 {
		return
	}
	top := len(dc.groupStack) - 1
	saved := dc.groupStack[top]
	dc.groupStack = dc.groupStack[:top]
	if dc.ownsMask && dc.mask != saved.mask {
		releaseAlpha(dc.mask)
	}
	groupImg := dc.img
	dc.img = saved.img
	dc.mask = saved.mask
	dc.ownsMask = saved.ownsMask
	dc.clipBounds = saved.clipBounds
	dc.clipBoundsValid = saved.clipBoundsValid
	dc.blendMode = saved.blendMode
	dc.fillAlpha = saved.fillAlpha
	dc.strokeAlpha = saved.strokeAlpha
	dc.color = saved.color
	dc.fillPattern = saved.fillPattern
	dc.strokePattern = saved.strokePattern
	dc.softMask = saved.softMask
	dc.backdrop = saved.backdrop
	dc.knockout = saved.knockout
	if saved.convert != nil {
		convertColors(groupImg, saved.convert)
	}
	compositeBlend(dc.img, groupImg, dc.backdrop, saved.blendMode, saved.fillAlpha, saved.softMask, dc.knockout)
}
