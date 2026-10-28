package render

import (
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
)

func (r *renderer) applyExtGState(ctx context.Context, op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, res *model.PdfPageResources) error {
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
	return nil
}

func (r *renderer) paintShadingOperator(ctx context.Context, op *contentstream.ContentStreamOperation, res *model.PdfPageResources) {
	if len(op.Params) != 1 {
		common.Log.Debug("Invalid sh params format")
		return
	}
	shadingName, ok := core.GetName(op.Params[0])
	if !ok {
		common.Log.Debug("Failed getting shading name")
		return
	}
	shading, ok := res.GetShadingByName(*shadingName)
	if !ok {
		common.Log.Debug("Failed getting shading data")
		return
	}
	pattern, bboxArr, err := r.processShading(ctx, shading, nil)
	if err != nil {
		r.noteDecodeLimit(err)
		common.Log.Debug("Error when processing shading data: %v", err)
		return
	}
	if pattern == nil {
		common.Log.Debug("Unknown shading")
		return
	}
	switch PdfShadingType(*shading.ShadingType) {
	case ShadingTypeAxial, ShadingTypeRadial:
		r.paintShadingClip(ctx, shading, pattern)
	default:
		bbox, bboxErr := bboxArr.ToFloat64Array()
		if bboxErr != nil {
			common.Log.Debug("Error parsing coordinates: %v", bboxErr)
			return
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
}
