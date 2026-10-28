// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package pdfutil

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/streamlimits"
	"github.com/OpenMellon/openpdf/model"
)

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
