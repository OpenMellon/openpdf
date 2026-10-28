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
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/precision"
	"github.com/OpenMellon/openpdf/model"
)

// WrapContentStream ensures that the entire content stream for a `page` is wrapped within q ... Q operands.
// Ensures that following operands that are added are not affected by additional operands that are added.
// Implements interface model.ContentStreamWrapper.
func (fa FieldAppearance) WrapContentStream(page *model.PdfPage) error {
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

// NewImageField generates a new image field with partial name `name` at location `rect`
// on specified `page` and with field specific options `opt`.
func NewImageField(page *model.PdfPage, name string, rect []float64, opt ImageFieldOptions) (*model.PdfFieldButton, error) {
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
	btn := &model.PdfFieldButton{}
	btn.PdfField = base
	base.SetContext(btn)
	btn.SetType(model.ButtonTypePush)
	btn.T = core.MakeString(name)
	widget := model.NewPdfAnnotationWidget()
	widget.Rect = core.MakeArrayFromFloats(rect)
	widget.P = page.ToPdfObject()
	widget.F = core.MakeInteger(4)
	widget.Parent = btn.ToPdfObject()
	w := rect[2] - rect[0]
	h := rect[3] - rect[1]
	style := opt.style
	cc := contentstream.NewContentCreator()
	if style.BorderSize > 0 {
		drawBorderAndFill(cc, style, w, h)
	}
	if style.DrawAlignmentReticle {
		reticleStyle := style
		reticleStyle.BorderSize = 0.2
		drawAlignmentReticle(cc, reticleStyle, w, h)
	}
	frm, err := imageForm(w, h, opt.Image, style)
	if err != nil {
		return nil, err
	}
	mk, ok := core.GetDict(widget.MK)
	if ok {
		mk.Set("l", frm.ToPdfObject())
	}
	xobjs := core.MakeDict()
	xobjs.Set("FRM", frm.ToPdfObject())
	resources := model.NewPdfPageResources()
	resources.ProcSet = core.MakeArray(core.MakeName("PDF"))
	resources.XObject = xobjs
	innerW := w - 2
	innerH := h - 2
	cc.Add_q()
	cc.Add_re(1, 1, innerW, innerH)
	cc.Add_W()
	cc.Add_n()
	innerW -= 2
	innerH -= 2
	cc.Add_q()
	cc.Add_re(2, 2, innerW, innerH)
	cc.Add_W()
	cc.Add_n()
	scale := math.Min(innerW/float64(opt.Image.Width), innerH/float64(opt.Image.Height))
	cc.Add_cm(scale, 0, 0, scale, (w/2)-(float64(opt.Image.Width)*scale/2)+2, 2)
	cc.Add_Do("FRM")
	cc.Add_Q()
	cc.Add_Q()
	form := model.NewXObjectForm()
	form.FormType = core.MakeInteger(1)
	form.Resources = resources
	form.BBox = core.MakeArrayFromFloats([]float64{0, 0, w, h})
	form.Matrix = core.MakeArrayFromFloats([]float64{1.0, 0.0, 0.0, 1.0, 0.0, 0.0})
	form.SetContentStream(cc.Bytes(), newFlateEncoder())
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	widget.AP = apDict
	btn.Annotations = append(btn.Annotations, widget)
	return btn, nil
}

// LineAnnotationDef defines a line between point 1 (X1,Y1) and point 2 (X2,Y2).  The line ending styles can be none
// (regular line), or arrows at either end.  The line also has a specified width, color and opacity.
type LineAnnotationDef struct {
	X1               float64
	Y1               float64
	X2               float64
	Y2               float64
	LineColor        *model.PdfColorDeviceRGB
	Opacity          float64
	LineWidth        float64
	LineEndingStyle1 contentdraw.LineEndingStyle
	LineEndingStyle2 contentdraw.LineEndingStyle
}

func inkAppearance(def *InkAnnotationDef) (*core.PdfObjectDictionary, *model.PdfRectangle, error) {
	form := model.NewXObjectForm()
	content, bbox, err := inkAppearanceStream(def)
	if err != nil {
		return nil, nil, err
	}
	err = form.SetContentStream(content, nil)
	if err != nil {
		return nil, nil, err
	}
	form.BBox = bbox.ToPdfObject()
	form.Resources = model.NewPdfPageResources()
	form.Resources.ProcSet = core.MakeArray(core.MakeName("PDF"))
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, bbox, nil
}

// AppearanceStyle defines style parameters for appearance stream generation.
type AppearanceStyle struct {
	// How much of Rect height to fill when autosizing text.
	AutoFontSizeFraction float64

	// CheckmarkRune is a rune used for check mark in checkboxes (for ZapfDingbats font).
	CheckmarkRune rune
	BorderSize    float64
	BorderColor   model.PdfColor
	FillColor     model.PdfColor

	// ListBoxHighlightColor is the background color used to highlight the
	// selected option(s) of a list box choice field. If nil, a default
	// light blue is used.
	ListBoxHighlightColor model.PdfColor

	// Multiplier for lineheight for multi line text.
	MultilineLineHeight   float64
	MultilineVAlignMiddle bool

	// Visual guide checking alignment of field contents (debugging).
	DrawAlignmentReticle bool

	// Allow field MK appearance characteristics to override style settings.
	AllowMK bool

	// Fonts holds appearance styles for fonts.
	Fonts *AppearanceFontStyle

	// MarginLeft represents the amount of space to leave on the left side of
	// the form field bounding box when generating appearances (default: 2.0).
	MarginLeft *float64
	TextColor  model.PdfColor

	// FieldColors holds separate set of appearance styles for fields.
	FieldColors map[string]model.PdfColor
}

func bezierControlPoints(pts []contentdraw.Point) (ctrl1 []contentdraw.Point, ctrl2 []contentdraw.Point, err error) {
	n := len(pts) - 1
	if len(pts) < 1 {
		return nil, nil, errors.New("at least two points required to calculate curve control points")
	}
	if n == 1 {
		c1 := contentdraw.Point{X: (2*pts[0].X + pts[1].X) / 3, Y: (2*pts[0].Y + pts[1].Y) / 3}
		ctrl1 = append(ctrl1, c1)
		ctrl2 = append(ctrl2, contentdraw.Point{X: 2*c1.X - pts[0].X, Y: 2*c1.Y - pts[0].Y})
		return ctrl1, ctrl2, nil
	}
	rhs := make([]float64, n)
	for i := 1; i < n-1; i++ {
		rhs[i] = 4*pts[i].X + 2*pts[i+1].X
	}
	rhs[0] = pts[0].X + 2*pts[1].X
	rhs[n-1] = (8*pts[n-1].X + pts[n].X) / 2.0
	xs := firstControlPoints(rhs)
	for i := 1; i < n-1; i++ {
		rhs[i] = 4*pts[i].Y + 2*pts[i+1].Y
	}
	rhs[0] = pts[0].Y + 2*pts[1].Y
	rhs[n-1] = (8*pts[n-1].Y + pts[n].Y) / 2.0
	ys := firstControlPoints(rhs)
	ctrl1 = make([]contentdraw.Point, n)
	ctrl2 = make([]contentdraw.Point, n)
	for i := 0; i < n; i++ {
		ctrl1[i] = contentdraw.Point{X: precision.RoundDefault(xs[i]), Y: precision.RoundDefault(ys[i])}
		if i < n-1 {
			ctrl2[i] = contentdraw.Point{X: precision.RoundDefault(2*pts[i+1].X - xs[i+1]), Y: precision.RoundDefault(2*pts[i+1].Y - ys[i+1])}
		} else {
			ctrl2[i] = contentdraw.Point{X: precision.RoundDefault((pts[n].X + xs[n-1]) / 2), Y: precision.RoundDefault((pts[n].Y + ys[n-1]) / 2)}
		}
	}
	return ctrl1, ctrl2, nil
}

// AppearanceFontStyle defines font style characteristics for form fields,
// used in the filling/flattening process.
type AppearanceFontStyle struct {
	// Fallback represents a global font fallback, used for fields which do
	// not specify a font in their default appearance (DA). The fallback is
	// also used if there is a font specified in the DA, but it is not
	// found in the AcroForm resources (DR).
	Fallback *AppearanceFont

	// FallbackSize represents a global font size fallback used for fields
	// which do not specify a font size in their default appearance (DA).
	// The fallback size is applied only if its value is larger than zero.
	FallbackSize float64

	// FieldFallbacks defines font fallbacks for specific fields. The map keys
	// represent the names of the fields (which can be specified by their
	// partial or full names). Specific field fallback fonts take precedence
	// over the global font fallback.
	FieldFallbacks map[string]*AppearanceFont

	// ForceReplace forces the replacement of fonts in the filling/flattening
	// process, even if the default appearance (DA) specifies a valid font.
	// If no fallback font is provided, setting this field has no effect.
	ForceReplace bool
}

// RectangleAnnotationDef is a rectangle defined with a specified Width and Height and a lower left corner at (X,Y).
// The rectangle can optionally have a border and a filling color.
// The Width/Height includes the border (if any specified).
type RectangleAnnotationDef struct {
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

func colorFromOps(ops *contentstream.ContentStreamOperations) model.PdfColor {
	if ops == nil {
		return nil
	}
	var color model.PdfColor
	for _, op := range *ops {
		nums, err := core.GetNumbersAsFloat(op.Params)
		if err != nil {
			continue
		}
		switch {
		case op.Operand == "g" && len(nums) == 1:
			color = model.NewPdfColorDeviceGray(nums[0])
		case op.Operand == "rg" && len(nums) == 3:
			color = model.NewPdfColorDeviceRGB(nums[0], nums[1], nums[2])
		case op.Operand == "k" && len(nums) == 4:
			color = model.NewPdfColorDeviceCMYK(nums[0], nums[1], nums[2], nums[3])
		}
	}
	return color
}

func setClippedContent(form *model.XObjectForm, content []byte) error {
	var bbox []float64
	if arr, ok := core.GetArray(form.BBox); ok {
		if nums, err := core.GetNumbersAsFloat(arr.Elements()); err == nil && len(nums) == 4 {
			bbox = nums
		}
	}
	if bbox == nil {
		common.Log.Debug("ERROR: form XObject missing valid BBox - skipping explicit clip")
		return form.SetContentStream(content, newFlateEncoder())
	}
	x0, x1 := math.Min(bbox[0], bbox[2]), math.Max(bbox[0], bbox[2])
	y0, y1 := math.Min(bbox[1], bbox[3]), math.Max(bbox[1], bbox[3])
	w, h := x1-x0, y1-y0
	ops, err := contentstream.NewContentStreamParser(string(content)).Parse()
	if err != nil {
		common.Log.Debug("Unable to parse appearance stream for clip detection: %v", err)
		ops = nil
	}
	if isClippedTo(ops, x0, y0, w, h) {
		return form.SetContentStream(content, newFlateEncoder())
	}
	content = balanceGraphicsState(ops, content)
	cc := contentstream.NewContentCreator()
	cc.Add_q().Add_re(x0, y0, w, h).Add_W().Add_n()
	out := append(cc.Bytes(), content...)
	if n := len(out); n > 0 && out[n-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, 'Q')
	return form.SetContentStream(out, newFlateEncoder())
}

// ImageFieldAppearance implements interface model.FieldAppearanceGenerator and generates appearance streams
// for attaching an image to a button field.
type ImageFieldAppearance struct {
	OnlyIfMissing bool
	style         *AppearanceStyle
}

func firstControlPoints(rhs []float64) []float64 {
	var (
		n   = len(rhs)
		x   = make([]float64, n)
		tmp = make([]float64, n)
	)
	b := 2.0
	x[0] = rhs[0] / b
	for i := 1; i < n; i++ {
		tmp[i] = 1 / b
		if i < n-1 {
			b = 4.0
		} else {
			b = 3.5
		}
		b -= tmp[i]
		x[i] = (rhs[i] - x[i-1]) / b
	}
	for i := 1; i < n; i++ {
		x[n-i-1] -= tmp[n-i] * x[n-i]
	}
	return x
}
