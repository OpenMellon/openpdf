// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package imagerender

import (
	"container/list"
	"errors"
	"fmt"
	"image"
	color2 "image/color"
	draw2 "image/draw"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/OpenMellon/freetype/raster"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"
)

func (dc *Context) BlendMode() context.BlendMode { return dc.blendMode }
func (dc *Context) MeasureString(s string, face font.Face) (width, height float64) {
	drawer := &font.Drawer{Face: face}
	advance := drawer.MeasureString(s)
	return float64(advance >> 6), dc.textState.Tf.Size
}

func lerpChannel(from, to uint32, t float64) uint8 {
	return uint8(int32(float64(from)*(1.0-t)+float64(to)*t) >> 8)
}

func patchCurvatureSamples(patch *MeshPatch) int {
	p00, p03 := patch.P[0][0], patch.P[0][3]
	p30, p33 := patch.P[3][0], patch.P[3][3]
	var maxDev float64
	for i := range patch.P {
		u := float64(i) / 3
		for j, pt := range patch.P[i] {
			v := float64(j) / 3
			bx := (1-u)*((1-v)*p00.X+v*p03.X) + u*((1-v)*p30.X+v*p33.X)
			by := (1-u)*((1-v)*p00.Y+v*p03.Y) + u*((1-v)*p30.Y+v*p33.Y)
			maxDev = math.Max(maxDev, math.Max(math.Abs(pt.X-bx), math.Abs(pt.Y-by)))
		}
	}
	return clampSampleCount(math.Sqrt(flatnessScale*maxDev/flatnessTolerance), maxSamples)
}
func (dc *Context) Interpolator() draw.Interpolator { return dc.interpolator }
func lerpColor(c0, c1 color2.Color, t float64) color2.Color {
	r0, g0, b0, a0 := c0.RGBA()
	r1, g1, b1, a1 := c1.RGBA()
	return color2.RGBA{lerpChannel(r0, r1, t), lerpChannel(g0, g1, t), lerpChannel(b0, b1, t), lerpChannel(a0, a1, t)}
}

func quadraticPoint(x0, y0, x1, y1, x2, y2, t float64) (x, y float64) {
	mt := 1 - t
	w0 := mt * mt
	w1 := 2 * mt * t
	w2 := t * t
	x = w0*x0 + w1*x1 + w2*x2
	y = w0*y0 + w1*y1 + w2*y2
	return
}

type MeshShadingSampler func(components []float64) (color2.RGBA, bool)

func flattenQuadratic(x0, y0, x1, y1, x2, y2 float64) []transform.Point {
	length := (math.Hypot(x1-x0, y1-y0) + math.Hypot(x2-x1, y2-y1))
	n := max(int(length+0.5), 4)
	last := float64(n) - 1
	pts := make([]transform.Point, n)
	for i := 0; i < n; i++ {
		t := float64(i) / last
		x, y := quadraticPoint(x0, y0, x1, y1, x2, y2, t)
		pts[i] = transform.NewPoint(x, y)
	}
	return pts
}

func dashPolylines(paths [][]transform.Point, dashes []float64, offset float64) [][]transform.Point {
	var result [][]transform.Point
	if len(dashes) == 0 {
		return paths
	}
	if len(dashes) == 1 {
		dashes = append(dashes, dashes[0])
	}
	for _, path := range paths {
		if len(path) < 2 {
			continue
		}
		prev := path[0]
		next := 1
		dashIdx := 0
		dashPos := 0.0
		if offset != 0 {
			var total float64
			for _, d := range dashes {
				total += d
			}
			offset = math.Mod(offset, total)
			if offset < 0 {
				offset += total
			}
			for k, d2 := range dashes {
				offset -= d2
				if offset < 0 {
					dashIdx = k
					dashPos = d2 + offset
					break
				}
			}
		}
		var dash []transform.Point
		dash = append(dash, prev)
		for next < len(path) {
			dashLen := dashes[dashIdx]
			target := path[next]
			segLen := prev.Distance(target)
			remain := dashLen - dashPos
			if segLen > remain {
				frac := remain / segLen
				cut := prev.Interpolate(target, frac)
				dash = append(dash, cut)
				if dashIdx%2 == 0 && len(dash) > 1 {
					result = append(result, dash)
				}
				dash = nil
				dash = append(dash, cut)
				dashPos = 0
				prev = cut
				dashIdx = (dashIdx + 1) % len(dashes)
			} else {
				dash = append(dash, target)
				prev = target
				dashPos += segLen
				next++
			}
		}
		if dashIdx%2 == 0 && len(dash) > 1 {
			result = append(result, dash)
		}
	}
	return result
}

func (dc *Context) SetInterpolator(interpolator draw.Interpolator) { dc.interpolator = interpolator }

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

type solidPattern struct{ color color2.Color }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func (dc *Context) SetDashOffset(offset float64) { dc.dashOffset = offset }

const alpha16Max = 1<<16 - 1

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

func dashPath(path raster.Path, dashes []float64, offset float64) raster.Path {
	return polylinesToPath(dashPolylines(pathToPolylines(path), dashes, offset))
}

func (dc *Context) DrawRectangle(x, y, w, h float64) {
	dc.NewSubPath()
	dc.MoveTo(x, y)
	dc.LineTo(x+w, y)
	dc.LineTo(x+w, y+h)
	dc.LineTo(x, y+h)
	dc.ClosePath()
}

func (dc *Context) AsMask() *image.Alpha {
	mask := image.NewAlpha(dc.img.Bounds())
	draw.Draw(mask, dc.img.Bounds(), dc.img, image.Point{}, draw.Src)
	return mask
}
func (dc *Context) Identity() { dc.matrix = transform.IdentityMatrix() }
func (dc *Context) Clip()     { dc.ClipPreserve(); dc.ClearPath() }
func (dc *Context) NewSubPath() {
	if dc.hasCurrent {
		dc.fillPath.Add1(toFixedPoint(dc.start))
	}
	dc.hasCurrent = false
}
func (dc *Context) SetGlyphCache(c *GlyphCache) { dc.glyphCache = c }
func setLum(c [3]float64, l float64) [3]float64 {
	d := l - lum(c)
	c[0] += d
	c[1] += d
	c[2] += d
	return clipColor(c)
}

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

const epsilon = 1e-9

func (dc *Context) SetRGB255(r, g, b int) { dc.SetRGBA255(r, g, b, 255) }
func clampSampleCount(v float64, maxN int) int {
	if !(v > 1) {
		return 1
	}
	if v >= float64(maxN) {
		return maxN
	}
	return int(math.Ceil(v))
}

func (gc *GlyphCache) lookup(fontID uint64, face font.Face, pos fixed.Point26_6, r rune) (image.Rectangle, *image.Alpha, image.Point, fixed.Int26_6, bool) {
	frac := int(pos.X) & 63
	subpixel := uint8(frac * glyphSubpixelSteps >> 6)
	key := glyphKey{fontID: fontID, r: r, subpixel: subpixel}
	origin := image.Pt(int(pos.X>>6), int(pos.Y>>6))
	if elem, hit := gc.entries[key]; hit {
		gc.lru.MoveToFront(elem)
		entry := elem.Value.(*glyphEntry)
		return entry.bounds.Add(origin), entry.mask, image.Point{}, entry.advance, true
	}
	subX := fixed.Int26_6((int(subpixel)*2 + 1) * 64 / (2 * glyphSubpixelSteps))
	bounds, glyphMask, maskPt, advance, ok := face.Glyph(fixed.Point26_6{X: subX}, r)
	if !ok {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	mask := copyToAlpha(glyphMask, maskPt, bounds)
	if size := len(mask.Pix); size <= gc.maxBytes {
		newEntry := &glyphEntry{key: key, bounds: bounds, mask: mask, advance: advance, size: size}
		gc.entries[key] = gc.lru.PushFront(newEntry)
		gc.usedBytes += size
		for gc.usedBytes > gc.maxBytes {
			back := gc.lru.Back()
			if back == nil {
				break
			}
			oldEntry := back.Value.(*glyphEntry)
			delete(gc.entries, oldEntry.key)
			gc.lru.Remove(back)
			gc.usedBytes -= oldEntry.size
		}
	}
	return bounds.Add(origin), mask, image.Point{}, advance, true
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

func (dc *Context) StrokePreserve() {
	var painter raster.Painter
	alpha := paintAlpha16(dc.strokePattern, dc.strokeAlpha, dc.colorLocked)
	if dc.mask == nil && dc.softMask == nil && alpha == alpha16Max && dc.blendMode == context.BlendModeNormal && !dc.knockout {
		if solid, ok := dc.strokePattern.(*solidPattern); ok {
			rgbaPainter := raster.NewRGBAPainter(dc.img)
			rgbaPainter.SetColor(solid.color)
			painter = rgbaPainter
		}
	}
	if painter == nil {
		painter = newPatternPainter(dc.img, dc.mask, dc.softMask, dc.strokePattern, alpha, dc.blendMode, dc.backdrop, dc.knockout)
	}
	dc.stroke(painter)
}

const (
	repeatBoth repeatOp = iota
	repeatX
	repeatY
	repeatNone
)

func (s stops) Len() int                     { return len(s) }
func (dc *Context) Matrix() transform.Matrix { return dc.matrix }
func (dc *Context) Image() image.Image       { return dc.img }
func (dc *Context) FillAlpha() float64       { return dc.fillAlpha }
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

func meshTrianglesBounds(tris []MeshTriangle) image.Rectangle {
	var bounds image.Rectangle
	for i := range tris {
		bounds = bounds.Union(triangleBounds(&tris[i]))
	}
	return bounds
}

type stop struct {
	offset float64
	color  color2.Color
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
func (dc *Context) Fill() { dc.FillPreserve(); dc.ClearPath() }
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
func (dc *Context) SetLineJoin(lineJoin context.LineJoin) { dc.lineJoin = lineJoin }
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

func patchSampleCount(patch *MeshPatch) int {
	n := patchSizeSamples(patch)
	if curvature := patchCurvatureSamples(patch); curvature > n {
		n = curvature
	}
	if color := patchColorSamples(patch); color > n {
		n = color
	}
	return n
}

func (e *patchEdge) sampleCount() int {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, pt := range e.points {
		minX, maxX = math.Min(minX, pt.X), math.Max(maxX, pt.X)
		minY, maxY = math.Min(minY, pt.Y), math.Max(maxY, pt.Y)
	}
	var maxDev float64
	for i := 1; i < 3; i++ {
		t := float64(i) / 3
		lx := (1-t)*e.points[0].X + t*e.points[3].X
		ly := (1-t)*e.points[0].Y + t*e.points[3].Y
		maxDev = math.Max(maxDev, math.Max(math.Abs(e.points[i].X-lx), math.Abs(e.points[i].Y-ly)))
	}
	n := clampSampleCount(math.Max(maxX-minX, maxY-minY)/pixelsPerSample, maxSamples)
	if curveN := clampSampleCount(math.Sqrt(flatnessScale*maxDev/flatnessTolerance), maxSamples); curveN > n {
		n = curveN
	}
	return nextPowerOfTwo(n)
}
func (dc *Context) Stroke() { dc.StrokePreserve(); dc.ClearPath() }
func rasterizeTriangle(img *image.RGBA, tri *MeshTriangle, sample MeshShadingSampler, comps []float64) {
	x0, y0 := tri[0].X, tri[0].Y
	x1, y1 := tri[1].X, tri[1].Y
	x2, y2 := tri[2].X, tri[2].Y
	area := (x1-x0)*(y2-y0) - (x2-x0)*(y1-y0)
	if area == 0 {
		return
	}
	bounds := triangleBounds(tri).Intersect(img.Rect)
	if bounds.Empty() {
		return
	}
	c0, c1, c2 := tri[0].Components, tri[1].Components, tri[2].Components
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		fy := float64(y) + 0.5
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			fx := float64(x) + 0.5
			w0 := ((x1-fx)*(y2-fy) - (x2-fx)*(y1-fy)) / area
			w1 := ((x2-fx)*(y0-fy) - (x0-fx)*(y2-fy)) / area
			w2 := 1 - w0 - w1
			if w0 < 0 || w1 < 0 || w2 < 0 {
				continue
			}
			for k := range comps {
				comps[k] = w0*c0[k] + w1*c1[k] + w2*c2[k]
			}
			col, ok := sample(comps)
			if !ok {
				continue
			}
			off := img.PixOffset(x, y)
			img.Pix[off+0] = col.R
			img.Pix[off+1] = col.G
			img.Pix[off+2] = col.B
			img.Pix[off+3] = col.A
		}
	}
}
func (dc *Context) SetLineWidth(lineWidth float64) { dc.lineWidth = lineWidth }
func TessellatePatch(patch *MeshPatch, out []MeshTriangle) []MeshTriangle {
	edges := patchEdges(patch)
	n := patchSampleCount(patch)
	for _, edge := range edges {
		if edge.samples > n {
			n = edge.samples
		}
	}
	n = nextPowerOfTwo(n)
	bern := bernsteinTable(n)
	nc := len(patch.Corners[0][0])
	comps := make([]float64, (n+1)*(n+1)*nc)
	verts := make([]MeshVertex, (n+1)*(n+1))
	for i := 0; i <= n; i++ {
		u := float64(i) / float64(n)
		for j := 0; j <= n; j++ {
			v := float64(j) / float64(n)
			var x, y float64
			switch {
			case i == 0:
				x, y = edges[0].at(v)
			case i == n:
				x, y = edges[1].at(v)
			case j == 0:
				x, y = edges[2].at(u)
			case j == n:
				x, y = edges[3].at(u)
			default:
				for a := 0; a < 4; a++ {
					for b := 0; b < 4; b++ {
						w := bern[i][a] * bern[j][b]
						x += w * patch.P[a][b].X
						y += w * patch.P[a][b].Y
					}
				}
			}
			off := (i*(n+1) + j) * nc
			vc := comps[off : off+nc : off+nc]
			for k := range vc {
				vc[k] = (1-u)*((1-v)*patch.Corners[0][0][k]+v*patch.Corners[0][1][k]) + u*((1-v)*patch.Corners[1][0][k]+v*patch.Corners[1][1][k])
			}
			verts[i*(n+1)+j] = MeshVertex{X: x, Y: y, Components: vc}
		}
	}
	for row := 0; row < n; row++ {
		for col := 0; col < n; col++ {
			v00 := verts[row*(n+1)+col]
			v01 := verts[row*(n+1)+col+1]
			v10 := verts[(row+1)*(n+1)+col]
			v11 := verts[(row+1)*(n+1)+col+1]
			out = append(out, MeshTriangle{v00, v10, v01}, MeshTriangle{v10, v11, v01})
		}
	}
	return out
}

func floorMod(x, m float64) float64 {
	x = math.Mod(x, m)
	if x < 0 {
		x += m
	}
	if x >= m {
		return 0
	}
	return x
}

func (dc *Context) DrawStringAnchored(s string, face font.Face, x, y, ax, ay float64) {
	w, h := dc.MeasureString(s, face)
	dc.drawString(s, face, x-ax*w, y+ay*h)
}
func (s stops) Less(i, j int) bool               { return s[i].offset < s[j].offset }
func (dc *Context) SetMatrix(m transform.Matrix) { dc.matrix = m }
func (dc *Context) fill(painter raster.Painter) {
	path := dc.fillPath
	if dc.hasCurrent {
		path = make(raster.Path, len(dc.fillPath))
		copy(path, dc.fillPath)
		path.Add1(toFixedPoint(dc.start))
	}
	rast := dc.rasterizer
	rast.UseNonZeroWinding = dc.fillRule == context.FillRuleWinding
	rast.Clear()
	rast.AddPath(path)
	rast.Rasterize(painter)
}

type stops []stop

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

type GlyphCache struct {
	enabled   bool
	maxBytes  int
	usedBytes int
	lru       *list.List
	entries   map[glyphKey]*list.Element
}

func (s stops) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (dc *Context) DrawEllipticalArc(x, y, rx, ry, angle1, angle2 float64) {
	const segments = 16
	for i := 0; i < segments; i++ {
		f0 := float64(i+0) / segments
		f1 := float64(i+1) / segments
		a1 := angle1 + (angle2-angle1)*f0
		a2 := angle1 + (angle2-angle1)*f1
		x1 := x + rx*math.Cos(a1)
		y1 := y + ry*math.Sin(a1)
		xm := x + rx*math.Cos((a1+a2)/2)
		ym := y + ry*math.Sin((a1+a2)/2)
		x2 := x + rx*math.Cos(a2)
		y2 := y + ry*math.Sin(a2)
		cx := 2*xm - x1/2 - x2/2
		cy := 2*ym - y1/2 - y2/2
		if i == 0 {
			if dc.hasCurrent {
				dc.LineTo(x1, y1)
			} else {
				dc.MoveTo(x1, y1)
			}
		}
		dc.QuadraticTo(cx, cy, x2, y2)
	}
}

func (dc *Context) DrawString(s string, face font.Face, x, y float64) {
	dc.DrawStringAnchored(s, face, x, y, 0, 0)
}

type repeatOp int

func toFixed(v float64) fixed.Int26_6 {
	v = precision.RoundDefault(v)
	return fixed.Int26_6(v * 64)
}

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
func (dc *Context) SetDash(dashes ...float64) { dc.dashes = dashes }

const defaultGlyphCacheBytes = 64 << 20

func (dc *Context) DrawCircle(x, y, r float64) {
	dc.NewSubPath()
	dc.DrawEllipticalArc(x, y, r, r, 0, 2*math.Pi)
	dc.ClosePath()
}

func (dc *Context) drawStringFast(s string, face font.Face, pos fixed.Point26_6) {
	m := dc.matrix
	flipped := m[4] < 0
	xShift := fixed.Int26_6(math.Round(m[6] * 64))
	baseY := int(math.Round(fromFixed(pos.Y)*m[4] + m[7]))
	r, g, b, a := dc.color.RGBA()
	fontID, cacheable := dc.glyphFontID(face)
	prev := rune(-1)
	for _, ch := range s {
		if prev >= 0 {
			pos.X += face.Kern(prev, ch)
		}
		bounds, mask, maskPt, advance, ok := dc.fastGlyph(cacheable, fontID, face, fixed.Point26_6{X: pos.X + xShift}, ch)
		if !ok {
			continue
		}
		dx := bounds.Min.X
		dy := bounds.Min.Y + baseY
		if flipped {
			dy = baseY - bounds.Max.Y
		}
		dc.blitGlyphMask(mask, maskPt, bounds.Dx(), bounds.Dy(), dx, dy, r, g, b, a, flipped)
		pos.X += advance
		prev = ch
	}
}
func (dc *Context) Rotate(angle float64) { dc.matrix = dc.matrix.Rotate(angle) }
func (dc *Context) RestorePath() {
	n := len(dc.pathStack)
	if n == 0 {
		return
	}
	snap := dc.pathStack[n-1]
	dc.pathStack = dc.pathStack[:n-1]
	dc.path = snap.path
	dc.fillPath = snap.fillPath
	dc.start = snap.start
	dc.current = snap.current
	dc.hasCurrent = snap.hasCurrent
}

func (dc *Context) setFillAndStrokeColor(col color2.Color) {
	if dc.colorLocked {
		return
	}
	dc.color = col
	dc.fillPattern = newSolidPattern(col)
	dc.strokePattern = newSolidPattern(col)
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

func (dc *Context) DrawImageAnchored(im image.Image, x, y int, ax, ay float64) {
	size := im.Bounds().Size()
	x -= int(ax * float64(size.X))
	y -= int(ay * float64(size.Y))
	m := dc.matrix.Clone().Translate(float64(x), float64(y))
	aff := f64.Aff3{m[0], m[3], m[6], m[1], m[4], m[7]}
	var mask, softMask *image.Alpha
	if dc.knockout {
		mask, softMask = dc.mask, dc.softMask
	} else {
		mask = dc.effectiveMask()
	}
	var opts *draw.Options
	if mask != nil {
		opts = &draw.Options{DstMask: mask, DstMaskP: image.Point{}}
	}
	if dc.blendMode != context.BlendModeNormal || dc.fillAlpha < 1 || dc.knockout {
		bounds := transformedBounds(aff, im.Bounds()).Intersect(dc.img.Bounds())
		if bounds.Empty() {
			return
		}
		tmp := image.NewRGBA(bounds)
		dc.interpolator.Transform(tmp, aff, im, im.Bounds(), draw.Over, opts)
		compositeBlend(dc.img, tmp, dc.backdrop, dc.blendMode, dc.fillAlpha, softMask, dc.knockout)
		return
	}
	dc.interpolator.Transform(dc.img, aff, im, im.Bounds(), draw.Over, opts)
}

func (dc *Context) SetFillStyle(pattern context.Pattern) {
	if dc.colorLocked {
		return
	}
	if solid, ok := pattern.(*solidPattern); ok {
		dc.color = solid.color
	}
	dc.fillPattern = pattern
}

func triangleBounds(tri *MeshTriangle) image.Rectangle {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, v := range tri {
		minX, maxX = math.Min(minX, v.X), math.Max(maxX, v.X)
		minY, maxY = math.Min(minY, v.Y), math.Max(maxY, v.Y)
	}
	return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY)))
}

func (dc *Context) stroke(painter raster.Painter) {
	path := dc.path
	if len(dc.dashes) > 0 {
		path = dashPath(path, dc.dashes, dc.dashOffset)
	} else {
		path = polylinesToPath(pathToPolylines(path))
	}
	rast := dc.rasterizer
	rast.UseNonZeroWinding = true
	rast.Clear()
	scale := (dc.matrix.ScalingFactorX() + dc.matrix.ScalingFactorY()) / 2
	rast.AddStroke(path, toFixed(dc.lineWidth*scale), dc.capper(), dc.joiner())
	rast.Rasterize(painter)
}

func (dc *Context) QuadraticTo(x1, y1, x2, y2 float64) {
	if !dc.hasCurrent {
		dc.MoveTo(x1, y1)
	}
	x1, y1 = dc.Transform(x1, y1)
	x2, y2 = dc.Transform(x2, y2)
	p1 := transform.NewPoint(x1, y1)
	p2 := transform.NewPoint(x2, y2)
	f1 := toFixedPoint(p1)
	f2 := toFixedPoint(p2)
	dc.path.Add2(f1, f2)
	dc.fillPath.Add2(f1, f2)
	dc.current = p2
}
func degreesToRadians(deg float64) float64 { return deg * math.Pi / 180 }
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
func (dc *Context) SetBlendMode(mode context.BlendMode) { dc.blendMode = mode }
func NewContextForRGBA(im *image.RGBA) *Context {
	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	return &Context{width: w, height: h, rasterizer: raster.NewRasterizer(w, h), img: im, baseImg: im, color: color2.Transparent, fillPattern: whitePattern, strokePattern: blackPattern, lineWidth: 1, fillRule: context.FillRuleWinding, matrix: transform.IdentityMatrix(), textState: context.NewTextState(), interpolator: draw.BiLinear, blendMode: context.BlendModeNormal, fillAlpha: 1, strokeAlpha: 1}
}

func dot3(ax, ay, az, bx, by, bz float64) float64 {
	return ax*bx + ay*by + az*bz
}
func (dc *Context) TextState() *context.TextState { return &dc.textState }
func (dc *Context) StrokeAlpha() float64          { return dc.strokeAlpha }
func (dc *Context) FillPattern() context.Pattern  { return dc.fillPattern }
func (e *patchEdge) at(t float64) (x, y float64) {
	ft := t * float64(e.samples)
	i := int(ft)
	if i >= e.samples {
		i = e.samples - 1
	}
	frac := ft - float64(i)
	x0, y0 := e.point(float64(i) / float64(e.samples))
	x1, y1 := e.point(float64(i+1) / float64(e.samples))
	return x0 + frac*(x1-x0), y0 + frac*(y1-y0)
}

func fromFixed(v fixed.Int26_6) float64 {
	const fracBits, fracMask = 6, 1<<6 - 1
	if v >= 0 {
		return float64(v>>fracBits) + float64(v&fracMask)/64
	}
	v = -v
	if v >= 0 {
		return -(float64(v>>fracBits) + float64(v&fracMask)/64)
	}
	return 0
}

func (dc *Context) joiner() raster.Joiner {
	switch dc.lineJoin {
	case context.LineJoinBevel:
		return raster.BevelJoiner
	case context.LineJoinRound:
		return raster.RoundJoiner
	}
	return nil
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

func paintAlpha16(pattern context.Pattern, alpha float64, colorLocked bool) uint32 {
	if _, isSolid := pattern.(*solidPattern); isSolid && !colorLocked {
		return alpha16Max
	}
	return uint32(alpha*alpha16Max + 0.5)
}
func (dc *Context) SetColor(c color2.Color) { dc.setFillAndStrokeColor(c) }
func patchExtent(patch *MeshPatch) float64 {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for i := range patch.P {
		for _, pt := range patch.P[i] {
			minX, maxX = math.Min(minX, pt.X), math.Max(maxX, pt.X)
			minY, maxY = math.Min(minY, pt.Y), math.Max(maxY, pt.Y)
		}
	}
	return math.Max(maxX-minX, maxY-minY)
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

type functionShading struct {
	deviceToShading transform.Matrix
	domain          [4]float64
	sample          FunctionShadingSampler
}

const glyphSubpixelSteps = 4

func (dc *Context) CubicTo(x1, y1, x2, y2, x3, y3 float64) {
	if !dc.hasCurrent {
		dc.MoveTo(x1, y1)
	}
	x0, y0 := dc.current.X, dc.current.Y
	x1, y1 = dc.Transform(x1, y1)
	x2, y2 = dc.Transform(x2, y2)
	x3, y3 = dc.Transform(x3, y3)
	pts := flattenCubic(x0, y0, x1, y1, x2, y2, x3, y3)
	last := toFixedPoint(dc.current)
	for _, pt := range pts[1:] {
		fp := toFixedPoint(pt)
		if fp == last {
			continue
		}
		last = fp
		dc.path.Add1(fp)
		dc.fillPath.Add1(fp)
		dc.current = pt
	}
}
func (dc *Context) SetStrokeAlpha(a float64) { dc.strokeAlpha = clamp01(a) }
func NewGlyphCache() *GlyphCache {
	return &GlyphCache{enabled: glyphCacheEnabled, maxBytes: defaultGlyphCacheBytes, lru: list.New(), entries: make(map[glyphKey]*list.Element)}
}

func sat(c [3]float64) float64 {
	return math.Max(c[0], math.Max(c[1], c[2])) - math.Min(c[0], math.Min(c[1], c[2]))
}

func (dc *Context) LineTo(x, y float64) {
	if !dc.hasCurrent {
		dc.MoveTo(x, y)
	} else {
		x, y = dc.Transform(x, y)
		p := transform.NewPoint(x, y)
		fp := toFixedPoint(p)
		dc.path.Add1(fp)
		dc.fillPath.Add1(fp)
		dc.current = p
	}
}

func sampleCell(cell *image.RGBA, x, y, height, sx, sy float64) color2.RGBA {
	bounds := cell.Bounds()
	px := bounds.Min.X + int(x*sx)
	py := bounds.Min.Y + int((height-y)*sy)
	if px >= bounds.Max.X {
		px = bounds.Max.X - 1
	}
	if py >= bounds.Max.Y {
		py = bounds.Max.Y - 1
	}
	if py < bounds.Min.Y {
		py = bounds.Min.Y
	}
	return cell.RGBAAt(px, py)
}

type surfacePattern struct {
	im     image.Image
	repeat repeatOp
}

func toFixedPoint(p transform.Point) fixed.Point26_6 {
	return fixed.Point26_6{X: toFixed(p.X), Y: toFixed(p.Y)}
}
func (dc *Context) SetRGB(r, g, b float64) { dc.SetRGBA(r, g, b, 1) }
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

func (dc *Context) ClosePath() {
	if dc.hasCurrent {
		fp := toFixedPoint(dc.start)
		dc.path.Add1(fp)
		dc.fillPath.Add1(fp)
		dc.current = dc.start
	}
}

func convertColors(img *image.RGBA, convert context.ColorConverter) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		i := img.PixOffset(bounds.Min.X, y)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			a := img.Pix[i+3]
			if a != 0 {
				r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
				if a != 0xff {
					r = unpremultiply(r, a)
					g = unpremultiply(g, a)
					b = unpremultiply(b, a)
				}
				nr, ng, nb := convert(r, g, b)
				if a != 0xff {
					nr = uint8((uint32(nr)*uint32(a) + 127) / 255)
					ng = uint8((uint32(ng)*uint32(a) + 127) / 255)
					nb = uint8((uint32(nb)*uint32(a) + 127) / 255)
				}
				img.Pix[i], img.Pix[i+1], img.Pix[i+2] = nr, ng, nb
			}
			i += 4
		}
	}
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

func toRGBA(im image.Image) *image.RGBA {
	bounds := im.Bounds()
	dst := image.NewRGBA(bounds)
	draw2.Draw(dst, bounds, im, bounds.Min, draw2.Src)
	return dst
}

func (dc *Context) DrawArc(x, y, r, angle1, angle2 float64) {
	dc.DrawEllipticalArc(x, y, r, r, angle1, angle2)
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

type FunctionShadingSampler func(x, y float64) (color2.Color, bool)

func (dc *Context) Scale(x, y float64) { dc.matrix = dc.matrix.Scale(x, y) }

var glyphCacheEnabled = os.Getenv("OPENPDF_GLYPH_CACHE") != "0"

func (dc *Context) GlyphCache() *GlyphCache { return dc.glyphCache }

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

func (dc *Context) SetPixel(x, y int) { dc.img.Set(x, y, dc.color) }
func pathToPolylines(path raster.Path) [][]transform.Point {
	var polys [][]transform.Point
	var cur []transform.Point
	var cx, cy float64
	for i := 0; i < len(path); {
		switch path[i] {
		case 0:
			if len(cur) > 0 {
				polys = append(polys, cur)
				cur = nil
			}
			x := fromFixed(path[i+1])
			y := fromFixed(path[i+2])
			cur = append(cur, transform.NewPoint(x, y))
			cx, cy = x, y
			i += 4
		case 1:
			x := fromFixed(path[i+1])
			y := fromFixed(path[i+2])
			cur = append(cur, transform.NewPoint(x, y))
			cx, cy = x, y
			i += 4
		case 2:
			x1 := fromFixed(path[i+1])
			y1 := fromFixed(path[i+2])
			x2 := fromFixed(path[i+3])
			y2 := fromFixed(path[i+4])
			pts := flattenQuadratic(cx, cy, x1, y1, x2, y2)
			cur = append(cur, pts...)
			cx, cy = x2, y2
			i += 6
		case 3:
			x1 := fromFixed(path[i+1])
			y1 := fromFixed(path[i+2])
			x2 := fromFixed(path[i+3])
			y2 := fromFixed(path[i+4])
			x3 := fromFixed(path[i+5])
			y3 := fromFixed(path[i+6])
			pts := flattenCubic(cx, cy, x1, y1, x2, y2, x3, y3)
			cur = append(cur, pts...)
			cx, cy = x3, y3
			i += 8
		default:
			common.Log.Debug("WARN: invalid path: %v", path)
			return polys
		}
	}
	if len(cur) > 0 {
		polys = append(polys, cur)
	}
	return polys
}

func (g *radialGradient) AddColorStop(offset float64, color color2.Color) {
	g.stops = append(g.stops, stop{offset: offset, color: color})
	sort.Sort(g.stops)
}

func (dc *Context) DrawEllipse(x, y, rx, ry float64) {
	dc.NewSubPath()
	dc.DrawEllipticalArc(x, y, rx, ry, 0, 2*math.Pi)
	dc.ClosePath()
}

func (e *patchEdge) point(t float64) (x, y float64) {
	mt := 1 - t
	bern := [4]float64{mt * mt * mt, 3 * mt * mt * t, 3 * mt * t * t, t * t * t}
	for i, pt := range e.points {
		x += bern[i] * pt.X
		y += bern[i] * pt.Y
	}
	return x, y
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

type pathSnapshot struct {
	path       raster.Path
	fillPath   raster.Path
	start      transform.Point
	current    transform.Point
	hasCurrent bool
}

func (dc *Context) StrokePattern() context.Pattern { return dc.strokePattern }
func NewContext(width, height int) *Context {
	return NewContextForRGBA(image.NewRGBA(image.Rect(0, 0, width, height)))
}

func (dc *Context) FillPreserve() {
	var painter raster.Painter
	alpha := paintAlpha16(dc.fillPattern, dc.fillAlpha, dc.colorLocked)
	if dc.mask == nil && dc.softMask == nil && alpha == alpha16Max && dc.blendMode == context.BlendModeNormal && !dc.knockout {
		if solid, ok := dc.fillPattern.(*solidPattern); ok {
			rgbaPainter := raster.NewRGBAPainter(dc.img)
			rgbaPainter.SetColor(solid.color)
			painter = rgbaPainter
		}
	}
	if painter == nil {
		painter = newPatternPainter(dc.img, dc.mask, dc.softMask, dc.fillPattern, alpha, dc.blendMode, dc.backdrop, dc.knockout)
	}
	dc.fill(painter)
}

func (dc *Context) DrawRoundedRectangle(x, y, w, h, r float64) {
	x0, x1, x2, x3 := x, x+r, x+w-r, x+w
	y0, y1, y2, y3 := y, y+r, y+h-r, y+h
	dc.NewSubPath()
	dc.MoveTo(x1, y0)
	dc.LineTo(x2, y0)
	dc.DrawArc(x2, y1, r, degreesToRadians(270), degreesToRadians(360))
	dc.LineTo(x3, y2)
	dc.DrawArc(x2, y2, r, degreesToRadians(0), degreesToRadians(90))
	dc.LineTo(x1, y3)
	dc.DrawArc(x1, y2, r, degreesToRadians(90), degreesToRadians(180))
	dc.LineTo(x0, y1)
	dc.DrawArc(x1, y1, r, degreesToRadians(180), degreesToRadians(270))
	dc.ClosePath()
}

func (dc *Context) drawString(s string, face font.Face, x, y float64) {
	pos := toFixedPoint(transform.NewPoint(x, y))
	if dc.softMask == nil && dc.blendMode == context.BlendModeNormal && !dc.knockout && canUseFastGlyphPath(dc.matrix) {
		dc.drawStringFast(s, face, pos)
		return
	}
	dc.drawStringSlow(s, face, pos)
}

func cubicPoint(x0, y0, x1, y1, x2, y2, x3, y3, t float64) (x, y float64) {
	mt := 1 - t
	w0 := mt * mt * mt
	w1 := 3 * mt * mt * t
	w2 := 3 * mt * t * t
	w3 := t * t * t
	x = w0*x0 + w1*x1 + w2*x2 + w3*x3
	y = w0*y0 + w1*y1 + w2*y2 + w3*y3
	return
}

func (dc *Context) SetRGBA255(r, g, b, a int) {
	if dc.colorLocked {
		return
	}
	dc.color = color2.NRGBA{uint8(r), uint8(g), uint8(b), uint8(a)}
	dc.setFillAndStrokeColor(dc.color)
}

func bernsteinTable(n int) [][4]float64 {
	table := make([][4]float64, n+1)
	for i := range table {
		t := float64(i) / float64(n)
		mt := 1 - t
		table[i] = [4]float64{mt * mt * mt, 3 * mt * mt * t, 3 * mt * t * t, t * t * t}
	}
	return table
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

type radialGradient struct {
	start, end, delta      circle
	quadA, invQuadA        float64
	negStartRadius         float64
	extendStart, extendEnd bool
	stops                  stops
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

func NewFunctionShading(deviceToShading transform.Matrix, domain [4]float64, sample FunctionShadingSampler) context.Pattern {
	return &functionShading{deviceToShading: deviceToShading, domain: domain, sample: sample}
}

type MeshPatch struct {
	P              [4][4]transform.Point
	Corners        [2][2][]float64
	ColorTolerance float64
}

const (
	defaultColorTolerance = 1.0 / 255
	defaultColorSamples   = 8
)

func polylinesToPath(polys [][]transform.Point) raster.Path {
	var path raster.Path
	for _, poly := range polys {
		var prev fixed.Point26_6
		for i, pt := range poly {
			fp := toFixedPoint(pt)
			if i == 0 {
				path.Start(fp)
			} else {
				dx := fp.X - prev.X
				dy := fp.Y - prev.Y
				if dx < 0 {
					dx = -dx
				}
				if dy < 0 {
					dy = -dy
				}
				if dx+dy > 8 {
					path.Add1(fp)
				}
			}
			prev = fp
		}
	}
	return path
}

func flattenCubic(x0, y0, x1, y1, x2, y2, x3, y3 float64) []transform.Point {
	length := (math.Hypot(x1-x0, y1-y0) + math.Hypot(x2-x1, y2-y1) + math.Hypot(x3-x2, y3-y2))
	n := max(int(length+0.5), 4)
	last := float64(n) - 1
	pts := make([]transform.Point, n)
	for i := 0; i < n; i++ {
		t := float64(i) / last
		x, y := cubicPoint(x0, y0, x1, y1, x2, y2, x3, y3, t)
		pts[i] = transform.NewPoint(x, y)
	}
	return pts
}

func (dc *Context) fastGlyph(cached bool, fontID uint64, face font.Face, pos fixed.Point26_6, r rune) (image.Rectangle, *image.Alpha, image.Point, fixed.Int26_6, bool) {
	if cached {
		return dc.glyphCache.lookup(fontID, face, pos, r)
	}
	bounds, glyphMask, maskPt, advance, ok := face.Glyph(pos, r)
	if !ok {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	if alpha, isAlpha := glyphMask.(*image.Alpha); isAlpha {
		return bounds, alpha, maskPt, advance, true
	}
	return bounds, copyToAlpha(glyphMask, maskPt, bounds), image.Point{}, advance, true
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

func (dc *Context) SetStrokeStyle(pattern context.Pattern) {
	if dc.colorLocked {
		return
	}
	dc.strokePattern = pattern
}
func (dc *Context) Shear(x, y float64) { dc.matrix.Shear(x, y) }
func alphaBounds(mask *image.Alpha) image.Rectangle {
	bounds := mask.Bounds()
	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := mask.Pix[(y-bounds.Min.Y)*mask.Stride : (y-bounds.Min.Y)*mask.Stride+bounds.Dx()]
		for i, v := range row {
			if v == 0 {
				continue
			}
			x := bounds.Min.X + i
			if x < minX {
				minX = x
			}
			if x+1 > maxX {
				maxX = x + 1
			}
			if y < minY {
				minY = y
			}
			if y+1 > maxY {
				maxY = y + 1
			}
		}
	}
	if minX >= maxX || minY >= maxY {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

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
func (dc *Context) LockColor()                       { dc.colorLocked = true }
func newSolidPattern(c color2.Color) context.Pattern { return &solidPattern{color: c} }
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

func (dc *Context) SetSoftMask(mask *image.Alpha) {
	if mask != nil && mask.Bounds().Size() != dc.img.Bounds().Size() {
		return
	}
	dc.softMask = mask
}

func (dc *Context) SavePath() {
	dc.pathStack = append(dc.pathStack, pathSnapshot{path: append(raster.Path(nil), dc.path...), fillPath: append(raster.Path(nil), dc.fillPath...), start: dc.start, current: dc.current, hasCurrent: dc.hasCurrent})
}

type MeshTriangle [3]MeshVertex

func (dc *Context) SetLineCap(lineCap context.LineCap) { dc.lineCap = lineCap }
func patchEdges(patch *MeshPatch) [4]patchEdge {
	edges := [4]patchEdge{{points: [4]transform.Point{patch.P[0][0], patch.P[0][1], patch.P[0][2], patch.P[0][3]}}, {points: [4]transform.Point{patch.P[3][0], patch.P[3][1], patch.P[3][2], patch.P[3][3]}}, {points: [4]transform.Point{patch.P[0][0], patch.P[1][0], patch.P[2][0], patch.P[3][0]}}, {points: [4]transform.Point{patch.P[0][3], patch.P[1][3], patch.P[2][3], patch.P[3][3]}}}
	for i := range edges {
		edges[i].samples = edges[i].sampleCount()
	}
	return edges
}
func (dc *Context) LineWidth() float64 { return dc.lineWidth }
func (g *radialGradient) radiusValid(t float64) bool {
	return t*g.delta.r >= g.negStartRadius
}

type patchEdge struct {
	points  [4]transform.Point
	samples int
}

func (g *linearGradient) AddColorStop(offset float64, color color2.Color) {
	g.stops = append(g.stops, stop{offset: offset, color: color})
	sort.Sort(g.stops)
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

func NewLinearGradient(x0, y0, x1, y1 float64, extendStart, extendEnd bool) context.Gradient {
	g := &linearGradient{x0: x0, y0: y0, x1: x1, y1: y1, extendStart: extendStart, extendEnd: extendEnd}
	return g
}

func (dc *Context) MoveTo(x, y float64) {
	if dc.hasCurrent {
		dc.fillPath.Add1(toFixedPoint(dc.start))
	}
	x, y = dc.Transform(x, y)
	p := transform.NewPoint(x, y)
	fp := toFixedPoint(p)
	dc.path.Start(fp)
	dc.fillPath.Start(fp)
	dc.start = p
	dc.current = p
	dc.hasCurrent = true
}
func (dc *Context) SetFillAlpha(a float64) { dc.fillAlpha = clamp01(a) }
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

const (
	flatnessScale     = 0.75
	flatnessTolerance = 0.5
)

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
func (p *solidPattern) ColorAt(x, y int) color2.Color  { return p.color }
func (dc *Context) DrawImage(im image.Image, x, y int) { dc.DrawImageAnchored(im, x, y, 0, 0) }
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

func (dc *Context) ResetClip() {
	if dc.ownsMask {
		releaseAlpha(dc.mask)
	}
	dc.mask = nil
	dc.ownsMask = false
}

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
func (dc *Context) ClearPath() { dc.path.Clear(); dc.fillPath.Clear(); dc.hasCurrent = false }

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
type glyphKey struct {
	fontID   uint64
	r        rune
	subpixel uint8
}

func (dc *Context) glyphFontID(face font.Face) (id uint64, ok bool) {
	if dc.glyphCache == nil || !dc.glyphCache.enabled {
		return 0, false
	}
	tf := dc.textState.Tf
	if tf == nil {
		return 0, false
	}
	return tf.FaceID(face)
}

func releaseAlpha(a *image.Alpha) {
	if a == nil {
		return
	}
	core.ReleaseBytes(a.Pix)
	a.Pix = nil
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
func (dc *Context) Width() int { return dc.width }
func (dc *Context) Clear() {
	src := image.NewUniform(dc.color)
	draw.Draw(dc.img, dc.img.Bounds(), src, image.Point{}, draw.Src)
}

func lum(c [3]float64) float64                              { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }
func (dc *Context) Transform(x, y float64) (tx, ty float64) { return dc.matrix.Transform(x, y) }
func NewMeshShading(triangles []MeshTriangle, bounds image.Rectangle, sample MeshShadingSampler) context.Pattern {
	img := image.NewRGBA(meshTrianglesBounds(triangles).Intersect(bounds))
	if len(triangles) != 0 {
		comps := make([]float64, len(triangles[0][0].Components))
		budget := meshPixelBudgetFactor * int64(img.Rect.Dx()) * int64(img.Rect.Dy())
		for i := range triangles {
			area := triangleBounds(&triangles[i]).Intersect(img.Rect)
			budget -= int64(area.Dx()) * int64(area.Dy())
			if budget < 0 {
				if common.Log.IsLogLevel(common.LogLevelDebug) {
					common.Log.Debug("Mesh shading exceeds its rasterization budget after %d of %d triangles, dropping the remainder", i, len(triangles))
				}
				break
			}
			rasterizeTriangle(img, &triangles[i], sample, comps)
		}
	}
	return &meshShading{img: img}
}

func (dc *Context) capper() raster.Capper {
	switch dc.lineCap {
	case context.LineCapButt:
		return raster.ButtCapper
	case context.LineCapRound:
		return raster.RoundCapper
	case context.LineCapSquare:
		return raster.SquareCapper
	}
	return nil
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

func (dc *Context) ScaleAbout(sx, sy, x, y float64) {
	dc.Translate(x, y)
	dc.Scale(sx, sy)
	dc.Translate(-x, -y)
}

func (dc *Context) DrawPoint(x, y, r float64) {
	dc.Push()
	tx, ty := dc.Transform(x, y)
	dc.Identity()
	dc.DrawCircle(tx, ty, r)
	dc.Pop()
}
func (dc *Context) Translate(x, y float64) { dc.matrix = dc.matrix.Translate(x, y) }
func canUseFastGlyphPath(m transform.Matrix) bool {
	return m[1] == 0 && m[3] == 0 && math.Abs(m[0]-1) < epsilon && math.Abs(math.Abs(m[4])-1) < epsilon
}

func lerpPixel(dst []uint8, src []uint8, w, maxW uint32) {
	inv := maxW - w
	for i := 0; i < 4; i++ {
		dst[i] = uint8((uint32(dst[i])*inv + uint32(src[i])*w + maxW/2) / maxW)
	}
}

const meshPixelBudgetFactor = 16

func (s *functionShading) ColorAt(px, py int) color2.Color {
	x, y := s.deviceToShading.Transform(float64(px)+0.5, float64(py)+0.5)
	if x < s.domain[0] || x > s.domain[1] || y < s.domain[2] || y > s.domain[3] {
		return color2.Transparent
	}
	if col, ok := s.sample(x, y); ok {
		return col
	}
	return color2.Transparent
}

type glyphEntry struct {
	key     glyphKey
	bounds  image.Rectangle
	mask    *image.Alpha
	advance fixed.Int26_6
	size    int
}

func (dc *Context) SetFillRule(fillRule context.FillRule) { dc.fillRule = fillRule }

type meshShading struct{ img *image.RGBA }

func patchSizeSamples(patch *MeshPatch) int {
	return clampSampleCount(patchExtent(patch)/pixelsPerSample, maxSamples)
}

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

func (dc *Context) blitGlyphMask(mask *image.Alpha, maskPt image.Point, w, h, dx, dy int, r, g, b, a uint32, flip bool) {
	const max16 = 0xffff
	dst := dc.img
	bounds := dst.Bounds()
	for i := 0; i < h; i++ {
		y := dy + i
		if y < bounds.Min.Y || y >= bounds.Max.Y {
			continue
		}
		my := maskPt.Y + i
		if flip {
			my = maskPt.Y + (h - 1 - i)
		}
		mo := mask.PixOffset(maskPt.X, my)
		for j := 0; j < w; j++ {
			x := dx + j
			if x < bounds.Min.X || x >= bounds.Max.X {
				continue
			}
			cov := uint32(mask.Pix[mo+j])
			if cov == 0 {
				continue
			}
			cov16 := cov * 0x101
			if dc.mask != nil {
				clip16 := uint32(dc.mask.Pix[dc.mask.PixOffset(x, y)]) * 0x101
				cov16 = cov16 * clip16 / max16
				if cov16 == 0 {
					continue
				}
			}
			inv := (max16 - a*cov16/max16) * 0x101
			off := dst.PixOffset(x, y)
			dst.Pix[off+0] = uint8((uint32(dst.Pix[off+0])*inv + r*cov16) / max16 >> 8)
			dst.Pix[off+1] = uint8((uint32(dst.Pix[off+1])*inv + g*cov16) / max16 >> 8)
			dst.Pix[off+2] = uint8((uint32(dst.Pix[off+2])*inv + b*cov16) / max16 >> 8)
			dst.Pix[off+3] = uint8((uint32(dst.Pix[off+3])*inv + a*cov16) / max16 >> 8)
		}
	}
}

const (
	pixelsPerSample = 4
	maxSamples      = 32
)

func (dc *Context) DrawLine(x1, y1, x2, y2 float64) {
	dc.MoveTo(x1, y1)
	dc.LineTo(x2, y2)
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

func (dc *Context) drawStringSlow(s string, face font.Face, pos fixed.Point26_6) {
	src := image.NewUniform(dc.color)
	var mask, softMask *image.Alpha
	if dc.knockout {
		mask, softMask = dc.mask, dc.softMask
	} else {
		mask = dc.effectiveMask()
	}
	var opts *draw.Options
	if mask != nil {
		opts = &draw.Options{DstMask: mask, DstMaskP: image.Point{}}
	}
	needsBlend := dc.blendMode != context.BlendModeNormal || dc.knockout
	alpha := 1.0
	if dc.knockout {
		col := color2.NRGBAModel.Convert(dc.color).(color2.NRGBA)
		alpha = float64(col.A) / 255
		col.A = 0xff
		src = image.NewUniform(col)
	}
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			pos.X += face.Kern(prev, r)
		}
		bounds, glyphMask, maskPt, advance, ok := face.Glyph(pos, r)
		if !ok {
			continue
		}
		local := bounds.Sub(bounds.Min)
		glyph := image.NewRGBA(local)
		draw.DrawMask(glyph, local, src, image.Point{}, glyphMask, maskPt, draw.Over)
		m := dc.matrix.Clone().Translate(float64(bounds.Min.X), float64(bounds.Min.Y))
		aff := f64.Aff3{m[0], m[3], m[6], m[1], m[4], m[7]}
		if needsBlend {
			area := transformedBounds(aff, local).Intersect(dc.img.Bounds())
			if !area.Empty() {
				tmp := image.NewRGBA(area)
				dc.interpolator.Transform(tmp, aff, glyph, local, draw.Over, opts)
				compositeBlend(dc.img, tmp, dc.backdrop, dc.blendMode, alpha, softMask, dc.knockout)
			}
		} else {
			dc.interpolator.Transform(dc.img, aff, glyph, local, draw.Over, opts)
		}
		pos.X += advance
		prev = r
	}
}

type circle struct{ x, y, r float64 }

func (s *meshShading) ColorAt(px, py int) color2.Color { return s.img.RGBAAt(px, py) }

type MeshVertex struct {
	X, Y       float64
	Components []float64
}

func patchColorSamples(patch *MeshPatch) int {
	c00, c01 := patch.Corners[0][0], patch.Corners[0][1]
	c10, c11 := patch.Corners[1][0], patch.Corners[1][1]
	var twist, span float64
	for k := range c00 {
		twist = math.Max(twist, math.Abs(c00[k]+c11[k]-c01[k]-c10[k]))
		lo := math.Min(math.Min(c00[k], c01[k]), math.Min(c10[k], c11[k]))
		hi := math.Max(math.Max(c00[k], c01[k]), math.Max(c10[k], c11[k]))
		span = math.Max(span, hi-lo)
	}
	p00, p03 := patch.P[0][0], patch.P[0][3]
	p30, p33 := patch.P[3][0], patch.P[3][3]
	extent := patchExtent(patch)
	if extent > 0 {
		geomTwist := math.Max(math.Abs(p00.X+p33.X-p03.X-p30.X), math.Abs(p00.Y+p33.Y-p03.Y-p30.Y))
		twist += span * geomTwist / extent
	}
	tol := patch.ColorTolerance
	if !(tol > 0) {
		tol = defaultColorTolerance
	}
	maxN := defaultColorSamples
	if extent > float64(maxN) {
		maxN = int(math.Min(extent, maxSamples))
	}
	return clampSampleCount(math.Sqrt(twist/(4*tol)), maxN)
}
func (dc *Context) Height() int { return dc.height }
