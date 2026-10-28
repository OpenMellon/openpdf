// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package pdfutil

import (
	"errors"

	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

type grayscaleStreamKey struct {
	stream    *core.PdfObjectStream
	resources *model.PdfPageResources
}

func isPatternColorspace(cs model.PdfColorspace) bool {
	_, ok := cs.(*model.PdfColorspaceSpecialPattern)
	return ok
}

func (w *grayscaleWalk) transformContentStream(content string, resources *model.PdfPageResources, stream *core.PdfObjectStream) ([]byte, error) {
	parser := contentstream.NewContentStreamParser(content)
	ops, err := parser.Parse()
	if err != nil {
		return nil, err
	}
	if stream != nil {
		w.limits.Charge(stream, len(*ops), len(content), 0)
	}
	out := &contentstream.ContentStreamOperations{}
	seenPatterns := map[core.PdfObjectName]bool{}
	seenShadings := map[core.PdfObjectName]bool{}
	processor := contentstream.NewContentStreamProcessor(*ops)
	processor.AddHandler(contentstream.HandlerConditionEnumAllOperands, "", func(op *contentstream.ContentStreamOperation, gs contentstream.GraphicsState, res *model.PdfPageResources) error {
		operand := op.Operand
		switch operand {
		case "CS":
			if isPatternColorspace(gs.ColorspaceStroking) {
				strokeCSName := op.Params[0].(*core.PdfObjectName)
				if *strokeCSName != "Pattern" {
					strokeCS, csFound := res.GetColorspaceByName(*strokeCSName)
					if !csFound {
						return errors.New("colorspace not defined")
					}
					strokePatternCS, csFound := strokeCS.(*model.PdfColorspaceSpecialPattern)
					if !csFound {
						return errors.New("type error")
					}
					if strokePatternCS.UnderlyingCS != nil {
						strokePatternCS.UnderlyingCS = model.NewPdfColorspaceDeviceGray()
					}
					err = res.SetColorspaceByName(*strokeCSName, strokePatternCS)
					if err != nil {
						return err
					}
				}
				*out = append(*out, op)
				return nil
			}
			strokeGrayOp := contentstream.ContentStreamOperation{}
			strokeGrayOp.Operand = operand
			strokeGrayOp.Params = []core.PdfObject{core.MakeName("DeviceGray")}
			*out = append(*out, &strokeGrayOp)
			return nil
		case "cs":
			if isPatternColorspace(gs.ColorspaceNonStroking) {
				fillCSName := op.Params[0].(*core.PdfObjectName)
				if *fillCSName != "Pattern" {
					fillCS, fillCSFound := res.GetColorspaceByName(*fillCSName)
					if !fillCSFound {
						return errors.New("colorspace not defined")
					}
					fillPatternCS, fillCSFound := fillCS.(*model.PdfColorspaceSpecialPattern)
					if !fillCSFound {
						return errors.New("type error")
					}
					if fillPatternCS.UnderlyingCS != nil {
						fillPatternCS.UnderlyingCS = model.NewPdfColorspaceDeviceGray()
					}
					if setErr := res.SetColorspaceByName(*fillCSName, fillPatternCS); setErr != nil {
						return setErr
					}
				}
				*out = append(*out, op)
				return nil
			}
			fillGrayOp := contentstream.ContentStreamOperation{}
			fillGrayOp.Operand = operand
			fillGrayOp.Params = []core.PdfObject{core.MakeName("DeviceGray")}
			*out = append(*out, &fillGrayOp)
			return nil
		case "SC", "SCN":
			if isPatternColorspace(gs.ColorspaceStroking) {
				strokeOp := contentstream.ContentStreamOperation{}
				strokeOp.Operand = operand
				strokeOp.Params = []core.PdfObject{}
				strokePattern, isPattern := gs.ColorStroking.(*model.PdfColorPattern)
				if !isPattern {
					return errors.New("invalid stroking color type")
				}
				if strokePattern.Color != nil {
					patColorRGB, patColorErr := gs.ColorspaceStroking.ColorToRGB(strokePattern.Color)
					if patColorErr != nil {
						return patColorErr
					}
					patColorRGBVal := patColorRGB.(*model.PdfColorDeviceRGB)
					patColorGray := patColorRGBVal.ToGray()
					strokeOp.Params = append(strokeOp.Params, core.MakeFloat(patColorGray.Val()))
				}
				if _, seen := seenPatterns[strokePattern.PatternName]; seen {
					strokeOp.Params = append(strokeOp.Params, core.MakeName(string(strokePattern.PatternName)))
					*out = append(*out, &strokeOp)
					return nil
				}
				seenPatterns[strokePattern.PatternName] = true
				patternObj, patternFound := res.GetPatternByName(strokePattern.PatternName)
				if !patternFound {
					return errors.New("undefined pattern name")
				}
				grayPattern, patternErr := w.convertPatternToGray(patternObj)
				if patternErr != nil {
					return patternErr
				}
				patternErr = res.SetPatternByName(strokePattern.PatternName, grayPattern.ToPdfObject())
				if patternErr != nil {
					return patternErr
				}
				strokeOp.Params = append(strokeOp.Params, core.MakeName(string(strokePattern.PatternName)))
				*out = append(*out, &strokeOp)
			} else {
				plainRGB, plainErr := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
				if plainErr != nil {
					return plainErr
				}
				plainRGBColor := plainRGB.(*model.PdfColorDeviceRGB)
				plainGray := plainRGBColor.ToGray()
				plainOp := contentstream.ContentStreamOperation{}
				plainOp.Operand = operand
				plainOp.Params = []core.PdfObject{core.MakeFloat(plainGray.Val())}
				*out = append(*out, &plainOp)
			}
			return nil
		case "sc", "scn":
			if isPatternColorspace(gs.ColorspaceNonStroking) {
				fillOp := contentstream.ContentStreamOperation{}
				fillOp.Operand = operand
				fillOp.Params = []core.PdfObject{}
				fillPattern, fillIsPattern := gs.ColorNonStroking.(*model.PdfColorPattern)
				if !fillIsPattern {
					return errors.New("invalid stroking color type")
				}
				if fillPattern.Color != nil {
					fillPatColorRGB, fillPatColorErr := gs.ColorspaceNonStroking.ColorToRGB(fillPattern.Color)
					if fillPatColorErr != nil {
						return fillPatColorErr
					}
					fillPatColorRGBVal := fillPatColorRGB.(*model.PdfColorDeviceRGB)
					fillPatColorGray := fillPatColorRGBVal.ToGray()
					fillOp.Params = append(fillOp.Params, core.MakeFloat(fillPatColorGray.Val()))
				}
				if _, fillSeen := seenPatterns[fillPattern.PatternName]; fillSeen {
					fillOp.Params = append(fillOp.Params, core.MakeName(string(fillPattern.PatternName)))
					*out = append(*out, &fillOp)
					return nil
				}
				seenPatterns[fillPattern.PatternName] = true
				fillPatternObj, fillPatternFound := res.GetPatternByName(fillPattern.PatternName)
				if !fillPatternFound {
					return errors.New("undefined pattern name")
				}
				fillGrayPattern, fillPatternErr := w.convertPatternToGray(fillPatternObj)
				if fillPatternErr != nil {
					return fillPatternErr
				}
				fillPatternErr = res.SetPatternByName(fillPattern.PatternName, fillGrayPattern.ToPdfObject())
				if fillPatternErr != nil {
					return fillPatternErr
				}
				fillOp.Params = append(fillOp.Params, core.MakeName(string(fillPattern.PatternName)))
				*out = append(*out, &fillOp)
			} else {
				fillPlainRGB, fillPlainErr := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
				if fillPlainErr != nil {
					return fillPlainErr
				}
				fillPlainRGBColor := fillPlainRGB.(*model.PdfColorDeviceRGB)
				fillPlainGray := fillPlainRGBColor.ToGray()
				fillPlainOp := contentstream.ContentStreamOperation{}
				fillPlainOp.Operand = operand
				fillPlainOp.Params = []core.PdfObject{core.MakeFloat(fillPlainGray.Val())}
				*out = append(*out, &fillPlainOp)
			}
			return nil
		case "RG", "K":
			strokeRGB, strokeRGBErr := gs.ColorspaceStroking.ColorToRGB(gs.ColorStroking)
			if strokeRGBErr != nil {
				return strokeRGBErr
			}
			strokeRGBColor := strokeRGB.(*model.PdfColorDeviceRGB)
			strokeGray := strokeRGBColor.ToGray()
			gOp := contentstream.ContentStreamOperation{}
			gOp.Operand = "G"
			gOp.Params = []core.PdfObject{core.MakeFloat(strokeGray.Val())}
			*out = append(*out, &gOp)
			return nil
		case "rg", "k":
			fillRGB, fillRGBErr := gs.ColorspaceNonStroking.ColorToRGB(gs.ColorNonStroking)
			if fillRGBErr != nil {
				return fillRGBErr
			}
			fillRGBColor := fillRGB.(*model.PdfColorDeviceRGB)
			fillGray := fillRGBColor.ToGray()
			gLowOp := contentstream.ContentStreamOperation{}
			gLowOp.Operand = "g"
			gLowOp.Params = []core.PdfObject{core.MakeFloat(fillGray.Val())}
			*out = append(*out, &gLowOp)
			return nil
		case "sh":
			if len(op.Params) != 1 {
				return errors.New("params to sh operator should be 1")
			}
			shadingName, isName := op.Params[0].(*core.PdfObjectName)
			if !isName {
				return errors.New("sh parameter should be a name")
			}
			if _, seenShading := seenShadings[*shadingName]; seenShading {
				*out = append(*out, op)
				return nil
			}
			seenShadings[*shadingName] = true
			shading, shadingFound := res.GetShadingByName(*shadingName)
			if !shadingFound {
				return errors.New("shading not defined in resources")
			}
			grayShading, shadingErr := shadingToGray(shading)
			if shadingErr != nil {
				return shadingErr
			}
			shadingErr = res.SetShadingByName(*shadingName, grayShading.GetContext().ToPdfObject())
			if shadingErr != nil {
				return shadingErr
			}
		}
		*out = append(*out, op)
		return nil
	})
	processor.AddHandler(contentstream.HandlerConditionEnumOperand, "BI", func(biOp *contentstream.ContentStreamOperation, biGS contentstream.GraphicsState, biRes *model.PdfPageResources) error {
		if len(biOp.Params) != 1 {
			return errors.New("invalid number of parameters")
		}
		inlineImg, isInline := biOp.Params[0].(*contentstream.ContentStreamInlineImage)
		if !isInline {
			return errors.New("invalid inline image parameter")
		}
		img, imgErr := inlineImg.ToImage(biRes)
		if imgErr != nil {
			return imgErr
		}
		imgCS, imgErr := inlineImg.GetColorSpace(biRes)
		if imgErr != nil {
			return imgErr
		}
		rgbImg, imgErr := imgCS.ImageToRGB(*img)
		if imgErr != nil {
			return imgErr
		}
		rgbCS := model.NewPdfColorspaceDeviceRGB()
		grayImg, imgErr := rgbCS.ImageToGray(rgbImg)
		if imgErr != nil {
			return imgErr
		}
		encoder, imgErr := inlineImg.GetEncoder()
		if imgErr != nil {
			return imgErr
		}
		if dct, isDCT := encoder.(*core.DCTEncoder); isDCT {
			dct.ColorComponents = 1
		}
		grayInline, imgErr := contentstream.NewInlineImageFromImage(grayImg, encoder)
		if imgErr != nil {
			if imgErr == core.ErrUnsupportedEncodingParameters {
				encoder = core.NewFlateEncoder()
			}
			grayInline, imgErr = contentstream.NewInlineImageFromImage(grayImg, encoder)
			if imgErr != nil {
				return imgErr
			}
		}
		biOut := contentstream.ContentStreamOperation{}
		biOut.Operand = "BI"
		biOut.Params = []core.PdfObject{grayInline}
		*out = append(*out, &biOut)
		return nil
	})
	seenXObjects := map[string]bool{}
	processor.AddHandler(contentstream.HandlerConditionEnumOperand, "Do", func(doOp *contentstream.ContentStreamOperation, doGS contentstream.GraphicsState, doRes *model.PdfPageResources) error {
		if len(doOp.Params) < 1 {
			return errors.New("ERROR: Invalid number of params for Do object: Range check")
		}
		xobjName := doOp.Params[0].(*core.PdfObjectName)
		_, seenXObj := seenXObjects[string(*xobjName)]
		if seenXObj {
			return nil
		}
		seenXObjects[string(*xobjName)] = true
		xobjStream, xobjType := doRes.GetXObjectByName(*xobjName)
		if xobjType == model.XObjectTypeImage {
			xobjImage, doErr := doRes.GetXObjectImageByName(*xobjName)
			if doErr != nil {
				return doErr
			}
			xImg, doErr := xobjImage.ToImage()
			if doErr != nil {
				return doErr
			}
			xRGB, doErr := xobjImage.ColorSpace.ImageToRGB(*xImg)
			if doErr != nil {
				return doErr
			}
			xRGBCS := model.NewPdfColorspaceDeviceRGB()
			xGray, doErr := xRGBCS.ImageToGray(xRGB)
			if doErr != nil {
				return doErr
			}
			xEncoder := xobjImage.Filter
			if xDCT, xIsDCT := xEncoder.(*core.DCTEncoder); xIsDCT {
				xDCT.ColorComponents = 1
			}
			newImage, doErr := model.NewXObjectImageFromImage(&xGray, nil, xEncoder)
			if doErr != nil {
				if doErr == core.ErrUnsupportedEncodingParameters {
					xEncoder = core.NewFlateEncoder()
				}
				newImage, doErr = model.NewXObjectImageFromImage(&xGray, nil, xEncoder)
				if doErr != nil {
					return doErr
				}
			}
			doErr = doRes.SetXObjectImageByName(*xobjName, newImage)
			if doErr != nil {
				return doErr
			}
		} else if xobjType == model.XObjectTypeForm {
			form, formErr := doRes.GetXObjectFormByName(*xobjName)
			if formErr != nil {
				return formErr
			}
			var enterRes *model.PdfPageResources
			if form.Resources == nil {
				enterRes = doRes
			}
			if !w.enter(xobjStream, enterRes) {
				return nil
			}
			defer w.leave(xobjStream)
			formContent, formErr := form.GetContentStream()
			if formErr != nil {
				return formErr
			}
			formRes := form.Resources
			if formRes == nil {
				formRes = doRes
			}
			grayContent, formErr := w.transformContentStream(string(formContent), formRes, xobjStream)
			if formErr != nil {
				return formErr
			}
			formErr = form.SetContentStream(grayContent, nil)
			if formErr != nil {
				return formErr
			}
			formErr = doRes.SetXObjectFormByName(*xobjName, form)
			if formErr != nil {
				return formErr
			}
		}
		return nil
	})
	err = processor.Process(resources)
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
