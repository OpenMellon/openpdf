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
	"math"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/contentstream"
	contentdraw "github.com/OpenMellon/openpdf/contentstream/draw"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func rectangleAppearance(def RectangleAnnotationDef) (*core.PdfObjectDictionary, *model.PdfRectangle, error) {
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
	content, bbox, rect, err := rectangleAppearanceStream(def, gsName)
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

func inkAppearanceStream(inkDef *InkAnnotationDef) ([]byte, *model.PdfRectangle, error) {
	allCurves := [][]contentdraw.CubicBezierCurve{}
	for _, path := range inkDef.Paths {
		if path.Length() == 0 {
			continue
		}
		points := path.Points
		ctrl1, ctrl2, err := bezierControlPoints(points)
		if err != nil {
			return nil, nil, err
		}
		if len(ctrl1) != len(ctrl2) {
			return nil, nil, errors.New("inequal number of calculated first and second control point")
		}
		curves := []contentdraw.CubicBezierCurve{}
		for i := 0; i < len(ctrl1); i++ {
			curves = append(curves, contentdraw.CubicBezierCurve{P0: points[i], P1: ctrl1[i], P2: ctrl2[i], P3: points[i+1]})
		}
		if len(curves) > 0 {
			allCurves = append(allCurves, curves)
		}
	}
	content, bbox, err := strokeCurves(allCurves, inkDef.Color, inkDef.LineWidth)
	if err != nil {
		return nil, nil, err
	}
	return content, bbox, nil
}

// SetStyle applies appearance `style` to `fa`.
func (a *ImageFieldAppearance) SetStyle(style AppearanceStyle) { a.style = &style }

type listboxOption struct {
	value string
	label string
}

func circleAppearanceStream(def CircleAnnotationDef, gsName string) ([]byte, *model.PdfRectangle, *model.PdfRectangle, error) {
	circle := contentdraw.Circle{X: def.X, Y: def.Y, Width: def.Width, Height: def.Height, FillEnabled: def.FillEnabled, FillColor: def.FillColor, BorderEnabled: def.BorderEnabled, BorderWidth: def.BorderWidth, BorderColor: def.BorderColor, Opacity: def.Opacity}
	content, bbox, err := circle.Draw(gsName)
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

func drawSignatureImage(img image.Image, name string, opts *SignatureFieldOpts, rect []float64, cc *contentstream.ContentCreator) (*core.PdfObjectName, *model.XObjectImage, error) {
	modelImg, err := model.DefaultImageHandler{}.NewImageFromGoImage(img)
	if err != nil {
		return nil, nil, err
	}
	xImg, err := model.NewXObjectImageFromImage(modelImg, nil, opts.Encoder)
	if err != nil {
		return nil, nil, err
	}
	w, h := float64(*xImg.Width), float64(*xImg.Height)
	rectW := rect[2] - rect[0]
	rectH := rect[3] - rect[1]
	x, y := rect[0], rect[1]
	if opts.AutoSize {
		scale := math.Min(rectW/w, rectH/h)
		w *= scale
		h *= scale
		x += (rectW / 2) - (w / 2)
		y += (rectH / 2) - (h / 2)
	}
	var imgName *core.PdfObjectName
	if nm, ok := core.GetName(xImg.Name); ok {
		imgName = nm
	} else {
		imgName = core.MakeName(name)
	}
	if cc != nil {
		cc.Add_q().Translate(x, y).Scale(w, h).Add_Do(*imgName).Add_Q()
	} else {
		return nil, nil, errors.New("ContentCreator is null")
	}
	return imgName, xImg, nil
}

func signatureAppearance(lines []*SignatureLine, opts *SignatureFieldOpts) (*core.PdfObjectDictionary, error) {
	if opts == nil {
		opts = NewSignatureFieldOpts()
	}
	var err error
	var fontName *core.PdfObjectName
	font := opts.Font
	if font != nil {
		desc, _ := font.GetFontDescriptor()
		if desc != nil {
			if nm, ok := desc.FontName.(*core.PdfObjectName); ok {
				fontName = nm
			}
		}
		if fontName == nil {
			fontName = core.MakeName("Font1")
		}
	} else {
		if font, err = model.NewStandard14Font("Helvetica"); err != nil {
			return nil, err
		}
		fontName = core.MakeName("Helv")
	}
	fontSize := opts.FontSize
	if fontSize <= 0 {
		fontSize = 10
	}
	if opts.LineHeight <= 0 {
		opts.LineHeight = 1
	}
	leading := opts.LineHeight * fontSize
	spaceMetrics, ok := font.GetRuneMetrics(' ')
	if !ok {
		return nil, errors.New("the font does not have a space glyph")
	}
	spaceWidth := spaceMetrics.Wx
	var maxWidth float64
	var textLines []string
	for _, line := range lines {
		if line.Text == "" {
			continue
		}
		text := line.Text
		if line.Desc != "" {
			text = line.Desc + ": " + text
		}
		textLines = append(textLines, text)
		var lineWidth float64
		for _, r := range text {
			metrics, ok := font.GetRuneMetrics(r)
			if !ok {
				continue
			}
			lineWidth += metrics.Wx
		}
		if lineWidth > maxWidth {
			maxWidth = lineWidth
		}
	}
	maxWidth = maxWidth * fontSize / 1000.0
	textHeight := float64(len(textLines)) * leading
	hasImage := opts.Image != nil
	rect := opts.Rect
	if rect == nil {
		rect = []float64{0, 0, maxWidth, textHeight}
		if hasImage {
			rect[2] = maxWidth * 2
			rect[3] = textHeight * 2
		}
		opts.Rect = rect
	}
	rect = []float64{math.Min(rect[0], rect[2]), math.Min(rect[1], rect[3]), math.Max(rect[0], rect[2]), math.Max(rect[1], rect[3])}
	rectW := rect[2] - rect[0]
	rectH := rect[3] - rect[1]
	imageRect, textRect := rect, rect
	var textW, textH float64
	if hasImage && len(textLines) > 0 {
		if opts.ImagePosition <= SignatureImageRight {
			leftHalf := []float64{rect[0], rect[1], rect[0] + (rectW / 2), rect[3]}
			rightHalf := []float64{rect[0] + (rectW / 2), rect[1], rect[2], rect[3]}
			if opts.ImagePosition == SignatureImageLeft {
				imageRect, textRect = leftHalf, rightHalf
			} else {
				imageRect, textRect = rightHalf, leftHalf
			}
		} else {
			bottomHalf := []float64{rect[0], rect[1], rect[2], rect[1] + (rectH / 2)}
			topHalf := []float64{rect[0], rect[1] + (rectH / 2), rect[2], rect[3]}
			if opts.ImagePosition == SignatureImageTop {
				imageRect, textRect = topHalf, bottomHalf
			} else {
				imageRect, textRect = bottomHalf, topHalf
			}
		}
	}
	textW = textRect[2] - textRect[0]
	textH = textRect[3] - textRect[1]
	var yOffset float64
	if opts.AutoSize {
		if maxWidth > textW || textHeight > textH {
			scale := math.Min(textW/maxWidth, textH/textHeight)
			fontSize *= scale
		}
		leading = opts.LineHeight * fontSize
		yOffset += (textH - float64(len(textLines))*leading) / 2
	}
	cc := contentstream.NewContentCreator()
	resources := model.NewPdfPageResources()
	resources.SetFontByName(*fontName, font.ToPdfObject())
	if opts.BorderSize <= 0 {
		opts.BorderSize = 0
		opts.BorderColor = model.NewPdfColorDeviceGray(1)
	}
	cc.Add_q()
	if opts.FillColor != nil {
		cc.SetNonStrokingColor(opts.FillColor)
	}
	if opts.BorderColor != nil {
		cc.SetStrokingColor(opts.BorderColor)
	}
	gsName := ""
	if opts.FillOpacity < 1.0 {
		idx := 0
		gsName = fmt.Sprintf("GS%d", idx)
		for {
			_, ok := resources.GetExtGState(core.PdfObjectName(gsName))
			if !ok {
				break
			}
			idx++
			gsName = fmt.Sprintf("GS%d", idx)
		}
		gsDict := core.MakeDict()
		gsDict.Set("ca", core.MakeFloat(opts.FillOpacity))
		err := resources.AddExtGState(core.PdfObjectName(gsName), core.MakeIndirectObject(gsDict))
		if err != nil {
			common.Log.Debug(err.Error())
		} else {
			cc.Add_gs(core.PdfObjectName(gsName))
		}
	}
	cc.Add_w(opts.BorderSize).Add_re(rect[0], rect[1], rectW, rectH)
	if opts.FillColor != nil && opts.BorderColor != nil {
		cc.Add_B()
	} else if opts.FillColor != nil {
		cc.Add_f()
	} else if opts.BorderColor != nil {
		cc.Add_S()
	}
	cc.Add_Q()
	if opts.WatermarkImage != nil {
		wmRect := []float64{rect[0], rect[1], rect[2], rect[3]}
		wmName, wmImg, err := drawSignatureImage(opts.WatermarkImage, "ImageWatermark", opts, wmRect, cc)
		if err != nil {
			return nil, err
		}
		resources.SetXObjectImageByName(*wmName, wmImg)
	}
	cc.Add_q()
	cc.Translate(textRect[0], textRect[3]-leading-yOffset)
	cc.Add_BT()
	encoder := font.Encoder()
	for _, textLine := range textLines {
		var buf []byte
		for _, r := range textLine {
			if unicode.IsSpace(r) {
				if len(buf) > 0 {
					cc.SetNonStrokingColor(opts.TextColor).Add_Tf(*fontName, fontSize).Add_TL(leading).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(buf)}...)
					buf = nil
				}
				cc.Add_Tf(*fontName, fontSize).Add_TL(leading).Add_TJ([]core.PdfObject{core.MakeFloat(-spaceWidth)}...)
			} else {
				buf = append(buf, encoder.Encode(string(r))...)
			}
		}
		if len(buf) > 0 {
			cc.SetNonStrokingColor(opts.TextColor).Add_Tf(*fontName, fontSize).Add_TL(leading).Add_TJ([]core.PdfObject{core.MakeStringFromBytes(buf)}...)
		}
		cc.Add_Td(0, -leading)
	}
	cc.Add_ET()
	cc.Add_Q()
	if hasImage {
		imgName, imgXObj, err := drawSignatureImage(opts.Image, "ImageSignature", opts, imageRect, cc)
		if err != nil {
			return nil, err
		}
		resources.SetXObjectImageByName(*imgName, imgXObj)
	}
	form := model.NewXObjectForm()
	form.Resources = resources
	form.BBox = core.MakeArrayFromFloats(rect)
	if err := setClippedContent(form, cc.Bytes()); err != nil {
		return nil, err
	}
	apDict := core.MakeDict()
	apDict.Set("N", form.ToPdfObject())
	return apDict, nil
}

func lineAppearanceStream(def LineAnnotationDef, gsName string) ([]byte, *model.PdfRectangle, *model.PdfRectangle, error) {
	line := contentdraw.Line{X1: 0, Y1: 0, X2: def.X2 - def.X1, Y2: def.Y2 - def.Y1, LineColor: def.LineColor, Opacity: def.Opacity, LineWidth: def.LineWidth, LineEndingStyle1: def.LineEndingStyle1, LineEndingStyle2: def.LineEndingStyle2}
	content, bbox, err := line.Draw(gsName)
	if err != nil {
		return nil, nil, nil, err
	}
	rect := &model.PdfRectangle{}
	rect.Llx = def.X1 + bbox.Llx
	rect.Lly = def.Y1 + bbox.Lly
	rect.Urx = def.X1 + bbox.Urx
	rect.Ury = def.Y1 + bbox.Ury
	return content, bbox, rect, nil
}

// CheckboxFieldOptions defines optional parameters for a checkbox field a form.
type CheckboxFieldOptions struct{ Checked bool }
