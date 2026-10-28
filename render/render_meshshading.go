package render

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/OpenMellon/openpdf/render/internal/context/imagerender"
)

const maxPatchCount = 1 << 18

func (s *meshShadingSpec) vertexBits() int {
	return 2*s.bitsPerCoordinate + s.numComps*s.bitsPerComponent
}

func meshFunctionSampler(fns []model.PdfFunction, cs model.PdfColorspace, t0, t1 float64) (imagerender.MeshShadingSampler, error) {
	lut := make([]color.RGBA, meshLUTSize+1)
	for i := range lut {
		t := t0 + (t1-t0)*float64(i)/meshLUTSize
		c, err := evalFunctionsColor(fns, cs, []float64{t})
		if err != nil {
			return nil, fmt.Errorf("evaluating mesh shading function: %w", err)
		}
		lut[i] = c
	}
	span := t1 - t0
	if span == 0 {
		return func([]float64) (color.RGBA, bool) { return lut[0], true }, nil
	}
	return func(params []float64) (color.RGBA, bool) {
		idx := int(math.Round((params[0] - t0) / span * meshLUTSize))
		if idx < 0 {
			idx = 0
		} else if idx > meshLUTSize {
			idx = meshLUTSize
		}
		return lut[idx], true
	}, nil
}

const meshLUTSize = 1024

func coonsInteriorPoints(p *[4][4]transform.Point) {
	for _, ci := range [2]int{0, 3} {
		for _, cj := range [2]int{0, 3} {
			ri, rj := 1+ci/3, 1+cj/3
			oi, oj := 3-ci, 3-cj
			p[ri][rj] = transform.NewPoint((-4*p[ci][cj].X+6*(p[ci][rj].X+p[ri][cj].X)-2*(p[ci][oj].X+p[oi][cj].X)+3*(p[oi][rj].X+p[ri][oj].X)-p[oi][oj].X)/9, (-4*p[ci][cj].Y+6*(p[ci][rj].Y+p[ri][cj].Y)-2*(p[ci][oj].Y+p[oi][cj].Y)+3*(p[oi][rj].Y+p[ri][oj].Y)-p[oi][oj].Y)/9)
		}
	}
}

func (d *meshDecoder) readValue(nbits int, lo, hi float64) (float64, error) {
	raw, err := d.reader.ReadBits(byte(nbits))
	if err != nil {
		return 0, fmt.Errorf("reading %d bit mesh value: %w", nbits, err)
	}
	maxVal := float64(uint64(1)<<uint(nbits) - 1)
	return lo + float64(raw)*(hi-lo)/maxVal, nil
}

func (d *meshDecoder) decodeFreeForm() ([]imagerender.MeshTriangle, error) {
	vertexBytes := (d.spec.bitsPerFlag + d.spec.vertexBits() + 7) / 8
	remaining := len(d.spec.data) / vertexBytes
	d.colors = make([]float64, 0, remaining*d.spec.numComps)
	var (
		tris         []imagerender.MeshTriangle
		va, vb, vc   imagerender.MeshVertex
		haveTriangle bool
	)
	for remaining > 0 {
		flag, v, err := d.readFlaggedVertex()
		if err != nil {
			return nil, err
		}
		remaining--
		switch {
		case flag == 0:
			if remaining < 2 {
				common.Log.Debug("Free-form mesh ends mid-triangle, dropping the remainder")
				return tris, nil
			}
			_, v2, err := d.readFlaggedVertex()
			if err != nil {
				return nil, err
			}
			_, v3, err := d.readFlaggedVertex()
			if err != nil {
				return nil, err
			}
			remaining -= 2
			va, vb, vc = v, v2, v3
		case (flag == 1 || flag == 2) && haveTriangle:
			if flag == 1 {
				va = vb
			}
			vb, vc = vc, v
		default:
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Free-form mesh has unusable edge flag %d, dropping the remainder", flag)
			}
			return tris, nil
		}
		tris = append(tris, imagerender.MeshTriangle{va, vb, vc})
		haveTriangle = true
	}
	return tris, nil
}

func parseMeshShading(shading *model.PdfShading) (*meshShadingSpec, error) {
	stream, ok := core.GetStream(shading.GetContainingPdfObject())
	if !ok {
		return nil, errors.New("mesh shading is not a stream object")
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, fmt.Errorf("decoding mesh shading stream: %w", err)
	}
	spec := &meshShadingSpec{shadingType: int(*shading.ShadingType), data: data}
	var (
		decode                                *core.PdfObjectArray
		fns                                   []model.PdfFunction
		coordBits, compBits, flagBits, perRow *core.PdfObjectInteger
	)
	switch sh := shading.GetContext().(type) {
	case *model.PdfShadingType4:
		coordBits, compBits, flagBits = sh.BitsPerCoordinate, sh.BitsPerComponent, sh.BitsPerFlag
		decode, fns = sh.Decode, sh.Function
	case *model.PdfShadingType5:
		coordBits, compBits, perRow = sh.BitsPerCoordinate, sh.BitsPerComponent, sh.VerticesPerRow
		decode, fns = sh.Decode, sh.Function
	case *model.PdfShadingType6:
		coordBits, compBits, flagBits = sh.BitsPerCoordinate, sh.BitsPerComponent, sh.BitsPerFlag
		decode, fns = sh.Decode, sh.Function
	case *model.PdfShadingType7:
		coordBits, compBits, flagBits = sh.BitsPerCoordinate, sh.BitsPerComponent, sh.BitsPerFlag
		decode, fns = sh.Decode, sh.Function
	default:
		return nil, errors.New("invalid mesh shading context")
	}
	if coordBits == nil || compBits == nil {
		return nil, errors.New("mesh shading missing a bit width entry")
	}
	spec.bitsPerCoordinate = int(*coordBits)
	spec.bitsPerComponent = int(*compBits)
	if !intIn(spec.bitsPerCoordinate, 1, 2, 4, 8, 12, 16, 24, 32) {
		return nil, fmt.Errorf("invalid BitsPerCoordinate %d", spec.bitsPerCoordinate)
	}
	if !intIn(spec.bitsPerComponent, 1, 2, 4, 8, 12, 16) {
		return nil, fmt.Errorf("invalid BitsPerComponent %d", spec.bitsPerComponent)
	}
	if spec.shadingType == int(ShadingTypeLatticeForm) {
		if perRow == nil {
			return nil, errors.New("lattice-form mesh has no VerticesPerRow")
		}
		spec.verticesPerRow = int(*perRow)
		if spec.verticesPerRow < 2 {
			return nil, fmt.Errorf("invalid VerticesPerRow %d", spec.verticesPerRow)
		}
	} else {
		if flagBits == nil {
			return nil, errors.New("mesh shading has no BitsPerFlag")
		}
		spec.bitsPerFlag = int(*flagBits)
		if !intIn(spec.bitsPerFlag, 2, 4, 8) {
			return nil, fmt.Errorf("invalid BitsPerFlag %d", spec.bitsPerFlag)
		}
	}
	if shading.ColorSpace == nil {
		return nil, errors.New("mesh shading has no color space")
	}
	spec.numComps = shading.ColorSpace.GetNumComponents()
	spec.hasFunction = len(fns) > 0
	if spec.hasFunction {
		spec.numComps = 1
	}
	if decode == nil {
		return nil, errors.New("mesh shading has no Decode array")
	}
	spec.decode, err = decode.ToFloat64Array()
	if err != nil {
		return nil, fmt.Errorf("reading mesh shading Decode: %w", err)
	}
	if want := 4 + 2*spec.numComps; len(spec.decode) != want {
		return nil, fmt.Errorf("mesh shading Decode has %d entries, want %d", len(spec.decode), want)
	}
	spec.sampler, err = meshColorSampler(fns, shading.ColorSpace, spec.decode)
	if err != nil {
		return nil, err
	}
	return spec, nil
}

func (d *meshDecoder) decodeLatticeForm() ([]imagerender.MeshTriangle, error) {
	perRow := d.spec.verticesPerRow
	rows := len(d.spec.data) * 8 / d.spec.vertexBits() / perRow
	if rows < 2 {
		return nil, errors.New("lattice-form mesh holds fewer than two complete rows")
	}
	d.colors = make([]float64, 0, rows*perRow*d.spec.numComps)
	var tris []imagerender.MeshTriangle
	prev := make([]imagerender.MeshVertex, perRow)
	cur := make([]imagerender.MeshVertex, perRow)
	for row := 0; row < rows; row++ {
		for col := 0; col < perRow; col++ {
			v, err := d.readVertex()
			if err != nil {
				return nil, err
			}
			cur[col] = v
		}
		if row > 0 {
			for i := 0; i+1 < perRow; i++ {
				tris = append(tris, imagerender.MeshTriangle{prev[i], prev[i+1], cur[i]}, imagerender.MeshTriangle{prev[i+1], cur[i+1], cur[i]})
			}
		}
		prev, cur = cur, prev
	}
	return tris, nil
}

var patchPointOrder = [16][2]int{{0, 0}, {0, 1}, {0, 2}, {0, 3}, {1, 3}, {2, 3}, {3, 3}, {3, 2}, {3, 1}, {3, 0}, {2, 0}, {1, 0}, {1, 1}, {1, 2}, {2, 2}, {2, 1}}

func (d *meshDecoder) readFlaggedVertex() (int, imagerender.MeshVertex, error) {
	flag, err := d.reader.ReadBits(byte(d.spec.bitsPerFlag))
	if err != nil {
		return 0, imagerender.MeshVertex{}, fmt.Errorf("reading mesh edge flag: %w", err)
	}
	v, err := d.readVertex()
	if err != nil {
		return 0, imagerender.MeshVertex{}, err
	}
	d.reader.Align()
	return int(flag), v, nil
}

type meshDecoder struct {
	spec      *meshShadingSpec
	reader    *bitwise.Reader
	matrix    transform.Matrix
	colors    []float64
	bounds    [4]float64
	hasBounds bool
}

func (d *meshDecoder) decodePatches(maxTriangles int) ([]imagerender.MeshTriangle, error) {
	pointsPerPatch := 12
	if d.spec.shadingType == int(ShadingTypeTensorProduct) {
		pointsPerPatch = 16
	}
	fullPatchBytes := d.spec.patchBytes(pointsPerPatch, 4)
	contPatchBytes := d.spec.patchBytes(pointsPerPatch-4, 2)
	tolerance := d.spec.colorTolerance()
	var (
		tris        []imagerender.MeshTriangle
		prevPoints  [4][4]transform.Point
		prevCorners [2][2][]float64
		havePrev    bool
	)
	for {
		remaining := len(d.spec.data) - int(d.reader.RelativePosition())
		if remaining < contPatchBytes {
			return tris, nil
		}
		flag, err := d.reader.ReadBits(byte(d.spec.bitsPerFlag))
		if err != nil {
			return nil, fmt.Errorf("reading patch mesh edge flag: %w", err)
		}
		var (
			patch imagerender.MeshPatch
			pts   [4][4]transform.Point
		)
		firstPoint, firstColor := 0, 0
		switch {
		case flag == 0:
			if remaining < fullPatchBytes {
				common.Log.Debug("Patch mesh ends mid-patch, dropping the remainder")
				return tris, nil
			}
		case flag <= 3 && havePrev:
			edge := sharedEdgePoints[flag]
			for i, idx := range edge {
				pts[0][i] = prevPoints[idx[0]][idx[1]]
			}
			cornerA, cornerB := cornerIndex[flag], cornerIndex[(flag+1)%4]
			patch.Corners[0][0] = prevCorners[cornerA[0]][cornerA[1]]
			patch.Corners[0][1] = prevCorners[cornerB[0]][cornerB[1]]
			firstPoint, firstColor = 4, 2
		default:
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("Patch mesh has unusable edge flag %d, dropping the remainder", flag)
			}
			return tris, nil
		}
		for i := firstPoint; i < pointsPerPatch; i++ {
			pos := patchPointOrder[i]
			if pts[pos[0]][pos[1]], err = d.readPoint(); err != nil {
				return nil, err
			}
		}
		for i := firstColor; i < 4; i++ {
			pos := cornerIndex[i]
			if patch.Corners[pos[0]][pos[1]], err = d.readColor(); err != nil {
				return nil, err
			}
		}
		d.reader.Align()
		if d.spec.shadingType == int(ShadingTypeCoons) {
			coonsInteriorPoints(&pts)
			for _, idx := range [4][2]int{{1, 1}, {1, 2}, {2, 1}, {2, 2}} {
				d.trackBounds(pts[idx[0]][idx[1]].X, pts[idx[0]][idx[1]].Y)
			}
		}
		for row := range pts {
			for col, p := range pts[row] {
				x, y := d.matrix.Transform(p.X, p.Y)
				patch.P[row][col] = transform.NewPoint(x, y)
			}
		}
		patch.ColorTolerance = tolerance
		tris = imagerender.TessellatePatch(&patch, tris)
		prevPoints, prevCorners, havePrev = pts, patch.Corners, true
		if len(tris) >= maxTriangles {
			common.Log.Debug("Patch mesh exceeds %d triangles, dropping the remainder", maxTriangles)
			return tris, nil
		}
	}
}

type meshShadingSpec struct {
	shadingType       int
	data              []byte
	bitsPerCoordinate int
	bitsPerComponent  int
	bitsPerFlag       int
	verticesPerRow    int
	decode            []float64
	numComps          int
	hasFunction       bool
	sampler           imagerender.MeshShadingSampler
}

func (s *meshShadingSpec) colorTolerance() float64 {
	if !s.hasFunction {
		return 0
	}
	return math.Abs(s.decode[5]-s.decode[4]) / meshLUTSize
}

func (s *meshShadingSpec) patchBytes(nPoints, nColors int) int {
	bits := s.bitsPerFlag + nPoints*2*s.bitsPerCoordinate + nColors*s.numComps*s.bitsPerComponent
	return (bits + 7) / 8
}

var sharedEdgePoints = [4][4][2]int{1: {{0, 3}, {1, 3}, {2, 3}, {3, 3}}, 2: {{3, 3}, {3, 2}, {3, 1}, {3, 0}}, 3: {{3, 0}, {2, 0}, {1, 0}, {0, 0}}}

func (d *meshDecoder) readPoint() (transform.Point, error) {
	x, err := d.readValue(d.spec.bitsPerCoordinate, d.spec.decode[0], d.spec.decode[1])
	if err != nil {
		return transform.Point{}, err
	}
	y, err := d.readValue(d.spec.bitsPerCoordinate, d.spec.decode[2], d.spec.decode[3])
	if err != nil {
		return transform.Point{}, err
	}
	d.trackBounds(x, y)
	return transform.NewPoint(x, y), nil
}

func meshColorSampler(fns []model.PdfFunction, cs model.PdfColorspace, decode []float64) (imagerender.MeshShadingSampler, error) {
	if len(fns) > 0 {
		return meshFunctionSampler(fns, cs, decode[4], decode[5])
	}
	switch cs.(type) {
	case *model.PdfColorspaceDeviceRGB:
		return func(comps []float64) (color.RGBA, bool) {
			return color.RGBA{R: clampToByteRounded(comps[0]), G: clampToByteRounded(comps[1]), B: clampToByteRounded(comps[2]), A: 255}, true
		}, nil
	case *model.PdfColorspaceDeviceGray:
		return func(comps []float64) (color.RGBA, bool) {
			g := clampToByteRounded(comps[0])
			return color.RGBA{R: g, G: g, B: g, A: 255}, true
		}, nil
	case *model.PdfColorspaceDeviceCMYK:
		return func(comps []float64) (color.RGBA, bool) {
			c := math.Min(math.Max(comps[0], 0), 1)
			m := math.Min(math.Max(comps[1], 0), 1)
			y := math.Min(math.Max(comps[2], 0), 1)
			invK := 1 - math.Min(math.Max(comps[3], 0), 1)
			return color.RGBA{R: clampToByteRounded((1 - c) * invK), G: clampToByteRounded((1 - m) * invK), B: clampToByteRounded((1 - y) * invK), A: 255}, true
		}, nil
	}
	var warned bool
	return func(comps []float64) (color.RGBA, bool) {
		c, err := colorFromComponents(cs, comps)
		if err != nil {
			if !warned {
				warned = true
				common.Log.Debug("Error sampling mesh shading color: %v", err)
			}
			return color.RGBA{}, false
		}
		return c, true
	}, nil
}

func (d *meshDecoder) readColor() ([]float64, error) {
	start := len(d.colors)
	for i := 0; i < d.spec.numComps; i++ {
		v, err := d.readValue(d.spec.bitsPerComponent, d.spec.decode[4+2*i], d.spec.decode[5+2*i])
		if err != nil {
			return nil, err
		}
		d.colors = append(d.colors, v)
	}
	return d.colors[start : start+d.spec.numComps : start+d.spec.numComps], nil
}

func (d *meshDecoder) readVertex() (imagerender.MeshVertex, error) {
	p, err := d.readPoint()
	if err != nil {
		return imagerender.MeshVertex{}, err
	}
	colors, err := d.readColor()
	if err != nil {
		return imagerender.MeshVertex{}, err
	}
	x, y := d.matrix.Transform(p.X, p.Y)
	return imagerender.MeshVertex{X: x, Y: y, Components: colors}, nil
}

var cornerIndex = [4][2]int{{0, 0}, {0, 1}, {1, 1}, {1, 0}}

func (d *meshDecoder) trackBounds(x, y float64) {
	if !d.hasBounds {
		d.bounds = [4]float64{x, x, y, y}
		d.hasBounds = true
		return
	}
	if x < d.bounds[0] {
		d.bounds[0] = x
	}
	if x > d.bounds[1] {
		d.bounds[1] = x
	}
	if y < d.bounds[2] {
		d.bounds[2] = y
	}
	if y > d.bounds[3] {
		d.bounds[3] = y
	}
}

func (r *renderer) processMeshShading(ctx context.Context, shading *model.PdfShading, matrix *core.PdfObjectArray) (context.Pattern, *core.PdfObjectArray, error) {
	spec, err := parseMeshShading(shading)
	if err != nil {
		return nil, nil, err
	}
	patMatrix := matrixOrIdentity(matrix)
	dec := &meshDecoder{spec: spec, reader: bitwise.NewReader(spec.data), matrix: r.shadingBaseMatrix(ctx, matrix).Mult(patMatrix)}
	var tris []imagerender.MeshTriangle
	switch PdfShadingType(spec.shadingType) {
	case ShadingTypeLatticeForm:
		tris, err = dec.decodeLatticeForm()
	case ShadingTypeCoons, ShadingTypeTensorProduct:
		tris, err = dec.decodePatches(maxPatchCount)
	default:
		tris, err = dec.decodeFreeForm()
	}
	if err != nil {
		return nil, nil, err
	}
	if len(tris) == 0 {
		return nil, nil, errors.New("mesh shading holds no triangles")
	}
	pattern := imagerender.NewMeshShading(tris, image.Rect(0, 0, ctx.Width(), ctx.Height()), spec.sampler)
	return pattern, transformedBBox(patMatrix, dec.bounds), nil
}
