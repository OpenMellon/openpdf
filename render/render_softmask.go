package render

import (
	"errors"
	"image"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/OpenMellon/openpdf/render/internal/context/imagerender"
)

const maxSoftMaskDepth = 8

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
