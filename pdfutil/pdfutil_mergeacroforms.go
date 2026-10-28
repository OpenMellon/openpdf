// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package pdfutil

import (
	"errors"
	"fmt"
	"os"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/ps"
)

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
	return MergePdfWithFilePermissionAndOptions(inputPaths, outputPath, MergePdfOptions{IsMergingForms: isMergingForms}, os.FileMode(0o644))
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
	return MergePdfWithFilePermissionAndOptions(inputPaths, outputPath, opts, os.FileMode(0o644))
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
