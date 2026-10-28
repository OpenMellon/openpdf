package imagerender

import (
	"image"
	color2 "image/color"
	"math"
	"sort"

	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/unidoc/freetype/raster"
)

func lerpChannel(from, to uint32, t float64) uint8 {
	return uint8(int32(float64(from)*(1.0-t)+float64(to)*t) >> 8)
}

func lerpColor(c0, c1 color2.Color, t float64) color2.Color {
	r0, g0, b0, a0 := c0.RGBA()
	r1, g1, b1, a1 := c1.RGBA()
	return color2.RGBA{lerpChannel(r0, r1, t), lerpChannel(g0, g1, t), lerpChannel(b0, b1, t), lerpChannel(a0, a1, t)}
}

type solidPattern struct{ color color2.Color }

func tileRepeatCount(size, step float64) int {
	n := math.Ceil(size / step)
	if !(n >= 1) {
		return 0
	}
	if n > maxTileRepeats {
		n = maxTileRepeats
	}
	return int(n) - 1
}

func clampSampleCount(v float64, maxN int) int {
	if !(v > 1) {
		return 1
	}
	if v >= float64(maxN) {
		return maxN
	}
	return int(math.Ceil(v))
}

const (
	repeatBoth repeatOp = iota
	repeatX
	repeatY
	repeatNone
)

func (s stops) Len() int { return len(s) }

type stop struct {
	offset float64
	color  color2.Color
}

func (g *radialGradient) ColorAt(x, y int) color2.Color {
	if len(g.stops) == 0 {
		return color2.Transparent
	}
	dx, dy := float64(x)+0.5-g.start.x, float64(y)+0.5-g.start.y
	b := dot3(dx, dy, g.start.r, g.delta.x, g.delta.y, g.delta.r)
	c := dot3(dx, dy, -g.start.r, dx, dy, g.start.r)
	if g.quadA == 0 {
		if b == 0 {
			return color2.Transparent
		}
		t := 0.5 * c / b
		if g.paints(t) {
			return g.colorForExtend(t)
		}
		return color2.Transparent
	}
	discr := dot3(b, g.quadA, 0, b, -c, 0)
	if discr >= 0 {
		root := math.Sqrt(discr)
		t1 := (b + root) * g.invQuadA
		t2 := (b - root) * g.invQuadA
		if g.paints(t1) {
			return g.colorForExtend(t1)
		}
		if g.paints(t2) {
			return g.colorForExtend(t2)
		}
	}
	return color2.Transparent
}
func (s stops) Less(i, j int) bool { return s[i].offset < s[j].offset }

type stops []stop

func (s stops) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

type repeatOp int

func nextPowerOfTwo(n int) int {
	p := 1
	for p < n {
		p *= 2
	}
	return p
}

type linearGradient struct {
	x0, y0, x1, y1         float64
	extendStart, extendEnd bool
	stops                  stops
}

func (g *radialGradient) paints(t float64) bool {
	if !g.radiusValid(t) {
		return false
	}
	if t < 0 && !g.extendStart {
		return false
	}
	if t > 1 && !g.extendEnd {
		return false
	}
	return true
}

func (p *surfacePattern) ColorAt(x, y int) color2.Color {
	bounds := p.im.Bounds()
	switch p.repeat {
	case repeatX:
		if y >= bounds.Dy() {
			return color2.Transparent
		}
	case repeatY:
		if x >= bounds.Dx() {
			return color2.Transparent
		}
	case repeatNone:
		if x >= bounds.Dx() || y >= bounds.Dy() {
			return color2.Transparent
		}
	}
	x = x%bounds.Dx() + bounds.Min.X
	y = y%bounds.Dy() + bounds.Min.Y
	return p.im.At(x, y)
}

func (g *linearGradient) ColorAt(x, y int) color2.Color {
	if len(g.stops) == 0 {
		return color2.Transparent
	}
	px, py := float64(x)+0.5, float64(y)+0.5
	dx, dy := g.x1-g.x0, g.y1-g.y0
	lenSq := dx*dx + dy*dy
	if lenSq == 0 {
		return color2.Transparent
	}
	t := ((px-g.x0)*dx + (py-g.y0)*dy) / lenSq
	if t < 0 {
		if !g.extendStart {
			return color2.Transparent
		}
		return g.stops[0].color
	}
	if t > 1 {
		if !g.extendEnd {
			return color2.Transparent
		}
		return g.stops[len(g.stops)-1].color
	}
	return gradientColorAt(t, g.stops)
}

type patternPainter struct {
	dst       *image.RGBA
	mask      *image.Alpha
	softMask  *image.Alpha
	pattern   context.Pattern
	alpha     uint32
	blendMode context.BlendMode
	backdrop  *image.RGBA
	knockout  bool
}

func newPatternPainter(dst *image.RGBA, mask, softMask *image.Alpha, pattern context.Pattern, alpha uint32, mode context.BlendMode, backdrop *image.RGBA, knockout bool) *patternPainter {
	if backdrop != nil && (!backdrop.Bounds().Eq(dst.Bounds()) || backdrop.Stride != dst.Stride) {
		backdrop = nil
	}
	return &patternPainter{dst, mask, softMask, pattern, alpha, mode, backdrop, knockout}
}

func (p *patternPainter) Paint(ss []raster.Span, done bool) {
	bounds := p.dst.Bounds()
	for _, span := range ss {
		if span.Y < bounds.Min.Y {
			continue
		}
		if span.Y >= bounds.Max.Y {
			return
		}
		if span.X0 < bounds.Min.X {
			span.X0 = bounds.Min.X
		}
		if span.X1 > bounds.Max.X {
			span.X1 = bounds.Max.X
		}
		if span.X0 >= span.X1 {
			continue
		}
		const max16 = 1<<16 - 1
		y := span.Y - p.dst.Rect.Min.Y
		x0 := span.X0 - p.dst.Rect.Min.X
		pix0 := (span.Y-p.dst.Rect.Min.Y)*p.dst.Stride + (span.X0-p.dst.Rect.Min.X)*4
		pixEnd := pix0 + (span.X1-span.X0)*4
		for pi, x := pix0, x0; pi < pixEnd; pi, x = pi+4, x+1 {
			cov := span.Alpha
			if p.mask != nil {
				cov = cov * uint32(p.mask.AlphaAt(x, y).A) / 255
				if cov == 0 {
					continue
				}
			}
			maskCov := cov
			if p.softMask != nil {
				cov = cov * uint32(p.softMask.AlphaAt(x, y).A) / 255
				if cov == 0 {
					continue
				}
			}
			cov = cov * p.alpha / max16
			if cov == 0 {
				continue
			}
			col := p.pattern.ColorAt(x, y)
			r, g, b, a := col.RGBA()
			if a == 0 {
				continue
			}
			if p.knockout || p.blendMode != context.BlendModeNormal {
				var bd []uint8
				if p.backdrop != nil {
					bd = p.backdrop.Pix[pi : pi+4]
				}
				srcA := float64(a) / max16
				sr := float64(r) / float64(a)
				sg := float64(g) / float64(a)
				sb := float64(b) / float64(a)
				if p.knockout {
					var tmp [4]uint8
					blendPixel(tmp[:], bd, p.blendMode, sr, sg, sb, srcA*float64(cov)/float64(maskCov))
					lerpPixel(p.dst.Pix[pi:pi+4], tmp[:], maskCov, max16)
					continue
				}
				blendPixel(p.dst.Pix[pi:pi+4], bd, p.blendMode, sr, sg, sb, srcA*float64(cov)/max16)
				continue
			}
			dr := uint32(p.dst.Pix[pi+0])
			dg := uint32(p.dst.Pix[pi+1])
			db := uint32(p.dst.Pix[pi+2])
			da := uint32(p.dst.Pix[pi+3])
			inv := (max16 - (a * cov / max16)) * 0x101
			p.dst.Pix[pi+0] = uint8((dr*inv + r*cov) / max16 >> 8)
			p.dst.Pix[pi+1] = uint8((dg*inv + g*cov) / max16 >> 8)
			p.dst.Pix[pi+2] = uint8((db*inv + b*cov) / max16 >> 8)
			p.dst.Pix[pi+3] = uint8((da*inv + a*cov) / max16 >> 8)
		}
	}
}

type surfacePattern struct {
	im     image.Image
	repeat repeatOp
}

func gradientColorAt(t float64, s stops) color2.Color {
	if t <= 0.0 || len(s) == 1 {
		return s[0].color
	}
	last := s[len(s)-1]
	if t >= last.offset {
		return last.color
	}
	for i, next := range s[1:] {
		if t < next.offset {
			t = (t - s[i].offset) / (next.offset - s[i].offset)
			return lerpColor(s[i].color, next.color, t)
		}
	}
	return last.color
}

func NewRadialGradient(x0, y0, r0, x1, y1, r1 float64, extendStart, extendEnd bool) context.Gradient {
	c0 := circle{x0, y0, r0}
	c1 := circle{x1, y1, r1}
	cd := circle{x1 - x0, y1 - y0, r1 - r0}
	a := dot3(cd.x, cd.y, -cd.r, cd.x, cd.y, cd.r)
	var invA float64
	if a != 0 {
		invA = 1.0 / a
	}
	negR0 := -c0.r
	g := &radialGradient{start: c0, end: c1, delta: cd, quadA: a, invQuadA: invA, negStartRadius: negR0, extendStart: extendStart, extendEnd: extendEnd}
	return g
}

func (g *radialGradient) AddColorStop(offset float64, color color2.Color) {
	g.stops = append(g.stops, stop{offset: offset, color: color})
	sort.Sort(g.stops)
}

type radialGradient struct {
	start, end, delta      circle
	quadA, invQuadA        float64
	negStartRadius         float64
	extendStart, extendEnd bool
	stops                  stops
}

func (p *tilingPattern) buildTile() {
	nx := tileRepeatCount(p.width, p.xStep)
	ny := tileRepeatCount(p.height, p.yStep)
	if nx == 0 && ny == 0 {
		return
	}
	if (nx+1)*(ny+1) > maxTileRepeats {
		nx = min(nx, maxTileRepeats-1)
		ny = min(ny, maxTileRepeats/(nx+1)-1)
	}
	cellW, cellH := p.cell.Bounds().Dx(), p.cell.Bounds().Dy()
	p.tileW = clampInt(int(math.Ceil(math.Min(p.xStep, p.width)*p.scaleX)), 1, cellW)
	p.tileH = clampInt(int(math.Ceil(math.Min(p.yStep, p.height)*p.scaleY)), 1, cellH)
	tile := image.NewRGBA(image.Rect(0, 0, p.tileW, p.tileH))
	for ty := 0; ty < p.tileH; ty++ {
		fy := (float64(ty) + 0.5) / p.scaleY
		for tx := 0; tx < p.tileW; tx++ {
			fx := (float64(tx) + 0.5) / p.scaleX
			tile.SetRGBA(tx, ty, p.resolve(fx, fy, nx, ny))
		}
	}
	p.tile = tile
	p.cell = nil
}
func newSolidPattern(c color2.Color) context.Pattern { return &solidPattern{color: c} }

var (
	whitePattern = newSolidPattern(color2.White)
	blackPattern = newSolidPattern(color2.Black)
)

func (p *tilingPattern) ColorAt(px, py int) color2.Color {
	x, y := p.deviceToPattern.Transform(float64(px)+0.5, float64(py)+0.5)
	cx := floorMod(x-p.originX, p.xStep)
	cy := floorMod(y-p.originY, p.yStep)
	if cx >= p.width || cy >= p.height {
		return color2.Transparent
	}
	if p.tile != nil {
		ix := min(int(cx*p.scaleX), p.tileW-1)
		iy := min(int(cy*p.scaleY), p.tileH-1)
		return p.tile.RGBAAt(ix, iy)
	}
	if col := sampleCell(p.cell, cx, cy, p.height, p.scaleX, p.scaleY); col.A != 0 {
		return col
	}
	return color2.Transparent
}

func (g *radialGradient) radiusValid(t float64) bool {
	return t*g.delta.r >= g.negStartRadius
}

func (g *linearGradient) AddColorStop(offset float64, color color2.Color) {
	g.stops = append(g.stops, stop{offset: offset, color: color})
	sort.Sort(g.stops)
}

func NewLinearGradient(x0, y0, x1, y1 float64, extendStart, extendEnd bool) context.Gradient {
	g := &linearGradient{x0: x0, y0: y0, x1: x1, y1: y1, extendStart: extendStart, extendEnd: extendEnd}
	return g
}
func (p *solidPattern) ColorAt(x, y int) color2.Color { return p.color }

const maxTileRepeats = 4096

func newSurfacePattern(im image.Image, repeat repeatOp) context.Pattern {
	return &surfacePattern{im: im, repeat: repeat}
}

func NewTilingPattern(cell *image.RGBA, deviceToPattern transform.Matrix, bbox [4]float64, xStep, yStep, sx, sy float64) context.Pattern {
	p := &tilingPattern{deviceToPattern: deviceToPattern, originX: bbox[0], originY: bbox[1], width: bbox[2] - bbox[0], height: bbox[3] - bbox[1], xStep: math.Abs(xStep), yStep: math.Abs(yStep), scaleX: sx, scaleY: sy}
	if p.xStep == 0 {
		p.xStep = p.width
	}
	if p.yStep == 0 {
		p.yStep = p.height
	}
	p.cell = cell
	p.buildTile()
	return p
}

type tilingPattern struct {
	cell             *image.RGBA
	tile             *image.RGBA
	deviceToPattern  transform.Matrix
	originX, originY float64
	width, height    float64
	xStep, yStep     float64
	scaleX, scaleY   float64
	tileW, tileH     int
}

func (p *tilingPattern) resolve(fx, fy float64, nx, ny int) color2.RGBA {
	for i := 0; i <= nx; i++ {
		cx := fx + float64(i)*p.xStep
		if cx >= p.width {
			break
		}
		for j := 0; j <= ny; j++ {
			cy := fy + float64(j)*p.yStep
			if cy >= p.height {
				break
			}
			if col := sampleCell(p.cell, cx, cy, p.height, p.scaleX, p.scaleY); col.A != 0 {
				return col
			}
		}
	}
	return color2.RGBA{}
}

func (g *radialGradient) colorForExtend(t float64) color2.Color {
	if t < 0 {
		return g.stops[0].color
	}
	if t > 1 {
		return g.stops[len(g.stops)-1].color
	}
	return gradientColorAt(t, g.stops)
}
