// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package annotator provides an interface for creating annotations with appearance
// streams.  It goes beyond the models package which includes definitions of basic annotation models, in that it
// can create the appearance streams which specify the exact appearance as needed by many pdf viewers for consistent
// appearance of the annotations.
// It also contains methods for generating appearance streams for fields via widget annotations.
package annotator

import (
	"errors"
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
)

func lineAppearance(def LineAnnotationDef) (*core.PdfObjectDictionary, *model.PdfRectangle, error) {
	form := model.NewXObjectForm()
	form.Resources = model.NewPdfPageResources()
	gsName := ""
	if def.Opacity < 1.0 {
		gsDict := core.MakeDict()
		gsDict.Set("ca", core.MakeFloat(def.Opacity))
		err := form.Resources.AddExtGState("gs1", gsDict)
		if err != nil {
			common.Log.Debug("Unable to add extgstate gs1")
			return nil, nil, err
		}
		gsName = "gs1"
	}
	content, bbox, rect, err := lineAppearanceStream(def, gsName)
	if err != nil {
		return nil, nil, err
	}
	err = form.SetContentStream(content, nil)
	if err != nil {
		return nil, nil, err
	}
	form.BBox = bbox.ToPdfObject()
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, rect, nil
}

// SetStyle applies appearance `style` to `fa`.
func (fa *FieldAppearance) SetStyle(style AppearanceStyle) { fa.style = &style }

func comboBoxAppearance(form *model.PdfAcroForm, widget *model.PdfAnnotationWidget, choice *model.PdfFieldChoice, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	rectArr, hasRect := core.GetArray(widget.Rect)
	if !hasRect {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, err
	}
	w, h := rect.Width(), rect.Height()
	common.Log.Debug("Choice, wa BS: %v", widget.BS)
	daOps, err := contentstream.NewContentStreamParser(textFieldDA(choice.PdfField)).Parse()
	if err != nil {
		return nil, err
	}
	mk, hasMK := core.GetDict(widget.MK)
	if hasMK {
		bs, _ := core.GetDict(widget.BS)
		err := style.applyAppearanceCharacteristics(mk, bs, nil)
		if err != nil {
			return nil, err
		}
	}
	states := core.MakeDict()
	for _, opt := range choice.Opt.Elements() {
		if pair, ok := core.GetArray(opt); ok && pair.Len() == 2 {
			opt = pair.Get(1)
		}
		var label string
		if str, ok := core.GetString(opt); ok {
			label = str.Decoded()
		} else if nm, ok := core.GetName(opt); ok {
			label = nm.String()
		} else {
			common.Log.Debug("ERROR: Opt not a name/string - %T", opt)
			return nil, errors.New("not a name/string")
		}
		if len(label) > 0 {
			xform, err := comboOptionAppearance(choice.PdfField, w, h, label, style, daOps, form.DR, mk)
			if err != nil {
				return nil, err
			}
			states.Set(*core.MakeName(label), xform.ToPdfObject())
		}
	}
	apDict := core.MakeDict()
	apDict.Set("N", states)
	return apDict, nil
}

func isClippedTo(ops *contentstream.ContentStreamOperations, x, y, w, h float64) bool {
	if ops == nil || len(*ops) < 5 {
		return false
	}
	head := (*ops)[:4]
	if head[0].Operand != "q" || head[1].Operand != "re" || (head[2].Operand != "W" && head[2].Operand != "W*") || head[3].Operand != "n" {
		return false
	}
	params, err := core.GetNumbersAsFloat(head[1].Params)
	if err != nil || len(params) != 4 {
		return false
	}
	const eps = 1e-3
	if math.Abs(params[0]-x) >= eps || math.Abs(params[1]-y) >= eps || math.Abs(params[2]-w) >= eps || math.Abs(params[3]-h) >= eps {
		return false
	}
	depth := 0
	for i, op := range *ops {
		switch op.Operand {
		case "q":
			depth++
		case "Q":
			depth--
		}
		if depth <= 0 && i < len(*ops)-1 {
			return false
		}
	}
	return depth == 0
}

func (s *AppearanceStyle) applyAppearanceCharacteristics(mk *core.PdfObjectDictionary, bs *core.PdfObjectDictionary, font *model.PdfFont) error {
	if !s.AllowMK {
		return nil
	}
	if CA, ok := core.GetString(mk.Get("CA")); ok && font != nil {
		caBytes := CA.Bytes()
		if len(caBytes) != 0 {
			runes := font.CharcodesToUnicode(font.BytesToCharcodes(caBytes))
			if len(runes) == 1 && runes[0] != textencoding.MissingCodeRune {
				s.CheckmarkRune = runes[0]
			}
		}
	}
	if BC, ok := core.GetArray(mk.Get("BC")); ok {
		comps, err := BC.ToFloat64Array()
		if err != nil {
			return err
		}
		switch len(comps) {
		case 1:
			s.BorderColor = model.NewPdfColorDeviceGray(comps[0])
		case 3:
			s.BorderColor = model.NewPdfColorDeviceRGB(comps[0], comps[1], comps[2])
		case 4:
			s.BorderColor = model.NewPdfColorDeviceCMYK(comps[0], comps[1], comps[2], comps[3])
		default:
			common.Log.Debug("ERROR: BC - Invalid number of color components (%d)", len(comps))
		}
		if bs != nil {
			if w, err := core.GetNumberAsFloat(bs.Get("W")); err == nil {
				s.BorderSize = w
			}
		}
	}
	if BG, ok := core.GetArray(mk.Get("BG")); ok {
		comps, err := BG.ToFloat64Array()
		if err != nil {
			return err
		}
		switch len(comps) {
		case 1:
			s.FillColor = model.NewPdfColorDeviceGray(comps[0])
		case 3:
			s.FillColor = model.NewPdfColorDeviceRGB(comps[0], comps[1], comps[2])
		case 4:
			s.FillColor = model.NewPdfColorDeviceCMYK(comps[0], comps[1], comps[2], comps[3])
		default:
			common.Log.Debug("ERROR: BG - Invalid number of color components (%d)", len(comps))
		}
	}
	return nil
}
func defaultHighlightColor() model.PdfColor { return model.NewPdfColorDeviceRGB(0.6, 0.757, 0.855) }

// FieldAppearance implements interface model.FieldAppearanceGenerator and generates appearance streams
// for fields taking into account what value is in the field. A common use case is for generating the
// appearance stream prior to flattening fields.
//
// If `OnlyIfMissing` is true, the field appearance is generated only for fields that do not have an
// appearance stream specified.
// If `RegenerateTextFields` is true, all text fields are regenerated (even if OnlyIfMissing is true).
type FieldAppearance struct {
	OnlyIfMissing        bool
	RegenerateTextFields bool
	style                *AppearanceStyle
}

// CreateRectangleAnnotation creates a rectangle annotation object that can be added to page PDF annotations.
func CreateRectangleAnnotation(rectDef RectangleAnnotationDef) (*model.PdfAnnotation, error) {
	annot := model.NewPdfAnnotationSquare()
	if rectDef.BorderEnabled {
		r, g, b := rectDef.BorderColor.R(), rectDef.BorderColor.G(), rectDef.BorderColor.B()
		annot.C = core.MakeArrayFromFloats([]float64{r, g, b})
		bs := model.NewBorderStyle()
		bs.SetBorderWidth(rectDef.BorderWidth)
		annot.BS = bs.ToPdfObject()
	}
	if rectDef.FillEnabled {
		r, g, b := rectDef.FillColor.R(), rectDef.FillColor.G(), rectDef.FillColor.B()
		annot.IC = core.MakeArrayFromFloats([]float64{r, g, b})
	} else {
		annot.IC = core.MakeArrayFromIntegers([]int{})
	}
	if rectDef.Opacity < 1.0 {
		annot.CA = core.MakeFloat(rectDef.Opacity)
	}
	ap, rect, err := rectangleAppearance(rectDef)
	if err != nil {
		return nil, err
	}
	annot.AP = ap
	annot.Rect = core.MakeArrayFromFloats([]float64{rect.Llx, rect.Lly, rect.Urx, rect.Ury})
	return annot.PdfAnnotation, nil
}
