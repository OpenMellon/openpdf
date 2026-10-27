// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package render

import (
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/annotator"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/bitwise"
	"github.com/OpenMellon/openpdf/internal/license"
	"github.com/OpenMellon/openpdf/internal/streamlimits"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/OpenMellon/openpdf/render/internal/context/imagerender"
	"github.com/adrg/sysfont"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

const maxPatchCount = 1 << 18

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

// RenderWithOpts converts the specified PDF page into an image, optionally flattens annotations and returns the result.
func (d *ImageDevice) RenderWithOpts(page *model.PdfPage, skipFlattening bool) (image.Image, error) {
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return nil, err
	}
	mediaBox.Normalize()
	cropBox := page.CropBox
	var cropW, cropH float64
	if cropBox != nil {
		cropBox.Normalize()
		cropW, cropH = cropBox.Width(), cropBox.Height()
	}
	rotate := page.Rotate
	originX, originY, pageW, pageH := mediaBox.Llx, mediaBox.Lly, mediaBox.Width(), mediaBox.Height()
	pageMatrix := transform.IdentityMatrix()
	if rotate != nil && *rotate%360 != 0 && *rotate%90 == 0 {
		angle := -float64(*rotate)
		rotated := rotatedBBox(pageW, pageH, angle)
		pageMatrix = pageMatrix.Translate((rotated.Width-pageW)/2+pageW/2, (rotated.Height-pageH)/2+pageH/2).Rotate(angle*math.Pi/180).Translate(-pageW/2, -pageH/2)
		pageW, pageH = rotated.Width, rotated.Height
		if cropBox != nil {
			rotatedCrop := rotatedBBox(cropW, cropH, angle)
			cropW, cropH = rotatedCrop.Width, rotatedCrop.Height
		}
	}
	if originX != 0 || originY != 0 {
		pageMatrix = pageMatrix.Translate(-originX, -originY)
	}
	d.scale = 1.0
	if d.OutputWidth != 0 {
		baseW := pageW
		if cropBox != nil {
			baseW = cropW
		}
		d.scale = float64(d.OutputWidth) / baseW
		pageW, pageH, cropW, cropH = pageW*d.scale, pageH*d.scale, cropW*d.scale, cropH*d.scale
		pageMatrix = transform.ScaleMatrix(d.scale, d.scale).Mult(pageMatrix)
	}
	ctx := imagerender.NewContext(int(pageW), int(pageH))
	ctx.SetInterpolator(d.interpolator)
	ctx.SetGlyphCache(d.glyphCache)
	defer ctx.ReleaseBuffers()
	if err := d.renderPage(ctx, page, pageMatrix, skipFlattening); err != nil {
		return nil, err
	}
	img := ctx.Image()
	if cropBox != nil {
		cropX, cropY := (cropBox.Llx-originX)*d.scale, (cropBox.Lly-originY)*d.scale
		cropRect := image.Rect(0, 0, int(cropW), int(cropH))
		srcPt := image.Pt(int(cropX), int(pageH-cropY-cropH))
		cropped := image.NewRGBA(cropRect)
		imagedraw.Draw(cropped, cropRect, img, srcPt, imagedraw.Src)
		img = cropped
	}
	return img, nil
}
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
func (s *meshShadingSpec) vertexBits() int {
	return 2*s.bitsPerCoordinate + s.numComps*s.bitsPerComponent
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
func systemFontFinder() *sysfont.Finder {
	if f := systemFontFinderCache.Load(); f != nil {
		return f
	}
	systemFontFinderMu.Lock()
	defer systemFontFinderMu.Unlock()
	if f := systemFontFinderCache.Load(); f != nil {
		return f
	}
	f := sysfont.NewFinder(&sysfont.FinderOpts{Extensions: []string{".ttf", ".ttc"}})
	systemFontFinderCache.Store(f)
	return f
}
func (r *renderer) noteDecodeLimit(err error) {
	if r.decodeLimitErr == nil && errors.Is(err, core.ErrDecodedSizeLimitExceeded) {
		r.decodeLimitErr = err
	}
}
func (r *renderer) renderContentStream(ctx context.Context, content string, res *model.PdfPageResources) error {
	return r.renderContentStreamWithBase(ctx, content, res, ctx.Matrix())
}

// Render converts the specified PDF page into an image, flattens annotations by default and returns the result.
func (d *ImageDevice) Render(page *model.PdfPage) (image.Image, error) {
	return d.RenderWithOpts(page, false)
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
func setFillFromColorspace(ctx context.Context, cs model.PdfColorspace, c model.PdfColor) error {
	if isPatternColorspace(cs) {
		return nil
	}
	rgb, err := cs.ColorToRGB(c)
	if err != nil {
		return err
	}
	if rgbColor, ok := rgb.(*model.PdfColorDeviceRGB); ok {
		ctx.SetFillRGBA(rgbColor.R(), rgbColor.G(), rgbColor.B(), 1)
	}
	return nil
}
func fontStyleHints(fontName string, desc *model.PdfFontDescriptor) (bold, italic, serif bool) {
	lower := strings.ToLower(fontName)
	if strings.Contains(lower, "italic") || strings.Contains(lower, "oblique") {
		italic = true
	}
	if strings.Contains(lower, "bold") {
		bold = true
	}
	if desc != nil {
		if flags, ok := core.GetIntVal(desc.Flags); ok {
			if flags&flagItalic != 0 {
				italic = true
			}
			if flags&flagForceBold != 0 {
				bold = true
			}
			if flags&flagSerif != 0 {
				serif = true
			}
		}
		if angle, err := core.GetNumberAsFloat(desc.ItalicAngle); err == nil && angle != 0 {
			italic = true
		}
		if weight, err := core.GetNumberAsFloat(desc.FontWeight); err == nil && weight >= 600 {
			bold = true
		}
	}
	if strings.Contains(lower, "sans") {
		serif = false
	} else if strings.Contains(lower, "serif") {
		serif = true
	}
	return bold, italic, serif
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

const (
	ShadingTypeFunctionBased PdfShadingType = 1
	ShadingTypeAxial         PdfShadingType = 2
	ShadingTypeRadial        PdfShadingType = 3
	ShadingTypeFreeForm      PdfShadingType = 4
	ShadingTypeLatticeForm   PdfShadingType = 5
	ShadingTypeCoons         PdfShadingType = 6
	ShadingTypeTensorProduct PdfShadingType = 7
)
const meshLUTSize = 1024

func matrixOrIdentity(arr *core.PdfObjectArray) transform.Matrix {
	m, err := transform.MatrixFromPdfObject(arr)
	if err != nil {
		common.Log.Debug("Invalid shading Matrix, using identity: %v", err)
	}
	return m
}
func (r *renderer) renderOperations(ctx context.Context, ops *contentstream.ContentStreamOperations, pageRes *model.PdfPageResources, baseMatrix transform.Matrix) error {
	prevBase := r.baseMatrix
	r.baseMatrix = baseMatrix
	defer func() { r.baseMatrix = prevBase }()
	if r.fontByName == nil {
		r.fontByName = map[string]*context.TextFont{}
	}
	if r.textFontByDict == nil {
		r.textFontByDict = map[*core.PdfObjectDictionary]*context.TextFont{}
	}
	if r.pdfFontByDict == nil {
		r.pdfFontByDict = map[*core.PdfObjectDictionary]*model.PdfFont{}
	}
	ts := ctx.TextState()
	ts.GlobalScale = r.scale
	finder := systemFontFinder()
	var prevOp *contentstream.ContentStreamOperation
	var clipPending bool
	var clipRule context.FillRule
	depth := 0
	proc := contentstream.NewContentStreamProcessor(*ops)
	proc.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", func(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, res *model.PdfPageResources) error {
		if r.decodeLimitErr != nil {
			return r.decodeLimitErr
		}
		common.Log.Debug("Processing %s", op.Operand)
		switch op.Operand {
		case "q":
			ctx.Push()
			depth++
		case "Q":
			if depth == 0 {
				common.Log.Debug("Unbalanced Q operator; ignoring")
				break
			}
			ctx.Pop()
			depth--
			ts = ctx.TextState()
		case "cm":
			if len(op.Params) != 6 {
				return errRangeCheck
			}
			vals, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			m := transform.NewMatrix(vals[0], vals[1], vals[2], vals[3], vals[4], vals[5])
			common.Log.Debug("Graphics state matrix: %+v", m)
			ctx.SetMatrix(ctx.Matrix().Mult(m))
		case "w":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			widthVals, widthErr := core.GetNumbersAsFloat(op.Params)
			if widthErr != nil {
				return widthErr
			}
			ctx.SetLineWidth(widthVals[0])
		case "J":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			capStyle, capOK := core.GetIntVal(op.Params[0])
			if !capOK {
				return errTypeCheck
			}
			switch capStyle {
			case 0:
				ctx.SetLineCap(context.LineCapButt)
			case 1:
				ctx.SetLineCap(context.LineCapRound)
			case 2:
				ctx.SetLineCap(context.LineCapSquare)
			default:
				common.Log.Debug("Invalid line cap style: %d", capStyle)
				return errRangeCheck
			}
		case "j":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			joinStyle, joinOK := core.GetIntVal(op.Params[0])
			if !joinOK {
				return errTypeCheck
			}
			switch joinStyle {
			case 0:
				ctx.SetLineJoin(context.LineJoinBevel)
			case 1:
				ctx.SetLineJoin(context.LineJoinRound)
			case 2:
				ctx.SetLineJoin(context.LineJoinBevel)
			default:
				common.Log.Debug("Invalid line join style: %d", joinStyle)
				return errRangeCheck
			}
		case "M":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			miterVals, miterErr := core.GetNumbersAsFloat(op.Params)
			if miterErr != nil {
				return miterErr
			}
			_ = miterVals
			common.Log.Debug("Miter limit not supported")
		case "d":
			if len(op.Params) != 2 {
				return errRangeCheck
			}
			dashArr, dashOK := core.GetArray(op.Params[0])
			if !dashOK {
				return errTypeCheck
			}
			dashPhase, dashOK := core.GetIntVal(op.Params[1])
			if !dashOK {
				_, phaseFloatOK := core.GetFloatVal(op.Params[1])
				if !phaseFloatOK {
					return errTypeCheck
				}
			}
			dashes, dashErr := core.GetNumbersAsFloat(dashArr.Elements())
			if dashErr != nil {
				return dashErr
			}
			ctx.SetDash(dashes...)
			_ = dashPhase
			common.Log.Debug("Line dash phase not supported")
		case "ri":
			common.Log.Debug("Rendering intent not supported")
		case "i":
			common.Log.Debug("Flatness tolerance not supported")
		case "gs":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			gsName, gsOK := core.GetName(op.Params[0])
			if !gsOK {
				return errTypeCheck
			}
			if gsName == nil {
				return errRangeCheck
			}
			gsObj, gsOK := res.GetExtGState(*gsName)
			if !gsOK {
				common.Log.Debug("ERROR: could not find resource: %s", *gsName)
				return errors.New("resource not found")
			}
			gsDict, gsOK := core.GetDict(gsObj)
			if !gsOK {
				common.Log.Debug("ERROR: could get graphics state dict")
				return errTypeCheck
			}
			common.Log.Debug("GS dict: %s", gsDict.String())
			if bm := gsDict.Get("BM"); bm != nil {
				ctx.SetBlendMode(blendModeFromObject(bm))
			}
			if strokeAlphaObj := gsDict.Get("CA"); strokeAlphaObj != nil {
				strokeAlpha, strokeAlphaErr := core.GetNumberAsFloat(strokeAlphaObj)
				if strokeAlphaErr == nil {
					ctx.SetStrokeAlpha(strokeAlpha)
				}
				if strokeAlphaErr == nil && !isPatternColorspace(gs.ColorspaceStroking) {
					strokeRGB, strokeConvErr := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
					if strokeConvErr != nil {
						common.Log.Debug("Error converting color: %v", strokeConvErr)
						return strokeConvErr
					}
					if strokeDevRGB, strokeRGBOK := strokeRGB.(*model.PdfColorDeviceRGB); strokeRGBOK {
						ctx.SetStrokeRGBA(strokeDevRGB.R(), strokeDevRGB.G(), strokeDevRGB.B(), strokeAlpha)
					}
				}
			}
			fillAlphaObj := gsDict.Get("ca")
			if fillAlphaObj != nil {
				fillAlpha, fillAlphaErr := core.GetNumberAsFloat(fillAlphaObj)
				if fillAlphaErr == nil {
					ctx.SetFillAlpha(fillAlpha)
				}
				if fillAlphaErr == nil && !isPatternColorspace(gs.ColorspaceNonStroking) {
					fillRGB, fillConvErr := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
					if fillConvErr != nil {
						common.Log.Debug("Error converting color: %v", fillConvErr)
						return fillConvErr
					}
					if fillDevRGB, fillRGBOK := fillRGB.(*model.PdfColorDeviceRGB); fillRGBOK {
						ctx.SetFillRGBA(fillDevRGB.R(), fillDevRGB.G(), fillDevRGB.B(), fillAlpha)
					}
				}
			}
			if smaskObj := gsDict.Get("SMask"); smaskObj != nil {
				if smaskName, smaskNameOK := core.GetName(smaskObj); smaskNameOK && smaskName.String() == "None" {
					ctx.SetSoftMask(nil)
				} else {
					mask, maskErr := r.buildSoftMask(ctx, smaskObj, res)
					r.noteDecodeLimit(maskErr)
					if errors.Is(maskErr, errSoftMaskBudget) {
						ctx.SetSoftMask(nil)
					} else if maskErr != nil {
						common.Log.Debug("Error building soft mask: %v", maskErr)
					} else if mask != nil {
						ctx.SetSoftMask(mask)
					}
				}
			}
		case "m":
			if len(op.Params) != 2 {
				common.Log.Debug("WARN: error while processing `m` operator: %s. Output may be incorrect.", errRangeCheck)
				return nil
			}
			moveVals, moveErr := core.GetNumbersAsFloat(op.Params)
			if moveErr != nil {
				return moveErr
			}
			common.Log.Debug("Move to: %v", moveVals)
			ctx.NewSubPath()
			ctx.MoveTo(moveVals[0], moveVals[1])
		case "l":
			if len(op.Params) != 2 {
				common.Log.Debug("WARN: error while processing `l` operator: %s. Output may be incorrect.", errRangeCheck)
				return nil
			}
			lineVals, lineErr := core.GetNumbersAsFloat(op.Params)
			if lineErr != nil {
				return lineErr
			}
			ctx.LineTo(lineVals[0], lineVals[1])
		case "c":
			if len(op.Params) != 6 {
				return errRangeCheck
			}
			curveVals, curveErr := core.GetNumbersAsFloat(op.Params)
			if curveErr != nil {
				return curveErr
			}
			common.Log.Debug("Cubic bezier params: %+v", curveVals)
			ctx.CubicTo(curveVals[0], curveVals[1], curveVals[2], curveVals[3], curveVals[4], curveVals[5])
		case "v", "y":
			if len(op.Params) != 4 {
				return errRangeCheck
			}
			quadVals, quadErr := core.GetNumbersAsFloat(op.Params)
			if quadErr != nil {
				return quadErr
			}
			common.Log.Debug("Cubic bezier params: %+v", quadVals)
			ctx.QuadraticTo(quadVals[0], quadVals[1], quadVals[2], quadVals[3])
		case "h":
			ctx.ClosePath()
			ctx.NewSubPath()
		case "re":
			if len(op.Params) != 4 {
				return errRangeCheck
			}
			rectVals, rectErr := core.GetNumbersAsFloat(op.Params)
			if rectErr != nil {
				return rectErr
			}
			ctx.DrawRectangle(rectVals[0], rectVals[1], rectVals[2], rectVals[3])
			ctx.NewSubPath()
		case "S":
			if err := setStrokeFromColorspace(ctx, gs.ColorspaceStroking, gs.ColorStroking); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
			ctx.Stroke()
		case "s":
			if err := setStrokeFromColorspace(ctx, gs.ColorspaceStroking, gs.ColorStroking); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
			ctx.ClosePath()
			ctx.NewSubPath()
			ctx.Stroke()
		case "f", "F":
			if err := setFillFromColorspace(ctx, gs.ColorspaceNonStroking, gs.ColorNonStroking); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
			ctx.SetFillRule(context.FillRuleWinding)
			ctx.Fill()
		case "f*":
			if err := setFillFromColorspace(ctx, gs.ColorspaceNonStroking, gs.ColorNonStroking); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
			ctx.SetFillRule(context.FillRuleEvenOdd)
			ctx.Fill()
		case "B":
			if err := fillAndStroke(ctx, gs, context.FillRuleWinding, false); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
		case "B*":
			if err := fillAndStroke(ctx, gs, context.FillRuleEvenOdd, false); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
		case "b":
			ctx.ClosePath()
			if err := fillAndStroke(ctx, gs, context.FillRuleWinding, true); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
		case "b*":
			ctx.ClosePath()
			if err := fillAndStroke(ctx, gs, context.FillRuleEvenOdd, true); err != nil {
				common.Log.Debug("Error converting color: %v", err)
				return err
			}
		case "n":
			if clipPending {
				ctx.SetFillRule(clipRule)
				ctx.ClipPreserve()
				clipPending = false
			}
			ctx.ClearPath()
		case "W":
			clipPending = true
			clipRule = context.FillRuleWinding
		case "W*":
			clipPending = true
			clipRule = context.FillRuleEvenOdd
		case "rg":
			devRGB, ok := gs.ColorNonStroking.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "RG":
			devRGB, ok := gs.ColorStroking.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "k":
			cmyk, ok := gs.ColorNonStroking.(*model.PdfColorDeviceCMYK)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(cmyk)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "K":
			cmyk, ok := gs.ColorStroking.(*model.PdfColorDeviceCMYK)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			rgb, err := gs.ColorspaceStroking.ColorToRGB(cmyk)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "g":
			gray, ok := gs.ColorNonStroking.(*model.PdfColorDeviceGray)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(gray)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "G":
			gray, ok := gs.ColorStroking.(*model.PdfColorDeviceGray)
			if !ok {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			rgb, err := gs.ColorspaceStroking.ColorToRGB(gray)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "cs":
			if len(op.Params) > 0 {
				if name, ok := core.GetName(op.Params[0]); ok && name.String() == "Pattern" {
					break
				}
			}
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "sc":
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "scn":
			if pattern, isShading := r.scnPattern(ctx, op, prevOp, gs.ColorspaceNonStroking, gs.ColorNonStroking, res); pattern != nil {
				ctx.SetFillStyle(pattern)
				if isShading {
					ctx.SetStrokeStyle(pattern)
				}
				break
			}
			rgb, err := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorNonStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetFillRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "CS":
			if len(op.Params) > 0 {
				if name, ok := core.GetName(op.Params[0]); ok && name.String() == "Pattern" {
					break
				}
			}
			rgb, err := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "SC":
			rgb, err := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "SCN":
			if pattern, isShading := r.scnPattern(ctx, op, prevOp, gs.ColorspaceStroking, gs.ColorStroking, res); pattern != nil {
				ctx.SetStrokeStyle(pattern)
				if isShading {
					ctx.SetFillStyle(pattern)
				}
				break
			}
			rgb, err := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
			if err != nil {
				common.Log.Debug("Error converting color: %v", gs.ColorStroking)
				return nil
			}
			devRGB, ok := rgb.(*model.PdfColorDeviceRGB)
			if !ok {
				common.Log.Debug("Error converting color: %v", rgb)
				return nil
			}
			ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
		case "sh":
			if len(op.Params) != 1 {
				common.Log.Debug("Invalid sh params format")
				break
			}
			shadingName, ok := core.GetName(op.Params[0])
			if !ok {
				common.Log.Debug("Failed getting shading name")
				break
			}
			shading, ok := res.GetShadingByName(*shadingName)
			if !ok {
				common.Log.Debug("Failed getting shading data")
				break
			}
			pattern, bboxArr, err := r.processShading(ctx, shading, nil)
			if err != nil {
				r.noteDecodeLimit(err)
				common.Log.Debug("Error when processing shading data: %v", err)
				break
			}
			if pattern == nil {
				common.Log.Debug("Unknown shading")
				break
			}
			switch PdfShadingType(*shading.ShadingType) {
			case ShadingTypeAxial, ShadingTypeRadial:
				r.paintShadingClip(ctx, shading, pattern)
			default:
				bbox, bboxErr := bboxArr.ToFloat64Array()
				if bboxErr != nil {
					common.Log.Debug("Error parsing coordinates: %v", bboxErr)
					break
				}
				ctx.SavePath()
				ctx.Push()
				ctx.DrawRectangle(bbox[0], bbox[1], bbox[2], bbox[3])
				ctx.NewSubPath()
				ctx.SetFillStyle(pattern)
				ctx.SetStrokeStyle(pattern)
				ctx.Fill()
				ctx.Pop()
				ctx.RestorePath()
			}
		case "Do":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			name, nameOK := core.GetName(op.Params[0])
			if !nameOK {
				return errTypeCheck
			}
			xobj, xobjType := res.GetXObjectByName(*name)
			switch xobjType {
			case model.XObjectTypeImage:
				common.Log.Debug("XObject image: %s", name.String())
				xi, err := res.GetXObjectImageByName(*name)
				if err != nil {
					return err
				}
				img, err := xi.ToImage()
				if err != nil {
					r.noteDecodeLimit(err)
					common.Log.Debug("Rendering result may be incomplete. Image conversion error: %v", err)
					return nil
				}
				if cs := xi.ColorSpace; cs != nil {
					var indexed bool
					switch cs.(type) {
					case *model.PdfColorspaceSpecialIndexed:
						indexed = true
					}
					if indexed {
						if rgbImg, convErr := cs.ImageToRGB(*img); convErr != nil {
							common.Log.Debug("WARN: could not convert image to RGB. Output may be incorrect.")
						} else {
							img = &rgbImg
						}
					}
				}
				fillColor := ctx.FillPattern().ColorAt(0, 0)
				forceOpaque := false
				var maskImg image.Image
				if xi.Mask != nil {
					if maskImg, err = explicitMaskImage(xi.Mask, fillColor); err != nil {
						r.noteDecodeLimit(err)
						common.Log.Debug("WARN: could not get explicit image mask. Output may be incorrect.")
						maskImg = nil
					} else if maskImg != nil {
						forceOpaque = true
					}
				} else if xi.SMask != nil {
					if maskImg, err = softMaskImage(xi.SMask); err != nil {
						r.noteDecodeLimit(err)
						common.Log.Debug("WARN: could not get soft image mask. Output may be incorrect.")
						maskImg = nil
					}
				}
				var goImg image.Image
				if isMask, _ := core.GetBoolVal(xi.ImageMask); isMask {
					goImg = stencilMaskImage(img, fillColor)
					forceOpaque = true
				} else {
					goImg, err = img.ToGoImage()
					if err != nil {
						common.Log.Debug("Rendering result may be incomplete. Image conversion error: %v", err)
						return nil
					}
				}
				if maskImg != nil {
					var interp xdraw.Interpolator = xdraw.BiLinear
					if imgCtx, isImgCtx := ctx.(*imagerender.Context); isImgCtx {
						interp = imgCtx.Interpolator()
					}
					goImg = applyImageMask(goImg, maskImg, interp)
				}
				bounds := goImg.Bounds()
				ctx.Push()
				ctx.Scale(1.0/float64(bounds.Dx()), -1.0/float64(bounds.Dy()))
				if forceOpaque {
					ctx.SetFillAlpha(1)
				}
				ctx.DrawImageAnchored(goImg, 0, 0, 0, 1)
				ctx.Pop()
			case model.XObjectTypeForm:
				common.Log.Debug("XObject form: %s", name.String())
				if !r.limits.Enter(xobj, "XObject form "+name.String()) {
					return nil
				}
				defer r.limits.Leave(xobj)
				form, err := res.GetXObjectFormByName(*name)
				if err != nil {
					return err
				}
				content, err := form.GetContentStream()
				if err != nil {
					return err
				}
				formOps, err := contentstream.NewContentStreamParser(string(content)).Parse()
				if err != nil {
					return err
				}
				formRes := form.Resources
				if formRes == nil {
					formRes = res
				}
				var group *core.PdfObjectDictionary
				if groupDict, groupOK := core.GetDict(form.Group); groupOK {
					if groupType, groupTypeOK := core.GetName(groupDict.Get("S")); groupTypeOK && groupType.String() == "Transparency" {
						group = groupDict
					}
				}
				groups := 0
				if group != nil {
					groups = 1
				}
				r.limits.Charge(xobj, len(*formOps), len(content), groups)
				if group != nil {
					ctx.BeginTransparencyGroup(r.transparencyGroupAttrs(group))
					defer ctx.EndTransparencyGroup()
				}
				ctx.Push()
				defer ctx.Pop()
				if form.Matrix != nil {
					formMatrix, matrixErr := transform.MatrixFromPdfObject(form.Matrix)
					if matrixErr != nil {
						return matrixErr
					}
					ctx.SetMatrix(ctx.Matrix().Mult(formMatrix))
				}
				if form.BBox != nil {
					bboxArr, bboxOK := core.GetArray(form.BBox)
					if !bboxOK {
						return errTypeCheck
					}
					bbox, bboxErr := core.GetNumbersAsFloat(bboxArr.Elements())
					if bboxErr != nil {
						return bboxErr
					}
					if len(bbox) != 4 {
						common.Log.Debug("Len = %d", len(bbox))
						return errRangeCheck
					}
					ctx.DrawRectangle(bbox[0], bbox[1], bbox[2]-bbox[0], bbox[3]-bbox[1])
					ctx.Clip()
				} else {
					common.Log.Debug("ERROR: Required BBox missing on XObject Form")
				}
				if renderErr := r.renderOperations(ctx, formOps, formRes, ctx.Matrix()); renderErr != nil {
					return renderErr
				}
			}
		case "BI":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			inline, ok := op.Params[0].(*contentstream.ContentStreamInlineImage)
			if !ok {
				return nil
			}
			img, err := inline.ToImage(res)
			if err != nil {
				r.noteDecodeLimit(err)
				common.Log.Debug("Rendering result may be incomplete. Image conversion error: %v", err)
				return nil
			}
			goImg, err := img.ToGoImage()
			if err != nil {
				common.Log.Debug("Rendering result may be incomplete. Image conversion error: %v", err)
				return nil
			}
			bounds := goImg.Bounds()
			ctx.Push()
			ctx.Scale(1.0/float64(bounds.Dx()), -1.0/float64(bounds.Dy()))
			ctx.DrawImageAnchored(goImg, 0, 0, 0, 1)
			ctx.Pop()
		case "BT":
			ts.Reset()
		case "ET":
			ts.Reset()
		case "Tr":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			mode, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			ts.Tr = context.TextRenderingMode(mode)
		case "TL":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			leading, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			ts.Tl = leading
		case "Tc":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			charSpace, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			common.Log.Debug("Tc: %v", charSpace)
			ts.Tc = charSpace
		case "Tw":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			wordSpace, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			common.Log.Debug("Tw: %v", wordSpace)
			ts.Tw = wordSpace
		case "Tz":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			scale, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			ts.Th = scale
		case "Ts":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			rise, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			ts.Ts = rise
		case "Td":
			if len(op.Params) != 2 {
				return errRangeCheck
			}
			vals, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("Td: %v", vals)
			ts.ProcTd(vals[0], vals[1])
		case "TD":
			if len(op.Params) != 2 {
				return errRangeCheck
			}
			vals, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("TD: %v", vals)
			ts.ProcTD(vals[0], vals[1])
		case "T*":
			ts.ProcTStar()
		case "Tm":
			if len(op.Params) != 6 {
				return errRangeCheck
			}
			vals, err := core.GetNumbersAsFloat(op.Params)
			if err != nil {
				return err
			}
			common.Log.Debug("Text matrix: %+v", vals)
			ts.ProcTm(vals[0], vals[1], vals[2], vals[3], vals[4], vals[5])
		case "'":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			text, ok := core.GetStringBytes(op.Params[0])
			if !ok {
				return errTypeCheck
			}
			common.Log.Debug("' string: %s", string(text))
			ts.ProcQ(text, ctx)
		case "\"":
			if len(op.Params) != 3 {
				return errRangeCheck
			}
			wordSpacing, err := core.GetNumberAsFloat(op.Params[0])
			if err != nil {
				return err
			}
			charSpacing, err := core.GetNumberAsFloat(op.Params[1])
			if err != nil {
				return err
			}
			text, ok := core.GetStringBytes(op.Params[2])
			if !ok {
				return errTypeCheck
			}
			ts.ProcDQ(text, wordSpacing, charSpacing, ctx)
		case "Tj":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			text, ok := core.GetStringBytes(op.Params[0])
			if !ok {
				return errTypeCheck
			}
			common.Log.Debug("Tj string: `%s`", string(text))
			ts.ProcTj(text, ctx)
		case "TJ":
			if len(op.Params) != 1 {
				return errRangeCheck
			}
			arr, ok := core.GetArray(op.Params[0])
			if !ok {
				common.Log.Debug("Type: %T", arr)
				return errTypeCheck
			}
			common.Log.Debug("TJ array: %+v", arr)
			for _, elem := range arr.Elements() {
				switch v := elem.(type) {
				case *core.PdfObjectString:
					if v != nil {
						ts.ProcTj(v.Bytes(), ctx)
					}
				case *core.PdfObjectFloat, *core.PdfObjectInteger:
					adjust, err := core.GetNumberAsFloat(v)
					if err == nil {
						ts.Translate(-adjust*0.001*ts.Tf.Size*ts.Th/100.0, 0)
					}
				}
			}
		case "Tf":
			if len(op.Params) != 2 {
				return errRangeCheck
			}
			common.Log.Debug("%#v", op.Params)
			fontName, ok := core.GetName(op.Params[0])
			if !ok || fontName == nil {
				common.Log.Debug("invalid font name object: %v", op.Params[0])
				return errTypeCheck
			}
			common.Log.Debug("Font name: %s", fontName.String())
			size, err := core.GetNumberAsFloat(op.Params[1])
			if err != nil {
				common.Log.Debug("invalid font size object: %v", op.Params[1])
				return errTypeCheck
			}
			maxDim := ctx.Width()
			if ctx.Height() > maxDim {
				maxDim = ctx.Height()
			}
			maxSize := float64(maxDim)
			if size > maxSize {
				common.Log.Debug("Font size exceeds image dimension, scaling down font size from %v to %v", size, maxSize)
				size = maxSize
			}
			common.Log.Debug("Font size: %v", size)
			fontObj, fontFound := res.GetFontByName(*fontName)
			if !fontFound {
				common.Log.Debug("ERROR: Font %s not found", fontName.String())
				return errors.New("font not found")
			}
			common.Log.Debug("Font: %T", fontObj)
			fontDict, ok := core.GetDict(fontObj)
			if !ok {
				common.Log.Debug("ERROR: could not get font dict")
				return errTypeCheck
			}
			pdfFont, ok := r.pdfFontByDict[fontDict]
			if !ok {
				pdfFont, err = model.NewPdfFontFromPdfObject(fontDict)
				if err != nil {
					common.Log.Debug("ERROR: could not load font from object")
					return err
				}
				r.pdfFontByDict[fontDict] = pdfFont
			}
			baseFont := pdfFont.BaseFont()
			if baseFont == "" {
				baseFont = fontName.String()
			}
			textFont, ok := r.textFontByDict[fontDict]
			if !ok {
				textFont, err = context.NewTextFont(pdfFont, size)
				if err != nil {
					r.noteDecodeLimit(err)
					if r.decodeLimitErr != nil {
						return r.decodeLimitErr
					}
					common.Log.Debug("ERROR: %v", err)
				}
			}
			if textFont == nil {
				if cached, cachedOK := r.fontByName[baseFont]; cachedOK {
					textFont = cached
				}
			}
			if textFont == nil {
				origName := baseFont
				if len(baseFont) > 7 && baseFont[6] == '+' {
					baseFont = baseFont[7:]
				}
				bold, italic, serif := fontStyleHints(baseFont, pdfFont.FontDescriptor())
				candidates := fontNameCandidates(baseFont, bold, italic, serif)
				loadFont := func(sf *sysfont.Font, cacheKey string) *context.TextFont {
					if shared, sharedOK := fontFileCache.Load(sf.Filename); sharedOK {
						return shared.(*context.TextFont).Clone()
					}
					font, loadErr := context.NewTextFontFromPath(sf.Filename, size)
					if loadErr != nil {
						common.Log.Debug("could not load font file %s", sf.Filename)
						if len(r.fontByName) >= maxFontCacheEntries {
							clear(r.fontByName)
						}
						r.fontByName[cacheKey] = nil
						return nil
					}
					if stored, loaded := fontFileCache.LoadOrStore(sf.Filename, font); loaded {
						font = stored.(*context.TextFont)
					}
					common.Log.Debug("Substituting font %s with %s (%s)", baseFont, sf.Name, sf.Filename)
					return font.Clone()
				}
				styled := bold || italic
				var fallback *sysfont.Font
				var fallbackName string
				for _, candidate := range candidates {
					if entry, entryOK := r.fontByName[candidate]; entryOK && entry == nil {
						continue
					}
					match := finder.Match(candidate)
					if match == nil {
						if common.Log.IsLogLevel(common.LogLevelDebug) {
							common.Log.Debug("could not find font file %s", candidate)
						}
						continue
					}
					if styled && !fontNameMatchesStyle(match.Name, bold, italic) {
						if fallback == nil {
							fallback, fallbackName = match, candidate
						}
						continue
					}
					if loaded := loadFont(match, candidate); loaded != nil {
						textFont = loaded
						break
					}
				}
				if textFont == nil && fallback != nil {
					textFont = loadFont(fallback, fallbackName)
				}
				if textFont != nil {
					if len(r.fontByName) >= maxFontCacheEntries {
						clear(r.fontByName)
					}
					r.fontByName[origName] = textFont
				}
			}
			if textFont != nil {
				if _, inDictCache := r.textFontByDict[fontDict]; !inDictCache {
					if len(r.textFontByDict) >= maxFontDictCacheEntries {
						clear(r.textFontByDict)
					}
					r.textFontByDict[fontDict] = textFont
				}
			}
			if textFont == nil {
				common.Log.Debug("ERROR: could not find any suitable font")
				return errors.New("could not find any suitable font")
			}
			ts.ProcTf(textFont.WithSize(size, pdfFont))
		case "BMC", "BDC":
			if len(op.Params) == 2 && op.Params[0].String() == "Artifact" {
				props, propsOK := core.GetDict(op.Params[1])
				if propsOK {
					if props.Get("Subtype") != nil && props.Get("Subtype").String() == "Watermark" {
						ts.Tr = context.TextRenderingModeInvisible
					}
				}
			}
		case "EMC":
		default:
			common.Log.Debug("ERROR: unsupported operand: %s", op.Operand)
		}
		prevOp = op
		return nil
	})
	if err := proc.Process(pageRes); err != nil {
		return err
	}
	return r.decodeLimitErr
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
func coonsInteriorPoints(p *[4][4]transform.Point) {
	for _, ci := range [2]int{0, 3} {
		for _, cj := range [2]int{0, 3} {
			ri, rj := 1+ci/3, 1+cj/3
			oi, oj := 3-ci, 3-cj
			p[ri][rj] = transform.NewPoint((-4*p[ci][cj].X+6*(p[ci][rj].X+p[ri][cj].X)-2*(p[ci][oj].X+p[oi][cj].X)+3*(p[oi][rj].X+p[ri][oj].X)-p[oi][oj].X)/9, (-4*p[ci][cj].Y+6*(p[ci][rj].Y+p[ri][cj].Y)-2*(p[ci][oj].Y+p[oi][cj].Y)+3*(p[oi][rj].Y+p[ri][oj].Y)-p[oi][oj].Y)/9)
		}
	}
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
func fontNameCandidates(name string, bold, italic, serif bool) []string {
	var suffixes []string
	switch {
	case bold && italic:
		suffixes = []string{" Bold Italic", " Bold Oblique"}
	case bold:
		suffixes = []string{" Bold"}
	case italic:
		suffixes = []string{" Italic", " Oblique"}
	default:
		suffixes = []string{""}
	}
	families := []string{"Arial", "DejaVu Sans", "Times New Roman"}
	if serif {
		families = []string{"Times New Roman", "DejaVu Serif", "Arial", "DejaVu Sans"}
	}
	out := []string{name}
	for _, family := range families {
		for _, suffix := range suffixes {
			out = append(out, family+suffix)
		}
		if bold || italic {
			out = append(out, family)
		}
	}
	return out
}

const (
	flagSerif     = 0x00002
	flagItalic    = 0x00040
	flagForceBold = 0x40000
)

// NewImageDevice returns a new image device.
func NewImageDevice() *ImageDevice {
	const funcName = "render.NewImageDevice"
	license.TrackUse(funcName)
	d := &ImageDevice{renderer: &renderer{}, interpolator: xdraw.BiLinear, glyphCache: imagerender.NewGlyphCache()}
	return d
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

// ResetSharedFontCaches clears the process-wide shared font finder and file caches.
// Future cache misses will rescan system fonts and reload font files from disk.
func ResetSharedFontCaches() {
	systemFontFinderMu.Lock()
	systemFontFinderCache.Store(nil)
	systemFontFinderMu.Unlock()
	fontFileCache.Range(func(key, _ any) bool { fontFileCache.Delete(key); return true })
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

const maxSoftMaskDepth = 8

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

// PdfShadingType defines PDF shading types.
// Source: PDF32000_2008.pdf. Chapter 8.7.4.5
type PdfShadingType int64

func (r *renderer) renderPage(ctx context.Context, page *model.PdfPage, matrix transform.Matrix, skipFlattening bool) error {
	r.limits = streamlimits.Walker{MinRepeatCost: streamlimits.RenderRepeatCost}
	r.decodeLimitErr = nil
	if !skipFlattening {
		opts := model.FieldFlattenOpts{AnnotFilterFunc: func(annot *model.PdfAnnotation) bool {
			switch annot.GetContext().(type) {
			case *model.PdfAnnotationLine:
				return true
			case *model.PdfAnnotationSquare:
				return true
			case *model.PdfAnnotationCircle:
				return true
			case *model.PdfAnnotationPolygon:
				return true
			case *model.PdfAnnotationPolyLine:
				return true
			}
			return false
		}}
		appearance := annotator.FieldAppearance{}
		err := page.FlattenFieldsWithOpts(appearance, &opts)
		if err != nil {
			r.noteDecodeLimit(err)
			common.Log.Debug("Error during annotation flattening %v", err)
		}
	}
	content, err := page.GetAllContentStreams()
	if err != nil {
		return err
	}
	matrix = matrix.Round(0.01)
	if m := matrix; !m.Identity() {
		content = fmt.Sprintf("%.2f %.2f %.2f %.2f %.2f %.2f cm %s", m[0], m[1], m[3], m[4], m[6], m[7], content)
	}
	ctx.Translate(0, float64(ctx.Height()))
	ctx.Scale(1, -1)
	ctx.Push()
	ctx.SetRGBA(1, 1, 1, 1)
	ctx.DrawRectangle(0, 0, float64(ctx.Width()), float64(ctx.Height()))
	ctx.Fill()
	ctx.Pop()
	ctx.SetLineWidth(1.0)
	ctx.SetRGBA(0, 0, 0, 1)
	return r.renderContentStreamWithBase(ctx, content, page.Resources, ctx.Matrix().Mult(matrix))
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

const maxFontCacheEntries = 128

var (
	systemFontFinderCache atomic.Pointer[sysfont.Finder]
	systemFontFinderMu    sync.Mutex
	fontFileCache         sync.Map
)

type renderer struct {
	scale           float64
	fontByName      map[string]*context.TextFont
	textFontByDict  map[*core.PdfObjectDictionary]*context.TextFont
	pdfFontByDict   map[*core.PdfObjectDictionary]*model.PdfFont
	baseMatrix      transform.Matrix
	tilingCache     map[tilingCellKey]*image.RGBA
	tilingDepth     int
	softMaskDepth   int
	limits          streamlimits.Walker
	colorConverters map[*core.PdfObjectDictionary]context.ColorConverter
	decodeLimitErr  error
}

func unitToByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v * 255)
}

type meshDecoder struct {
	spec      *meshShadingSpec
	reader    *bitwise.Reader
	matrix    transform.Matrix
	colors    []float64
	bounds    [4]float64
	hasBounds bool
}

func softMaskImage(obj core.PdfObject) (image.Image, error) {
	stream, ok := core.GetStream(obj)
	if !ok {
		return nil, nil
	}
	xi, err := model.NewXObjectImageFromStream(stream)
	if err != nil {
		return nil, err
	}
	img, err := xi.ToImage()
	if err != nil {
		return nil, err
	}
	if img.Width == 0 || img.Height == 0 {
		common.Log.Debug("soft mask image has invalid dimensions (%d, %d). Output may be incorrect.", img.Width, img.Height)
		return nil, nil
	}
	return maskImageToRGBA(img), nil
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

var (
	errTypeCheck           = errors.New("type check error")
	errRangeCheck          = errors.New("range check error")
	errSoftMaskBudget      = errors.New("page nested stream budget spent")
	errNestedStreamRefused = errors.New("nested stream refused")
)

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
func groupColorConverter(cs model.PdfColorspace) context.ColorConverter {
	if cs == nil {
		return nil
	}
	switch cs.(type) {
	case *model.PdfColorspaceSpecialPattern, *model.PdfColorspaceSpecialIndexed, *model.PdfColorspaceSpecialSeparation, *model.PdfColorspaceDeviceN, *model.PdfColorspaceLab:
		common.Log.Debug("Transparency group /CS %T cannot be a group colour space; ignoring", cs)
		return nil
	}
	if n := cs.GetNumComponents(); n != 1 {
		return nil
	}
	return grayColorConverter(cs)
}
func grayColorConverter(cs model.PdfColorspace) context.ColorConverter {
	var rgbTable [256][3]uint8
	var grayTable [256]uint8
	for i := range rgbTable {
		rgb, err := colorToRGB8(cs, []float64{float64(i) / 255})
		if err != nil {
			common.Log.Debug("Transparency group /CS %T: %v", cs, err)
			return nil
		}
		rgbTable[i] = rgb
		grayTable[i] = luminanceGray(rgb[0], rgb[1], rgb[2])
	}
	var inverse [256]uint8
	for g := range inverse {
		best, bestDist := 0, 1<<31-1
		for i, v := range grayTable {
			d := int(v) - g
			if d < 0 {
				d = -d
			}
			if d < bestDist {
				best, bestDist = i, d
			}
		}
		inverse[g] = uint8(best)
	}
	return func(r, g, b uint8) (uint8, uint8, uint8) {
		c := rgbTable[inverse[luminanceGray(r, g, b)]]
		return c[0], c[1], c[2]
	}
}
func saveJPEG(path string, img image.Image, quality int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: quality})
}

type tilingPatternRequest struct {
	name  core.PdfObjectName
	color *model.PdfColorDeviceRGB
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

func isPatternColorspace(cs model.PdfColorspace) bool {
	_, ok := cs.(*model.PdfColorspaceSpecialPattern)
	return ok
}

const maxFontDictCacheEntries = 128

func (s *meshShadingSpec) colorTolerance() float64 {
	if !s.hasFunction {
		return 0
	}
	return math.Abs(s.decode[5]-s.decode[4]) / meshLUTSize
}
func fontNameMatchesStyle(name string, bold, italic bool) bool {
	lower := strings.ToLower(name)
	if bold && !strings.Contains(lower, "bold") {
		return false
	}
	if italic && !strings.Contains(lower, "italic") && !strings.Contains(lower, "oblique") {
		return false
	}
	return true
}

// RenderToPath converts the specified PDF page into an image and saves the
// result at the specified location.
func (d *ImageDevice) RenderToPath(page *model.PdfPage, outputPath string) error {
	img, err := d.Render(page)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(outputPath))
	if ext == "" {
		return errors.New("could not recognize output file type")
	}
	switch ext {
	case ".png":
		return savePNG(outputPath, img)
	case ".jpg", ".jpeg":
		return saveJPEG(outputPath, img, 100)
	}
	return fmt.Errorf("unrecognized output file type: %s", ext)
}

// ImageDevice is used to render PDF pages to image targets.
type ImageDevice struct {
	*renderer

	// OutputWidth represents the width of the rendered images in pixels.
	// The heights of the output images are calculated based on the selected
	// width and the original height of each rendered page.
	OutputWidth  int
	interpolator xdraw.Interpolator
	glyphCache   *imagerender.GlyphCache
}

func (r *renderer) renderContentStreamWithBase(ctx context.Context, content string, res *model.PdfPageResources, matrix transform.Matrix) error {
	ops, err := contentstream.NewContentStreamParser(content).Parse()
	if err != nil {
		return err
	}
	return r.renderOperations(ctx, ops, res, matrix)
}
func shadingDomain(arr *core.PdfObjectArray) []float64 {
	if arr != nil {
		if vals, err := arr.ToFloat64Array(); err == nil && len(vals) == 2 {
			return vals
		}
	}
	return []float64{0, 1}
}
func (s *meshShadingSpec) patchBytes(nPoints, nColors int) int {
	bits := s.bitsPerFlag + nPoints*2*s.bitsPerCoordinate + nColors*s.numComps*s.bitsPerComponent
	return (bits + 7) / 8
}

var sharedEdgePoints = [4][4][2]int{1: {{0, 3}, {1, 3}, {2, 3}, {3, 3}}, 2: {{3, 3}, {3, 2}, {3, 1}, {3, 0}}, 3: {{3, 0}, {2, 0}, {1, 0}, {0, 0}}}

func blendModeFromObject(obj core.PdfObject) context.BlendMode {
	byName := func(name *core.PdfObjectName) (context.BlendMode, bool) {
		switch name.String() {
		case "Multiply":
			return context.BlendModeMultiply, true
		case "Screen":
			return context.BlendModeScreen, true
		case "Overlay":
			return context.BlendModeOverlay, true
		case "Darken":
			return context.BlendModeDarken, true
		case "Lighten":
			return context.BlendModeLighten, true
		case "ColorDodge":
			return context.BlendModeColorDodge, true
		case "ColorBurn":
			return context.BlendModeColorBurn, true
		case "HardLight":
			return context.BlendModeHardLight, true
		case "SoftLight":
			return context.BlendModeSoftLight, true
		case "Difference":
			return context.BlendModeDifference, true
		case "Exclusion":
			return context.BlendModeExclusion, true
		case "Hue":
			return context.BlendModeHue, true
		case "Saturation":
			return context.BlendModeSaturation, true
		case "Color":
			return context.BlendModeColor, true
		case "Luminosity":
			return context.BlendModeLuminosity, true
		case "Normal", "Compatible":
			return context.BlendModeNormal, true
		}
		return context.BlendModeNormal, false
	}
	if name, ok := core.GetName(obj); ok {
		if mode, found := byName(name); found {
			return mode
		}
		common.Log.Debug("Unsupported blend mode %q; using Normal", name.String())
		return context.BlendModeNormal
	}
	if arr, ok := core.GetArray(obj); ok {
		for _, elem := range arr.Elements() {
			if elemName, nameOK := core.GetName(elem); nameOK {
				if mode, found := byName(elemName); found {
					return mode
				}
			}
		}
		common.Log.Debug("No supported blend mode in %s; using Normal", arr.String())
	}
	return context.BlendModeNormal
}
func (r *renderer) buildSoftMask(ctx context.Context, obj core.PdfObject, res *model.PdfPageResources) (*image.Alpha, error) {
	dict, ok := core.GetDict(obj)
	if !ok {
		return nil, nil
	}
	if r.softMaskDepth >= maxSoftMaskDepth {
		common.Log.Debug("Soft mask nested deeper than %d levels; ignoring", maxSoftMaskDepth)
		return nil, nil
	}
	r.softMaskDepth++
	defer func() { r.softMaskDepth-- }()
	luminosity := true
	if subtype, ok := core.GetName(dict.Get("S")); ok && subtype.String() == "Alpha" {
		luminosity = false
	}
	gStream, ok := core.GetStream(dict.Get("G"))
	if !ok {
		return nil, nil
	}
	if r.limits.Exhausted() {
		return nil, errSoftMaskBudget
	}
	form, err := model.NewXObjectFormFromStream(gStream)
	if err != nil {
		return nil, err
	}
	content, err := form.GetContentStream()
	if err != nil {
		return nil, err
	}
	ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
	if err != nil {
		return nil, err
	}
	r.limits.Charge(gStream, len(*ops), len(content), 1)
	formRes := form.Resources
	if formRes == nil {
		formRes = res
	}
	isolated := false
	if groupDict, groupOK := core.GetDict(form.Group); groupOK {
		if isolatedObj, isolatedOK := core.GetBool(groupDict.Get("I")); isolatedOK {
			isolated = bool(*isolatedObj)
		}
	}
	w, h := ctx.Width(), ctx.Height()
	maskCtx := imagerender.NewContext(w, h)
	defer maskCtx.ReleaseBuffers()
	if parent, isImgCtx := ctx.(*imagerender.Context); isImgCtx {
		maskCtx.SetInterpolator(parent.Interpolator())
		maskCtx.SetGlyphCache(parent.GlyphCache())
	}
	backdrop := shadingBackground(dict, r.softMaskGroupColorspace(form))
	if luminosity && !isolated {
		maskCtx.SetColor(backdrop)
		maskCtx.Clear()
	}
	maskCtx.SetMatrix(ctx.Matrix())
	if formMatrix, matrixErr := transform.MatrixFromPdfObject(form.Matrix); matrixErr == nil {
		maskCtx.SetMatrix(maskCtx.Matrix().Mult(formMatrix))
	}
	maskCtx.SetRGBA(0, 0, 0, 1)
	if form.BBox != nil {
		if bboxArr, bboxOK := core.GetArray(form.BBox); bboxOK {
			if bbox, bboxErr := core.GetNumbersAsFloat(bboxArr.Elements()); bboxErr == nil && len(bbox) == 4 {
				maskCtx.DrawRectangle(bbox[0], bbox[1], bbox[2]-bbox[0], bbox[3]-bbox[1])
				maskCtx.Clip()
			}
		}
	}
	if renderErr := r.renderOperations(maskCtx, ops, formRes, maskCtx.Matrix()); renderErr != nil {
		return nil, renderErr
	}
	rgba, ok := maskCtx.Image().(*image.RGBA)
	if !ok {
		return nil, nil
	}
	transfer := r.softMaskTransferLUT(dict.Get("TR"))
	backdropR, backdropG, backdropB := colorRGB8(backdrop)
	alpha := image.NewAlpha(rgba.Bounds())
	for i, n := 0, len(alpha.Pix); i < n; i++ {
		off := i * 4
		var a uint8
		if luminosity {
			r, g, b := uint32(rgba.Pix[off]), uint32(rgba.Pix[off+1]), uint32(rgba.Pix[off+2])
			if isolated {
				transparency := uint32(255 - rgba.Pix[off+3])
				r += backdropR * transparency / 255
				g += backdropG * transparency / 255
				b += backdropB * transparency / 255
			}
			a = uint8((77*r + 150*g + 29*b) >> 8)
		} else {
			a = rgba.Pix[off+3]
		}
		if transfer != nil {
			a = transfer[a]
		}
		alpha.Pix[i] = a
	}
	return alpha, nil
}

const (
	maxShadingDim    = 2048
	maxShadingPixels = 1 << 21
	maxTilingDepth   = 4
	maxTilingCells   = 32
)

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
func colorRGB8(c color.Color) (r, g, b uint32) {
	cr, cg, cb, _ := c.RGBA()
	return cr >> 8, cg >> 8, cb >> 8
}
func maskImageToRGBA(img *model.Image) image.Image {
	w, h := int(img.Width), int(img.Height)
	if w <= 0 || h <= 0 {
		common.Log.Debug("invalid image dimensions (%d, %d)", w, h)
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c, err := img.ColorAt(x, y)
			if err != nil {
				common.Log.Debug("could not retrieve image mask value at (%d, %d). Output may be incorrect.", x, y)
				continue
			}
			r, g, b, _ := c.RGBA()
			var px color.Color
			if r+g+b == 0 {
				px = color.Transparent
			} else {
				gray := uint8(core.RGBToGrayscale(int(r>>8), int(g>>8), int(b>>8)))
				px = color.RGBA{R: 255, G: 255, B: 255, A: gray}
			}
			out.Set(x, y, px)
		}
	}
	return out
}
func (r *renderer) softMaskGroupColorspace(form *model.XObjectForm) model.PdfColorspace {
	groupDict, ok := core.GetDict(form.Group)
	if !ok {
		return nil
	}
	cs, _ := r.groupColorspace(groupDict)
	return cs
}
func (r *renderer) softMaskTransferLUT(obj core.PdfObject) []uint8 {
	obj = core.ResolveReference(obj)
	if obj == nil {
		return nil
	}
	if name, ok := core.GetName(obj); ok && name.String() == "Identity" {
		return nil
	}
	fn, err := model.NewPdfFunctionFromPdfObject(obj)
	if err != nil {
		r.noteDecodeLimit(err)
		common.Log.Debug("Soft mask /TR: %v", err)
		return nil
	}
	lut := make([]uint8, 256)
	for i := 0; i < 256; i++ {
		out, err := fn.Evaluate([]float64{float64(i) / 255})
		if err != nil || len(out) == 0 {
			return nil
		}
		v := out[0]
		switch {
		case v <= 0:
			lut[i] = 0
		case v >= 1:
			lut[i] = 255
		default:
			lut[i] = uint8(v*255 + 0.5)
		}
	}
	return lut
}
func applyImageMask(img, mask image.Image, interp xdraw.Interpolator) image.Image {
	maskSize, imgSize := mask.Bounds().Size(), img.Bounds().Size()
	w, h := maskSize.X, maskSize.Y
	if imgSize.X > w {
		w = imgSize.X
	}
	if imgSize.Y > h {
		h = imgSize.Y
	}
	rect := image.Rect(0, 0, w, h)
	if maskSize.X != w || maskSize.Y != h {
		scaledMask := image.NewRGBA(rect)
		interp.Scale(scaledMask, rect, mask, mask.Bounds(), xdraw.Over, nil)
		mask = scaledMask
	}
	if imgSize.X != w || imgSize.Y != h {
		scaledImg := image.NewRGBA(rect)
		interp.Scale(scaledImg, rect, img, img.Bounds(), xdraw.Over, nil)
		img = scaledImg
	}
	out := image.NewRGBA(rect)
	xdraw.DrawMask(out, rect, img, image.Point{}, mask, image.Point{}, xdraw.Over)
	return out
}
func intIn(v int, set ...int) bool {
	for _, x := range set {
		if v == x {
			return true
		}
	}
	return false
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

func (r *renderer) shadingBaseMatrix(ctx context.Context, matrix *core.PdfObjectArray) transform.Matrix {
	if matrix != nil {
		return r.baseMatrix
	}
	return ctx.Matrix()
}

// SetInterpolator sets the interpolation algorithm to use for image scaling and transformations.
// The interpolator determines the quality of image scaling. Available interpolators from
// golang.org/x/image/draw include:
//   - draw.NearestNeighbor (fastest, lowest quality)
//   - draw.ApproxBiLinear (fast, good quality)
//   - draw.BiLinear (balanced, default)
//   - draw.CatmullRom (high quality, slower)
func (d *ImageDevice) SetInterpolator(interpolator xdraw.Interpolator) {
	d.interpolator = interpolator
	d.tilingCache = nil
}
func (r *renderer) groupColorspace(dict *core.PdfObjectDictionary) (model.PdfColorspace, error) {
	csObj := dict.Get("CS")
	if csObj == nil {
		return nil, nil
	}
	cs, err := model.NewPdfColorspaceFromPdfObject(csObj)
	if err != nil {
		r.noteDecodeLimit(err)
		common.Log.Debug("Transparency group /CS: %v", err)
		return nil, err
	}
	return cs, nil
}
func stencilMaskImage(img *model.Image, c color.Color) image.Image {
	w, h := int(img.Width), int(img.Height)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px, err := img.ColorAt(x, y)
			if err != nil {
				common.Log.Debug("could not retrieve image mask value at (%d, %d). Output may be incorrect.", x, y)
				continue
			}
			r, g, b, _ := px.RGBA()
			var outPx color.Color
			if r+g+b == 0 {
				outPx = c
			} else {
				outPx = color.Transparent
			}
			out.Set(x, y, outPx)
		}
	}
	return out
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
func fillAndStroke(ctx context.Context, gs contentstream.GraphicsState, rule context.FillRule, newSubPath bool) error {
	if err := setFillFromColorspace(ctx, gs.ColorspaceNonStroking, gs.ColorNonStroking); err != nil {
		return err
	}
	if newSubPath {
		ctx.NewSubPath()
	}
	ctx.SetFillRule(rule)
	ctx.FillPreserve()
	if err := setStrokeFromColorspace(ctx, gs.ColorspaceStroking, gs.ColorStroking); err != nil {
		return err
	}
	ctx.Stroke()
	return nil
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
func explicitMaskImage(obj core.PdfObject, c color.Color) (image.Image, error) {
	stream, ok := core.GetStream(obj)
	if !ok {
		return nil, nil
	}
	xi, err := model.NewXObjectImageFromStream(stream)
	if err != nil {
		return nil, err
	}
	img, err := xi.ToImage()
	if err != nil {
		return nil, err
	}
	return stencilMaskImage(img, c), nil
}
func setStrokeFromColorspace(ctx context.Context, cs model.PdfColorspace, c model.PdfColor) error {
	if isPatternColorspace(cs) {
		return nil
	}
	rgb, err := cs.ColorToRGB(c)
	if err != nil {
		return err
	}
	if devRGB, ok := rgb.(*model.PdfColorDeviceRGB); ok {
		ctx.SetStrokeRGBA(devRGB.R(), devRGB.G(), devRGB.B(), 1)
	}
	return nil
}
func (r *renderer) transparencyGroupAttrs(dict *core.PdfObjectDictionary) context.TransparencyGroup {
	var group context.TransparencyGroup
	if isolatedObj, ok := core.GetBool(dict.Get("I")); ok {
		group.Isolated = bool(*isolatedObj)
	}
	if group.Isolated {
		conv, cached := r.colorConverters[dict]
		if !cached {
			cs, err := r.groupColorspace(dict)
			conv = groupColorConverter(cs)
			if !errors.Is(err, core.ErrDecodedSizeLimitExceeded) {
				if r.colorConverters == nil {
					r.colorConverters = map[*core.PdfObjectDictionary]context.ColorConverter{}
				}
				r.colorConverters[dict] = conv
			}
		}
		group.Convert = conv
	}
	if knockoutObj, ok := core.GetBool(dict.Get("K")); ok {
		group.Knockout = bool(*knockoutObj)
	}
	return group
}
func evalFunctionsAt(fns []model.PdfFunction, cs model.PdfColorspace, t float64) (color.RGBA, error) {
	return evalFunctionsColor(fns, cs, []float64{t})
}
