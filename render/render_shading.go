package render

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"slices"
	"sort"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/OpenMellon/openpdf/render/internal/context/imagerender"
)

func (r *renderer) processFunctionShading(ctx context.Context, shading *model.PdfShading, patternMatrix *core.PdfObjectArray) (context.Pattern, *core.PdfObjectArray, error) {
	fnShading, ok := shading.GetContext().(*model.PdfShadingType1)
	if !ok {
		return nil, nil, errors.New("invalid function-based shading context")
	}
	if len(fnShading.Function) == 0 {
		return nil, nil, errors.New("no shading function found, skip conversion")
	}
	domain := functionDomain(fnShading.Domain)
	fullMatrix := matrixOrIdentity(patternMatrix).Mult(matrixOrIdentity(fnShading.Matrix))
	invMatrix, ok := r.shadingBaseMatrix(ctx, patternMatrix).Mult(fullMatrix).Inverse()
	if !ok {
		return nil, nil, errors.New("singular function-based shading matrix")
	}
	fns, cs := fnShading.Function, shading.ColorSpace
	mid := []float64{(domain[0] + domain[1]) / 2, (domain[2] + domain[3]) / 2}
	if _, err := evalFunctionsColor(fns, cs, mid); err != nil {
		return nil, nil, fmt.Errorf("evaluating function-based shading: %w", err)
	}
	var warned bool
	sample := func(x, y float64) (color.Color, bool) {
		c, err := evalFunctionsColor(fns, cs, []float64{x, y})
		if err != nil {
			if !warned {
				warned = true
				common.Log.Debug("Error sampling function-based shading at (%f, %f): %v", x, y, err)
			}
			return nil, false
		}
		return c, true
	}
	pattern := imagerender.NewFunctionShading(invMatrix, domain, sample)
	return pattern, transformedBBox(fullMatrix, domain), nil
}

var (
	srgbToLinear = func() (lut [256]float64) {
		for i := range lut {
			v := float64(i) / 255
			if v <= 0.04045 {
				lut[i] = v / 12.92
			} else {
				lut[i] = math.Pow((v+0.055)/1.055, 2.4)
			}
		}
		return lut
	}()
	linearToSRGB = func() (lut [4096]uint8) {
		for i := range lut {
			v := float64(i) / float64(len(lut)-1)
			if v <= 0.0031308 {
				lut[i] = unitToByteRound(12.92 * v)
			} else {
				lut[i] = unitToByteRound(1.055*math.Pow(v, 1/2.4) - 0.055)
			}
		}
		return lut
	}()
)

func floatVal(f *core.PdfObjectFloat) float64 {
	if f == nil {
		return 0
	}
	return float64(*f)
}

func stitchBoundsIn(fns []model.PdfFunction, t0, t1 float64) []float64 {
	lo, hi := math.Min(t0, t1), math.Max(t0, t1)
	var bounds []float64
	for _, fn := range fns {
		stitch, ok := fn.(*model.PdfFunctionType3)
		if !ok {
			continue
		}
		for _, b := range stitch.Bounds {
			if b > lo && b < hi {
				bounds = append(bounds, b)
			}
		}
	}
	sort.Float64s(bounds)
	return bounds
}

const gradientSamples = 64

func (r *renderer) processShadingPattern(ctx context.Context, op *contentstream.ContentStreamOperation, res *model.PdfPageResources, name *core.PdfObjectName) (context.Pattern, error) {
	if pat, ok := res.GetPatternByName(*name); ok && pat.IsShading() {
		shading := pat.GetAsShadingPattern().Shading
		pattern, _, err := r.processShading(ctx, shading, pat.GetAsShadingPattern().Matrix)
		if err != nil {
			return nil, err
		}
		return pattern, nil
	}
	return nil, nil
}

const (
	ShadingTypeFunctionBased PdfShadingType = 1
	ShadingTypeAxial         PdfShadingType = 2
	ShadingTypeRadial        PdfShadingType = 3
	ShadingTypeFreeForm      PdfShadingType = 4
	ShadingTypeLatticeForm   PdfShadingType = 5
	ShadingTypeCoons         PdfShadingType = 6
	ShadingTypeTensorProduct PdfShadingType = 7
)

func matrixOrIdentity(arr *core.PdfObjectArray) transform.Matrix {
	m, err := transform.MatrixFromPdfObject(arr)
	if err != nil {
		common.Log.Debug("Invalid shading Matrix, using identity: %v", err)
	}
	return m
}

func (r *renderer) processShading(ctx context.Context, shading *model.PdfShading, matrix *core.PdfObjectArray) (context.Pattern, *core.PdfObjectArray, error) {
	switch PdfShadingType(*shading.ShadingType) {
	case ShadingTypeFunctionBased:
		return r.processFunctionShading(ctx, shading, matrix)
	case ShadingTypeAxial:
		return r.processLinearShading(ctx, shading, matrix)
	case ShadingTypeRadial:
		return r.processRadialShading(ctx, shading, matrix)
	case ShadingTypeFreeForm, ShadingTypeLatticeForm, ShadingTypeCoons, ShadingTypeTensorProduct:
		return r.processMeshShading(ctx, shading, matrix)
	}
	common.Log.Debug(fmt.Sprintf("Processing shading type %d not yet supported", int64(*shading.ShadingType)))
	return nil, nil, nil
}

func shadingBackground(dict *core.PdfObjectDictionary, cs model.PdfColorspace) color.Color {
	arr, ok := core.GetArray(dict.Get("BC"))
	if !ok {
		return color.RGBA{A: 255}
	}
	vals, err := core.GetNumbersAsFloat(arr.Elements())
	if err != nil || len(vals) == 0 {
		return color.RGBA{A: 255}
	}
	toByte := func(v float64) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(v*255 + 0.5)
	}
	if cs != nil {
		if c, err := cs.ColorFromFloats(vals); err == nil {
			if rgb, convErr := cs.ColorToRGB(c); convErr == nil {
				if devRGB, ok := rgb.(*model.PdfColorDeviceRGB); ok {
					return color.RGBA{R: toByte(devRGB.R()), G: toByte(devRGB.G()), B: toByte(devRGB.B()), A: 255}
				}
			}
		}
	}
	switch len(vals) {
	case 1:
		g := toByte(vals[0])
		return color.RGBA{R: g, G: g, B: g, A: 255}
	case 3:
		return color.RGBA{R: toByte(vals[0]), G: toByte(vals[1]), B: toByte(vals[2]), A: 255}
	case 4:
		cyan, magenta, yellow, black := vals[0], vals[1], vals[2], vals[3]
		return color.RGBA{R: toByte((1 - cyan) * (1 - black)), G: toByte((1 - magenta) * (1 - black)), B: toByte((1 - yellow) * (1 - black)), A: 255}
	default:
		return color.RGBA{A: 255}
	}
}

func unitToByteRound(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return uint8(v*255 + 0.5)
	}
}

func colorToRGB8(cs model.PdfColorspace, comps []float64) ([3]uint8, error) {
	c, err := cs.ColorFromFloats(comps)
	if err != nil {
		return [3]uint8{}, err
	}
	rgb, err := cs.ColorToRGB(c)
	if err != nil {
		return [3]uint8{}, err
	}
	devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
	if !ok {
		return [3]uint8{}, errTypeCheck
	}
	return [3]uint8{unitToByteRound(devRGB.R()), unitToByteRound(devRGB.G()), unitToByteRound(devRGB.B())}, nil
}

func buildGradient(g context.Gradient, fns []model.PdfFunction, cs model.PdfColorspace, domain []float64) (context.Gradient, error) {
	if len(fns) == 0 {
		return nil, errors.New("no shading function found")
	}
	t0, t1 := domain[0], domain[1]
	normalize := func(t float64) float64 {
		if t1 == t0 {
			return 0
		}
		return (t - t0) / (t1 - t0)
	}
	addStop := func(t float64) error {
		c, err := evalFunctionsAt(fns, cs, t)
		if err != nil {
			return err
		}
		g.AddColorStop(normalize(t), c)
		return nil
	}
	bounds := stitchBoundsIn(fns, t0, t1)
	eps := math.Abs(t1-t0) * 1e-6
	segStart := t0
	for _, segEnd := range append(bounds, t1) {
		for i := 0; i <= gradientSamples; i++ {
			t := segStart + (segEnd-segStart)*float64(i)/float64(gradientSamples)
			if i == gradientSamples && segEnd != t1 {
				t = segEnd - eps
			}
			if err := addStop(t); err != nil {
				return nil, err
			}
		}
		segStart = segEnd
	}
	return g, nil
}

func functionDomain(arr *core.PdfObjectArray) [4]float64 {
	if arr != nil {
		if vals, err := arr.ToFloat64Array(); err == nil && len(vals) == 4 {
			return [4]float64{vals[0], vals[1], vals[2], vals[3]}
		}
		common.Log.Debug("Invalid function-based shading Domain, using the unit square")
	}
	return [4]float64{0, 1, 0, 1}
}

// PdfShadingType defines PDF shading types.
// Source: PDF32000_2008.pdf. Chapter 8.7.4.5
type PdfShadingType int64

func unitToByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v * 255)
}

func colorFromComponents(cs model.PdfColorspace, comps []float64) (color.RGBA, error) {
	if cs == nil {
		return color.RGBA{}, errors.New("shading has no color space")
	}
	if decode := cs.DecodeArray(); len(decode) == 2*len(comps) {
		for i := range comps {
			comps[i] = math.Min(math.Max(comps[i], decode[2*i]), decode[2*i+1])
		}
	}
	c, err := cs.ColorFromFloats(comps)
	if err != nil {
		return color.RGBA{}, err
	}
	rgb, err := cs.ColorToRGB(c)
	if err != nil {
		return color.RGBA{}, err
	}
	devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
	if !ok {
		return color.RGBA{}, fmt.Errorf("unexpected color type %T from colorspace conversion", rgb)
	}
	return color.RGBA{R: unitToByte(devRGB.R()), G: unitToByte(devRGB.G()), B: unitToByte(devRGB.B()), A: 255}, nil
}

func clampToByteRounded(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

func luminanceGray(r, g, b uint8) uint8 {
	lum := 0.2126*srgbToLinear[r] + 0.7152*srgbToLinear[g] + 0.0722*srgbToLinear[b]
	idx := int(lum*float64(len(linearToSRGB)-1) + 0.5)
	if idx < 0 {
		idx = 0
	} else if idx >= len(linearToSRGB) {
		idx = len(linearToSRGB) - 1
	}
	return linearToSRGB[idx]
}

func evalFunctionsColor(fns []model.PdfFunction, cs model.PdfColorspace, in []float64) (color.RGBA, error) {
	var comps []float64
	if len(fns) == 1 {
		out, err := fns[0].Evaluate(in)
		if err != nil {
			return color.RGBA{}, err
		}
		comps = out
	} else {
		for _, fn := range fns {
			out, err := fn.Evaluate(in)
			if err != nil {
				return color.RGBA{}, err
			}
			comps = append(comps, out...)
		}
	}
	return colorFromComponents(cs, comps)
}

func shadingDomain(arr *core.PdfObjectArray) []float64 {
	if arr != nil {
		if vals, err := arr.ToFloat64Array(); err == nil && len(vals) == 2 {
			return vals
		}
	}
	return []float64{0, 1}
}

const (
	maxShadingDim    = 2048
	maxShadingPixels = 1 << 21
	maxTilingDepth   = 4
	maxTilingCells   = 32
)

func colorRGB8(c color.Color) (r, g, b uint32) {
	cr, cg, cb, _ := c.RGBA()
	return cr >> 8, cg >> 8, cb >> 8
}

func intIn(v int, set ...int) bool {
	return slices.Contains(set, v)
}

func boolPairFromArray(arr *core.PdfObjectArray) (first, second bool) {
	if arr == nil || arr.Len() != 2 {
		return false, false
	}
	first, firstOK := core.GetBoolVal(arr.Get(0))
	second, secondOK := core.GetBoolVal(arr.Get(1))
	if !firstOK || !secondOK {
		return false, false
	}
	return first, second
}

func (r *renderer) shadingBaseMatrix(ctx context.Context, matrix *core.PdfObjectArray) transform.Matrix {
	if matrix != nil {
		return r.baseMatrix
	}
	return ctx.Matrix()
}

func (r *renderer) processLinearShading(ctx context.Context, shading *model.PdfShading, matrix *core.PdfObjectArray) (context.Pattern, *core.PdfObjectArray, error) {
	sh := shading.GetContext().(*model.PdfShadingType2)
	if len(sh.Function) == 0 {
		return nil, nil, errors.New("no gradient function found, skip conversion")
	}
	coords, err := sh.Coords.ToFloat64Array()
	if err != nil {
		return nil, nil, errors.New("failed getting shading position")
	}
	cs := shading.ColorSpace
	m := r.shadingBaseMatrix(ctx, matrix).Mult(matrixOrIdentity(matrix))
	x0, y0 := m.Transform(coords[0], coords[1])
	x1, y1 := m.Transform(coords[2], coords[3])
	extStart, extEnd := boolPairFromArray(sh.Extend)
	grad := imagerender.NewLinearGradient(x0, y0, x1, y1, extStart, extEnd)
	bbox := core.MakeArrayFromFloats([]float64{0, 0, 1, 1})
	for _, v := range coords {
		if v > 1 {
			bbox = sh.Coords
			break
		}
	}
	grad, err = buildGradient(grad, sh.Function, cs, shadingDomain(sh.Domain))
	return grad, bbox, err
}

func (r *renderer) paintShadingClip(ctx context.Context, shading *model.PdfShading, pattern context.Pattern) {
	ctx.SavePath()
	defer ctx.RestorePath()
	ctx.Push()
	defer ctx.Pop()
	ctx.ClearPath()
	ctx.SetFillStyle(pattern)
	if bbox := shading.BBox; bbox != nil {
		ctx.DrawRectangle(bbox.Llx, bbox.Lly, bbox.Urx-bbox.Llx, bbox.Ury-bbox.Lly)
		ctx.Fill()
		return
	}
	ctx.SetMatrix(transform.IdentityMatrix())
	clip := ctx.ClipBounds()
	ctx.DrawRectangle(float64(clip.Min.X), float64(clip.Min.Y), float64(clip.Dx()), float64(clip.Dy()))
	ctx.Fill()
}

func (r *renderer) processRadialShading(ctx context.Context, shading *model.PdfShading, matrix *core.PdfObjectArray) (context.Pattern, *core.PdfObjectArray, error) {
	sh := shading.GetContext().(*model.PdfShadingType3)
	if len(sh.Function) == 0 {
		return nil, nil, errors.New("no gradient function found, skip conversion")
	}
	coords, err := sh.Coords.ToFloat64Array()
	if err != nil {
		return nil, nil, errors.New("failed getting shading position")
	}
	cs := shading.ColorSpace
	bbox := core.MakeArrayFromFloats([]float64{0, 0, 1, 1})
	var x0, y0, x1, y1, r0, r1 float64
	m := r.shadingBaseMatrix(ctx, matrix).Mult(matrixOrIdentity(matrix))
	x0, y0 = m.Transform(coords[0], coords[1])
	x1, y1 = m.Transform(coords[3], coords[4])
	r0, _ = m.Transform(coords[2], 0)
	r1, _ = m.Transform(coords[5], 0)
	tx, _ := m.Translation()
	r0 -= tx
	r1 -= tx
	for i, v := range coords {
		if i == 2 || i == 5 {
			continue
		}
		if v > 1.0 {
			minX := math.Min(x0-r0, x1-r1)
			minY := math.Min(y0-r0, y1-r1)
			maxX := math.Max(x0+r0, x1+r1)
			maxY := math.Max(y0+r0, y1+r1)
			w := maxX - minX
			h := minY - maxY
			bbox = core.MakeArrayFromFloats([]float64{minX, minY, w, h})
			break
		}
	}
	extStart, extEnd := boolPairFromArray(sh.Extend)
	grad := imagerender.NewRadialGradient(x0, y0, r0, x1, y1, r1, extStart, extEnd)
	grad, err = buildGradient(grad, sh.Function, cs, shadingDomain(sh.Domain))
	if err != nil {
		return nil, nil, err
	}
	return grad, bbox, nil
}

func evalFunctionsAt(fns []model.PdfFunction, cs model.PdfColorspace, t float64) (color.RGBA, error) {
	return evalFunctionsColor(fns, cs, []float64{t})
}
