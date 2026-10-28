// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package annotator provides an interface for creating annotations with appearance
// streams.  It goes beyond the models package which includes definitions of basic annotation models, in that it
// can create the appearance streams which specify the exact appearance as needed by many pdf viewers for consistent
// appearance of the annotations.
// It also contains methods for generating appearance streams for fields via widget annotations.
package annotator

import (
	"math"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/textshaping"
)

func comboOptionAppearance(field *model.PdfField, w, h float64, text string, style AppearanceStyle, daOps *contentstream.ContentStreamOperations, dr *model.PdfPageResources, mk *core.PdfObjectDictionary) (*model.XObjectForm, error) {
	resources := model.NewPdfPageResources()
	formW, formH := w, h
	cc := contentstream.NewContentCreator()
	if style.BorderSize > 0 {
		drawBorderAndFill(cc, style, w, h)
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	cc.Add_BMC("Tx")
	cc.Add_q()
	cc.Add_BT()
	w, h = style.applyRotation(mk, w, h, cc)
	daFont, hasTf, err := style.processDA(field, daOps, dr, resources, cc)
	if err != nil {
		return nil, err
	}
	font := daFont.Font
	fontSize := daFont.Size
	fontName := core.MakeName(daFont.Name)
	autoSize := fontSize == 0
	if autoSize && hasTf {
		fontSize = h * style.AutoFontSizeFraction
	}
	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}
	if len(text) == 0 {
		return nil, nil
	}
	marginLeft := defaultMarginLeft
	if style.MarginLeft != nil {
		marginLeft = *style.MarginLeft
	}
	textWidth := 0.0
	if encoder != nil {
		for _, r := range text {
			metrics, ok := font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("Font does not have rune metrics for %v - skipping", r)
				continue
			}
			textWidth += metrics.Wx
		}
		text = string(encoder.Encode(text))
	}
	if fontSize == 0 || autoSize && textWidth > 0 && marginLeft+textWidth*fontSize/1000.0 > w {
		fontSize = 0.95 * 1000.0 * (w - marginLeft) / textWidth
	}
	lineH := 1.0 * fontSize
	y := 2.0
	{
		needed := lineH
		if autoSize && y+needed > h {
			fontSize = 0.95 * (h - y)
			lineH = 1.0 * fontSize
			needed = lineH
		}
		if h > needed {
			y = (h - needed) / 2.0
			y += 1.50
		}
	}
	cc.Add_Tf(*fontName, fontSize)
	cc.Add_Td(marginLeft, y)
	cc.Add_Tj(*core.MakeString(text))
	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()
	xform := model.NewXObjectForm()
	xform.Resources = resources
	xform.BBox = core.MakeArrayFromFloats([]float64{0, 0, formW, formH})
	if err := setClippedContent(xform, cc.Bytes()); err != nil {
		return nil, err
	}
	return xform, nil
}

// GenerateAppearanceDict generates an appearance dictionary for widget annotation `wa` for the `field` in `form`.
// Implements interface model.FieldAppearanceGenerator.
func (fa FieldAppearance) GenerateAppearanceDict(form *model.PdfAcroForm, field *model.PdfField, wa *model.PdfAnnotationWidget) (*core.PdfObjectDictionary, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("GenerateAppearanceDict for %v  V: %+v", field.PartialName(), field.V)
	}
	_, isText := field.GetContext().(*model.PdfFieldText)
	ap, hasAP := core.GetDict(wa.AP)
	hasAP = hasAP && ap != nil
	hasN := hasAP && ap.Get("N") != nil
	if hasN && fa.OnlyIfMissing && (!isText || !fa.RegenerateTextFields) {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Already populated - ignoring")
		}
		return ap, nil
	}
	if form.DR == nil {
		form.DR = model.NewPdfPageResources()
	}
	switch ctx := field.GetContext().(type) {
	case *model.PdfFieldText:
		tf := ctx
		if da := textFieldDA(tf.PdfField); da == "" {
			tf.DA = form.DA
		}
		if fa.style != nil {
			if fa.style.FieldColors != nil && fa.style.FieldColors[field.PartialName()] != nil {
				color := fa.style.FieldColors[field.PartialName()]
				da, err := daWithColor(tf, color)
				if err != nil {
					return nil, err
				}
				tf.DA = da
			} else if fa.style.TextColor != nil {
				da, err := daWithColor(tf, fa.style.TextColor)
				if err != nil {
					return nil, err
				}
				tf.DA = da
			}
		}
		switch {
		case tf.Flags().Has(model.FieldFlagPassword):
			return nil, nil
		case tf.Flags().Has(model.FieldFlagFileSelect):
			return nil, nil
		case tf.Flags().Has(model.FieldFlagComb):
			if tf.MaxLen != nil {
				ap, err := combTextFieldAppearance(wa, tf, form.DR, fa.Style())
				if err != nil {
					return nil, err
				}
				return ap, nil
			}
		}
		ap, err := textFieldAppearance(wa, tf, form.DR, fa.Style())
		if err != nil {
			return nil, err
		}
		return ap, nil
	case *model.PdfFieldButton:
		btn := ctx
		if btn.IsCheckbox() {
			ap, err := checkboxAppearance(wa, btn, form.DR, fa.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		}
		if btn.IsPush() {
			ap, err := pushButtonAppearance(form, wa, btn, fa.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		}
		common.Log.Debug("TODO: UNHANDLED button type: %+v", btn.GetType())
	case *model.PdfFieldChoice:
		choice := ctx
		switch {
		case choice.Flags().Has(model.FieldFlagCombo):
			ap, err := comboBoxAppearance(form, wa, choice, fa.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		default:
			ap, err := listBoxAppearance(form, wa, choice, fa.Style())
			if err != nil {
				return nil, err
			}
			return ap, nil
		}
	case *model.PdfFieldSignature:
		sigField := ctx
		if hasN {
			return ap, nil
		}
		lines := signatureLines(sigField.V)
		if len(lines) == 0 {
			common.Log.Debug("Signature field %s has no printable signature info - skipping appearance generation", field.PartialName())
			return ap, nil
		}
		opts := NewSignatureFieldOpts()
		if arr, ok := core.GetArray(wa.Rect); ok {
			if rect, err := arr.ToFloat64Array(); err == nil && len(rect) == 4 {
				opts.Rect = []float64{math.Min(rect[0], rect[2]), math.Min(rect[1], rect[3]), math.Max(rect[0], rect[2]), math.Max(rect[1], rect[3])}
			}
		}
		da := variableTextDA(field)
		if da == "" && form.DA != nil {
			da = form.DA.Str()
		}
		daOps, err := contentstream.NewContentStreamParser(da).Parse()
		if err != nil {
			return nil, err
		}
		style := fa.Style()
		daFont, _, err := style.processDA(field, daOps, form.DR, nil, contentstream.NewContentCreator())
		if err != nil {
			return nil, err
		}
		opts.Font = daFont.Font
		if daFont.Size > 0 {
			opts.FontSize = daFont.Size
		}
		if color := colorFromOps(daOps); color != nil {
			opts.TextColor = color
		}
		if style.TextColor != nil {
			opts.TextColor = style.TextColor
		}
		sigAP, err := signatureAppearance(lines, opts)
		if err != nil {
			return nil, err
		}
		if hasAP {
			ap.Set("N", sigAP.Get("N"))
			return ap, nil
		}
		return sigAP, nil
	default:
		common.Log.Debug("TODO: UNHANDLED field type: %T", ctx)
	}
	return nil, nil
}

// Style returns the appearance style of `fa`. If not specified, returns default style.
func (fa FieldAppearance) Style() AppearanceStyle {
	if fa.style != nil {
		return *fa.style
	}
	margin := defaultMarginLeft
	return AppearanceStyle{AutoFontSizeFraction: 0.65, CheckmarkRune: '✔', BorderSize: 0.0, BorderColor: model.NewPdfColorDeviceGray(0), FillColor: model.NewPdfColorDeviceGray(1), MultilineLineHeight: 1.2, MultilineVAlignMiddle: false, DrawAlignmentReticle: false, AllowMK: true, MarginLeft: &margin, ListBoxHighlightColor: defaultHighlightColor()}
}

// AppearanceFont represents a font used for generating the appearance of a
// field in the filling/flattening process.
type AppearanceFont struct {
	// Name represents the name of the font which will be added to the
	// AcroForm resources (DR).
	Name string

	// Font represents the actual font used for the field appearance.
	Font *model.PdfFont

	// Size represents the size of the font used for the field appearance.
	// If the font size is 0, the value of the FallbackSize field of the
	// AppearanceFontStyle is used, if set. Otherwise, the font size is
	// calculated based on the available annotation height and on the
	// AutoFontSizeFraction field of the AppearanceStyle.
	Size float64
}

// TextFieldOptions defines optional parameter for a text field in a form.
type TextFieldOptions struct {
	MaxLen int
	Value  string

	// TextColor defines the color of the text in hex format. e.g #43fd23.
	// If it has an invalid value a #000000 (black) color is taken as default
	TextColor string

	// FontName defines the font of the text. Helvetica font is the default one.
	// It is recommended to use one of 14 standard PDF fonts.
	FontName string

	// FontSize defines the font size of the text, 12 is used by default.
	FontSize int
}

// FormSubmitActionOptions holds options for creating a form submit button.
type FormSubmitActionOptions struct {
	// Rectangle holds the button position, size, and color.
	Rectangle contentdraw.Rectangle

	// Url specifies the URL where the fieds will be submitted.
	Url string

	// Label specifies the text that would be displayed on the button.
	Label string

	// LabelColor specifies the button label color.
	LabelColor model.PdfColor

	// Font specifies a font used for rendering the button label.
	// When omitted it will fallback to use a Helvetica font.
	Font *model.PdfFont

	// FontSize specifies the font size used in rendering the button label.
	// The default font size is 12pt.
	FontSize *float64

	// Fields specifies list of fields that could be submitted.
	// This list may contain indirect object to fields or field names.
	Fields *core.PdfObjectArray

	// IsExclusionList specifies that the fields contain in `Fields` array would not be submitted.
	IsExclusionList bool

	// IncludeEmptyFields specifies if all fields would be submitted even though it's value is empty.
	IncludeEmptyFields bool

	// SubmitAsPDF specifies that the document shall be submitted as PDF.
	// If set then all the other flags shall be ignored.
	SubmitAsPDF bool
}

func shapeText(s string) string {
	if len(s) == 0 {
		return s
	}
	ltr := core.IsTextWriteDirectionLTR(s)
	if !ltr {
		shaped, err := textshaping.ArabicShape(s)
		if err != nil {
			common.Log.Debug("ERROR applying Arabic text shaping: %v", err.Error())
			return s
		}
		return core.FormatWriteDirectionLTR(shaped, false)
	}
	return s
}

// SignatureImagePosition specifies the image signature location relative to the text signature.
// If text signature is not defined, this position will be ignored.
type SignatureImagePosition int

func inheritedString(field *model.PdfField, get func(*model.PdfField) (string, bool)) string {
	for f := field; f != nil; f = f.Parent {
		if s, ok := get(f); ok {
			return s
		}
	}
	return ""
}

// InkAnnotationDef holds base information for constructing an ink annotation.
type InkAnnotationDef struct {
	// Paths is the array of stroked paths which compose the annotation.
	Paths []contentdraw.Path

	// Color is the color of the line. Default to black.
	Color *model.PdfColorDeviceRGB

	// LineWidth is the width of the line.
	LineWidth float64
}
