package render

import (
	"image"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/render/internal/context"
	"github.com/OpenMellon/openpdf/render/internal/context/imagerender"
	xdraw "golang.org/x/image/draw"
)

func (r *renderer) drawXObject(ctx context.Context, op *contentstream.ContentStreamOperation, res *model.PdfPageResources) (bool, error) {
	if len(op.Params) != 1 {
		return true, errRangeCheck
	}
	name, nameOK := core.GetName(op.Params[0])
	if !nameOK {
		return true, errTypeCheck
	}
	xobj, xobjType := res.GetXObjectByName(*name)
	switch xobjType {
	case model.XObjectTypeImage:
		common.Log.Debug("XObject image: %s", name.String())
		xi, err := res.GetXObjectImageByName(*name)
		if err != nil {
			return true, err
		}
		img, err := xi.ToImage()
		if err != nil {
			r.noteDecodeLimit(err)
			common.Log.Debug("Rendering result may be incomplete. Image conversion error: %v", err)
			return true, nil
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
				return true, nil
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
			return true, nil
		}
		defer r.limits.Leave(xobj)
		form, err := res.GetXObjectFormByName(*name)
		if err != nil {
			return true, err
		}
		content, err := form.GetContentStream()
		if err != nil {
			return true, err
		}
		formOps, err := contentstream.NewContentStreamParser(string(content)).Parse()
		if err != nil {
			return true, err
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
				return true, matrixErr
			}
			ctx.SetMatrix(ctx.Matrix().Mult(formMatrix))
		}
		if form.BBox != nil {
			bboxArr, bboxOK := core.GetArray(form.BBox)
			if !bboxOK {
				return true, errTypeCheck
			}
			bbox, bboxErr := core.GetNumbersAsFloat(bboxArr.Elements())
			if bboxErr != nil {
				return true, bboxErr
			}
			if len(bbox) != 4 {
				common.Log.Debug("Len = %d", len(bbox))
				return true, errRangeCheck
			}
			ctx.DrawRectangle(bbox[0], bbox[1], bbox[2]-bbox[0], bbox[3]-bbox[1])
			ctx.Clip()
		} else {
			common.Log.Debug("ERROR: Required BBox missing on XObject Form")
		}
		if renderErr := r.renderOperations(ctx, formOps, formRes, ctx.Matrix()); renderErr != nil {
			return true, renderErr
		}
	}
	return false, nil
}
