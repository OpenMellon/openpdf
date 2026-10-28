// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package annotator provides an interface for creating annotations with appearance
// streams.  It goes beyond the models package which includes definitions of basic annotation models, in that it
// can create the appearance streams which specify the exact appearance as needed by many pdf viewers for consistent
// appearance of the annotations.
// It also contains methods for generating appearance streams for fields via widget annotations.
package annotator

import (
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

func (s *AppearanceStyle) processDA(field *model.PdfField, ops *contentstream.ContentStreamOperations, dr, resources *model.PdfPageResources, cc *contentstream.ContentCreator) (*AppearanceFont, bool, error) {
	var fallback *AppearanceFont
	var forceReplace bool
	if s.Fonts != nil {
		if s.Fonts.Fallback != nil {
			fallback = s.Fonts.Fallback
		}
		if fieldFallbacks := s.Fonts.FieldFallbacks; fieldFallbacks != nil {
			if f, ok := fieldFallbacks[field.PartialName()]; ok {
				fallback = f
			} else if fullName, err := field.FullName(); err == nil {
				if f, ok := fieldFallbacks[fullName]; ok {
					fallback = f
				}
			}
		}
		if fallback != nil {
			fallback.fillName()
		}
		forceReplace = s.Fonts.ForceReplace
	}
	var fontName string
	var fontSize float64
	var hasTf bool
	if ops != nil {
		for _, op := range *ops {
			if op.Operand == "Tf" && len(op.Params) == 2 {
				if name, ok := core.GetNameVal(op.Params[0]); ok {
					fontName = name
				}
				if size, err := core.GetNumberAsFloat(op.Params[1]); err == nil {
					fontSize = size
				}
				hasTf = true
				continue
			}
			cc.AddOperand(*op)
		}
	}
	var font *AppearanceFont
	var fontObj core.PdfObject
	if forceReplace && fallback != nil {
		font = fallback
	} else {
		if dr != nil && fontName != "" {
			if fo, ok := dr.GetFontByName(*core.MakeName(fontName)); ok {
				if pdfFont, err := model.NewPdfFontFromPdfObject(fo); err == nil {
					fontObj = fo
					font = &AppearanceFont{Name: fontName, Font: pdfFont, Size: fontSize}
				} else {
					common.Log.Debug("ERROR: could not load appearance font: %v", err)
				}
			}
		}
		if font == nil && fallback != nil {
			font = fallback
		}
		if font == nil {
			stdFont, err := model.NewStandard14Font("Helvetica")
			if err != nil {
				return nil, false, err
			}
			font = &AppearanceFont{Name: "Helv", Font: stdFont, Size: fontSize}
		}
	}
	if font.Size <= 0 && s.Fonts != nil && s.Fonts.FallbackSize > 0 {
		font.Size = s.Fonts.FallbackSize
	}
	fontNameObj := *core.MakeName(font.Name)
	if fontObj == nil {
		fontObj = font.Font.ToPdfObject()
	}
	if dr != nil && !dr.HasFontByName(fontNameObj) {
		dr.SetFontByName(fontNameObj, fontObj)
	}
	if resources != nil && !resources.HasFontByName(fontNameObj) {
		resources.SetFontByName(fontNameObj, fontObj)
	}
	return font, hasTf, nil
}

func balanceGraphicsState(ops *contentstream.ContentStreamOperations, content []byte) []byte {
	if ops == nil {
		return content
	}
	depth, minDepth := 0, 0
	for _, op := range *ops {
		switch op.Operand {
		case "q":
			depth++
		case "Q":
			depth--
			if depth < minDepth {
				minDepth = depth
			}
		}
	}
	if depth == 0 && minDepth == 0 {
		return content
	}
	buf := []byte(strings.Repeat("q\n", -minDepth))
	buf = append(buf, content...)
	if n := len(buf); n > 0 && buf[n-1] != '\n' {
		buf = append(buf, '\n')
	}
	return append(buf, strings.Repeat("Q\n", depth-minDepth)...)
}

// CreateInkAnnotation creates an ink annotation object that can be added to the annotation list of a PDF page.
func CreateInkAnnotation(inkDef InkAnnotationDef) (*model.PdfAnnotation, error) {
	annot := model.NewPdfAnnotationInk()
	inkList := core.MakeArray()
	for _, path := range inkDef.Paths {
		if path.Length() == 0 {
			continue
		}
		coords := []float64{}
		for _, pt := range path.Points {
			x := precision.RoundDefault(pt.X)
			y := precision.RoundDefault(pt.Y)
			coords = append(coords, x, y)
		}
		inkList.Append(core.MakeArrayFromFloats(coords))
	}
	annot.InkList = inkList
	if inkDef.Color == nil {
		inkDef.Color = model.NewPdfColorDeviceRGB(0.0, 0.0, 0.0)
	}
	annot.C = core.MakeArrayFromFloats([]float64{inkDef.Color.R(), inkDef.Color.G(), inkDef.Color.B()})
	ap, rect, err := inkAppearance(&inkDef)
	if err != nil {
		return nil, err
	}
	annot.AP = ap
	annot.Rect = core.MakeArrayFromFloats([]float64{rect.Llx, rect.Lly, rect.Urx, rect.Ury})
	return annot.PdfAnnotation, nil
}
