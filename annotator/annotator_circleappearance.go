// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package annotator provides an interface for creating annotations with appearance
// streams.  It goes beyond the models package which includes definitions of basic annotation models, in that it
// can create the appearance streams which specify the exact appearance as needed by many pdf viewers for consistent
// appearance of the annotations.
// It also contains methods for generating appearance streams for fields via widget annotations.
package annotator

import (
	"bytes"
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
)

func circleAppearance(def CircleAnnotationDef) (*core.PdfObjectDictionary, *model.PdfRectangle, error) {
	form := model.NewXObjectForm()
	form.Resources = model.NewPdfPageResources()
	gsName := ""
	if def.Opacity < 1.0 {
		gsDict := core.MakeDict()
		gsDict.Set("ca", core.MakeFloat(def.Opacity))
		gsDict.Set("CA", core.MakeFloat(def.Opacity))
		err := form.Resources.AddExtGState("gs1", gsDict)
		if err != nil {
			common.Log.Debug("Unable to add extgstate gs1")
			return nil, nil, err
		}
		gsName = "gs1"
	}
	content, bbox, rect, err := circleAppearanceStream(def, gsName)
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

// NewCheckboxField generates a new checkbox field with partial name `name` at location `rect`
// on specified `page` and with field specific options `opt`.
func NewCheckboxField(page *model.PdfPage, name string, rect []float64, opt CheckboxFieldOptions) (*model.PdfFieldButton, error) {
	if page == nil {
		return nil, errors.New("page not specified")
	}
	if len(name) <= 0 {
		return nil, errors.New("required attribute not specified")
	}
	if len(rect) != 4 {
		return nil, errors.New("invalid range")
	}
	zapf, err := model.NewStandard14Font(model.ZapfDingbatsName)
	if err != nil {
		return nil, err
	}
	base := model.NewPdfField()
	cb := &model.PdfFieldButton{}
	base.SetContext(cb)
	cb.PdfField = base
	cb.T = core.MakeString(name)
	cb.SetType(model.ButtonTypeCheckbox)
	state := "Off"
	if opt.Checked {
		state = "Yes"
	}
	cb.V = core.MakeName(state)
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats(rect)
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = cb.ToPdfObject()
	w := rect[2] - rect[0]
	h := rect[3] - rect[1]
	var buf bytes.Buffer
	buf.WriteString("q\n")
	buf.WriteString("0 0 1 rg\n")
	buf.WriteString("BT\n")
	buf.WriteString("/ZaDb 12 Tf\n")
	buf.WriteString("ET\n")
	buf.WriteString("Q\n")
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	cc.Add_rg(0, 0, 1)
	cc.Add_BT()
	cc.Add_Tf(*core.MakeName("ZaDb"), 12)
	cc.Add_Td(0, 0)
	cc.Add_ET()
	cc.Add_Q()
	offForm := model.NewXObjectForm()
	offForm.SetContentStream(cc.Bytes(), core.NewRawEncoder())
	offForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	offForm.Resources = model.NewPdfPageResources()
	offForm.Resources.SetFontByName("ZaDb", zapf.ToPdfObject())
	cc = contentstream.NewContentCreator()
	cc.Add_q()
	cc.Add_re(0, 0, w, h)
	cc.Add_W().Add_n()
	cc.Add_rg(0, 0, 1)
	cc.Translate(0, 3.0)
	cc.Add_BT()
	cc.Add_Tf(*core.MakeName("ZaDb"), 12)
	cc.Add_Td(0, 0)
	cc.Add_Tj(*core.MakeString("4"))
	cc.Add_ET()
	cc.Add_Q()
	onForm := model.NewXObjectForm()
	onForm.SetContentStream(cc.Bytes(), core.NewRawEncoder())
	onForm.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	onForm.Resources = model.NewPdfPageResources()
	onForm.Resources.SetFontByName("ZaDb", zapf.ToPdfObject())
	states := core.MakeDict()
	states.Set("Off", offForm.ToPdfObject())
	states.Set("Yes", onForm.ToPdfObject())
	apDict := core.MakeDict()
	apDict.Set("N", states)
	widget.AP = apDict
	widget.AS = core.MakeName(state)
	cb.Annotations = append(cb.Annotations, widget)
	return cb, nil
}

// NewSignatureLine returns a new signature line displayed as a part of the
// signature field appearance.
func NewSignatureLine(desc, text string) *SignatureLine {
	return &SignatureLine{Desc: desc, Text: text}
}

func rectangleAppearanceStream(def RectangleAnnotationDef, gsName string) ([]byte, *model.PdfRectangle, *model.PdfRectangle, error) {
	rectangle := contentdraw.Rectangle{X: 0, Y: 0, Width: def.Width, Height: def.Height, FillEnabled: def.FillEnabled, FillColor: def.FillColor, BorderEnabled: def.BorderEnabled, BorderWidth: 2 * def.BorderWidth, BorderColor: def.BorderColor, Opacity: def.Opacity}
	content, bbox, err := rectangle.Draw(gsName)
	if err != nil {
		return nil, nil, nil, err
	}
	rect := &model.PdfRectangle{}
	rect.Llx = def.X + bbox.Llx
	rect.Lly = def.Y + bbox.Lly
	rect.Urx = def.X + bbox.Urx
	rect.Ury = def.Y + bbox.Ury
	return content, bbox, rect, nil
}

// NewSignatureField returns a new signature field with a visible appearance
// containing the specified signature lines and styled according to the
// specified options.
func NewSignatureField(signature *model.PdfSignature, lines []*SignatureLine, opts *SignatureFieldOpts) (*model.PdfFieldSignature, error) {
	if signature == nil {
		return nil, errors.New("signature cannot be nil")
	}
	ap, err := signatureAppearance(lines, opts)
	if err != nil {
		return nil, err
	}
	field := model.NewPdfFieldSignature(signature)
	field.Rect = core.MakeArrayFromFloats(opts.Rect)
	field.AP = ap
	return field, nil
}

type quadding int

func listBoxAppearance(form *model.PdfAcroForm, widget *model.PdfAnnotationWidget, field *model.PdfFieldChoice, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
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
	common.Log.Debug("Choice (list box), wa BS: %v", widget.BS)
	daOps, err := contentstream.NewContentStreamParser(textFieldDA(field.PdfField)).Parse()
	if err != nil {
		return nil, err
	}
	mk, hasMK := core.GetDict(widget.MK)
	if hasMK {
		bs, _ := core.GetDict(widget.BS)
		if err := style.applyAppearanceCharacteristics(mk, bs, nil); err != nil {
			return nil, err
		}
	}
	var options []listboxOption
	if field.Opt != nil {
		for _, opt := range field.Opt.Elements() {
			var option listboxOption
			if pair, ok := core.GetArray(opt); ok && pair.Len() == 2 {
				value, okValue := nameOrString(pair.Get(0))
				label, okLabel := nameOrString(pair.Get(1))
				if !okValue || !okLabel {
					common.Log.Debug("ERROR: Opt pair elements not name/string - %v", pair)
					return nil, errors.New("invalid option pair")
				}
				option.value, option.label = value, label
			} else if s, ok := nameOrString(opt); ok {
				option.label = s
				option.value = s
			} else {
				common.Log.Debug("ERROR: Opt not a name/string - %T", opt)
				return nil, errors.New("not a name/string")
			}
			options = append(options, option)
		}
	}
	selected := selectedOptions(field, options)
	resources := model.NewPdfPageResources()
	cc := contentstream.NewContentCreator()
	cc.Add_q()
	w, h = style.applyRotation(mk, w, h, cc)
	if style.BorderSize > 0 {
		drawBorderAndFill(cc, style, w, h)
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	daFont, _, err := style.processDA(field.PdfField, daOps, form.DR, resources, cc)
	if err != nil {
		return nil, err
	}
	font := daFont.Font
	fontSize := daFont.Size
	if fontSize <= 0 {
		fontSize = 12.0
	}
	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}
	marginLeft := defaultMarginLeft
	if style.MarginLeft != nil {
		marginLeft = *style.MarginLeft
	}
	rowH := fontSize * 1.15
	if rowH > h && h > 0 {
		fontSize = h / 1.15
		rowH = h
	}
	maxRows := max(int(h/rowH), 1)
	first := 0
	if ti, ok := core.GetIntVal(field.TI); ok && ti >= 0 && ti < len(options) {
		first = ti
	} else if len(selected) > 0 {
		minSel := len(options)
		for i := range selected {
			if i < minSel {
				minSel = i
			}
		}
		if minSel >= maxRows {
			first = minSel
			if maxFirst := len(options) - maxRows; first > maxFirst {
				first = maxFirst
			}
		}
	}
	highlight := style.ListBoxHighlightColor
	if highlight == nil {
		highlight = defaultHighlightColor()
	}
	borderW := style.BorderSize
	if borderW < 0 {
		borderW = 0
	}
	type visibleRow struct {
		index int
		textY float64
		rowY  float64
	}
	var rows []visibleRow
	top := h
	for i := first; i < len(options) && len(rows) < maxRows; i++ {
		bottom := top - rowH
		baseline := bottom + (rowH-fontSize)/2.0 + fontSize*0.2
		rows = append(rows, visibleRow{index: i, textY: baseline, rowY: bottom})
		top = bottom
	}
	hlW := w - 2*borderW
	if hlW < 0 {
		hlW = 0
	}
	for _, row := range rows {
		if !selected[row.index] {
			continue
		}
		cc.Add_q().SetNonStrokingColor(highlight).Add_re(borderW, row.rowY, hlW, rowH).Add_f().Add_Q()
	}
	cc.Add_BMC("Tx")
	cc.Add_q()
	cc.Add_BT()
	cc.Add_Tf(*core.MakeName(daFont.Name), fontSize)
	prevY := 0.0
	for i, row := range rows {
		if i == 0 {
			cc.Add_Td(marginLeft, row.textY)
		} else {
			cc.Add_Td(0, row.textY-prevY)
		}
		prevY = row.textY
		label := options[row.index].label
		if len(label) == 0 {
			continue
		}
		cc.Add_Tj(*core.MakeString(string(encoder.Encode(label))))
	}
	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()
	cc.Add_Q()
	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
	if err := setClippedContent(xform, cc.Bytes()); err != nil {
		return nil, err
	}
	apDict := core.MakeDict()
	apDict.Set("N", xform.ToPdfObject())
	return apDict, nil
}

func nameOrString(obj core.PdfObject) (string, bool) {
	if s, ok := core.GetString(obj); ok {
		return s.Decoded(), true
	}
	if nm, ok := core.GetName(obj); ok {
		return nm.String(), true
	}
	return "", false
}

func (f *AppearanceFont) fillName() {
	if f.Font == nil || f.Name != "" {
		return
	}
	desc := f.Font.FontDescriptor()
	if desc == nil || desc.FontName == nil {
		return
	}
	f.Name = desc.FontName.String()
}

// WrapContentStream ensures that the entire content stream for a `page` is wrapped within q ... Q operands.
// Ensures that following operands that are added are not affected by additional operands that are added.
// Implements interface model.ContentStreamWrapper.
func (a ImageFieldAppearance) WrapContentStream(page *model.PdfPage) error {
	content, err := page.GetAllContentStreams()
	if err != nil {
		return err
	}
	parser := contentstream.NewContentStreamParser(content)
	ops, err := parser.Parse()
	if err != nil {
		return err
	}
	ops.WrapIfNeeded()
	contents := []string{ops.String()}
	return page.SetContentStreams(contents, newFlateEncoder())
}
