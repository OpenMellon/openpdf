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

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func imageForm(_, _ float64, img *model.Image, _ AppearanceStyle) (*model.XObjectForm, error) {
	xImg, err := model.NewXObjectImageFromImage(img, nil, core.NewFlateEncoder())
	if err != nil {
		return nil, err
	}
	xImg.Decode = core.MakeArrayFromFloats([]float64{0.0, 1.0, 0.0, 1.0, 0.0, 1.0})
	resources := model.NewPdfPageResources()
	resources.ProcSet = core.MakeArray(core.MakeName("PDF"), core.MakeName("ImageC"))
	resources.SetXObjectImageByName(core.PdfObjectName("Im0"), xImg)
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	cc.Add_cm(float64(img.Width), 0, 0, float64(img.Height), 0, 0)
	cc.Add_Do("Im0")
	cc.Add_Q()
	form := model.NewXObjectForm()
	form.FormType = core.MakeInteger(1)
	form.BBox = core.MakeArrayFromFloats([]float64{0, 0, float64(img.Width), float64(img.Height)})
	form.Resources = resources
	form.SetContentStream(cc.Bytes(), newFlateEncoder())
	return form, nil
}

func drawAlignmentReticle(cc *contentstream.ContentCreator, style AppearanceStyle, w, h float64) {
	cc.Add_q().Add_re(0, 0, w, h).Add_re(0, h/2, w, h/2).Add_re(0, 0, w, h).Add_re(w/2, 0, w/2, h).Add_w(style.BorderSize).SetStrokingColor(style.BorderColor).SetNonStrokingColor(style.FillColor).Add_B().Add_Q()
}

func selectedOptions(field *model.PdfFieldChoice, options []listboxOption) map[int]bool {
	selected := map[int]bool{}
	if field.I != nil {
		for _, el := range field.I.Elements() {
			if idx, ok := core.GetIntVal(el); ok && idx >= 0 && idx < len(options) {
				selected[idx] = true
			}
		}
		return selected
	}
	var values []string
	switch v := core.TraceToDirectObject(field.V).(type) {
	case *core.PdfObjectString:
		values = append(values, v.Decoded())
	case *core.PdfObjectArray:
		for _, el := range v.Elements() {
			if s, ok := core.GetString(el); ok {
				values = append(values, s.Decoded())
			}
		}
	}
	for _, val := range values {
		found := false
		for i, opt := range options {
			if opt.value == val {
				selected[i] = true
				found = true
			}
		}
		if found {
			continue
		}
		for i, opt := range options {
			if opt.label == val {
				selected[i] = true
			}
		}
	}
	return selected
}

func variableTextDA(field *model.PdfField) string {
	return inheritedString(field, func(f *model.PdfField) (string, bool) {
		if f.VariableText != nil && f.VariableText.DA != nil {
			return f.VariableText.DA.Str(), true
		}
		return "", false
	})
}

// ImageFieldOptions defines optional parameters for a push button with image attach capability form field.
type ImageFieldOptions struct {
	Image *model.Image
	style AppearanceStyle
}

func newPushButton(page *model.PdfPage, rect contentdraw.Rectangle, name string, label string, labelColor model.PdfColor, font *model.PdfFont, fontSize *float64, action core.PdfObject) (*model.PdfFieldButton, error) {
	x, y := rect.X, rect.Y
	w := rect.Width
	h := rect.Height
	if rect.FillColor == nil {
		rect.FillColor = model.NewPdfColorDeviceGray(0.7)
	}
	if labelColor == nil {
		labelColor = model.NewPdfColorDeviceGray(0)
	}
	if font == nil {
		stdFont, err := model.NewStandard14Font("Helvetica")
		if err != nil {
			return nil, err
		}
		font = stdFont
	}
	base := model.NewPdfField()
	btn := &model.PdfFieldButton{}
	base.SetContext(btn)
	btn.PdfField = base
	btn.T = core.MakeString(name)
	btn.SetType(model.ButtonTypePush)
	btn.V = core.MakeName("Off")
	btn.Ff = core.MakeInteger(4)
	mk := core.MakeDict()
	mk.Set(*core.MakeName("CA"), core.MakeString(label))
	desc, err := font.GetFontDescriptor()
	if err != nil {
		return nil, err
	}
	fontName := core.MakeName("Helvetica")
	size := 12.0
	if desc != nil && desc.FontName != nil {
		fontName, _ = core.GetName(desc.FontName)
	}
	if fontSize != nil {
		size = *fontSize
	}
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	cc.SetNonStrokingColor(rect.FillColor)
	cc.Add_re(0, 0, w, h)
	cc.Add_f()
	cc.Add_Q()
	cc.Add_q()
	cc.Add_BT()
	textW := 0.0
	for _, r := range label {
		metrics, ok := font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("Font does not have rune metrics for %v - skipping", r)
			continue
		}
		textW += metrics.Wx
	}
	textW = textW / 1000.0 * size
	var capHeight float64
	if desc != nil {
		capHeight, err = desc.GetCapHeight()
		if err != nil {
			common.Log.Debug("ERROR: Unable to get font CapHeight: %v", err)
		}
	}
	if int(capHeight) <= 0 {
		common.Log.Debug("WARN: CapHeight not available - setting to 1000")
		capHeight = 1000
	}
	scaledCap := capHeight / 1000.0 * size
	ty := (h - scaledCap) / 2.0
	tx := (w - textW) / 2.0
	cc.Add_Tf(*fontName, size)
	cc.SetNonStrokingColor(labelColor)
	cc.Add_Td(tx, ty)
	cc.Add_Tj(*core.MakeString(label))
	cc.Add_ET()
	cc.Add_Q()
	form := model.NewXObjectForm()
	form.SetContentStream(cc.Bytes(), core.NewRawEncoder())
	form.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	form.Resources = model.NewPdfPageResources()
	form.Resources.SetFontByName(*fontName, font.ToPdfObject())
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats([]float64{x, y, x + w, y + h})
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = btn.ToPdfObject()
	widget.A = action
	widget.MK = mk
	widget.AP = apDict
	btn.Annotations = append(btn.Annotations, widget)
	return btn, nil
}

// NewFormResetButtonField would create a reset button in specified page according to the parameter in `FormResetActionOptions`.
func NewFormResetButtonField(page *model.PdfPage, opt FormResetActionOptions) (*model.PdfFieldButton, error) {
	action := model.NewPdfActionResetForm()
	action.Fields = opt.Fields
	action.Flags = core.MakeInteger(0)
	if opt.IsExclusionList {
		action.Flags = core.MakeInteger(1)
	}
	btn, err := newPushButton(page, opt.Rectangle, "btnReset", opt.Label, opt.LabelColor, opt.Font, opt.FontSize, action.ToPdfObject())
	if err != nil {
		return nil, err
	}
	return btn, nil
}

func checkboxAppearance(widget *model.PdfAnnotationWidget, _ *model.PdfFieldButton, _ *model.PdfPageResources, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	rectArr, hasRect := core.GetArray(widget.Rect)
	if !hasRect {
		return nil, errors.New("invalid Rect")
	}
	rect, err := model.NewPdfRectangle(*rectArr)
	if err != nil {
		return nil, err
	}
	w, h := rect.Width(), rect.Height()
	formW, formH := w, h
	common.Log.Debug("Checkbox, wa BS: %v", widget.BS)
	zapf, err := model.NewStandard14Font("ZapfDingbats")
	if err != nil {
		return nil, err
	}
	mk, ok := core.GetDict(widget.MK)
	if ok {
		bs, _ := core.GetDict(widget.BS)
		err := style.applyAppearanceCharacteristics(mk, bs, zapf)
		if err != nil {
			return nil, err
		}
	}
	onForm := model.NewXObjectForm()
	{
		cc := contentstream.NewContentCreator()
		if style.BorderSize > 0 {
			drawBorderAndFill(cc, style, w, h)
		}
		if style.DrawAlignmentReticle {
			reticleStyle := style
			reticleStyle.BorderSize = 0.2
			drawAlignmentReticle(cc, reticleStyle, w, h)
		}
		w, h = style.applyRotation(mk, w, h, cc)
		fontSize := style.AutoFontSizeFraction * h
		metrics, ok := zapf.GetRuneMetrics(style.CheckmarkRune)
		if !ok {
			return nil, errors.New("glyph not found")
		}
		encoder := zapf.Encoder()
		encoded := encoder.Encode(string(style.CheckmarkRune))
		glyphW := metrics.Wx * fontSize / 1000.0
		markHeight := 705.0
		glyphH := markHeight / 1000.0 * fontSize
		x := defaultMarginLeft
		if style.MarginLeft != nil {
			x = *style.MarginLeft
		}
		y := 1.0
		if glyphW < w {
			x = (w - glyphW) / 2.0
		}
		if glyphH < h {
			y = (h - glyphH) / 2.0
		}
		cc.Add_q().Add_g(0).Add_BT().Add_Tf("ZaDb", fontSize).Add_Td(x, y).Add_Tj(*core.MakeStringFromBytes(encoded)).Add_ET().Add_Q()
		onForm.Resources = model.NewPdfPageResources()
		onForm.Resources.SetFontByName("ZaDb", zapf.ToPdfObject())
		onForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
		if err := setClippedContent(onForm, cc.Bytes()); err != nil {
			return nil, err
		}
	}
	offForm := model.NewXObjectForm()
	{
		cc := contentstream.NewContentCreator()
		if style.BorderSize > 0 {
			drawBorderAndFill(cc, style, w, h)
		}
		offForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
		if err := setClippedContent(offForm, cc.Bytes()); err != nil {
			return nil, err
		}
	}
	onName := core.PdfObjectName("Yes")
	ap, ok := core.GetDict(widget.AP)
	if ok && ap != nil {
		n := core.TraceToDirectObject(ap.Get("N"))
		switch d := n.(type) {
		case *core.PdfObjectDictionary:
			keys := d.Keys()
			for _, key := range keys {
				if key != "Off" {
					onName = key
				}
			}
		}
	}
	states := core.MakeDict()
	states.Set("Off", offForm.ToPdfObject())
	states.Set(onName, onForm.ToPdfObject())
	apDict := core.MakeDict()
	apDict.Set("N", states)
	return apDict, nil
}
