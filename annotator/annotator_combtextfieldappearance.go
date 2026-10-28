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
	"fmt"
	"image"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/creator"
	"github.com/OpenMellon/openpdf/internal/textencoding"
	"github.com/OpenMellon/openpdf/model"
)

func combTextFieldAppearance(widget *model.PdfAnnotationWidget, field *model.PdfFieldText, dr *model.PdfPageResources, style AppearanceStyle) (*core.PdfObjectDictionary, error) {
	resources := model.NewPdfPageResources()
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
	mk, ok := core.GetDict(widget.MK)
	if ok {
		bs, _ := core.GetDict(widget.BS)
		err := style.applyAppearanceCharacteristics(mk, bs, nil)
		if err != nil {
			return nil, err
		}
	}
	maxLen, ok := core.GetIntVal(field.MaxLen)
	if !ok {
		return nil, errors.New("maxlen not set")
	}
	if maxLen <= 0 {
		return nil, errors.New("maxLen invalid")
	}
	cellW := w / float64(maxLen)
	daOps, err := contentstream.NewContentStreamParser(textFieldDA(field.PdfField)).Parse()
	if err != nil {
		return nil, err
	}
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
	_, h = style.applyRotation(mk, w, h, cc)
	cc.Add_BT()
	daFont, hasTf, err := style.processDA(field.PdfField, daOps, dr, resources, cc)
	if err != nil {
		return nil, err
	}
	font := daFont.Font
	fontName := core.MakeName(daFont.Name)
	fontSize := daFont.Size
	autoSize := fontSize == 0
	if autoSize && hasTf {
		fontSize = h * style.AutoFontSizeFraction
	}
	encoder := font.Encoder()
	if encoder == nil {
		common.Log.Debug("WARN: font encoder is nil. Assuming identity encoder. Output may be incorrect.")
		encoder = textencoding.NewIdentityTextEncoder("Identity-H")
	}
	var text string
	if str, ok := core.GetString(field.V); ok {
		text = str.Decoded()
	}
	text = shapeText(text)
	cc.Add_Tf(*fontName, fontSize)
	var maxGlyph float64
	for _, r := range text {
		metrics, ok := font.GetRuneMetrics(r)
		if !ok {
			common.Log.Debug("ERROR: Rune not found in font: %v - skipping over", r)
			continue
		}
		glyphSize := metrics.Wy
		if int(glyphSize) <= 0 {
			glyphSize = metrics.Wx
		}
		if glyphSize > maxGlyph {
			maxGlyph = glyphSize
		}
	}
	if int(maxGlyph) == 0 {
		common.Log.Debug("ERROR: Unable to determine max glyph size - using 1000")
		maxGlyph = 1000
	}
	desc, err := font.GetFontDescriptor()
	if err != nil {
		common.Log.Debug("Error: Unable to get font descriptor")
	}
	var capHeight float64
	if desc != nil {
		capHeight, err = desc.GetCapHeight()
		if err != nil {
			common.Log.Debug("ERROR: Unable to get font CapHeight: %v", err)
		}
	}
	if int(capHeight) <= 0 {
		common.Log.Debug("WARN: CapHeight not available - setting to 1000")
		capHeight = 1000.0
	}
	scaledCap := capHeight / 1000.0 * fontSize
	y := 0.0
	glyphH := 1.0 * fontSize * (maxGlyph / 1000.0)
	{
		needed := glyphH
		if autoSize && y+needed > h {
			fontSize = 0.95 * (h - y)
			scaledCap = capHeight / 1000.0 * fontSize
		}
		if h > scaledCap {
			y = (h - scaledCap) / 2.0
		}
	}
	cc.Add_Td(0, y)
	if q, ok := core.GetIntVal(field.Q); ok {
		switch q {
		case 2:
			if len(text) < maxLen {
				pad := float64(maxLen-len(text)) * cellW
				cc.Add_Td(pad, 0)
			}
		}
	}
	for i, r := range text {
		x := defaultMarginLeft
		if style.MarginLeft != nil {
			x = *style.MarginLeft
		}
		s := string(r)
		if encoder != nil {
			metrics, ok := font.GetRuneMetrics(r)
			if !ok {
				common.Log.Debug("ERROR: Rune not found in font: %v - skipping over", r)
				continue
			}
			s = string(encoder.Encode(s))
			glyphW := fontSize * metrics.Wx / 1000.0
			cx := (cellW - glyphW) / 2
			x = cx
		}
		cc.Add_Td(x, 0)
		cc.Add_Tj(*core.MakeString(s))
		if i != len(text)-1 {
			cc.Add_Td(cellW-x, 0)
		}
	}
	cc.Add_ET()
	cc.Add_Q()
	cc.Add_EMC()
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

// NewComboboxField generates a new combobox form field with partial name `name` at location `rect`
// on specified `page` and with field specific options `opt`.
func NewComboboxField(page *model.PdfPage, name string, rect []float64, opt ComboboxFieldOptions) (*model.PdfFieldChoice, error) {
	if page == nil {
		return nil, errors.New("page not specified")
	}
	if len(name) <= 0 {
		return nil, errors.New("required attribute not specified")
	}
	if len(rect) != 4 {
		return nil, errors.New("invalid range")
	}
	base := model.NewPdfField()
	choice := &model.PdfFieldChoice{}
	base.SetContext(choice)
	choice.PdfField = base
	choice.T = core.MakeString(name)
	choice.Opt = core.MakeArray()
	for _, c := range opt.Choices {
		choice.Opt.Append(core.MakeString(c))
	}
	choice.SetFlag(model.FieldFlagCombo)
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats(rect)
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = choice.ToPdfObject()
	choice.Annotations = append(choice.Annotations, widget)
	return choice, nil
}

// CircleAnnotationDef defines a circle annotation or ellipse at position (X, Y) and Width and Height.
// The annotation has various style parameters including Fill and Border options and Opacity.
type CircleAnnotationDef struct {
	X             float64
	Y             float64
	Width         float64
	Height        float64
	FillEnabled   bool
	FillColor     *model.PdfColorDeviceRGB
	BorderEnabled bool
	BorderWidth   float64
	BorderColor   *model.PdfColorDeviceRGB
	Opacity       float64
}

// SignatureFieldOpts represents a set of options used to configure
// an appearance widget dictionary.
type SignatureFieldOpts struct {
	// Rect represents the area the signature annotation is displayed on.
	Rect []float64

	// AutoSize specifies if the content of the appearance should be
	// scaled to fit in the annotation rectangle.
	AutoSize bool

	// Font specifies the font of the text content.
	Font *model.PdfFont

	// FontSize specifies the size of the text content.
	FontSize float64

	// LineHeight specifies the height of a line of text in the appearance annotation.
	LineHeight float64

	// TextColor represents the color of the text content displayed.
	TextColor model.PdfColor

	// FillColor represents the background color of the appearance annotation area.
	FillColor model.PdfColor

	// FillOpacity sets the opacity of the background of the appearance annotation area.
	// Range is 0.0 - 1.0. Default is 1.0, opaque.
	FillOpacity float64

	// BorderSize represents border size of the appearance annotation area.
	BorderSize float64

	// BorderColor represents the border color of the appearance annotation area.
	BorderColor model.PdfColor

	// WatermarkImage specifies the image used as a watermark that will be rendered
	// behind the signature.
	WatermarkImage image.Image

	// Image represents the image used for the signature appearance.
	Image image.Image

	// Encoder specifies the image encoder used for image signature. Defaults to flate encoder.
	Encoder core.StreamEncoder

	// ImagePosition specifies the image location relative to the text signature.
	ImagePosition SignatureImagePosition
}

func (s *AppearanceStyle) applyRotation(mk *core.PdfObjectDictionary, w, h float64, cc *contentstream.ContentCreator) (float64, float64) {
	if !s.AllowMK {
		return w, h
	}
	if mk == nil {
		return w, h
	}
	angle, _ := core.GetNumberAsFloat(mk.Get("R"))
	if angle == 0 {
		return w, h
	}
	negAngle := -angle
	bounds := contentdraw.Path{Points: []contentdraw.Point{contentdraw.NewPoint(0, 0).Rotate(negAngle), contentdraw.NewPoint(w, 0).Rotate(negAngle), contentdraw.NewPoint(0, h).Rotate(negAngle), contentdraw.NewPoint(w, h).Rotate(negAngle)}}.GetBoundingBox()
	cc.RotateDeg(angle)
	cc.Translate(bounds.X, bounds.Y)
	return bounds.Width, bounds.Height
}

func textFieldDA(field *model.PdfField) string {
	return inheritedString(field, func(f *model.PdfField) (string, bool) {
		if tf, ok := f.GetContext().(*model.PdfFieldText); ok && tf.DA != nil {
			return tf.DA.Str(), true
		}
		return "", false
	})
}

// FormResetActionOptions holds options for creating a form reset button.
type FormResetActionOptions struct {
	// Rectangle holds the button position, size, and color.
	Rectangle contentdraw.Rectangle

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

	// Fields specifies list of fields that could be resetted.
	// This list may contain indirect object to fields or field names.
	Fields *core.PdfObjectArray

	// IsExclusionList specifies that the fields in the `Fields` array would be excluded form reset process.
	IsExclusionList bool
}

// NewTextField generates a new text field with partial name `name` at location
// specified by `rect` on given `page` and with field specific options `opt`.
func NewTextField(page *model.PdfPage, name string, rect []float64, opt TextFieldOptions) (*model.PdfFieldText, error) {
	if page == nil {
		return nil, errors.New("page not specified")
	}
	if len(name) <= 0 {
		return nil, errors.New("required attribute not specified")
	}
	if len(rect) != 4 {
		return nil, errors.New("invalid range")
	}
	base := model.NewPdfField()
	tf := &model.PdfFieldText{}
	base.SetContext(tf)
	tf.PdfField = base
	tf.T = core.MakeString(name)
	if opt.MaxLen > 0 {
		tf.MaxLen = core.MakeInteger(int64(opt.MaxLen))
	}
	if len(opt.Value) > 0 {
		tf.V = core.MakeString(opt.Value)
	}
	if opt.TextColor != "" {
		color := creator.ColorRGBFromHex(opt.TextColor)
		r, g, b := color.ToRGB()
		fontSize := 12
		if opt.FontSize > 0 {
			fontSize = opt.FontSize
		}
		fontName := "Helvetica"
		if opt.FontName != "" {
			fontName = opt.FontName
		}
		da := fmt.Sprintf("/%s %d Tf %.3f %.3f %.3f rg", fontName, fontSize, r, g, b)
		tf.DA = core.MakeString(da)
	}
	base.SetContext(tf)
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats(rect)
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = tf.ToPdfObject()
	tf.Annotations = append(tf.Annotations, widget)
	return tf, nil
}
