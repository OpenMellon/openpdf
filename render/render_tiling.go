package render

import (
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/OpenMellon/openpdf/render/internal/context/imagerender"
)

func tilingBBox(pattern *model.PdfTilingPattern) ([4]float64, error) {
	if pattern.BBox == nil {
		return [4]float64{}, errors.New("tiling pattern has no BBox")
	}
	bbox := *pattern.BBox
	bbox.Normalize()
	if bbox.Width() <= 0 || bbox.Height() <= 0 {
		return [4]float64{}, errors.New("tiling pattern BBox is empty")
	}
	return [4]float64{bbox.Llx, bbox.Lly, bbox.Urx, bbox.Ury}, nil
}

func (r *renderer) scnPattern(ctx context.Context, scnOp, csOp *contentstream.ContentStreamOperation, cs model.PdfColorspace, color model.PdfColor, res *model.PdfPageResources) (pattern context.Pattern, isShading bool) {
	if csOp != nil && len(scnOp.Params) > 0 && len(csOp.Params) > 0 {
		if csName, ok := core.GetName(csOp.Params[0]); ok && csName.String() == "Pattern" {
			if patternName, ok := core.GetName(scnOp.Params[0]); ok {
				shading, err := r.processShadingPattern(ctx, scnOp, res, patternName)
				if err != nil {
					r.noteDecodeLimit(err)
					common.Log.Debug("Error when processing shading pattern data: %v", err)
					return nil, false
				}
				if shading != nil {
					return shading, true
				}
			}
		}
	}
	req, ok := tilingRequestFromColor(cs, color)
	if !ok {
		return nil, false
	}
	tiling, err := r.processTilingPattern(ctx, res, req)
	if err != nil {
		r.noteDecodeLimit(err)
		common.Log.Debug("Error when processing tiling pattern data: %v", err)
		return nil, false
	}
	if tiling == nil {
		common.Log.Debug("Unknown pattern %s", req.name)
	}
	return tiling, false
}

func (r *renderer) tilingCell(ctx context.Context, pattern *model.PdfTilingPattern, rgb *model.PdfColorDeviceRGB, bbox [4]float64, sx, sy float64, w, h int) (*image.RGBA, error) {
	key := tilingCellKey{pattern: pattern.GetContainingPdfObject(), width: w, height: h}
	if rgb != nil {
		key.color = [3]float64{rgb.R(), rgb.G(), rgb.B()}
	}
	if cell, ok := r.tilingCache[key]; ok {
		return cell, nil
	}
	stream, ok := core.GetStream(core.TraceToDirectObject(pattern.GetContainingPdfObject()))
	if !ok {
		return nil, errTypeCheck
	}
	if !r.limits.Enter(stream, "tiling pattern") {
		return nil, errNestedStreamRefused
	}
	defer r.limits.Leave(stream)
	content, err := pattern.GetContentStream()
	if err != nil {
		return nil, err
	}
	ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
	if err != nil {
		return nil, err
	}
	r.limits.Charge(stream, len(*ops), len(content), 1)
	cellCtx := imagerender.NewContext(w, h)
	defer cellCtx.ReleaseBuffers()
	if parent, ok := ctx.(*imagerender.Context); ok {
		cellCtx.SetInterpolator(parent.Interpolator())
		cellCtx.SetGlyphCache(parent.GlyphCache())
	}
	cellCtx.SetMatrix(transform.NewMatrix(sx, 0, 0, -sy, -bbox[0]*sx, bbox[3]*sy))
	cellCtx.SetLineWidth(1.0)
	if rgb != nil {
		cellCtx.SetRGBA(rgb.R(), rgb.G(), rgb.B(), 1)
		cellCtx.LockColor()
	} else {
		cellCtx.SetRGBA(0, 0, 0, 1)
	}
	r.tilingDepth++
	err = r.renderOperations(cellCtx, ops, pattern.Resources, cellCtx.Matrix())
	r.tilingDepth--
	if err != nil {
		return nil, err
	}
	img := cellCtx.Image().(*image.RGBA)
	if r.limits.Exhausted() {
		return img, nil
	}
	if r.tilingCache == nil {
		r.tilingCache = map[tilingCellKey]*image.RGBA{}
	}
	if len(r.tilingCache) >= maxTilingCells {
		clear(r.tilingCache)
	}
	r.tilingCache[key] = img
	return img, nil
}

func tilingRequestFromColor(cs model.PdfColorspace, color model.PdfColor) (tilingPatternRequest, bool) {
	patCS, ok := cs.(*model.PdfColorspaceSpecialPattern)
	if !ok {
		return tilingPatternRequest{}, false
	}
	patColor, ok := color.(*model.PdfColorPattern)
	if !ok || patColor.PatternName == "" {
		return tilingPatternRequest{}, false
	}
	req := tilingPatternRequest{name: patColor.PatternName}
	if patColor.Color == nil {
		return req, true
	}
	if patCS.UnderlyingCS == nil {
		common.Log.Debug("Pattern colour has components but no underlying colorspace")
		return req, true
	}
	rgb, err := patCS.UnderlyingCS.ColorToRGB(patColor.Color)
	if err != nil {
		common.Log.Debug("Error converting uncolored pattern colour: %v", err)
		return req, true
	}
	if devRGB, ok := rgb.(*model.PdfColorDeviceRGB); ok {
		req.color = devRGB
	}
	return req, true
}

func (r *renderer) processTilingPattern(ctx context.Context, res *model.PdfPageResources, req tilingPatternRequest) (context.Pattern, error) {
	pat, ok := res.GetPatternByName(req.name)
	if !ok || !pat.IsTiling() {
		return nil, nil
	}
	tiling := pat.GetAsTilingPattern()
	if r.tilingDepth >= maxTilingDepth {
		return nil, fmt.Errorf("tiling pattern nested deeper than %d levels", maxTilingDepth)
	}
	bbox, err := tilingBBox(tiling)
	if err != nil {
		return nil, err
	}
	bboxW, bboxH := bbox[2]-bbox[0], bbox[3]-bbox[1]
	m := r.baseMatrix.Mult(matrixOrIdentity(tiling.Matrix))
	inv, ok := m.Inverse()
	if !ok {
		return nil, errors.New("singular tiling pattern matrix")
	}
	sx, sy, cellW, cellH := tilingCellSize(m, bboxW, bboxH)
	color := req.color
	if tiling.IsColored() {
		color = nil
	}
	cell, err := r.tilingCell(ctx, tiling, color, bbox, sx, sy, cellW, cellH)
	if err != nil {
		return nil, err
	}
	return imagerender.NewTilingPattern(cell, inv, bbox, floatVal(tiling.XStep), floatVal(tiling.YStep), sx, sy), nil
}

func transformedBBox(m transform.Matrix, domain [4]float64) *core.PdfObjectArray {
	corners := [4][2]float64{{domain[0], domain[2]}, {domain[1], domain[2]}, {domain[1], domain[3]}, {domain[0], domain[3]}}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range corners {
		x, y := m.Transform(corner[0], corner[1])
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	return core.MakeArrayFromFloats([]float64{minX, minY, maxX - minX, maxY - minY})
}

type tilingPatternRequest struct {
	name  core.PdfObjectName
	color *model.PdfColorDeviceRGB
}

func tilingCellSize(m transform.Matrix, w, h float64) (sx, sy float64, cellW, cellH int) {
	sx = m.ScalingFactorX()
	sy = m.ScalingFactorY()
	pw, ph := w*sx, h*sy
	if pw > maxShadingDim {
		pw, ph = maxShadingDim, ph*maxShadingDim/pw
	}
	if ph > maxShadingDim {
		pw, ph = pw*maxShadingDim/ph, maxShadingDim
	}
	cellW = max(int(math.Ceil(pw)), 1)
	cellH = max(int(math.Ceil(ph)), 1)
	if area := cellW * cellH; area > maxShadingPixels {
		shrink := math.Sqrt(float64(maxShadingPixels) / float64(area))
		cellW = max(int(float64(cellW)*shrink), 1)
		cellH = max(int(float64(cellH)*shrink), 1)
	}
	return float64(cellW) / w, float64(cellH) / h, cellW, cellH
}

func rotatedBBox(w, h, angle float64) contentdraw.BoundingBox {
	return contentdraw.Path{Points: []contentdraw.Point{contentdraw.NewPoint(0, 0).Rotate(angle), contentdraw.NewPoint(w, 0).Rotate(angle), contentdraw.NewPoint(0, h).Rotate(angle), contentdraw.NewPoint(w, h).Rotate(angle)}}.GetBoundingBox()
}

type tilingCellKey struct {
	pattern core.PdfObject
	width   int
	height  int
	color   [3]float64
}
