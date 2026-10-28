package imagerender

import (
	"image"
	color2 "image/color"
	"math"

	"github.com/OpenMellon/freetype/raster"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"golang.org/x/image/draw"
)

func (dc *Context) BlendMode() context.BlendMode    { return dc.blendMode }
func (dc *Context) Interpolator() draw.Interpolator { return dc.interpolator }

func (dc *Context) SetInterpolator(interpolator draw.Interpolator) { dc.interpolator = interpolator }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func (dc *Context) Identity() { dc.matrix = transform.IdentityMatrix() }

func (dc *Context) SetRGB255(r, g, b int)    { dc.SetRGBA255(r, g, b, 255) }
func (dc *Context) Matrix() transform.Matrix { return dc.matrix }
func (dc *Context) Image() image.Image       { return dc.img }
func (dc *Context) SetStrokeRGBA(r, g, b, a float64) {
	if dc.colorLocked {
		return
	}
	if solid, ok := dc.strokePattern.(*solidPattern); ok {
		if _, _, _, a16 := solid.color.RGBA(); a16 > 0 && a16 != 65535 && a == 1 {
			a = float64(a16) / 65535
		}
	}
	if a == 1 && dc.strokeAlpha < 1 {
		a = dc.strokeAlpha
	}
	col := color2.NRGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), uint8(a * 255)}
	dc.strokePattern = newSolidPattern(col)
}

func (dc *Context) ShearAbout(sx, sy, x, y float64) {
	dc.Translate(x, y)
	dc.Shear(sx, sy)
	dc.Translate(-x, -y)
}

func (dc *Context) Pop() {
	cur := *dc
	stack := dc.stack
	prev := stack[len(stack)-1]
	*dc = *prev
	dc.path = cur.path
	dc.fillPath = cur.fillPath
	dc.start = cur.start
	dc.current = cur.current
	dc.hasCurrent = cur.hasCurrent
	if cur.clipScratch != nil {
		dc.clipScratch = cur.clipScratch
	}
	if cur.ownsMask && cur.mask != prev.mask {
		releaseAlpha(cur.mask)
	}
}
func (dc *Context) SetMatrix(m transform.Matrix) { dc.matrix = m }
func (dc *Context) Rotate(angle float64)         { dc.matrix = dc.matrix.Rotate(angle) }

func (dc *Context) SetFillStyle(pattern context.Pattern) {
	if dc.colorLocked {
		return
	}
	if solid, ok := pattern.(*solidPattern); ok {
		dc.color = solid.color
	}
	dc.fillPattern = pattern
}
func (dc *Context) SetBlendMode(mode context.BlendMode) { dc.blendMode = mode }
func NewContextForRGBA(im *image.RGBA) *Context {
	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	return &Context{width: w, height: h, rasterizer: raster.NewRasterizer(w, h), img: im, baseImg: im, color: color2.Transparent, fillPattern: whitePattern, strokePattern: blackPattern, lineWidth: 1, fillRule: context.FillRuleWinding, matrix: transform.IdentityMatrix(), textState: context.NewTextState(), interpolator: draw.BiLinear, blendMode: context.BlendModeNormal, fillAlpha: 1, strokeAlpha: 1}
}
func (dc *Context) SetColor(c color2.Color)  { dc.setFillAndStrokeColor(c) }
func (dc *Context) SetStrokeAlpha(a float64) { dc.strokeAlpha = clamp01(a) }
func (dc *Context) SetRGB(r, g, b float64)   { dc.SetRGBA(r, g, b, 1) }

func (dc *Context) Scale(x, y float64) { dc.matrix = dc.matrix.Scale(x, y) }

func (dc *Context) SetPixel(x, y int) { dc.img.Set(x, y, dc.color) }

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func NewContext(width, height int) *Context {
	return NewContextForRGBA(image.NewRGBA(image.Rect(0, 0, width, height)))
}

func (dc *Context) SetRGBA255(r, g, b, a int) {
	if dc.colorLocked {
		return
	}
	dc.color = color2.NRGBA{uint8(r), uint8(g), uint8(b), uint8(a)}
	dc.setFillAndStrokeColor(dc.color)
}

func (dc *Context) SetStrokeStyle(pattern context.Pattern) {
	if dc.colorLocked {
		return
	}
	dc.strokePattern = pattern
}
func (dc *Context) Shear(x, y float64) { dc.matrix.Shear(x, y) }

func (dc *Context) SetRGBA(r, g, b, a float64) {
	if dc.colorLocked {
		return
	}
	_, _, _, a16 := dc.color.RGBA()
	if a16 > 0 && a16 != 65535 && a == 1 {
		a = float64(a16) / 65535
	}
	dc.color = color2.NRGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), uint8(a * 255)}
	dc.setFillAndStrokeColor(dc.color)
}
func (dc *Context) LockColor() { dc.colorLocked = true }
func (dc *Context) drawRegularPolygon(n int, x, y, r, rotation float64) {
	angle := 2 * math.Pi / float64(n)
	rotation -= math.Pi / 2
	if n%2 == 0 {
		rotation += angle / 2
	}
	dc.NewSubPath()
	for i := 0; i < n; i++ {
		a := rotation + angle*float64(i)
		dc.LineTo(x+r*math.Cos(a), y+r*math.Sin(a))
	}
	dc.ClosePath()
}

type Context struct {
	width           int
	height          int
	rasterizer      *raster.Rasterizer
	img             *image.RGBA
	mask            *image.Alpha
	color           color2.Color
	fillPattern     context.Pattern
	strokePattern   context.Pattern
	path            raster.Path
	fillPath        raster.Path
	start           transform.Point
	current         transform.Point
	hasCurrent      bool
	dashes          []float64
	dashOffset      float64
	lineWidth       float64
	lineCap         context.LineCap
	lineJoin        context.LineJoin
	fillRule        context.FillRule
	matrix          transform.Matrix
	textState       context.TextState
	stack           []*Context
	pathStack       []pathSnapshot
	clipBounds      image.Rectangle
	clipBoundsValid bool
	colorLocked     bool
	ownsMask        bool
	clipScratch     *image.Alpha
	interpolator    draw.Interpolator
	glyphCache      *GlyphCache
	blendMode       context.BlendMode
	fillAlpha       float64
	strokeAlpha     float64
	softMask        *image.Alpha
	combinedMask    *image.Alpha
	baseImg         *image.RGBA
	backdrop        *image.RGBA
	knockout        bool
	groupStack      []*groupState
}

func (dc *Context) LineWidth() float64     { return dc.lineWidth }
func (dc *Context) SetFillAlpha(a float64) { dc.fillAlpha = clamp01(a) }

func (dc *Context) RotateAbout(angle, x, y float64) {
	dc.Translate(x, y)
	dc.Rotate(angle)
	dc.Translate(-x, -y)
}

func (dc *Context) Push() {
	saved := *dc
	dc.stack = append(dc.stack, &saved)
	dc.ownsMask = false
}
func (dc *Context) Width() int { return dc.width }
func (dc *Context) Clear() {
	src := image.NewUniform(dc.color)
	draw.Draw(dc.img, dc.img.Bounds(), src, image.Point{}, draw.Src)
}
func (dc *Context) Transform(x, y float64) (tx, ty float64) { return dc.matrix.Transform(x, y) }

func (dc *Context) ScaleAbout(sx, sy, x, y float64) {
	dc.Translate(x, y)
	dc.Scale(sx, sy)
	dc.Translate(-x, -y)
}
func (dc *Context) Translate(x, y float64) { dc.matrix = dc.matrix.Translate(x, y) }

func (dc *Context) SetFillRule(fillRule context.FillRule) { dc.fillRule = fillRule }

func (dc *Context) SetHexColor(x string) {
	r, g, b, a := parseHexColor(x)
	dc.SetRGBA255(r, g, b, a)
}
func NewContextForImage(im image.Image) *Context { return NewContextForRGBA(toRGBA(im)) }
func (dc *Context) SetFillRGBA(r, g, b, a float64) {
	if dc.colorLocked {
		return
	}
	_, _, _, a16 := dc.color.RGBA()
	if a16 > 0 && a16 != 65535 && a == 1 {
		a = float64(a16) / 65535
	}
	if a == 1 && dc.fillAlpha < 1 {
		a = dc.fillAlpha
	}
	col := color2.NRGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), uint8(a * 255)}
	dc.color = col
	dc.fillPattern = newSolidPattern(col)
}
func (dc *Context) Height() int { return dc.height }
