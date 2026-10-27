// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package pdfutil

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/streamlimits"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/ps"
	"io"
	"os"
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

// NormalizePage performs the following operations on the passed in page:
//   - Normalize the page rotation.
//     Rotates the contents of the page according to the Rotate entry, thus
//     flattening the rotation. The Rotate entry of the page is set to nil.
//   - Normalize the media box.
//     If the media box of the page is offsetted (Llx != 0 or Lly != 0),
//     the contents of the page are translated to (-Llx, -Lly). After
//     normalization, the media box is updated (Llx and Lly are set to 0 and
//     Urx and Ury are updated accordingly).
//   - Normalize the crop box.
//     The crop box of the page is updated based on the previous operations.
//
// After normalization, the page should look the same if openend using a
// PDF viewer.
// NOTE: This function does not normalize annotations, outlines other parts
// that are not part of the basic geometry and page content streams.
func NormalizePage(page *model.PdfPage) error {
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return err
	}
	rotate, err := page.GetRotate()
	if err != nil {
		common.Log.Debug("ERROR: %s - ignoring and assuming no rotation\n", err.Error())
	}
	rotated := rotate%360 != 0 && rotate%90 == 0
	mediaBox.Normalize()
	llx, lly, width, height := mediaBox.Llx, mediaBox.Lly, mediaBox.Width(), mediaBox.Height()
	offset := llx != 0 || lly != 0
	if !rotated && !offset {
		return nil
	}
	rotatedBounds := func(w, h, angle float64) draw.BoundingBox {
		return draw.Path{Points: []draw.Point{draw.NewPoint(0, 0).Rotate(angle), draw.NewPoint(w, 0).Rotate(angle), draw.NewPoint(0, h).Rotate(angle), draw.NewPoint(w, h).Rotate(angle)}}.GetBoundingBox()
	}
	cc := contentstream.NewContentCreator()
	var rotation float64
	if rotated {
		rotation = -float64(rotate)
		bounds := rotatedBounds(width, height, rotation)
		cc.Translate((bounds.Width-width)/2+width/2, (bounds.Height-height)/2+height/2)
		cc.RotateDeg(rotation)
		cc.Translate(-width/2, -height/2)
		width, height = bounds.Width, bounds.Height
	}
	if offset {
		cc.Translate(-llx, -lly)
	}
	ops := cc.Operations()
	stream, err := core.MakeStream(ops.Bytes(), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	contents := core.MakeArray(stream)
	contents.Append(page.GetContentStreamObjs()...)
	*mediaBox = model.PdfRectangle{Urx: width, Ury: height}
	if cropBox := page.CropBox; cropBox != nil {
		cropBox.Normalize()
		cropLlx, cropLly, cropW, cropH := cropBox.Llx-llx, cropBox.Lly-lly, cropBox.Width(), cropBox.Height()
		if rotated {
			cropBounds := rotatedBounds(cropW, cropH, rotation)
			cropW, cropH = cropBounds.Width, cropBounds.Height
		}
		*cropBox = model.PdfRectangle{Llx: cropLlx, Lly: cropLly, Urx: cropLlx + cropW, Ury: cropLly + cropH}
	}
	common.Log.Debug("Rotate=%f° Ops=%q MediaBox=%.2f", rotation, ops, mediaBox)
	page.Contents = contents
	page.Rotate = nil
	return nil
}

type grayscaleWalk struct {
	visited map[grayscaleStreamKey]struct{}
	limits  streamlimits.Walker
}

func (w *grayscaleWalk) convertPatternToGray(pattern *model.PdfPattern) (*model.PdfPattern, error) {
	if pattern.IsTiling() {
		tiling := pattern.GetAsTilingPattern()
		if tiling.IsColored() {
			stream, ok := core.GetStream(core.TraceToDirectObject(pattern.GetContainingPdfObject()))
			if ok {
				if !w.enter(stream, nil) {
					return pattern, nil
				}
				defer w.leave(stream)
			}
			content, err := tiling.GetContentStream()
			if err != nil {
				return nil, err
			}
			grayContent, err := w.transformContentStream(string(content), tiling.Resources, stream)
			if err != nil {
				return nil, err
			}
			err = tiling.SetContentStream(grayContent, nil)
			if err != nil {
				return nil, err
			}
			_ = tiling.ToPdfObject()
		}
	} else if pattern.IsShading() {
		shadingPat := pattern.GetAsShadingPattern()
		grayShading, shadingErr := shadingToGray(shadingPat.Shading)
		if shadingErr != nil {
			return nil, shadingErr
		}
		shadingPat.Shading = grayShading
		_ = shadingPat.ToPdfObject()
	}
	return pattern, nil
}

// MergePdfWithFilePermission merges several PDF files passed as inputPaths to one PDF file, outputPath.
// isMergingForms controls if it is necessary to merge AcroForms from input files.
// If you are not sure, you can always set isMergingForms to true.
// perm sets the file permission for the output file.
func MergePdfWithFilePermission(inputPaths []string, outputPath string, isMergingForms bool, perm os.FileMode) error {
	return MergePdfWithFilePermissionAndOptions(inputPaths, outputPath, MergePdfOptions{IsMergingForms: isMergingForms}, perm)
}

// ConvertPageToGrayscale() replaces color objects on the page with grayscale ones.  Also references XObject Images and Forms
// to convert those to grayscale.
func ConvertPageToGrayscale(page *model.PdfPage) error {
	contents, err := page.GetAllContentStreams()
	if err != nil {
		return err
	}
	gray, err := contentStreamToGray(contents, page.Resources)
	if err != nil {
		return err
	}
	err = page.SetContentStreams([]string{string(gray)}, core.NewFlateEncoder())
	if err != nil {
		return err
	}
	return nil
}

// MergePdfStreamsWithOptions merges several PDF streams and writes the result to output,
// applying the supplied options. See MergePdfOptions for available knobs.
// Inputs must implement io.ReaderAt (*os.File, *bytes.Reader, *io.SectionReader all qualify).
func MergePdfStreamsWithOptions(inputs []io.ReaderAt, output io.Writer, opts MergePdfOptions) error {
	if len(inputs) == 0 {
		return fmt.Errorf("no input PDFs provided")
	}
	writer := model.NewPdfWriter()
	if opts.Optimizer != nil {
		writer.SetOptimizer(opts.Optimizer)
	}
	var mergedForm *model.PdfAcroForm
	for idx, input := range inputs {
		reader, err := model.NewPdfReaderWithOpts(input, nil)
		if err != nil {
			return err
		}
		numPages, err := reader.GetNumPages()
		if err != nil {
			return err
		}
		for i := range numPages {
			pageNum := i + 1
			page, pageErr := reader.GetPage(pageNum)
			if pageErr != nil {
				return pageErr
			}
			pageErr = writer.AddPage(page)
			if pageErr != nil {
				return pageErr
			}
		}
		if opts.IsMergingForms && reader.AcroForm != nil {
			if mergedForm == nil {
				mergedForm = reader.AcroForm
			} else {
				mergedForm, err = mergeAcroForms(mergedForm, reader.AcroForm, idx+1)
				if err != nil {
					return err
				}
			}
		}
	}
	if mergedForm != nil {
		writer.SetForms(mergedForm)
	}
	return writer.Write(output)
}

// MergePdfStreams merges several PDF streams passed as io.ReaderAt and writes the result to output.
// isMergingForms controls if it is necessary to merge AcroForms from input streams.
// If you are not sure, you can always set isMergingForms to true.
func MergePdfStreams(inputs []io.ReaderAt, output io.Writer, isMergingForms bool) error {
	return MergePdfStreamsWithOptions(inputs, output, MergePdfOptions{IsMergingForms: isMergingForms})
}
func (w *grayscaleWalk) leave(stream *core.PdfObjectStream) { w.limits.Leave(stream) }

// MergePdfWithFilePermissionAndOptions merges several PDF files passed as inputPaths to one PDF file,
// outputPath, applying the supplied options and writing the result with the given file permission.
// See MergePdfOptions for available knobs.
func MergePdfWithFilePermissionAndOptions(inputPaths []string, outputPath string, opts MergePdfOptions, perm os.FileMode) error {
	readers := make([]io.ReaderAt, 0, len(inputPaths))
	for _, path := range inputPaths {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		readers = append(readers, f)
	}
	var buf bytes.Buffer
	if mergeErr := MergePdfStreamsWithOptions(readers, &buf, opts); mergeErr != nil {
		return mergeErr
	}
	return os.WriteFile(outputPath, buf.Bytes(), perm)
}
func mergeResources(dst, src *model.PdfPageResources) (*model.PdfPageResources, error) {
	if dst.XObject == nil {
		dst.XObject = src.XObject
	} else {
		dstXObj, _ := core.GetDict(dst.XObject)
		if src.XObject != nil {
			srcXObj, _ := core.GetDict(src.XObject)
			for _, xKey := range srcXObj.Keys() {
				xVal := srcXObj.Get(xKey)
				dstXObj.Set(xKey, xVal)
			}
		}
	}
	dstCS, err := dst.GetColorspaces()
	if err != nil {
		return nil, err
	}
	srcCS, err := src.GetColorspaces()
	if err != nil {
		return nil, err
	}
	if dstCS == nil {
		dst.SetColorSpace(srcCS)
	} else {
		if srcCS != nil {
			for name, cs := range srcCS.Colorspaces {
				if _, exists := dstCS.Colorspaces[name]; !exists {
					dstCS.Names = append(dstCS.Names, name)
				}
				dst.SetColorspaceByName(core.PdfObjectName(name), cs)
			}
		}
	}
	if dst.ExtGState == nil {
		dst.ExtGState = src.ExtGState
	} else {
		dstGS, _ := core.GetDict(dst.ExtGState)
		if src.ExtGState != nil {
			srcGS, _ := core.GetDict(src.ExtGState)
			for _, gsKey := range srcGS.Keys() {
				gsVal := srcGS.Get(gsKey)
				dstGS.Set(gsKey, gsVal)
			}
		}
	}
	if dst.Shading == nil {
		dst.Shading = src.Shading
	} else {
		dstShading, _ := core.GetDict(dst.Shading)
		if src.Shading != nil {
			srcShading, _ := core.GetDict(src.Shading)
			for _, shKey := range srcShading.Keys() {
				shVal := srcShading.Get(shKey)
				dstShading.Set(shKey, shVal)
			}
		}
	}
	if dst.Pattern == nil {
		dst.Pattern = src.Pattern
	} else {
		dstPattern, _ := core.GetDict(dst.Pattern)
		if src.Pattern != nil {
			srcPattern, _ := core.GetDict(src.Pattern)
			for _, patKey := range srcPattern.Keys() {
				patVal := srcPattern.Get(patKey)
				dstPattern.Set(patKey, patVal)
			}
		}
	}
	if dst.Font == nil {
		dst.Font = src.Font
	} else {
		dstFont, _ := core.GetDict(dst.Font)
		if src.Font != nil {
			srcFont, _ := core.GetDict(src.Font)
			for _, fontKey := range srcFont.Keys() {
				fontVal := srcFont.Get(fontKey)
				dstFont.Set(fontKey, fontVal)
			}
		}
	}
	if dst.ProcSet == nil {
		dst.ProcSet = src.ProcSet
	} else {
		dstProcSet, _ := core.GetDict(dst.ProcSet)
		if src.ProcSet != nil {
			srcProcSet, _ := core.GetDict(src.ProcSet)
			for _, psKey := range srcProcSet.Keys() {
				psVal := srcProcSet.Get(psKey)
				dstProcSet.Set(psKey, psVal)
			}
		}
	}
	if dst.Properties == nil {
		dst.Properties = src.Properties
	} else {
		dstProps, _ := core.GetDict(dst.Properties)
		if src.Properties != nil {
			srcProps, _ := core.GetDict(src.Properties)
			for _, propKey := range srcProps.Keys() {
				propVal := srcProps.Get(propKey)
				dstProps.Set(propKey, propVal)
			}
		}
	}
	return dst, nil
}

// MergePdfOptions configures behaviour of the merge operation.
type MergePdfOptions struct {

	// IsMergingForms controls whether AcroForms from input documents are merged.
	IsMergingForms bool

	// Optimizer is applied to the merged document on write. When nil (default),
	// the merged output is written without optimization. To deduplicate streams
	// shared across input documents (e.g. embedded font programs, image
	// XObjects, ToUnicode CMaps), pass
	// optimize.New(optimize.Options{CombineDuplicateStreams: true}).
	Optimizer model.Optimizer
}

func mergeAcroForms(dst, src *model.PdfAcroForm, docIndex int) (*model.PdfAcroForm, error) {
	if dst.NeedAppearances == nil {
		dst.NeedAppearances = src.NeedAppearances
	}
	if dst.SigFlags == nil {
		dst.SigFlags = src.SigFlags
	}
	if dst.CO == nil {
		dst.CO = src.CO
	}
	if dst.DR == nil {
		dst.DR = src.DR
	} else if src.DR != nil {
		dr, err := mergeResources(dst.DR, src.DR)
		if err != nil {
			return nil, err
		}
		dst.DR = dr
	}
	if dst.DA == nil {
		dst.DA = src.DA
	}
	if dst.Q == nil {
		dst.Q = src.Q
	}
	if dst.XFA == nil {
		dst.XFA = src.XFA
	} else {
		if src.XFA != nil {
			common.Log.Debug("TODO: Handle XFA merging - Currently just using first one that is encountered")
		}
	}
	if dst.Fields == nil {
		dst.Fields = src.Fields
	} else {
		docField := model.NewPdfField()
		docField.T = core.MakeString(fmt.Sprintf("doc%d", docIndex))
		docField.Kids = []*model.PdfField{}
		if src.Fields != nil {
			for _, field := range *src.Fields {
				field.Parent = docField
				docField.Kids = append(docField.Kids, field)
			}
		}
		*dst.Fields = append(*dst.Fields, docField)
	}
	return dst, nil
}

// MergePdf merges several PDF files passed as inputPaths to one PDF file, outputPath.
// isMergingForms controls if it is necessary to merge AcroForms from input files.
// If you are not sure, you can always set isMergingForms to true.
func MergePdf(inputPaths []string, outputPath string, isMergingForms bool) error {
	return MergePdfWithFilePermissionAndOptions(inputPaths, outputPath, MergePdfOptions{IsMergingForms: isMergingForms}, os.FileMode(0644))
}
func (w *grayscaleWalk) enter(stream *core.PdfObjectStream, resources *model.PdfPageResources) bool {
	key := grayscaleStreamKey{stream: stream, resources: resources}
	if _, seen := w.visited[key]; seen {
		return false
	}
	if !w.limits.Enter(stream, "form or pattern") {
		return false
	}
	if w.visited == nil {
		w.visited = map[grayscaleStreamKey]struct{}{}
	}
	w.visited[key] = struct{}{}
	return true
}

// MergePdfWithOptions merges several PDF files passed as inputPaths to one PDF file, outputPath,
// applying the supplied options. See MergePdfOptions for available knobs.
func MergePdfWithOptions(inputPaths []string, outputPath string, opts MergePdfOptions) error {
	return MergePdfWithFilePermissionAndOptions(inputPaths, outputPath, opts, os.FileMode(0644))
}
func shadingToGray(shading *model.PdfShading) (*model.PdfShading, error) {
	cs := shading.ColorSpace
	if cs.GetNumComponents() == 1 {
		return shading, nil
	} else if cs.GetNumComponents() == 3 {
		fn3 := &model.PdfFunctionType4{}
		fn3.Domain = []float64{0, 1, 0, 1, 0, 1}
		fn3.Range = []float64{0, 1}
		prog3 := ps.NewPSProgram()
		prog3.Append(ps.MakeReal(0.11))
		prog3.Append(ps.MakeOperand("mul"))
		prog3.Append(ps.MakeOperand("exch"))
		prog3.Append(ps.MakeReal(0.59))
		prog3.Append(ps.MakeOperand("mul"))
		prog3.Append(ps.MakeOperand("add"))
		prog3.Append(ps.MakeOperand("exch"))
		prog3.Append(ps.MakeReal(0.3))
		prog3.Append(ps.MakeOperand("mul"))
		prog3.Append(ps.MakeOperand("add"))
		fn3.Program = prog3
		deviceN3 := model.NewPdfColorspaceDeviceN()
		deviceN3.AlternateSpace = model.NewPdfColorspaceDeviceGray()
		deviceN3.ColorantNames = core.MakeArray(core.MakeName("R"), core.MakeName("G"), core.MakeName("B"))
		deviceN3.TintTransform = fn3
		shading.ColorSpace = deviceN3
		return shading, nil
	} else if cs.GetNumComponents() == 4 {
		fn4 := &model.PdfFunctionType4{}
		fn4.Domain = []float64{0, 1, 0, 1, 0, 1, 0, 1}
		fn4.Range = []float64{0, 1}
		prog4 := ps.NewPSProgram()
		prog4.Append(ps.MakeOperand("exch"))
		prog4.Append(ps.MakeReal(0.11))
		prog4.Append(ps.MakeOperand("mul"))
		prog4.Append(ps.MakeOperand("add"))
		prog4.Append(ps.MakeOperand("exch"))
		prog4.Append(ps.MakeReal(0.59))
		prog4.Append(ps.MakeOperand("mul"))
		prog4.Append(ps.MakeOperand("add"))
		prog4.Append(ps.MakeOperand("exch"))
		prog4.Append(ps.MakeReal(0.30))
		prog4.Append(ps.MakeOperand("mul"))
		prog4.Append(ps.MakeOperand("add"))
		prog4.Append(ps.MakeOperand("dup"))
		prog4.Append(ps.MakeReal(1.0))
		prog4.Append(ps.MakeOperand("ge"))
		clamp := ps.NewPSProgram()
		clamp.Append(ps.MakeOperand("pop"))
		clamp.Append(ps.MakeReal(1.0))
		prog4.Append(clamp)
		prog4.Append(ps.MakeOperand("if"))
		fn4.Program = prog4
		deviceN4 := model.NewPdfColorspaceDeviceN()
		deviceN4.AlternateSpace = model.NewPdfColorspaceDeviceGray()
		deviceN4.ColorantNames = core.MakeArray(core.MakeName("C"), core.MakeName("M"), core.MakeName("Y"), core.MakeName("K"))
		deviceN4.TintTransform = fn4
		shading.ColorSpace = deviceN4
		return shading, nil
	} else {
		return nil, errors.New("unsupported pattern colorspace for grayscale conversion")
	}
}
func contentStreamToGray(content string, resources *model.PdfPageResources) ([]byte, error) {
	return (&grayscaleWalk{}).transformContentStream(content, resources, nil)
}

// ExtractPageRange extracts a range of pages from inputPath PDF file and saves it as outputPath PDF file.
// keepOptionalContent controls if we need to process OC properties (rarely needed).
func ExtractPageRange(inputPath string, outputPath string, pageFrom int, pageTo int, keepOptionalContent bool) error {
	writer := model.NewPdfWriter()
	reader, file, err := model.NewPdfReaderFromFile(inputPath, nil)
	if err != nil {
		return err
	}
	defer file.Close()
	numPages, err := reader.GetNumPages()
	if err != nil {
		return err
	}
	if numPages < pageTo {
		return fmt.Errorf("numPages (%d) < pageTo (%d)", numPages, pageTo)
	}
	if keepOptionalContent {
		ocProps, ocErr := reader.GetOCProperties()
		if ocErr != nil {
			return ocErr
		}
		writer.SetOCProperties(ocProps)
	}
	for i := pageFrom; i <= pageTo; i++ {
		pageNum := i
		page, pageErr := reader.GetPage(pageNum)
		if pageErr != nil {
			return pageErr
		}
		pageErr = writer.AddPage(page)
		if pageErr != nil {
			return pageErr
		}
	}
	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()
	err = writer.Write(out)
	if err != nil {
		return err
	}
	return nil
}
