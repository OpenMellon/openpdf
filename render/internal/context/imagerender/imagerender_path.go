package imagerender

import (
	"image"
	color2 "image/color"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/unidoc/freetype/raster"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"
)

func quadraticPoint(x0, y0, x1, y1, x2, y2, t float64) (x, y float64) {
	mt := 1 - t
	w0 := mt * mt
	w1 := 2 * mt * t
	w2 := t * t
	x = w0*x0 + w1*x1 + w2*x2
	y = w0*y0 + w1*y1 + w2*y2
	return
}

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
func (dc *Context) SetDashOffset(offset float64) { dc.dashOffset = offset }

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

func (dc *Context) NewSubPath() {
	if dc.hasCurrent {
		dc.fillPath.Add1(toFixedPoint(dc.start))
	}
	dc.hasCurrent = false
}

const epsilon = 1e-9

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
func (dc *Context) FillAlpha() float64                    { return dc.fillAlpha }
func (dc *Context) Fill()                                 { dc.FillPreserve(); dc.ClearPath() }
func (dc *Context) SetLineJoin(lineJoin context.LineJoin) { dc.lineJoin = lineJoin }
func (dc *Context) Stroke()                               { dc.StrokePreserve(); dc.ClearPath() }
func (dc *Context) SetLineWidth(lineWidth float64)        { dc.lineWidth = lineWidth }

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

func toFixed(v float64) fixed.Int26_6 {
	v = precision.RoundDefault(v)
	return fixed.Int26_6(v * 64)
}
func (dc *Context) SetDash(dashes ...float64) { dc.dashes = dashes }

func (dc *Context) DrawCircle(x, y, r float64) {
	dc.NewSubPath()
	dc.DrawEllipticalArc(x, y, r, r, 0, 2*math.Pi)
	dc.ClosePath()
}

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
func degreesToRadians(deg float64) float64       { return deg * math.Pi / 180 }
func (dc *Context) StrokeAlpha() float64         { return dc.strokeAlpha }
func (dc *Context) FillPattern() context.Pattern { return dc.fillPattern }

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

func toFixedPoint(p transform.Point) fixed.Point26_6 {
	return fixed.Point26_6{X: toFixed(p.X), Y: toFixed(p.Y)}
}

func (dc *Context) ClosePath() {
	if dc.hasCurrent {
		fp := toFixedPoint(dc.start)
		dc.path.Add1(fp)
		dc.fillPath.Add1(fp)
		dc.current = dc.start
	}
}

func (dc *Context) DrawArc(x, y, r, angle1, angle2 float64) {
	dc.DrawEllipticalArc(x, y, r, r, angle1, angle2)
}

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

func (dc *Context) DrawEllipse(x, y, rx, ry float64) {
	dc.NewSubPath()
	dc.DrawEllipticalArc(x, y, rx, ry, 0, 2*math.Pi)
	dc.ClosePath()
}

type pathSnapshot struct {
	path       raster.Path
	fillPath   raster.Path
	start      transform.Point
	current    transform.Point
	hasCurrent bool
}

func (dc *Context) StrokePattern() context.Pattern { return dc.strokePattern }

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

func (dc *Context) SavePath() {
	dc.pathStack = append(dc.pathStack, pathSnapshot{path: append(raster.Path(nil), dc.path...), fillPath: append(raster.Path(nil), dc.fillPath...), start: dc.start, current: dc.current, hasCurrent: dc.hasCurrent})
}

func (dc *Context) SetLineCap(lineCap context.LineCap) { dc.lineCap = lineCap }

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

const (
	flatnessScale     = 0.75
	flatnessTolerance = 0.5
)

func (dc *Context) DrawImage(im image.Image, x, y int) { dc.DrawImageAnchored(im, x, y, 0, 0) }
func (dc *Context) ClearPath()                         { dc.path.Clear(); dc.fillPath.Clear(); dc.hasCurrent = false }

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

func (dc *Context) DrawPoint(x, y, r float64) {
	dc.Push()
	tx, ty := dc.Transform(x, y)
	dc.Identity()
	dc.DrawCircle(tx, ty, r)
	dc.Pop()
}

func (dc *Context) DrawLine(x1, y1, x2, y2 float64) {
	dc.MoveTo(x1, y1)
	dc.LineTo(x2, y2)
}

type circle struct{ x, y, r float64 }
