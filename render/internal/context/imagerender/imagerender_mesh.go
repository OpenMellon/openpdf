package imagerender

import (
	"image"
	color2 "image/color"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/render/internal/context"
)

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

type MeshShadingSampler func(components []float64) (color2.RGBA, bool)

func meshTrianglesBounds(tris []MeshTriangle) image.Rectangle {
	var bounds image.Rectangle
	for i := range tris {
		bounds = bounds.Union(triangleBounds(&tris[i]))
	}
	return bounds
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

func triangleBounds(tri *MeshTriangle) image.Rectangle {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, v := range tri {
		minX, maxX = math.Min(minX, v.X), math.Max(maxX, v.X)
		minY, maxY = math.Min(minY, v.Y), math.Max(maxY, v.Y)
	}
	return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY)))
}

func dot3(ax, ay, az, bx, by, bz float64) float64 {
	return ax*bx + ay*by + az*bz
}

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

type functionShading struct {
	deviceToShading transform.Matrix
	domain          [4]float64
	sample          FunctionShadingSampler
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

type FunctionShadingSampler func(x, y float64) (color2.Color, bool)

func (e *patchEdge) point(t float64) (x, y float64) {
	mt := 1 - t
	bern := [4]float64{mt * mt * mt, 3 * mt * mt * t, 3 * mt * t * t, t * t * t}
	for i, pt := range e.points {
		x += bern[i] * pt.X
		y += bern[i] * pt.Y
	}
	return x, y
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

func bernsteinTable(n int) [][4]float64 {
	table := make([][4]float64, n+1)
	for i := range table {
		t := float64(i) / float64(n)
		mt := 1 - t
		table[i] = [4]float64{mt * mt * mt, 3 * mt * mt * t, 3 * mt * t * t, t * t * t}
	}
	return table
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

type MeshTriangle [3]MeshVertex

func patchEdges(patch *MeshPatch) [4]patchEdge {
	edges := [4]patchEdge{{points: [4]transform.Point{patch.P[0][0], patch.P[0][1], patch.P[0][2], patch.P[0][3]}}, {points: [4]transform.Point{patch.P[3][0], patch.P[3][1], patch.P[3][2], patch.P[3][3]}}, {points: [4]transform.Point{patch.P[0][0], patch.P[1][0], patch.P[2][0], patch.P[3][0]}}, {points: [4]transform.Point{patch.P[0][3], patch.P[1][3], patch.P[2][3], patch.P[3][3]}}}
	for i := range edges {
		edges[i].samples = edges[i].sampleCount()
	}
	return edges
}

type patchEdge struct {
	points  [4]transform.Point
	samples int
}

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

type meshShading struct{ img *image.RGBA }

func patchSizeSamples(patch *MeshPatch) int {
	return clampSampleCount(patchExtent(patch)/pixelsPerSample, maxSamples)
}

const (
	pixelsPerSample = 4
	maxSamples      = 32
)

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
